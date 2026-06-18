CREATE TABLE IF NOT EXISTS awm_verification_batches (
	batch_run_id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	receipt_id TEXT NOT NULL DEFAULT '',
	plan_key TEXT NOT NULL DEFAULT '',
	phase TEXT NOT NULL DEFAULT '',
	tests_source_path TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL CHECK (status IN ('passed', 'failed')),
	passed BOOLEAN NOT NULL DEFAULT FALSE,
	selected_test_ids TEXT[] NOT NULL DEFAULT '{}',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_awm_verification_batches_project_created
	ON awm_verification_batches (project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_verification_batches_project_receipt_created
	ON awm_verification_batches (project_id, receipt_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_verification_batches_project_plan_created
	ON awm_verification_batches (project_id, plan_key, created_at DESC);

CREATE TABLE IF NOT EXISTS awm_verification_results (
	result_id BIGSERIAL PRIMARY KEY,
	batch_run_id TEXT NOT NULL REFERENCES awm_verification_batches (batch_run_id) ON DELETE CASCADE,
	project_id TEXT NOT NULL,
	test_id TEXT NOT NULL,
	definition_hash TEXT NOT NULL,
	summary TEXT NOT NULL DEFAULT '',
	command_argv TEXT[] NOT NULL DEFAULT '{}',
	command_cwd TEXT NOT NULL DEFAULT '.',
	timeout_sec INTEGER NOT NULL DEFAULT 300,
	expected_exit_code INTEGER NOT NULL DEFAULT 0,
	selection_reasons TEXT[] NOT NULL DEFAULT '{}',
	status TEXT NOT NULL CHECK (status IN ('passed', 'failed', 'timed_out', 'errored', 'skipped')),
	exit_code INTEGER NULL,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	stdout_excerpt TEXT NOT NULL DEFAULT '',
	stderr_excerpt TEXT NOT NULL DEFAULT '',
	started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	finished_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_awm_verification_results_batch_started
	ON awm_verification_results (batch_run_id, started_at, result_id);
CREATE INDEX IF NOT EXISTS idx_awm_verification_results_project_test_started
	ON awm_verification_results (project_id, test_id, started_at DESC);
