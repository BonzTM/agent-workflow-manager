# AGENTS.md

Starter operating contract for a repo that uses `awm`.

## Source Of Truth

- Follow this file first.
- Keep canonical rules in `.awm/awm-rules.yaml` (preferred) or `awm-rules.yaml` at the repo root.
- Keep canonical tags in `.awm/awm-tags.yaml` and executable checks in `.awm/awm-tests.yaml`.
- Keep canonical completion workflow gates in `.awm/awm-workflows.yaml` (preferred) or `awm-workflows.yaml`.
- If tool-specific instructions conflict with this file, this file wins unless a human explicitly says otherwise.

## Task Loop

See [.awm/awm-work-loop.md](.awm/awm-work-loop.md) for the full AWM command reference (CLI and MCP).

The short version: `context` → `work` → `verify` → `done`. Trivial single-file fixes can skip the ceremony.

## Working Rules

- Do not silently expand governed file scope. Use `work.plan.discovered_paths` when later-discovered files must be declared.
- Prefer small, reviewable changes over broad cleanup.
- Do not invent product requirements or compatibility guarantees the repo does not define.
- If verification fails, fix it or report it clearly. Do not claim success.
- Keep work state current when you pause, hand off, or hit a blocker.
- Mark obsolete tasks `superseded` instead of leaving them open or `blocked`.

## Ruleset Maintenance

1. Edit the canonical rules, tags, tests, or workflow files.
2. Run `awm sync --mode working_tree --insert-new-candidates` or `awm health --apply`.
3. Run `awm health --include-details` and resolve blocking findings.