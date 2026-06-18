---
name: awm-broker
description: Use the agent-workflow-manager broker (CLI or MCP) to load repo-owned rules and durable state, post work/review updates, and close tasks with deterministic JSON contracts.
---

# awm-broker

Use this skill when a task needs repo-owned agent state, hard rule compliance, durable planning, or deterministic completion reporting through `agent-workflow-manager`.

If the repo also uses [Agent Memory Manager (AMM)](https://github.com/bonztm/agent-memory-manager), use AMM for durable memory.

## Required Flow

1. Call `context` first.
2. Read and follow the returned rules block (or rule pointers) as hard constraints.
3. Call `fetch` only for plan, task, or pointer content you actually need to hydrate (or use `receipt_id` shorthand without explicit keys).
4. Execute work; if the task evolves, call `work` with `receipt_id` (optionally without `plan_key`) to publish broader updates. Use `tasks` payloads and `verify:tests` as the built-in executable verification task key. `.awm/awm-workflows.yaml` may require additional task keys. If the repo defines a richer feature-plan contract, populate the required `plan.stages`, top-level `stage:*` tasks, `parent_task_key`, and leaf `acceptance_criteria` before implementation; `verify` may enforce that schema.
5. When governed work discovers file scope beyond the initial receipt, record it through `work.plan.discovered_paths` before relying on `review` to pass. `done` can also honor path-like `plan.in_scope` entries for plan-owned closure scope, but `discovered_paths` is still the preferred way to declare later-found concrete files.
6. Call `review` when you only need to record a single review-gate outcome. It lowers to one `work` task update; use `run=true` when the repo workflow defines a runnable review gate, because manual complete notes do not satisfy runnable gates. Runnable review gates only skip reruns after a passing attempt already assessed the current fingerprint; failed or interrupted same-fingerprint attempts rerun until any configured `max_attempts` budget is exhausted.
7. When code changes are involved, call `verify` before `done`. Include `receipt_id` or `plan_key` when available so `verify` can update `verify:tests`.
8. Call `done` with changed files for file-backed work when you know them; otherwise omit or leave `files_changed` empty and let AWM derive the delta from the receipt baseline. When that detected delta is empty, the closeout is effectively no-file. AWM also treats built-in governance files such as repo-root `AGENTS.md`, `CLAUDE.md`, and canonical `.awm/**` contract files as managed completion scope outside the original code receipt.
9. When code changes are involved, call `verify` before `done`. Include `receipt_id` or `plan_key` when available so `verify` can update `verify:tests`.

When the task changes repo governance or onboarding state such as rules, tags, tests, workflows, or tool-surface behavior, also run `awm sync --mode working_tree --insert-new-candidates` and `awm health --include-details` before `done`.

## Interfaces

- These commands assume installed `awm` and `awm-mcp` binaries are available on `PATH`. An optional `awm-web` binary provides a read-only web dashboard for humans at `http://localhost:8080/` (kanban board, status).
- Preferred CLI path:
  - `awm context ...`
  - `awm fetch ...`
  - `awm work ...`
  - `awm review ...`
  - `awm verify ...`
  - `awm done ...`
- Optional structured JSON automation path:
  - `awm validate --in <request.json>`
  - `awm run --in <request.json>`
  - `awm run --in assets/requests/export.json` for backend-only JSON/Markdown rendering when the task explicitly needs a rendered AWM artifact
- MCP path (JSON-RPC 2.0 over stdin/stdout):
  - `awm-mcp` — start the JSON-RPC server, then send `tools/list` to discover tools and `tools/call` to invoke them
  - See `docs/mcp-reference.md` for protocol details and `skills/awm-broker/assets/requests/` for example payloads

Defaults:
- SQLite backend is default when `AWM_PG_DSN` is unset.
- `AWM_PROJECT_ID` can provide a default project namespace for convenience, `run`, `validate`, and MCP calls; otherwise awm infers from the effective repo root.
- Optional logging controls:
  - `AWM_LOG_LEVEL=debug|info|warn|error`
  - `AWM_LOG_SINK=stderr|stdout|discard`

## Templates

Use templates from `references/templates.md` and `assets/requests/*.json`.
For Codex-first repo setup, see `codex/README.md` and `codex/AGENTS.example.md` inside this skill for the companion docs that pair the global skill with a repo-root `AGENTS.md`.
For OpenCode-first repo setup, see `opencode/README.md` and `opencode/AGENTS.example.md` for the repo-local companion docs installed through `scripts/install-skill-pack.sh --opencode` or `awm init --apply-template opencode-pack`.

## Rules

- Keep all requests valid `awm.v1` JSON contracts.
- Never skip `context` before execution.
- Treat the `context` rules block (or rule pointers) as mandatory requirements.
- Do not invent or silently widen governed file scope. Record discovered paths through `work` when they matter to `review` or `done`.
- If a planned step or review gate becomes obsolete, mark it `superseded` instead of leaving it open or `blocked`.
- Treat advisory scope as `warn` by default unless an explicit `scope_mode` override is required.
- `review` is a thin convenience that lowers to one `work.tasks[]` update. Defaults: `key=review:cross-llm`, `summary="Cross-LLM review"`, `status=complete`.
- When the repo workflow defines a runnable review gate, prefer `run=true` and keep manual `status`, `outcome`, `blocked_reason`, and `evidence` fields for non-run mode.
- Runnable review gates may return a skipped result when the current scoped fingerprint was already assessed or an explicitly configured `max_attempts` budget is exhausted.
- When `work.tasks` is non-empty, include `verify:tests` for executable verification tracking.
- `verify:diff-review` is optional workflow metadata, not a built-in awm completion gate.
- Some repos use `verify` to enforce richer feature-plan schemas built on `kind=feature`, stage tasks, task hierarchy, and leaf-task acceptance criteria. Follow the repo-local contract when it exists.
- Some repos require TDD gates: a `tdd:red` task before implementation for behavior-changing code, or a `tdd:exemption` task with justification. Check the repo's rules and `awm-tests.yaml` selectors.
- When a repo uses stage-based feature plans, keep `plan.stages.*` and the matching `stage:*` tasks aligned; AWM may reconcile the stage fields from those grouping-task statuses during terminal auto-close.
- For code changes, run `verify` before `done` unless the repo rules explicitly allow otherwise.
- For `done`, `scope_mode=strict` blocks on incomplete required completion tasks. When changed files are supplied and no workflow gates are configured, AWM falls back to `verify:tests`; `scope_mode=warn` surfaces warnings.
- `health` and `status` warn on stale work plans, terminal-task plan status drift, and administrative-closeout-only plans.
- No-file `done` calls are valid for legitimate planning, research, or review-only closures.
- If the receipt is too narrow or the task materially changed, refine and re-run `context` instead of guessing.
- Preserve structured JSON output for all broker interactions.
- `export` is an advanced backend-only surface; do not substitute it for the default `context` / `fetch` / `history` / `status` task loop unless the task specifically needs rendered JSON or Markdown output.
- For ad hoc CLI rendering, the read-oriented convenience surfaces `context`, `fetch`, `history`, and `status` accept `--format json|markdown` plus `--out-file` / `--force`, but they still lower into the backend `export` command under the hood.
