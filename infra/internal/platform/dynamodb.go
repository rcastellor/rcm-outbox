package platform

import (
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/dynamodb"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// TableArgs define los parámetros del componente Table (DynamoDB).
type TableArgs struct {
	// Name es el nombre de la tabla.
	Name string
	// HashKey es el nombre del atributo de clave de partición (tipo String).
	HashKey string
	// RangeKey es el nombre del atributo de clave de ordenación (tipo String);
	// vacío significa tabla solo con clave de partición.
	RangeKey string
}

// Table es una tabla DynamoDB on-demand con claves de tipo String.
type Table struct {
	pulumi.ResourceState

	// TableName es el nombre físico de la tabla.
	TableName pulumi.StringOutput `pulumi:"tableName"`
	// TableARN es el ARN de la tabla.
	TableARN pulumi.StringOutput `pulumi:"tableARN"`
}

// NewTable crea una tabla DynamoDB con capacidad bajo demanda (PAY_PER_REQUEST).
func NewTable(ctx *pulumi.Context, name string, args *TableArgs, opts ...pulumi.ResourceOption) (*Table, error) {
	c := &Table{}
	if err := ctx.RegisterComponentResource("rcm-outbox:dynamodb:Table", name, c, opts...); err != nil {
		return nil, err
	}
	if args == nil {
		args = &TableArgs{}
	}

	attributes := dynamodb.TableAttributeArray{
		dynamodb.TableAttributeArgs{Name: pulumi.String(args.HashKey), Type: pulumi.String("S")},
	}
	if args.RangeKey != "" {
		attributes = append(attributes, dynamodb.TableAttributeArgs{
			Name: pulumi.String(args.RangeKey), Type: pulumi.String("S"),
		})
	}

	tableArgs := &dynamodb.TableArgs{
		Name:        pulumi.String(args.Name),
		HashKey:     pulumi.String(args.HashKey),
		BillingMode: pulumi.String("PAY_PER_REQUEST"),
		Attributes:  attributes,
	}
	if args.RangeKey != "" {
		tableArgs.RangeKey = pulumi.String(args.RangeKey)
	}

	table, err := dynamodb.NewTable(ctx, args.Name, tableArgs, pulumi.Parent(c))
	if err != nil {
		return nil, err
	}

	c.TableName = table.Name
	c.TableARN = table.Arn

	if err := ctx.RegisterResourceOutputs(c, pulumi.Map{
		"tableName": c.TableName,
		"tableARN":  c.TableARN,
	}); err != nil {
		return nil, err
	}
	return c, nil
}
