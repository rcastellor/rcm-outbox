// Package stats despliega el dominio de estadísticas: la cola SQS suscrita al
// topic de órdenes, la tabla DynamoDB de agregación y la Lambda consumidora
// que suma los items comprados por cliente, producto y día.
package stats

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/rcastellor/rcm-outbox/infra/internal/config"
	"github.com/rcastellor/rcm-outbox/infra/internal/platform"
)

// functionName es el nombre físico de la Lambda; fijo para poder invocarla
// directamente por nombre.
const functionName = "rcm-outbox-orders-stats-consumer"

// StatsArgs define los parámetros del componente Stats.
type StatsArgs struct {
	// TopicARN es el ARN del topic SNS FIFO de órdenes al que se suscribe la
	// cola de consumidores.
	TopicARN pulumi.StringInput
}

// Stats agrupa la cola de consumidores, la tabla de estadísticas y la Lambda
// consumidora con su event source mapping.
type Stats struct {
	pulumi.ResourceState

	// TableName es el nombre de la tabla DynamoDB de estadísticas.
	TableName pulumi.StringOutput `pulumi:"tableName"`
	// TableARN es el ARN de la tabla DynamoDB de estadísticas.
	TableARN pulumi.StringOutput `pulumi:"tableARN"`
	// QueueARN es el ARN de la cola SQS de consumidores.
	QueueARN pulumi.StringOutput `pulumi:"queueARN"`
	// FunctionARN es el ARN de la Lambda consumidora.
	FunctionARN pulumi.StringOutput `pulumi:"functionARN"`
}

// NewStats crea la infraestructura del dominio de estadísticas.
func NewStats(ctx *pulumi.Context, name string, args *StatsArgs, opts ...pulumi.ResourceOption) (*Stats, error) {
	c := &Stats{}
	if err := ctx.RegisterComponentResource("rcm-outbox:stats:Stats", name, c, opts...); err != nil {
		return nil, err
	}
	if args == nil {
		args = &StatsArgs{}
	}

	queue, err := platform.NewSQS(ctx, "queue", &platform.SQSArgs{
		QueueName:       config.DefaultStatsQueueName,
		TopicARN:        args.TopicARN,
		DLQName:         config.DefaultStatsDLQName,
		MaxReceiveCount: config.DefaultMaxReceiveCount,
	}, pulumi.Parent(c))
	if err != nil {
		return nil, err
	}

	table, err := platform.NewTable(ctx, "table", &platform.TableArgs{
		Name:     config.DefaultStatsTableName,
		HashKey:  "pk",
		RangeKey: "sk",
	}, pulumi.Parent(c))
	if err != nil {
		return nil, err
	}

	fn, err := platform.NewFunction(ctx, "consumer", &platform.FunctionArgs{
		Name:   functionName,
		Binary: "../bin/orders-stats-consumer",
		Env: pulumi.StringMap{
			"STATS_TABLE_NAME": table.TableName,
		},
		EventSourceARN: queue.QueueARN,
		ExtraPolicies: []platform.ExtraPolicy{
			{
				Name:      "stats-update",
				Actions:   []string{"dynamodb:UpdateItem"},
				Resources: []pulumi.StringInput{table.TableARN},
			},
		},
	}, pulumi.Parent(c))
	if err != nil {
		return nil, err
	}

	c.TableName = table.TableName
	c.TableARN = table.TableARN
	c.QueueARN = queue.QueueARN
	c.FunctionARN = fn.FunctionARN

	if err := ctx.RegisterResourceOutputs(c, pulumi.Map{
		"tableName":   c.TableName,
		"tableARN":    c.TableARN,
		"queueARN":    c.QueueARN,
		"functionARN": c.FunctionARN,
	}); err != nil {
		return nil, err
	}
	return c, nil
}
