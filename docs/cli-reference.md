# CLI Reference

This document provides a detailed reference for the `awm` command-line interface. All commands support `--help` for full flag documentation.

## Core Agent-Facing

```bash
awm context        [--project <id>] (--task-text <text>|--task-file <path>) [--phase <plan|execute|review>] [--tags-file <path>] [--scope-path <path>]...
awm work           [--project <id>] [--plan-key <key>|--receipt-id <id>] [--plan-title <text>] [--mode <merge|replace>] [--discovered-path <path>]... [--plan-file <path>|--plan-json <json>] [--tasks-file <path>|--tasks-json <json>]
awm verify        [--project <id>] [--receipt-id <id>] [--plan-key <key>] [--phase <plan|execute|review>] [--test-id <id>]... [--file-changed <path>]... [--files-changed-file <path>|--files-changed-json <json>] [--tests-file <path>] [--tags-file <path>] [--dry-run]
awm done           [--project <id>] --receipt-id <id> [--file-changed <path>]... [--files-changed-file <path>|--files-changed-json <json>] (--outcome <text>|--outcome-file <path>) [--scope-mode <strict|warn>] [--tags-file <path>]
```

`done` accepts omitted or empty `files_changed`. AWM computes the task delta from the receipt baseline when that baseline is available, so omission can mean "auto-detect the real delta" rather than only "no-file closure." When the detected delta is empty, the closeout is effectively no-file. When files are supplied explicitly, AWM cross-checks them against the detected delta, surfaces mismatches as violations, and still uses the detected delta as the source of truth for scope and completion-gate checks. For scope validation, `done` accepts receipt `initial_scope_paths`, plan `discovered_paths`, AWM-managed governance files (including repo-root `AGENTS.md`, `CLAUDE.md`, and canonical `.awm/**` sources), and path-like entries from `plan.in_scope` so feature-wide plans can close cleanly without forcing every owned path into the original receipt.

## Supporting Agent-Facing

```bash
awm fetch          [--project <id>] [--key <key>]... [--keys-file <path>|--keys-json <json>] [--expect <key=version>]... [--expected-versions-file <path>|--expected-versions-json <json>] [--receipt-id <id>]
awm review         [--project <id>] (--receipt-id <id>|--plan-key <key>) [--run] [--key <task-key>] [--summary <text>] [--status <pending|in_progress|complete|blocked|superseded>] [--outcome <text>|--outcome-file <path>] [--blocked-reason <text>] [--evidence <text>]... [--evidence-file <path>|--evidence-json <json>] [--tags-file <path>]
awm history        [--project <id>] [--entity <all|work|receipt|run>] [--query <text>|--query-file <path>] [--scope <current|deferred|completed|all>] [--kind <kind>] [--limit <n>] [--unbounded]
```

If `--project` is omitted, convenience commands default to `AWM_PROJECT_ID` and otherwise infer the project from the effective repo root name. Explicit `--project` still wins.

For raw rendered artifacts, use the backend-only `export` command through `awm run` or MCP. Example request envelope: [docs/examples/export-request.json](docs/examples/export-request.json).

For ad hoc CLI rendering, `context`, `fetch`, `history`, and `status` also accept `--format json|markdown`, plus `--out-file` and `--force` for raw artifact output through the same backend export path. `--out-file` and `--force` require `--format`, and `--force` requires `--out-file`. Example command: [docs/examples/context-export-command.txt](docs/examples/context-export-command.txt).

```bash
# Markdown artifact written to a file
awm context --task-text "resume payment work" --phase execute --format markdown --out-file artifacts/context.md --force

# Stable JSON document on stdout
awm history --entity run --limit 10 --format json
```

Export documents cover the `context`, `plan`, `receipt`, `task`, `run`, `fetch_bundle`, `history`, and `status` kinds. The `plan`, `receipt`, `task`, and `run` kinds render through `fetch` selectors (see the request envelope example above); the rest map one-to-one onto the read surfaces.

Most list and text flags support inline values and `--*-file` alternatives (`-` for stdin). JSON list/object inputs also support `--*-json` for one-shot agent calls without temporary files.

`review` is intentionally thin — it lowers to a single `work.tasks[]` merge update.

| Surface | `verify` | `review` |
|---|---|---|
| Source of truth | `.awm/awm-tests.yaml` | `.awm/awm-workflows.yaml` |
| Execution fan-out | zero or more selected checks | exactly one named gate |
| Recorded task | `verify:tests` | one review task such as `review:cross-llm` |
| Primary use | deterministic executable checks | workflow signoff or secondary reviewer gate |
| Wrong use | not a substitute for reviewer signoff | not a substitute for bulk repo checks |

**Defaults** (when flags are omitted): `key=review:cross-llm`, `summary="Cross-LLM review"`, `status=complete`.

**Run mode** (`--run` or `run=true`): awm loads the matching task from `.awm/awm-workflows.yaml`, executes its `run` block, persists an append-only review-attempt record, and updates the work-task snapshot. When fingerprint dedupe is enabled, AWM only skips reruns after a passing attempt already assessed the current fingerprint; failed or interrupted same-fingerprint attempts rerun until any configured `max_attempts` budget is exhausted. `done` requires a passing runnable review for the current fingerprint when dedupe is enabled. The scoped fingerprint covers effective scope: receipt `initial_scope_paths`, work-declared `discovered_paths`, and AWM-managed governance files that completion reporting already allows outside project file scope, including repo-root `AGENTS.md` and `CLAUDE.md`.
If the repo has uncommitted changes but the current effective scope captures none of them, the review runner should block and tell you to refresh `context` or declare the missing files through `work` rather than silently reviewing nothing.

**Manual mode** (no `--run`): use `--status`, `--outcome`, `--blocked-reason`, and `--evidence` to record a review note directly. Manual notes do not satisfy runnable review gates that define `run`, and these fields are ignored in run mode. Use `status=superseded` when a review gate or planned step is obsolete and should close cleanly instead of lingering as blocked.

Keep raw reviewer commands in repo-local scripts and workflow definitions, not maintainer prose. If a repo-local reviewer script needs model, reasoning, or sandbox settings, pass them through the workflow `run.argv` list.

**History discovery:** use `awm history` for both work-specific and multi-entity discovery. Set `--entity work` when you need `--scope` or `--kind`; use other entities for receipts or runs. Results include `fetch_keys` for follow-up `awm fetch`.

## Human-Facing Setup And Maintenance

```bash
awm init          [--project <id>] [--project-root .] [--apply-template <id>]... [--persist-candidates] [--respect-gitignore] [--output-candidates-path <path>] [--rules-file <path>] [--tags-file <path>]
awm sync          [--project <id>] --mode <changed|full|working_tree> [--git-range <range>] [--project-root <path>] [--insert-new-candidates] [--rules-file <path>] [--tags-file <path>]
awm health        [--project <id>] [--include-details] [--max-findings-per-check <n>] | [--fix <name>]... [--dry-run|--apply] [--project-root <path>] [--rules-file <path>] [--tags-file <path>]
awm status        [--project <id>] [--project-root <path>] [--rules-file <path>] [--tags-file <path>] [--tests-file <path>] [--workflows-file <path>] [--task-text <text>|--task-file <path>] [--phase <plan|execute|review>]
```

`awm health` is the only human CLI surface for repository health. Use it without `--fix` to inspect drift, add `--fix <name>` to apply a specific fixer by default, add `--dry-run` to preview without changing state, or use `--fix all` / `--apply` with no `--fix` values to run the default fixer set. It now warns on stale work plans, plans whose tasks are all terminal but whose plan status drifted, and plans left open only for administrative closeout. Run `awm health --help` to see the available fixers and preview/apply examples.

`awm status` is the preferred diagnostics surface. It reports the active project, runtime/backend details, loaded rules/tags/tests/workflows, installed init-managed integrations, missing setup, and non-blocking warnings for stale or drifted plans. With `--task-text`, it also previews the simplified `context` load.

## Optional Structured JSON Contract Mode

Use direct convenience commands for day-to-day work. `awm run` and `awm validate` operate on the full `awm.v1` request envelope when you want:

- One complete JSON request per call (scripts, CI)
- Request fixtures checked into a repo for repeatable workflows
- Payload validation before execution

Envelope shape:

```json
{
  "version": "awm.v1",
  "command": "context",
  "request_id": "req-context-001",
  "payload": {
    "project_id": "my-cool-app",
    "task_text": "add input validation to the signup form",
    "phase": "execute"
  }
}
```

Run or validate it with:

```bash
awm run --in request.json
awm validate --in request.json
```

MCP tools use the same payload schema but omit the outer envelope because the tool name already identifies the command. See [Schema Reference](spec/v1/README.md) and [skills/awm-broker/assets/requests](skills/awm-broker/assets/requests) for worked request examples.
Structured payloads may omit `project_id` when runtime defaults are configured; `awm run` and `awm validate` resolve it in the same order as convenience commands. The MCP server (`awm-mcp`) uses JSON-RPC 2.0 `tools/call` requests over stdin/stdout; see [MCP Reference](docs/mcp-reference.md).
