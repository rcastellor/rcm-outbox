package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/rcastellor/rcm-outbox/orders-api/internal/domain"
	"github.com/rcastellor/rcm-outbox/orders-api/internal/repository"
)

// ErrInvalidInput indica que los datos de entrada no son válidos.
var ErrInvalidInput = errors.New("entrada inválida")

// LineInput describe una línea de orden de entrada.
type LineInput struct {
	ProductID string  `json:"productId"`
	Quantity  int     `json:"quantity"`
	UnitPrice float64 `json:"unitPrice"`
}

// CreateOrderInput describe la entrada para crear una orden.
type CreateOrderInput struct {
	CustomerID string      `json:"customerId"`
	Status     string      `json:"status"`
	Lines      []LineInput `json:"lines"`
}

// UpdateOrderInput describe la entrada para actualizar una orden.
type UpdateOrderInput struct {
	CustomerID string      `json:"customerId"`
	Status     string      `json:"status"`
	Lines      []LineInput `json:"lines"`
}

// Orders agrupa los casos de uso del CRUD de órdenes.
type Orders struct {
	repo   *repository.Orders
	logger *slog.Logger
}

// NewOrders crea el servicio de casos de uso de órdenes.
func NewOrders(repo *repository.Orders, logger *slog.Logger) *Orders {
	return &Orders{repo: repo, logger: logger}
}

// Create crea una orden y sus líneas.
func (u *Orders) Create(ctx context.Context, in CreateOrderInput) (*domain.Order, error) {
	if err := validate(in.CustomerID, in.Status, in.Lines); err != nil {
		return nil, err
	}

	o, err := u.repo.Create(ctx, &domain.Order{
		CustomerID: in.CustomerID,
		Status:     in.Status,
		Lines:      toLines(in.Lines),
	})
	if err != nil {
		u.logger.Error("orden no creada", "error", err)
		return nil, err
	}
	u.logger.Info("orden creada", "event", domain.EventCreatedOrder, "order_id", o.ID, "lines", len(o.Lines))
	return o, nil
}

// Get devuelve una orden por su id.
func (u *Orders) Get(ctx context.Context, id string) (*domain.Order, error) {
	o, err := u.repo.Get(ctx, id)
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			u.logger.Error("orden no obtenida", "order_id", id, "error", err)
		}
		return nil, err
	}
	u.logger.Info("orden obtenida", "order_id", id)
	return o, nil
}

// List devuelve todas las órdenes activas.
func (u *Orders) List(ctx context.Context) ([]domain.Order, error) {
	orders, err := u.repo.List(ctx)
	if err != nil {
		u.logger.Error("órdenes no listadas", "error", err)
		return nil, err
	}
	u.logger.Info("órdenes listadas", "count", len(orders))
	return orders, nil
}

// Update actualiza una orden y sus líneas.
func (u *Orders) Update(ctx context.Context, id string, in UpdateOrderInput) (*domain.Order, error) {
	if err := validate(in.CustomerID, in.Status, in.Lines); err != nil {
		return nil, err
	}

	o, err := u.repo.Update(ctx, &domain.Order{
		ID:         id,
		CustomerID: in.CustomerID,
		Status:     in.Status,
		Lines:      toLines(in.Lines),
	})
	if err != nil {
		u.logger.Error("orden no actualizada", "order_id", id, "error", err)
		return nil, err
	}
	u.logger.Info("orden actualizada", "event", domain.EventUpdatedOrder, "order_id", o.ID, "lines", len(o.Lines))
	return o, nil
}

// Delete elimina (soft-delete) una orden.
func (u *Orders) Delete(ctx context.Context, id string) error {
	if err := u.repo.Delete(ctx, id); err != nil {
		u.logger.Error("orden no eliminada", "order_id", id, "error", err)
		return err
	}
	u.logger.Info("orden eliminada", "event", domain.EventDeletedOrder, "order_id", id)
	return nil
}

func validate(customerID, status string, lines []LineInput) error {
	if customerID == "" {
		return fmt.Errorf("%w: customer_id es obligatorio", ErrInvalidInput)
	}
	if status == "" {
		return fmt.Errorf("%w: status es obligatorio", ErrInvalidInput)
	}
	if len(lines) == 0 {
		return fmt.Errorf("%w: la orden debe tener al menos una línea", ErrInvalidInput)
	}
	for i, l := range lines {
		if l.ProductID == "" {
			return fmt.Errorf("%w: línea %d sin product_id", ErrInvalidInput, i)
		}
		if l.Quantity <= 0 {
			return fmt.Errorf("%w: línea %d con cantidad inválida", ErrInvalidInput, i)
		}
	}
	return nil
}

func toLines(in []LineInput) []domain.OrderLine {
	lines := make([]domain.OrderLine, len(in))
	for i, l := range in {
		lines[i] = domain.OrderLine{
			ProductID: l.ProductID,
			Quantity:  l.Quantity,
			UnitPrice: l.UnitPrice,
		}
	}
	return lines
}
