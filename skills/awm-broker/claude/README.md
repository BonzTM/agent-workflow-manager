# Claude Command Pack - awm-broker

This folder provides Claude Code slash-command prompts that mirror the `awm-broker` workflow.
The intended default story is the modular core loop: `context`, `work`, `verify`, and `done`, with `fetch` and `review` as supporting surfaces.

This command pack covers the AWM workflow loop only.

## Commands

- `/awm-context [phase] <task text>`
  - requests `context` first, surfaces hard rules, and returns plans and any known initial scope.
  - includes a `fetch` step with `receipt_id` shorthand (or explicit keys when needed).
- `/awm-work <receipt_id-or-plan_key> <tasks-json> [plan-json]`
  - publishes plan/task updates through `work`; use `plan-json` when you need named-plan metadata such as `title` or `mode`.
  - use `plan.discovered_paths` when governed work expands beyond the receipt's initial scope and later `review` or `done` must validate those files.
  - if the repo defines a richer feature-plan contract, this is where `plan.stages`, top-level `stage:*` tasks, task hierarchy, and leaf `acceptance_criteria` should be recorded.
- `/awm-review <receipt_id-or-plan_key> [review-json]`
  - records one workflow review gate through the thin `review` surface, defaulting to `review:cross-llm`; use `{"run":true}` when the repo workflow defines a runnable review gate.
- `/awm-verify <receipt_id-or-plan_key> [comma-separated files] [phase]`
  - runs deterministic repo-defined executable verification from `.awm/awm-tests.yaml` and updates `verify:tests` when work context is available. Omit the file segment only when the receipt baseline or repo selectors make explicit files unnecessary.
- `/awm-done <receipt_id-or-plan_key> [comma-separated files] -- <outcome summary>`
  - runs completion reporting after verification is satisfied and applies effective-scope plus configured completion-gate semantics. Omit the file segment to rely on the baseline-derived delta; if that detected delta is empty, the closeout is effectively no-file.
  - built-in governance files such as repo-root `AGENTS.md`, `CLAUDE.md`, and canonical `.awm/**` contract files are already treated as managed completion scope.

For compact rediscovery of archived plans, receipts, runs, and durable results, use direct CLI `awm history`, setting `--entity work` when you need work-specific `--scope` or `--kind` filters, then `awm fetch` the returned `fetch_keys`. The default command pack does not add a dedicated `/awm-history` slash command.
For runtime and setup diagnostics, use direct CLI `awm status`. It reports active project/backend state, loaded AWM files, integrations, missing setup, and an optional simple `context` preview.
For rendered AWM artifacts, use the backend-only `export` surface through `awm run --in assets/requests/export.json`; `awm-mcp` now uses the JSON-RPC stdio protocol. For export via MCP, send a JSON-RPC `tools/call` request to the running server. The slash-command pack intentionally does not add `/awm-export`. For quick human-facing CLI output, `context`, `fetch`, `history`, and `status` also support `--format json|markdown` with optional `--out-file` / `--force`, and those flags lower to the same backend export path.
When you change rules, tags, tests, workflows, onboarding, or tool-surface behavior, run direct CLI `awm sync --mode working_tree --insert-new-candidates` and `awm health --include-details` before `/awm-done`.

## Install into a project

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/bonztm/agent-workflow-manager/main/scripts/install-skill-pack.sh) --claude
```

Run this from your project root, then restart Claude Code so commands are reloaded.

If you already have this repo checked out locally, the equivalent command is `./scripts/install-skill-pack.sh --claude`.

If the current repo already uses `init`, you can also seed the same files with:

```bash
awm init [--project <id>] [--project-root .] --apply-template claude-command-pack
```

Add `--apply-template claude-hooks` when you also want the optional Claude AWM process guard hooks.
Add `--apply-template git-hooks-precommit` when you also want the staged-file `awm verify` pre-commit hook template.
For repo-local verification scaffolding, pair `init` with `--apply-template verify-generic` for the lowest-friction default, or choose `verify-go`, `verify-ts`, `verify-python`, or `verify-rust` for a language-oriented starter.

## Runtime notes

- Slash-command prompts assume installed `awm` and `awm-mcp` binaries are available on `PATH`.
- Default backend: SQLite unless `AWM_PG_DSN` is set.
- `AWM_PROJECT_ID` can provide a default project namespace; otherwise awm infers from the effective repo root and `AWM_PROJECT_ROOT` when set.
- The optional `claude-hooks` template re-injects the AWM loop at session start or compaction, keeps edits blocked until `/awm-context` succeeds, nudges `/awm-work` once edits span files or governed scope expands, and blocks stop until edited work is closed with `/awm-done`.
- Scope mode defaults to advisory `warn` when `scope_mode` is omitted.
- `/awm-done` can rely on the receipt baseline delta when explicit files are inconvenient. If that detected delta is empty, the closeout is effectively no-file.
- Runnable review gates can carry repo-local script arguments in `.awm/awm-workflows.yaml` `run.argv`, which is where model and reasoning choices should live.
- Use `verify` for deterministic repo checks and `review` for one named workflow signoff gate; they are complementary, not interchangeable.
- The practical sequencing rule is `work` -> `verify` -> `review --run` when a gate exists -> `done`.
- Some repos enforce richer feature-plan schemas through verify-time scripts that inspect `AWM_PLAN_KEY` / `AWM_RECEIPT_ID`; keep that structure in `work`, not in free-form prose.
- Optional logger controls:
  - `AWM_LOG_LEVEL=debug|info|warn|error`
  - `AWM_LOG_SINK=stderr|stdout|discard`
