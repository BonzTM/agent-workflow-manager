package postgres

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultMaxConns int32 = 8
	defaultMinConns int32 = 0
)

const (
	defaultConnMaxLifetime = 30 * time.Minute
	defaultConnMaxIdleTime = 5 * time.Minute
)

// Config holds the settings needed to open a Postgres-backed repository.
// Only DSN is required; zero values for the pool fields select the package
// defaults.
type Config struct {
	DSN             string
	MaxConns        int32
	MinConns        int32
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// Validate reports an error when the config cannot produce a usable pool:
// a missing DSN, negative sizes or durations, or min_conns exceeding the
// effective max_conns.
func (c Config) Validate() error {
	if strings.TrimSpace(c.DSN) == "" {
		return errors.New("postgres dsn is required")
	}
	if c.MaxConns < 0 {
		return errors.New("max_conns must be >= 0")
	}
	if c.MinConns < 0 {
		return errors.New("min_conns must be >= 0")
	}
	maxConns := c.MaxConns
	if maxConns == 0 {
		maxConns = defaultMaxConns
	}
	minConns := c.MinConns
	if minConns > maxConns {
		return errors.New("min_conns must be <= max_conns")
	}
	if c.ConnMaxLifetime < 0 {
		return errors.New("conn_max_lifetime must be >= 0")
	}
	if c.ConnMaxIdleTime < 0 {
		return errors.New("conn_max_idle_time must be >= 0")
	}
	return nil
}

// PoolConfig validates the config and converts it into a pgxpool.Config,
// applying the package defaults for any pool setting left at its zero value.
func (c Config) PoolConfig() (*pgxpool.Config, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	cfg, err := pgxpool.ParseConfig(strings.TrimSpace(c.DSN))
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}

	if c.MaxConns == 0 {
		cfg.MaxConns = defaultMaxConns
	} else {
		cfg.MaxConns = c.MaxConns
	}
	cfg.MinConns = c.MinConns

	if c.ConnMaxLifetime == 0 {
		cfg.MaxConnLifetime = defaultConnMaxLifetime
	} else {
		cfg.MaxConnLifetime = c.ConnMaxLifetime
	}
	if c.ConnMaxIdleTime == 0 {
		cfg.MaxConnIdleTime = defaultConnMaxIdleTime
	} else {
		cfg.MaxConnIdleTime = c.ConnMaxIdleTime
	}

	return cfg, nil
}
