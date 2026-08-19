package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/aws/aws-lambda-go/events"

	"github.com/rcastellor/rcm-outbox/orders-api/internal/router"
)

// Handler adapta las peticiones de API Gateway (HTTP API, payload 2.0) al router.
type Handler struct {
	router *router.Router
	logger *slog.Logger
}

// New crea el handler de la Lambda.
func New(r *router.Router, logger *slog.Logger) *Handler {
	return &Handler{router: r, logger: logger}
}

// Handle procesa una petición de API Gateway.
func (h *Handler) Handle(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	status, body := h.router.Route(ctx, req.RequestContext.HTTP.Method, subpath(req.RawPath), []byte(req.Body))

	resp := events.APIGatewayV2HTTPResponse{
		StatusCode: status,
		Headers:    map[string]string{"Content-Type": "application/json"},
	}

	if body == nil {
		return resp, nil
	}

	b, err := json.Marshal(body)
	if err != nil {
		h.logger.Error("serializando respuesta", "error", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"error":"error interno"}`,
		}, nil
	}
	resp.Body = string(b)
	return resp, nil
}

// subpath devuelve la ruta relativa a /orders a partir de la ruta completa de
// la petición. Es robusto ante emuladores locales que rellenan {proxy+} con la
// ruta completa en lugar del segmento relativo.
func subpath(path string) string {
	p := strings.TrimPrefix(path, "/orders")
	return strings.TrimPrefix(p, "/")
}
