package bootstrap

import (
	"context"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/rcastellor/rcm-outbox/orders-dispatcher/internal/config"
	"github.com/rcastellor/rcm-outbox/orders-dispatcher/internal/dispatcher"
	"github.com/rcastellor/rcm-outbox/orders-dispatcher/internal/handler"
	"github.com/rcastellor/rcm-outbox/orders-dispatcher/internal/queue"
	"github.com/rcastellor/rcm-outbox/orders-dispatcher/internal/repository"
	"github.com/rcastellor/rcm-outbox/rcm-platform/database"
	"github.com/rcastellor/rcm-outbox/rcm-platform/logger"
	"github.com/rcastellor/rcm-outbox/rcm-platform/secrets"
)

// Load arranca la Lambda del dispatcher: lee la configuración, recupera las
// credenciales de PostgreSQL, abre el pool, crea el cliente SQS y ensambla el
// handler con sus dependencias.
func Load(ctx context.Context) (*handler.Handler, error) {
	log := logger.New()

	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("cargando configuración: %w", err)
	}

	creds, err := secrets.Fetch(ctx, cfg.DB.DBSecretARN)
	if err != nil {
		return nil, err
	}

	pool, err := database.NewPool(ctx, creds.DSN())
	if err != nil {
		return nil, err
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("cargando configuración AWS: %w", err)
	}
	sqsClient := sqs.NewFromConfig(awsCfg)

	repo := repository.NewOutbox(pool)
	pub := queue.New(sqsClient, cfg.DispatchQueueURL)
	d := dispatcher.New(repo, pub, cfg.BatchSize, cfg.MaxWorkers, log)

	return handler.New(d), nil
}
