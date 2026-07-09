//go:build integration

package integration

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bonztm/agent-workflow-manager/internal/logging"
	"github.com/bonztm/agent-workflow-manager/internal/runtime"
)

func TestRuntimePostgresIntegration_MemorySchemaDropped(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(integrationDSNEnvVar))
	if dsn == "" {
		t.Skipf("%s is required", integrationDSNEnvVar)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	_, cleanup, err := runtime.NewServiceWithLogger(ctx, runtime.Config{PostgresDSN: dsn}, logging.NewDiscardLogger())
	if err != nil {
		t.Fatalf("new runtime service: %v", err)
	}
	t.Cleanup(cleanup)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect postgres for assertions: %v", err)
	}
	t.Cleanup(pool.Close)

	for _, table := range []string{"awm_memories", "awm_memory_candidates"} {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT to_regclass($1) IS NOT NULL`, table,
		).Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if exists {
			t.Fatalf("expected dormant table %s to be dropped, but it exists", table)
		}
	}

	var columnCount int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(1) FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = 'awm_receipts' AND column_name = 'memory_ids'
`).Scan(&columnCount); err != nil {
		t.Fatalf("check awm_receipts.memory_ids column: %v", err)
	}
	if columnCount != 0 {
		t.Fatal("expected awm_receipts.memory_ids to be dropped, but it exists")
	}
}
