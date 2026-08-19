// Package e2e contiene el test end-to-end del flujo completo del outbox sobre
// floci: API → outbox (PostgreSQL) → dispatcher (schedule rate(1 minute)) →
// worker → SNS → consumidor de estadísticas → DynamoDB.
//
// Requiere el entorno local levantado y desplegado:
//
//	make up && make build && make infra-up && make migrate
//
// Se ejecuta con `make test-e2e`, que resuelve la URL de la API desde los
// outputs del stack de Pulumi.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// defaultStatsTable es la tabla que despliega el dominio stats cuando no se
// sobrescribe en la configuración del stack.
const defaultStatsTable = "rcm-outbox-stats"

// pollInterval y waitDeadline cubren la espera del schedule de EventBridge
// (hasta 60 s) más el pipeline dispatcher → worker → SNS → consumidor.
const (
	pollInterval = 3 * time.Second
	waitDeadline = 180 * time.Second
)

type orderLine struct {
	ProductID string  `json:"productId"`
	Quantity  int     `json:"quantity"`
	UnitPrice float64 `json:"unitPrice"`
}

type createdOrder struct {
	ID string `json:"id"`
}

// TestOrdersFlowAggregatesStats crea dos órdenes del mismo cliente en el mismo
// día y comprueba que el consumidor agrega los items por producto en DynamoDB,
// esperando el ciclo natural del scheduler (sin invocar lambdas a mano).
func TestOrdersFlowAggregatesStats(t *testing.T) {
	apiURL := os.Getenv("ORDERS_API_URL")
	if apiURL == "" {
		t.Skip("ORDERS_API_URL no definida; levanta el entorno y ejecuta make test-e2e")
	}
	table := os.Getenv("STATS_TABLE_NAME")
	if table == "" {
		table = defaultStatsTable
	}

	ctx := context.Background()
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatalf("cargando configuración AWS: %v", err)
	}
	ddb := dynamodb.NewFromConfig(awsCfg)

	// Identificadores únicos por ejecución para no chocar con datos previos.
	run := time.Now().UnixNano()
	customer := fmt.Sprintf("e2e-cust-%d", run)
	productA := fmt.Sprintf("e2e-prod-a-%d", run)
	productB := fmt.Sprintf("e2e-prod-b-%d", run)

	createOrder(t, apiURL, customer, []orderLine{
		{ProductID: productA, Quantity: 2, UnitPrice: 10},
		{ProductID: productB, Quantity: 1, UnitPrice: 4.5},
	})
	createOrder(t, apiURL, customer, []orderLine{
		{ProductID: productA, Quantity: 3, UnitPrice: 10},
	})

	pk := fmt.Sprintf("CUSTOMER#%s#%s", customer, time.Now().UTC().Format("2006-01-02"))
	waitForItems(t, ctx, ddb, table, pk, "PRODUCT#"+productA, 5)
	waitForItems(t, ctx, ddb, table, pk, "PRODUCT#"+productB, 1)
}

// createOrder publica una orden vía la API y verifica que se crea (201).
func createOrder(t *testing.T, apiURL, customer string, lines []orderLine) {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"customerId": customer,
		"status":     "created",
		"lines":      lines,
	})
	if err != nil {
		t.Fatalf("serializando orden: %v", err)
	}

	resp, err := http.Post(apiURL+"/orders", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("creando orden: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("creando orden: status %d, esperaba 201", resp.StatusCode)
	}

	var created createdOrder
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil || created.ID == "" {
		t.Fatalf("respuesta de creación inválida: %v (id=%q)", err, created.ID)
	}
}

// waitForItems consulta la tabla de estadísticas hasta que el contador del par
// cliente/producto alcanza el valor esperado o se agota el plazo.
func waitForItems(t *testing.T, ctx context.Context, ddb *dynamodb.Client, table, pk, sk string, want int) {
	t.Helper()

	deadline := time.Now().Add(waitDeadline)
	var lastFound bool
	var lastItems string

	for time.Now().Before(deadline) {
		out, err := ddb.GetItem(ctx, &dynamodb.GetItemInput{
			TableName: aws.String(table),
			Key: map[string]types.AttributeValue{
				"pk": &types.AttributeValueMemberS{Value: pk},
				"sk": &types.AttributeValueMemberS{Value: sk},
			},
			ConsistentRead: aws.Bool(true),
		})
		if err != nil {
			t.Fatalf("consultando %s | %s: %v", pk, sk, err)
		}

		if out.Item != nil {
			lastFound = true
			if attr, ok := out.Item["items"]; ok {
				num, ok := attr.(*types.AttributeValueMemberN)
				if !ok {
					t.Fatalf("atributo items de %s | %s no es numérico", pk, sk)
				}
				lastItems = num.Value
				n, err := strconv.Atoi(num.Value)
				if err != nil {
					t.Fatalf("atributo items de %s | %s inválido: %q", pk, sk, num.Value)
				}
				switch {
				case n == want:
					return
				case n > want:
					t.Fatalf("contador duplicado en %s | %s: items=%d, esperaba %d", pk, sk, n, want)
				}
			}
		}
		time.Sleep(pollInterval)
	}

	if !lastFound {
		t.Fatalf("timeout esperando %s | %s: el registro nunca apareció", pk, sk)
	}
	t.Fatalf("timeout esperando %s | %s: items=%s, esperaba %d", pk, sk, lastItems, want)
}
