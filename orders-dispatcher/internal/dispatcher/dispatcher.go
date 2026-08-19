package dispatcher

import (
	"context"
	"log/slog"
)

// outboxRepository abstrae el conteo de registros pendientes del outbox, para
// poder probar el dispatcher con un fake.
type outboxRepository interface {
	CountPending(ctx context.Context) (int, error)
}

// queuePublisher abstrae el encolado de trabajos en la cola de dispatch.
type queuePublisher interface {
	SendBatch(ctx context.Context, n int) error
}

// Dispatcher calcula cuántos workers son necesarios en función de los registros
// pendientes del outbox y encola un mensaje de trabajo por cada uno.
type Dispatcher struct {
	repo       outboxRepository
	queue      queuePublisher
	batchSize  int
	maxWorkers int
	logger     *slog.Logger
}

// New crea el dispatcher con sus dependencias.
func New(repo outboxRepository, queue queuePublisher, batchSize, maxWorkers int, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		repo:       repo,
		queue:      queue,
		batchSize:  batchSize,
		maxWorkers: maxWorkers,
		logger:     logger,
	}
}

// Run cuenta los registros pendientes y encola ceil(pending/batchSize) trabajos,
// acotado a maxWorkers. Si no hay pendientes, no encola nada.
func (d *Dispatcher) Run(ctx context.Context) error {
	pending, err := d.repo.CountPending(ctx)
	if err != nil {
		return err
	}

	if pending == 0 {
		d.logger.Info("sin registros pendientes en el outbox")
		return nil
	}

	workers := (pending + d.batchSize - 1) / d.batchSize
	if workers > d.maxWorkers {
		workers = d.maxWorkers
	}

	d.logger.Info("encolando trabajos de outbox", "pending", pending, "workers", workers)
	return d.queue.SendBatch(ctx, workers)
}
