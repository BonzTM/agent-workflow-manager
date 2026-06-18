# awm v1 Schemas

This directory defines the v1 wire contract for AWM's modular control plane, including the preferred diagnostics surface `status`.

## Files

- `shared.schema.json`: reusable types used by CLI and MCP contracts.
- `cli.command.schema.json`: input envelope for `awm` CLI JSON mode.
- `cli.result.schema.json`: output envelope for `awm` CLI JSON mode.
- `mcp.tools.v1.json`: MCP tool contracts with exact input/output schema refs.

## CLI Contract

`awm` in JSON mode should accept a request matching `cli.command.schema.json` and emit a response matching `cli.result.schema.json`.
For project-scoped commands, `project_id` may be omitted when runtime defaults are configured. Resolution order is explicit `--project` / payload `project_id`, then `AWM_PROJECT_ID`, then inferred effective repo root name.

## MCP Contract

The MCP adapter exposes twelve public contract tools. CLI convenience syntax maps onto the same operations when a convenience route exists:

Agent-facing:

1. `context`
2. `fetch`
3. `export`
4. `done`
5. `review`
6. `work`
7. `history`

Maintenance:

8. `sync`
9. `health`
10. `status`
11. `verify`
12. `init`

Tool input/output shapes are referenced from CLI payload/result defs to guarantee parity.

The MCP flow centers on durable state and governed closure, not ranked retrieval:

- `context` returns a receipt with scoped rules, current plans, and any explicitly known initial scope paths.
- Each rule entry now includes `rule_id`, a deterministic stable identifier derived from the existing rule `key` semantics (no additional input required).
- `fetch` resolves receipt/plan-scoped artifacts by key, or derives the plan fetch key from `receipt_id` when keys are omitted.
- `export` is the backend-only rendering surface for stable JSON or Markdown output. It is available through `awm run` / MCP, not as a standalone convenience subcommand.
- `review` remains work-backed and defaults to `review:cross-llm` with `complete` status when callers omit manual fields; it can also execute a workflow-defined `run` command before recording the final review task status.
- `work` creates/updates structured plans with tasks (max 256 per request). Supports `receipt_id` without `plan_key` (derives `plan_key` as `plan:<receipt_id>`). `mode` controls merge vs replace semantics.
- `history` lists or searches compact work, receipt, and run history and returns targeted `fetch_keys` for selective follow-up fetches. `entity` defaults to `all`; `scope` and `kind` are only valid when `entity=work`.
- For work updates, `verify:tests` is the built-in executable verification key. `verify:diff-review` is optional workflow metadata.
- `verify` selects repo-defined executable checks from `.awm/awm-tests.yaml` or `awm-tests.yaml`, with `tests_file` as the explicit override.
- `status` reports the active project/backend/runtime snapshot, loaded rules/tags/tests/workflows, installed init-managed integrations, and optionally a simple `context` preview when callers provide `task_text`.
- `init` accepts repeatable `apply_templates` ids and reports per-template `template_results`. Template application is additive-only: create missing files, upgrade AWM-owned pristine scaffolds, and merge known additive JSON fragments without overwriting edited repo files.
- `done` can enforce repo-defined completion task keys from `.awm/awm-workflows.yaml` or `awm-workflows.yaml`; runnable review gates may also require a fresh passing attempt for the current scoped fingerprint when fingerprint dedupe is enabled. When no workflow gates are configured, awm falls back to `verify:tests`.
- `done` accepts either `receipt_id` or `plan_key`. `plan_key` derives the effective receipt as `plan:<receipt_id>`, then uses the selected plan's discovered scope together with the receipt's initial scope.

`done.scope_mode` defaults to `warn` when omitted. `done.files_changed` is optional, but no-file closeout requires either a reliable receipt baseline with no detected changes or an explicit `no_file_changes=true` declaration. When changed files are supplied or detected and work tasks are present, `scope_mode=strict` enforces configured completion checks and `scope_mode=warn` surfaces warnings.

## Notes

- These schemas are framework-agnostic and can be implemented in Go, TypeScript, Rust, or Python.
- Recommended implementation approach for Go:
  - define structs from schema
  - validate with a JSON Schema library at ingress/egress
  - keep context, plan, scope, and verification logic in a shared core package used by both CLI and MCP adapters.
