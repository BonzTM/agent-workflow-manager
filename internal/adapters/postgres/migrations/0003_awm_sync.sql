ALTER TABLE awm_pointers
	ADD COLUMN IF NOT EXISTS content_hash TEXT;

CREATE INDEX IF NOT EXISTS idx_awm_pointers_project_path
	ON awm_pointers (project_id, path);

CREATE TABLE IF NOT EXISTS awm_pointer_candidates (
	candidate_id BIGSERIAL PRIMARY KEY,
	project_id TEXT NOT NULL,
	path TEXT NOT NULL,
	content_hash TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE (project_id, path)
);

CREATE INDEX IF NOT EXISTS idx_awm_pointer_candidates_project_created
	ON awm_pointer_candidates (project_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_awm_pointer_candidates_project_updated
	ON awm_pointer_candidates (project_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_awm_pointer_candidates_project_hash
	ON awm_pointer_candidates (project_id, content_hash);
