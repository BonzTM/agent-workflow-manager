ALTER TABLE awm_work_plans
	ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS parent_plan_key TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS external_refs TEXT[] NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_awm_work_plans_project_parent_updated
	ON awm_work_plans (project_id, parent_plan_key, updated_at DESC);

ALTER TABLE awm_work_plan_tasks
	ADD COLUMN IF NOT EXISTS parent_task_key TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS external_refs TEXT[] NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_awm_work_plan_tasks_project_plan_parent
	ON awm_work_plan_tasks (project_id, plan_key, parent_task_key, task_key);
