package handler

import (
	"context"

	"github.com/aws/aws-lambda-go/events"

	"github.com/rcastellor/rcm-outbox/orders-workers/internal/worker"
)

// Handler adapta las invocaciones de SQS al worker.
type Handler struct {
	worker *worker.Worker
}

// New crea el handler de la Lambda.
func New(w *worker.Worker) *Handler {
	return &Handler{worker: w}
}

// Handle procesa una invocación del worker (disparada por SQS). Cada mensaje de
// la cola de dispatch corresponde a un batch; con BatchSize=1 el event source
// mapping entrega un mensaje por invocación. Si Process falla, se devuelve el
// error para que SQS reintente el mensaje.
func (h *Handler) Handle(ctx context.Context, event events.SQSEvent) error {
	for range event.Records {
		if err := h.worker.Process(ctx); err != nil {
			return err
		}
	}
	return nil
}
