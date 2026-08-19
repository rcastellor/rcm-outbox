// Package stats agrega en DynamoDB el número de items comprados por cliente,
// producto y día a partir de los eventos de órdenes publicados en SNS.
package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// eventCreatedOrder es el único evento que actualiza las estadísticas: solo
// las compras nuevas suman items. El resto de eventos se registran y se
// confirman sin efecto para no envenenar la cola.
const eventCreatedOrder = "CreatedOrder"

// Aggregator incrementa los contadores de items comprados en la tabla de
// estadísticas de DynamoDB.
type Aggregator struct {
	client *dynamodb.Client
	table  string
	logger *slog.Logger
}

// New crea el agregador sobre la tabla indicada.
func New(client *dynamodb.Client, table string, logger *slog.Logger) *Aggregator {
	return &Aggregator{client: client, table: table, logger: logger}
}

// envelope es el formato del mensaje recibido vía SNS (raw message delivery):
// el tipo de evento más el payload original del outbox.
type envelope struct {
	EventType string          `json:"eventType"`
	Payload   json.RawMessage `json:"payload"`
}

// order es el subconjunto del payload de una orden necesario para agregar:
// cliente, líneas y fecha de creación (fuente del día en la PK).
type order struct {
	CustomerID string    `json:"customerId"`
	Lines      []line    `json:"lines"`
	CreatedAt  time.Time `json:"createdAt"`
}

// line es una línea de la orden con el producto y los items comprados.
type line struct {
	ProductID string `json:"productId"`
	Quantity  int    `json:"quantity"`
}

// Process procesa un mensaje de la cola de consumidores. Para CreatedOrder
// incrementa el contador de cada línea; otros eventos se ignoran sin error.
// Si falla la escritura en DynamoDB se devuelve el error para que SQS
// reintente el mensaje.
func (a *Aggregator) Process(ctx context.Context, body []byte) error {
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("deserializando mensaje: %w", err)
	}

	if env.EventType != eventCreatedOrder {
		a.logger.Info("evento ignorado", "event_type", env.EventType)
		return nil
	}
	return a.addOrder(ctx, env.Payload)
}

// addOrder suma los items de cada línea de la orden al contador diario del
// par cliente/producto. La clave diaria usa la fecha UTC de creación de la
// orden, correcta aunque el procesamiento llegue con retraso.
func (a *Aggregator) addOrder(ctx context.Context, payload json.RawMessage) error {
	var o order
	if err := json.Unmarshal(payload, &o); err != nil {
		return fmt.Errorf("deserializando orden: %w", err)
	}
	if o.CustomerID == "" || len(o.Lines) == 0 {
		return fmt.Errorf("orden inválida: customerId=%q lines=%d", o.CustomerID, len(o.Lines))
	}

	day := o.CreatedAt.UTC().Format("2006-01-02")
	for _, l := range o.Lines {
		pk := fmt.Sprintf("CUSTOMER#%s#%s", o.CustomerID, day)
		sk := fmt.Sprintf("PRODUCT#%s", l.ProductID)
		if _, err := a.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
			TableName: aws.String(a.table),
			Key: map[string]types.AttributeValue{
				"pk": &types.AttributeValueMemberS{Value: pk},
				"sk": &types.AttributeValueMemberS{Value: sk},
			},
			UpdateExpression: aws.String("ADD #items :quantity"),
			// "items" es palabra reservada de DynamoDB y requiere alias.
			ExpressionAttributeNames: map[string]string{
				"#items": "items",
			},
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":quantity": &types.AttributeValueMemberN{Value: strconv.Itoa(l.Quantity)},
			},
		}); err != nil {
			return fmt.Errorf("incrementando items de %s/%s: %w", pk, sk, err)
		}
		a.logger.Info("items agregados", "customer_id", o.CustomerID, "product_id", l.ProductID, "quantity", l.Quantity, "day", day)
	}
	return nil
}
