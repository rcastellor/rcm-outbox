# Local floci: usage manual and cold start

floci is the local AWS emulator used by this project (not LocalStack). It runs with Docker Compose in [`local/docker-compose.yml`](../local/docker-compose.yml) and emulates the services the pipeline needs: Lambda, SQS, SNS, DynamoDB, EventBridge, RDS (PostgreSQL), Secrets Manager and API Gateway, among others.

- Endpoint: `http://localhost:4566`
- UI: `http://localhost:4500`

> Spanish version: [docs/floci-local.md](../floci-local.md)

## Persistence model

floci stores its state in two places, both **outside Git control**:

| Location | Contents |
|---|---|
| `local/data/` (bind-mounted at `/app/data`) | State of the emulated services: DynamoDB tables, SQS queues, SNS topics, EventBridge rules, Lambda functions, secrets, etc. (one JSON file per service). |
| Docker volumes `floci-rds-db-*` | PostgreSQL data of each RDS instance floci spins up. |

Pulumi state (`infra/Pulumi.dev.yaml` + local backend) is independent of floci: regenerating floci does **not** erase the Pulumi state, so you do not need to run `make infra-init` again.

## Known behaviors

When the persisted state is reused across sessions (stopping/starting floci without cleaning), floci can become inconsistent. Two concrete failures have been observed:

- **DynamoDB tables disappearing**: floci moves a table's metadata to a `dynamodb-tables.json.corrupt` file when it detects the file was corrupted (e.g. after an abrupt shutdown), and stops serving the table even though its items remain in `dynamodb-items.json`. Clients receive `ResourceNotFoundException` on `GetItem`.
- **EventBridge scheduler not restored**: after restarting floci, the `rate(1 minute)` rule that triggers the dispatcher can stay `ENABLED` but lose its in-memory scheduler, so it stops invoking the Lambda (the outbox is no longer processed on its own). Querying the EventBridge API (e.g. `aws events list-rules`) forces the lazy loading of the rules and usually restores it.
- **Orphaned containers and volumes**: floci runs Lambdas in disposable containers and a separate RDS instance container; over time, `floci-*` containers and `floci-rds-db-*` volumes from previous deployments pile up.

For all of these reasons, **the recommendation is to regenerate the floci containers when starting work** instead of relying on the persisted state of the previous session. It is cheap (a `make reset-floci` + `make up` + `make infra-up` + `make migrate`) and avoids wasting time debugging corrupt states.

## Usage manual

| Command | Description |
|---|---|
| `make up` | Starts floci + UI. |
| `make down` | Stops floci + UI (keeps `local/data` and the RDS volumes). |
| `make reset-floci` | Regenerates floci from scratch: removes `floci-*` containers, `floci-rds-db-*` volumes and `local/data`. |
| `make logs` / `make ps` | floci + UI logs / container status. |

Query emulated services via CLI (with the variables the Makefile already exports):

```bash
aws dynamodb list-tables
aws sqs list-queues
aws events list-rules
```

## Cold start (clean project boot)

When you start working (or the environment has been idle for a while), regenerate floci and deploy from scratch:

```bash
make reset-floci   # deletes floci's persisted state (containers + volumes + local/data)
make up            # starts a clean floci + UI
make build         # compiles the Lambdas to bin/<module>/bootstrap
make infra-refresh # syncs the Pulumi state with the empty floci (marks resources as gone)
make infra-up      # deploys/recreates all infrastructure with Pulumi
make migrate       # applies SQL migrations via the rcm-migrations Lambda
make test-e2e      # optional: verifies the full flow (API → outbox → SNS → DynamoDB)
```

Notes:

- **`make infra-refresh` before `make infra-up` is required after `reset-floci`**: Pulumi keeps in its state the resources it already deployed, and `pulumi up` alone does not detect that floci no longer has them (there is nothing to "recreate" if the state still believes they exist). `pulumi refresh` reconciles the state with the empty floci (marks the resources as gone) and, from there, `pulumi up` recreates them.
- `make infra-init` only runs the first time on a machine (initializes Pulumi's local backend and the `dev` stack); it is not needed after `make reset-floci` because Pulumi state is not erased.
- `make reset-floci` does not delete the Pulumi state nor `bin/`.
- If after `make up` the e2e test keeps waiting for the scheduler cycle, try forcing the EventBridge rules to load (`aws events list-rules`) or, as a last resort, regenerate with `make reset-floci`.
