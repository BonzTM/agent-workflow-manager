# [1.0.0] Release Notes - 2026-03-15

## Release Summary

Initial public release of **awm** (agent-workflow-manager), a modular control plane for AI coding agents. awm gives Claude, Codex, OpenCode, and MCP-compatible clients shared durable state outside any single model session or vendor surface. This release establishes the core workflow, storage backends, agent integrations, web dashboard, and the v1 wire contract.

## Fixed

- N/A (initial release)

## Added

### Core Agent Workflow

- **`awm context`** — Scoped task receipts with hard rules, durable memories, active plans, initial scope paths, and receipt-baseline metadata for later delta detection.
- **`awm work`** — Durable plan and task tracking that survives context compaction and agent handoffs. Supports merge and replace modes, plan stages (`spec_outline`, `refined_spec`, `implementation_plan`), `discovered_paths` for governed scope expansion, and parent/child task hierarchies with `depends_on` edges.
- **`awm memory`** — Evidence-backed, model-agnostic durable memories with category (`decision`, `gotcha`, `pattern`, `preference`), confidence scoring (1-5), governed evidence/related pointer keys, memory tags, and quarantine-to-promotion lifecycle. `--auto-promote` available for immediate promotion.
- **`awm verify`** — Deterministic executable verification driven by `.awm/awm-tests.yaml`. Supports phase-aware and path-aware test selection, `always_run` smoke checks, `--dry-run` for selection preview, and injects `AWM_RECEIPT_ID`, `AWM_PLAN_KEY`, `AWM_VERIFY_PHASE`, `AWM_VERIFY_TAGS_JSON`, and `AWM_VERIFY_FILES_CHANGED_JSON` into test commands.
- **`awm done`** — Governed task closure with receipt-baseline delta detection, scope validation against `initial_scope_paths` + `discovered_paths` + managed governance files, configurable `scope_mode` (warn/strict), and workflow completion-gate enforcement.

### Supporting Agent Surfaces

- **`awm fetch`** — On-demand content hydration by pointer key or receipt-derived plan key. Supports versioned expectations via `--expect`.
- **`awm review`** — Thin workflow signoff gate that lowers to a single work-task merge update. Run mode executes workflow-defined `run` blocks with append-only review-attempt recording, scoped fingerprint deduplication, and bounded `max_attempts`. Manual mode for non-runnable review notes. `--status superseded` for clean obsolescence.
- **`awm history`** — Multi-entity discovery across work plans, memories, receipts, and runs. Supports `--scope`, `--kind`, `--query` text search, and returns `fetch_keys` for selective follow-up.

### Human-Facing Setup and Maintenance

- **`awm init`** — Repository bootstrapping with `.gitignore`-aware file scanning, auto-indexed pointer stubs, and seeding of `.awm/awm-rules.yaml`, `.awm/awm-tags.yaml`, `.awm/awm-tests.yaml`, `.awm/awm-workflows.yaml`, and `.env.example`. Supports `--persist-candidates` and `--project` namespace override.
- **`awm sync`** — Index and rule synchronization in `changed`, `full`, or `working_tree` modes. `--insert-new-candidates` for auto-indexing during sync.
- **`awm health`** — Repository health inspection with `--include-details`, stale plan detection, plan-status drift warnings, and fixers (`sync_working_tree`, `index_unindexed_files`, `sync_ruleset`, `all`). Supports `--dry-run` and `--apply`.
- **`awm status`** — Diagnostics surface reporting active project, backend, loaded rules/tags/tests/workflows, installed integrations, missing setup, and optional `context` preview with `--task-text`.

### Init Templates

- `starter-contract` — Seeds `AGENTS.md`, `CLAUDE.md`, and a richer starter ruleset.
- `detailed-planning-enforcement` — Richer staged-plan contract with `docs/feature-plans.md` and `scripts/awm-feature-plan-validate.py`.
- `verify-generic` — Language-agnostic verification profile.
- `verify-go`, `verify-ts`, `verify-python`, `verify-rust` — Language-specific verification profiles.
- `claude-command-pack` — Slash commands for Claude Code (`.claude/commands/*`).
- `claude-hooks` — Claude hook settings with AWM receipt guard, edit-state tracking, session-context injection, and stop-guard scripts.
- `codex-pack` — Repo-local Codex companion docs (`.codex/awm-broker/`).
- `opencode-pack` — Repo-local OpenCode companion docs (`.opencode/awm-broker/`).
- `git-hooks-precommit` — Pre-commit hook for staged-file `awm verify` gating.

### Agent Integrations

- **Claude Code** — One-line skill-pack installer (`install-skill-pack.sh --claude`) seeds `/awm-context`, `/awm-work`, `/awm-review`, `/awm-verify`, `/awm-done`, and `/awm-memory` slash commands.
- **Codex** — Global skill install (`install-skill-pack.sh --codex`) to `~/.codex/skills/awm-broker` with companion docs.
- **OpenCode** — Repo-local companion docs (`install-skill-pack.sh --opencode`) with explicit CLI/MCP workflow.
- **MCP** — `awm-mcp` adapter exposing 13 tools (5 core, 3 supporting, 1 advanced backend-only, 4 maintenance) via `awm-mcp invoke` and `awm-mcp tools`.

### Storage Backends

- **SQLite** — Zero-config default at `<repo-root>/.awm/context.db`. Supports `AWM_SQLITE_PATH` override.
- **Postgres** — Production multi-writer backend via `AWM_PG_DSN`. Full migration and adapter parity with SQLite.

### Web Dashboard (`awm-web`)

- **Board** (`/`) — Kanban board with Pending, In Progress, Blocked, and Done columns. Tree-sorted tasks with parent/child navigation, detail modals, and rolled-up progress. Scope toggle (Current / Completed / All) with 10-second polling.
- **Memories** (`/memories.html`) — All durable memories with category, content, and confidence display.
- **Status** (`/status.html`) — Project info, loaded sources, installed integrations, and warnings.
- **Health** (`/healthz`) — JSON liveness probe for Kubernetes readiness/liveness checks.
- **Docker** — `Dockerfile.awm-web` for containerized deployment.

### Backend-Only Surfaces

- **`export`** — Stable JSON and Markdown artifact rendering available through `awm run` or MCP. Supports context, fetch, history, and status data.
- **CLI export mode** — `context`, `fetch`, `history`, and `status` accept `--format json|markdown`, `--out-file`, and `--force` for rendered artifact output.

### Configuration

- **Rules** (`.awm/awm-rules.yaml`) — Hard/soft enforcement behavioral constraints with tag-based scoping.
- **Tags** (`.awm/awm-tags.yaml`) — Canonical tag dictionary with alias normalization, layered on embedded base dictionary.
- **Tests** (`.awm/awm-tests.yaml`) — Executable verification definitions with phase/tag/path selectors.
- **Workflows** (`.awm/awm-workflows.yaml`) — Completion gates, runnable review definitions, and fingerprint deduplication.

### Wire Contract

- `spec/v1/` — Full v1 schema definitions: `shared.schema.json`, `cli.command.schema.json`, `cli.result.schema.json`, `mcp.tools.v1.json`.
- CLI/MCP parity enforced across all 13 operations.

### Documentation

- `README.md` — Product overview, install, quick start, CLI reference, and configuration guide.
- `docs/getting-started.md` — Step-by-step adopter walkthrough from zero to working setup.
- `docs/concepts.md` — Core terminology (pointers, receipts, rules, memories, plans, tasks, tags, phases, scope modes).
- `docs/feature-plans.md` — Staged planning contract for governed multi-step work.
- `docs/sqlite.md` — SQLite deployment, backup, and rotation guidance.
- `docs/logging.md` — Structured logging standards.
- `docs/examples/` — Starter templates and example configurations.
- `skills/awm-broker/` — Skill-pack assets, request templates, and reference docs.

### Adoption Modes

- **plans-only** — `init`, `context`, `work` for durable task state.
- **plans+memory** — Add `memory` for shared cross-agent facts.
- **governed workflow** — Add `verify`, `review`, `done` for explicit completion gates and audit history.
- **full brokered flow** — Add `fetch`, explicit history hydration, governed review/closeout, and compact always-loaded context.

## Changed

- N/A (initial release)

## Admin/Operations

- Three binaries: `awm` (CLI), `awm-mcp` (MCP adapter), `awm-web` (web dashboard).
- Environment variables: `AWM_PROJECT_ID`, `AWM_PROJECT_ROOT`, `AWM_SQLITE_PATH`, `AWM_PG_DSN`, `AWM_UNBOUNDED`, `AWM_LOG_LEVEL`, `AWM_LOG_SINK`.
- `.env` auto-loading from repo root with process environment taking precedence.
- Init auto-manages `.gitignore` entries for SQLite database files.

## Deployment and Distribution

- Go install: `go install github.com/bonztm/agent-workflow-manager/cmd/awm@v1.0.0`
- Go install (MCP): `go install github.com/bonztm/agent-workflow-manager/cmd/awm-mcp@v1.0.0`
- Go install (Web): `go install github.com/bonztm/agent-workflow-manager/cmd/awm-web@v1.0.0`
- Source: `https://github.com/BonzTM/agent-workflow-manager`
- Prebuilt binaries: download `awm-binaries` artifact from GitHub Actions `Go Build` workflow.
- Docker (web): `docker build -f Dockerfile.awm-web -t awm-web .`

```bash
go install github.com/bonztm/agent-workflow-manager/cmd/awm@v1.0.0
go install github.com/bonztm/agent-workflow-manager/cmd/awm-mcp@v1.0.0
go install github.com/bonztm/agent-workflow-manager/cmd/awm-web@v1.0.0
```

## Breaking Changes

- N/A (initial release)

## Known Issues

- The OpenCode integration path is explicitly repo-local; no global skill or hook pack is shipped until a verified native OpenCode hook mechanism is documented.
- `verify` and `review` workflow selectors do not currently distinguish thin plans from `kind=feature` plans by themselves.

## Compatibility and Migration

- Requires Go 1.26+ for `go install` or building from source.
- SQLite is the default backend; no external dependencies required for single-writer usage.
- Postgres requires a running instance with `AWM_PG_DSN` configured for multi-writer concurrency.
- This is the initial release. No migration from prior versions is necessary.

## Full Changelog

- Compare changes: https://github.com/BonzTM/agent-workflow-manager/compare/...1.0.0
- Full changelog: https://github.com/BonzTM/agent-workflow-manager/commits/1.0.0
