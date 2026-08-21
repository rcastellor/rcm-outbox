# Infrastructure configuration conventions (Pulumi)

This document records how resources and their configuration are defined in the `infra` module, and why.

> Spanish version: [docs/config-conventions.md](../config-conventions.md)

## Single source of truth for shared types

Types used both as stack configuration schema (read via `cfg.GetObject`) and as input to a component live in a single shared package: `infra/internal/config`.

### Rule

- If a type represents the same thing in two layers (stack config and a component's API), it is defined **once** in `infra/internal/config` and reused.
- Do not duplicate a type in the `config` package and in the component (the *parallel type hierarchy* anti-pattern): it forces maintaining a field-by-field manual mapping and can silently diverge.
- Types in `infra/internal/config` carry explicit JSON tags so `cfg.GetObject` deserializes robustly, without relying on `encoding/json`'s case-insensitive matching.

### Example: `Topic`

`infra/internal/config/topic.go` defines `Topic` (with `json:"name"`, `json:"fifo"`, `json:"contentBasedDeduplication"`).

- `infra/internal/config`: `Config.Topics []Topic`.
- `infra/internal/platform/sns.go`: `SNSArgs.Topics []config.Topic`.
- `infra/main.go`: passes `cfg.Topics` directly, with no intermediate mapping.

## When NOT to unify

A `*Config` in the `config` package can stay separate when it is a **subset** with different semantics, not a duplicate:

- `DatabaseConfig` (`infra/internal/config`) only exposes what the user overrides in the stack (`skipFinalSnapshot`).
- `platform.DatabaseArgs` is the component's full API (13 fields, with defaults applied in `platform/database.go`).

They are different schemas: the first is user input, the second is the component's internal contract. They are not unified.

## Defaults

Default values are applied **inside the component** (e.g. `applyDatabaseDefaults` in `platform/database.go` or `applySNSDefaults` in `platform/sns.go`), not in the config layer. The stack config only contains what the user wants to override.

### Example: `worker`

`infra/internal/config` defines `WorkerConfig` (`topicName`, `batchSize`, `maxWorkers`, `maxAttempts`, `backoffBaseSeconds`, `maxBackoffSeconds`) as the subset the user overrides in the stack. Defaults are applied in the worker component (`domains/outbox/worker.go`, `applyDefaults`), not in the config. The default for `topicName` (`rcm-outbox-orders.fifo`, `config.DefaultWorkerTopicName`) is resolved in `main.go`, because it is an orchestration reference (which topic of the `sns` component to use), not a single component's contract.
