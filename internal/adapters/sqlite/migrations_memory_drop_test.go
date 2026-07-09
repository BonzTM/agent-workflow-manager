package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func TestMigrations_DropDormantMemorySchema(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "memory-drop.sqlite")

	repo, err := New(ctx, Config{Path: dbPath})
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	for _, table := range []string{"awm_memories", "awm_memory_candidates"} {
		var count int
		if scanErr := repo.db.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&count); scanErr != nil {
			t.Fatalf("query sqlite_master for %s: %v", table, scanErr)
		}
		if count != 0 {
			t.Fatalf("expected dormant table %s to be dropped, but it exists", table)
		}
	}

	rows, err := repo.db.QueryContext(ctx, `SELECT name FROM pragma_table_info('awm_receipts')`)
	if err != nil {
		t.Fatalf("read awm_receipts columns: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr != nil {
			t.Fatalf("scan column name: %v", scanErr)
		}
		if name == "memory_ids_json" {
			t.Fatal("expected awm_receipts.memory_ids_json to be dropped, but it exists")
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		t.Fatalf("iterate awm_receipts columns: %v", rowsErr)
	}

	scope := core.ReceiptScope{
		ProjectID:    "project.memory-drop",
		ReceiptID:    "receipt-memory-drop",
		TaskText:     "verify receipts read cleanly without memory columns",
		Phase:        "execute",
		ResolvedTags: []string{"migrations"},
	}
	if upsertErr := repo.UpsertReceiptScope(ctx, scope); upsertErr != nil {
		t.Fatalf("upsert receipt scope: %v", upsertErr)
	}
	got, fetchErr := repo.FetchReceiptScope(ctx, core.ReceiptScopeQuery{
		ProjectID: scope.ProjectID,
		ReceiptID: scope.ReceiptID,
	})
	if fetchErr != nil {
		t.Fatalf("fetch receipt scope: %v", fetchErr)
	}
	if got.TaskText != scope.TaskText || got.Phase != scope.Phase {
		t.Fatalf("unexpected receipt scope round-trip: %+v", got)
	}
}
