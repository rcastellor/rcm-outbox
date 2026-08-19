package handler

import (
	"context"

	"github.com/rcastellor/rcm-outbox/rcm-migrations/internal/runner"
)

// Handler atiende las invocaciones manuales de la Lambda de migraciones.
type Handler struct {
	runner *runner.Runner
}

// New crea el handler de la Lambda.
func New(r *runner.Runner) *Handler {
	return &Handler{runner: r}
}

// Handle aplica las migraciones pendientes. La lambda solo soporta "up": el
// payload de la invocación se ignora y la respuesta resume lo aplicado.
func (h *Handler) Handle(ctx context.Context) (*runner.Result, error) {
	return h.runner.Run(ctx)
}
