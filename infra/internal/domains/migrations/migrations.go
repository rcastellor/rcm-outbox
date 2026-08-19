// Package migrations despliega la Lambda que aplica las migraciones SQL sobre
// la base de datos. El SQL va embebido en el binario (rcm-migrations) y se
// ejecuta al invocar la función manualmente (make migrate).
package migrations

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/rcastellor/rcm-outbox/infra/internal/platform"
)

// functionName es el nombre físico de la Lambda; fijo para poder invocarla
// directamente por nombre.
const functionName = "rcm-migrations"

// MigrationsArgs define los parámetros del componente Migrations.
type MigrationsArgs struct {
	// DBSecretARN es el ARN del secreto de Secrets Manager con las credenciales
	// de PostgreSQL; se inyecta como variable de entorno en la Lambda.
	DBSecretARN pulumi.StringInput
}

// Migrations agrupa la función Lambda rcm-migrations con su rol de ejecución
// y el permiso de lectura del secreto de la base de datos.
type Migrations struct {
	pulumi.ResourceState

	FunctionName pulumi.StringOutput `pulumi:"functionName"`
	FunctionARN  pulumi.StringOutput `pulumi:"functionARN"`
}

// NewMigrations crea la función Lambda de migraciones. El timeout es alto para
// cubrir migraciones largas como los CREATE INDEX CONCURRENTLY.
func NewMigrations(ctx *pulumi.Context, name string, args *MigrationsArgs, opts ...pulumi.ResourceOption) (*Migrations, error) {
	c := &Migrations{}
	if err := ctx.RegisterComponentResource("rcm-outbox:migrations:Migrations", name, c, opts...); err != nil {
		return nil, err
	}
	if args == nil {
		args = &MigrationsArgs{}
	}

	timeout := 300
	fn, err := platform.NewFunction(ctx, "function", &platform.FunctionArgs{
		Name:        functionName,
		Binary:      "../bin/rcm-migrations",
		Env:         pulumi.StringMap{"DB_SECRET_ARN": args.DBSecretARN},
		Timeout:     &timeout,
		DBSecretARN: args.DBSecretARN,
	}, pulumi.Parent(c))
	if err != nil {
		return nil, err
	}

	c.FunctionName = fn.Function.Name
	c.FunctionARN = fn.FunctionARN

	if err := ctx.RegisterResourceOutputs(c, pulumi.Map{
		"functionName": c.FunctionName,
		"functionARN":  c.FunctionARN,
	}); err != nil {
		return nil, err
	}
	return c, nil
}
