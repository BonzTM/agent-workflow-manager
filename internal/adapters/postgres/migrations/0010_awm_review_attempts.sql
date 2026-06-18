CREATE TABLE IF NOT EXISTS awm_review_attempts (
	attempt_id BIGSERIAL PRIMARY KEY,
	project_id TEXT NOT NULL,
	receipt_id TEXT NOT NULL REFERENCES awm_receipts (receipt_id) ON DELETE CASCADE,
	plan_key TEXT NOT NULL DEFAULT '',
	review_key TEXT NOT NULL,
	summary TEXT NOT NULL DEFAULT '',
	fingerprint TEXT NOT NULL,
	status TEXT NOT NULL,
	passed BOOLEAN NOT NULL DEFAULT FALSE,
	outcome TEXT NOT NULL DEFAULT '',
	workflow_source_path TEXT NOT NULL DEFAULT '',
	command_argv TEXT[] NOT NULL DEFAULT '{}',
	command_cwd TEXT NOT NULL DEFAULT '',
	timeout_sec INTEGER NOT NULL DEFAULT 0,
	exit_code INTEGER NULL,
	timed_out BOOLEAN NOT NULL DEFAULT FALSE,
	stdout_excerpt TEXT NOT NULL DEFAULT '',
	stderr_excerpt TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_awm_review_attempts_project_receipt_key_created
	ON awm_review_attempts (project_id, receipt_id, review_key, created_at DESC, attempt_id DESC);

CREATE INDEX IF NOT EXISTS idx_awm_review_attempts_project_receipt_fingerprint
	ON awm_review_attempts (project_id, receipt_id, review_key, fingerprint);
