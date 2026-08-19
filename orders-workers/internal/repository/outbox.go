package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rcastellor/rcm-outbox/orders-workers/internal/domain"
)

// Estados del ciclo de vida de un registro del outbox.
const (
	StatusPending   = "pending"
	StatusClaimed   = "claimed"
	StatusPublished = "published"
	StatusDead      = "dead"
)

// Outbox es el repositorio del worker sobre la tabla outbox.
type Outbox struct {
	pool *pgxpool.Pool
}

// NewOutbox crea el repositorio del outbox.
func NewOutbox(pool *pgxpool.Pool) *Outbox {
	return &Outbox{pool: pool}
}

// ClaimPending reclama de forma atómica hasta limit registros pendientes y
// disponibles, marcándolos como reclamados e incrementando su contador de
// intentos. Usa FOR UPDATE SKIP LOCKED para que las invocaciones concurrentes
// no reclamen los mismos registros; al ser una única sentencia, los locks se
// liberan al terminar el statement (no se retienen durante la publicación).
func (r *Outbox) ClaimPending(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	rows, err := r.pool.Query(ctx,
		`UPDATE outbox
		 SET status = 'claimed', claimed_at = now(), attempts = attempts + 1
		 WHERE id IN (
		   SELECT id
		   FROM outbox
		   WHERE published_at IS NULL
		     AND status = 'pending'
		     AND (available_at IS NULL OR available_at <= now())
		   ORDER BY created_at
		   LIMIT $1
		   FOR UPDATE SKIP LOCKED
		 )
		 RETURNING id, aggregate_id, event_type, payload, attempts`, limit)
	if err != nil {
		return nil, fmt.Errorf("reclamando registros pendientes del outbox: %w", err)
	}
	defer rows.Close()

	events := []domain.OutboxEvent{}
	for rows.Next() {
		var e domain.OutboxEvent
		if err := rows.Scan(&e.ID, &e.AggregateID, &e.EventType, &e.Payload, &e.Attempts); err != nil {
			return nil, fmt.Errorf("escaneando registro del outbox: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// MarkPublished marca un registro del outbox como publicado con éxito.
func (r *Outbox) MarkPublished(ctx context.Context, id string) error {
	if _, err := r.pool.Exec(ctx,
		`UPDATE outbox SET status = 'published', published_at = now() WHERE id = $1`, id); err != nil {
		return fmt.Errorf("marcando outbox %s como publicado: %w", id, err)
	}
	return nil
}

// MarkRetry devuelve un registro a pendiente con un nuevo available_at
// (backoff + jitter) y guarda el error del intento fallido.
func (r *Outbox) MarkRetry(ctx context.Context, id string, availableAt time.Time, lastError string) error {
	if _, err := r.pool.Exec(ctx,
		`UPDATE outbox SET status = 'pending', available_at = $2, last_error = $3 WHERE id = $1`,
		id, availableAt, lastError); err != nil {
		return fmt.Errorf("programando reintento del outbox %s: %w", id, err)
	}
	return nil
}

// MarkDead marca un registro como fallido de forma definitiva tras agotar los
// reintentos (DLQ lógica en la propia tabla).
func (r *Outbox) MarkDead(ctx context.Context, id, lastError string) error {
	if _, err := r.pool.Exec(ctx,
		`UPDATE outbox SET status = 'dead', last_error = $2 WHERE id = $1`, id, lastError); err != nil {
		return fmt.Errorf("marcando outbox %s como dead: %w", id, err)
	}
	return nil
}

// ResetStuck devuelve a pendiente los registros reclamados cuyo lease expiró
// (p.ej. un worker que cayó entre el claim y el ack/nack).
func (r *Outbox) ResetStuck(ctx context.Context, leaseTimeout time.Duration) error {
	if _, err := r.pool.Exec(ctx,
		`UPDATE outbox
		 SET status = 'pending', available_at = now()
		 WHERE status = 'claimed' AND claimed_at < now() - ($1 * interval '1 second')`,
		int(leaseTimeout.Seconds())); err != nil {
		return fmt.Errorf("reseteando registros reclamados del outbox: %w", err)
	}
	return nil
}
