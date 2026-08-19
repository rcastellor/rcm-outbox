package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/rcastellor/rcm-outbox/orders-workers/internal/backoff"
	"github.com/rcastellor/rcm-outbox/orders-workers/internal/domain"
)

// leaseTimeout es el tiempo máximo que un registro puede permanecer reclamado
// antes de considerarse huérfano (worker caído) y devolverse a pendiente.
const leaseTimeout = 2 * time.Minute

// outboxRepository abstrae las operaciones del repositorio que necesita el
// worker, para poder probarlo con un fake.
type outboxRepository interface {
	ClaimPending(ctx context.Context, limit int) ([]domain.OutboxEvent, error)
	MarkPublished(ctx context.Context, id string) error
	MarkRetry(ctx context.Context, id string, availableAt time.Time, lastError string) error
	MarkDead(ctx context.Context, id, lastError string) error
	ResetStuck(ctx context.Context, leaseTimeout time.Duration) error
}

// eventPublisher abstrae la publicación de eventos en SNS.
type eventPublisher interface {
	Publish(ctx context.Context, evt domain.OutboxEvent) error
}

// Worker procesa un único batch de registros pendientes de la tabla outbox: los
// reclama en una transacción corta, publica en SNS fuera de transacción y
// registra el resultado (publicado, reintento con backoff o dead).
type Worker struct {
	repo        outboxRepository
	publisher   eventPublisher
	batchSize   int
	maxAttempts int
	backoffBase time.Duration
	maxBackoff  time.Duration
	logger      *slog.Logger
}

// New crea el worker con sus dependencias.
func New(repo outboxRepository, publisher eventPublisher, batchSize, maxAttempts int, backoffBase, maxBackoff time.Duration, logger *slog.Logger) *Worker {
	return &Worker{
		repo:        repo,
		publisher:   publisher,
		batchSize:   batchSize,
		maxAttempts: maxAttempts,
		backoffBase: backoffBase,
		maxBackoff:  maxBackoff,
		logger:      logger,
	}
}

// Process reclama y publica un único batch de registros. FOR UPDATE SKIP LOCKED
// garantiza que las invocaciones concurrentes no reclaman los mismos registros.
// Antes de procesar, resetea los registros reclamados cuyo lease expiró.
func (w *Worker) Process(ctx context.Context) error {
	if err := w.repo.ResetStuck(ctx, leaseTimeout); err != nil {
		w.logger.Error("no se pudieron resetear registros reclamados", "error", err)
	}

	events, err := w.repo.ClaimPending(ctx, w.batchSize)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}

	published := 0
	for _, evt := range events {
		if err := w.publisher.Publish(ctx, evt); err != nil {
			w.logger.Error("evento no publicado", "event_type", evt.EventType, "aggregate_id", evt.AggregateID, "attempts", evt.Attempts, "error", err)
			if err := w.handleFailure(ctx, evt, err); err != nil {
				return err
			}
			continue
		}
		if err := w.repo.MarkPublished(ctx, evt.ID); err != nil {
			return err
		}
		w.logger.Info("evento publicado", "event_type", evt.EventType, "aggregate_id", evt.AggregateID)
		published++
	}

	if published > 0 {
		w.logger.Info("bloque de outbox procesado", "published", published, "claimed", len(events))
	}
	return nil
}

// handleFailure decide qué hacer con un evento que no se pudo publicar: si
// agotó los reintentos lo marca como dead; si no, programa un reintento con
// backoff exponencial y jitter.
func (w *Worker) handleFailure(ctx context.Context, evt domain.OutboxEvent, publishErr error) error {
	if evt.Attempts >= w.maxAttempts {
		if err := w.repo.MarkDead(ctx, evt.ID, publishErr.Error()); err != nil {
			return err
		}
		w.logger.Error("evento movido a DLQ", "event_type", evt.EventType, "aggregate_id", evt.AggregateID, "attempts", evt.Attempts)
		return nil
	}

	delay := backoff.NextAttemptDelay(evt.Attempts, w.backoffBase, w.maxBackoff)
	availableAt := time.Now().Add(delay)
	if err := w.repo.MarkRetry(ctx, evt.ID, availableAt, publishErr.Error()); err != nil {
		return err
	}
	w.logger.Info("evento reintentado con backoff", "event_type", evt.EventType, "aggregate_id", evt.AggregateID, "attempts", evt.Attempts, "delay", delay)
	return nil
}
