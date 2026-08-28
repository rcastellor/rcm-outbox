# Consumidor de estadísticas (`orders-stats-consumer`)

Este documento describe el dominio `stats`: la cola de consumidores, la tabla DynamoDB de agregación, la tabla de deduplicación (inbox) y la Lambda que suma los items comprados por cliente, producto y día.

## Objetivo

A partir de los eventos publicados en el topic SNS de órdenes (ver [outbox-worker.md](outbox-worker.md)), mantener contadores diarios del número de items comprados por cliente y producto. Es el primer consumidor del fan-out: la cola SQS `rcm-outbox-orders-stats.fifo` está suscrita al topic con **raw message delivery**, por lo que cada mensaje SQS es exactamente el mensaje SNS original.

Para que el agregado sea **exactamente-una-vez** a pesar de la entrega *at-least-once* de SQS/SNS, aplica el **patrón inbox**: cada evento procesado se registra por su `id` en una tabla de deduplicación dentro de la misma transacción DynamoDB que el incremento del contador.

## Infraestructura (`infra/internal/domains/stats`)

El componente `rcm-outbox:stats:Stats` compone cuatro recursos:

1. **Cola SQS FIFO** (`platform.SQS`, nombre `rcm-outbox-orders-stats.fifo`) suscrita al topic de órdenes con política de envío y suscripción raw. Tiene una DLQ FIFO (`rcm-outbox-orders-stats-dlq.fifo`, retención 14 días) con redrive policy `maxReceiveCount=3`: un mensaje que falle en la Lambda 3 veces se aísla en la DLQ sin bloquear el resto de la cola. Para re-procesarlo: `make redrive QUEUE=rcm-outbox-orders-stats.fifo`.
2. **Tabla DynamoDB** (`platform.Table`, nombre `rcm-outbox-stats`, claves `pk`/`sk` String, billing `PAY_PER_REQUEST`).
3. **Tabla DynamoDB de inbox** (`platform.Table`, nombre `rcm-outbox-stats-inbox`, clave `pk` String, billing `PAY_PER_REQUEST`, TTL sobre `expiresAt`): guarda los `id` de eventos ya procesados para deduplicar.
4. **Lambda consumidora** (`orders-stats-consumer`, nombre fijo `rcm-outbox-orders-stats-consumer`) con *event source mapping* desde la cola (`BatchSize=1`, `MaximumConcurrency=5`) y permiso IAM mínimo `dynamodb:TransactWriteItems` sobre ambas tablas.

## Modelo de datos

### Tabla de estadísticas (`rcm-outbox-stats`)

Un registro por par cliente/producto y día:

| Clave | Valor | Ejemplo |
|-------|-------|---------|
| `pk` (String) | `CUSTOMER#<customerId>#<YYYY-MM-DD>` | `CUSTOMER#c1#2026-01-01` |
| `sk` (String) | `PRODUCT#<productId>` | `PRODUCT#p1` |
| `items` (Number) | items comprados ese día | `30` |

### Tabla de inbox (`rcm-outbox-stats-inbox`)

Un registro por evento procesado:

| Clave | Valor | Ejemplo |
|-------|-------|---------|
| `pk` (String) | `id` del evento (UUID del outbox) | `7a6a...` |
| `processedAt` (Number) | epoch (s) del procesamiento | `1767225600` |
| `expiresAt` (Number) | epoch (s) de expiración (TTL, 30 días) | `1769817600` |

## Procesamiento

1. El event source mapping entrega mensajes de la cola a la Lambda.
2. El handler deserializa el envelope `{"id": ..., "eventType": ..., "payload": ...}` (el `id` es el UUID del registro `outbox`).
3. Solo `CreatedOrder` actualiza estadísticas; otros eventos (`UpdatedOrder`, `DeletedOrder`) se registran en el log y se confirman sin efecto, para no envenenar la cola.
4. Para cada `CreatedOrder`, la Lambda ejecuta **una única transacción `TransactWriteItems`**:
   - `Put` en la tabla inbox con `ConditionExpression: attribute_not_exists(pk)`: si el `id` ya existe, la transacción se anula y el evento se trata como **duplicado** (se confirma sin efecto).
   - Un `Update` con `ADD items :quantity` por cada par `pk=CUSTOMER#<customerId>#<día>` / `sk=PRODUCT#<productId>` (las líneas del mismo producto se suman antes, porque una transacción no admite dos operaciones sobre el mismo item).
5. El día de la PK es la fecha **UTC del `createdAt` de la orden** (no la de procesamiento): el conteo queda en el día correcto aunque haya retrasos o reintentos.
6. Si la transacción falla por una causa transitoria (p. ej. *throttling* o conflicto), se devuelve error y SQS reintenta el mensaje; al ser atómica no deja efectos parciales. El `ADD` atómico también hace idempotente la concurrencia entre invocaciones de eventos distintos.

## Configuración

### `orders-stats-consumer`

| Variable de entorno | Uso |
|---------------------|-----|
| `STATS_TABLE_NAME` | Nombre de la tabla DynamoDB de estadísticas. |
| `INBOX_TABLE_NAME` | Nombre de la tabla DynamoDB de inbox (deduplicación de eventos procesados). |

No usa base de datos relacional ni secreto de Secrets Manager.

## Test end-to-end

`make test-e2e` ejecuta `test/e2e` contra floci: crea dos órdenes del mismo cliente el mismo día vía la API, espera el ciclo natural del scheduler (`rate(1 minute)`, sin invocar lambdas a mano) y verifica en DynamoDB que los contadores por producto alcanzan la suma esperada (timeout 180 s). Requiere `make up && make build && make infra-up && make migrate`.

## Convenciones tomadas

- **La cola vive en el dominio `stats`**: al moverla dentro del componente cambió el URN lógico de Pulumi respecto a cuando se creaba suelta en `main.go`; el primer `pulumi up` posterior reemplaza cola y suscripción (aceptado en dev).
- **Solo compras nuevas suman**: las actualizaciones y borrados de órdenes no recalculan deltas; quedan como extensión futura si se necesita.
- **Inbox transaccional (exactamente-una-vez)**: el `Put` condicional en la tabla de deduplicación y el `ADD` del contador viven en la misma `TransactWriteItems`, de modo que un mensaje redelivered (reintento de SQS o redrive de la DLQ) se descarta sin doble conteo.
- **Contador atómico**: `ADD` evita leer-modificar-escribir y hace idempotente la concurrencia entre invocaciones.
- **TTL del inbox**: las entradas expiran a los 30 días (`expiresAt`), acotando el crecimiento de la tabla; es una ventana amplia respecto a los reintentos reales del pipeline.
- **Permisos de menor privilegio**: el rol del consumidor solo puede `dynamodb:TransactWriteItems` sobre las tablas de estadísticas e inbox.
