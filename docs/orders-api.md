# API de órdenes (`orders-api`)

Este documento describe la API HTTP de órdenes, su tipo de API Gateway y las convenciones seguidas.

## Tipo de API Gateway

La API se expone mediante **API Gateway HTTP API (v2)**, el tipo más reciente de API Gateway (`apigatewayv2` en Pulumi). Sustituye a la antigua REST API (v1).

- **Protocolo**: `HTTP` (`ProtocolType: "HTTP"`).
- **Integración**: `AWS_PROXY` hacia la Lambda `orders-api`, con `PayloadFormatVersion: "2.0"`.
- **Rutas** (ambas apuntan a la misma integración):
  - `ANY /orders` → colección (listar/crear).
  - `ANY /orders/{proxy+}` → recurso individual.
- **Stage**: `dev`, URL de la forma `https://{api-id}.execute-api.{region}.amazonaws.com/dev`.
- **Permiso de invocación**: la Lambda acepta invocaciones desde API Gateway (`apigateway.amazonaws.com`) sobre `{execution-arn}/*/*`.

Al usar **payload 2.0**, la Lambda recibe/emite los eventos `APIGatewayV2HTTPRequest`/`APIGatewayV2HTTPResponse` de `aws-lambda-go`. El handler lee el método de `requestContext.http.method` y la ruta de `rawPath` (ver `internal/handler/handler.go`).

## Endpoints

| Método | Ruta | Acción | Códigos |
|--------|------|--------|---------|
| `GET`  | `/orders`        | Lista todas las órdenes activas. | `200` |
| `POST` | `/orders`        | Crea una orden y sus líneas. | `201`, `400` |
| `GET`  | `/orders/{id}`   | Obtiene una orden por id. | `200`, `404` |
| `PUT`  | `/orders/{id}`   | Actualiza una orden y sus líneas. | `200`, `400`, `404` |
| `DELETE` | `/orders/{id}` | Elimina (soft-delete) una orden. | `204`, `404` |

El handler delega el enrutado a `internal/router/router.go`, que decide por el método HTTP y el segmento tras `/orders` (vacío para la colección o el id de la orden).

## Entrada/salida

La orden se serializa en JSON:

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

El campo `deletedAt` se omite del JSON cuando es null (`omitempty`): solo aparece en las órdenes eliminadas lógicamente.

Para crear/actualizar (`POST`/`PUT`) el cuerpo es `{ "customerId", "status", "lines": [{ "productId", "quantity", "unitPrice" }] }`. Los errores se devuelven como `{ "error": "…" }`.

## Infraestructura

El componente `rcm-outbox:orders:API` (`infra/internal/domains/orders/api.go`) crea:

1. `apigatewayv2.Api` (HTTP API) → `apigatewayv2.Integration` (`AWS_PROXY`, payload 2.0) → dos `apigatewayv2.Route`.
2. `lambda.Permission` para permitir la invocación desde API Gateway.
3. `apigatewayv2.Deployment` + `apigatewayv2.Stage` (`dev`); la URL pública es `stage.invokeUrl`.

## Configuración

| Variable de entorno | Uso |
|---------------------|-----|
| `DB_SECRET_ARN` | ARN del secreto de Secrets Manager con las credenciales de PostgreSQL (común vía `rcm-platform`). |

## Convenciones tomadas

- **Estructura del módulo**: `cmd/lambda` → `internal/bootstrap` → `internal/handler` → `internal/router` → `internal/usecase` → `internal/repository`.
- **Código común en `rcm-platform`**: `logger`, `config` (lectura de `DB_SECRET_ARN`), `secrets` y `database`.
- **Sin autorización**: las rutas no exigen autenticación (`AuthorizationType: NONE`, valor por defecto de HTTP API).
- **Payload 2.0**: elegido para usar el formato moderno de HTTP API; el handler consume los tipos `APIGatewayV2*` de `aws-lambda-go`.
