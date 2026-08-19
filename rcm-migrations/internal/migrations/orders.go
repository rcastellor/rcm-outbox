package migrations

// OrdersMigrations definen el esquema de órdenes (orders y orders_lines). La
// lista está ordenada: se aplican en orden ascendente; Down documenta cómo
// deshacerlas. El borrado de órdenes es lógico (columna deleted_at).
var OrdersMigrations = []Migration{
	{
		ID: "0001_create_orders_table",
		Up: `CREATE TABLE IF NOT EXISTS orders (
			id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			customer_id VARCHAR(255) NOT NULL,
			status      VARCHAR(50)  NOT NULL,
			created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
			updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
			deleted_at  TIMESTAMPTZ
		);`,
		Down: "DROP TABLE IF EXISTS orders;",
	},
	{
		ID: "0002_create_orders_lines_table",
		Up: `CREATE TABLE IF NOT EXISTS orders_lines (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			order_id   UUID NOT NULL REFERENCES orders(id),
			product_id VARCHAR(255) NOT NULL,
			quantity   INTEGER NOT NULL CHECK (quantity > 0),
			unit_price DOUBLE PRECISION NOT NULL
		);`,
		Down: "DROP TABLE IF EXISTS orders_lines;",
	},
	{
		// El índice se crea con CONCURRENTLY para no bloquear writes en la tabla.
		// Debe ser la única sentencia de la migración: CONCURRENTLY no puede
		// correr dentro de un transaction block (aunque sí en el protocolo
		// simple de una sola sentencia que usa el runner).
		ID:   "0003_add_orders_lines_order_id_index",
		Up:   `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_lines_order_id ON orders_lines (order_id);`,
		Down: `DROP INDEX CONCURRENTLY IF EXISTS idx_orders_lines_order_id;`,
	},
}
