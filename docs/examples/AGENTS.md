# AGENTS.md

Starter operating contract for a repo that uses `awm`.
Keep this file as the fast path, then move heavier architecture, checklist, or troubleshooting material into linked repo-local maintainer docs as the project grows.

## Source Of Truth

- Follow this file first.
- Keep canonical rules in `.awm/awm-rules.yaml` (preferred) or `awm-rules.yaml` at the repo root.
- Keep canonical tags in `.awm/awm-tags.yaml` and executable checks in `.awm/awm-tests.yaml`.
- Keep canonical completion workflow gates in `.awm/awm-workflows.yaml` (preferred) or `awm-workflows.yaml`.
- If tool-specific instructions conflict with this file, this file wins unless a human explicitly says otherwise.

## Task Loop

1. Read this file and the human task.
2. For non-trivial work (multi-step, multi-file, or governed), run `awm context` to get a scoped receipt. Trivial single-file fixes can skip this.
3. Follow all hard rules returned in the receipt.
4. Use `fetch` only for the pointers, plans, and task keys needed for the current step.
5. When a task spans multiple steps, multiple files, or a likely handoff, create or update `work`.
6. If code, config, schema, or other executable behavior changes, run `verify` before `done`.
7. If `.awm/awm-workflows.yaml` requires review task keys such as `review:cross-llm`, prefer `review --run` when the task defines a `run` block; otherwise use manual `review` fields or `work` before `done`.
8. End non-trivial tasks with `done`, including every changed file for file-backed work when you know them, or letting AWM derive the task delta from the receipt baseline. When that detected delta is empty, the closeout is effectively no-file.
   Built-in governance files such as repo-root `AGENTS.md`, `CLAUDE.md`, and canonical `.awm/**` contract files are already treated as managed completion scope.
When the task changes rules, tags, tests, workflows, onboarding, or tool-surface behavior, refresh broker state with `awm sync --mode working_tree --insert-new-candidates` and then run `awm health --include-details` before `done`.

If you need to resume after compaction or inspect archived work, use direct CLI `awm history` with `--entity work` for plan/task discovery or another entity for receipts and runs, then `awm fetch` the returned `fetch_keys`.
If you need to debug project setup, loaded AWM files, integrations, or what `context` would load for a task, use `awm status`.

## Working Rules

- Do not silently expand governed file scope. Refresh context first if the task spills into adjacent systems, and use `work.plan.discovered_paths` when later-discovered files must be declared for review/done.
- Prefer small, reviewable changes over broad cleanup.
- Do not invent product requirements, compatibility guarantees, or migration behavior when the repo does not define them.
- If verification fails, either fix the issue or report the failure clearly. Do not claim the task is complete as if checks passed.
- Keep work state current when you pause, hand off, or hit a blocker.
- If the repo also uses AMM, use AMM for durable memory.

## When To Use work

Use `work` when any of the following are true:

- the task will take more than one material step
- more than one file or subsystem is involved
- the task includes explicit planning, verification, or handoff
- you need durable task state that should survive compaction or session reset

For code changes, include a `verify:tests` task. Add other task keys when they help resumption, coordination, or are required by `.awm/awm-workflows.yaml`. For single review-gate updates, `review` is the thinner convenience wrapper around `work`; use `review --run` for runnable workflow gates, and keep manual `status` / `outcome` / `blocked_reason` / `evidence` fields for non-run mode. If a planned task or review gate becomes obsolete, mark it `superseded` instead of leaving it open or `blocked`.

## Optional Feature Plans

If the repo wants stricter planning for net-new feature work:

- require root plans with `kind=feature`, explicit scope metadata, and `plan.stages.spec_outline` / `refined_spec` / `implementation_plan`
- require top-level `stage:*` tasks with child tasks linked through `parent_task_key`
- treat leaf tasks as the atomic tasks and require `acceptance_criteria` on those leaves
- use `kind=feature_stream` plus `parent_plan_key` for parallel execution streams
- enforce the schema through a repo-local `verify` script selected from `.awm/awm-tests.yaml`

## Ruleset Maintenance

1. Edit the canonical rules, tags, tests, or workflow files.
2. Run `awm sync` or `awm health --apply`.
3. Run `awm health` and resolve blocking findings.

## Web Dashboard

If `awm-web` is installed, humans can view agent work at `http://localhost:8080/` without touching the CLI. Run `awm-web` or `awm-web serve --addr :9090` for a custom port. The dashboard is read-only and shares the same database as `awm`.

## Tool-Specific Companions

`CLAUDE.md`, slash commands, and Codex skills should stay thin and map their workflow back to this file.
If they disagree with this file, this file is authoritative.
