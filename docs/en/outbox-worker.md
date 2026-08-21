# Outbox worker (`orders-workers`) and dispatcher (`orders-dispatcher`)

This document describes how pending events from the `outbox` table are processed, the decisions made when implementing it and the conventions followed.

> Spanish version: [docs/outbox-worker.md](../outbox-worker.md)

## Goal

Events from the `outbox` table are published to SNS through an **on-demand fan-out**: a *dispatcher* counts the pending records and launches as many *worker* invocations as batches are required (bounded by `maxWorkers`). Both replicate the same DB connection *harness* as `orders-api` (common code extracted into `rcm-platform`).

## Flow of a processing round

1. EventBridge invokes the **dispatcher** (`orders-dispatcher`) according to the schedule (by default `rate(1 minute)`).
2. The dispatcher counts the pending and available records of `outbox` and computes `workers = min(ceil(pending / batchSize), maxWorkers)`.
3. It enqueues `workers` work messages into the standard SQS queue `rcm-outbox-dispatch` (one message per worker), in chunks of 10 (`SendMessageBatch`). If there are no pending records, nothing is enqueued.
4. Each queue message triggers a **worker** invocation (`orders-workers`) via an *event source mapping* (`BatchSize=1`): one message = one batch.
5. The worker processes **one block** of records (10 by default) in three phases, without holding locks during publishing:
   - **Claim**: `UPDATE ... SET status='claimed', attempts=attempts+1 ... RETURNING ...` with `FOR UPDATE SKIP LOCKED`, as a single statement (locks are released when the statement finishes).
   - **Publish**: publishes each record to the SNS FIFO topic with the format `{"eventType": ..., "payload": ...}`, outside any transaction.
   - **Ack/Nack**: depending on the result:
     - Success → `UPDATE ... SET status='published', published_at=now()`.
     - Failure → `SET status='dead'` if retries are exhausted, or `SET status='pending', available_at=now()+backoff` to retry with backoff and jitter.

## Scaling by pending records

- The **dispatcher** decides how many worker instances are launched based on the pending count: there is no fixed number of EventBridge rules.
- The worker caps its maximum concurrency with `ReservedConcurrentExecutions = maxWorkers`, so the dispatcher's fan-out never exceeds that limit.
- `FOR UPDATE SKIP LOCKED` guarantees that concurrent invocations do not claim the same records: each row locked by a worker is "skipped" by the others.
- If a dispatch message fails in the worker, SQS retries it after the visibility window (the message is not deleted); this is harmless because the claim skips already-published/claimed records.

## Delivery semantics: *at-least-once*

- A record is only marked `published` after being successfully published to SNS.
- If the Lambda dies between `Publish` and the ack, the record stays `claimed` and the *reaper* returns it to `pending` once the lease expires, publishing it again (possible duplicate). The topic is FIFO with **content-based deduplication**, which mitigates duplicates within the deduplication window.

## Retries, backoff, jitter and DLQ

- When publishing fails, the record goes back to `pending` with an `available_at` computed using **capped exponential backoff** (`base × 2^(attempts-1)`, capped at `maxBackoff`) and **full jitter** (`random(0, backoff)`). The claim query only claims records with `available_at <= now()`, which implements retrying without sleeps inside the Lambda and spreads out the *thundering herd*.
- `attempts` is incremented on claim; `last_error` stores the last error.
- When `maxAttempts` is exhausted, the record is marked `status='dead'` (logical DLQ in the table itself). Redrive = back to `pending` via SQL.

### Dispatch queue DLQ

The `rcm-outbox-dispatch` queue has its own DLQ (`rcm-outbox-dispatch-dlq`) with a redrive policy (`maxReceiveCount=3`): if a work message fails in the worker 3 times (SQS receives, e.g. Lambda errors or timeouts), SQS moves it to the DLQ so it stops blocking the flow. Messages in the DLQ do not imply `dead` records in the table: they are jobs that never got to run and can be relaunched.

To reprocess DLQ messages (move them back to the source queue):

```sh
make redrive QUEUE=rcm-outbox-dispatch
```

The target uses aws cli against the active endpoint (floci or real AWS depending on environment), preserves body, attributes and `MessageGroupId`, and regenerates the `MessageDeduplicationId` so FIFO deduplication does not discard the redrive. The DLQ retains messages for 14 days.

## Ordering in the FIFO topic

- `MessageGroupId = aggregate_id` is used to preserve per-aggregate ordering within each group.
- **Caveat**: with several concurrent workers, two events from the same `aggregate_id` can be published out of order if different invocations claim them. Accepted for this scope; it could be strengthened by grouping by aggregate or serializing publishing.

## Published message format

```json
{
  "eventType": "CreatedOrder",
  "payload": { "...original outbox payload..." }
}
```

The `eventType` allows the consumer to route; the `payload` is the original JSON stored in the `payload` column of `outbox`.

## Configuration

### `orders-dispatcher`

| Environment variable | Use |
|---------------------|-----|
| `DB_SECRET_ARN` | ARN of the Secrets Manager secret with the PostgreSQL credentials (common via `rcm-platform`). |
| `DISPATCH_QUEUE_URL` | URL of the dispatch SQS queue where jobs are enqueued. |
| `BATCH_SIZE` | Block size per worker (default 10); used to compute the number of workers. |
| `MAX_WORKERS` | Cap of workers launched per dispatcher invocation (default 20). |

### `orders-workers`

| Environment variable | Use |
|---------------------|-----|
| `DB_SECRET_ARN` | ARN of the Secrets Manager secret with the PostgreSQL credentials (common via `rcm-platform`). |
| `SNS_TOPIC_ARN` | ARN of the SNS FIFO topic where events are published. |
| `BATCH_SIZE` | Size of the record block per invocation (default 10). |
| `MAX_ATTEMPTS` | Maximum publish attempts before marking `dead` (default 5). |
| `BACKOFF_BASE_SECONDS` | Base of the exponential backoff in seconds (default 60). |
| `MAX_BACKOFF_SECONDS` | Cap of the exponential backoff in seconds (default 480). |

In the Pulumi stack, the `worker` section (`infra/Pulumi.dev.yaml`) defines `topicName`, `batchSize`, `maxWorkers`, `maxAttempts`, `backoffBaseSeconds` and `maxBackoffSeconds`.

## Conventions taken

- **Common code in `rcm-platform`**: `logger`, `config` (reading `DB_SECRET_ARN`), `secrets` and `database` were extracted from `orders-api` to avoid duplication. Each Lambda consumes `github.com/rcastellor/rcm-outbox/rcm-platform/...`.
- **Module structure** mirrors `orders-api`: `cmd/lambda` → `internal/bootstrap` → `internal/handler`, with `repository`/`publisher`/`worker` (or `dispatcher`/`queue`) for the logic.
- **Atomic and short claim**: claiming is a single `UPDATE ... RETURNING` statement; locks are not held during the SNS calls.
- **Minimum frequency**: EventBridge/CloudWatch does not support seconds; the minimum is `rate(1 minute)`.
- **Standard dispatch queue** (not FIFO): work messages do not require ordering; retrying a message produces no logical duplicates thanks to `SKIP LOCKED`.
- **Least-privilege permissions**: the dispatcher role can only `secretsmanager:GetSecretValue` on the DB secret and `sqs:SendMessage` on the dispatch queue; the worker role can only `secretsmanager:GetSecretValue`, `sns:Publish` on the topic and `sqs:ReceiveMessage`/`sqs:DeleteMessage`/`sqs:GetQueueAttributes` on the dispatch queue.
