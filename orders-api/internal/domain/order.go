package domain

import "time"

// Eventos de dominio que se escriben en la tabla outbox.
const (
	EventCreatedOrder = "CreatedOrder"
	EventUpdatedOrder = "UpdatedOrder"
	EventDeletedOrder = "DeletedOrder"
)

// Order representa una orden de compra.
type Order struct {
	ID         string      `json:"id"`
	CustomerID string      `json:"customerId"`
	Status     string      `json:"status"`
	Lines      []OrderLine `json:"lines"`
	CreatedAt  time.Time   `json:"createdAt"`
	UpdatedAt  time.Time   `json:"updatedAt"`
	DeletedAt  *time.Time  `json:"deletedAt,omitempty"`
}

// OrderLine representa una línea de una orden de compra.
type OrderLine struct {
	ID        string  `json:"id"`
	ProductID string  `json:"productId"`
	Quantity  int     `json:"quantity"`
	UnitPrice float64 `json:"unitPrice"`
}
