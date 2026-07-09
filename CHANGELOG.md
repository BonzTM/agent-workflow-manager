# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.4.0] - 2026-07-09

Repository hardening release bringing awm to the shared house standard with its
sibling `agent-context-manager`: a single `make verify` gate enforced by CI
(which previously never ran tests), codebase-wide lint remediation, a
consistent release pipeline with per-architecture checksummed archives and
dual-form tags, security-driven dependency and toolchain upgrades, and the
standard governance meta files. No command, contract, storage, or MCP surface
changes.

### Added

- House-standard repository governance, matching agent-context-manager:
  `.github/CODEOWNERS`, weekly grouped Dependabot updates (`gomod` +
  `github-actions`), a pull-request template, and `.editorconfig`.
- A single canonical verification gate: `make verify` runs tidy-check
  (`go mod tidy -diff`, read-only so CI fails on committed go.mod/go.sum
  drift), format check (gofumpt + gci), golangci-lint, vet, tests, the race
  detector, govulncheck, and the build. golangci-lint and govulncheck are
  pinned as `go.mod` tool directives, and the full lint policy
  (`.golangci.yml`) was applied across the codebase.
- CI (`ci.yml`) runs `make verify` plus a six-target cross-compile check
  (linux/darwin/windows on amd64/arm64) on every push and pull request,
  replacing the build-only `go-build.yml` — tests, lint, race, and
  vulnerability scanning now gate every change.

### Changed

- Release archives include per-archive `.sha256` checksums, and the release
  workflow mirrors whichever tag form is missing onto the release commit —
  bare `X.Y.Z` stays the house tag convention, while the `vX.Y.Z` alias makes
  `go install github.com/bonztm/agent-workflow-manager/cmd/awm@latest`
  resolve the canonical release instead of a `main` pseudo-version. The
  workflow also supports `workflow_dispatch` to rebuild assets for an
  existing tag. Retroactive `v` aliases were pushed for every existing
  release tag (`v1.0.0`–`v1.3.0`); note that only `v1.3.0` and later are
  resolvable module versions, since older commits predate the module-path
  rename.
- Displayed versions are v-less everywhere (`awm --version`, stamped
  binaries); the `v` prefix exists only on git tags.
- Codebase-wide mechanical lint remediation (no behavior changes): error-wrap
  and shadowing fixes, godoc contracts on every exported identifier,
  dead-code removal, modernized idioms, checked type assertions in the
  command dispatch layer (payload mismatches now return `INTERNAL_ERROR`
  instead of panicking), and tightened file permissions for private state.

### Security

- Upgraded `github.com/jackc/pgx/v5` 5.8.0 → 5.10.0. The 5.9.2 floor fixes
  GO-2026-5004 (SQL injection via placeholder confusion with dollar-quoted
  string literals), which the new `govulncheck` gate flagged on a called path
  (`ListReviewAttempts`); Dependabot then took the minor-patch group to
  5.10.0 (with `modernc.org/sqlite` 1.46.1 → 1.53.0).
- Raised the `go` directive to 1.26.5, whose standard library fixes
  GO-2026-5856 (`crypto/tls`) and GO-2026-4970 (`os`) — both reached from awm
  code paths and flagged by the CI vulnerability gate.

See [docs/release-notes/RELEASE_NOTES_1.4.0.md](docs/release-notes/RELEASE_NOTES_1.4.0.md) for the full release notes.

## [1.3.0] - 2026-06-18

Project renamed `agent-context-manager` (`acm`) → `agent-workflow-manager` (`awm`), reflecting the tool's role as a governed-workflow control plane. Breaking change across binaries, Go module path, `.awm/` config, `AWM_*` env prefix, and `awm_*` database identifiers, with an automatic in-place legacy-database upgrade guard. The Codex hooks bootstrap template is corrected to `[features] hooks = true`.

### Added

- `migrateLegacyAcmSchema` pre-migration guard in both the Postgres (`internal/adapters/postgres/migrations.go`) and SQLite (`internal/adapters/sqlite/migrations.go`) adapters — upgrades a pre-rename `acm_*` database to the current `awm_*` naming in place on first run, before the migration ledger is consulted, so no migration is re-run and existing data is preserved. No-op on fresh databases and on databases already using `awm_*`.
- `internal/adapters/sqlite/migrations_legacy_test.go` — covers the in-place legacy upgrade (data preserved, ledger rewritten) and the fresh-database no-op path.

### Changed

- **BREAKING — project renamed `agent-context-manager` (`acm`) → `agent-workflow-manager` (`awm`).** The name now reflects the tool's actual role as a governed-workflow control plane; the `agent-context-manager` name is retired here and reused by a separate context-manager project. The rename spans:
  - CLI binaries `acm` / `acm-mcp` / `acm-web` → `awm` / `awm-mcp` / `awm-web`
  - Go module path `github.com/bonztm/agent-context-manager` → `github.com/bonztm/agent-workflow-manager` (and all import paths)
  - Config directory `.acm/` → `.awm/` and config files `acm-*.yaml` → `awm-*.yaml`
  - Environment variable prefix `ACM_*` → `AWM_*`
  - Database tables, indexes, and identifiers `acm_*` → `awm_*`, including the `acm_schema_migrations` ledger
  - Bootstrap templates, Claude/Codex/OpenCode hooks, skills (`acm-broker` → `awm-broker`), slash commands, docs, and architecture diagrams

  **Upgrade notes:** install the renamed `awm` / `awm-mcp` binaries (the old `acm*` binaries are gone), rename `ACM_*` environment variables to `AWM_*`, and move any project `.acm/` directory to `.awm/`. Existing databases upgrade in place automatically via the migration guard above on first run of the new binary.

### Fixed

- Codex hooks bootstrap template (`internal/bootstrap/bootstrap_templates/codex-hooks/files/.codex/config.toml`) now sets the current `[features] hooks = true` flag instead of the deprecated `codex_hooks = true`. The `codex-hooks` init-template id is unchanged.

### Refactored

### Removed

See [docs/release-notes/RELEASE_NOTES_1.3.0.md](docs/release-notes/RELEASE_NOTES_1.3.0.md) for the full release notes.

## [1.2.1] - 2026-03-27

Release distribution and documentation cleanup. Cross-platform binary builds for Linux, macOS, and Windows across amd64 and arm64. Release workflow automates GitHub Release asset uploads. Version string now injected via ldflags at build time. Stale `awm-mcp invoke` references cleaned up across docs and CLI help.

### Added

- GitHub Actions release workflow (`.github/workflows/release.yml`) — builds cross-platform archives and uploads them to GitHub Releases on tag publish
- `internal/buildinfo.version` linker variable — release workflow injects the git tag so `awm --version` reports the release version instead of a commit hash
- `internal/buildinfo/buildinfo_test.go` — `TestVersion_UsesInjectedVersionOverCommitShort` verifying version takes precedence over commit short
- Windows build targets (`windows/amd64`, `windows/arm64`) in both build and release workflows, with `.exe` extension and `.zip` packaging

### Changed

- CI build workflow (`.github/workflows/go-build.yml`) — replaced single-platform build with matrix strategy across `linux/{amd64,arm64}`, `darwin/{amd64,arm64}`, `windows/{amd64,arm64}`; artifacts named per platform
- `internal/adapters/cli/app.go` — CLI help text updated from stale `awm-mcp tools` reference to `awm-mcp --help` describing the JSON-RPC 2.0 MCP server
- `docs/cli-reference.md` — replaced stale `awm-mcp invoke` reference with JSON-RPC 2.0 guidance
- `skills/awm-broker/SKILL.md` — replaced per-tool `awm-mcp invoke` examples with JSON-RPC 2.0 protocol summary
- `skills/awm-broker/references/templates.md` — all MCP example invocations converted from `awm-mcp invoke` to `tools/call` JSON-RPC piped commands

See [docs/release-notes/RELEASE_NOTES_1.2.1.md](docs/release-notes/RELEASE_NOTES_1.2.1.md) for the full release notes.

## [1.2.0] - 2026-03-26

Architectural retrofit and code quality pass. MCP server migrates to JSON-RPC 2.0, CLI and MCP routing move into internal adapters behind ultra-thin entrypoints, error codes are centralized, and four reference docs ship. Status constant duplication resolved, monolithic test file split into per-command files, hand-rolled helpers replaced with Go builtins, storage parity coverage strengthened, and cmd/ entrypoints gain smoke tests.

### Added

- JSON-RPC 2.0 stdio MCP server — `awm-mcp` now implements the standard MCP protocol (`initialize`, `tools/list`, `tools/call`) over line-delimited JSON on stdin/stdout
- `internal/contracts/v1/errors.go` — centralized error code constants and error source constants
- `Source` field on `ErrorPayload` and `core.APIError` — traces error origin for debugging
- `core.NewErrorWithSource()` constructor and `internal/service/backend/errors.go` helper
- `internal/adapters/mcp/jsonrpc.go`, `protocol.go`, `server.go` — JSON-RPC 2.0 types, MCP method dispatch, and stdio server loop
- `internal/adapters/mcp/jsonrpc_test.go`, `protocol_test.go`, `app_test.go` — full MCP test coverage
- `internal/contracts/v1/command_catalog_test.go` — catalog completeness test
- `internal/contracts/v1/errors_test.go` — error code uniqueness and format tests
- `internal/core/errors_test.go` — `APIError.ToPayload()` source propagation test
- `cmd/awm/main_test.go`, `cmd/awm-mcp/main_test.go`, `cmd/awm-web/main_test.go` — entrypoint smoke tests
- 2 new shared parity contract subtests (rule sync roundtrip, DoD JSON roundtrip) in `repositorycontract/repository_contract.go`
- `docs/architecture.md`, `docs/cli-reference.md`, `docs/mcp-reference.md`, `docs/integration.md`

### Changed

- **BREAKING**: `awm-mcp` is now a JSON-RPC 2.0 stdio server; `awm-mcp tools` and `awm-mcp invoke` subcommands removed
- `cmd/awm/main.go` and `cmd/awm-mcp/main.go` — reduced to 11-line thin shells delegating to adapter `Run*` functions
- CLI routing moved from `cmd/awm/` to `internal/adapters/cli/`; MCP dispatch moved to `internal/adapters/mcp/`
- `internal/adapters/mcp/invoke.go` — `ToolDef` updated to MCP format; `spec/v1/mcp.tools.v1.json` updated to match
- Ad-hoc error code string literals replaced with `v1.ErrCode*` constants across validation, dispatch, and backend
- `README.md` and `docs/getting-started.md` — updated with links to new reference docs
- `skills/awm-broker/` — MCP example payloads and READMEs converted to JSON-RPC 2.0

### Fixed

- `complete` vs `completed` status duplication — removed dead `WorkItemStatusCompleted` and `PlanStatusCompleted` constants; collapsed dual-case switch branches; normalization functions retain `"completed"` literal matching as safety net for legacy data

### Refactored

- Split monolithic `service_test.go` (7645 lines, 150 tests) into `fakes_test.go`, `helpers_test.go`, and 10 per-command test files
- Replaced hand-rolled `minInt`, `maxZero`, `maxInt` helpers with Go 1.21+ builtin `min()` and `max()`
- Promoted 2 SQLite-only parity tests to shared contract — both adapters now run them automatically
- Added doc comment to `ProjectIDFromPayload` reflect fallback explaining forward-compatibility purpose

### Removed

- `awm-mcp tools` and `awm-mcp invoke` subcommands — replaced by JSON-RPC methods
- `internal/service/backend/service_test.go` — split into 12 per-command test files
- `internal/adapters/sqlite/repository_rules_test.go` and `repository_run_summary_test.go` — promoted to shared contract

See [docs/release-notes/RELEASE_NOTES_1.2.0.md](docs/release-notes/RELEASE_NOTES_1.2.0.md) for the full release notes.

## [1.1.2] - 2026-03-24

Fix for `work` command not supporting clearing `parent_task_key` once set, plus agent directive improvements and plan validator hardening.

### Fixed

- `work` merge logic now supports clearing `parent_task_key` by explicitly sending an empty string; previously, empty values were silently ignored and the stored value persisted
- `WorkTaskPayload.ParentTaskKey` changed from `string` to `*string` to distinguish "not provided" (nil, preserve existing) from "explicitly clear" (empty string)
- Plan validator (`awm-feature-plan-validate.py`) now skips tasks with `status=superseded` during validation
- Plan validator no longer errors on gate tasks (`verify:tests`, `review:*`) that have stale `parent_task_key` values

### Added

- `AGENTS.md` — Build And Verify section with build/test/lint commands for all agents
- `AGENTS.md` — Common Mistakes section with 10 repo-specific anti-patterns
- `AGENTS.md` — Decision Authority section distinguishing autonomous vs human-required agent decisions
- `CONTRIBUTING.md` — Go Style And Patterns section with errors, logging, package boundaries, and style guidance referencing the coding-handbook
- `internal/storage/domain/domain_test.go` — tests for `parent_task_key` clear and preserve merge behavior

### Changed

- `docs/examples/CLAUDE.md` — replaced duplicated AWM workflow loop with concise slash-command mapping table
- Bootstrap template `awm-feature-plan-validate.py` — fully synced to current canonical version
- `CLAUDE.md` — kept minimal as routing-only file (build commands now in `AGENTS.md`)

See [docs/release-notes/RELEASE_NOTES_1.1.2.md](docs/release-notes/RELEASE_NOTES_1.1.2.md) for the full release notes.

## [1.1.1] - 2026-03-23

Post-release cleanup: completes memory surface removal, improves Claude/Codex hooks, adds architecture diagrams, and hardens docs and init templates.

### Added

- Architecture diagrams — Excalidraw source files and PNG exports for layer and flow diagrams (`docs/architecture/`)
- `docs/maintainer-reference.md` — architecture diagram pointers
- `codex-hooks` init template — Codex-compatible hooks with receipt guard, session-context injection, prompt guard, and stop guard (`.codex/hooks/`)
- Schema file parity tests (`schema_files_test.go`) and web embed tests (`embed_test.go`)

### Changed

- Claude hooks (`awm-receipt-guard.sh`, `awm-session-context.sh`, `awm-stop-guard.sh`) — improved error handling and robustness
- `AGENTS.md` and `CLAUDE.md` — updated for post-memory workflow; AMM integration notes added
- Init templates (`starter-contract`, `detailed-planning-enforcement`) — updated `AGENTS.md`, `CLAUDE.md`, and `awm-rules.yaml` to reflect memory removal and hook improvements
- Skill-pack docs (`SKILL.md`, Claude/Codex/OpenCode READMEs) — AMM migration notes added
- `awm health` — `unknown_tags` check now only inspects pointer tags; stale memory tag references removed from canonical tags
- `awm fetch` — cleaned up dead memory-key code paths
- `spec/v1/README.md` — updated tool count and surface descriptions
- Web dashboard — removed Memories nav link from all pages; removed memory-related CSS and JS

### Removed

- `web/memories.html` — Memories page fully removed from web dashboard
- `spec/v1/shared.schema.json` — remaining memory-era schema definitions removed
- `spec/v1/cli.result.schema.json` — remaining memory result definitions removed
- `skills/awm-broker/assets/requests/mcp_memory.json` and `memory.json` — request templates removed
- `canonical_tags.json` — `memory` tag removed from embedded tag dictionary

See [docs/release-notes/RELEASE_NOTES_1.1.1.md](docs/release-notes/RELEASE_NOTES_1.1.1.md) for the full release notes.

## [1.1.0] - 2026-03-23

Memory subsystem removed in favor of [Agent Memory Manager (AMM)](https://github.com/bonztm/agent-memory-manager).

### Added

- `docs/deprecation/memory-removal.md` — migration guidance for adopters moving from AWM memory to AMM

### Changed

- `context` receipts no longer include memories or memory-derived tags; receipt IDs differ from 1.0.0 for projects that had active memories
- `health` reports 10 check categories (down from 11); `weak_memories` removed, `unknown_tags` inspects pointer tags only
- `history` entity list is now `all|work|receipt|run` (memory entity removed)
- MCP tool catalog exposes 12 tools (down from 13)
- Claude command pack produces 7 slash commands (down from 8); `/awm-memory` removed
- All skill-pack and documentation references to `awm memory` and "durable memory" removed

### Removed

- `awm memory` command — CLI, MCP tool, HTTP API, command dispatch, and all contract types
- `fetch mem:<id>` key lookups — memory keys return not-found
- `/api/memories` HTTP routes and web dashboard Memories page
- Storage adapter methods, query builders, and domain normalization for memory persistence
- `ContextMemory`, `ExportMemoryDocument`, and `MemoryCount` from receipt and status types
- `spec/v1/` schema definitions for memory command, result, and entity enums

### Migration

- Database migration DDL preserved — existing `awm_memories` and `awm_memory_candidates` tables remain inert
- Direct upgrade from 1.0.0; no data migration required
- Adopters using `awm memory` should adopt AMM before upgrading
- See `docs/deprecation/memory-removal.md` for detailed guidance

See [docs/release-notes/RELEASE_NOTES_1.1.0.md](docs/release-notes/RELEASE_NOTES_1.1.0.md) for the full release notes.

## [1.0.0] - 2026-03-15

Initial public release of awm (agent-workflow-manager).

### Added

- Core agent workflow: `context`, `work`, `memory`, `verify`, `done`
- Supporting surfaces: `fetch`, `review`, `history`
- Human-facing setup: `init`, `sync`, `health`, `status`
- Backend-only `export` surface via `awm run` or MCP
- Init templates: `starter-contract`, `detailed-planning-enforcement`, `verify-generic`, `verify-go`, `verify-ts`, `verify-python`, `verify-rust`, `codex-pack`, `opencode-pack`, `claude-command-pack`, `claude-hooks`, `git-hooks-precommit`
- Agent integrations: Claude Code (slash commands), Codex (global skill), OpenCode (repo-local companion docs), MCP (13 tools)
- Storage backends: SQLite (zero-config default) and Postgres (multi-writer)
- Web dashboard (`awm-web`): Board, Memories, Status, Health pages with Docker support
- Configuration: rules, tags, tests, workflows in `.awm/` YAML files
- Wire contract: `spec/v1/` with full JSON schema definitions and CLI/MCP parity
- Documentation: README, getting-started guide, concepts, feature-plans, SQLite operations, logging standards, examples
- Four adoption modes: plans-only, plans+memory, governed workflow, full brokered flow

See [docs/release-notes/RELEASE_NOTES_1.0.0.md](docs/release-notes/RELEASE_NOTES_1.0.0.md) for the full release notes.

[1.3.0]: https://github.com/BonzTM/agent-workflow-manager/releases/tag/1.3.0
[1.2.1]: https://github.com/BonzTM/agent-workflow-manager/releases/tag/1.2.1
[1.2.0]: https://github.com/BonzTM/agent-workflow-manager/releases/tag/1.2.0
[1.1.2]: https://github.com/BonzTM/agent-workflow-manager/releases/tag/1.1.2
[1.1.1]: https://github.com/BonzTM/agent-workflow-manager/releases/tag/1.1.1
[1.1.0]: https://github.com/BonzTM/agent-workflow-manager/releases/tag/1.1.0
[1.0.0]: https://github.com/BonzTM/agent-workflow-manager/releases/tag/1.0.0
