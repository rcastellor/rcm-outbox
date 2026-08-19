package publisher

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"

	"github.com/rcastellor/rcm-outbox/orders-workers/internal/domain"
)

// Publisher publica los eventos del outbox en el topic SNS.
type Publisher struct {
	client   *sns.Client
	topicARN string
}

// New crea el publicador de eventos SNS.
func New(client *sns.Client, topicARN string) *Publisher {
	return &Publisher{client: client, topicARN: topicARN}
}

// envelope es el formato del mensaje publicado en SNS: el tipo de evento más el
// payload original del outbox.
type envelope struct {
	EventType string          `json:"eventType"`
	Payload   json.RawMessage `json:"payload"`
}

// Publish publica un evento del outbox en el topic SNS FIFO. Usa el
// aggregate_id como MessageGroupId para preservar el orden por aggregate. El
// topic tiene deduplicación por contenido, por lo que no se envía
// MessageDeduplicationId.
func (p *Publisher) Publish(ctx context.Context, evt domain.OutboxEvent) error {
	msg, err := json.Marshal(envelope{
		EventType: evt.EventType,
		Payload:   json.RawMessage(evt.Payload),
	})
	if err != nil {
		return fmt.Errorf("serializando evento %s: %w", evt.EventType, err)
	}

	if _, err := p.client.Publish(ctx, &sns.PublishInput{
		TopicArn:       aws.String(p.topicARN),
		Message:        aws.String(string(msg)),
		MessageGroupId: aws.String(evt.AggregateID),
	}); err != nil {
		return fmt.Errorf("publicando evento %s en SNS: %w", evt.EventType, err)
	}
	return nil
}
