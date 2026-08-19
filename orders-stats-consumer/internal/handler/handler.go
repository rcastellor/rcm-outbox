package handler

import (
	"context"

	"github.com/aws/aws-lambda-go/events"

	"github.com/rcastellor/rcm-outbox/orders-stats-consumer/internal/stats"
)

// Handler adapta las invocaciones de SQS al agregador de estadísticas.
type Handler struct {
	stats *stats.Aggregator
}

// New crea el handler de la Lambda.
func New(a *stats.Aggregator) *Handler {
	return &Handler{stats: a}
}

// Handle procesa una invocación de la cola de consumidores (disparada por el
// event source mapping). Con raw message delivery el body de cada record es el
// mensaje SNS original. Si Process falla, se devuelve el error para que SQS
// reintente el mensaje.
func (h *Handler) Handle(ctx context.Context, event events.SQSEvent) error {
	for _, record := range event.Records {
		if err := h.stats.Process(ctx, []byte(record.Body)); err != nil {
			return err
		}
	}
	return nil
}
