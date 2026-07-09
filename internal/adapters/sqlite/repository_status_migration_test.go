package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestSQLiteMigrations_ConvertCompletedStatusesToComplete(t *testing.T) {
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

	if _, execErr := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS awm_schema_migrations (
	migration_name TEXT PRIMARY KEY,
	applied_at INTEGER NOT NULL DEFAULT (unixepoch())
)`); execErr != nil {
		t.Fatalf("create schema migrations table: %v", execErr)
	}

	for _, name := range []string{
		"0001_awm_foundation.sql",
		"0002_awm_propose_memory.sql",
		"0003_awm_sync.sql",
		"0004_awm_work_items.sql",
		"0005_awm_work_plans.sql",
		"0006_awm_work_plan_hierarchy.sql",
		"0007_awm_verification_runs.sql",
		"0008_awm_sqlite_parity.sql",
		"0009_awm_run_history_indexes.sql",
		"0010_awm_review_attempts.sql",
		"0011_awm_receipt_scope_pointer_paths.sql",
		"0012_awm_initial_scope_and_baselines.sql",
	} {
		if _, execErr := db.ExecContext(ctx, `INSERT INTO awm_schema_migrations (migration_name) VALUES (?)`, name); execErr != nil {
			t.Fatalf("record pre-0013 migration %s: %v", name, execErr)
		}
	}

	if _, execErr := db.ExecContext(ctx, `
CREATE TABLE awm_receipts (
	receipt_id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	task_text TEXT NOT NULL,
	phase TEXT NOT NULL,
	resolved_tags_json TEXT NOT NULL DEFAULT '[]',
	pointer_keys_json TEXT NOT NULL DEFAULT '[]',
	pointer_paths_json TEXT NOT NULL DEFAULT '[]',
	initial_scope_paths_json TEXT NOT NULL DEFAULT '[]',
	baseline_paths_json TEXT NOT NULL DEFAULT '[]',
	baseline_captured INTEGER NOT NULL DEFAULT 0 CHECK (baseline_captured IN (0, 1)),
	memory_ids_json TEXT NOT NULL DEFAULT '[]',
	summary_json TEXT NOT NULL DEFAULT '{}',
	created_at INTEGER NOT NULL DEFAULT (unixepoch())
)`); execErr != nil {
		t.Fatalf("create legacy receipt table: %v", execErr)
	}

	if _, execErr := db.ExecContext(ctx, `
CREATE TABLE awm_work_items (
	work_item_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	receipt_id TEXT NOT NULL,
	item_key TEXT NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('pending', 'in_progress', 'blocked', 'completed')),
	created_at INTEGER NOT NULL DEFAULT (unixepoch()),
	updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
	UNIQUE (project_id, receipt_id, item_key),
	FOREIGN KEY (receipt_id) REFERENCES awm_receipts (receipt_id) ON DELETE CASCADE
)`); execErr != nil {
		t.Fatalf("create legacy work items table: %v", execErr)
	}

	if _, execErr := db.ExecContext(ctx, `
CREATE TABLE awm_work_plans (
	plan_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	plan_key TEXT NOT NULL,
	receipt_id TEXT NULL,
	title TEXT NOT NULL DEFAULT '',
	objective TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL CHECK (status IN ('pending', 'in_progress', 'blocked', 'completed')),
	stage_spec_outline TEXT NOT NULL DEFAULT 'pending' CHECK (stage_spec_outline IN ('pending', 'in_progress', 'blocked', 'completed')),
	stage_refined_spec TEXT NOT NULL DEFAULT 'pending' CHECK (stage_refined_spec IN ('pending', 'in_progress', 'blocked', 'completed')),
	stage_implementation_plan TEXT NOT NULL DEFAULT 'pending' CHECK (stage_implementation_plan IN ('pending', 'in_progress', 'blocked', 'completed')),
	in_scope_json TEXT NOT NULL DEFAULT '[]',
	out_of_scope_json TEXT NOT NULL DEFAULT '[]',
	constraints_json TEXT NOT NULL DEFAULT '[]',
	references_json TEXT NOT NULL DEFAULT '[]',
	created_at INTEGER NOT NULL DEFAULT (unixepoch()),
	updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
	kind TEXT NOT NULL DEFAULT '',
	parent_plan_key TEXT NOT NULL DEFAULT '',
	external_refs_json TEXT NOT NULL DEFAULT '[]',
	discovered_paths_json TEXT NOT NULL DEFAULT '[]',
	UNIQUE (project_id, plan_key)
)`); execErr != nil {
		t.Fatalf("create legacy work plans table: %v", execErr)
	}

	if _, execErr := db.ExecContext(ctx, `
CREATE TABLE awm_work_plan_tasks (
	task_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	plan_key TEXT NOT NULL,
	task_key TEXT NOT NULL,
	summary TEXT NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('pending', 'in_progress', 'blocked', 'completed')),
	depends_on_json TEXT NOT NULL DEFAULT '[]',
	acceptance_criteria_json TEXT NOT NULL DEFAULT '[]',
	references_json TEXT NOT NULL DEFAULT '[]',
	blocked_reason TEXT NOT NULL DEFAULT '',
	outcome TEXT NOT NULL DEFAULT '',
	evidence_json TEXT NOT NULL DEFAULT '[]',
	created_at INTEGER NOT NULL DEFAULT (unixepoch()),
	updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
	parent_task_key TEXT NOT NULL DEFAULT '',
	external_refs_json TEXT NOT NULL DEFAULT '[]',
	UNIQUE (project_id, plan_key, task_key),
	FOREIGN KEY (project_id, plan_key) REFERENCES awm_work_plans (project_id, plan_key) ON DELETE CASCADE
)`); execErr != nil {
		t.Fatalf("create legacy work plan tasks table: %v", execErr)
	}

	// The remaining historical tables 0016+ migrations alter; a real pre-0013
	// database has these from migrations 0001, 0007, and 0010.
	if _, execErr := db.ExecContext(ctx, `
CREATE TABLE awm_runs (
	run_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	request_id TEXT NOT NULL DEFAULT '',
	receipt_id TEXT NOT NULL,
	status TEXT NOT NULL,
	files_changed_json TEXT NOT NULL DEFAULT '[]',
	outcome TEXT NOT NULL DEFAULT '',
	summary_json TEXT NOT NULL DEFAULT '{}',
	created_at INTEGER NOT NULL DEFAULT (unixepoch())
)`); execErr != nil {
		t.Fatalf("create legacy runs table: %v", execErr)
	}
	if _, execErr := db.ExecContext(ctx, `
CREATE TABLE awm_verification_batches (
	batch_run_id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	receipt_id TEXT NOT NULL DEFAULT '',
	plan_key TEXT NOT NULL DEFAULT '',
	phase TEXT NOT NULL DEFAULT '',
	tests_source_path TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	passed INTEGER NOT NULL CHECK (passed IN (0, 1)),
	selected_test_ids_json TEXT NOT NULL DEFAULT '[]',
	created_at INTEGER NOT NULL DEFAULT (unixepoch())
)`); execErr != nil {
		t.Fatalf("create legacy verification batches table: %v", execErr)
	}
	if _, execErr := db.ExecContext(ctx, `
CREATE TABLE awm_review_attempts (
	attempt_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	receipt_id TEXT NOT NULL,
	plan_key TEXT NOT NULL DEFAULT '',
	review_key TEXT NOT NULL,
	summary TEXT NOT NULL DEFAULT '',
	fingerprint TEXT NOT NULL,
	status TEXT NOT NULL,
	passed INTEGER NOT NULL CHECK (passed IN (0, 1)),
	outcome TEXT NOT NULL DEFAULT '',
	workflow_source_path TEXT NOT NULL DEFAULT '',
	command_argv_json TEXT NOT NULL DEFAULT '[]',
	command_cwd TEXT NOT NULL DEFAULT '',
	timeout_sec INTEGER NOT NULL DEFAULT 0,
	exit_code INTEGER NULL,
	timed_out INTEGER NOT NULL DEFAULT 0 CHECK (timed_out IN (0, 1)),
	stdout_excerpt TEXT NOT NULL DEFAULT '',
	stderr_excerpt TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL DEFAULT (unixepoch())
)`); execErr != nil {
		t.Fatalf("create legacy review attempts table: %v", execErr)
	}

	if _, execErr := db.ExecContext(ctx, `
INSERT INTO awm_receipts (
	receipt_id,
	project_id,
	task_text,
	phase,
	resolved_tags_json,
	pointer_keys_json,
	pointer_paths_json,
	initial_scope_paths_json,
	baseline_paths_json,
	baseline_captured,
	memory_ids_json,
	summary_json,
	created_at
) VALUES (?, ?, ?, ?, '[]', '[]', '[]', '[]', '[]', 0, '[]', '{}', unixepoch())
`, "receipt.complete", "project.alpha", "seed receipt", "execute"); execErr != nil {
		t.Fatalf("seed receipt: %v", execErr)
	}

	if _, execErr := db.ExecContext(ctx, `
INSERT INTO awm_work_items (
	project_id,
	receipt_id,
	item_key,
	status,
	created_at,
	updated_at
) VALUES (?, ?, ?, 'completed', unixepoch(), unixepoch())
`, "project.alpha", "receipt.complete", "verify:tests"); execErr != nil {
		t.Fatalf("seed completed work item: %v", execErr)
	}

	if _, execErr := db.ExecContext(ctx, `
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
	discovered_paths_json,
	created_at,
	updated_at
) VALUES (?, ?, ?, ?, ?, 'completed', 'completed', 'completed', 'completed', '[]', '[]', '[]', '[]', '', '', '[]', '[]', unixepoch(), unixepoch())
`, "project.alpha", "plan:receipt.complete", "receipt.complete", "Complete migration", "Rewrite storage status"); execErr != nil {
		t.Fatalf("seed completed work plan: %v", execErr)
	}

	if _, execErr := db.ExecContext(ctx, `
INSERT INTO awm_work_plan_tasks (
	project_id,
	plan_key,
	task_key,
	summary,
	status,
	depends_on_json,
	acceptance_criteria_json,
	references_json,
	blocked_reason,
	outcome,
	evidence_json,
	parent_task_key,
	external_refs_json,
	created_at,
	updated_at
) VALUES (?, ?, ?, ?, 'completed', '[]', '[]', '[]', '', '', '[]', '', '[]', unixepoch(), unixepoch())
`, "project.alpha", "plan:receipt.complete", "verify:tests", "Verification complete"); execErr != nil {
		t.Fatalf("seed completed work plan task: %v", execErr)
	}

	if closeErr := db.Close(); closeErr != nil {
		t.Fatalf("close pre-migration db: %v", closeErr)
	}
	db = nil

	repo, err := New(ctx, Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open migrated repository: %v", err)
	}
	defer func() { _ = repo.Close() }()

	var workItemStatus string
	if err := repo.db.QueryRowContext(ctx, `
SELECT status
FROM awm_work_items
WHERE project_id = ? AND receipt_id = ? AND item_key = ?
`, "project.alpha", "receipt.complete", "verify:tests").Scan(&workItemStatus); err != nil {
		t.Fatalf("query migrated work item status: %v", err)
	}
	if workItemStatus != "complete" {
		t.Fatalf("unexpected migrated work item status: got %q want %q", workItemStatus, "complete")
	}

	var (
		planStatus string
		stageSpec  string
		stageRef   string
		stageImpl  string
	)
	if err := repo.db.QueryRowContext(ctx, `
SELECT status, stage_spec_outline, stage_refined_spec, stage_implementation_plan
FROM awm_work_plans
WHERE project_id = ? AND plan_key = ?
`, "project.alpha", "plan:receipt.complete").Scan(&planStatus, &stageSpec, &stageRef, &stageImpl); err != nil {
		t.Fatalf("query migrated work plan status: %v", err)
	}
	if planStatus != "complete" || stageSpec != "complete" || stageRef != "complete" || stageImpl != "complete" {
		t.Fatalf("unexpected migrated work plan statuses: plan=%q spec=%q refined=%q impl=%q", planStatus, stageSpec, stageRef, stageImpl)
	}

	var taskStatus string
	if err := repo.db.QueryRowContext(ctx, `
SELECT status
FROM awm_work_plan_tasks
WHERE project_id = ? AND plan_key = ? AND task_key = ?
`, "project.alpha", "plan:receipt.complete", "verify:tests").Scan(&taskStatus); err != nil {
		t.Fatalf("query migrated work plan task status: %v", err)
	}
	if taskStatus != "complete" {
		t.Fatalf("unexpected migrated work plan task status: got %q want %q", taskStatus, "complete")
	}

	if _, err := repo.db.ExecContext(ctx, `
INSERT INTO awm_work_items (
	project_id,
	receipt_id,
	item_key,
	status,
	created_at,
	updated_at
) VALUES (?, ?, ?, 'superseded', unixepoch(), unixepoch())
`, "project.alpha", "receipt.complete", "verify:diff-review"); err != nil {
		t.Fatalf("expected superseded work item status to be accepted after migration: %v", err)
	}

	if _, err := repo.db.ExecContext(ctx, `
UPDATE awm_work_plans
SET status = 'superseded', stage_implementation_plan = 'superseded'
WHERE project_id = ? AND plan_key = ?
`, "project.alpha", "plan:receipt.complete"); err != nil {
		t.Fatalf("expected superseded work plan status to be accepted after migration: %v", err)
	}

	if _, err := repo.db.ExecContext(ctx, `
UPDATE awm_work_plan_tasks
SET status = 'superseded'
WHERE project_id = ? AND plan_key = ? AND task_key = ?
`, "project.alpha", "plan:receipt.complete", "verify:tests"); err != nil {
		t.Fatalf("expected superseded work plan task status to be accepted after migration: %v", err)
	}
}
