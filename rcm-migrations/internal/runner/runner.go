package runner

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rcastellor/rcm-outbox/rcm-migrations/internal/migrations"
)

const (
	// lockKey es la clave del advisory lock de sesión que evita dos ejecuciones
	// concurrentes del runner sobre la misma base de datos. El valor es
	// arbitrario pero fijo.
	lockKey int64 = 0x72636D6D // 'rcmm'

	// trackingDDL crea la tabla de tracking de migraciones aplicadas. A
	// diferencia del antiguo sql_migrate de Pulumi (que guardaba el estado en
	// el state), aquí el estado vive en la propia base de datos.
	trackingDDL = `CREATE TABLE IF NOT EXISTS schema_migrations (
		id         TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	);`
)

// Result resume una ejecución del runner; se devuelve como payload de la
// invocación de la Lambda.
type Result struct {
	// Applied son los IDs de las migraciones aplicadas en esta ejecución.
	Applied []string `json:"applied"`
	// Skipped es el número de migraciones ya aplicadas que se omitieron.
	Skipped int `json:"skipped"`
}

// Runner aplica migraciones SQL pendientes sobre la base de datos.
type Runner struct {
	pool  *pgxpool.Pool
	migs  []migrations.Migration
	log   *slog.Logger
}

// New crea el runner con el pool, la lista ordenada de migraciones y el logger.
func New(pool *pgxpool.Pool, migs []migrations.Migration, log *slog.Logger) *Runner {
	return &Runner{pool: pool, migs: migs, log: log}
}

// Run aplica las migraciones pendientes en orden y registra cada una en
// schema_migrations tras ejecutar su Up con éxito. Si un Up falla a mitad, la
// siguiente ejecución reintenta la misma migración: por eso todo Up debe ser
// idempotente.
func (r *Runner) Run(ctx context.Context) (*Result, error) {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("adquiriendo conexión: %w", err)
	}
	defer conn.Release()

	// Advisory lock de sesión: bloquea ejecuciones concurrentes. Se libera
	// explícitamente porque Release devuelve la conexión al pool sin cerrar la
	// sesión y el lock persistiría en ella.
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return nil, fmt.Errorf("adquiriendo advisory lock: %w", err)
	}
	defer func() {
		if _, err := conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", lockKey); err != nil {
			r.log.Error("liberando advisory lock", "err", err)
		}
	}()

	if _, err := conn.Exec(ctx, trackingDDL); err != nil {
		return nil, fmt.Errorf("creando tabla schema_migrations: %w", err)
	}

	applied, err := r.loadApplied(ctx, conn)
	if err != nil {
		return nil, err
	}

	res := &Result{Applied: []string{}}
	for _, m := range r.migs {
		if _, done := applied[m.ID]; done {
			res.Skipped++
			continue
		}

		r.log.Info("aplicando migración", "id", m.ID)
		if err := execSimple(ctx, conn, m.Up); err != nil {
			return nil, fmt.Errorf("aplicando migración %s: %w", m.ID, err)
		}
		if _, err := conn.Exec(ctx, "INSERT INTO schema_migrations (id) VALUES ($1)", m.ID); err != nil {
			return nil, fmt.Errorf("registrando migración %s: %w", m.ID, err)
		}
		res.Applied = append(res.Applied, m.ID)
	}

	r.log.Info("migraciones finalizadas", "aplicadas", len(res.Applied), "omitidas", res.Skipped)
	return res, nil
}

// loadApplied lee los IDs registrados en schema_migrations y avisa de los que
// no existen en la lista embebida (p.ej. migraciones retiradas del binario).
func (r *Runner) loadApplied(ctx context.Context, conn *pgxpool.Conn) (map[string]struct{}, error) {
	rows, err := conn.Query(ctx, "SELECT id FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("leyendo migraciones aplicadas: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("escaneando migración aplicada: %w", err)
		}
		applied[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterando migraciones aplicadas: %w", err)
	}

	known := make(map[string]struct{}, len(r.migs))
	for _, m := range r.migs {
		known[m.ID] = struct{}{}
	}
	for id := range applied {
		if _, ok := known[id]; !ok {
			r.log.Warn("migración registrada no presente en el binario", "id", id)
		}
	}
	return applied, nil
}

// execSimple ejecuta un script SQL (posiblemente multi-sentencia) mediante el
// protocolo simple de PostgreSQL, igual que hacía el runner paultyng/sql.
// Las múltiples sentencias de una query simple corren dentro de una única
// transacción implícita; una sentencia única (p.ej. CREATE INDEX CONCURRENTLY)
// corre en autocommit.
func execSimple(ctx context.Context, conn *pgxpool.Conn, script string) error {
	_, err := conn.Conn().PgConn().Exec(ctx, script).ReadAll()
	return err
}
