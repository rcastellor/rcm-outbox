# Stats consumer (`orders-stats-consumer`)

This document describes the `stats` domain: the consumer queue, the DynamoDB aggregation table and the Lambda that sums purchased items per customer, product and day.

> Spanish version: [docs/stats-consumer.md](../stats-consumer.md)

## Goal

Starting from the events published to the orders SNS topic (see [outbox-worker.md](outbox-worker.md)), maintain daily counters of the number of items purchased per customer and product. It is the first consumer of the fan-out: the SQS queue `rcm-outbox-orders-stats.fifo` is subscribed to the topic with **raw message delivery**, so each SQS message is exactly the original SNS message.

## Infrastructure (`infra/internal/domains/stats`)

The `rcm-outbox:stats:Stats` component composes three resources:

1. **SQS FIFO queue** (`platform.SQS`, name `rcm-outbox-orders-stats.fifo`) subscribed to the orders topic with a delivery policy and raw subscription. It has a FIFO DLQ (`rcm-outbox-orders-stats-dlq.fifo`, 14-day retention) with redrive policy `maxReceiveCount=3`: a message that fails in the Lambda 3 times is isolated into the DLQ without blocking the rest of the queue. To reprocess it: `make redrive QUEUE=rcm-outbox-orders-stats.fifo`.
2. **DynamoDB table** (`platform.Table`, name `rcm-outbox-stats`, keys `pk`/`sk` String, billing `PAY_PER_REQUEST`).
3. **Consumer Lambda** (`orders-stats-consumer`, fixed name `rcm-outbox-orders-stats-consumer`) with an *event source mapping* from the queue (`BatchSize=1`, `MaximumConcurrency=5`) and minimal IAM permission `dynamodb:UpdateItem` on the table.

## Data model

One record per customer/product pair and day:

| Key | Value | Example |
|-------|-------|---------|
| `pk` (String) | `CUSTOMER#<customerId>#<YYYY-MM-DD>` | `CUSTOMER#c1#2026-01-01` |
| `sk` (String) | `PRODUCT#<productId>` | `PRODUCT#p1` |
| `items` (Number) | items purchased that day | `30` |

## Processing

1. The event source mapping delivers messages from the queue to the Lambda.
2. The handler deserializes the envelope `{"eventType": ..., "payload": ...}`.
3. Only `CreatedOrder` updates stats; for each order line it runs an atomic `UpdateItem` with `ADD items :quantity` on `pk=CUSTOMER#<customerId>#<day>` / `sk=PRODUCT#<productId>`.
4. The day of the PK is the **UTC date of the order's `createdAt`** (not the processing date): the count lands on the correct day even with delays or retries.
5. Other events (`UpdatedOrder`, `DeletedOrder`) are logged and acknowledged without effect, so they do not poison the queue.
6. If the DynamoDB write fails, the handler returns an error and SQS retries the message. Since the increment is an atomic `ADD`, a retry after a transient failure does not duplicate unless loss happens after writing and before acking SQS (*at-least-once*, same as the rest of the pipeline).

## Configuration

### `orders-stats-consumer`

| Environment variable | Use |
|---------------------|-----|
| `STATS_TABLE_NAME` | Name of the stats DynamoDB table. |

It uses neither a relational database nor a Secrets Manager secret.

## End-to-end test

`make test-e2e` runs `test/e2e` against floci: it creates two orders from the same customer on the same day via the API, waits for the natural scheduler cycle (`rate(1 minute)`, without invoking lambdas by hand) and verifies in DynamoDB that the per-product counters reach the expected sum (180 s timeout). Requires `make up && make build && make infra-up && make migrate`.

## Conventions taken

- **The queue lives in the `stats` domain**: moving it inside the component changed Pulumi's logical URN compared to when it was created standalone in `main.go`; the first `pulumi up` afterwards replaces the queue and subscription (accepted in dev).
- **Only new purchases count**: order updates and deletions do not recompute deltas; they remain as a future extension if needed.
- **Atomic counter**: `ADD` avoids read-modify-write and makes concurrency idempotent across invocations.
- **Least-privilege permissions**: the consumer role can only `dynamodb:UpdateItem` on the stats table.
