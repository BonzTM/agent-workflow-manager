package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type migration struct {
	Name string
	SQL  string
}

var migrations = []migration{
	{
		Name: "0001_awm_foundation.sql",
		SQL: `
CREATE TABLE IF NOT EXISTS awm_pointers (
	pointer_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	pointer_key TEXT NOT NULL,
	path TEXT NOT NULL,
	anchor TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL,
	label TEXT NOT NULL,
	description TEXT NOT NULL,
	tags_json TEXT NOT NULL DEFAULT '[]',
	is_rule INTEGER NOT NULL DEFAULT 0 CHECK (is_rule IN (0, 1)),
	is_stale INTEGER NOT NULL DEFAULT 0 CHECK (is_stale IN (0, 1)),
	stale_at INTEGER NULL,
	content_hash TEXT NULL,
	updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
	UNIQUE (project_id, pointer_key)
);

CREATE INDEX IF NOT EXISTS idx_awm_pointers_project_updated
	ON awm_pointers (project_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_pointers_project_path
	ON awm_pointers (project_id, path);

CREATE TABLE IF NOT EXISTS awm_pointer_links (
	project_id TEXT NOT NULL,
	from_key TEXT NOT NULL,
	to_key TEXT NOT NULL,
	created_at INTEGER NOT NULL DEFAULT (unixepoch()),
	PRIMARY KEY (project_id, from_key, to_key),
	FOREIGN KEY (project_id, from_key) REFERENCES awm_pointers (project_id, pointer_key) ON DELETE CASCADE,
	FOREIGN KEY (project_id, to_key) REFERENCES awm_pointers (project_id, pointer_key) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_awm_pointer_links_project_to_key
	ON awm_pointer_links (project_id, to_key);

CREATE TABLE IF NOT EXISTS awm_memories (
	memory_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	category TEXT NOT NULL,
	subject TEXT NOT NULL,
	content TEXT NOT NULL,
	confidence INTEGER NOT NULL CHECK (confidence BETWEEN 1 AND 5),
	tags_json TEXT NOT NULL DEFAULT '[]',
	related_pointer_keys_json TEXT NOT NULL DEFAULT '[]',
	evidence_pointer_keys_json TEXT NOT NULL DEFAULT '[]',
	dedupe_key TEXT NULL,
	active INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
	created_at INTEGER NOT NULL DEFAULT (unixepoch()),
	updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE INDEX IF NOT EXISTS idx_awm_memories_project_active
	ON awm_memories (project_id, active, updated_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS uq_awm_memories_project_dedupe_active
	ON awm_memories (project_id, dedupe_key)
	WHERE active = 1 AND dedupe_key IS NOT NULL;

CREATE TABLE IF NOT EXISTS awm_receipts (
	receipt_id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	task_text TEXT NOT NULL DEFAULT '',
	phase TEXT NOT NULL DEFAULT 'execute',
	resolved_tags_json TEXT NOT NULL DEFAULT '[]',
	pointer_keys_json TEXT NOT NULL DEFAULT '[]',
	memory_ids_json TEXT NOT NULL DEFAULT '[]',
	summary_json TEXT NOT NULL DEFAULT '{}',
	created_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE INDEX IF NOT EXISTS idx_awm_receipts_project_created
	ON awm_receipts (project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS awm_runs (
	run_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	request_id TEXT NOT NULL DEFAULT '',
	receipt_id TEXT NOT NULL,
	status TEXT NOT NULL,
	files_changed_json TEXT NOT NULL DEFAULT '[]',
	outcome TEXT NOT NULL DEFAULT '',
	summary_json TEXT NOT NULL DEFAULT '{}',
	created_at INTEGER NOT NULL DEFAULT (unixepoch()),
	FOREIGN KEY (receipt_id) REFERENCES awm_receipts (receipt_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_awm_runs_project_created
	ON awm_runs (project_id, created_at DESC);
`,
	},
	{
		Name: "0002_awm_propose_memory.sql",
		SQL: `
CREATE TABLE IF NOT EXISTS awm_memory_candidates (
	candidate_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	receipt_id TEXT NOT NULL,
	category TEXT NOT NULL CHECK (category IN ('decision', 'gotcha', 'pattern', 'preference')),
	subject TEXT NOT NULL,
	content TEXT NOT NULL,
	confidence INTEGER NOT NULL CHECK (confidence BETWEEN 1 AND 5),
	tags_json TEXT NOT NULL DEFAULT '[]',
	related_pointer_keys_json TEXT NOT NULL DEFAULT '[]',
	evidence_pointer_keys_json TEXT NOT NULL CHECK (
		json_valid(evidence_pointer_keys_json)
		AND json_type(evidence_pointer_keys_json) = 'array'
		AND json_array_length(evidence_pointer_keys_json) >= 1
	),
	dedupe_key TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'promoted', 'rejected')),
	promoted_memory_id INTEGER NULL,
	hard_passed INTEGER NOT NULL CHECK (hard_passed IN (0, 1)),
	soft_passed INTEGER NOT NULL CHECK (soft_passed IN (0, 1)),
	validation_errors_json TEXT NOT NULL DEFAULT '[]',
	validation_warnings_json TEXT NOT NULL DEFAULT '[]',
	auto_promote INTEGER NOT NULL DEFAULT 1 CHECK (auto_promote IN (0, 1)),
	promotable INTEGER NOT NULL DEFAULT 0 CHECK (promotable IN (0, 1)),
	created_at INTEGER NOT NULL DEFAULT (unixepoch()),
	updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
	FOREIGN KEY (promoted_memory_id) REFERENCES awm_memories (memory_id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_awm_memory_candidates_project_created
	ON awm_memory_candidates (project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_memory_candidates_project_status_created
	ON awm_memory_candidates (project_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_memory_candidates_receipt_created
	ON awm_memory_candidates (receipt_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_memory_candidates_project_dedupe
	ON awm_memory_candidates (project_id, dedupe_key);
`,
	},
	{
		Name: "0003_awm_sync.sql",
		SQL: `
CREATE TABLE IF NOT EXISTS awm_pointer_candidates (
	candidate_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	path TEXT NOT NULL,
	content_hash TEXT NOT NULL,
	created_at INTEGER NOT NULL DEFAULT (unixepoch()),
	updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
	last_seen_at INTEGER NOT NULL DEFAULT (unixepoch()),
	UNIQUE (project_id, path)
);

CREATE INDEX IF NOT EXISTS idx_awm_pointer_candidates_project_created
	ON awm_pointer_candidates (project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_pointer_candidates_project_updated
	ON awm_pointer_candidates (project_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_pointer_candidates_project_hash
	ON awm_pointer_candidates (project_id, content_hash);
`,
	},
	{
		Name: "0004_awm_work_items.sql",
		SQL: `
CREATE TABLE IF NOT EXISTS awm_work_items (
	work_item_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	receipt_id TEXT NOT NULL,
	item_key TEXT NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('pending', 'in_progress', 'blocked', 'complete')),
	created_at INTEGER NOT NULL DEFAULT (unixepoch()),
	updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
	UNIQUE (project_id, receipt_id, item_key),
	FOREIGN KEY (receipt_id) REFERENCES awm_receipts (receipt_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_awm_work_items_project_receipt
	ON awm_work_items (project_id, receipt_id, item_key);
CREATE INDEX IF NOT EXISTS idx_awm_work_items_project_receipt_status
	ON awm_work_items (project_id, receipt_id, status, item_key);
CREATE INDEX IF NOT EXISTS idx_awm_work_items_project_receipt_updated
	ON awm_work_items (project_id, receipt_id, updated_at DESC);
`,
	},
	{
		Name: "0005_awm_work_plans.sql",
		SQL: `
CREATE TABLE IF NOT EXISTS awm_work_plans (
	plan_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	plan_key TEXT NOT NULL,
	receipt_id TEXT NULL,
	title TEXT NOT NULL DEFAULT '',
	objective TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL CHECK (status IN ('pending', 'in_progress', 'blocked', 'complete')),
	stage_spec_outline TEXT NOT NULL DEFAULT 'pending' CHECK (stage_spec_outline IN ('pending', 'in_progress', 'blocked', 'complete')),
	stage_refined_spec TEXT NOT NULL DEFAULT 'pending' CHECK (stage_refined_spec IN ('pending', 'in_progress', 'blocked', 'complete')),
	stage_implementation_plan TEXT NOT NULL DEFAULT 'pending' CHECK (stage_implementation_plan IN ('pending', 'in_progress', 'blocked', 'complete')),
	in_scope_json TEXT NOT NULL DEFAULT '[]',
	out_of_scope_json TEXT NOT NULL DEFAULT '[]',
	constraints_json TEXT NOT NULL DEFAULT '[]',
	references_json TEXT NOT NULL DEFAULT '[]',
	created_at INTEGER NOT NULL DEFAULT (unixepoch()),
	updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
	UNIQUE (project_id, plan_key)
);

CREATE INDEX IF NOT EXISTS idx_awm_work_plans_project_status_updated
	ON awm_work_plans (project_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_work_plans_project_receipt_updated
	ON awm_work_plans (project_id, receipt_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS awm_work_plan_tasks (
	task_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	plan_key TEXT NOT NULL,
	task_key TEXT NOT NULL,
	summary TEXT NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('pending', 'in_progress', 'blocked', 'complete')),
	depends_on_json TEXT NOT NULL DEFAULT '[]',
	acceptance_criteria_json TEXT NOT NULL DEFAULT '[]',
	references_json TEXT NOT NULL DEFAULT '[]',
	blocked_reason TEXT NOT NULL DEFAULT '',
	outcome TEXT NOT NULL DEFAULT '',
	evidence_json TEXT NOT NULL DEFAULT '[]',
	created_at INTEGER NOT NULL DEFAULT (unixepoch()),
	updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
	UNIQUE (project_id, plan_key, task_key),
	FOREIGN KEY (project_id, plan_key) REFERENCES awm_work_plans (project_id, plan_key) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_awm_work_plan_tasks_project_plan_status
	ON awm_work_plan_tasks (project_id, plan_key, status, task_key);
CREATE INDEX IF NOT EXISTS idx_awm_work_plan_tasks_project_plan_updated
	ON awm_work_plan_tasks (project_id, plan_key, updated_at DESC);
`,
	},
	{
		Name: "0006_awm_work_plan_hierarchy.sql",
		SQL: `
ALTER TABLE awm_work_plans
	ADD COLUMN kind TEXT NOT NULL DEFAULT '';
ALTER TABLE awm_work_plans
	ADD COLUMN parent_plan_key TEXT NOT NULL DEFAULT '';
ALTER TABLE awm_work_plans
	ADD COLUMN external_refs_json TEXT NOT NULL DEFAULT '[]';

CREATE INDEX IF NOT EXISTS idx_awm_work_plans_project_parent_updated
	ON awm_work_plans (project_id, parent_plan_key, updated_at DESC);

ALTER TABLE awm_work_plan_tasks
	ADD COLUMN parent_task_key TEXT NOT NULL DEFAULT '';
ALTER TABLE awm_work_plan_tasks
	ADD COLUMN external_refs_json TEXT NOT NULL DEFAULT '[]';

CREATE INDEX IF NOT EXISTS idx_awm_work_plan_tasks_project_plan_parent
	ON awm_work_plan_tasks (project_id, plan_key, parent_task_key, task_key);
`,
	},
	{
		Name: "0007_awm_verification_runs.sql",
		SQL: `
CREATE TABLE IF NOT EXISTS awm_verification_batches (
	batch_run_id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	receipt_id TEXT NOT NULL DEFAULT '',
	plan_key TEXT NOT NULL DEFAULT '',
	phase TEXT NOT NULL DEFAULT '',
	tests_source_path TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL CHECK (status IN ('passed', 'failed')),
	passed INTEGER NOT NULL DEFAULT 0 CHECK (passed IN (0, 1)),
	selected_test_ids_json TEXT NOT NULL DEFAULT '[]',
	created_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE INDEX IF NOT EXISTS idx_awm_verification_batches_project_created
	ON awm_verification_batches (project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_verification_batches_project_receipt_created
	ON awm_verification_batches (project_id, receipt_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_verification_batches_project_plan_created
	ON awm_verification_batches (project_id, plan_key, created_at DESC);

CREATE TABLE IF NOT EXISTS awm_verification_results (
	result_id INTEGER PRIMARY KEY AUTOINCREMENT,
	batch_run_id TEXT NOT NULL,
	project_id TEXT NOT NULL,
	test_id TEXT NOT NULL,
	definition_hash TEXT NOT NULL,
	summary TEXT NOT NULL DEFAULT '',
	command_argv_json TEXT NOT NULL DEFAULT '[]',
	command_cwd TEXT NOT NULL DEFAULT '.',
	timeout_sec INTEGER NOT NULL DEFAULT 300,
	expected_exit_code INTEGER NOT NULL DEFAULT 0,
	selection_reasons_json TEXT NOT NULL DEFAULT '[]',
	status TEXT NOT NULL CHECK (status IN ('passed', 'failed', 'timed_out', 'errored', 'skipped')),
	exit_code INTEGER NULL,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	stdout_excerpt TEXT NOT NULL DEFAULT '',
	stderr_excerpt TEXT NOT NULL DEFAULT '',
	started_at INTEGER NOT NULL DEFAULT (unixepoch()),
	finished_at INTEGER NOT NULL DEFAULT (unixepoch()),
	FOREIGN KEY (batch_run_id) REFERENCES awm_verification_batches (batch_run_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_awm_verification_results_batch_started
	ON awm_verification_results (batch_run_id, started_at, result_id);
CREATE INDEX IF NOT EXISTS idx_awm_verification_results_project_test_started
	ON awm_verification_results (project_id, test_id, started_at DESC);
`,
	},
	{
		Name: "0008_awm_sqlite_parity.sql",
		SQL: `
CREATE TABLE IF NOT EXISTS awm_pointer_links_new (
	project_id TEXT NOT NULL,
	from_key TEXT NOT NULL,
	to_key TEXT NOT NULL,
	created_at INTEGER NOT NULL DEFAULT (unixepoch()),
	PRIMARY KEY (project_id, from_key, to_key),
	FOREIGN KEY (project_id, from_key) REFERENCES awm_pointers (project_id, pointer_key) ON DELETE CASCADE,
	FOREIGN KEY (project_id, to_key) REFERENCES awm_pointers (project_id, pointer_key) ON DELETE CASCADE
);

INSERT INTO awm_pointer_links_new (project_id, from_key, to_key, created_at)
SELECT l.project_id, l.from_key, l.to_key, l.created_at
FROM awm_pointer_links l
WHERE EXISTS (
	SELECT 1 FROM awm_pointers p
	WHERE p.project_id = l.project_id AND p.pointer_key = l.from_key
)
AND EXISTS (
	SELECT 1 FROM awm_pointers p
	WHERE p.project_id = l.project_id AND p.pointer_key = l.to_key
);

DROP TABLE awm_pointer_links;
ALTER TABLE awm_pointer_links_new RENAME TO awm_pointer_links;

CREATE INDEX IF NOT EXISTS idx_awm_pointer_links_project_to_key
	ON awm_pointer_links (project_id, to_key);

CREATE TABLE IF NOT EXISTS awm_memory_candidates_new (
	candidate_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	receipt_id TEXT NOT NULL,
	category TEXT NOT NULL CHECK (category IN ('decision', 'gotcha', 'pattern', 'preference')),
	subject TEXT NOT NULL,
	content TEXT NOT NULL,
	confidence INTEGER NOT NULL CHECK (confidence BETWEEN 1 AND 5),
	tags_json TEXT NOT NULL DEFAULT '[]',
	related_pointer_keys_json TEXT NOT NULL DEFAULT '[]',
	evidence_pointer_keys_json TEXT NOT NULL CHECK (
		json_valid(evidence_pointer_keys_json)
		AND json_type(evidence_pointer_keys_json) = 'array'
		AND json_array_length(evidence_pointer_keys_json) >= 1
	),
	dedupe_key TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'promoted', 'rejected')),
	promoted_memory_id INTEGER NULL,
	hard_passed INTEGER NOT NULL CHECK (hard_passed IN (0, 1)),
	soft_passed INTEGER NOT NULL CHECK (soft_passed IN (0, 1)),
	validation_errors_json TEXT NOT NULL DEFAULT '[]',
	validation_warnings_json TEXT NOT NULL DEFAULT '[]',
	auto_promote INTEGER NOT NULL DEFAULT 1 CHECK (auto_promote IN (0, 1)),
	promotable INTEGER NOT NULL DEFAULT 0 CHECK (promotable IN (0, 1)),
	created_at INTEGER NOT NULL DEFAULT (unixepoch()),
	updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
	FOREIGN KEY (promoted_memory_id) REFERENCES awm_memories (memory_id) ON DELETE SET NULL
);

INSERT INTO awm_memory_candidates_new (
	candidate_id,
	project_id,
	receipt_id,
	category,
	subject,
	content,
	confidence,
	tags_json,
	related_pointer_keys_json,
	evidence_pointer_keys_json,
	dedupe_key,
	status,
	promoted_memory_id,
	hard_passed,
	soft_passed,
	validation_errors_json,
	validation_warnings_json,
	auto_promote,
	promotable,
	created_at,
	updated_at
)
SELECT
	candidate_id,
	project_id,
	receipt_id,
	category,
	subject,
	content,
	confidence,
	tags_json,
	related_pointer_keys_json,
	evidence_pointer_keys_json,
	dedupe_key,
	status,
	promoted_memory_id,
	hard_passed,
	soft_passed,
	validation_errors_json,
	validation_warnings_json,
	auto_promote,
	promotable,
	created_at,
	updated_at
FROM awm_memory_candidates
WHERE json_valid(evidence_pointer_keys_json)
  AND json_type(evidence_pointer_keys_json) = 'array'
  AND json_array_length(evidence_pointer_keys_json) >= 1;

DROP TABLE awm_memory_candidates;
ALTER TABLE awm_memory_candidates_new RENAME TO awm_memory_candidates;

CREATE INDEX IF NOT EXISTS idx_awm_memory_candidates_project_created
	ON awm_memory_candidates (project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_memory_candidates_project_status_created
	ON awm_memory_candidates (project_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_memory_candidates_receipt_created
	ON awm_memory_candidates (receipt_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_memory_candidates_project_dedupe
	ON awm_memory_candidates (project_id, dedupe_key);
`,
	},
	{
		Name: "0009_awm_run_history_indexes.sql",
		SQL: `
CREATE INDEX IF NOT EXISTS idx_awm_runs_project_receipt_created
	ON awm_runs (project_id, receipt_id, created_at DESC, run_id DESC);
`,
	},
	{
		Name: "0010_awm_review_attempts.sql",
		SQL: `
CREATE TABLE IF NOT EXISTS awm_review_attempts (
	attempt_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	receipt_id TEXT NOT NULL,
	plan_key TEXT NOT NULL DEFAULT '',
	review_key TEXT NOT NULL,
	summary TEXT NOT NULL DEFAULT '',
	fingerprint TEXT NOT NULL,
	status TEXT NOT NULL,
	passed INTEGER NOT NULL DEFAULT 0,
	outcome TEXT NOT NULL DEFAULT '',
	workflow_source_path TEXT NOT NULL DEFAULT '',
	command_argv_json TEXT NOT NULL DEFAULT '[]',
	command_cwd TEXT NOT NULL DEFAULT '',
	timeout_sec INTEGER NOT NULL DEFAULT 0,
	exit_code INTEGER NULL,
	timed_out INTEGER NOT NULL DEFAULT 0,
	stdout_excerpt TEXT NOT NULL DEFAULT '',
	stderr_excerpt TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL DEFAULT (unixepoch()),
	FOREIGN KEY (receipt_id) REFERENCES awm_receipts (receipt_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_awm_review_attempts_project_receipt_key_created
	ON awm_review_attempts (project_id, receipt_id, review_key, created_at DESC, attempt_id DESC);

CREATE INDEX IF NOT EXISTS idx_awm_review_attempts_project_receipt_fingerprint
	ON awm_review_attempts (project_id, receipt_id, review_key, fingerprint);
`,
	},
	{
		Name: "0011_awm_receipt_scope_pointer_paths.sql",
		SQL: `
ALTER TABLE awm_receipts
	ADD COLUMN pointer_paths_json TEXT NOT NULL DEFAULT '[]';

UPDATE awm_receipts AS r
SET pointer_paths_json = COALESCE((
	SELECT json_group_array(path)
	FROM (
		SELECT DISTINCT p.path AS path
		FROM json_each(r.pointer_keys_json) AS pk
		JOIN awm_pointers p
			ON p.project_id = r.project_id
			AND p.pointer_key = pk.value
		WHERE p.path IS NOT NULL
		ORDER BY p.path
	)
), '[]');
`,
	},
	{
		Name: "0012_awm_initial_scope_and_baselines.sql",
		SQL: `
ALTER TABLE awm_receipts
	ADD COLUMN initial_scope_paths_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE awm_receipts
	ADD COLUMN baseline_paths_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE awm_receipts
	ADD COLUMN baseline_captured INTEGER NOT NULL DEFAULT 0 CHECK (baseline_captured IN (0, 1));

UPDATE awm_receipts
SET initial_scope_paths_json = COALESCE(NULLIF(pointer_paths_json, ''), '[]')
WHERE COALESCE(initial_scope_paths_json, '[]') IN ('', '[]');

ALTER TABLE awm_work_plans
	ADD COLUMN discovered_paths_json TEXT NOT NULL DEFAULT '[]';
`,
	},
	{
		Name: "0013_awm_complete_status.sql",
		SQL: `
PRAGMA foreign_keys = OFF;

ALTER TABLE awm_work_items RENAME TO awm_work_items_legacy_completed;

CREATE TABLE awm_work_items (
	work_item_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	receipt_id TEXT NOT NULL,
	item_key TEXT NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('pending', 'in_progress', 'blocked', 'complete')),
	created_at INTEGER NOT NULL DEFAULT (unixepoch()),
	updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
	UNIQUE (project_id, receipt_id, item_key),
	FOREIGN KEY (receipt_id) REFERENCES awm_receipts (receipt_id) ON DELETE CASCADE
);

INSERT INTO awm_work_items (
	work_item_id,
	project_id,
	receipt_id,
	item_key,
	status,
	created_at,
	updated_at
)
SELECT
	work_item_id,
	project_id,
	receipt_id,
	item_key,
	CASE WHEN status = 'completed' THEN 'complete' ELSE status END,
	created_at,
	updated_at
FROM awm_work_items_legacy_completed;

DROP TABLE awm_work_items_legacy_completed;

CREATE INDEX IF NOT EXISTS idx_awm_work_items_project_receipt
	ON awm_work_items (project_id, receipt_id, item_key);
CREATE INDEX IF NOT EXISTS idx_awm_work_items_project_receipt_status
	ON awm_work_items (project_id, receipt_id, status, item_key);
CREATE INDEX IF NOT EXISTS idx_awm_work_items_project_receipt_updated
	ON awm_work_items (project_id, receipt_id, updated_at DESC);

ALTER TABLE awm_work_plans RENAME TO awm_work_plans_legacy_completed;
ALTER TABLE awm_work_plan_tasks RENAME TO awm_work_plan_tasks_legacy_completed;

CREATE TABLE awm_work_plans (
	plan_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	plan_key TEXT NOT NULL,
	receipt_id TEXT NULL,
	title TEXT NOT NULL DEFAULT '',
	objective TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL CHECK (status IN ('pending', 'in_progress', 'blocked', 'complete')),
	stage_spec_outline TEXT NOT NULL DEFAULT 'pending' CHECK (stage_spec_outline IN ('pending', 'in_progress', 'blocked', 'complete')),
	stage_refined_spec TEXT NOT NULL DEFAULT 'pending' CHECK (stage_refined_spec IN ('pending', 'in_progress', 'blocked', 'complete')),
	stage_implementation_plan TEXT NOT NULL DEFAULT 'pending' CHECK (stage_implementation_plan IN ('pending', 'in_progress', 'blocked', 'complete')),
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
);

INSERT INTO awm_work_plans (
	plan_id,
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
	created_at,
	updated_at,
	kind,
	parent_plan_key,
	external_refs_json,
	discovered_paths_json
)
SELECT
	plan_id,
	project_id,
	plan_key,
	receipt_id,
	title,
	objective,
	CASE WHEN status = 'completed' THEN 'complete' ELSE status END,
	CASE WHEN stage_spec_outline = 'completed' THEN 'complete' ELSE stage_spec_outline END,
	CASE WHEN stage_refined_spec = 'completed' THEN 'complete' ELSE stage_refined_spec END,
	CASE WHEN stage_implementation_plan = 'completed' THEN 'complete' ELSE stage_implementation_plan END,
	in_scope_json,
	out_of_scope_json,
	constraints_json,
	references_json,
	created_at,
	updated_at,
	kind,
	parent_plan_key,
	external_refs_json,
	discovered_paths_json
FROM awm_work_plans_legacy_completed;

CREATE INDEX IF NOT EXISTS idx_awm_work_plans_project_status_updated
	ON awm_work_plans (project_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_work_plans_project_receipt_updated
	ON awm_work_plans (project_id, receipt_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_work_plans_project_parent_updated
	ON awm_work_plans (project_id, parent_plan_key, updated_at DESC);

CREATE TABLE awm_work_plan_tasks (
	task_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	plan_key TEXT NOT NULL,
	task_key TEXT NOT NULL,
	summary TEXT NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('pending', 'in_progress', 'blocked', 'complete')),
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
);

INSERT INTO awm_work_plan_tasks (
	task_id,
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
	created_at,
	updated_at,
	parent_task_key,
	external_refs_json
)
SELECT
	task_id,
	project_id,
	plan_key,
	task_key,
	summary,
	CASE WHEN status = 'completed' THEN 'complete' ELSE status END,
	depends_on_json,
	acceptance_criteria_json,
	references_json,
	blocked_reason,
	outcome,
	evidence_json,
	created_at,
	updated_at,
	parent_task_key,
	external_refs_json
FROM awm_work_plan_tasks_legacy_completed;

CREATE INDEX IF NOT EXISTS idx_awm_work_plan_tasks_project_plan_status
	ON awm_work_plan_tasks (project_id, plan_key, status, task_key);
CREATE INDEX IF NOT EXISTS idx_awm_work_plan_tasks_project_plan_updated
	ON awm_work_plan_tasks (project_id, plan_key, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_work_plan_tasks_project_plan_parent
	ON awm_work_plan_tasks (project_id, plan_key, parent_task_key, task_key);

DROP TABLE awm_work_plan_tasks_legacy_completed;
DROP TABLE awm_work_plans_legacy_completed;

PRAGMA foreign_keys = ON;
`,
	},
	{
		Name: "0014_awm_superseded_status.sql",
		SQL: `
PRAGMA foreign_keys = OFF;

ALTER TABLE awm_work_items RENAME TO awm_work_items_legacy_superseded;

CREATE TABLE awm_work_items (
	work_item_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	receipt_id TEXT NOT NULL,
	item_key TEXT NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('pending', 'in_progress', 'blocked', 'complete', 'superseded')),
	created_at INTEGER NOT NULL DEFAULT (unixepoch()),
	updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
	UNIQUE (project_id, receipt_id, item_key),
	FOREIGN KEY (receipt_id) REFERENCES awm_receipts (receipt_id) ON DELETE CASCADE
);

INSERT INTO awm_work_items (
	work_item_id,
	project_id,
	receipt_id,
	item_key,
	status,
	created_at,
	updated_at
)
SELECT
	work_item_id,
	project_id,
	receipt_id,
	item_key,
	status,
	created_at,
	updated_at
FROM awm_work_items_legacy_superseded;

DROP TABLE awm_work_items_legacy_superseded;

CREATE INDEX IF NOT EXISTS idx_awm_work_items_project_receipt
	ON awm_work_items (project_id, receipt_id, item_key);
CREATE INDEX IF NOT EXISTS idx_awm_work_items_project_receipt_status
	ON awm_work_items (project_id, receipt_id, status, item_key);
CREATE INDEX IF NOT EXISTS idx_awm_work_items_project_receipt_updated
	ON awm_work_items (project_id, receipt_id, updated_at DESC);

ALTER TABLE awm_work_plans RENAME TO awm_work_plans_legacy_superseded;
ALTER TABLE awm_work_plan_tasks RENAME TO awm_work_plan_tasks_legacy_superseded;

CREATE TABLE awm_work_plans (
	plan_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	plan_key TEXT NOT NULL,
	receipt_id TEXT NULL,
	title TEXT NOT NULL DEFAULT '',
	objective TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL CHECK (status IN ('pending', 'in_progress', 'blocked', 'complete', 'superseded')),
	stage_spec_outline TEXT NOT NULL DEFAULT 'pending' CHECK (stage_spec_outline IN ('pending', 'in_progress', 'blocked', 'complete', 'superseded')),
	stage_refined_spec TEXT NOT NULL DEFAULT 'pending' CHECK (stage_refined_spec IN ('pending', 'in_progress', 'blocked', 'complete', 'superseded')),
	stage_implementation_plan TEXT NOT NULL DEFAULT 'pending' CHECK (stage_implementation_plan IN ('pending', 'in_progress', 'blocked', 'complete', 'superseded')),
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
);

INSERT INTO awm_work_plans (
	plan_id,
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
	created_at,
	updated_at,
	kind,
	parent_plan_key,
	external_refs_json,
	discovered_paths_json
)
SELECT
	plan_id,
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
	created_at,
	updated_at,
	kind,
	parent_plan_key,
	external_refs_json,
	discovered_paths_json
FROM awm_work_plans_legacy_superseded;

CREATE INDEX IF NOT EXISTS idx_awm_work_plans_project_status_updated
	ON awm_work_plans (project_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_work_plans_project_receipt_updated
	ON awm_work_plans (project_id, receipt_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_work_plans_project_parent_updated
	ON awm_work_plans (project_id, parent_plan_key, updated_at DESC);

CREATE TABLE awm_work_plan_tasks (
	task_id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	plan_key TEXT NOT NULL,
	task_key TEXT NOT NULL,
	summary TEXT NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('pending', 'in_progress', 'blocked', 'complete', 'superseded')),
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
);

INSERT INTO awm_work_plan_tasks (
	task_id,
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
	created_at,
	updated_at,
	parent_task_key,
	external_refs_json
)
SELECT
	task_id,
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
	created_at,
	updated_at,
	parent_task_key,
	external_refs_json
FROM awm_work_plan_tasks_legacy_superseded;

CREATE INDEX IF NOT EXISTS idx_awm_work_plan_tasks_project_plan_status
	ON awm_work_plan_tasks (project_id, plan_key, status, task_key);
CREATE INDEX IF NOT EXISTS idx_awm_work_plan_tasks_project_plan_updated
	ON awm_work_plan_tasks (project_id, plan_key, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_work_plan_tasks_project_plan_parent
	ON awm_work_plan_tasks (project_id, plan_key, parent_task_key, task_key);

DROP TABLE awm_work_plan_tasks_legacy_superseded;
DROP TABLE awm_work_plans_legacy_superseded;

PRAGMA foreign_keys = ON;
`,
	},
	{
		Name: "0015_awm_drop_memory.sql",
		SQL: `
DROP TABLE IF EXISTS awm_memory_candidates;
DROP TABLE IF EXISTS awm_memories;

ALTER TABLE awm_receipts DROP COLUMN memory_ids_json;
`,
	},
	{
		Name: "0016_awm_actor_attribution.sql",
		SQL: `
ALTER TABLE awm_receipts ADD COLUMN actor_harness TEXT NOT NULL DEFAULT '';
ALTER TABLE awm_receipts ADD COLUMN actor_model TEXT NOT NULL DEFAULT '';
ALTER TABLE awm_receipts ADD COLUMN actor_session TEXT NOT NULL DEFAULT '';

ALTER TABLE awm_runs ADD COLUMN actor_harness TEXT NOT NULL DEFAULT '';
ALTER TABLE awm_runs ADD COLUMN actor_model TEXT NOT NULL DEFAULT '';
ALTER TABLE awm_runs ADD COLUMN actor_session TEXT NOT NULL DEFAULT '';

ALTER TABLE awm_verification_batches ADD COLUMN actor_harness TEXT NOT NULL DEFAULT '';
ALTER TABLE awm_verification_batches ADD COLUMN actor_model TEXT NOT NULL DEFAULT '';
ALTER TABLE awm_verification_batches ADD COLUMN actor_session TEXT NOT NULL DEFAULT '';

ALTER TABLE awm_review_attempts ADD COLUMN actor_harness TEXT NOT NULL DEFAULT '';
ALTER TABLE awm_review_attempts ADD COLUMN actor_model TEXT NOT NULL DEFAULT '';
ALTER TABLE awm_review_attempts ADD COLUMN actor_session TEXT NOT NULL DEFAULT '';
`,
	},
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("sqlite db is required")
	}

	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS awm_schema_migrations (
	migration_name TEXT PRIMARY KEY,
	applied_at INTEGER NOT NULL DEFAULT (unixepoch())
)`); err != nil {
		return fmt.Errorf("ensure schema migrations table: %w", err)
	}

	for _, migration := range migrations {
		var applied int
		if err := db.QueryRowContext(
			ctx,
			`SELECT COUNT(1) FROM awm_schema_migrations WHERE migration_name = ?`,
			migration.Name,
		).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", migration.Name, err)
		}
		if applied > 0 {
			continue
		}

		// Legacy on-disk SQLite databases can fail schema-altering migrations inside
		// an explicit transaction even when the same statements succeed normally.
		// Apply each migration directly and record completion afterward.
		if _, err := db.ExecContext(ctx, migration.SQL); err != nil {
			return fmt.Errorf("apply migration %s: %w", migration.Name, err)
		}
		if _, err := db.ExecContext(
			ctx,
			`INSERT OR IGNORE INTO awm_schema_migrations (migration_name) VALUES (?)`,
			migration.Name,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", migration.Name, err)
		}
	}

	return nil
}
