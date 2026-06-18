ALTER TABLE awm_receipts
	ADD COLUMN IF NOT EXISTS initial_scope_paths TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[];

ALTER TABLE awm_receipts
	ADD COLUMN IF NOT EXISTS baseline_paths_json JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE awm_receipts
	ADD COLUMN IF NOT EXISTS baseline_captured BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE awm_receipts
SET initial_scope_paths = pointer_paths
WHERE COALESCE(array_length(initial_scope_paths, 1), 0) = 0
	AND COALESCE(array_length(pointer_paths, 1), 0) > 0;

ALTER TABLE awm_work_plans
	ADD COLUMN IF NOT EXISTS discovered_paths TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[];
