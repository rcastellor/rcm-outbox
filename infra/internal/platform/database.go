package platform

import (
	"encoding/json"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/rds"
	"github.com/pulumi/pulumi-random/sdk/v4/go/random"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const (
	defaultEngineVersion    = "16.4"
	defaultInstanceClass    = "db.t4g.micro"
	defaultAllocatedStorage = 20
	defaultDbName           = "rcmoutbox"
	defaultUsername         = "rcmoutbox"
	defaultBackupRetention  = 7
	defaultPostgresPort     = 5432
)

// DatabaseArgs define los parámetros del componente Database.
type DatabaseArgs struct {
	// EngineVersion es la versión del motor PostgreSQL (p.ej. "16.4").
	EngineVersion string
	// InstanceClass es el tipo de instancia RDS (p.ej. "db.t4g.micro").
	InstanceClass string
	// AllocatedStorage es el almacenamiento inicial en GiB.
	AllocatedStorage int
	// DbName es el nombre de la base de datos inicial.
	DbName string
	// SubnetIDs son las subnets para el DB subnet group. Si están vacías se
	// usan las subnets de la VPC por defecto.
	SubnetIDs []string
	// SecurityGroupIDs son los security groups asociados a la instancia. Si está
	// vacío se crea uno con ingress en el puerto 5432 desde IngressCidrs.
	SecurityGroupIDs []string
	// IngressCidrs son los CIDRs de acceso al puerto 5432 cuando se crea el
	// security group automáticamente.
	IngressCidrs []string
	// MultiAz habilita el despliegue multi-AZ.
	MultiAz bool
	// PubliclyAccessible expone la instancia a internet.
	PubliclyAccessible bool
	// SkipFinalSnapshot evita el snapshot final al eliminar (útil en dev).
	SkipFinalSnapshot bool
	// FinalSnapshotIdentifier es el nombre del snapshot final que se crea al
	// eliminar la instancia cuando SkipFinalSnapshot es false. Si ambos están
	// vacíos, se omite el snapshot final (equivale a SkipFinalSnapshot=true).
	FinalSnapshotIdentifier string
	// BackupRetentionPeriod en días (0 desactiva los backups).
	BackupRetentionPeriod int
	// SecretsManager es el componente donde se guardan las credenciales.
	SecretsManager *SecretsManager
}

// dbCredentials es el documento JSON que se almacena en Secrets Manager.
type dbCredentials struct {
	Engine   string `json:"engine"`
	Username string `json:"username"`
	Password string `json:"password"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	DbName   string `json:"dbname"`
}

// Database agrupa los recursos de base de datos de la infraestructura.
type Database struct {
	pulumi.ResourceState

	Endpoint           pulumi.StringOutput `pulumi:"endpoint"`
	Port               pulumi.IntOutput    `pulumi:"port"`
	InstanceIdentifier pulumi.StringOutput `pulumi:"instanceIdentifier"`
	SecretARN          pulumi.StringOutput `pulumi:"secretARN"`
	ConnectionString   pulumi.StringOutput `pulumi:"connectionString"`
}

// NewDatabase crea el componente Database con una instancia RDS PostgreSQL.
func NewDatabase(ctx *pulumi.Context, name string, args *DatabaseArgs, opts ...pulumi.ResourceOption) (*Database, error) {
	c := &Database{}
	if err := ctx.RegisterComponentResource("rcm-outbox:database:Database", name, c, opts...); err != nil {
		return nil, err
	}

	if args == nil {
		args = &DatabaseArgs{}
	}
	applyDatabaseDefaults(args)

	subnetIDs, vpcID, err := resolveNetwork(ctx, args)
	if err != nil {
		return nil, err
	}

	subnetGroup, err := rds.NewSubnetGroup(ctx, "subnet-group", &rds.SubnetGroupArgs{
		SubnetIds: pulumi.ToStringArray(subnetIDs),
	}, pulumi.Parent(c))
	if err != nil {
		return nil, err
	}

	sgIDs, err := resolveSecurityGroupIDs(ctx, c, args, vpcID)
	if err != nil {
		return nil, err
	}

	password, err := random.NewRandomPassword(ctx, "password", &random.RandomPasswordArgs{
		Length:  pulumi.Int(32),
		Special: pulumi.BoolPtr(false),
	}, pulumi.Parent(c))
	if err != nil {
		return nil, err
	}

	instanceArgs := &rds.InstanceArgs{
		Identifier:            pulumi.String(name),
		Engine:                pulumi.String("postgres"),
		EngineVersion:         pulumi.StringPtr(args.EngineVersion),
		InstanceClass:         pulumi.String(args.InstanceClass),
		AllocatedStorage:      pulumi.IntPtr(args.AllocatedStorage),
		StorageEncrypted:      pulumi.BoolPtr(true),
		Username:              pulumi.StringPtr(defaultUsername),
		Password:              password.Result,
		DbName:                pulumi.StringPtr(args.DbName),
		DbSubnetGroupName:     subnetGroup.Name,
		VpcSecurityGroupIds:   sgIDs,
		MultiAz:               pulumi.BoolPtr(args.MultiAz),
		PubliclyAccessible:    pulumi.BoolPtr(args.PubliclyAccessible),
		SkipFinalSnapshot:     pulumi.BoolPtr(args.SkipFinalSnapshot),
		BackupRetentionPeriod: pulumi.IntPtr(args.BackupRetentionPeriod),
	}
	if args.FinalSnapshotIdentifier != "" {
		instanceArgs.FinalSnapshotIdentifier = pulumi.StringPtr(args.FinalSnapshotIdentifier)
	}

	instance, err := rds.NewInstance(ctx, "instance", instanceArgs, pulumi.Parent(c),
		// floci no devuelve estos atributos (los reporta como null), lo que
		// provocaría un replace espurio en cada despliegue local. Ignoramos sus
		// cambios para que no se intente recrear la instancia.
		pulumi.IgnoreChanges([]string{"autoMinorVersionUpgrade", "backupRetentionPeriod", "storageEncrypted"}))
	if err != nil {
		return nil, err
	}

	c.Endpoint = instance.Address
	c.Port = instance.Port
	c.InstanceIdentifier = instance.Identifier
	c.ConnectionString = buildConnectionString(defaultUsername, password.Result, instance.Address, instance.Port, args.DbName)

	if args.SecretsManager != nil {
		secretString := buildCredentials(defaultUsername, password.Result, instance.Address, instance.Port, args.DbName)
		secret, err := args.SecretsManager.PutSecret(ctx, name+"-credentials", "Credenciales de acceso a la base de datos PostgreSQL", secretString)
		if err != nil {
			return nil, err
		}
		c.SecretARN = secret.Arn
	}

	if err := ctx.RegisterResourceOutputs(c, pulumi.Map{
		"endpoint":           c.Endpoint,
		"port":               c.Port,
		"instanceIdentifier": c.InstanceIdentifier,
		"secretARN":          c.SecretARN,
		"connectionString":   c.ConnectionString,
	}); err != nil {
		return nil, err
	}
	return c, nil
}

func applyDatabaseDefaults(args *DatabaseArgs) {
	if args.EngineVersion == "" {
		args.EngineVersion = defaultEngineVersion
	}
	if args.InstanceClass == "" {
		args.InstanceClass = defaultInstanceClass
	}
	if args.AllocatedStorage == 0 {
		args.AllocatedStorage = defaultAllocatedStorage
	}
	if args.DbName == "" {
		args.DbName = defaultDbName
	}
	if args.BackupRetentionPeriod == 0 {
		args.BackupRetentionPeriod = defaultBackupRetention
	}
	if len(args.IngressCidrs) == 0 {
		args.IngressCidrs = []string{"0.0.0.0/0"}
	}
	if !args.SkipFinalSnapshot && args.FinalSnapshotIdentifier == "" {
		args.SkipFinalSnapshot = true
	}
}

// resolveNetwork devuelve las subnets del DB subnet group y el ID de la VPC
// (solo si es necesario crear el security group).
func resolveNetwork(ctx *pulumi.Context, args *DatabaseArgs) ([]string, string, error) {
	if len(args.SubnetIDs) > 0 {
		var vpcID string
		if len(args.SecurityGroupIDs) == 0 {
			subnet, err := ec2.LookupSubnet(ctx, &ec2.LookupSubnetArgs{Id: pulumi.StringRef(args.SubnetIDs[0])})
			if err != nil {
				return nil, "", err
			}
			vpcID = subnet.VpcId
		}
		return args.SubnetIDs, vpcID, nil
	}

	vpc, err := ec2.LookupVpc(ctx, &ec2.LookupVpcArgs{Default: pulumi.BoolRef(true)})
	if err != nil {
		return nil, "", err
	}
	subnets, err := ec2.GetSubnets(ctx, &ec2.GetSubnetsArgs{
		Filters: []ec2.GetSubnetsFilter{
			{Name: "vpc-id", Values: []string{vpc.Id}},
		},
	})
	if err != nil {
		return nil, "", err
	}
	return subnets.Ids, vpc.Id, nil
}

func resolveSecurityGroupIDs(ctx *pulumi.Context, c *Database, args *DatabaseArgs, vpcID string) (pulumi.StringArrayInput, error) {
	if len(args.SecurityGroupIDs) > 0 {
		return pulumi.ToStringArray(args.SecurityGroupIDs), nil
	}

	sg, err := ec2.NewSecurityGroup(ctx, "security-group", &ec2.SecurityGroupArgs{
		Description: pulumi.String("Security group para la instancia PostgreSQL (puerto 5432)"),
		VpcId:       pulumi.String(vpcID),
		Ingress: ec2.SecurityGroupIngressArray{
			ec2.SecurityGroupIngressArgs{
				Protocol:   pulumi.String("tcp"),
				FromPort:   pulumi.Int(defaultPostgresPort),
				ToPort:     pulumi.Int(defaultPostgresPort),
				CidrBlocks: pulumi.ToStringArray(args.IngressCidrs),
			},
		},
	}, pulumi.Parent(c))
	if err != nil {
		return nil, err
	}
	return pulumi.StringArray{sg.ID().ToStringOutput()}, nil
}

func buildCredentials(username string, password pulumi.StringInput, host pulumi.StringInput, port pulumi.IntInput, dbName string) pulumi.StringOutput {
	return pulumi.All(username, password, host, port, dbName).ApplyT(
		func(vals []interface{}) (string, error) {
			creds := dbCredentials{
				Engine:   "postgres",
				Username: vals[0].(string),
				Password: vals[1].(string),
				Host:     vals[2].(string),
				Port:     vals[3].(int),
				DbName:   vals[4].(string),
			}
			b, err := json.Marshal(creds)
			if err != nil {
				return "", err
			}
			return string(b), nil
		}).(pulumi.StringOutput)
}

// buildConnectionString construye el DSN de PostgreSQL para conectarse a la
// instancia RDS. El resultado hereda el secretismo del password.
func buildConnectionString(username string, password pulumi.StringInput, host pulumi.StringInput, port pulumi.IntInput, dbName string) pulumi.StringOutput {
	return pulumi.Sprintf("postgres://%s:%s@%s:%d/%s", username, password, host, port, dbName)
}
