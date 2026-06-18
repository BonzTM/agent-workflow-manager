# [1.3.0] Release Notes - 2026-06-18

## Release Summary

**Project rename: `agent-context-manager` (`acm`) → `agent-workflow-manager` (`awm`).** The name now reflects what the tool actually is — a governed-workflow control plane (rules, receipts, work plans, verify/review/done closure) rather than a context store. The `agent-context-manager` name is retired here and freed for a separate, dedicated context-manager project.

This is a **breaking** release: the CLI binaries, Go module path, config directory, environment-variable prefix, and database identifiers all change. To make upgrades painless, a pre-migration guard automatically renames a legacy `acm_*` database to the new `awm_*` schema in place on first run, with zero data loss and no-op behavior on fresh databases. The Codex hooks bootstrap template is also corrected to the current `[features] hooks = true` flag.

## Added

### Legacy database upgrade guard

- `internal/adapters/postgres/migrations.go` and `internal/adapters/sqlite/migrations.go` — new `migrateLegacyAcmSchema` step that runs **before** the schema-migrations ledger is consulted. On a pre-rename database it renames every `acm_*` table/index and the `acm_schema_migrations` ledger to `awm_*`, then rewrites recorded migration names so no migration is re-run and existing data is preserved. It is a no-op on fresh databases and on databases already using `awm_*`.
- `internal/adapters/sqlite/migrations_legacy_test.go` — covers the in-place legacy upgrade (data preserved, ledger rewritten) and the fresh-database no-op path.

## Changed

### Rename (BREAKING)

- **CLI binaries:** `acm` / `acm-mcp` / `acm-web` → `awm` / `awm-mcp` / `awm-web`.
- **Go module path:** `github.com/bonztm/agent-context-manager` → `github.com/bonztm/agent-workflow-manager` (and all import paths).
- **Config surface:** directory `.acm/` → `.awm/`; config files `acm-*.yaml` → `awm-*.yaml`.
- **Environment variables:** `ACM_*` prefix → `AWM_*` (all variables).
- **Database:** tables, indexes, and identifiers `acm_*` → `awm_*`, including the `acm_schema_migrations` ledger.
- **Distributed assets:** bootstrap templates, Claude/Codex/OpenCode hooks, the `acm-broker` skill → `awm-broker`, slash commands, docs, and architecture diagrams all renamed.
- **Branding:** README, CLI help/tagline ("agent workflow manager CLI"), and release notes updated to the new name.

## Fixed

### Codex hooks feature flag

- `internal/bootstrap/bootstrap_templates/codex-hooks/files/.codex/config.toml` — the Codex `[features]` flag is now `hooks = true` (was the deprecated `codex_hooks = true`). Newly bootstrapped projects produce the correct flag; `templates_test.go` updated to assert it. The `codex-hooks` init-template **id** (`--apply-template codex-hooks`) is unchanged.

## Admin/Operations

- **Binary reinstall required.** The `acm*` binaries are gone; install `awm` / `awm-mcp` (and `awm-web` if used). Any PATH entries, hooks, or scripts that invoked `acm` must call `awm`.
- **Environment variables.** Rename `ACM_*` → `AWM_*` in every `.env` and shell profile (≈40 variables, e.g. `AWM_PG_DSN`, `AWM_PROJECT_ID`, `AWM_SQLITE_PATH`, `AWM_LOG_LEVEL`).
- **Project config directory.** Move any repo's `.acm/` directory to `.awm/` (config file names `acm-*.yaml` → `awm-*.yaml`).
- **Database.** No manual migration needed. On first run the new binary auto-upgrades a legacy `acm_*` database in place via the guard above; a freshly created database starts as `awm_*` directly.

## Deployment and Distribution

- Go install (CLI): `go install github.com/bonztm/agent-workflow-manager/cmd/awm@1.3.0`
- Go install (MCP): `go install github.com/bonztm/agent-workflow-manager/cmd/awm-mcp@1.3.0`
- Go install (Web): `go install github.com/bonztm/agent-workflow-manager/cmd/awm-web@1.3.0`
- Source: `https://github.com/BonzTM/agent-workflow-manager`

```bash
go install github.com/bonztm/agent-workflow-manager/cmd/awm@1.3.0
go install github.com/bonztm/agent-workflow-manager/cmd/awm-mcp@1.3.0
go install github.com/bonztm/agent-workflow-manager/cmd/awm-web@1.3.0
```

Prebuilt binaries are available from the GitHub Release page for:
- `linux/amd64`, `linux/arm64` (`.tar.gz`)
- `darwin/amd64`, `darwin/arm64` (`.tar.gz`)
- `windows/amd64`, `windows/arm64` (`.zip`)

## Breaking Changes

- **Binary names** changed (`acm*` → `awm*`); the old binaries no longer exist.
- **Go module path** changed; importers must update to `github.com/bonztm/agent-workflow-manager`.
- **Environment-variable prefix** changed (`ACM_*` → `AWM_*`); old variables are not read.
- **Config directory** changed (`.acm/` → `.awm/`) and config file names (`acm-*.yaml` → `awm-*.yaml`).
- **Database identifiers** changed (`acm_*` → `awm_*`). Existing databases are upgraded automatically in place; no `acm_*`-named objects remain afterward.
- **GitHub repository** renamed to `BonzTM/agent-workflow-manager` (the old URL redirects).

## Known Issues

- None identified.

## Compatibility and Migration

- Requires Go 1.26+ for `go install` or building from source.
- Upgrade from any 1.2.x or earlier:
  1. Install the `awm` / `awm-mcp` binaries and remove the old `acm*` binaries.
  2. Rename `ACM_*` environment variables to `AWM_*`.
  3. Rename each repo's `.acm/` directory to `.awm/`.
  4. Run any `awm` command — a legacy `acm_*` database upgrades in place on first run.
- CLI command/flag surface, MCP tool surface, and wire format (JSON payloads) are unchanged apart from the names above.

## Full Changelog

- Compare changes: https://github.com/BonzTM/agent-workflow-manager/compare/1.2.1...1.3.0
- Full changelog: https://github.com/BonzTM/agent-workflow-manager/commits/1.3.0
