# CLAUDE.md

Claude companion for a repo whose primary contract is `AGENTS.md`.

## Source Of Truth

- Follow `AGENTS.md` first.
- Use this file only to map Claude's workflow to the repo contract.
- If this file conflicts with `AGENTS.md`, `AGENTS.md` wins.

## Claude Workflow

Follow the task loop in `AGENTS.md`. For Claude, the AWM commands map to slash commands:

| AWM step | Claude slash command |
|---|---|
| `awm context` | `/awm-context [phase] <task>` |
| `awm work` | `/awm-work` |
| `awm verify` | `/awm-verify` |
| `awm review --run` | `/awm-review <id> {"run":true}` |
| `awm done` | `/awm-done` |

Direct CLI (`awm sync`, `awm health`, `awm history`, `awm status`) has no slash-command aliases — call those directly when needed.

## Claude-Specific Notes

- Keep prompts specific enough that `context` can load the right rules, plans, and any explicit initial scope.
- If the receipt looks stale or too narrow, re-run `/awm-context` with a better task description instead of guessing.
- If governed file work expands beyond the initial receipt scope, record the new files through `/awm-work` before expecting `/awm-review` or `/awm-done` to pass.
- Do not claim success when `/awm-verify` failed or was skipped for code changes.
- `/awm-review` stays thin. Use `{"run":true}` for runnable workflow gates because manual complete notes do not satisfy runnable gates, and reserve manual `status`, `outcome`, `blocked_reason`, and `evidence` fields for non-run mode.
- If `/awm-review {"run":true}` reports repo changes but zero scoped review files, the receipt or declared discovered scope is too narrow. Re-run `/awm-context` or update `/awm-work` before retrying review.
- If the repo defines a richer feature-plan contract, populate the required `plan.stages`, `stage:*` tasks, `parent_task_key`, and leaf `acceptance_criteria` before implementation, then let `verify` enforce it.
- When blocked on a missing product or architectural decision, surface the decision instead of improvising it.

## Web Dashboard

If `awm-web` is installed, humans can view agent work at `http://localhost:8080/` — a read-only kanban board and status page. No agent interaction needed.

## Ruleset Maintenance

When `.awm/awm-rules.yaml`, `.awm/awm-tags.yaml`, `.awm/awm-tests.yaml`, or `.awm/awm-workflows.yaml` changes, refresh broker state with `awm sync` or `awm health --apply`, then run `awm health`.
