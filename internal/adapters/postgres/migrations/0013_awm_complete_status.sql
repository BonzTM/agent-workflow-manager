ALTER TABLE awm_work_items
	DROP CONSTRAINT IF EXISTS awm_work_items_status_check;

ALTER TABLE awm_work_plans
	DROP CONSTRAINT IF EXISTS awm_work_plans_status_check,
	DROP CONSTRAINT IF EXISTS awm_work_plans_stage_spec_outline_check,
	DROP CONSTRAINT IF EXISTS awm_work_plans_stage_refined_spec_check,
	DROP CONSTRAINT IF EXISTS awm_work_plans_stage_implementation_plan_check;

ALTER TABLE awm_work_plan_tasks
	DROP CONSTRAINT IF EXISTS awm_work_plan_tasks_status_check;

UPDATE awm_work_items
SET status = 'complete'
WHERE status = 'completed';

UPDATE awm_work_plans
SET status = 'complete'
WHERE status = 'completed';

UPDATE awm_work_plans
SET stage_spec_outline = 'complete'
WHERE stage_spec_outline = 'completed';

UPDATE awm_work_plans
SET stage_refined_spec = 'complete'
WHERE stage_refined_spec = 'completed';

UPDATE awm_work_plans
SET stage_implementation_plan = 'complete'
WHERE stage_implementation_plan = 'completed';

UPDATE awm_work_plan_tasks
SET status = 'complete'
WHERE status = 'completed';

ALTER TABLE awm_work_items
	ADD CONSTRAINT awm_work_items_status_check
	CHECK (status IN ('pending', 'in_progress', 'blocked', 'complete'));

ALTER TABLE awm_work_plans
	ADD CONSTRAINT awm_work_plans_status_check
	CHECK (status IN ('pending', 'in_progress', 'blocked', 'complete')),
	ADD CONSTRAINT awm_work_plans_stage_spec_outline_check
	CHECK (stage_spec_outline IN ('pending', 'in_progress', 'blocked', 'complete')),
	ADD CONSTRAINT awm_work_plans_stage_refined_spec_check
	CHECK (stage_refined_spec IN ('pending', 'in_progress', 'blocked', 'complete')),
	ADD CONSTRAINT awm_work_plans_stage_implementation_plan_check
	CHECK (stage_implementation_plan IN ('pending', 'in_progress', 'blocked', 'complete'));

ALTER TABLE awm_work_plan_tasks
	ADD CONSTRAINT awm_work_plan_tasks_status_check
	CHECK (status IN ('pending', 'in_progress', 'blocked', 'complete'));
