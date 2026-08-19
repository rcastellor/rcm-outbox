package queue

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// maxBatchSize es el máximo de mensajes por llamada a SendMessageBatch de SQS.
const maxBatchSize = 10

// Publisher encola mensajes de trabajo en la cola SQS de dispatch.
type Publisher struct {
	client   *sqs.Client
	queueURL string
}

// New crea el publicador de mensajes de dispatch.
func New(client *sqs.Client, queueURL string) *Publisher {
	return &Publisher{client: client, queueURL: queueURL}
}

// SendBatch encola n mensajes de trabajo (uno por worker) en la cola SQS, en
// bloques de hasta 10 mensajes por llamada.
func (p *Publisher) SendBatch(ctx context.Context, n int) error {
	for sent := 0; sent < n; sent += maxBatchSize {
		count := min(maxBatchSize, n-sent)
		entries := make([]types.SendMessageBatchRequestEntry, count)
		for i := range count {
			entries[i] = types.SendMessageBatchRequestEntry{
				Id:          aws.String(fmt.Sprintf("work-%d", sent+i)),
				MessageBody: aws.String("{}"),
			}
		}

		if _, err := p.client.SendMessageBatch(ctx, &sqs.SendMessageBatchInput{
			QueueUrl: aws.String(p.queueURL),
			Entries:  entries,
		}); err != nil {
			return fmt.Errorf("encolando %d trabajos en la cola de dispatch: %w", count, err)
		}
	}
	return nil
}
