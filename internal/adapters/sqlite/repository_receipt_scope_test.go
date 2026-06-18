package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func TestSQLiteMigrations_BackfillReceiptScopeInitialScopePaths(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "ctx.sqlite")

	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS awm_schema_migrations (
	migration_name TEXT PRIMARY KEY,
	applied_at INTEGER NOT NULL DEFAULT (unixepoch())
)`); err != nil {
		t.Fatalf("create schema migrations table: %v", err)
	}

	for _, migration := range migrations {
		if migration.Name == "0011_awm_receipt_scope_pointer_paths.sql" {
			break
		}
		if _, err := db.ExecContext(ctx, migration.SQL); err != nil {
			t.Fatalf("apply pre-0011 migration %s: %v", migration.Name, err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO awm_schema_migrations (migration_name) VALUES (?)
`, migration.Name); err != nil {
			t.Fatalf("record pre-0011 migration %s: %v", migration.Name, err)
		}
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO awm_pointers (
	project_id,
	pointer_key,
	path,
	anchor,
	kind,
	label,
	description,
	tags_json,
	is_rule,
	is_stale
) VALUES (?, ?, ?, '', 'doc', ?, ?, '[]', 0, 0)
`, "project.alpha", "pointer.runtime", "docs/runtime.md", "Runtime", "Runtime pointer"); err != nil {
		t.Fatalf("seed pointer: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO awm_receipts (
	receipt_id,
	project_id,
	task_text,
	phase,
	resolved_tags_json,
	pointer_keys_json,
	memory_ids_json,
	summary_json,
	created_at
) VALUES (?, ?, ?, ?, '[]', ?, '[]', '{}', unixepoch())
`, "receipt.alpha", "project.alpha", "seed receipt scope", "execute", `["pointer.runtime"]`); err != nil {
		t.Fatalf("seed pre-0011 receipt: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close pre-migration db: %v", err)
	}
	db = nil

	repo, err := New(ctx, Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open migrated repository: %v", err)
	}
	defer func() { _ = repo.Close() }()

	scope, err := repo.FetchReceiptScope(ctx, core.ReceiptScopeQuery{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.alpha",
	})
	if err != nil {
		t.Fatalf("fetch migrated receipt scope: %v", err)
	}
	if got, want := scope.InitialScopePaths, []string{"docs/runtime.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected migrated initial scope paths: got %v want %v", got, want)
	}
}

func TestSQLiteMigrations_BackfillInitialScopePathsAndDiscoveredPaths(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "ctx.sqlite")

	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS awm_schema_migrations (
	migration_name TEXT PRIMARY KEY,
	applied_at INTEGER NOT NULL DEFAULT (unixepoch())
)`); err != nil {
		t.Fatalf("create schema migrations table: %v", err)
	}

	for _, migration := range migrations {
		if migration.Name == "0012_awm_initial_scope_and_baselines.sql" {
			break
		}
		if _, err := db.ExecContext(ctx, migration.SQL); err != nil {
			t.Fatalf("apply pre-0012 migration %s: %v", migration.Name, err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO awm_schema_migrations (migration_name) VALUES (?)
`, migration.Name); err != nil {
			t.Fatalf("record pre-0012 migration %s: %v", migration.Name, err)
		}
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO awm_receipts (
	receipt_id,
	project_id,
	task_text,
	phase,
	resolved_tags_json,
	pointer_keys_json,
	pointer_paths_json,
	memory_ids_json,
	summary_json,
	created_at
) VALUES (?, ?, ?, ?, '[]', '[]', ?, '[]', '{}', unixepoch())
`, "receipt.scope", "project.alpha", "seed initial scope", "execute", `["docs/runtime.md","internal/service/backend/context.go"]`); err != nil {
		t.Fatalf("seed pre-0012 receipt: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO awm_work_plans (
	project_id,
	plan_key,
	receipt_id,
	title,
	objective,
	status,
	stage_spec_outline,
	stage_refined_spec,
	stage_implementation_plan,
	in_scope_json,
	out_of_scope_json,
	constraints_json,
	references_json,
	kind,
	parent_plan_key,
	external_refs_json,
	created_at,
	updated_at
) VALUES (?, ?, ?, ?, ?, 'in_progress', 'pending', 'pending', 'pending', '[]', '[]', '[]', '[]', '', '', '[]', unixepoch(), unixepoch())
`, "project.alpha", "plan:receipt.scope", "receipt.scope", "Scope migration", "Verify 0012 migration"); err != nil {
		t.Fatalf("seed pre-0012 work plan: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close pre-migration db: %v", err)
	}
	db = nil

	repo, err := New(ctx, Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open migrated repository: %v", err)
	}
	defer func() { _ = repo.Close() }()

	scope, err := repo.FetchReceiptScope(ctx, core.ReceiptScopeQuery{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.scope",
	})
	if err != nil {
		t.Fatalf("fetch migrated receipt scope: %v", err)
	}
	if got, want := scope.InitialScopePaths, []string{"docs/runtime.md", "internal/service/backend/context.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected migrated initial scope: got %v want %v", got, want)
	}
	if scope.BaselineCaptured {
		t.Fatalf("expected baseline to remain uncaptured after migration")
	}
	if len(scope.BaselinePaths) != 0 {
		t.Fatalf("expected no baseline paths after migration, got %v", scope.BaselinePaths)
	}

	plan, err := repo.LookupWorkPlan(ctx, core.WorkPlanLookupQuery{
		ProjectID: "project.alpha",
		PlanKey:   "plan:receipt.scope",
	})
	if err != nil {
		t.Fatalf("fetch migrated work plan: %v", err)
	}
	if len(plan.DiscoveredPaths) != 0 {
		t.Fatalf("expected discovered paths to default empty after migration, got %v", plan.DiscoveredPaths)
	}
}
