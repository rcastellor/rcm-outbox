#!/usr/bin/env bash

set -euo pipefail

TOTAL=10000
CONCURRENCY=10
URL="http://localhost:4566/restapis/ed111fbdba/dev/_user_request_/orders"
PAYLOAD='{"customerId":"c1","status":"created","lines":[{"productId":"p1","quantity":2,"unitPrice":10.5},{"productId":"p2","quantity":1,"unitPrice":4.99}]}'

run_request() {
  local delay=$(( RANDOM % 151 + 100 ))
  sleep "$(echo "scale=3; $delay/1000" | bc)"
  curl --silent --output /dev/null --write-out "%{http_code}" \
    --request POST \
    --url "$URL" \
    --header 'Content-Type: application/json' \
    --data "$PAYLOAD"
}

export -f run_request
export URL PAYLOAD

echo "Iniciando load test: $TOTAL peticiones con $CONCURRENCY hilos..."

seq "$TOTAL" | xargs -P "$CONCURRENCY" -I {} bash -c 'run_request'

echo "Load test completado."
