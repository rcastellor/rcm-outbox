package platform

import (
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/sns"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/sqs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/rcastellor/rcm-outbox/infra/internal/config"
)

// SQSArgs define los parámetros del componente SQS.
type SQSArgs struct {
	// QueueName es el nombre de la cola SQS FIFO (debe terminar en ".fifo").
	QueueName string
	// TopicARN es el ARN del topic SNS FIFO al que se suscribe la cola.
	TopicARN pulumi.StringInput
	// DLQName es el nombre de la cola dead-letter asociada; si está vacío no se
	// crea DLQ ni redrive policy.
	DLQName string
	// MaxReceiveCount es el número de recibos fallidos antes de que la redrive
	// policy mueva un mensaje a la DLQ; 0 usa config.DefaultMaxReceiveCount.
	MaxReceiveCount int
}

// SQS agrupa la cola SQS, su dead-letter queue con redrive policy y su
// suscripción al topic de outbox.
type SQS struct {
	pulumi.ResourceState

	// QueueARN es el ARN de la cola SQS.
	QueueARN pulumi.StringOutput `pulumi:"queueARN"`
	// DLQueueARN es el ARN de la cola dead-letter; vacío si no hay DLQ.
	DLQueueARN pulumi.StringOutput `pulumi:"dlqARN"`
}

// NewSQS crea una cola SQS FIFO suscrita al topic SNS indicado y, si se
// configura, su DLQ FIFO con la redrive policy correspondiente.
func NewSQS(ctx *pulumi.Context, name string, args *SQSArgs, opts ...pulumi.ResourceOption) (*SQS, error) {
	c := &SQS{}
	if err := ctx.RegisterComponentResource("rcm-outbox:messaging:SQS", name, c, opts...); err != nil {
		return nil, err
	}
	if args == nil {
		args = &SQSArgs{}
	}

	queueArgs := &sqs.QueueArgs{
		Name:      pulumi.String(args.QueueName),
		FifoQueue: pulumi.BoolPtr(true),
	}
	if args.DLQName != "" {
		dlq, err := newDLQ(ctx, c, args.DLQName, true)
		if err != nil {
			return nil, err
		}
		maxReceiveCount := args.MaxReceiveCount
		if maxReceiveCount == 0 {
			maxReceiveCount = config.DefaultMaxReceiveCount
		}
		queueArgs.RedrivePolicy = pulumi.Sprintf(
			`{"deadLetterTargetArn":%q,"maxReceiveCount":%d}`, dlq.Arn, maxReceiveCount)
		c.DLQueueARN = dlq.Arn
	}

	queue, err := sqs.NewQueue(ctx, args.QueueName, queueArgs, pulumi.Parent(c))
	if err != nil {
		return nil, err
	}

	if args.TopicARN != nil {
		// Permite al topic SNS publicar en la cola.
		policy := iam.GetPolicyDocumentOutput(ctx, iam.GetPolicyDocumentOutputArgs{
			Statements: iam.GetPolicyDocumentStatementArray{
				iam.GetPolicyDocumentStatementArgs{
					Effect:  pulumi.StringPtr("Allow"),
					Actions: pulumi.StringArray{pulumi.String("sqs:SendMessage")},
					Principals: iam.GetPolicyDocumentStatementPrincipalArray{
						iam.GetPolicyDocumentStatementPrincipalArgs{
							Type:        pulumi.String("AWS"),
							Identifiers: pulumi.StringArray{args.TopicARN},
						},
					},
					Conditions: iam.GetPolicyDocumentStatementConditionArray{
						iam.GetPolicyDocumentStatementConditionArgs{
							Test:     pulumi.String("ArnEquals"),
							Variable: pulumi.String("aws:SourceArn"),
							Values:   pulumi.StringArray{args.TopicARN},
						},
					},
					Resources: pulumi.StringArray{queue.Arn},
				},
			},
		})

		_, err = sqs.NewQueuePolicy(ctx, args.QueueName+"-policy", &sqs.QueuePolicyArgs{
			QueueUrl: queue.Url,
			Policy:   policy.Json(),
		}, pulumi.Parent(c))
		if err != nil {
			return nil, err
		}

		_, err = sns.NewTopicSubscription(ctx, args.QueueName+"-subscription", &sns.TopicSubscriptionArgs{
			Topic:              args.TopicARN,
			Protocol:           pulumi.String("sqs"),
			Endpoint:           queue.Arn,
			RawMessageDelivery: pulumi.BoolPtr(true),
		}, pulumi.Parent(c))
		if err != nil {
			return nil, err
		}
	}

	c.QueueARN = queue.Arn

	if err := ctx.RegisterResourceOutputs(c, pulumi.Map{
		"queueARN": c.QueueARN,
		"dlqARN":   c.DLQueueARN,
	}); err != nil {
		return nil, err
	}
	return c, nil
}

// newDLQ crea la cola dead-letter (FIFO si el origen lo es) como hija del
// componente indicado.
func newDLQ(ctx *pulumi.Context, parent pulumi.Resource, name string, fifo bool) (*sqs.Queue, error) {
	dlqArgs := &sqs.QueueArgs{
		Name: pulumi.String(name),
		// Message retention de 14 días para dar margen a investigar y hacer
		// redrive antes de perder los mensajes.
		MessageRetentionSeconds: pulumi.IntPtr(1209600),
	}
	if fifo {
		dlqArgs.FifoQueue = pulumi.BoolPtr(true)
	}
	return sqs.NewQueue(ctx, name, dlqArgs, pulumi.Parent(parent))
}
