package platform

import (
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	awslambda "github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lambda"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// assumeRolePolicy permite que Lambda asuma el rol de ejecución.
const assumeRolePolicy = `{
	"Version": "2012-10-17",
	"Statement": [{
		"Effect": "Allow",
		"Principal": {"Service": "lambda.amazonaws.com"},
		"Action": "sts:AssumeRole"
	}]
}`

// ExtraPolicy define una política IAM adicional que se adjunta al rol de
// ejecución de la función.
type ExtraPolicy struct {
	Name      string               // nombre lógico del recurso de política
	Actions   []string             // acciones IAM permitidas (p.ej. "sns:Publish")
	Resources []pulumi.StringInput // ARNs sobre los que aplica la política
}

// FunctionArgs define los parámetros del componente Function.
type FunctionArgs struct {
	// Name es el nombre físico de la función Lambda. Si está vacío, Pulumi
	// genera uno con sufijo aleatorio.
	Name string
	// Binary es la ruta al binario de la función (p.ej. "../bin/orders-api").
	Binary string
	// Env son las variables de entorno que se inyectan en la función.
	Env pulumi.StringMap
	// Timeout es el timeout de la función en segundos; si es nil se usa el
	// default de AWS.
	Timeout *int
	// ReservedConcurrentExecutions limita las invocaciones concurrentes; nil
	// significa sin reserva.
	ReservedConcurrentExecutions *int
	// DBSecretARN habilita la política secretsmanager:GetSecretValue sobre el
	// secreto indicado. Si es nil no se crea la política.
	DBSecretARN pulumi.StringInput
	// ExtraPolicies son políticas IAM adicionales para el rol de ejecución.
	ExtraPolicies []ExtraPolicy
	// EventSourceARN crea un event source mapping (BatchSize=1) desde esa cola
	// SQS hacia la función. Si es nil no se crea el mapping.
	EventSourceARN pulumi.StringInput
}

// Function agrupa una función Lambda con su rol de ejecución y sus permisos.
type Function struct {
	pulumi.ResourceState

	Function    *awslambda.Function
	FunctionARN pulumi.StringOutput
}

// NewFunction crea una función Lambda con su rol de ejecución, las políticas
// IAM indicadas y, opcionalmente, un event source mapping desde SQS.
func NewFunction(ctx *pulumi.Context, name string, args *FunctionArgs, opts ...pulumi.ResourceOption) (*Function, error) {
	c := &Function{}
	if err := ctx.RegisterComponentResource("rcm-outbox:lambda:Function", name, c, opts...); err != nil {
		return nil, err
	}
	if args == nil {
		args = &FunctionArgs{}
	}

	role, err := iam.NewRole(ctx, "role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(assumeRolePolicy),
	}, pulumi.Parent(c))
	if err != nil {
		return nil, err
	}

	if _, err := iam.NewRolePolicyAttachment(ctx, "execution", &iam.RolePolicyAttachmentArgs{
		Role:      role.Name,
		PolicyArn: pulumi.String("arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"),
	}, pulumi.Parent(role)); err != nil {
		return nil, err
	}

	if args.DBSecretARN != nil {
		policy := iam.GetPolicyDocumentOutput(ctx, iam.GetPolicyDocumentOutputArgs{
			Statements: iam.GetPolicyDocumentStatementArray{
				iam.GetPolicyDocumentStatementArgs{
					Effect:    pulumi.StringPtr("Allow"),
					Actions:   pulumi.StringArray{pulumi.String("secretsmanager:GetSecretValue")},
					Resources: pulumi.StringArray{args.DBSecretARN},
				},
			},
		})

		if _, err := iam.NewRolePolicy(ctx, "secrets", &iam.RolePolicyArgs{
			Role:   role.Name,
			Policy: policy.Json(),
		}, pulumi.Parent(role)); err != nil {
			return nil, err
		}
	}

	for _, p := range args.ExtraPolicies {
		policy := iam.GetPolicyDocumentOutput(ctx, iam.GetPolicyDocumentOutputArgs{
			Statements: iam.GetPolicyDocumentStatementArray{
				iam.GetPolicyDocumentStatementArgs{
					Effect:    pulumi.StringPtr("Allow"),
					Actions:   pulumi.ToStringArray(p.Actions),
					Resources: pulumi.StringArray(p.Resources),
				},
			},
		})

		if _, err := iam.NewRolePolicy(ctx, p.Name, &iam.RolePolicyArgs{
			Role:   role.Name,
			Policy: policy.Json(),
		}, pulumi.Parent(role)); err != nil {
			return nil, err
		}
	}

	fnArgs := &awslambda.FunctionArgs{
		Code:    pulumi.NewFileArchive(args.Binary),
		Runtime: pulumi.String("provided.al2023"),
		Handler: pulumi.String("bootstrap"),
		Role:    role.Arn,
		Environment: &awslambda.FunctionEnvironmentArgs{
			Variables: args.Env,
		},
	}
	if args.Name != "" {
		fnArgs.Name = pulumi.String(args.Name)
	}
	if args.Timeout != nil {
		fnArgs.Timeout = pulumi.IntPtr(*args.Timeout)
	}
	if args.ReservedConcurrentExecutions != nil {
		fnArgs.ReservedConcurrentExecutions = pulumi.IntPtr(*args.ReservedConcurrentExecutions)
	}

	fn, err := awslambda.NewFunction(ctx, "function", fnArgs, pulumi.Parent(c))
	if err != nil {
		return nil, err
	}

	if args.EventSourceARN != nil {
		if _, err := awslambda.NewEventSourceMapping(ctx, "event-source", &awslambda.EventSourceMappingArgs{
			EventSourceArn: args.EventSourceARN,
			FunctionName:   fn.Name,
			BatchSize:      pulumi.IntPtr(1),
			Enabled:        pulumi.BoolPtr(true),
			ScalingConfig: &awslambda.EventSourceMappingScalingConfigArgs{
				MaximumConcurrency: pulumi.IntPtr(5),
			},
		}, pulumi.Parent(c)); err != nil {
			return nil, err
		}
	}

	c.Function = fn
	c.FunctionARN = fn.Arn

	if err := ctx.RegisterResourceOutputs(c, pulumi.Map{
		"functionARN": c.FunctionARN,
	}); err != nil {
		return nil, err
	}
	return c, nil
}
