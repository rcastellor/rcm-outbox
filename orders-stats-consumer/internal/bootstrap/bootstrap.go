package bootstrap

import (
	"context"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/rcastellor/rcm-outbox/orders-stats-consumer/internal/config"
	"github.com/rcastellor/rcm-outbox/orders-stats-consumer/internal/handler"
	"github.com/rcastellor/rcm-outbox/orders-stats-consumer/internal/stats"
	"github.com/rcastellor/rcm-outbox/rcm-platform/logger"
)

// Load arranca la Lambda consumidora de estadísticas: lee la configuración,
// crea el cliente DynamoDB y ensambla el handler con sus dependencias.
func Load(ctx context.Context) (*handler.Handler, error) {
	log := logger.New()

	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("cargando configuración: %w", err)
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("cargando configuración AWS: %w", err)
	}
	client := dynamodb.NewFromConfig(awsCfg)

	agg := stats.New(client, cfg.StatsTableName, cfg.InboxTableName, log)
	return handler.New(agg), nil
}
