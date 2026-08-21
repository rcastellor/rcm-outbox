# Improvement plan (`improvement-plan`)

This document collects the analysis of the source code looking for redundancies, simplifications and improvements aligned with the **SOLID**, **KISS** and **YAGNI** principles. Each item points to concrete files and, where applicable, an orientative snippet. The final matrix prioritizes changes by effort/impact.

> Spanish version: [docs/improvement-plan.md](../improvement-plan.md)

## Overall assessment

The codebase is well structured: layering is consistent (`cmd → bootstrap → handler → business logic → repository/publisher`), the outbox pattern is correctly implemented (*claims* with `FOR UPDATE SKIP LOCKED`, transactional writes, advisory lock on migrations) and the `List` query already avoids the N+1 problem. The findings are mostly consolidation opportunities and a few point defects/gaps.

---

## 1. Redundant code (remove / consolidate)

### R1. Duplicated `positiveInt` — high value

Identical function in two modules; `positiveSeconds` is its variant:

- `orders-workers/internal/config/config.go:79-88`
- `orders-dispatcher/internal/config/config.go:62-71`

**Recommendation:** move it into `rcm-platform/config` once:

```go
// rcm-platform/config/env.go
// PositiveInt reads a positive integer environment variable, returning the
// default if it is not set.
func PositiveInt(name string, def int) (int, error) { /* current body */ }

// PositiveSeconds ditto, returning time.Duration.
func PositiveSeconds(name string, defSeconds int) (time.Duration, error) { /* ... */ }
```

### R2. DB bootstrap sequence repeated 4 times — high value

`config.Load → secrets.Fetch → database.NewPool` appears identically in the bootstraps of `orders-api`, `orders-workers`, `orders-dispatcher` and `rcm-migrations`.

**Recommendation:** encapsulate it in the shared library:

```go
// rcm-platform/database/database.go
// ConnectFromSecret fetches the credentials of the given secret and opens the pool.
func ConnectFromSecret(ctx context.Context, secretARN string) (*pgxpool.Pool, error) {
	creds, err := secrets.Fetch(ctx, secretARN)
	if err != nil {
		return nil, err
	}
	return NewPool(ctx, creds.DSN())
}
```

Each bootstrap shrinks to one line; `bootstrap` keeps only genuine dependency assembly (SRP).

### R3. SNS producer/consumer contract duplicated across modules — drift risk

- The `envelope{EventType, Payload}` struct is defined twice: `orders-workers/internal/publisher/publisher.go:27-30` and `orders-stats-consumer/internal/stats/stats.go:38-41`.
- The event name `"CreatedOrder"` is hardcoded twice: `orders-api/internal/domain/order.go:7` and `orders-stats-consumer/internal/stats/stats.go:21`.

If the producer renames a field, the consumer breaks silently at runtime. **Recommendation:** create `rcm-platform/events` with the shared `Envelope` type and event constants; both modules import it. It is a contract, not domain logic: it belongs to the shared library.

### R4. Operational defaults duplicated between infra and Lambdas

`infra/internal/config/defaults.go` (BatchSize=10, MaxWorkers=20, MaxAttempts=5, Backoff=60/480) replicates `orders-workers/internal/config/config.go:12-15` and `orders-dispatcher/internal/config/config.go:11-12`. Changing a value only in infra makes the Lambda's fallback diverge silently. **Recommendation:** centralize these constants in `rcm-platform/config` and reference them from both sides (infra can import the workspace module).

### R5. `CreateOrderInput` ≡ `UpdateOrderInput`

`orders-api/internal/usecase/orders.go:24-35` — structs identical in fields and JSON tags. Replace both with a single `OrderInput`; only two call sites change in `router.go`.

### R6. IAM policy construction duplicated in `function.go`

`infra/internal/platform/function.go:85-102` (DB secret block) and `:104-121` (`ExtraPolicies` loop) build the same `GetPolicyDocumentOutput` + `NewRolePolicy` pair:

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

The `DBSecretARN` block becomes one call; the loop shrinks to one line.

### R7. Two overlapping SQS components

`outbox.Queue` (standard queue, `queue.go`) and `platform.SQS` (FIFO + subscription, `sqs.go`) both wrap `sqs.NewQueue`, with inconsistent naming (`NewQueue` vs `NewSQS`). **Recommendation:** consolidate into a single `platform.Queue` with an optional `TopicARN`; remove the `outbox.Queue` shim. Medium effort, low urgency.

### R8. Double status encoding in the `outbox` schema

`ClaimPending` filters `published_at IS NULL AND status = 'pending'` (`orders-workers/repository/outbox.go:43-44`). Since migration `0002` added `status`, `published_at` is redundant as a status marker (still useful as an audit timestamp). For compatibility with existing data the column should be kept, but the predicate should rely only on `status`, with a comment clarifying that `published_at` is audit-only.

### R9. Five nearly identical `main.go`s — verdict: keep

They differ only in the module name. A shared helper `Run(name, loader)` would add indirection for ~20 trivial lines per binary. It is idiomatic Go; **do not consolidate** (KISS).

---

## 2. SOLID compliance

| Principle | Status | Notes |
|---|---|---|
| **SRP** | ✅ Good | Handlers are thin adapters; logic is isolated (`worker`, `dispatcher`, `stats`, `usecase`); `bootstrap` only composes. |
| **OCP** | ✅ Acceptable | The `Router.Route` switch over the method is enough at this scale. A route table would violate KISS/YAGNI — don't add one. |
| **LSP** | ✅ No violations | No inheritance hierarchies; test fakes respect their interfaces' contracts. |
| **ISP** | ⚠️ Inconsistent | Exemplary in `worker`/`dispatcher` (minimal consumer-defined interfaces, e.g. `worker.go:18-24`). Missing in `stats.Aggregator` and `usecase.Orders`. |
| **DIP** | ⚠️ Inconsistent | `worker`/`dispatcher` depend on abstractions; **`usecase.Orders` depends on the concrete repository `*repository.Orders`** (`usecase/orders.go:39`). |

**Recommendation (ISP+DIP, enables the first unit tests of orders-api):**

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

Same pattern for `stats.Aggregator` — extract the DynamoDB write behind a minimal interface:

```go
type counterStore interface {
	IncrementItem(ctx context.Context, pk, sk string, quantity int) error
}
```

Relevant data point: the packages *with* interfaces are exactly the ones that *have* tests; the ones without (`orders-api`, `orders-stats-consumer`) have none. That correlation is the best argument for the change.

**Encapsulation leak:** `Dispatcher` exposes `Function *platform.Function` (`dispatcher.go:37`) and `main.go:106` reaches through the component: `dispatcherFn.Function.Function.Name`. `Migrations` and `Worker` already expose `FunctionName`/`FunctionARN` outputs. Align `Dispatcher` with them.

---

## 3. KISS findings

The code is genuinely simple — no DI frameworks or premature abstractions. Specific observations:

- **Keep:** `config.LoadDB()` returning a struct around a string borders on ceremony, but it names the concept and follows the conventions — fine.
- **Simplify:** `SNS.TopicARN` (`sns.go:71-78`) uses `MapIndex(...).ApplyT(...).(pulumi.StringOutput)` with a defensive assertion for what is a map lookup. It works, but it is the most convoluted fragment in the repo; a direct `ApplyT` on the map would be more readable.
- **Minor:** `Router.mapError` (`router.go:86`) never uses its receiver — turn it into a plain function.
- **Minor:** the empty `RegisterResourceOutputs(c, pulumi.Map{})` calls in `SecretsManager` and `Schedule` are boilerplate Pulumi does not require — removable.
- **Anti-recommendations:** do not introduce chi/gorilla for routing, wire/fx for DI, generic repository base classes, or reflection for config. The current manual wiring is the right weight for 5 small Lambdas.

---

## 4. YAGNI findings

1. **`Migration.Down` is dead code** (`migration.go:13`). The runner only runs `Up`. It serves as rollback documentation, which has real value — but decide explicitly: either keep it with a comment stating it is documentation-only (current de facto state), or remove it until a `down` command exists. Do not leave it ambiguous.
2. **The `FinalSnapshotIdentifier` logic is effectively unreachable** — `applyDatabaseDefaults` (`database.go:190-192`) forces `SkipFinalSnapshot=true` when `FinalSnapshotIdentifier` is empty, and nothing in `main.go` sets it. Two flags interacting where neither is used. Simplify to just `SkipFinalSnapshot`.
3. **The List endpoint lacks pagination** (`repository/orders.go:82`) — right not to build it now, but flag it: it will break at a few thousand orders. Add `LIMIT/OFFSET` or keyset pagination when a real consumer needs it.
4. **Log level fixed at Info** (`logger/logger.go:11`) — add `LOG_LEVEL` support only when debugging in prod demands it.
5. **`platform.SQS` hardcodes FIFO but has a generic name** — either rename it to reflect reality (`FIFOQueue`) or parameterize it when a second use case appears. Renaming now is the YAGNI-aligned play.

---

## 5. Improvements (performance / maintainability)

### E1. Index on `orders_lines(order_id)` — ✅ implemented

PostgreSQL does not automatically index FK columns, and every `Get`, `List`, `Create` and `Update` queries `orders_lines WHERE order_id = $1` (`repository/orders.go`), which was a sequential scan. The embedded migration `0003_add_orders_lines_order_id_index` (`rcm-migrations/internal/migrations/orders.go`) already creates the index with the intended pattern:

```go
{
	ID:   "0003_add_orders_lines_order_id_index",
	Up:   `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_lines_order_id ON orders_lines (order_id);`,
	Down: `DROP INDEX CONCURRENTLY IF EXISTS idx_orders_lines_order_id;`,
},
```

(Same pattern as `0003_add_outbox_claim_index`, valid under the simple-protocol runner.)

### E2. Chain line inserts with `pgx.Batch`

`insertLines` (`repository/orders.go:221-233`) does one round trip per line. `pgx.Batch` sends all inserts in one while preserving `RETURNING id`:

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
		return fmt.Errorf("inserting order line: %w", err)
	}
}
```

(Avoid `CopyFrom` — it does not return generated IDs, which the API response includes.)

### E3. `sslmode=disable` in the DSN

`rcm-platform/secrets/secrets.go:50` hardcodes `sslmode=disable`. Valid against floci, wrong against real RDS. Make it configurable per environment (`DB_SSLMODE`, default `require` outside local).

### E4. Batch failure semantics in the worker

In `Process` (`worker.go:74-87`), a DB error in `MarkPublished`/`handleFailure` aborts the whole batch mid-loop; the remaining claimed records wait until the 2-minute lease expires and get reprocessed (duplicates are inherent to at-least-once, but the wait is not). Options: continue the loop and return an aggregated error, or at minimum document the behavior. Related: `ResetStuck` runs on *every* invocation — one extra UPDATE per invocation; moving it to the dispatcher path (once per minute instead of once per worker invocation) is a cheap improvement.

### E5. Intentional designs — do not "optimize"

- **The worker's sequential publishing is correct**: `MessageGroupId = AggregateID` means publishing concurrently could reorder events within an aggregate. Keep it serial.
- **The per-line `UpdateItem` in stats is correct**: `ADD` is atomic and `BatchWriteItem` does not support increments. Parallelizing with `errgroup` is possible but not justified yet (YAGNI).

---

## Priority matrix

| # | Item | Principle | Effort | Impact |
|---|---|---|---|---|
| 1 | ~~E1: index on `orders_lines(order_id)`~~ ✅ implemented | Performance | Low | **High** |
| 2 | R1: shared env helpers in `rcm-platform` | DRY/KISS | Low | Medium |
| 3 | R2: `database.ConnectFromSecret` | DRY/SRP | Low | Medium |
| 4 | R3: shared `events` package | DRY | Low | Medium |
| 5 | §2: interfaces for `usecase.Orders` + `stats` store | DIP/ISP | Low | Medium |
| 6 | E3: configurable `sslmode` | Security | Low | Medium |
| 7 | R4: single source for numeric defaults | DRY | Low | Medium |
| 8 | R6: `newRolePolicy` helper; R5: `OrderInput`; §2: `Dispatcher.FunctionName` | DRY/Encap. | Low | Low |
| 9 | R7: consolidate queue components; R8: predicate cleanup; E4: batch semantics | KISS | Medium | Low-Medium |

All changes are compatible with existing conventions (Spanish comments, per-module independence, embedded migrations) and verifiable with `make lint && make test`, plus `make migrate` for E1.
