package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rcastellor/rcm-outbox/orders-api/internal/domain"
)

// ErrNotFound se devuelve cuando la orden no existe o fue eliminada.
var ErrNotFound = errors.New("orden no encontrada")

// Orders es el repositorio de órdenes. Las operaciones de escritura persisten
// la orden y su evento de outbox en la misma transacción.
type Orders struct {
	pool *pgxpool.Pool
}

// NewOrders crea el repositorio de órdenes.
func NewOrders(pool *pgxpool.Pool) *Orders {
	return &Orders{pool: pool}
}

// Create inserta una orden y sus líneas, y escribe el evento CreatedOrder en el
// outbox dentro de la misma transacción.
func (r *Orders) Create(ctx context.Context, o *domain.Order) (*domain.Order, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("iniciando transacción: %w", err)
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx,
		`INSERT INTO orders (customer_id, status) VALUES ($1, $2) RETURNING id, created_at, updated_at`,
		o.CustomerID, o.Status,
	).Scan(&o.ID, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("insertando orden: %w", err)
	}

	if err := insertLines(ctx, tx, o); err != nil {
		return nil, err
	}
	if err := appendOutbox(ctx, tx, o.ID, domain.EventCreatedOrder, o); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("confirmando transacción: %w", err)
	}
	return o, nil
}

// Get devuelve una orden activa con sus líneas.
func (r *Orders) Get(ctx context.Context, id string) (*domain.Order, error) {
	o := &domain.Order{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, customer_id, status, created_at, updated_at, deleted_at
		 FROM orders WHERE id = $1 AND deleted_at IS NULL`, id,
	).Scan(&o.ID, &o.CustomerID, &o.Status, &o.CreatedAt, &o.UpdatedAt, &o.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("consultando orden: %w", err)
	}

	lines, err := r.linesByOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	o.Lines = lines
	return o, nil
}

// List devuelve las órdenes activas con sus líneas.
func (r *Orders) List(ctx context.Context) ([]domain.Order, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, customer_id, status, created_at, updated_at
		 FROM orders WHERE deleted_at IS NULL ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("listando órdenes: %w", err)
	}
	defer rows.Close()

	orders := []domain.Order{}
	index := map[string]int{}
	var ids []string
	for rows.Next() {
		var o domain.Order
		if err := rows.Scan(&o.ID, &o.CustomerID, &o.Status, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, fmt.Errorf("escaneando orden: %w", err)
		}
		index[o.ID] = len(orders)
		orders = append(orders, o)
		ids = append(ids, o.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterando órdenes: %w", err)
	}
	if len(orders) == 0 {
		return orders, nil
	}

	lineRows, err := r.pool.Query(ctx,
		`SELECT id, order_id, product_id, quantity, unit_price
		 FROM orders_lines WHERE order_id = ANY($1) ORDER BY order_id, id`, ids)
	if err != nil {
		return nil, fmt.Errorf("listando líneas de órdenes: %w", err)
	}
	defer lineRows.Close()

	for lineRows.Next() {
		var l domain.OrderLine
		var orderID string
		if err := lineRows.Scan(&l.ID, &orderID, &l.ProductID, &l.Quantity, &l.UnitPrice); err != nil {
			return nil, fmt.Errorf("escaneando línea: %w", err)
		}
		if i, ok := index[orderID]; ok {
			orders[i].Lines = append(orders[i].Lines, l)
		}
	}
	if err := lineRows.Err(); err != nil {
		return nil, fmt.Errorf("iterando líneas: %w", err)
	}
	return orders, nil
}

// Update actualiza una orden activa y sus líneas, y escribe el evento
// UpdatedOrder en el outbox dentro de la misma transacción.
func (r *Orders) Update(ctx context.Context, o *domain.Order) (*domain.Order, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("iniciando transacción: %w", err)
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx,
		`UPDATE orders SET customer_id = $2, status = $3, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING created_at, updated_at`,
		o.ID, o.CustomerID, o.Status,
	).Scan(&o.CreatedAt, &o.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("actualizando orden: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM orders_lines WHERE order_id = $1`, o.ID); err != nil {
		return nil, fmt.Errorf("eliminando líneas de orden: %w", err)
	}
	if err := insertLines(ctx, tx, o); err != nil {
		return nil, err
	}
	if err := appendOutbox(ctx, tx, o.ID, domain.EventUpdatedOrder, o); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("confirmando transacción: %w", err)
	}
	return o, nil
}

// Delete marca una orden como eliminada (soft-delete) y escribe el evento
// DeletedOrder en el outbox dentro de la misma transacción.
func (r *Orders) Delete(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("iniciando transacción: %w", err)
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`UPDATE orders SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("eliminando orden: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	payload := struct {
		ID string `json:"id"`
	}{ID: id}
	if err := appendOutbox(ctx, tx, id, domain.EventDeletedOrder, payload); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("confirmando transacción: %w", err)
	}
	return nil
}

func (r *Orders) linesByOrder(ctx context.Context, orderID string) ([]domain.OrderLine, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, product_id, quantity, unit_price FROM orders_lines WHERE order_id = $1 ORDER BY id`, orderID)
	if err != nil {
		return nil, fmt.Errorf("consultando líneas de orden: %w", err)
	}
	defer rows.Close()

	lines := []domain.OrderLine{}
	for rows.Next() {
		var l domain.OrderLine
		if err := rows.Scan(&l.ID, &l.ProductID, &l.Quantity, &l.UnitPrice); err != nil {
			return nil, fmt.Errorf("escaneando línea de orden: %w", err)
		}
		lines = append(lines, l)
	}
	return lines, rows.Err()
}

func insertLines(ctx context.Context, tx pgx.Tx, o *domain.Order) error {
	for i := range o.Lines {
		err := tx.QueryRow(ctx,
			`INSERT INTO orders_lines (order_id, product_id, quantity, unit_price)
			 VALUES ($1, $2, $3, $4) RETURNING id`,
			o.ID, o.Lines[i].ProductID, o.Lines[i].Quantity, o.Lines[i].UnitPrice,
		).Scan(&o.Lines[i].ID)
		if err != nil {
			return fmt.Errorf("insertando línea de orden: %w", err)
		}
	}
	return nil
}

func appendOutbox(ctx context.Context, tx pgx.Tx, aggregateID, eventType string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("serializando evento %s: %w", eventType, err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO outbox (id, aggregate_id, event_type, payload)
		 VALUES (gen_random_uuid(), $1, $2, $3::jsonb)`,
		aggregateID, eventType, string(b)); err != nil {
		return fmt.Errorf("escribiendo evento outbox %s: %w", eventType, err)
	}
	return nil
}
