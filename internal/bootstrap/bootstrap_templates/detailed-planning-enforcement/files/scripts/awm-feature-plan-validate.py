#!/usr/bin/env python3

import argparse
import json
import os
import subprocess
import sys
from collections import defaultdict


ROOT_KINDS = {"feature", "maintenance", "governance"}
STREAM_KIND = "feature_stream"
STAGE_TASKS = {
    "stage:spec-outline": "spec_outline",
    "stage:refined-spec": "refined_spec",
    "stage:implementation-plan": "implementation_plan",
}
STAGE_CHILD_PREFIXES = {
    "stage:spec-outline": ("spec:",),
    "stage:refined-spec": ("refine:",),
    "stage:implementation-plan": ("impl:", "tdd:"),
}
SUPERSEDED_STATUSES = {"superseded"}
LEAF_TASK_EXEMPT_KEYS = {"verify:tests"}
CATCH_ALL_LEAF_SUMMARIES = {
    "cleanup",
    "misc",
    "misc cleanup",
    "polish",
    "remaining",
    "remaining work",
    "wire the rest",
}


def trimmed(value):
    return value.strip() if isinstance(value, str) else ""


def normalized_list(value):
    if not isinstance(value, list):
        return []
    output = []
    for item in value:
        item_value = trimmed(item)
        if item_value:
            output.append(item_value)
    return output


def normalized_summary(value):
    return " ".join(trimmed(value).lower().split())


def is_gate_task(task_key):
    value = trimmed(task_key)
    return value in LEAF_TASK_EXEMPT_KEYS or value.startswith("review:")


def default_project_id():
    env_project = trimmed(os.environ.get("AWM_PROJECT_ID"))
    if env_project:
        return env_project
    return os.path.basename(os.getcwd()) or "repo"


def parse_args():
    parser = argparse.ArgumentParser(
        description="Validate this repo's AWM staged plan convention."
    )
    parser.add_argument(
        "--project",
        default=default_project_id(),
        help="AWM project id (defaults to AWM_PROJECT_ID or the current directory name).",
    )
    parser.add_argument(
        "--plan-key",
        default=os.environ.get("AWM_PLAN_KEY", ""),
        help="Plan key to validate (defaults to AWM_PLAN_KEY).",
    )
    parser.add_argument(
        "--receipt-id",
        default=os.environ.get("AWM_RECEIPT_ID", ""),
        help="Receipt id to derive plan:<receipt_id> when --plan-key is omitted.",
    )
    return parser.parse_args()


def fail(message):
    print(f"awm-feature-plan-validate: {message}", file=sys.stderr)
    return 1


def fetch_plan(project, plan_key, allow_unmaterialized=False):
    env = os.environ.copy()
    env["AWM_LOG_SINK"] = "discard"
    command = ["awm", "fetch", "--project", project, "--key", plan_key]
    result = subprocess.run(command, capture_output=True, text=True, env=env)
    if result.returncode != 0:
        raise RuntimeError(
            f"awm fetch failed for {plan_key}: {trimmed(result.stderr) or trimmed(result.stdout) or 'unknown error'}"
        )
    try:
        envelope = json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"awm fetch returned invalid JSON for {plan_key}: {exc}") from exc
    items = envelope.get("result", {}).get("items", [])
    if not items:
        if allow_unmaterialized:
            return None
        raise RuntimeError(f"expected one fetched item for {plan_key}, got 0")
    if len(items) != 1:
        raise RuntimeError(f"expected one fetched item for {plan_key}, got {len(items)}")
    content = trimmed(items[0].get("content"))
    if not content:
        if allow_unmaterialized:
            return None
        raise RuntimeError(f"fetched plan {plan_key} had empty content")
    try:
        plan = json.loads(content)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"plan content for {plan_key} was not valid JSON: {exc}") from exc
    if not isinstance(plan, dict):
        raise RuntimeError(f"plan content for {plan_key} was not an object")
    plan["__plan_key"] = plan_key
    return plan


def build_chain(project, starting_plan):
    chain = [starting_plan]
    seen = {starting_plan["__plan_key"]}
    current = starting_plan
    while trimmed(current.get("parent_plan_key")):
        parent_key = trimmed(current.get("parent_plan_key"))
        if parent_key in seen:
            raise RuntimeError(f"cycle detected in plan hierarchy at {parent_key}")
        parent = fetch_plan(project, parent_key)
        chain.append(parent)
        seen.add(parent_key)
        current = parent
    chain.reverse()
    return chain


def require_non_empty_string(plan, field_name, errors):
    value = trimmed(plan.get(field_name))
    if not value:
        errors.append(f"{plan['__plan_key']}: missing non-empty {field_name}")


def require_non_empty_list(plan, field_name, errors):
    if not normalized_list(plan.get(field_name)):
        errors.append(f"{plan['__plan_key']}: {field_name} must contain at least one item")


def index_tasks(plan, errors):
    raw_tasks = plan.get("tasks")
    if not isinstance(raw_tasks, list) or not raw_tasks:
        errors.append(f"{plan['__plan_key']}: tasks must contain at least one task")
        return {}, defaultdict(list)

    tasks_by_key = {}
    children_by_parent = defaultdict(list)

    for index, task in enumerate(raw_tasks):
        if not isinstance(task, dict):
            errors.append(f"{plan['__plan_key']}: task[{index}] must be an object")
            continue
        task_key = trimmed(task.get("key"))
        if not task_key:
            errors.append(f"{plan['__plan_key']}: task[{index}] is missing key")
            continue
        if task_key in tasks_by_key:
            errors.append(f"{plan['__plan_key']}: duplicate task key {task_key}")
            continue
        if trimmed(task.get("status")) in SUPERSEDED_STATUSES:
            continue
        tasks_by_key[task_key] = task

    for task_key, task in tasks_by_key.items():
        if not trimmed(task.get("summary")):
            errors.append(f"{plan['__plan_key']}: task {task_key} is missing summary")
        parent_task_key = trimmed(task.get("parent_task_key"))
        if parent_task_key:
            if parent_task_key not in tasks_by_key:
                errors.append(
                    f"{plan['__plan_key']}: task {task_key} references unknown parent_task_key {parent_task_key}"
                )
            else:
                children_by_parent[parent_task_key].append(task_key)
        for dependency in normalized_list(task.get("depends_on")):
            if dependency not in tasks_by_key:
                errors.append(
                    f"{plan['__plan_key']}: task {task_key} depends on unknown task {dependency}"
                )

    return tasks_by_key, children_by_parent


def validate_leaf_acceptance(plan, tasks_by_key, children_by_parent, errors):
    found_leaf = False
    for task_key, task in tasks_by_key.items():
        if task_key in STAGE_TASKS:
            continue
        if task_key in children_by_parent:
            continue
        if is_gate_task(task_key):
            continue
        found_leaf = True
        acceptance_criteria = normalized_list(task.get("acceptance_criteria"))
        if len(acceptance_criteria) < 2:
            errors.append(
                f"{plan['__plan_key']}: leaf task {task_key} must include at least 2 acceptance_criteria"
            )
        references = normalized_list(task.get("references"))
        if not references:
            errors.append(f"{plan['__plan_key']}: leaf task {task_key} must include references")
        if len(references) > 3:
            errors.append(
                f"{plan['__plan_key']}: leaf task {task_key} must reference at most 3 repo paths"
            )
        if normalized_summary(task.get("summary")) in CATCH_ALL_LEAF_SUMMARIES:
            errors.append(
                f"{plan['__plan_key']}: leaf task {task_key} summary is too vague for atomic execution"
            )
    if not found_leaf:
        errors.append(f"{plan['__plan_key']}: expected at least one non-gate leaf task")


def validate_no_non_stage_parents(plan, children_by_parent, errors):
    for parent_task_key in children_by_parent:
        if parent_task_key in STAGE_TASKS:
            continue
        if is_gate_task(parent_task_key):
            errors.append(
                f"{plan['__plan_key']}: gate task {parent_task_key} must not own child tasks"
            )
            continue
        errors.append(
            f"{plan['__plan_key']}: task {parent_task_key} must be a direct leaf under one stage task, not a parent task"
        )


def validate_root_stage_hierarchy(plan, tasks_by_key, children_by_parent, errors):
    for task_key, stage_field in STAGE_TASKS.items():
        stage_task = tasks_by_key.get(task_key)
        if not stage_task:
            errors.append(f"{plan['__plan_key']}: missing top-level stage task {task_key}")
            continue
        if trimmed(stage_task.get("parent_task_key")):
            errors.append(
                f"{plan['__plan_key']}: stage task {task_key} must not set parent_task_key"
            )
        stage_children = children_by_parent.get(task_key, [])
        if not stage_children:
            errors.append(
                f"{plan['__plan_key']}: stage task {task_key} must own at least one child task"
            )
            continue
        allowed_prefixes = STAGE_CHILD_PREFIXES[task_key]
        for child_task_key in stage_children:
            if is_gate_task(child_task_key):
                # Gate tasks should ideally be top-level, but AWM does not
                # support clearing parent_task_key once set, so tolerate
                # stale parent relationships on gate tasks.
                continue
            child_task = tasks_by_key[child_task_key]
            if trimmed(child_task.get("parent_task_key")) != task_key:
                errors.append(
                    f"{plan['__plan_key']}: task {child_task_key} must be a direct child of {task_key}"
                )
            if not any(child_task_key.startswith(prefix) for prefix in allowed_prefixes):
                errors.append(
                    f"{plan['__plan_key']}: task {child_task_key} is under {task_key} but must use one of the prefixes {', '.join(allowed_prefixes)}"
                )

    for task_key, task in tasks_by_key.items():
        if task_key in STAGE_TASKS:
            continue
        parent_task_key = trimmed(task.get("parent_task_key"))
        if is_gate_task(task_key):
            # Gate tasks should ideally be top-level, but AWM does not support
            # clearing parent_task_key once set, so tolerate stale parents.
            continue
        if parent_task_key not in STAGE_TASKS:
            errors.append(
                f"{plan['__plan_key']}: task {task_key} must be a direct child of one stage task"
            )


def is_thin_plan_exempt(plan):
    if trimmed(plan.get("parent_plan_key")):
        return False
    stages = plan.get("stages")
    if isinstance(stages, dict) and stages:
        return False

    tasks_by_key, children_by_parent = index_tasks(plan, [])
    if not tasks_by_key:
        return False
    if any(task_key in STAGE_TASKS for task_key in tasks_by_key):
        return False
    if children_by_parent:
        return False

    non_gate_task_keys = [
        task_key for task_key in tasks_by_key if not is_gate_task(task_key)
    ]
    if len(non_gate_task_keys) != 1:
        return False

    task = tasks_by_key[non_gate_task_keys[0]]
    if trimmed(task.get("parent_task_key")):
        return False
    if normalized_list(task.get("depends_on")):
        return False
    return True


def validate_root_staged_plan(plan, errors):
    if trimmed(plan.get("parent_plan_key")):
        errors.append(f"{plan['__plan_key']}: root staged plans must not set parent_plan_key")

    require_non_empty_string(plan, "title", errors)
    require_non_empty_string(plan, "objective", errors)
    require_non_empty_list(plan, "in_scope", errors)
    require_non_empty_list(plan, "out_of_scope", errors)
    require_non_empty_list(plan, "constraints", errors)
    require_non_empty_list(plan, "references", errors)

    stages = plan.get("stages")
    if not isinstance(stages, dict):
        stages = {}
        errors.append(f"{plan['__plan_key']}: stages must be present for staged root plans")
    for stage_field in STAGE_TASKS.values():
        if not trimmed(stages.get(stage_field)):
            errors.append(f"{plan['__plan_key']}: stages.{stage_field} must be set")

    tasks_by_key, children_by_parent = index_tasks(plan, errors)
    if not tasks_by_key:
        return

    if "verify:tests" not in tasks_by_key:
        errors.append(f"{plan['__plan_key']}: staged root plans must include a verify:tests task")

    for task_key, stage_field in STAGE_TASKS.items():
        stage_task = tasks_by_key.get(task_key)
        if not stage_task:
            continue
        plan_stage_status = trimmed(stages.get(stage_field))
        task_status = trimmed(stage_task.get("status"))
        if plan_stage_status and task_status and plan_stage_status != task_status:
            errors.append(
                f"{plan['__plan_key']}: stage task {task_key} status {task_status} must match stages.{stage_field}={plan_stage_status}"
            )

    validate_no_non_stage_parents(plan, children_by_parent, errors)
    validate_root_stage_hierarchy(plan, tasks_by_key, children_by_parent, errors)
    validate_leaf_acceptance(plan, tasks_by_key, children_by_parent, errors)


def validate_feature_stream_plan(plan, errors):
    if not trimmed(plan.get("parent_plan_key")):
        errors.append(f"{plan['__plan_key']}: feature_stream plans must set parent_plan_key")
    require_non_empty_string(plan, "title", errors)
    require_non_empty_string(plan, "objective", errors)
    require_non_empty_list(plan, "in_scope", errors)
    require_non_empty_list(plan, "out_of_scope", errors)
    require_non_empty_list(plan, "references", errors)

    tasks_by_key, children_by_parent = index_tasks(plan, errors)
    if not tasks_by_key:
        return

    if "verify:tests" not in tasks_by_key:
        errors.append(
            f"{plan['__plan_key']}: feature_stream plans must include a verify:tests task"
        )

    validate_no_non_stage_parents(plan, children_by_parent, errors)
    validate_leaf_acceptance(plan, tasks_by_key, children_by_parent, errors)


def validate_staged_plan_hierarchy(chain):
    errors = []
    root_plan = chain[0]
    root_kind = trimmed(root_plan.get("kind"))
    if root_kind not in ROOT_KINDS:
        errors.append(
            f"{root_plan['__plan_key']}: root plan must use kind=feature|maintenance|governance"
        )
        return errors

    validate_root_staged_plan(root_plan, errors)

    if root_kind != "feature" and len(chain) > 1:
        errors.append(
            f"{root_plan['__plan_key']}: maintenance/governance staged plans must stay in one root plan with atomic leaf tasks"
        )
        return errors

    for descendant in chain[1:]:
        descendant_kind = trimmed(descendant.get("kind"))
        if descendant_kind != STREAM_KIND:
            errors.append(
                f"{descendant['__plan_key']}: child plans under a staged feature root must use kind={STREAM_KIND}"
            )
            continue
        validate_feature_stream_plan(descendant, errors)

    return errors


def main():
    args = parse_args()
    plan_key = trimmed(args.plan_key)
    receipt_id = trimmed(args.receipt_id)

    if not plan_key and receipt_id:
        plan_key = f"plan:{receipt_id}"

    if not plan_key:
        print(
            "awm-feature-plan-validate: skip - no AWM_PLAN_KEY or AWM_RECEIPT_ID was provided",
            file=sys.stderr,
        )
        return 0

    try:
        current_plan = fetch_plan(args.project, plan_key, allow_unmaterialized=True)
        if current_plan is None:
            print(
                f"awm-feature-plan-validate: skip - active plan {plan_key} has no materialized content in this receipt context"
            )
            return 0
        chain = build_chain(args.project, current_plan)
    except RuntimeError as exc:
        return fail(str(exc))

    current_kind = trimmed(current_plan.get("kind"))
    root_kind = trimmed(chain[0].get("kind"))
    if is_thin_plan_exempt(current_plan):
        print(
            f"awm-feature-plan-validate: skip - active plan {plan_key} matches the thin-plan exemption"
        )
        return 0

    managed_hierarchy = root_kind in ROOT_KINDS or current_kind == STREAM_KIND
    if not managed_hierarchy:
        return fail(
            f'active plan {plan_key} uses kind "{current_kind or "unspecified"}" but does not match the thin-plan exemption; governed multi-step work must use kind=feature|maintenance|governance'
        )

    errors = validate_staged_plan_hierarchy(chain)
    if errors:
        print("awm-feature-plan-validate: staged plan contract failed:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1

    chain_text = " -> ".join(plan["__plan_key"] for plan in chain)
    print(f"awm-feature-plan-validate: ok - validated staged plan hierarchy {chain_text}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
