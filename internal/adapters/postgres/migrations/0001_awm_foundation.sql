CREATE OR REPLACE FUNCTION awm_pointer_search_vector(label TEXT, description TEXT, tags TEXT[])
RETURNS TSVECTOR
LANGUAGE SQL
IMMUTABLE
AS $$
	SELECT
		setweight(to_tsvector('simple', coalesce(label, '')), 'A') ||
		setweight(to_tsvector('simple', coalesce(description, '')), 'B') ||
		setweight(to_tsvector('simple', coalesce(array_to_string(tags, ' '), '')), 'C')
$$;

CREATE TABLE IF NOT EXISTS awm_pointers (
	pointer_id BIGSERIAL PRIMARY KEY,
	project_id TEXT NOT NULL,
	pointer_key TEXT NOT NULL,
	path TEXT NOT NULL,
	anchor TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL,
	label TEXT NOT NULL,
	description TEXT NOT NULL,
	tags TEXT[] NOT NULL DEFAULT '{}',
	is_rule BOOLEAN NOT NULL DEFAULT FALSE,
	is_stale BOOLEAN NOT NULL DEFAULT FALSE,
	stale_at TIMESTAMPTZ NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	search_vector TSVECTOR GENERATED ALWAYS AS (awm_pointer_search_vector(label, description, tags)) STORED,
	UNIQUE (project_id, pointer_key)
);

CREATE INDEX IF NOT EXISTS idx_awm_pointers_project_updated
	ON awm_pointers (project_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_pointers_tags_gin
	ON awm_pointers USING GIN (tags);
CREATE INDEX IF NOT EXISTS idx_awm_pointers_search_vector_gin
	ON awm_pointers USING GIN (search_vector);

CREATE TABLE IF NOT EXISTS awm_pointer_links (
	project_id TEXT NOT NULL,
	from_key TEXT NOT NULL,
	to_key TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (project_id, from_key, to_key),
	FOREIGN KEY (project_id, from_key)
		REFERENCES awm_pointers (project_id, pointer_key)
		ON DELETE CASCADE,
	FOREIGN KEY (project_id, to_key)
		REFERENCES awm_pointers (project_id, pointer_key)
		ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_awm_pointer_links_project_to_key
	ON awm_pointer_links (project_id, to_key);

CREATE TABLE IF NOT EXISTS awm_memories (
	memory_id BIGSERIAL PRIMARY KEY,
	project_id TEXT NOT NULL,
	category TEXT NOT NULL,
	subject TEXT NOT NULL,
	content TEXT NOT NULL,
	confidence SMALLINT NOT NULL CHECK (confidence BETWEEN 1 AND 5),
	tags TEXT[] NOT NULL DEFAULT '{}',
	related_pointer_keys TEXT[] NOT NULL DEFAULT '{}',
	active BOOLEAN NOT NULL DEFAULT TRUE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_awm_memories_project_active
	ON awm_memories (project_id, active, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_memories_tags_gin
	ON awm_memories USING GIN (tags);
CREATE INDEX IF NOT EXISTS idx_awm_memories_related_pointer_keys_gin
	ON awm_memories USING GIN (related_pointer_keys);

CREATE TABLE IF NOT EXISTS awm_receipts (
	receipt_id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	task_text TEXT NOT NULL DEFAULT '',
	phase TEXT NOT NULL DEFAULT 'execute',
	resolved_tags TEXT[] NOT NULL DEFAULT '{}',
	pointer_keys TEXT[] NOT NULL DEFAULT '{}',
	memory_ids BIGINT[] NOT NULL DEFAULT '{}',
	summary_json JSONB NOT NULL DEFAULT '{}'::JSONB,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_awm_receipts_project_created
	ON awm_receipts (project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS awm_runs (
	run_id BIGSERIAL PRIMARY KEY,
	project_id TEXT NOT NULL,
	request_id TEXT NOT NULL DEFAULT '',
	receipt_id TEXT NOT NULL REFERENCES awm_receipts (receipt_id) ON DELETE CASCADE,
	status TEXT NOT NULL,
	files_changed TEXT[] NOT NULL DEFAULT '{}',
	outcome TEXT NOT NULL DEFAULT '',
	summary_json JSONB NOT NULL DEFAULT '{}'::JSONB,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_awm_runs_project_created
	ON awm_runs (project_id, created_at DESC);
