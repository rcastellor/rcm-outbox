# AGENTS.md

Go monorepo implementing the transactional outbox pattern on AWS (Lambda + Pulumi). Managed as a Go workspace (`go.work`); every module has its own `go.mod`.

## Commands (all via Makefile)
- `make build` — compile all Lambdas to `bin/<module>/bootstrap`
- `make test` / `make lint` — `go test ./...` / `go vet ./...` per module
- `make up` / `make down` / `make logs` — start/stop floci (local AWS emulator) via Docker
- `make infra-init|refresh|preview|up|destroy` — Pulumi stack `dev`
- `make migrate` — aplica las migraciones pendientes invocando la Lambda `rcm-migrations`
- `make redrive QUEUE=<cola>` — reencola los mensajes de una DLQ en su cola origen (p. ej. `rcm-outbox-dispatch`)
- `make test-e2e` — test end-to-end del flujo completo contra floci (requiere `up` + `build` + `infra-up` + `migrate`)

Do NOT run `go test ./...` from the repo root — there is no root package (workspace mode errors). Run inside a module dir or use the Make targets, which `cd` into each module.

## Modules
- `orders-api`, `orders-workers`, `orders-stats-consumer`, `orders-dispatcher` — AWS Lambda functions. Entrypoint `cmd/lambda/main.go` → `internal/handler/handler.go`. Each is an independent module at `github.com/rcastellor/rcm-outbox/<module>`.
- `rcm-migrations` — Lambda que aplica las migraciones SQL (embebidas como strings inline en `internal/migrations`); se invoca manualmente con `make migrate`.
- `rcm-platform` — biblioteca compartida (no es Lambda) con código común de conexión a BD y utilidades: `logger`, `config`, `secrets`, `database`. La consumen las Lambdas vía `github.com/rcastellor/rcm-outbox/rcm-platform/...`.
- `infra` — Pulumi (Go) IaC, module `.../infra`. Layout interno: `internal/platform` (un paquete único con un fichero por componente: `database.go`, `dynamodb.go`, `function.go`, `sns.go`, `sqs.go`, `secrets.go`), `internal/domains` (composición por dominio de negocio: `migrations`, `orders`, `outbox`, `stats`) e `internal/config` (carga del stack y tipos compartidos). Regla de dependencias: `domains → {platform, config}` y `platform → config`; nunca al revés.
- `test/e2e` — test end-to-end del flujo completo contra floci (`make test-e2e`).

## Build / Lambda quirks
- Lambdas build to a single binary named `bootstrap` (`GOOS=linux GOARCH=amd64 CGO_ENABLED=0`, target `./cmd/lambda`); the name is required by the Lambda custom runtime.
- `bin/` is gitignored (build artifacts).

## Local AWS (floci)
- The local AWS emulator is **floci**, not LocalStack (`local/docker-compose.yml`).
- floci runs Lambdas in disposable Docker containers, so it mounts `/var/run/docker.sock`; without it, invoke fails with "Failed to start Lambda container".
- Endpoint `http://localhost:4566`, UI `http://localhost:4500`.
- The Makefile exports `AWS_ENDPOINT_URL` + fake creds (`test`/`test`, `us-east-1`). Running `pulumi`, `aws`, or `go test` directly (outside `make`) targets real AWS unless you export these yourself.
- floci's persisted state (`local/data/` + `floci-rds-db-*` volumes) can get corrupted between sessions (e.g. DynamoDB tables marked `.corrupt`, EventBridge scheduler not restored). Prefer `make reset-floci` when starting work; see [docs/floci-local.md](docs/floci-local.md) for the caveats and the cold-start flow.

## Pulumi
- Stack `dev`, local backend (`pulumi login --local`), runtime Go.
- `aws:region: us-east-1` is in `infra/Pulumi.dev.yaml`; la configuración del stack se lee en el paquete `infra/internal/config` (`LoadConfig`, claves `database`, `topics`, `worker`). El usuario de BD por defecto es `rcmoutbox` (`infra/internal/platform/database.go`).
- Componentes implementados: `platform` (un fichero por componente: `database`, `dynamodb`, `secrets`, `sns`/`sqs`, `function`) y los dominios `migrations`, `orders` (API HTTP), `outbox` (cola, worker, dispatcher y schedule) y `stats` (cola de consumidores, tabla DynamoDB y consumidor). Los roles IAM se crean dentro del componente genérico de Lambda (`platform.Function`).
- The `worker` config key (`topicName`, `batchSize`, `maxWorkers`) configures the outbox dispatcher/worker; see [docs/outbox-worker.md](docs/outbox-worker.md).
- Migraciones SQL embebidas en la Lambda `rcm-migrations` (strings inline + lista exportada por dominio); el componente `infra/internal/domains/migrations` solo la despliega y se aplican con `make migrate`; see [docs/migrations-conventions.md](docs/migrations-conventions.md).
- El componente `orders` expone `orders-api` vía una **API Gateway HTTP API (v2)** con Lambda proxy payload 2.0; see [docs/orders-api.md](docs/orders-api.md).
- El dominio `stats` despliega el consumidor de estadísticas (cola `orders-stats`, tabla DynamoDB y Lambda `orders-stats-consumer`); see [docs/stats-consumer.md](docs/stats-consumer.md).

## Conventions
- Code comments and commit messages are written in Spanish; match that style.
- Infrastructure config/types follow the conventions in [docs/config-conventions.md](docs/config-conventions.md).
