package outbox

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/rcastellor/rcm-outbox/infra/internal/config"
	"github.com/rcastellor/rcm-outbox/infra/internal/platform"
)

// dispatcherFunctionName es el nombre físico de la Lambda; fijo para poder
// invocarla directamente por nombre.
const dispatcherFunctionName = "rcm-outbox-orders-dispatcher"

// DispatcherArgs define los parámetros del componente Dispatcher.
type DispatcherArgs struct {
	// DBSecretARN es el ARN del secreto de Secrets Manager con las credenciales
	// de PostgreSQL; se inyecta como variable de entorno en la Lambda.
	DBSecretARN pulumi.StringInput
	// DispatchQueueURL es la URL de la cola SQS de dispatch donde el dispatcher
	// encola los trabajos de outbox.
	DispatchQueueURL pulumi.StringInput
	// DispatchQueueARN es el ARN de la cola SQS de dispatch; se usa en la
	// política IAM de envío.
	DispatchQueueARN pulumi.StringInput
	// BatchSize es el tamaño de bloque de registros que procesa cada worker; el
	// dispatcher lo usa para calcular cuántos workers son necesarios.
	BatchSize int
	// MaxWorkers es el tope de instancias de worker que el dispatcher lanza por
	// invocación.
	MaxWorkers int
}

// Dispatcher agrupa la función Lambda del dispatcher y su rol de ejecución.
type Dispatcher struct {
	pulumi.ResourceState

	Function    *platform.Function
	FunctionARN pulumi.StringOutput `pulumi:"functionARN"`
}

// NewDispatcher crea la función Lambda del dispatcher con su rol IAM.
func NewDispatcher(ctx *pulumi.Context, name string, args *DispatcherArgs, opts ...pulumi.ResourceOption) (*Dispatcher, error) {
	c := &Dispatcher{}
	if err := ctx.RegisterComponentResource("rcm-outbox:outbox:Dispatcher", name, c, opts...); err != nil {
		return nil, err
	}
	if args == nil {
		args = &DispatcherArgs{}
	}
	applyDispatcherDefaults(args)

	timeout := 30
	fn, err := platform.NewFunction(ctx, "dispatcher", &platform.FunctionArgs{
		Name:   dispatcherFunctionName,
		Binary: "../bin/orders-dispatcher",
		Env: pulumi.StringMap{
			"DB_SECRET_ARN":      args.DBSecretARN,
			"DISPATCH_QUEUE_URL": args.DispatchQueueURL,
			"BATCH_SIZE":         pulumi.Sprintf("%d", args.BatchSize),
			"MAX_WORKERS":        pulumi.Sprintf("%d", args.MaxWorkers),
		},
		Timeout:     &timeout,
		DBSecretARN: args.DBSecretARN,
		ExtraPolicies: []platform.ExtraPolicy{
			{Name: "dispatch-send", Actions: []string{"sqs:SendMessage"}, Resources: []pulumi.StringInput{args.DispatchQueueARN}},
		},
	}, pulumi.Parent(c))
	if err != nil {
		return nil, err
	}

	c.Function = fn
	c.FunctionARN = fn.FunctionARN

	if err := ctx.RegisterResourceOutputs(c, pulumi.Map{
		"functionARN": c.FunctionARN,
	}); err != nil {
		return nil, err
	}
	return c, nil
}

func applyDispatcherDefaults(args *DispatcherArgs) {
	if args.BatchSize == 0 {
		args.BatchSize = config.DefaultBatchSize
	}
	if args.MaxWorkers == 0 {
		args.MaxWorkers = config.DefaultMaxWorkers
	}
}
