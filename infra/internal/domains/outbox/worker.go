package outbox

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/rcastellor/rcm-outbox/infra/internal/config"
	"github.com/rcastellor/rcm-outbox/infra/internal/platform"
)

// workerFunctionName es el nombre físico de la Lambda; fijo para poder
// invocarla directamente por nombre.
const workerFunctionName = "rcm-outbox-orders-worker"

// WorkerArgs define los parámetros del componente Worker.
type WorkerArgs struct {
	// DBSecretARN es el ARN del secreto de Secrets Manager con las credenciales
	// de PostgreSQL; se inyecta como variable de entorno en la Lambda.
	DBSecretARN pulumi.StringInput
	// SNSTopicARN es el ARN del topic SNS FIFO donde se publican los eventos del
	// outbox. Se usa tanto en la política IAM como en la variable de entorno.
	SNSTopicARN pulumi.StringInput
	// DispatchQueueARN es el ARN de la cola SQS de dispatch que dispara la
	// Lambda mediante un event source mapping (BatchSize=1).
	DispatchQueueARN pulumi.StringInput
	// BatchSize es el tamaño de bloque de registros que procesa cada invocación.
	BatchSize int
	// MaxWorkers es el máximo de invocaciones concurrentes que la Lambda puede
	// alojar (ReservedConcurrentExecutions), limitando el fan-out del dispatcher.
	MaxWorkers int
	// MaxAttempts es el número máximo de intentos de publicación antes de
	// marcar un registro como dead.
	MaxAttempts int
	// BackoffBaseSeconds es la base (en segundos) del backoff exponencial.
	BackoffBaseSeconds int
	// MaxBackoffSeconds es el tope (en segundos) del backoff exponencial.
	MaxBackoffSeconds int
}

// Worker agrupa la función Lambda del worker de outbox, su rol de ejecución y
// el event source mapping que la conecta a la cola de dispatch.
type Worker struct {
	pulumi.ResourceState

	FunctionARN pulumi.StringOutput `pulumi:"functionARN"`
}

// NewWorker crea la función Lambda del worker con su rol IAM y el event source
// mapping desde la cola SQS de dispatch.
func NewWorker(ctx *pulumi.Context, name string, args *WorkerArgs, opts ...pulumi.ResourceOption) (*Worker, error) {
	c := &Worker{}
	if err := ctx.RegisterComponentResource("rcm-outbox:outbox:Worker", name, c, opts...); err != nil {
		return nil, err
	}
	if args == nil {
		args = &WorkerArgs{}
	}
	applyDefaults(args)

	timeout := 30
	fn, err := platform.NewFunction(ctx, "worker", &platform.FunctionArgs{
		Name:   workerFunctionName,
		Binary: "../bin/orders-workers",
		Env: pulumi.StringMap{
			"DB_SECRET_ARN":        args.DBSecretARN,
			"SNS_TOPIC_ARN":        args.SNSTopicARN,
			"BATCH_SIZE":           pulumi.Sprintf("%d", args.BatchSize),
			"MAX_ATTEMPTS":         pulumi.Sprintf("%d", args.MaxAttempts),
			"BACKOFF_BASE_SECONDS": pulumi.Sprintf("%d", args.BackoffBaseSeconds),
			"MAX_BACKOFF_SECONDS":  pulumi.Sprintf("%d", args.MaxBackoffSeconds),
		},
		Timeout:                      &timeout,
		ReservedConcurrentExecutions: &args.MaxWorkers,
		DBSecretARN:                  args.DBSecretARN,
		ExtraPolicies: []platform.ExtraPolicy{
			{Name: "sns-publish", Actions: []string{"sns:Publish"}, Resources: []pulumi.StringInput{args.SNSTopicARN}},
			{Name: "dispatch-consume", Actions: []string{"sqs:ReceiveMessage", "sqs:DeleteMessage", "sqs:GetQueueAttributes"}, Resources: []pulumi.StringInput{args.DispatchQueueARN}},
		},
		EventSourceARN: args.DispatchQueueARN,
	}, pulumi.Parent(c))
	if err != nil {
		return nil, err
	}

	c.FunctionARN = fn.FunctionARN

	if err := ctx.RegisterResourceOutputs(c, pulumi.Map{
		"functionARN": c.FunctionARN,
	}); err != nil {
		return nil, err
	}
	return c, nil
}

func applyDefaults(args *WorkerArgs) {
	if args.BatchSize == 0 {
		args.BatchSize = config.DefaultBatchSize
	}
	if args.MaxWorkers == 0 {
		args.MaxWorkers = config.DefaultMaxWorkers
	}
	if args.MaxAttempts == 0 {
		args.MaxAttempts = config.DefaultMaxAttempts
	}
	if args.BackoffBaseSeconds == 0 {
		args.BackoffBaseSeconds = config.DefaultBackoffBaseSeconds
	}
	if args.MaxBackoffSeconds == 0 {
		args.MaxBackoffSeconds = config.DefaultMaxBackoffSeconds
	}
}
