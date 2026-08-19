package bootstrap

import (
	"context"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"

	"github.com/rcastellor/rcm-outbox/orders-workers/internal/config"
	"github.com/rcastellor/rcm-outbox/orders-workers/internal/handler"
	"github.com/rcastellor/rcm-outbox/orders-workers/internal/publisher"
	"github.com/rcastellor/rcm-outbox/orders-workers/internal/repository"
	"github.com/rcastellor/rcm-outbox/orders-workers/internal/worker"
	"github.com/rcastellor/rcm-outbox/rcm-platform/database"
	"github.com/rcastellor/rcm-outbox/rcm-platform/logger"
	"github.com/rcastellor/rcm-outbox/rcm-platform/secrets"
)

// Load arranca la Lambda del worker: lee la configuración, recupera las
// credenciales de PostgreSQL, abre el pool, crea el cliente SNS y ensambla el
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
	snsClient := sns.NewFromConfig(awsCfg)

	repo := repository.NewOutbox(pool)
	pub := publisher.New(snsClient, cfg.SNSTopicARN)
	w := worker.New(repo, pub, cfg.BatchSize, cfg.MaxAttempts, cfg.BackoffBase, cfg.MaxBackoff, log)

	return handler.New(w), nil
}
