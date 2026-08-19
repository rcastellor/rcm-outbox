package outbox

import (
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/cloudwatch"
	awslambda "github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lambda"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// defaultScheduleExpression es la frecuencia por defecto con la que EventBridge
// invoca al dispatcher. EventBridge/CloudWatch no soporta segundos: el mínimo es
// un minuto.
const defaultScheduleExpression = "rate(1 minute)"

// ScheduleArgs define los parámetros del componente Schedule.
type ScheduleArgs struct {
	// FunctionName es el nombre de la función Lambda del dispatcher.
	FunctionName pulumi.StringInput
	// FunctionARN es el ARN de la función Lambda del dispatcher.
	FunctionARN pulumi.StringInput
	// ScheduleExpression es la expresión de programación de EventBridge.
	ScheduleExpression string
}

// Schedule agrupa los recursos que disparan periódicamente al
// dispatcher: una única regla de EventBridge y el permiso para que EventBridge
// pueda invocar la Lambda.
type Schedule struct {
	pulumi.ResourceState

	Rule *cloudwatch.EventRule
}

// NewSchedule crea la regla de EventBridge que invoca al dispatcher.
func NewSchedule(ctx *pulumi.Context, name string, args *ScheduleArgs, opts ...pulumi.ResourceOption) (*Schedule, error) {
	c := &Schedule{}
	if err := ctx.RegisterComponentResource("rcm-outbox:outbox:Schedule", name, c, opts...); err != nil {
		return nil, err
	}
	if args == nil {
		args = &ScheduleArgs{}
	}
	if args.ScheduleExpression == "" {
		args.ScheduleExpression = defaultScheduleExpression
	}

	rule, err := cloudwatch.NewEventRule(ctx, "schedule", &cloudwatch.EventRuleArgs{
		Description:        pulumi.String("Dispara el dispatcher de outbox periódicamente"),
		ScheduleExpression: pulumi.String(args.ScheduleExpression),
	}, pulumi.Parent(c))
	if err != nil {
		return nil, err
	}

	if _, err := cloudwatch.NewEventTarget(ctx, "target", &cloudwatch.EventTargetArgs{
		Rule:     rule.Name,
		Arn:      args.FunctionARN,
		TargetId: pulumi.String("outbox-dispatcher"),
	}, pulumi.Parent(c)); err != nil {
		return nil, err
	}

	if _, err := awslambda.NewPermission(ctx, "schedule-invoke", &awslambda.PermissionArgs{
		Action:    pulumi.String("lambda:InvokeFunction"),
		Function:  args.FunctionName,
		Principal: pulumi.String("events.amazonaws.com"),
		SourceArn: rule.Arn,
	}, pulumi.Parent(c)); err != nil {
		return nil, err
	}

	c.Rule = rule

	if err := ctx.RegisterResourceOutputs(c, pulumi.Map{}); err != nil {
		return nil, err
	}
	return c, nil
}
