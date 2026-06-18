ALTER TABLE awm_memories
	ADD COLUMN IF NOT EXISTS dedupe_key TEXT;

ALTER TABLE awm_memories
	ADD COLUMN IF NOT EXISTS evidence_pointer_keys TEXT[] NOT NULL DEFAULT '{}';

CREATE UNIQUE INDEX IF NOT EXISTS uq_awm_memories_project_dedupe_active
	ON awm_memories (project_id, dedupe_key)
	WHERE active = TRUE AND dedupe_key IS NOT NULL;

CREATE TABLE IF NOT EXISTS awm_memory_candidates (
	candidate_id BIGSERIAL PRIMARY KEY,
	project_id TEXT NOT NULL,
	receipt_id TEXT NOT NULL,
	category TEXT NOT NULL CHECK (category IN ('decision', 'gotcha', 'pattern', 'preference')),
	subject TEXT NOT NULL,
	content TEXT NOT NULL,
	confidence SMALLINT NOT NULL CHECK (confidence BETWEEN 1 AND 5),
	tags TEXT[] NOT NULL DEFAULT '{}',
	related_pointer_keys TEXT[] NOT NULL DEFAULT '{}',
	evidence_pointer_keys TEXT[] NOT NULL DEFAULT '{}'
		CHECK (coalesce(array_length(evidence_pointer_keys, 1), 0) >= 1),
	dedupe_key TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'promoted', 'rejected')),
	promoted_memory_id BIGINT NULL REFERENCES awm_memories (memory_id) ON DELETE SET NULL,
	hard_passed BOOLEAN NOT NULL,
	soft_passed BOOLEAN NOT NULL,
	validation_errors TEXT[] NOT NULL DEFAULT '{}',
	validation_warnings TEXT[] NOT NULL DEFAULT '{}',
	auto_promote BOOLEAN NOT NULL DEFAULT TRUE,
	promotable BOOLEAN NOT NULL DEFAULT FALSE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_awm_memory_candidates_project_created
	ON awm_memory_candidates (project_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_awm_memory_candidates_project_status_created
	ON awm_memory_candidates (project_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_awm_memory_candidates_receipt_created
	ON awm_memory_candidates (receipt_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_awm_memory_candidates_project_dedupe
	ON awm_memory_candidates (project_id, dedupe_key);
