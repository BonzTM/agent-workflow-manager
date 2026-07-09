package runtime

import (
	"context"
	"fmt"

	"github.com/bonztm/agent-workflow-manager/internal/adapters/postgres"
	sqliteadapter "github.com/bonztm/agent-workflow-manager/internal/adapters/sqlite"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	"github.com/bonztm/agent-workflow-manager/internal/logging"
	backendsvc "github.com/bonztm/agent-workflow-manager/internal/service/backend"
)

// CleanupFunc releases the resources (e.g. database connections) backing a
// service; call it once the service is no longer needed.
type CleanupFunc func()

// NewServiceFromEnv constructs a service using configuration from the
// environment and the default runtime logger.
func NewServiceFromEnv(ctx context.Context) (core.Service, CleanupFunc, error) {
	return NewServiceFromEnvWithLogger(ctx, NewLogger())
}

// NewServiceFromEnvWithLogger constructs a service using configuration from
// the environment and the provided logger.
func NewServiceFromEnvWithLogger(ctx context.Context, logger logging.Logger) (core.Service, CleanupFunc, error) {
	return NewServiceWithLogger(ctx, ConfigFromEnv(), logger)
}

// NewService constructs a service from cfg using the default runtime logger.
func NewService(ctx context.Context, cfg Config) (core.Service, CleanupFunc, error) {
	return NewServiceWithLogger(ctx, cfg, NewLogger())
}

// NewServiceWithLogger constructs a logging-wrapped service from cfg,
// backed by Postgres when cfg.PostgresConfigured() and SQLite otherwise.
// The returned CleanupFunc closes the underlying repository; a nil logger
// is replaced with a discard logger.
func NewServiceWithLogger(ctx context.Context, cfg Config, logger logging.Logger) (core.Service, CleanupFunc, error) {
	logger = logging.Normalize(logger)

	if cfg.PostgresConfigured() {
		repo, err := postgres.New(ctx, postgres.Config{DSN: cfg.PostgresDSN})
		if err != nil {
			return nil, nil, fmt.Errorf("initialize postgres repository: %w", err)
		}

		svc, err := backendsvc.NewWithRuntimeStatus(repo, cfg.ProjectRoot, backendsvcRuntimeSnapshot(cfg, "postgres"))
		if err != nil {
			repo.Close()
			return nil, nil, fmt.Errorf("initialize postgres service: %w", err)
		}

		return core.WithLogging(svc, logger), func() {
			repo.Close()
		}, nil
	}

	sqliteRepo, err := sqliteadapter.New(ctx, sqliteadapter.Config{
		Path: cfg.EffectiveSQLitePath(),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("initialize sqlite repository: %w", err)
	}

	svc, err := backendsvc.NewWithRuntimeStatus(sqliteRepo, cfg.ProjectRoot, backendsvcRuntimeSnapshot(cfg, "sqlite"))
	if err != nil {
		_ = sqliteRepo.Close()
		return nil, nil, fmt.Errorf("initialize sqlite service: %w", err)
	}

	return core.WithLogging(svc, logger), func() {
		_ = sqliteRepo.Close()
	}, nil
}

func backendsvcRuntimeSnapshot(cfg Config, backend string) backendsvc.RuntimeStatusSnapshot {
	return backendsvc.RuntimeStatusSnapshot{
		Backend:                backend,
		PostgresConfigured:     cfg.PostgresConfigured(),
		SQLitePath:             cfg.EffectiveSQLitePath(),
		UsesImplicitSQLitePath: cfg.UsesImplicitSQLitePath(),
	}
}
