CREATE INDEX IF NOT EXISTS idx_awm_runs_project_receipt_created
	ON awm_runs (project_id, receipt_id, created_at DESC, run_id DESC);
