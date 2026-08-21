# Orders API (`orders-api`)

This document describes the orders HTTP API, its API Gateway type and the conventions followed.

> Spanish version: [docs/orders-api.md](../orders-api.md)

## API Gateway type

The API is exposed through an **API Gateway HTTP API (v2)**, the newest API Gateway type (`apigatewayv2` in Pulumi). It replaces the old REST API (v1).

- **Protocol**: `HTTP` (`ProtocolType: "HTTP"`).
- **Integration**: `AWS_PROXY` to the `orders-api` Lambda, with `PayloadFormatVersion: "2.0"`.
- **Routes** (both point to the same integration):
  - `ANY /orders` → collection (list/create).
  - `ANY /orders/{proxy+}` → individual resource.
- **Stage**: `dev`, URL of the form `https://{api-id}.execute-api.{region}.amazonaws.com/dev`.
- **Invocation permission**: the Lambda accepts invocations from API Gateway (`apigateway.amazonaws.com`) on `{execution-arn}/*/*`.

By using **payload 2.0**, the Lambda receives/emits the `aws-lambda-go` `APIGatewayV2HTTPRequest`/`APIGatewayV2HTTPResponse` events. The handler reads the method from `requestContext.http.method` and the path from `rawPath` (see `internal/handler/handler.go`).

## Endpoints

| Method | Path | Action | Codes |
|--------|------|--------|-------|
| `GET`  | `/orders`        | Lists all active orders. | `200` |
| `POST` | `/orders`        | Creates an order and its lines. | `201`, `400` |
| `GET`  | `/orders/{id}`   | Gets an order by id. | `200`, `404` |
| `PUT`  | `/orders/{id}`   | Updates an order and its lines. | `200`, `400`, `404` |
| `DELETE` | `/orders/{id}` | Deletes (soft-delete) an order. | `204`, `404` |

The handler delegates routing to `internal/router/router.go`, which decides based on the HTTP method and the segment after `/orders` (empty for the collection or the order id).

## Input/output

The order is serialized as JSON:

```json
{
  "id": "…",
  "customerId": "…",
  "status": "…",
  "lines": [{ "id": "…", "productId": "…", "quantity": 1, "unitPrice": 9.99 }],
  "createdAt": "…",
  "updatedAt": "…"
}
```

The `deletedAt` field is omitted from JSON when null (`omitempty`): it only appears on logically deleted orders.

For create/update (`POST`/`PUT`) the body is `{ "customerId", "status", "lines": [{ "productId", "quantity", "unitPrice" }] }`. Errors are returned as `{ "error": "…" }`.

## Infrastructure

The `rcm-outbox:orders:API` component (`infra/internal/domains/orders/api.go`) creates:

1. `apigatewayv2.Api` (HTTP API) → `apigatewayv2.Integration` (`AWS_PROXY`, payload 2.0) → two `apigatewayv2.Route`.
2. `lambda.Permission` to allow invocation from API Gateway.
3. `apigatewayv2.Deployment` + `apigatewayv2.Stage` (`dev`); the public URL is `stage.invokeUrl`.

## Configuration

| Environment variable | Use |
|---------------------|-----|
| `DB_SECRET_ARN` | ARN of the Secrets Manager secret with the PostgreSQL credentials (common via `rcm-platform`). |

## Conventions taken

- **Module structure**: `cmd/lambda` → `internal/bootstrap` → `internal/handler` → `internal/router` → `internal/usecase` → `internal/repository`.
- **Common code in `rcm-platform`**: `logger`, `config` (reading `DB_SECRET_ARN`), `secrets` and `database`.
- **No authorization**: routes do not require authentication (`AuthorizationType: NONE`, the HTTP API default).
- **Payload 2.0**: chosen to use the modern HTTP API format; the handler consumes the `APIGatewayV2*` types from `aws-lambda-go`.
