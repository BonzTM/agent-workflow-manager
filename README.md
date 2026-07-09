# awm — Modular Control Plane for AI Coding Agents

awm is a repo-owned control plane for AI coding agents. It gives Claude, Codex, and MCP clients shared durable state outside any one model session or vendor surface.

- **Rules and receipts replace giant always-loaded markdown** — hard rules are carried in task receipts, not left to best-effort prompt compliance.
- **Work plans survive context loss and agent handoffs** — plans and tasks live in SQLite or Postgres, so a later `context` call can resume the real state of the work.
- **Closure is auditable** — `verify`, `review`, and `done` record what was checked, what was required, and what closed the task.
- **`context` hydrates work without replacing native search** — awm frames the task with rules, active work, and any explicitly known initial scope while the agent still uses its own file-reading tools.

awm is intentionally modular. You can adopt only the pieces you need.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/architecture/awm-flow-diagram-dark.png">
  <source media="(prefers-color-scheme: light)" srcset="docs/architecture/awm-flow-diagram.png">
  <img alt="awm control plane workflow" src="docs/architecture/awm-flow-diagram.png">
</picture>

## Adoption Modes

- **plans-only**: use `init`, `context`, and `work` when the main need is durable task state that survives compaction or cross-agent handoff.
- **governed workflow**: add `verify`, `review`, and `done` when you want explicit completion gates and audit history.
- **full brokered flow**: use `context`, explicit `fetch` / history hydration, and governed review/closeout when you also want compact always-loaded context and receipt-scoped execution.

The repo you are reading uses a stricter dogfood workflow than many adopters need. `awm init` starts with the minimal core and lets you opt into heavier templates later.
Prefer the current `init/context/work/verify/done` story over older compatibility aliases when they disagree.

## Install

Preferred install path:

```bash
go install github.com/bonztm/agent-workflow-manager/cmd/awm@latest
go install github.com/bonztm/agent-workflow-manager/cmd/awm-mcp@latest
go install github.com/bonztm/agent-workflow-manager/cmd/awm-web@latest
```

Go installs binaries to `$GOBIN` if it is set, otherwise to `$(go env GOPATH)/bin` (typically `~/go/bin`). That directory must be on your `PATH`.

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

If you want prebuilt binaries instead, download the `awm-binaries` artifact from a successful `Go Build` GitHub Actions run and place `awm`, `awm-mcp`, and `awm-web` on your `PATH`.

If you are working from a checkout, build from source:

```bash
git clone https://github.com/bonztm/agent-workflow-manager.git
cd agent-workflow-manager
go build -o dist/awm ./cmd/awm
go build -o dist/awm-mcp ./cmd/awm-mcp
go build -o dist/awm-web ./cmd/awm-web
```

## Quick Start (5 minutes)

### 1. Initialize your project

Scan your repo, seed repo-local AWM files, and materialize the initial AWM inventory:

```bash
awm init
```

Init respects `.gitignore` by default. It also:

- Seeds `.awm/awm-rules.yaml`, `.awm/awm-tags.yaml`, `.awm/awm-tests.yaml`, and `.awm/awm-workflows.yaml` when missing
- Appends `.awm/context.db`, `.awm/context.db-shm`, and `.awm/context.db-wal` to `.gitignore`
- Creates or extends `.env.example`
- Auto-indexes discovered repo files into pointer stubs so `fetch`, health, and governed scope checks work immediately

Use `--persist-candidates` to save the enumerated file list to `.awm/init_candidates.json`.

When `--project` is omitted, awm resolves the project namespace from `AWM_PROJECT_ID` first and otherwise infers it from the repo root folder name. Pass `--project` explicitly when you want a stable namespace that differs from the folder name.

If you want a heavier starter, rerun `init` with one or more additive templates:

```bash
awm init \
  --apply-template starter-contract \
  --apply-template verify-generic \
  --apply-template claude-command-pack \
  --apply-template claude-hooks \
  --apply-template git-hooks-precommit
```

`--apply-template` is repeatable and safe to re-run. Templates only create missing files, upgrade pristine scaffolds, and merge additive JSON fragments (e.g. `.claude/settings.json`). They never delete files or overwrite files you've edited. Template names that have been removed or renamed resolve through a deprecated-alias map to their current replacement, so recorded init commands keep working across releases.
Add `--apply-template codex-pack` when you want repo-local Codex companion docs under `.codex/awm-broker/`.
Add `--apply-template codex-hooks` when you want the current experimental repo-local Codex hook layer under `.codex/`.
Add `--apply-template opencode-pack` when you want repo-local OpenCode companion docs under `.opencode/awm-broker/`.

Starter verify profiles:

- `verify-generic` — language-agnostic `.awm/awm-tests.yaml` that works out of the box
- `verify-go` — Go-oriented `.awm/awm-tests.yaml`
- `verify-ts` — TypeScript-oriented `.awm/awm-tests.yaml`
- `verify-python` — Python-oriented `.awm/awm-tests.yaml`
- `verify-rust` — Rust-oriented `.awm/awm-tests.yaml`

Planning profile:

- `detailed-planning-enforcement` — seeds `docs/feature-plans.md` and `scripts/awm-feature-plan-validate.py`, and upgrades pristine `starter-contract` and `verify-*` scaffolds (generic or language profiles) to the richer feature-planning workflow

Tooling companions:

- `codex-pack` — seeds `.codex/awm-broker/README.md` and `.codex/awm-broker/AGENTS.example.md` so Codex has repo-local companion docs in addition to the global skill install
- `codex-hooks` — seeds `.codex/config.toml`, `.codex/hooks.json`, and `.codex/hooks/*` to enable Codex's current experimental lifecycle hooks for startup reminders, prompt-time nudges, and one-time closeout guards
- `opencode-pack` — seeds `.opencode/awm-broker/README.md` and `.opencode/awm-broker/AGENTS.example.md` for the explicit repo-local OpenCode companion path
- `claude-command-pack` — seeds `.claude/commands/*` and `.claude/awm-broker/*`
- `claude-hooks` — seeds `.claude/hooks/awm-receipt-guard.sh`, `.claude/hooks/awm-receipt-mark.sh`, `.claude/hooks/awm-session-context.sh`, `.claude/hooks/awm-edit-state.sh`, and `.claude/hooks/awm-stop-guard.sh` to inject the AWM loop at session start, block edits until `/awm-context` succeeds, require `/awm-work` before untracked multi-file edits, and block stop until edits are reported
- `git-hooks-precommit` — seeds `.githooks/pre-commit` for staged-file `awm verify` gating; enable with `git config core.hooksPath .githooks`

### 2. Fill in your seeded rules

Init creates `.awm/awm-rules.yaml` if it does not already exist. Replace the blank scaffold with your project rules:

```yaml
version: awm.rules.v1
rules:
  - id: rule_context_first
    summary: Always call context before reading or editing files.
    enforcement: hard
    tags: [startup]

  - id: rule_done
    summary: Close every task with done.
    enforcement: hard
    tags: [completion]

  - id: rule_verify_before_completion
    summary: Run verify before done when code changes.
    enforcement: hard
    tags: [verification]
```

### 3. Sync rules into awm

```bash
awm sync --mode working_tree
```

### 4. Set up agent integration

Wire agents to awm via slash commands, skill packs, or MCP tools — see [Getting Started](docs/getting-started.md) for adopter setup details.

Once connected, most adopters mainly use `context`, `work`, `verify`, and `done`, with `fetch`, `review`, and `history` as supporting surfaces. The advanced backend-only `export` surface is available through `awm run` or MCP when you need stable JSON or Markdown artifact rendering; for ad hoc use, `context`, `fetch`, `history`, and `status` accept `--format json|markdown` (with `--out-file`/`--force`) through the same export path. You can test any operation manually via CLI (e.g., `awm context --task-text "fix the login bug" --phase execute`). See the [CLI Reference](docs/cli-reference.md) and [MCP Reference](docs/mcp-reference.md) for details.

If you are maintaining AWM itself rather than adopting it in another repo, use [AGENTS.md](AGENTS.md), [docs/maintainer-map.md](docs/maintainer-map.md), and [docs/maintainer-reference.md](docs/maintainer-reference.md) for the repo's maintainer workflow. This README stays product-facing.

## Agent Integration

### Claude Code (slash commands)

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/bonztm/agent-workflow-manager/main/scripts/install-skill-pack.sh) --claude
```

Run this from your project root. It installs `/awm-context`, `/awm-work`, `/awm-review`, `/awm-verify`, and `/awm-done` slash commands into `.claude/commands/`.

If you already have this repo checked out locally, the equivalent command is `./scripts/install-skill-pack.sh --claude`.

### Codex (skill pack)

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/bonztm/agent-workflow-manager/main/scripts/install-skill-pack.sh) --codex
```

Installs the awm-broker skill to `~/.codex/skills/awm-broker` so Codex can share the same AWM plans, verification state, and completion history used by Claude or MCP clients. The installed skill also includes `codex/README.md` and `codex/AGENTS.example.md` companion docs.

If you already have this repo checked out locally, the equivalent command is `./scripts/install-skill-pack.sh --codex`.

If you want repo-local Codex companion files in the project itself, also run:

```bash
awm init --apply-template codex-pack
```

That seeds `.codex/awm-broker/README.md` and `.codex/awm-broker/AGENTS.example.md`. Keep the repo-root `AGENTS.md` authoritative; the Codex companion files are there to make the full AWM loop explicit for Codex-driven repos, not to replace the root contract.

If you also want the experimental repo-local Codex hook layer, run:

```bash
awm init --apply-template codex-hooks
```

That seeds `.codex/config.toml`, `.codex/hooks.json`, and `.codex/hooks/*`. The hook layer is opt-in and intentionally narrower than Claude's hook pack: it currently only covers startup guidance, prompt-time context nudges, and a one-time stop reminder, and it depends on Codex's current experimental hook support.

Codex can drive the same core workflow directly: `context`, `work`, `verify`, `review`, and `done`. The docs-only path still works with just the installed skill, repo-root `AGENTS.md`, and normal CLI/MCP access; `codex-hooks` is an additional experimental helper, not a parity claim.

### OpenCode (repo-local companion docs)

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/bonztm/agent-workflow-manager/main/scripts/install-skill-pack.sh) --opencode
```

Run this from your project root. It installs `.opencode/awm-broker/README.md` and `.opencode/awm-broker/AGENTS.example.md` into the current repo.

If you already have this repo checked out locally, the equivalent command is `./scripts/install-skill-pack.sh --opencode`.

Use `--opencode` when you want to add the OpenCode companion docs to an existing repo immediately.

If you prefer to seed the same repo-local companion files through `init`, run:

```bash
awm init --apply-template opencode-pack
```

Use `opencode-pack` when you are bootstrapping a repo with `awm init` and want the OpenCode companion docs created alongside the rest of the starter AWM assets.

That seeds `.opencode/awm-broker/README.md` and `.opencode/awm-broker/AGENTS.example.md`. Keep the repo-root `AGENTS.md` authoritative; the OpenCode companion path is intentionally explicit and repo-local until this repo documents a verified stronger native OpenCode integration surface.

OpenCode can drive the same core workflow directly: `context`, `work`, `verify`, `review`, and `done`. Use OpenCode's native repo search and edit tools normally; AWM supplies durable context, planning state, verification, review gates, and completion reporting.

Minimal walkthrough:

1. Install the repo-local companion docs with `--opencode` or seed them during `awm init` with `opencode-pack`.
2. Keep the repo-root `AGENTS.md` as the source of truth.
3. In OpenCode, start real work with `awm context --project <id> --task-text "..." --phase plan|execute|review`.
4. For multi-step work, persist plan/task state with `awm work` and declare `plan.discovered_paths` when governed scope expands.
5. Before closing, run `awm verify`, then `awm review --run` when the workflow requires it, then `awm done`.
6. If the task produced a reusable decision or pitfall, record it for future reference.

For already isolated hosts such as devcontainers, LXC containers, or similar outer sandboxes, prefer repo workflow review argv that use `--yolo` on `scripts/awm-cross-review.sh`. That keeps the review runner from fighting a second nested sandbox while still preserving the outer isolation boundary.

Unlike the Claude path, AWM does not currently ship an OpenCode hook pack. That is intentional: this repo has not yet documented a verified native OpenCode hook mechanism, so the supported path stays explicit and inspectable through repo-local docs plus normal CLI/MCP access.

### MCP (tool-native models)

Twelve tools are exposed to tool-native models through the MCP interface. See [MCP Reference](docs/mcp-reference.md) for tool details and typical closeout sequences.

## CLI Reference

All commands support `--help` for full flag documentation. See [CLI Reference](docs/cli-reference.md) for the complete command reference.

## Web Dashboard

`awm-web` is a read-only web dashboard that gives humans a live view of what agents are working on. It reuses the same `core.Service` and storage backend as `awm` and `awm-mcp`, bundled into a single binary via `go:embed`.

### Running

```bash
awm-web                       # starts on :8080
awm-web serve --addr :9090    # custom port
```

### Pages

| Page | URL | Description |
|---|---|---|
| Board | `/` | Kanban board with Pending, In Progress, Blocked, and Done columns. Tasks are tree-sorted so children appear beneath their parent. Click any card for a detail modal with navigable parent/child/dependency links and rolled-up progress for parent tasks. |
| Status | `/status.html` | Project info, loaded sources, installed integrations, and warnings. |
| Health | `/healthz` | JSON liveness probe for k8s readiness/liveness checks. |

The board supports a scope toggle (Current / Completed / All) and polls the API every 10 seconds.

### Configuration

`awm-web` reads the same environment variables as `awm` and `awm-mcp` (`AWM_PROJECT_ID`, `AWM_PG_DSN`, `AWM_SQLITE_PATH`, etc.). No additional configuration is needed beyond what you already have for the CLI.

### Docker

A `Dockerfile.awm-web` is provided for containerized deployment:

```bash
docker build -f Dockerfile.awm-web -t awm-web .
docker run -p 8080:8080 -e AWM_PG_DSN='...' awm-web
```

## Storage Backend

SQLite is zero-config by default. awm resolves config in this order:

1. Process environment (`AWM_*`)
2. Explicit `--project` / `project_id` wins when provided
3. Otherwise `AWM_PROJECT_ID` sets the default project namespace
4. Otherwise `AWM_PROJECT_ROOT` pins the repo root when running awm from another directory and the repo-root name is inferred
5. Repo-root `.env` is loaded when present
6. If `AWM_PG_DSN` is set, Postgres is used
7. Otherwise SQLite defaults to `<repo-root>/.awm/context.db`

If no explicit path is set and no project root can be resolved at all, awm fails with an error naming `AWM_SQLITE_PATH` and `AWM_PROJECT_ROOT` as remedies. It never falls back to an implicit temp-directory database, so durable state is never silently written somewhere ephemeral.

Init scaffolding is responsible for adding the implicit SQLite files to `.gitignore` when you want repo-local setup materialized.

Set `AWM_PG_DSN` for Postgres when you need write concurrency.

```bash
# Optional stable namespace override when folder names vary
export AWM_PROJECT_ID=my-cool-app

# SQLite override
export AWM_SQLITE_PATH=/path/to/context.db

# Postgres
export AWM_PG_DSN='postgres://user:pass@localhost:5432/agents_context?sslmode=disable'
```

See [SQLite Operations](docs/sqlite.md) for deployment, backup, and rotation guidance.

## Documentation

User guides:

- [Getting Started](docs/getting-started.md) — full walkthrough from zero to working awm setup
- [Integration Guide](docs/integration.md) — wire AWM into any MCP-compatible runtime
- [CLI Reference](docs/cli-reference.md) — complete `awm` command reference
- [MCP Reference](docs/mcp-reference.md) — tool-native model integration reference
- [Web Dashboard](#web-dashboard) — read-only kanban board and status pages
- [Concepts](docs/concepts.md) — what pointers, receipts, rules, plans, and tags are
- [SQLite Operations](docs/sqlite.md) — deployment, backup, and rotation
- [Schema Reference](spec/v1/README.md) — v1 wire contract schemas
- [Skill Templates](skills/awm-broker/references/templates.md) — request/response examples

Architecture (contributors):

- [Logging Standards](docs/logging.md) — structured logging contract

## Configuration Files

awm doesn't ship project rules or opinions. You author configuration in repo-local YAML files, and awm discovers, ingests, and enforces them.

### Rules (`.awm/awm-rules.yaml`)

Define behavioral constraints for agents. Hard rules are always included in receipts; soft rules are summary-only. Use `--rules-file` on `sync`, `health --fix`, or `init` to override auto-discovery.

### Tags (`.awm/awm-tags.yaml`)

Repo-local canonical tag aliases that extend awm's embedded base dictionary. Merged on every runtime call. Use `--tags-file` on any command that does tag normalization to override.

### Verification (`.awm/awm-tests.yaml`)

Repo-defined executable checks for `verify`. v1 definitions are argv-only. Use `--tests-file` on `verify` to override auto-discovery.

When verify context is available, AWM also injects generic metadata for repo-local scripts:

- `AWM_RECEIPT_ID` and `AWM_PLAN_KEY`
- `AWM_VERIFY_PHASE`
- `AWM_VERIFY_TAGS_JSON`
- `AWM_VERIFY_FILES_CHANGED_JSON`

That metadata is policy-neutral. Repos can use it for targeted test selection, plan-aware guards, or other local workflow checks without making those policies part of AWM's product defaults.

### Repo-Local Staged Plan Conventions

AWM's built-in `work` schema already supports richer planning detail through `plan.stages`, `parent_task_key`, `depends_on`, and `acceptance_criteria`. Repos can layer a stricter feature-planning contract on top of those fields without changing AWM itself.

This repo does that for governed multi-step work: root plans use `kind=feature`, `kind=maintenance`, or `kind=governance`, track `spec_outline` / `refined_spec` / `implementation_plan`, group work under top-level `stage:*` tasks, and treat leaf tasks with exact `references` plus explicit `acceptance_criteria` as the atomic units of execution. Terminal plan auto-close also reconciles the plan-stage fields from those `stage:*` task statuses so completed staged plans do not linger with stale stage metadata. `awm verify` enforces the contract through `scripts/awm-feature-plan-validate.py`. That stricter staged-plan schema is repo policy, not an AWM product default. See [docs/feature-plans.md](docs/feature-plans.md).

### Workflows (`.awm/awm-workflows.yaml`)

Completion gates that control which work task keys must be satisfied before `done` succeeds. Runnable review gates can define `max_attempts` and `rerun_requires_new_fingerprint` for bounded final-gate retries. When no workflow gates are configured, awm falls back to requiring `verify:tests`.

### Init Templates

Templates are seed-only — they create missing files but never overwrite edited ones. Built-ins:

| Template | What it seeds |
|---|---|
| `starter-contract` | `AGENTS.md`, `CLAUDE.md`, richer starter ruleset |
| `detailed-planning-enforcement` | Richer feature-plan contract, `docs/feature-plans.md`, and `scripts/awm-feature-plan-validate.py` |
| `verify-generic` | Language-agnostic `.awm/awm-tests.yaml` that works out of the box |
| `verify-go` | Go-oriented `.awm/awm-tests.yaml` |
| `verify-ts` | TypeScript-oriented `.awm/awm-tests.yaml` |
| `verify-python` | Python-oriented `.awm/awm-tests.yaml` |
| `verify-rust` | Rust-oriented `.awm/awm-tests.yaml` |
| `codex-pack` | `.codex/awm-broker/README.md`, `.codex/awm-broker/AGENTS.example.md` |
| `codex-hooks` | `.codex/config.toml`, `.codex/hooks.json`, `.codex/hooks/*` |
| `opencode-pack` | `.opencode/awm-broker/README.md`, `.opencode/awm-broker/AGENTS.example.md` |
| `claude-command-pack` | `.claude/commands/*`, `.claude/awm-broker/*` |
| `claude-hooks` | Claude hook settings plus AWM process guard scripts |
| `git-hooks-precommit` | `.githooks/pre-commit` |

See [docs/examples/init-templates.md](docs/examples/init-templates.md) for usage examples. Format references: [awm-rules.yaml](docs/examples/awm-rules.yaml), [awm-tags.yaml](docs/examples/awm-tags.yaml), [awm-workflows.yaml](docs/examples/awm-workflows.yaml). For a concrete richer planning contract layered on top of AWM's built-in schema, see [docs/feature-plans.md](docs/feature-plans.md). Full authoring workflow: [Getting Started](docs/getting-started.md).

## Environment Variables

```bash
export AWM_PROJECT_ID=my-cool-app      # optional stable project namespace
export AWM_PROJECT_ROOT=/path/to/repo  # optional when running awm from another directory
export AWM_UNBOUNDED=false             # true removes built-in history/list caps for supported surfaces
export AWM_LOG_LEVEL=debug             # debug|info|warn|error (default: info)
export AWM_LOG_SINK=stderr             # stderr|stdout|discard (default: stderr)
```

## License

This project is licensed under the Apache License, Version 2.0 (`Apache-2.0`). See [LICENSE](LICENSE).
