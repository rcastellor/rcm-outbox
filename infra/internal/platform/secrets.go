package platform

import (
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/secretsmanager"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// SecretsManager agrupa los secretos de la infraestructura en AWS Secrets Manager.
// Es un componente reutilizable por otros recursos (base de datos, workers, etc.).
type SecretsManager struct {
	pulumi.ResourceState
}

// NewSecretsManager crea el componente SecretsManager.
func NewSecretsManager(ctx *pulumi.Context, name string, opts ...pulumi.ResourceOption) (*SecretsManager, error) {
	c := &SecretsManager{}
	if err := ctx.RegisterComponentResource("rcm-outbox:secrets:SecretsManager", name, c, opts...); err != nil {
		return nil, err
	}
	if err := ctx.RegisterResourceOutputs(c, pulumi.Map{}); err != nil {
		return nil, err
	}
	return c, nil
}

// PutSecret crea un secreto con su valor como recurso hijo del componente y
// devuelve el recurso Secret para que el llamador pueda exponer su ARN/nombre.
func (s *SecretsManager) PutSecret(ctx *pulumi.Context, name, description string, secretString pulumi.StringInput) (*secretsmanager.Secret, error) {
	secret, err := secretsmanager.NewSecret(ctx, name, &secretsmanager.SecretArgs{
		Description:          pulumi.String(description),
		RecoveryWindowInDays: pulumi.Int(0),
	}, pulumi.Parent(s))
	if err != nil {
		return nil, err
	}

	if _, err := secretsmanager.NewSecretVersion(ctx, name+"-version", &secretsmanager.SecretVersionArgs{
		SecretId:     secret.Arn,
		SecretString: secretString,
	}, pulumi.Parent(secret)); err != nil {
		return nil, err
	}

	return secret, nil
}
