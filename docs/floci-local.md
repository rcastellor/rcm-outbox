# floci local: manual de uso y puesta en marcha

floci es el emulador de AWS local que usa este proyecto (no LocalStack). Corre con Docker Compose en [`local/docker-compose.yml`](../local/docker-compose.yml) y emula los servicios que necesita el pipeline: Lambda, SQS, SNS, DynamoDB, EventBridge, RDS (PostgreSQL), Secrets Manager y API Gateway, entre otros.

- Endpoint: `http://localhost:4566`
- UI: `http://localhost:4500`

## Modelo de persistencia

floci guarda su estado en dos sitios, **fuera del control de Git**:

| Ubicación | Contenido |
|---|---|
| `local/data/` (bind mount en `/app/data`) | Estado de los servicios emulados: tablas DynamoDB, colas SQS, topics SNS, reglas de EventBridge, funciones Lambda, secretos, etc. (un fichero JSON por servicio). |
| Volúmenes Docker `floci-rds-db-*` | Datos de PostgreSQL de cada instancia RDS que floci levanta. |

El estado de Pulumi (`infra/Pulumi.dev.yaml` + backend local) es independiente de floci: regenerar floci **no** borra el estado de Pulumi, por lo que no hace falta volver a hacer `make infra-init`.

## Comportamientos conocidos

Al reutilizar el estado persistido entre sesiones (parar/arrancar floci sin limpiar), floci puede quedar inconsistente. Hemos observado dos fallos concretos:

- **Tablas DynamoDB que desaparecen**: floci mueve la metadata de una tabla a un fichero `dynamodb-tables.json.corrupt` cuando detecta que su fichero se corrompió (p. ej. tras un cierre brusco), y deja de servir la tabla aunque sus items sigan en `dynamodb-items.json`. Los clientes reciben `ResourceNotFoundException` en `GetItem`.
- **Scheduler de EventBridge que no se restaura**: tras reiniciar floci, la regla `rate(1 minute)` que dispara el dispatcher puede quedar en estado `ENABLED` pero sin su *scheduler* en memoria, por lo que deja de invocar la Lambda (el outbox no se procesa solo). Consultar la API de EventBridge (p. ej. `aws events list-rules`) fuerza la carga perezosa de las reglas y suele restaurarlo.
- **Contenedores y volúmenes huérfanos**: floci levanta Lambdas en contenedores desechables y una instancia RDS en un contenedor aparte; con el tiempo quedan contenedores `floci-*` y volúmenes `floci-rds-db-*` de despliegues anteriores.

Por todo esto, **la recomendación es regenerar los contenedores de floci al empezar a trabajar** en vez de confiar en el estado persistido de la sesión anterior. Es barato (un `make reset-floci` + `make up` + `make infra-up` + `make migrate`) y evita perder tiempo depurando estados corruptos.

## Manual de uso

| Comando | Descripción |
|---|---|
| `make up` | Arranca floci + UI. |
| `make down` | Detiene floci + UI (mantiene `local/data` y los volúmenes RDS). |
| `make reset-floci` | Regenera floci desde cero: borra contenedores `floci-*`, volúmenes `floci-rds-db-*` y `local/data`. |
| `make logs` / `make ps` | Logs de floci + UI / estado de los contenedores. |

Consultar servicios emulados por CLI (con las variables que ya exporta el Makefile):

```bash
aws dynamodb list-tables
aws sqs list-queues
aws events list-rules
```

## Puesta en marcha (arranque limpio del proyecto)

Cuando empieces a trabajar (o el entorno lleve tiempo sin uso), regenera floci y despliega desde cero:

```bash
make reset-floci   # borra el estado persistido de floci (contenedores + volúmenes + local/data)
make up            # arranca floci limpio + UI
make build         # compila las Lambdas a bin/<módulo>/bootstrap
make infra-refresh # sincroniza el estado de Pulumi con el floci vacío (marca los recursos como desaparecidos)
make infra-up      # despliega/recrea toda la infraestructura con Pulumi
make migrate       # aplica las migraciones SQL vía la Lambda rcm-migrations
make test-e2e      # opcional: verifica el flujo completo (API → outbox → SNS → DynamoDB)
```

Notas:

- **`make infra-refresh` antes de `make infra-up` es necesario tras `reset-floci`**: Pulumi conserva en su estado los recursos que ya desplegó, y `pulumi up` por sí solo no detecta que floci ya no los tiene (no hay nada que "recrear" si el estado sigue creyendo que existen). El `pulumi refresh` reconcilia el estado con el floci vacío (marca los recursos como desaparecidos) y, a partir de ahí, `pulumi up` los recrea.
- `make infra-init` solo se ejecuta la primera vez en una máquina (inicializa el backend local de Pulumi y el stack `dev`); no es necesario tras un `make reset-floci` porque el estado de Pulumi no se borra.
- `make reset-floci` no borra el estado de Pulumi ni `bin/`.
- Si tras un `make up` el test e2e se queda esperando el ciclo del scheduler, prueba a forzar la carga de las reglas de EventBridge (`aws events list-rules`) o, en último caso, regenera con `make reset-floci`.
