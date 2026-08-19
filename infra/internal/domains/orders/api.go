package orders

import (
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/apigatewayv2"
	awslambda "github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lambda"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/rcastellor/rcm-outbox/infra/internal/platform"
)

// functionName es el nombre físico de la Lambda; fijo para poder invocarla
// directamente por nombre.
const functionName = "rcm-outbox-orders-api"

// APIArgs define los parámetros del componente API.
type APIArgs struct {
	// DBSecretARN es el ARN del secreto de Secrets Manager con las credenciales
	// de PostgreSQL; se inyecta como variable de entorno en la Lambda.
	DBSecretARN pulumi.StringInput
}

// API agrupa la función Lambda de orders-api y el API Gateway HTTP (cualquier
// método sobre /orders y /orders/{proxy+}) integrado con ella.
type API struct {
	pulumi.ResourceState

	// URL es la URL pública del stage desplegado.
	URL pulumi.StringOutput
	// FunctionARN es el ARN de la función Lambda de orders-api.
	FunctionARN pulumi.StringOutput
}

// NewAPI crea la función Lambda de orders-api con su API Gateway HTTP.
func NewAPI(ctx *pulumi.Context, name string, args *APIArgs, opts ...pulumi.ResourceOption) (*API, error) {
	c := &API{}
	if err := ctx.RegisterComponentResource("rcm-outbox:orders:API", name, c, opts...); err != nil {
		return nil, err
	}
	if args == nil {
		args = &APIArgs{}
	}

	fn, err := platform.NewFunction(ctx, "api-function", &platform.FunctionArgs{
		Name:        functionName,
		Binary:      "../bin/orders-api",
		Env:         pulumi.StringMap{"DB_SECRET_ARN": args.DBSecretARN},
		DBSecretARN: args.DBSecretARN,
	}, pulumi.Parent(c))
	if err != nil {
		return nil, err
	}

	api, err := apigatewayv2.NewApi(ctx, "api", &apigatewayv2.ApiArgs{
		Name:         pulumi.String("orders-api"),
		ProtocolType: pulumi.String("HTTP"),
		Description:  pulumi.String("API de órdenes de compra (rcm-outbox)"),
	}, pulumi.Parent(c))
	if err != nil {
		return nil, err
	}

	integration, err := apigatewayv2.NewIntegration(ctx, "orders-integration", &apigatewayv2.IntegrationArgs{
		ApiId:                api.ID(),
		IntegrationType:      pulumi.String("AWS_PROXY"),
		IntegrationMethod:    pulumi.String("POST"),
		IntegrationUri:       fn.FunctionARN,
		PayloadFormatVersion: pulumi.String("2.0"),
	}, pulumi.Parent(c))
	if err != nil {
		return nil, err
	}

	target := pulumi.Sprintf("integrations/%s", integration.ID())
	routes := []struct {
		name     string
		routeKey string
	}{
		{name: "orders", routeKey: "ANY /orders"},
		{name: "orders-proxy", routeKey: "ANY /orders/{proxy+}"},
	}
	var routeResources []pulumi.Resource
	for _, r := range routes {
		route, err := apigatewayv2.NewRoute(ctx, r.name+"-route", &apigatewayv2.RouteArgs{
			ApiId:    api.ID(),
			RouteKey: pulumi.String(r.routeKey),
			Target:   target,
		}, pulumi.Parent(c))
		if err != nil {
			return nil, err
		}
		routeResources = append(routeResources, route)
	}

	if _, err := awslambda.NewPermission(ctx, "api-invoke", &awslambda.PermissionArgs{
		Action:    pulumi.String("lambda:InvokeFunction"),
		Function:  fn.Function.Name,
		Principal: pulumi.String("apigateway.amazonaws.com"),
		SourceArn: pulumi.Sprintf("%s/*/*", api.ExecutionArn),
	}, pulumi.Parent(c)); err != nil {
		return nil, err
	}

	deployment, err := apigatewayv2.NewDeployment(ctx, "deployment", &apigatewayv2.DeploymentArgs{
		ApiId: api.ID(),
	}, pulumi.Parent(c), pulumi.DependsOn(routeResources))
	if err != nil {
		return nil, err
	}

	stage, err := apigatewayv2.NewStage(ctx, "stage", &apigatewayv2.StageArgs{
		ApiId:        api.ID(),
		DeploymentId: deployment.ID(),
		Name:         pulumi.String("dev"),
	}, pulumi.Parent(c))
	if err != nil {
		return nil, err
	}

	c.URL = stage.InvokeUrl
	c.FunctionARN = fn.FunctionARN

	if err := ctx.RegisterResourceOutputs(c, pulumi.Map{
		"url":         c.URL,
		"functionARN": c.FunctionARN,
	}); err != nil {
		return nil, err
	}
	return c, nil
}
