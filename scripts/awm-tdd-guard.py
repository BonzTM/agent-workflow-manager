#!/usr/bin/env python3

import argparse
import json
import os
import subprocess
import sys


BEHAVIOR_PREFIXES = ("cmd/", "internal/")
GATE_TASK_KEYS = {"verify:tests", "review:cross-llm"}
COMPLETE_STATUSES = {"complete", "completed"}


def trimmed(value):
    return value.strip() if isinstance(value, str) else ""


def parse_json_list(raw, field_name):
    if not trimmed(raw):
        return []
    try:
        decoded = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise ValueError(f"{field_name} must be valid JSON: {exc}") from exc
    if not isinstance(decoded, list):
        raise ValueError(f"{field_name} must decode to a JSON array")

    values = []
    seen = set()
    for item in decoded:
        value = trimmed(item)
        if not value:
            continue
        normalized = value.replace("\\", "/")
        if normalized in seen:
            continue
        seen.add(normalized)
        values.append(normalized)
    return values


def is_behavior_go_path(path):
    return path.startswith(BEHAVIOR_PREFIXES) and path.endswith(".go") and not path.endswith("_test.go")


def is_go_test_path(path):
    return path.startswith(BEHAVIOR_PREFIXES) and path.endswith("_test.go")


def task_matches_prefix(task_key, prefix):
    return task_key == prefix or task_key.startswith(prefix + ":")


def task_is_complete(task):
    return trimmed(task.get("status")) in COMPLETE_STATUSES


def first_completed_task(tasks, prefix):
    for task in tasks:
        task_key = trimmed(task.get("key"))
        if task_matches_prefix(task_key, prefix) and task_is_complete(task):
            return task
    return None


def requires_tdd_red(tasks):
    for task in tasks:
        task_key = trimmed(task.get("key"))
        if not task_key:
            continue
        if task_key in GATE_TASK_KEYS:
            continue
        if task_matches_prefix(task_key, "tdd:red"):
            continue
        if task_matches_prefix(task_key, "tdd:exemption"):
            continue
        return True
    return False


def derive_plan_key(plan_key, receipt_id):
    if trimmed(plan_key):
        return trimmed(plan_key)
    if trimmed(receipt_id):
        return f"plan:{trimmed(receipt_id)}"
    return ""


def fetch_plan(project, plan_key):
    env = os.environ.copy()
    env["AWM_LOG_SINK"] = "discard"
    result = subprocess.run(
        ["awm", "fetch", "--project", project, "--key", plan_key],
        capture_output=True,
        text=True,
        env=env,
    )
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
        return None
    if len(items) != 1:
        raise RuntimeError(f"expected one fetched item for {plan_key}, got {len(items)}")

    content = trimmed(items[0].get("content"))
    if not content:
        return None

    try:
        decoded = json.loads(content)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"plan content for {plan_key} was not valid JSON: {exc}") from exc
    if not isinstance(decoded, dict):
        raise RuntimeError(f"plan content for {plan_key} was not an object")
    return decoded


def parse_args():
    parser = argparse.ArgumentParser(
        description="Enforce the agent-workflow-manager repo's TDD workflow for Go behavior changes."
    )
    parser.add_argument(
        "--project",
        default=os.environ.get("AWM_PROJECT_ID", "agent-workflow-manager"),
        help="AWM project id (defaults to AWM_PROJECT_ID or agent-workflow-manager).",
    )
    parser.add_argument(
        "--plan-key",
        default=os.environ.get("AWM_PLAN_KEY", ""),
        help="Plan key to inspect for repo-local TDD tasks (defaults to AWM_PLAN_KEY).",
    )
    parser.add_argument(
        "--receipt-id",
        default=os.environ.get("AWM_RECEIPT_ID", ""),
        help="Receipt id used to derive plan:<receipt_id> when --plan-key is omitted.",
    )
    parser.add_argument(
        "--files-changed-json",
        default=os.environ.get("AWM_VERIFY_FILES_CHANGED_JSON", "[]"),
        help="JSON array of repo-relative changed paths (defaults to AWM_VERIFY_FILES_CHANGED_JSON).",
    )
    return parser.parse_args()


def fail(message):
    print(f"awm-tdd-guard: {message}", file=sys.stderr)
    return 1


def main():
    args = parse_args()
    try:
        changed_paths = parse_json_list(args.files_changed_json, "files_changed")
    except ValueError as exc:
        return fail(str(exc))

    behavior_paths = [path for path in changed_paths if is_behavior_go_path(path)]
    if not behavior_paths:
        print("awm-tdd-guard: skip - no behavior-changing Go files matched")
        return 0

    plan = None
    plan_key = derive_plan_key(args.plan_key, args.receipt_id)
    if plan_key:
        try:
            plan = fetch_plan(args.project, plan_key)
        except RuntimeError as exc:
            return fail(str(exc))

    tasks = plan.get("tasks", []) if isinstance(plan, dict) else []
    if not isinstance(tasks, list):
        tasks = []

    if first_completed_task(tasks, "tdd:exemption") is not None:
        print("awm-tdd-guard: pass - completed tdd:exemption task found")
        return 0

    test_paths = [path for path in changed_paths if is_go_test_path(path)]
    if not test_paths:
        return fail(
            "behavior-changing Go files changed without a Go test-file delta or completed tdd:exemption task"
        )

    if requires_tdd_red(tasks) and first_completed_task(tasks, "tdd:red") is None:
        return fail(
            "planned behavior-changing Go work must complete a tdd:red task before implementation"
        )

    print("awm-tdd-guard: pass - Go test delta present and TDD metadata satisfied")
    return 0


if __name__ == "__main__":
    sys.exit(main())
