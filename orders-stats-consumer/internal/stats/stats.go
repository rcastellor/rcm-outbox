// Package stats agrega en DynamoDB el número de items comprados por cliente,
// producto y día a partir de los eventos de órdenes publicados en SNS.
//
// Para que el agregado sea exactamente-una-vez (aunque SQS redelivery o el
// redrive de la DLQ reenvíen el mismo mensaje), aplica el patrón inbox: cada
// evento procesado se registra en una tabla de deduplicación dentro de la MISMA
// transacción DynamoDB que el incremento del contador. Si el evento ya estaba
// registrado, la transacción se anula y el duplicado se descarta.
package stats

import (
	"context"
	"encoding/json"
	"errors"
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

// inboxTTL es el tiempo de retención de las entradas del inbox: transcurrido
// este plazo, DynamoDB elimina el registro y el evento podría volver a
// procesarse si reaparece (suficientemente amplio para la ventana de
// deduplicación real del pipeline).
const inboxTTL = 30 * 24 * time.Hour

// cancellationConditionalCheckFailed es el código con el que DynamoDB informa
// que la condición de una operación dentro de una transacción falló; en el
// inbox indica que el evento ya estaba procesado (duplicado).
const cancellationConditionalCheckFailed = "ConditionalCheckFailed"

// transactor abstrae la escritura transaccional de DynamoDB para poder probar
// el agregador con un fake.
type transactor interface {
	TransactWriteItems(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error)
}

// Aggregator incrementa los contadores de items comprados en la tabla de
// estadísticas, deduplicando eventos vía la tabla de inbox.
type Aggregator struct {
	client     transactor
	table      string
	inboxTable string
	logger     *slog.Logger
}

// New crea el agregador sobre las tablas indicadas.
func New(client transactor, table, inboxTable string, logger *slog.Logger) *Aggregator {
	return &Aggregator{client: client, table: table, inboxTable: inboxTable, logger: logger}
}

// envelope es el formato del mensaje recibido vía SNS (raw message delivery):
// el id del evento (clave de deduplicación), su tipo y el payload original del
// outbox.
type envelope struct {
	ID        string          `json:"id"`
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
// registra el evento en el inbox y, en la misma transacción, incrementa los
// contadores; si el evento ya fue procesado (duplicado) se descarta sin error.
// Otros eventos se ignoran sin efecto. Si falla la escritura en DynamoDB se
// devuelve el error para que SQS reintente el mensaje.
func (a *Aggregator) Process(ctx context.Context, body []byte) error {
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("deserializando mensaje: %w", err)
	}

	if env.EventType != eventCreatedOrder {
		a.logger.Info("evento ignorado", "event_type", env.EventType)
		return nil
	}

	if env.ID == "" {
		return errors.New("evento CreatedOrder sin id: no se puede deduplicar")
	}

	return a.addOrder(ctx, env.ID, env.Payload)
}

// addOrder registra el evento en el inbox y suma los items de cada línea de la
// orden al contador diario del par cliente/producto, todo en una única
// transacción DynamoDB. La clave diaria usa la fecha UTC de creación de la
// orden, correcta aunque el procesamiento llegue con retraso.
func (a *Aggregator) addOrder(ctx context.Context, eventID string, payload json.RawMessage) error {
	var o order
	if err := json.Unmarshal(payload, &o); err != nil {
		return fmt.Errorf("deserializando orden: %w", err)
	}
	if o.CustomerID == "" || len(o.Lines) == 0 {
		return fmt.Errorf("orden inválida: customerId=%q lines=%d", o.CustomerID, len(o.Lines))
	}

	day := o.CreatedAt.UTC().Format("2006-01-02")

	// La transacción no admite dos operaciones sobre el mismo item: se suman las
	// cantidades por producto para emitir un único Update por par (cliente,
	// producto, día).
	quantities := make(map[string]int)
	var products []string
	for _, l := range o.Lines {
		if _, ok := quantities[l.ProductID]; !ok {
			products = append(products, l.ProductID)
		}
		quantities[l.ProductID] += l.Quantity
	}

	now := time.Now()
	items := make([]types.TransactWriteItem, 0, 1+len(products))

	// Put del inbox: si el id ya existe (attribute_not_exists falla) la
	// transacción se anula y el evento se trata como duplicado.
	items = append(items, types.TransactWriteItem{
		Put: &types.Put{
			TableName: aws.String(a.inboxTable),
			Item: map[string]types.AttributeValue{
				"pk":          &types.AttributeValueMemberS{Value: eventID},
				"processedAt": &types.AttributeValueMemberN{Value: strconv.FormatInt(now.Unix(), 10)},
				"expiresAt":   &types.AttributeValueMemberN{Value: strconv.FormatInt(now.Add(inboxTTL).Unix(), 10)},
			},
			ConditionExpression: aws.String("attribute_not_exists(pk)"),
		},
	})

	for _, productID := range products {
		quantity := quantities[productID]
		pk := fmt.Sprintf("CUSTOMER#%s#%s", o.CustomerID, day)
		sk := fmt.Sprintf("PRODUCT#%s", productID)
		items = append(items, types.TransactWriteItem{
			Update: &types.Update{
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
					":quantity": &types.AttributeValueMemberN{Value: strconv.Itoa(quantity)},
				},
			},
		})
	}

	if _, err := a.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: items,
	}); err != nil {
		if isDuplicate(err) {
			a.logger.Info("evento duplicado ignorado", "event_id", eventID)
			return nil
		}
		return fmt.Errorf("aplicando evento %s: %w", eventID, err)
	}

	for _, productID := range products {
		a.logger.Info("items agregados", "event_id", eventID, "customer_id", o.CustomerID, "product_id", productID, "quantity", quantities[productID], "day", day)
	}
	return nil
}

// isDuplicate informa si la transacción se canceló únicamente porque el evento
// ya existía en el inbox (duplicado). Cualquier otra causa de cancelación o
// error se trata como fallo transitorio para reintentar.
func isDuplicate(err error) bool {
	var canceled *types.TransactionCanceledException
	if !errors.As(err, &canceled) {
		return false
	}
	// La operación del inbox es la primera de la transacción; si su condición
	// falló es un duplicado (los Updates usan ADD y no llevan condición).
	if len(canceled.CancellationReasons) == 0 {
		return false
	}
	return aws.ToString(canceled.CancellationReasons[0].Code) == cancellationConditionalCheckFailed
}
