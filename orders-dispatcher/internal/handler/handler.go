package handler

import (
	"context"

	"github.com/aws/aws-lambda-go/events"

	"github.com/rcastellor/rcm-outbox/orders-dispatcher/internal/dispatcher"
)

// Handler adapta las invocaciones de EventBridge al dispatcher.
type Handler struct {
	dispatcher *dispatcher.Dispatcher
}

// New crea el handler de la Lambda.
func New(d *dispatcher.Dispatcher) *Handler {
	return &Handler{dispatcher: d}
}

// Handle procesa una invocación del dispatcher (disparada por EventBridge). El
// payload del evento se ignora: el dispatcher cuenta y encola según pendientes.
func (h *Handler) Handle(ctx context.Context, _ events.CloudWatchEvent) error {
	return h.dispatcher.Run(ctx)
}
