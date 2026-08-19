# Convenciones de migraciones SQL (rcm-migrations)

Este documento registra cómo se definen y aplican las migraciones SQL sobre la base de datos creada por Pulumi, y por qué.

## Ejecución: Lambda `rcm-migrations`

Las migraciones se aplican con la **Lambda `rcm-migrations`**, que lleva el SQL embebido en el binario. Sustituye al antiguo recurso `sql_migrate` del proveedor `paultyng/sql`, que se eliminó de Pulumi (junto con su SDK vendado en `infra/sdks/sql`).

- **Despliegue**: el componente `infra/internal/domains/migrations` crea la función con el componente genérico `platform.Function` (nombre físico fijo `rcm-migrations`, env `DB_SECRET_ARN`, política Secrets Manager, timeout 300s).
- **Ejecución**: manual, invocando la función tras el deploy: `make migrate` (`aws lambda invoke --function-name rcm-migrations --payload '{}'`). La lambda solo soporta "up": ignora el payload y devuelve un resumen JSON `{ "applied": [...], "skipped": n }`.
- **Harness**: idéntico al resto de Lambdas (`cmd/lambda` → `internal/bootstrap` → `platformconfig.LoadDB()` → `secrets.Fetch` → `database.NewPool`).

## Tracking en la base de datos

El runner guarda su estado en una tabla `schema_migrations(id TEXT PRIMARY KEY, applied_at TIMESTAMPTZ)` creada si no existe. A diferencia de `sql_migrate` (que guardaba el estado en el state de Pulumi), aquí el estado vive en la propia base de datos.

- Cada migración se registra solo tras ejecutar su `Up` con éxito; las ya registradas se omiten.
- Una ejecución toma un **advisory lock de sesión** (`pg_advisory_lock`) para impedir dos ejecuciones concurrentes. El lock se libera explícitamente antes de devolver la conexión al pool.
- Si una base de datos existente aún no tiene la tabla (p.ej. migrada desde `sql_migrate`), la primera ejecución re-ejecuta todos los `Up`: por eso deben ser idempotentes.

## Modelo elegido: strings inline + lista exportada por dominio

Cada dominio define sus migraciones como un slice exportado de `Migration` en su propio archivo dentro de `rcm-migrations/internal/migrations/`. El SQL vive **en el paquete**, nunca en `main.go`.

### Regla

- El tipo `Migration{ID, Up, Down}` vive en `internal/migrations/migration.go`.
- El SQL concreto vive en un archivo por dominio (p.ej. `outbox.go`, `orders.go`), como una variable exportada del tipo `[]Migration`.
- `All()` concatena las listas en el orden de aplicación; es lo único que consume el runner.

### Ejemplo: `outbox.go`

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

## Reglas de las migraciones

- `ID` es estable y único: **no** cambiar el `ID` de una migración ya aplicada (se trataría como nueva y se reejecutaría).
- El orden importa: se aplican en orden ascendente. Añadir migraciones nuevas **solo al final** de la lista de su dominio.
- `Down` es documental: el runner actual no lo ejecuta (solo soporta up). Se mantiene para conservar el historial de cómo deshacer cada cambio.

## Reintentabilidad

Las migraciones deben ser **idempotentes**: si se ejecutan más de una vez (p.ej. por un reintento tras un fallo parcial), no deben fallar ni producir efectos duplicados.

- El runner marca una migración como aplicada solo tras ejecutar `Up` con éxito. Si `Up` falla a mitad (conexión caída, timeout, etc.), en la siguiente invocación se vuelve a ejecutar la misma `Up`. Por eso `Up` debe tolerar que parte de su efecto ya exista.
- Usar variantes idempotentes: `CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`, `ADD COLUMN IF NOT EXISTS`, `DROP ... IF EXISTS`, etc.
- Ejemplo: `CREATE TABLE outbox (...)` **no** sería reintentable; por eso las migraciones del repo usan la variante `CREATE TABLE IF NOT EXISTS outbox (...)` (p. ej. `0001_create_outbox_table`).

## Ejecución de scripts

Cada `Up` se envía como una única query de **protocolo simple** de PostgreSQL vía `pgconn.Exec` (pgx), igual que hacía el runner `paultyng/sql`. Consecuencias:

- Las múltiples sentencias de un script corren dentro de una única transacción implícita (atómico).
- Una sentencia única corre en autocommit, lo que permite `CONCURRENTLY`.

## Creación de índices

Para crear índices sobre tablas potencialmente grandes se usa **siempre** `CREATE INDEX CONCURRENTLY IF NOT EXISTS`, que construye el índice sin bloquear escrituras (a diferencia del `CREATE INDEX` normal, que toma un lock `SHARE`).

Reglas y limitaciones:

- **El índice debe ser la única sentencia de su migración.** `CONCURRENTLY` no puede correr dentro de un *transaction block*, y PostgreSQL procesa las múltiples sentencias de una query simple en una única transacción implícita. La forma de evitarlo es aislar el `CREATE INDEX CONCURRENTLY` en una migración dedicada con una sola sentencia.
- **Usar `IF NOT EXISTS`** para que el `Up` sea reintentable (`CREATE INDEX CONCURRENTLY IF NOT EXISTS ...`).
- **`Down` idempotente**: `DROP INDEX CONCURRENTLY IF EXISTS ...`.
- **Si `CONCURRENTLY` falla** (p.ej. violación de un constraint único) deja un índice `INVALID` en la base. Remedio: `DROP INDEX CONCURRENTLY IF EXISTS ...` y reintentar la migración.

Ejemplo:

```go
{
	ID: "0003_add_outbox_claim_index",
	Up:   `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_outbox_claim ON outbox (created_at) WHERE status = 'pending';`,
	Down: `DROP INDEX CONCURRENTLY IF EXISTS idx_outbox_claim;`,
},
```

## Por qué no otras alternativas

- **Archivos `.sql` con `go:embed` + `golang-migrate`**: más fricción (pares up/down por migración) sin beneficio cuando el modelo de strings inline ya funciona y evita una dependencia externa.
- **Invocación automática desde Pulumi** (provider `pulumi-command`): acopla el apply de infraestructura con la ejecución de DDL; la invocación manual con `make migrate` es explícita y controlable.
