package migrations

// Migration describe una migración SQL individual. Up se aplica al migrar;
// Down documenta cómo deshacerla (el runner actual solo aplica Up). ID es un
// identificador estable usado para detectar qué migraciones ya están aplicadas
// (debe ser único y no cambiar).
type Migration struct {
	// ID identifica la migración de forma estable (p.ej. "0001_create_outbox_table").
	ID string
	// Up es la sentencia SQL que se ejecuta al aplicar la migración.
	Up string
	// Down es la sentencia SQL que desharía la migración.
	Down string
}
