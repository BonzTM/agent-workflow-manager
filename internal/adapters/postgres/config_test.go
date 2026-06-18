package postgres

import "testing"

func TestConfigValidate_RequiresDSN(t *testing.T) {
	cfg := Config{}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for missing dsn")
	}
}

func TestConfigPoolConfig_AppliesDefaults(t *testing.T) {
	cfg := Config{
		DSN: "postgres://ctx:ctx@localhost:5432/awm_test?sslmode=disable",
	}

	poolCfg, err := cfg.PoolConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if poolCfg.MaxConns != defaultMaxConns {
		t.Fatalf("expected max conns %d, got %d", defaultMaxConns, poolCfg.MaxConns)
	}
	if poolCfg.MinConns != defaultMinConns {
		t.Fatalf("expected min conns %d, got %d", defaultMinConns, poolCfg.MinConns)
	}
}

func TestNewWithPool_RequiresPool(t *testing.T) {
	repo, err := NewWithPool(nil)
	if err == nil {
		t.Fatal("expected error for nil pool")
	}
	if repo != nil {
		t.Fatalf("expected nil repo, got %#v", repo)
	}
}
