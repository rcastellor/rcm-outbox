# Consumidor de estadísticas (`orders-stats-consumer`)

Este documento describe el dominio `stats`: la cola de consumidores, la tabla DynamoDB de agregación y la Lambda que suma los items comprados por cliente, producto y día.

## Objetivo

A partir de los eventos publicados en el topic SNS de órdenes (ver [outbox-worker.md](outbox-worker.md)), mantener contadores diarios del número de items comprados por cliente y producto. Es el primer consumidor del fan-out: la cola SQS `rcm-outbox-orders-stats.fifo` está suscrita al topic con **raw message delivery**, por lo que cada mensaje SQS es exactamente el mensaje SNS original.

## Infraestructura (`infra/internal/domains/stats`)

El componente `rcm-outbox:stats:Stats` compone tres recursos:

1. **Cola SQS FIFO** (`platform.SQS`, nombre `rcm-outbox-orders-stats.fifo`) suscrita al topic de órdenes con política de envío y suscripción raw. Tiene una DLQ FIFO (`rcm-outbox-orders-stats-dlq.fifo`, retención 14 días) con redrive policy `maxReceiveCount=3`: un mensaje que falle en la Lambda 3 veces se aísla en la DLQ sin bloquear el resto de la cola. Para re-procesarlo: `make redrive QUEUE=rcm-outbox-orders-stats.fifo`.
2. **Tabla DynamoDB** (`platform.Table`, nombre `rcm-outbox-stats`, claves `pk`/`sk` String, billing `PAY_PER_REQUEST`).
3. **Lambda consumidora** (`orders-stats-consumer`, nombre fijo `rcm-outbox-orders-stats-consumer`) con *event source mapping* desde la cola (`BatchSize=1`, `MaximumConcurrency=5`) y permiso IAM mínimo `dynamodb:UpdateItem` sobre la tabla.

## Modelo de datos

Un registro por par cliente/producto y día:

| Clave | Valor | Ejemplo |
|-------|-------|---------|
| `pk` (String) | `CUSTOMER#<customerId>#<YYYY-MM-DD>` | `CUSTOMER#c1#2026-01-01` |
| `sk` (String) | `PRODUCT#<productId>` | `PRODUCT#p1` |
| `items` (Number) | items comprados ese día | `30` |

## Procesamiento

1. El event source mapping entrega mensajes de la cola a la Lambda.
2. El handler deserializa el envelope `{"eventType": ..., "payload": ...}`.
3. Solo `CreatedOrder` actualiza estadísticas; para cada línea de la orden ejecuta un `UpdateItem` atómico con `ADD items :quantity` sobre `pk=CUSTOMER#<customerId>#<día>` / `sk=PRODUCT#<productId>`.
4. El día de la PK es la fecha **UTC del `createdAt` de la orden** (no la de procesamiento): el conteo queda en el día correcto aunque haya retrasos o reintentos.
5. Otros eventos (`UpdatedOrder`, `DeletedOrder`) se registran en el log y se confirman sin efecto, para no envenenar la cola.
6. Si falla la escritura en DynamoDB, el handler devuelve error y SQS reintenta el mensaje. Como el incremento es un `ADD` atómico, un reintento tras un fallo transitorio no duplica salvo pérdida tras escribir y antes del ack de SQS (*at-least-once*, igual que el resto del pipeline).

## Configuración

### `orders-stats-consumer`

| Variable de entorno | Uso |
|---------------------|-----|
| `STATS_TABLE_NAME` | Nombre de la tabla DynamoDB de estadísticas. |

No usa base de datos relacional ni secreto de Secrets Manager.

## Test end-to-end

`make test-e2e` ejecuta `test/e2e` contra floci: crea dos órdenes del mismo cliente el mismo día vía la API, espera el ciclo natural del scheduler (`rate(1 minute)`, sin invocar lambdas a mano) y verifica en DynamoDB que los contadores por producto alcanzan la suma esperada (timeout 180 s). Requiere `make up && make build && make infra-up && make migrate`.

## Convenciones tomadas

- **La cola vive en el dominio `stats`**: al moverla dentro del componente cambió el URN lógico de Pulumi respecto a cuando se creaba suelta en `main.go`; el primer `pulumi up` posterior reemplaza cola y suscripción (aceptado en dev).
- **Solo compras nuevas suman**: las actualizaciones y borrados de órdenes no recalculan deltas; quedan como extensión futura si se necesita.
- **Contador atómico**: `ADD` evita leer-modificar-escribir y hace idempotente la concurrencia entre invocaciones.
- **Permisos de menor privilegio**: el rol del consumidor solo puede `dynamodb:UpdateItem` sobre la tabla de estadísticas.
