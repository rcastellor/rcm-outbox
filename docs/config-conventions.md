# Convenciones de configuración de infraestructura (Pulumi)

Este documento registra cómo se definen los recursos y su configuración en el módulo `infra`, y por qué.

## Fuente única de verdad para los tipos compartidos

Los tipos que se usan tanto como esquema de configuración del stack (leídos vía `cfg.GetObject`) como de entrada a un componente viven en un único paquete compartido: `infra/internal/config`.

### Regla

- Si un tipo representa lo mismo en dos capas (config del stack y API de un componente), se define **una sola vez** en `infra/internal/config` y se reutiliza.
- No duplicar un tipo en el paquete `config` y en el componente (anti-patrón *parallel type hierarchy*): obliga a mantener un mapeo manual campo a campo y puede divergir silenciosamente.
- Los tipos de `infra/internal/config` llevan tags JSON explícitos para que `cfg.GetObject` deserialice de forma robusta, sin depender del matching case-insensitive de `encoding/json`.

### Ejemplo: `Topic`

`infra/internal/config/topic.go` define `Topic` (con `json:"name"`, `json:"fifo"`, `json:"contentBasedDeduplication"`).

- `infra/internal/config`: `Config.Topics []Topic`.
- `infra/internal/platform/sns.go`: `SNSArgs.Topics []config.Topic`.
- `infra/main.go`: pasa `cfg.Topics` directamente, sin mapeo intermedio.

## Cuándo NO unificar

Una `*Config` en el paquete `config` puede mantenerse separada cuando es un **subconjunto** con semántica distinta, no un duplicado:

- `DatabaseConfig` (`infra/internal/config`) solo expone lo que el usuario sobrescribe en el stack (`skipFinalSnapshot`).
- `platform.DatabaseArgs` es la API completa del componente (13 campos, con defaults aplicados en `platform/database.go`).

Son esquemas diferentes: el primero es entrada del usuario, el segundo es el contrato interno del componente. No se unifican.

## Defaults

Los valores por defecto se aplican **dentro del componente** (p.ej. `applyDatabaseDefaults` en `platform/database.go` o `applySNSDefaults` en `platform/sns.go`), no en la capa de config. La config del stack solo contiene lo que el usuario quiere sobrescribir.

### Ejemplo: `worker`

`infra/internal/config` define `WorkerConfig` (`topicName`, `batchSize`, `maxWorkers`, `maxAttempts`, `backoffBaseSeconds`, `maxBackoffSeconds`) como el subconjunto que el usuario sobrescribe en el stack. Los defaults se aplican en el componente worker (`domains/outbox/worker.go`, `applyDefaults`), no en la config. El default de `topicName` (`rcm-outbox-orders.fifo`, `config.DefaultWorkerTopicName`) se resuelve en `main.go`, porque es una referencia de orquestación (qué topic del componente `sns` usar), no un contrato de un componente.
