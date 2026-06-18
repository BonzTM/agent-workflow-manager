ALTER TABLE awm_work_items
	DROP CONSTRAINT IF EXISTS awm_work_items_status_check;

ALTER TABLE awm_work_plans
	DROP CONSTRAINT IF EXISTS awm_work_plans_status_check,
	DROP CONSTRAINT IF EXISTS awm_work_plans_stage_spec_outline_check,
	DROP CONSTRAINT IF EXISTS awm_work_plans_stage_refined_spec_check,
	DROP CONSTRAINT IF EXISTS awm_work_plans_stage_implementation_plan_check;

ALTER TABLE awm_work_plan_tasks
	DROP CONSTRAINT IF EXISTS awm_work_plan_tasks_status_check;

ALTER TABLE awm_work_items
	ADD CONSTRAINT awm_work_items_status_check
	CHECK (status IN ('pending', 'in_progress', 'blocked', 'complete', 'superseded'));

ALTER TABLE awm_work_plans
	ADD CONSTRAINT awm_work_plans_status_check
	CHECK (status IN ('pending', 'in_progress', 'blocked', 'complete', 'superseded')),
	ADD CONSTRAINT awm_work_plans_stage_spec_outline_check
	CHECK (stage_spec_outline IN ('pending', 'in_progress', 'blocked', 'complete', 'superseded')),
	ADD CONSTRAINT awm_work_plans_stage_refined_spec_check
	CHECK (stage_refined_spec IN ('pending', 'in_progress', 'blocked', 'complete', 'superseded')),
	ADD CONSTRAINT awm_work_plans_stage_implementation_plan_check
	CHECK (stage_implementation_plan IN ('pending', 'in_progress', 'blocked', 'complete', 'superseded'));

ALTER TABLE awm_work_plan_tasks
	ADD CONSTRAINT awm_work_plan_tasks_status_check
	CHECK (status IN ('pending', 'in_progress', 'blocked', 'complete', 'superseded'));
