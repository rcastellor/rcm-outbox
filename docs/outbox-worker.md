# Worker de outbox (`orders-workers`) y dispatcher (`orders-dispatcher`)

Este documento describe cómo se procesan los eventos pendientes de la tabla `outbox`, las decisiones tomadas al implementarlo y las convenciones seguidas.

## Objetivo

Los eventos de la tabla `outbox` se publican en SNS mediante un **fan-out bajo demanda**: un *dispatcher* cuenta los registros pendientes y lanza tantas invocaciones del *worker* como batches sean necesarios (acotado por `maxWorkers`). Ambos replican el mismo *harness* de conexión a BD que `orders-api` (código común extraído a `rcm-platform`).

## Flujo de una ronda de procesamiento

1. EventBridge invoca al **dispatcher** (`orders-dispatcher`) según el schedule (por defecto `rate(1 minute)`).
2. El dispatcher cuenta los registros pendientes y disponibles de `outbox` y calcula `workers = min(ceil(pending / batchSize), maxWorkers)`.
3. Encola `workers` mensajes de trabajo en la cola SQS estándar `rcm-outbox-dispatch` (un mensaje por worker), en bloques de 10 (`SendMessageBatch`). Si no hay pendientes, no encola nada.
4. Cada mensaje de la cola dispara una invocación del **worker** (`orders-workers`) vía *event source mapping* (`BatchSize=1`): un mensaje = un batch.
5. El worker procesa **un bloque** de registros (por defecto 10) en tres fases, sin retener locks durante la publicación:
   - **Claim**: `UPDATE ... SET status='claimed', attempts=attempts+1 ... RETURNING ...` con `FOR UPDATE SKIP LOCKED`, como sentencia única (los locks se liberan al terminar el statement).
   - **Publish**: publica cada registro en el topic SNS FIFO con formato `{"eventType": ..., "payload": ...}`, fuera de transacción.
   - **Ack/Nack**: según el resultado:
     - Éxito → `UPDATE ... SET status='published', published_at=now()`.
     - Fallo → `SET status='dead'` si agotó los reintentos, o `SET status='pending', available_at=now()+backoff` para reintentar con backoff y jitter.

## Escalado según registros pendientes

- El **dispatcher** decide cuántas instancias del worker se lanzan en función de los pendientes: no hay un número fijo de reglas de EventBridge.
- El worker limita su concurrencia máxima con `ReservedConcurrentExecutions = maxWorkers`, de modo que el fan-out del dispatcher no excede ese tope.
- `FOR UPDATE SKIP LOCKED` garantiza que las invocaciones concurrentes no reclamen los mismos registros: cada fila bloqueada por un worker queda "saltada" por los demás.
- Si un mensaje de dispatch falla en el worker, SQS lo reintenta tras la ventana de visibilidad (el mensaje no se elimina); es inofensivo porque el claim salta los registros ya publicados/reclamados.

## Semántica de entrega: *at-least-once*

- Un registro solo se marca `published` tras publicarse con éxito en SNS.
- Si la Lambda muere entre `Publish` y el ack, el registro queda `claimed` y el *reaper* lo devuelve a `pending` al expirar el lease, volviéndose a publicar (posible duplicado). El topic es FIFO con **deduplicación por contenido**, lo que mitiga duplicados dentro de la ventana de deduplicación.

## Reintentos, backoff, jitter y DLQ

- Al fallar la publicación, el registro vuelve a `pending` con un `available_at` calculado con **backoff exponencial acotado** (`base × 2^(attempts-1)`, cap en `maxBackoff`) y **full jitter** (`random(0, backoff)`). La query de claim solo reclama registros con `available_at <= now()`, lo que implementa el reintento sin sleeps en la Lambda y dispersa el *thundering herd*.
- `attempts` se incrementa al reclamar; `last_error` guarda el último error.
- Al agotar `maxAttempts`, el registro se marca `status='dead'` (DLQ lógica en la propia tabla). Redrive = volver a `pending` vía SQL.

### DLQ de la cola de dispatch

La cola `rcm-outbox-dispatch` tiene su propia DLQ (`rcm-outbox-dispatch-dlq`) con una redrive policy (`maxReceiveCount=3`): si un mensaje de trabajo falla en el worker 3 veces (recibos SQS, p. ej. errores de la Lambda o timeouts), SQS lo mueve a la DLQ y deja de bloquear el flujo. Los mensajes en la DLQ no implican registros `dead` en la tabla: son trabajos que no llegó a ejecutarse y pueden re-lanzarse.

Para re-procesar los mensajes de la DLQ (moverlos de vuelta a la cola origen):

```sh
make redrive QUEUE=rcm-outbox-dispatch
```

El target usa aws cli contra el endpoint activo (floci o AWS real según entorno), preserva body, atributos y `MessageGroupId`, y regenera el `MessageDeduplicationId` para que la deduplicación FIFO no descarte el redrive. La DLQ retiene mensajes 14 días.

## Orden en el topic FIFO

- Se usa `MessageGroupId = aggregate_id` para preservar el orden por aggregate dentro de cada grupo.
- **Caveat**: con varios workers concurrentes, dos eventos del mismo `aggregate_id` pueden publicarse desordenados si los reclaman invocaciones distintas. Aceptado para este alcance; se podría reforzar agrupando por aggregate o serializando la publicación.

## Formato del mensaje publicado

```json
{
  "id": "7a6a... (UUID del registro outbox)",
  "eventType": "CreatedOrder",
  "payload": { "...payload original del outbox..." }
}
```

El `id` es el UUID del registro `outbox` y permite a los consumidores deduplicar (patrón inbox); el `eventType` permite enrutar; el `payload` es el JSON original almacenado en la columna `payload` de `outbox`.

## Configuración

### `orders-dispatcher`

| Variable de entorno | Uso |
|---------------------|-----|
| `DB_SECRET_ARN` | ARN del secreto de Secrets Manager con las credenciales de PostgreSQL (común vía `rcm-platform`). |
| `DISPATCH_QUEUE_URL` | URL de la cola SQS de dispatch donde se encolan los trabajos. |
| `BATCH_SIZE` | Tamaño de bloque por worker (default 10); usado para calcular el nº de workers. |
| `MAX_WORKERS` | Tope de workers a lanzar por invocación del dispatcher (default 20). |

### `orders-workers`

| Variable de entorno | Uso |
|---------------------|-----|
| `DB_SECRET_ARN` | ARN del secreto de Secrets Manager con las credenciales de PostgreSQL (común vía `rcm-platform`). |
| `SNS_TOPIC_ARN` | ARN del topic SNS FIFO donde se publican los eventos. |
| `BATCH_SIZE` | Tamaño del bloque de registros por invocación (default 10). |
| `MAX_ATTEMPTS` | Intentos máximos de publicación antes de marcar `dead` (default 5). |
| `BACKOFF_BASE_SECONDS` | Base del backoff exponencial en segundos (default 60). |
| `MAX_BACKOFF_SECONDS` | Tope del backoff exponencial en segundos (default 480). |

En el stack de Pulumi, la sección `worker` (`infra/Pulumi.dev.yaml`) define `topicName`, `batchSize`, `maxWorkers`, `maxAttempts`, `backoffBaseSeconds` y `maxBackoffSeconds`.

## Convenciones tomadas

- **Código común en `rcm-platform`**: `logger`, `config` (lectura de `DB_SECRET_ARN`), `secrets` y `database` se extrajeron de `orders-api` para no duplicarlos. Cada Lambda consume `github.com/rcastellor/rcm-outbox/rcm-platform/...`.
- **Estructura del módulo** espejo de `orders-api`: `cmd/lambda` → `internal/bootstrap` → `internal/handler`, con `repository`/`publisher`/`worker` (o `dispatcher`/`queue`) para la lógica.
- **Claim atómico y corto**: el reclamo es una única sentencia `UPDATE ... RETURNING`; los locks no se retienen durante las llamadas a SNS.
- **Frecuencia mínima**: EventBridge/CloudWatch no soporta segundos; el mínimo es `rate(1 minute)`.
- **Cola de dispatch estándar** (no FIFO): los mensajes de trabajo no requieren orden; el reintento de un mensaje no produce duplicados lógicos gracias a `SKIP LOCKED`.
- **Permisos de menor privilegio**: el rol del dispatcher solo puede `secretsmanager:GetSecretValue` sobre el secreto de BD y `sqs:SendMessage` sobre la cola de dispatch; el rol del worker solo puede `secretsmanager:GetSecretValue`, `sns:Publish` sobre el topic y `sqs:ReceiveMessage`/`sqs:DeleteMessage`/`sqs:GetQueueAttributes` sobre la cola de dispatch.
