package platform

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/sns"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/rcastellor/rcm-outbox/infra/internal/config"
)

// SNSArgs define los parámetros del componente SNS.
type SNSArgs struct {
	// Topics son los topics SNS a crear.
	Topics []config.Topic
}

// SNS agrupa los topics y suscripciones SNS de la infraestructura.
type SNS struct {
	pulumi.ResourceState

	// TopicARNs mapea el nombre de cada topic a su ARN.
	TopicARNs pulumi.MapOutput `pulumi:"topicARNs"`
}

// NewSNS crea el componente SNS con los topics indicados.
func NewSNS(ctx *pulumi.Context, name string, args *SNSArgs, opts ...pulumi.ResourceOption) (*SNS, error) {
	c := &SNS{}
	if err := ctx.RegisterComponentResource("rcm-outbox:messaging:SNS", name, c, opts...); err != nil {
		return nil, err
	}
	if args == nil {
		args = &SNSArgs{}
	}
	applySNSDefaults(args)

	topicARNs := map[string]pulumi.Output{}
	for _, t := range args.Topics {
		topic, err := sns.NewTopic(ctx, t.Name, &sns.TopicArgs{
			Name:                      pulumi.String(t.Name),
			FifoTopic:                 pulumi.BoolPtr(t.Fifo),
			ContentBasedDeduplication: pulumi.BoolPtrFromPtr(t.ContentBasedDeduplication),
		}, pulumi.Parent(c))
		if err != nil {
			return nil, err
		}
		topicARNs[t.Name] = topic.Arn
	}
	c.TopicARNs = pulumi.ToMapOutput(topicARNs)

	if err := ctx.RegisterResourceOutputs(c, pulumi.Map{
		"topicARNs": c.TopicARNs,
	}); err != nil {
		return nil, err
	}
	return c, nil
}

// applySNSDefaults activa la deduplicación por contenido en topics FIFO cuando no se especifica.
func applySNSDefaults(args *SNSArgs) {
	for i := range args.Topics {
		if args.Topics[i].Fifo && args.Topics[i].ContentBasedDeduplication == nil {
			enabled := true
			args.Topics[i].ContentBasedDeduplication = &enabled
		}
	}
}

// TopicARN devuelve el ARN del topic con el nombre indicado. El output falla
// si el topic no existe en el componente.
func (s *SNS) TopicARN(name string) pulumi.StringOutput {
	return s.TopicARNs.MapIndex(pulumi.String(name)).ApplyT(func(v any) (string, error) {
		arn, ok := v.(string)
		if !ok || arn == "" {
			return "", fmt.Errorf("topic SNS %q no encontrado", name)
		}
		return arn, nil
	}).(pulumi.StringOutput)
}
