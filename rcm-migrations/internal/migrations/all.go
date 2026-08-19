package migrations

// All devuelve todas las migraciones del sistema en el orden en que deben
// aplicarse: primero outbox y después órdenes.
func All() []Migration {
	all := make([]Migration, 0, len(OutboxMigrations)+len(OrdersMigrations))
	all = append(all, OutboxMigrations...)
	all = append(all, OrdersMigrations...)
	return all
}
