SHELL := /bin/bash

COMPOSE      := docker compose
COMPOSE_FILE := local/docker-compose.yml

MODULES  := rcm-migrations orders-api orders-workers orders-stats-consumer orders-dispatcher
LIBS     := rcm-platform
INFRA_DIR := infra
BIN_DIR  := bin

# Endpoint del emulador local floci (AWS).
FLOCI_ENDPOINT := http://localhost:4566

# Entorno AWS apuntando a floci. Se exporta para que todos los comandos
# (pulumi, aws cli, go test, ...) hablen con el emulador en vez de AWS real.
export AWS_ENDPOINT_URL            := $(FLOCI_ENDPOINT)
export AWS_ACCESS_KEY_ID           := test
export AWS_SECRET_ACCESS_KEY       := test
export AWS_DEFAULT_REGION          := us-east-1
export AWS_SKIP_METADATA_API_CHECK := true

.PHONY: help up down logs ps build $(addprefix build-,$(MODULES)) test test-e2e lint migrate redrive \
        infra-init infra-preview infra-up infra-destroy clean

help: ## Muestra esta ayuda
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

up: ## Arranca floci + floci-ui en local
	$(COMPOSE) -f $(COMPOSE_FILE) up -d

down: ## Detiene floci + floci-ui
	$(COMPOSE) -f $(COMPOSE_FILE) down

logs: ## Sigue los logs de floci + floci-ui
	$(COMPOSE) -f $(COMPOSE_FILE) logs -f

ps: ## Muestra el estado de los servicios locales
	$(COMPOSE) -f $(COMPOSE_FILE) ps

build: $(addprefix build-,$(MODULES)) ## Compila todas las lambdas (bin/<modulo>/bootstrap)

define BUILD_LAMBDA
build-$(1): ; @echo "==> building $(1)" && mkdir -p $(BIN_DIR)/$(1) && (cd $(1) && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ../$(BIN_DIR)/$(1)/bootstrap ./cmd/lambda)
endef

$(foreach m,$(MODULES),$(eval $(call BUILD_LAMBDA,$(m))))

migrate: ## Aplica las migraciones pendientes invocando la lambda rcm-migrations
	@out=$$(mktemp) && \
	fn=$$(cd $(INFRA_DIR) && pulumi stack output migrationsFunctionName) && \
	aws lambda invoke --function-name "$$fn" --payload '{}' \
		--cli-binary-format raw-in-base64-out "$$out" && cat "$$out" && rm -f "$$out"

# Reencola los mensajes de la DLQ de una cola hacia su cola origen. Preserva
# body, atributos y MessageGroupId; regenera el MessageDeduplicationId (sufijo
# -redrive-<epoch>) para que la deduplicación FIFO no descarte el redrive.
redrive: ## Mueve los mensajes de una DLQ a su cola origen (uso: make redrive QUEUE=<cola>)
	@test -n "$(QUEUE)" || { echo "uso: make redrive QUEUE=<cola> (p.ej. rcm-outbox-dispatch o rcm-outbox-orders-stats.fifo)"; exit 1; }
	@src="$(QUEUE)"; \
	case "$$src" in *.fifo) dlq="$${src%.fifo}-dlq.fifo" ;; *) dlq="$$src-dlq" ;; esac; \
	src_url=$$(aws sqs get-queue-url --queue-name "$$src" --query QueueUrl --output text) || exit 1; \
	dlq_url=$$(aws sqs get-queue-url --queue-name "$$dlq" --query QueueUrl --output text) || exit 1; \
	echo "==> redrive $$dlq -> $$src"; \
	total=0; ts=$$(date +%s); \
	while :; do \
		resp=$$(aws sqs receive-message --queue-url "$$dlq_url" --max-number-of-messages 10 \
			--message-attribute-names All --wait-time-seconds 1); \
		[ -z "$$resp" ] && break; \
		while read -r msg; do \
			send=$$(printf '%s' "$$msg" | jq --arg url "$$src_url" --arg ts "$$ts" '{QueueUrl: $$url, MessageBody: .Body, MessageAttributes: (.MessageAttributes // {})} + (if .MessageGroupId then {MessageGroupId: .MessageGroupId, MessageDeduplicationId: ((.MessageDeduplicationId // "")[0:100] + "-redrive-" + $$ts)} else {} end)'); \
			aws sqs send-message --cli-input-json "$$send" > /dev/null || { echo "error reencolando en $$src"; exit 1; }; \
			aws sqs delete-message --queue-url "$$dlq_url" \
				--receipt-handle "$$(printf '%s' "$$msg" | jq -r .ReceiptHandle)" > /dev/null \
				|| { echo "error borrando de $$dlq"; exit 1; }; \
			total=$$((total + 1)); \
		done < <(printf '%s' "$$resp" | jq -c '.Messages[]?'); \
	done; \
	echo "==> $$total mensajes reencolados"

test: ## Ejecuta los tests de todos los módulos
	@for m in $(MODULES) $(LIBS) $(INFRA_DIR); do \
		echo "==> testing $$m"; \
		(cd $$m && go test ./...) || exit 1; \
	done

test-e2e: ## Ejecuta el test e2e contra floci (requiere up + build + infra-up + migrate)
	@url=$$(cd $(INFRA_DIR) && pulumi stack output ordersApiUrl) && \
	url=$$(printf '%s' "$$url" | sed -E 's|https://([^.]+)\.execute-api\.[^/]+|$(FLOCI_ENDPOINT)/restapis/\1|;s|(/restapis/[^/]+/[^/]+)$$|\1/_user_request_|') && \
	echo "==> e2e contra $$url" && \
	ORDERS_API_URL="$$url" go test ./test/e2e/... -v -count=1 -timeout 300s

lint: ## go vet sobre todos los módulos
	@for m in $(MODULES) $(LIBS) $(INFRA_DIR); do \
		echo "==> vet $$m"; \
		(cd $$m && go vet ./...) || exit 1; \
	done

infra-init: ## Inicializa el stack dev (backend local + stack init)
	cd $(INFRA_DIR) && pulumi login --local && pulumi stack init dev

infra-refresh : ## Pulumi refresh de la infraestructura
	cd $(INFRA_DIR) && pulumi refresh

infra-preview: ## Pulumi preview de la infraestructura
	cd $(INFRA_DIR) && pulumi preview

infra-up: ## Pulumi up de la infraestructura
	cd $(INFRA_DIR) && pulumi up

infra-destroy: ## Pulumi destroy de la infraestructura
	cd $(INFRA_DIR) && pulumi destroy

clean: ## Elimina los binarios generados
	rm -rf $(BIN_DIR)
