package outbox

import (
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/sqs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/rcastellor/rcm-outbox/infra/internal/config"
)

// QueueArgs define los parámetros del componente Queue.
type QueueArgs struct {
	// QueueName es el nombre de la cola SQS de dispatch.
	QueueName string
	// DLQName es el nombre de la cola dead-letter asociada; si está vacío no se
	// crea DLQ ni redrive policy.
	DLQName string
	// MaxReceiveCount es el número de recibos fallidos antes de que la redrive
	// policy mueva un mensaje a la DLQ; 0 usa config.DefaultMaxReceiveCount.
	MaxReceiveCount int
}

// Queue agrupa la cola SQS estándar donde el dispatcher encola los trabajos de
// outbox (un mensaje por worker) y su dead-letter queue con redrive policy.
type Queue struct {
	pulumi.ResourceState

	// QueueURL es la URL de la cola SQS.
	QueueURL pulumi.StringOutput `pulumi:"queueURL"`
	// QueueARN es el ARN de la cola SQS.
	QueueARN pulumi.StringOutput `pulumi:"queueARN"`
	// DLQueueURL es la URL de la cola dead-letter; vacía si no hay DLQ.
	DLQueueURL pulumi.StringOutput `pulumi:"dlqURL"`
	// DLQueueARN es el ARN de la cola dead-letter; vacío si no hay DLQ.
	DLQueueARN pulumi.StringOutput `pulumi:"dlqARN"`
}

// NewQueue crea una cola SQS estándar para los trabajos de dispatch y, si se
// configura, su DLQ con la redrive policy correspondiente.
func NewQueue(ctx *pulumi.Context, name string, args *QueueArgs, opts ...pulumi.ResourceOption) (*Queue, error) {
	c := &Queue{}
	if err := ctx.RegisterComponentResource("rcm-outbox:outbox:Queue", name, c, opts...); err != nil {
		return nil, err
	}
	if args == nil {
		args = &QueueArgs{}
	}

	queueArgs := &sqs.QueueArgs{
		Name: pulumi.String(args.QueueName),
	}
	if args.DLQName != "" {
		dlq, err := sqs.NewQueue(ctx, args.DLQName, &sqs.QueueArgs{
			Name: pulumi.String(args.DLQName),
			// Message retention de 14 días para dar margen a investigar y hacer
			// redrive antes de perder los mensajes.
			MessageRetentionSeconds: pulumi.IntPtr(1209600),
		}, pulumi.Parent(c))
		if err != nil {
			return nil, err
		}
		maxReceiveCount := args.MaxReceiveCount
		if maxReceiveCount == 0 {
			maxReceiveCount = config.DefaultMaxReceiveCount
		}
		queueArgs.RedrivePolicy = pulumi.Sprintf(
			`{"deadLetterTargetArn":%q,"maxReceiveCount":%d}`, dlq.Arn, maxReceiveCount)
		c.DLQueueURL = dlq.Url
		c.DLQueueARN = dlq.Arn
	}

	queue, err := sqs.NewQueue(ctx, args.QueueName, queueArgs, pulumi.Parent(c))
	if err != nil {
		return nil, err
	}

	c.QueueURL = queue.Url
	c.QueueARN = queue.Arn

	if err := ctx.RegisterResourceOutputs(c, pulumi.Map{
		"queueURL": c.QueueURL,
		"queueARN": c.QueueARN,
		"dlqURL":   c.DLQueueURL,
		"dlqARN":   c.DLQueueARN,
	}); err != nil {
		return nil, err
	}
	return c, nil
}
