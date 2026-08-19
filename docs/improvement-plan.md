# Plan de mejoras (`improvement-plan`)

Este documento recoge el análisis del código fuente buscando redundancias, simplificaciones y mejoras alineadas con los principios **SOLID**, **KISS** y **YAGNI**. Cada punto indica ficheros concretos y, cuando aplica, un snippet orientativo. La matriz final prioriza los cambios por esfuerzo/impacto.

## Evaluación general

La base de código está bien estructurada: el *layering* es consistente (`cmd → bootstrap → handler → lógica de negocio → repository/publisher`), el patrón outbox está correctamente implementado (*claims* con `FOR UPDATE SKIP LOCKED`, escrituras transaccionales, advisory lock en las migraciones) y la query de `List` ya evita el problema N+1. Los hallazgos son sobre todo oportunidades de consolidación y algunos defectos carencias puntuales.

---

## 1. Código redundante (eliminar / consolidar)

### R1. `positiveInt` duplicada — valor alto

Función idéntica en dos módulos; `positiveSeconds` es su variante:

- `orders-workers/internal/config/config.go:79-88`
- `orders-dispatcher/internal/config/config.go:62-71`

**Recomendación:** moverla a `rcm-platform/config` una sola vez:

```go
// rcm-platform/config/env.go
// PositiveInt lee una variable de entorno entera positiva, devolviendo el
// default si no está definida.
func PositiveInt(name string, def int) (int, error) { /* cuerpo actual */ }

// PositiveSeconds ídem, devolviendo time.Duration.
func PositiveSeconds(name string, defSeconds int) (time.Duration, error) { /* ... */ }
```

### R2. Secuencia de arranque de BD repetida 4 veces — valor alto

`config.Load → secrets.Fetch → database.NewPool` aparece igual en los bootstrap de `orders-api`, `orders-workers`, `orders-dispatcher` y `rcm-migrations`.

**Recomendación:** encapsular en la biblioteca compartida:

```go
// rcm-platform/database/database.go
// ConnectFromSecret recupera las credenciales del secreto indicado y abre el pool.
func ConnectFromSecret(ctx context.Context, secretARN string) (*pgxpool.Pool, error) {
	creds, err := secrets.Fetch(ctx, secretARN)
	if err != nil {
		return nil, err
	}
	return NewPool(ctx, creds.DSN())
}
```

Cada bootstrap se reduce a una línea; `bootstrap` conserva solo el ensamblado genuino de dependencias (SRP).

### R3. Contrato SNS productor/consumidor duplicado entre módulos — riesgo de deriva

- El struct `envelope{EventType, Payload}` está definido por partida doble: `orders-workers/internal/publisher/publisher.go:27-30` y `orders-stats-consumer/internal/stats/stats.go:38-41`.
- El nombre de evento `"CreatedOrder"` está hardcodeado dos veces: `orders-api/internal/domain/order.go:7` y `orders-stats-consumer/internal/stats/stats.go:21`.

Si el productor renombra un campo, el consumidor rompe silenciosamente en runtime. **Recomendación:** crear `rcm-platform/events` con el tipo `Envelope` compartido y las constantes de eventos; ambos módulos lo importan. Es un contrato, no lógica de dominio: pertenece a la biblioteca compartida.

### R4. Defaults operativos duplicados entre infra y Lambdas

`infra/internal/config/defaults.go` (BatchSize=10, MaxWorkers=20, MaxAttempts=5, Backoff=60/480) replica `orders-workers/internal/config/config.go:12-15` y `orders-dispatcher/internal/config/config.go:11-12`. Cambiar un valor solo en infra hace que el fallback de la Lambda diverja silenciosamente. **Recomendación:** centralizar estas constantes en `rcm-platform/config` y referenciarlas desde ambos lados (infra puede importar el módulo del workspace).

### R5. `CreateOrderInput` ≡ `UpdateOrderInput`

`orders-api/internal/usecase/orders.go:24-35` — structs idénticos en campos y tags JSON. Sustituir ambos por un único `OrderInput`; solo cambian dos call sites en `router.go`.

### R6. Construcción de políticas IAM duplicada en `function.go`

`infra/internal/platform/function.go:85-102` (bloque del secreto de BD) y `:104-121` (bucle de `ExtraPolicies`) construyen el mismo par `GetPolicyDocumentOutput` + `NewRolePolicy`:

```go
func newRolePolicy(ctx *pulumi.Context, role *iam.Role, name string,
	actions []string, resources []pulumi.StringInput) error {
	policy := iam.GetPolicyDocumentOutput(ctx, iam.GetPolicyDocumentOutputArgs{
		Statements: iam.GetPolicyDocumentStatementArray{
			iam.GetPolicyDocumentStatementArgs{
				Effect:    pulumi.StringPtr("Allow"),
				Actions:   pulumi.ToStringArray(actions),
				Resources: pulumi.StringArray(resources),
			},
		},
	})
	_, err := iam.NewRolePolicy(ctx, name, &iam.RolePolicyArgs{Role: role.Name, Policy: policy.Json()}, pulumi.Parent(role))
	return err
}
```

El bloque de `DBSecretARN` pasa a ser una llamada; el bucle se reduce a una línea.

### R7. Dos componentes SQS solapados

`outbox.Queue` (cola estándar, `queue.go`) y `platform.SQS` (FIFO + suscripción, `sqs.go`) envuelven ambos `sqs.NewQueue`, con nomenclatura inconsistente (`NewQueue` vs `NewSQS`). **Recomendación:** consolidar en un único `platform.Queue` con `TopicARN` opcional; eliminar el shim `outbox.Queue`. Esfuerzo medio, urgencia baja.

### R8. Doble codificación de estado en el esquema `outbox`

`ClaimPending` filtra `published_at IS NULL AND status = 'pending'` (`orders-workers/repository/outbox.go:43-44`). Desde la migración `0002` añadió `status`, `published_at` es redundante como marcador de estado (sigue siendo útil como timestamp de auditoría). Por compatibilidad con datos existentes conviene conservar la columna, pero el predicado debería apoyarse solo en `status`, con un comentario que aclare que `published_at` es solo auditoría.

### R9. Cinco `main.go` casi idénticos — veredicto: mantener

Solo difieren en el nombre del módulo. Un helper compartido `Run(name, loader)` añadiría indirección por ~20 líneas triviales por binario. Es Go idiomático; **no consolidar** (KISS).

---

## 2. Cumplimiento de SOLID

| Principio | Estado | Notas |
|---|---|---|
| **SRP** | ✅ Bien | Los handlers son adaptadores finos; la lógica está aislada (`worker`, `dispatcher`, `stats`, `usecase`); `bootstrap` solo compone. |
| **OCP** | ✅ Aceptable | El switch de `Router.Route` sobre el método es suficiente a esta escala. Una tabla de rutas violaría KISS/YAGNI — no añadirla. |
| **LSP** | ✅ Sin violaciones | No hay jerarquías de herencia; los fakes de los tests respetan los contratos de sus interfaces. |
| **ISP** | ⚠️ Inconsistente | Ejemplar en `worker`/`dispatcher` (interfaces mínimas definidas por el consumidor, p.ej. `worker.go:18-24`). Ausente en `stats.Aggregator` y `usecase.Orders`. |
| **DIP** | ⚠️ Inconsistente | `worker`/`dispatcher` dependen de abstracciones; **`usecase.Orders` depende del repositorio concreto `*repository.Orders`** (`usecase/orders.go:39`). |

**Recomendación (ISP+DIP, habilita los primeros tests unitarios de orders-api):**

```go
// orders-api/internal/usecase/orders.go
type Repository interface {
	Create(ctx context.Context, o *domain.Order) (*domain.Order, error)
	Get(ctx context.Context, id string) (*domain.Order, error)
	List(ctx context.Context) ([]domain.Order, error)
	Update(ctx context.Context, o *domain.Order) (*domain.Order, error)
	Delete(ctx context.Context, id string) error
}
```

Mismo patrón para `stats.Aggregator` — extraer la escritura DynamoDB tras una interfaz mínima:

```go
type counterStore interface {
	IncrementItem(ctx context.Context, pk, sk string, quantity int) error
}
```

Dato relevante: los paquetes *con* interfaces son exactamente los que *tienen* tests; los que no (`orders-api`, `orders-stats-consumer`) no tienen ninguno. Esa correlación es el mejor argumento para el cambio.

**Fuga de encapsulación:** `Dispatcher` expone `Function *platform.Function` (`dispatcher.go:37`) y `main.go:106` atraviesa el componente: `dispatcherFn.Function.Function.Name`. `Migrations` y `Worker` ya exponen outputs `FunctionName`/`FunctionARN`. Alinear `Dispatcher` con ellos.

---

## 3. Hallazgos KISS

El código es genuinamente simple — sin frameworks de DI ni abstracciones prematuras. Observaciones concretas:

- **Mantener:** `config.LoadDB()` devolviendo un struct alrededor de un string roza la ceremonia, pero nombra el concepto y sigue las convenciones — correcto.
- **Simplificar:** `SNS.TopicARN` (`sns.go:71-78`) usa `MapIndex(...).ApplyT(...).(pulumi.StringOutput)` con aserción defensiva para lo que es una consulta de mapa. Funciona, pero es el fragmento más enrevesado del repo; un `ApplyT` directo sobre el mapa sería más legible.
- **Menor:** `Router.mapError` (`router.go:86`) nunca usa su receptor — convertirlo en función plana.
- **Menor:** los `RegisterResourceOutputs(c, pulumi.Map{})` vacíos en `SecretsManager` y `Schedule` son boilerplate que Pulumi no exige — eliminables.
- **Antirrecomendaciones:** no introducir chi/gorilla para routing, wire/fx para DI, clases-base genéricas de repository, ni reflexión para config. El cableado manual actual es el peso correcto para 5 Lambdas pequeñas.

---

## 4. Hallazgos YAGNI

1. **`Migration.Down` es código muerto** (`migration.go:13`). El runner solo ejecuta `Up`. Sirve como documentación de rollback, lo cual tiene valor real — pero hay que decidirlo explícitamente: o se mantiene con un comentario que indique que es solo documentación (estado de facto actual), o se elimina hasta que exista un comando `down`. No dejarlo ambiguo.
2. **La lógica de `FinalSnapshotIdentifier` es efectivamente inalcanzable** — `applyDatabaseDefaults` (`database.go:190-192`) fuerza `SkipFinalSnapshot=true` cuando `FinalSnapshotIdentifier` está vacío, y nada en `main.go` lo establece. Dos flags interactuando donde no se usa ninguno. Simplificar a solo `SkipFinalSnapshot`.
3. **El endpoint List carece de paginación** (`repository/orders.go:82`) — correcto no construirla ahora, pero señalarlo: romperá con unos miles de órdenes. Añadir `LIMIT/OFFSET` o keyset pagination cuando un consumidor real lo necesite.
4. **Nivel de log fijo a Info** (`logger/logger.go:11`) — añadir soporte de `LOG_LEVEL` solo cuando depurar en prod lo exija.
5. **`platform.SQS` hardcodea FIFO pero tiene nombre genérico** — o renombrarlo para reflejar la realidad (`FIFOQueue`) o parametrizarlo cuando aparezca un segundo caso de uso. Renombrar ahora es la jugada alineada con YAGNI.

---

## 5. Mejoras (rendimiento / mantenibilidad)

### E1. Índice en `orders_lines(order_id)` — ✅ implementado

PostgreSQL no indexa automáticamente las columnas FK y cada `Get`, `List`, `Create` y `Update` consulta `orders_lines WHERE order_id = $1` (`repository/orders.go`), lo que era un sequential scan. La migración embebida `0003_add_orders_lines_order_id_index` (`rcm-migrations/internal/migrations/orders.go`) ya crea el índice con el patrón previsto:

```go
{
	ID:   "0003_add_orders_lines_order_id_index",
	Up:   `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_lines_order_id ON orders_lines (order_id);`,
	Down: `DROP INDEX CONCURRENTLY IF EXISTS idx_orders_lines_order_id;`,
},
```

(Mismo patrón que `0003_add_outbox_claim_index`, válido bajo el runner de protocolo simple.)

### E2. Encadenar los inserts de líneas con `pgx.Batch`

`insertLines` (`repository/orders.go:221-233`) hace un round trip por línea. `pgx.Batch` envía todos los inserts en uno conservando `RETURNING id`:

```go
batch := &pgx.Batch{}
for i := range o.Lines {
	batch.Queue(`INSERT INTO orders_lines (...) VALUES ($1,$2,$3,$4) RETURNING id`,
		o.ID, o.Lines[i].ProductID, o.Lines[i].Quantity, o.Lines[i].UnitPrice)
}
br := tx.SendBatch(ctx, batch)
defer br.Close()
for i := range o.Lines {
	if err := br.QueryRow().Scan(&o.Lines[i].ID); err != nil {
		return fmt.Errorf("insertando línea de orden: %w", err)
	}
}
```

(Evitar `CopyFrom` — no devuelve IDs generados, que la respuesta de la API incluye.)

### E3. `sslmode=disable` en el DSN

`rcm-platform/secrets/secrets.go:50` hardcodea `sslmode=disable`. Válido contra floci, incorrecto contra RDS real. Hacerlo configurable por entorno (`DB_SSLMODE`, default `require` fuera de local).

### E4. Semántica de fallo del batch en el worker

En `Process` (`worker.go:74-87`), un error de BD en `MarkPublished`/`handleFailure` aborta todo el batch a mitad de bucle; los registros reclamados restantes esperan hasta que expire el lease de 2 minutos y se reprocesan (los duplicados son inherentes a at-least-once, pero la espera no). Opciones: continuar el bucle y devolver un error agregado, o como mínimo documentar el comportamiento. Relacionado: `ResetStuck` corre en *cada* invocación — un UPDATE extra por invocación; moverlo a la ruta del dispatcher (una vez por minuto en vez de una por invocación de worker) es una mejora barata.

### E5. Diseños intencionales — no "optimizar"

- **La publicación secuencial del worker es correcta**: `MessageGroupId = AggregateID` implica que publicar concurrentemente podría reordenar eventos dentro de un aggregate. Dejarla serial.
- **El `UpdateItem` por línea en stats es correcto**: `ADD` es atómico y `BatchWriteItem` no soporta incrementos. Paralelizar con `errgroup` es posible pero no está justificado aún (YAGNI).

---

## Matriz de prioridades

| # | Ítem | Principio | Esfuerzo | Impacto |
|---|---|---|---|---|
| 1 | ~~E1: índice `orders_lines(order_id)`~~ ✅ implementado | Rendimiento | Bajo | **Alto** |
| 2 | R1: helpers de env compartidos en `rcm-platform` | DRY/KISS | Bajo | Medio |
| 3 | R2: `database.ConnectFromSecret` | DRY/SRP | Bajo | Medio |
| 4 | R3: paquete compartido `events` | DRY | Bajo | Medio |
| 5 | §2: interfaces para `usecase.Orders` + store de `stats` | DIP/ISP | Bajo | Medio |
| 6 | E3: `sslmode` configurable | Seguridad | Bajo | Medio |
| 7 | R4: fuente única para defaults numéricos | DRY | Bajo | Medio |
| 8 | R6: helper `newRolePolicy`; R5: `OrderInput`; §2: `Dispatcher.FunctionName` | DRY/Encap. | Bajo | Bajo |
| 9 | R7: consolidar componentes de cola; R8: limpieza de predicado; E4: semántica de batch | KISS | Medio | Bajo-Medio |

Todos los cambios son compatibles con las convenciones existentes (comentarios en español, independencia por módulo, migraciones embebidas) y verificables con `make lint && make test`, más `make migrate` para E1.
