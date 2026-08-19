package bootstrap

import (
	"context"
	"fmt"

	"github.com/rcastellor/rcm-outbox/rcm-migrations/internal/handler"
	"github.com/rcastellor/rcm-outbox/rcm-migrations/internal/migrations"
	"github.com/rcastellor/rcm-outbox/rcm-migrations/internal/runner"
	platformconfig "github.com/rcastellor/rcm-outbox/rcm-platform/config"
	"github.com/rcastellor/rcm-outbox/rcm-platform/database"
	"github.com/rcastellor/rcm-outbox/rcm-platform/logger"
	"github.com/rcastellor/rcm-outbox/rcm-platform/secrets"
)

// Load arranca la Lambda: lee la configuración, recupera las credenciales de
// PostgreSQL desde Secrets Manager y ensambla el handler con el runner de
// migraciones.
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

	r := runner.New(pool, migrations.All(), log)

	return handler.New(r), nil
}
