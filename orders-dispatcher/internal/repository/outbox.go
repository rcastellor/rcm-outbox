package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Outbox es el repositorio del dispatcher sobre la tabla outbox.
type Outbox struct {
	pool *pgxpool.Pool
}

// NewOutbox crea el repositorio del outbox.
func NewOutbox(pool *pgxpool.Pool) *Outbox {
	return &Outbox{pool: pool}
}

// CountPending cuenta los registros del outbox pendientes y disponibles
// (same predicate que el claim del worker: status pending y available_at
// cumplido).
func (r *Outbox) CountPending(ctx context.Context) (int, error) {
	var count int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*)
		 FROM outbox
		 WHERE status = 'pending'
		   AND (available_at IS NULL OR available_at <= now())`).Scan(&count); err != nil {
		return 0, fmt.Errorf("contando registros pendientes del outbox: %w", err)
	}
	return count, nil
}
