package domain

// OutboxEvent es un registro reclamado de la tabla outbox. Attempts indica el
// número de intentos de publicación acumulados (incrementado al reclamar).
type OutboxEvent struct {
	ID          string
	AggregateID string
	EventType   string
	Payload     []byte
	Attempts    int
}
