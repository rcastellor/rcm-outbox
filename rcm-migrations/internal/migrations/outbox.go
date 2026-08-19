package migrations

// OutboxMigrations definen el esquema de la tabla outbox. La lista está
// ordenada: se aplican en orden ascendente; Down documenta cómo deshacerlas.
var OutboxMigrations = []Migration{
	{
		ID: "0001_create_outbox_table",
		Up: `CREATE TABLE IF NOT EXISTS outbox (
			id           UUID PRIMARY KEY,
			aggregate_id VARCHAR(255) NOT NULL,
			event_type   VARCHAR(255) NOT NULL,
			payload      JSONB        NOT NULL,
			created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
			published_at TIMESTAMPTZ
		);`,
		Down: "DROP TABLE IF EXISTS outbox;",
	},
	{
		// Añade las columnas de reintento sin reescribir la tabla: available_at
		// se añade nullable (sin default, O(1)) y el default now() se aplica
		// solo a inserciones futuras. Las filas existentes quedan con
		// available_at NULL, que el worker interpreta como "disponible ya".
		ID: "0002_add_outbox_retry_columns",
		Up: `SET lock_timeout = '5s';

		ALTER TABLE outbox
			ADD COLUMN status       VARCHAR(20) NOT NULL DEFAULT 'pending',
			ADD COLUMN attempts     INT         NOT NULL DEFAULT 0,
			ADD COLUMN claimed_at   TIMESTAMPTZ,
			ADD COLUMN available_at TIMESTAMPTZ,
			ADD COLUMN last_error   TEXT;

		ALTER TABLE outbox ALTER COLUMN available_at SET DEFAULT now();`,
		Down: `ALTER TABLE outbox
			DROP COLUMN status,
			DROP COLUMN attempts,
			DROP COLUMN claimed_at,
			DROP COLUMN available_at,
			DROP COLUMN last_error;`,
	},
	{
		// El índice de claim se crea con CONCURRENTLY para no bloquear writes.
		// Debe ser la única sentencia de la migración: CONCURRENTLY no puede
		// correr dentro de un transaction block (aunque sí en el protocolo
		// simple de una sola sentencia que usa el runner).
		ID:   "0003_add_outbox_claim_index",
		Up:   `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_outbox_claim ON outbox (created_at) WHERE status = 'pending';`,
		Down: `DROP INDEX CONCURRENTLY IF EXISTS idx_outbox_claim;`,
	},
}
