# SQL migration conventions (rcm-migrations)

This document records how the SQL migrations over the database created by Pulumi are defined and applied, and why.

> Spanish version: [docs/migrations-conventions.md](../migrations-conventions.md)

## Execution: `rcm-migrations` Lambda

Migrations are applied with the **`rcm-migrations` Lambda**, which carries the SQL embedded in the binary. It replaces the old `sql_migrate` resource from the `paultyng/sql` provider, which was removed from Pulumi (along with its vendored SDK in `infra/sdks/sql`).

- **Deployment**: the `infra/internal/domains/migrations` component creates the function with the generic `platform.Function` component (fixed physical name `rcm-migrations`, env `DB_SECRET_ARN`, Secrets Manager policy, 300s timeout).
- **Execution**: manual, invoking the function after deploy: `make migrate` (`aws lambda invoke --function-name rcm-migrations --payload '{}'`). The lambda only supports "up": it ignores the payload and returns a JSON summary `{ "applied": [...], "skipped": n }`.
- **Harness**: identical to the rest of the Lambdas (`cmd/lambda` → `internal/bootstrap` → `platformconfig.LoadDB()` → `secrets.Fetch` → `database.NewPool`).

## Tracking in the database

The runner keeps its state in a `schema_migrations(id TEXT PRIMARY KEY, applied_at TIMESTAMPTZ)` table, created if it does not exist. Unlike `sql_migrate` (which kept state in Pulumi's state), here the state lives in the database itself.

- Each migration is recorded only after its `Up` runs successfully; already-recorded ones are skipped.
- An execution takes a **session-level advisory lock** (`pg_advisory_lock`) to prevent two concurrent executions. The lock is explicitly released before returning the connection to the pool.
- If an existing database does not yet have the table (e.g. migrated from `sql_migrate`), the first execution re-runs all `Up`s: that is why they must be idempotent.

## Chosen model: inline strings + exported list per domain

Each domain defines its migrations as an exported slice of `Migration` in its own file inside `rcm-migrations/internal/migrations/`. The SQL lives **in the package**, never in `main.go`.

### Rule

- The `Migration{ID, Up, Down}` type lives in `internal/migrations/migration.go`.
- The concrete SQL lives in one file per domain (e.g. `outbox.go`, `orders.go`), as an exported variable of type `[]Migration`.
- `All()` concatenates the lists in application order; it is the only thing the runner consumes.

### Example: `outbox.go`

```go
package migrations

var OutboxMigrations = []Migration{
	{
		ID:   "0001_create_outbox_table",
		Up:   `CREATE TABLE IF NOT EXISTS outbox (...)`,
		Down: "DROP TABLE IF EXISTS outbox;",
	},
}
```

## Migration rules

- `ID` is stable and unique: do **not** change the `ID` of an already-applied migration (it would be treated as new and re-executed).
- Order matters: they are applied in ascending order. Add new migrations **only at the end** of their domain's list.
- `Down` is documentation-only: the current runner does not execute it (up only). It is kept to preserve the history of how to undo each change.

## Retryability

Migrations must be **idempotent**: if executed more than once (e.g. due to a retry after a partial failure), they must not fail or produce duplicated effects.

- The runner marks a migration as applied only after its `Up` succeeds. If `Up` fails halfway (dropped connection, timeout, etc.), the next invocation re-executes the same `Up`. That is why `Up` must tolerate part of its effect already existing.
- Use idempotent variants: `CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`, `ADD COLUMN IF NOT EXISTS`, `DROP ... IF EXISTS`, etc.
- Example: `CREATE TABLE outbox (...)` would **not** be retryable; that is why the repo's migrations use the `CREATE TABLE IF NOT EXISTS outbox (...)` variant (e.g. `0001_create_outbox_table`).

## Script execution

Each `Up` is sent as a single PostgreSQL **simple protocol** query via `pgconn.Exec` (pgx), just like the `paultyng/sql` runner did. Consequences:

- A script's multiple statements run within a single implicit transaction (atomic).
- A single statement runs in autocommit, which allows `CONCURRENTLY`.

## Index creation

To create indexes on potentially large tables we **always** use `CREATE INDEX CONCURRENTLY IF NOT EXISTS`, which builds the index without blocking writes (unlike plain `CREATE INDEX`, which takes a `SHARE` lock).

Rules and limitations:

- **The index must be the only statement of its migration.** `CONCURRENTLY` cannot run inside a *transaction block*, and PostgreSQL processes a simple query's multiple statements within a single implicit transaction. The way around this is isolating the `CREATE INDEX CONCURRENTLY` in a dedicated migration with a single statement.
- **Use `IF NOT EXISTS`** so the `Up` is retryable (`CREATE INDEX CONCURRENTLY IF NOT EXISTS ...`).
- **Idempotent `Down`**: `DROP INDEX CONCURRENTLY IF EXISTS ...`.
- **If `CONCURRENTLY` fails** (e.g. a unique constraint violation) it leaves an `INVALID` index in the database. Remedy: `DROP INDEX CONCURRENTLY IF EXISTS ...` and retry the migration.

Example:

```go
{
	ID: "0003_add_outbox_claim_index",
	Up:   `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_outbox_claim ON outbox (created_at) WHERE status = 'pending';`,
	Down: `DROP INDEX CONCURRENTLY IF EXISTS idx_outbox_claim;`,
},
```

## Why not other alternatives

- **`.sql` files with `go:embed` + `golang-migrate`**: more friction (up/down pairs per migration) with no benefit when the inline-strings model already works and avoids an external dependency.
- **Automatic invocation from Pulumi** (`pulumi-command` provider): couples infrastructure apply with DDL execution; manual invocation with `make migrate` is explicit and controllable.
