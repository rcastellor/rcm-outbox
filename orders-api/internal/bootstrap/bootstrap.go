package bootstrap

import (
	"context"
	"fmt"

	"github.com/rcastellor/rcm-outbox/orders-api/internal/handler"
	"github.com/rcastellor/rcm-outbox/orders-api/internal/repository"
	"github.com/rcastellor/rcm-outbox/orders-api/internal/router"
	"github.com/rcastellor/rcm-outbox/orders-api/internal/usecase"
	platformconfig "github.com/rcastellor/rcm-outbox/rcm-platform/config"
	"github.com/rcastellor/rcm-outbox/rcm-platform/database"
	"github.com/rcastellor/rcm-outbox/rcm-platform/logger"
	"github.com/rcastellor/rcm-outbox/rcm-platform/secrets"
)

// Load arranca la Lambda: lee la configuración, recupera las credenciales de
// PostgreSQL desde Secrets Manager y ensambla el handler con sus dependencias.
func Load(ctx context.Context) (*handler.Handler, error) {
	log := logger.New()

	cfg, err := platformconfig.LoadDB()
	if err != nil {
		return nil, fmt.Errorf("cargando configuración: %w", err)
	}

	creds, err := secrets.Fetch(ctx, cfg.DBSecretARN)
	if err != nil {
		return nil, err
	}

	pool, err := database.NewPool(ctx, creds.DSN())
	if err != nil {
		return nil, err
	}

	repo := repository.NewOrders(pool)
	uc := usecase.NewOrders(repo, log)
	r := router.New(uc)

	return handler.New(r, log), nil
}
