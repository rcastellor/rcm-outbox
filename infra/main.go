package main

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/rcastellor/rcm-outbox/infra/internal/config"
	"github.com/rcastellor/rcm-outbox/infra/internal/domains/migrations"
	"github.com/rcastellor/rcm-outbox/infra/internal/domains/orders"
	"github.com/rcastellor/rcm-outbox/infra/internal/domains/outbox"
	"github.com/rcastellor/rcm-outbox/infra/internal/domains/stats"
	"github.com/rcastellor/rcm-outbox/infra/internal/platform"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg, err := config.LoadConfig(ctx)
		if err != nil {
			return err
		}

		secretsManager, err := platform.NewSecretsManager(ctx, "secrets")
		if err != nil {
			return err
		}

		db, err := platform.NewDatabase(ctx, "database", &platform.DatabaseArgs{
			SkipFinalSnapshot: cfg.Database.SkipFinalSnapshot,
			SecretsManager:    secretsManager,
		})
		if err != nil {
			return err
		}

		mig, err := migrations.NewMigrations(ctx, "migrations", &migrations.MigrationsArgs{
			DBSecretARN: db.SecretARN,
		}, pulumi.DependsOn([]pulumi.Resource{db}))
		if err != nil {
			return err
		}

		ctx.Export("migrationsFunctionName", mig.FunctionName)
		ctx.Export("migrationsFunctionARN", mig.FunctionARN)

		sns, err := platform.NewSNS(ctx, "sns", &platform.SNSArgs{Topics: cfg.Topics})
		if err != nil {
			return err
		}

		topicName := cfg.Worker.TopicName
		if topicName == "" {
			topicName = config.DefaultWorkerTopicName
		}
		topicARN := sns.TopicARN(topicName)

		statsDomain, err := stats.NewStats(ctx, "stats", &stats.StatsArgs{
			TopicARN: topicARN,
		})
		if err != nil {
			return err
		}

		ctx.Export("statsTableName", statsDomain.TableName)
		ctx.Export("statsInboxTableName", statsDomain.InboxTableName)

		dispatchQueue, err := outbox.NewQueue(ctx, "dispatch", &outbox.QueueArgs{
			QueueName:       config.DefaultDispatchQueueName,
			DLQName:         config.DefaultDispatchDLQName,
			MaxReceiveCount: config.DefaultMaxReceiveCount,
		})
		if err != nil {
			return err
		}

		api, err := orders.NewAPI(ctx, "orders-api", &orders.APIArgs{
			DBSecretARN: db.SecretARN,
		}, pulumi.DependsOn([]pulumi.Resource{db}))
		if err != nil {
			return err
		}

		ctx.Export("ordersApiUrl", api.URL)

		_, err = outbox.NewWorker(ctx, "orders-worker", &outbox.WorkerArgs{
			DBSecretARN:        db.SecretARN,
			SNSTopicARN:        topicARN,
			DispatchQueueARN:   dispatchQueue.QueueARN,
			BatchSize:          cfg.Worker.BatchSize,
			MaxWorkers:         cfg.Worker.MaxWorkers,
			MaxAttempts:        cfg.Worker.MaxAttempts,
			BackoffBaseSeconds: cfg.Worker.BackoffBaseSeconds,
			MaxBackoffSeconds:  cfg.Worker.MaxBackoffSeconds,
		}, pulumi.DependsOn([]pulumi.Resource{db, dispatchQueue}))
		if err != nil {
			return err
		}

		dispatcherFn, err := outbox.NewDispatcher(ctx, "orders-dispatcher", &outbox.DispatcherArgs{
			DBSecretARN:      db.SecretARN,
			DispatchQueueURL: dispatchQueue.QueueURL,
			DispatchQueueARN: dispatchQueue.QueueARN,
			BatchSize:        cfg.Worker.BatchSize,
			MaxWorkers:       cfg.Worker.MaxWorkers,
		}, pulumi.DependsOn([]pulumi.Resource{db, dispatchQueue}))
		if err != nil {
			return err
		}

		_, err = outbox.NewSchedule(ctx, "dispatcher", &outbox.ScheduleArgs{
			FunctionName: dispatcherFn.Function.Function.Name,
			FunctionARN:  dispatcherFn.FunctionARN,
		}, pulumi.DependsOn([]pulumi.Resource{mig, dispatcherFn}))
		if err != nil {
			return err
		}

		return nil
	})
}
