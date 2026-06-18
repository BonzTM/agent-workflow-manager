CREATE TABLE IF NOT EXISTS awm_work_plans (
	plan_id BIGSERIAL PRIMARY KEY,
	project_id TEXT NOT NULL,
	plan_key TEXT NOT NULL,
	receipt_id TEXT NULL,
	title TEXT NOT NULL DEFAULT '',
	objective TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL CHECK (status IN ('pending', 'in_progress', 'blocked', 'complete')),
	stage_spec_outline TEXT NOT NULL DEFAULT 'pending' CHECK (stage_spec_outline IN ('pending', 'in_progress', 'blocked', 'complete')),
	stage_refined_spec TEXT NOT NULL DEFAULT 'pending' CHECK (stage_refined_spec IN ('pending', 'in_progress', 'blocked', 'complete')),
	stage_implementation_plan TEXT NOT NULL DEFAULT 'pending' CHECK (stage_implementation_plan IN ('pending', 'in_progress', 'blocked', 'complete')),
	in_scope TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	out_of_scope TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	constraints_list TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	references_list TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE (project_id, plan_key)
);

CREATE INDEX IF NOT EXISTS idx_awm_work_plans_project_status_updated
	ON awm_work_plans (project_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_awm_work_plans_project_receipt_updated
	ON awm_work_plans (project_id, receipt_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS awm_work_plan_tasks (
	task_id BIGSERIAL PRIMARY KEY,
	project_id TEXT NOT NULL,
	plan_key TEXT NOT NULL,
	task_key TEXT NOT NULL,
	summary TEXT NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('pending', 'in_progress', 'blocked', 'complete')),
	depends_on TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	acceptance_criteria TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	references_list TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	blocked_reason TEXT NOT NULL DEFAULT '',
	outcome TEXT NOT NULL DEFAULT '',
	evidence TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE (project_id, plan_key, task_key),
	FOREIGN KEY (project_id, plan_key)
		REFERENCES awm_work_plans (project_id, plan_key)
		ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_awm_work_plan_tasks_project_plan_status
	ON awm_work_plan_tasks (project_id, plan_key, status, task_key);
CREATE INDEX IF NOT EXISTS idx_awm_work_plan_tasks_project_plan_updated
	ON awm_work_plan_tasks (project_id, plan_key, updated_at DESC);
