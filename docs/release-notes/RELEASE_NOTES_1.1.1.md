# [1.1.1] Release Notes - 2026-03-23

## Release Summary

Post-release cleanup following the 1.1.0 memory removal. This patch completes the memory surface removal across web, spec schemas, and request templates; hardens Claude and Codex hooks; adds Excalidraw architecture diagrams; introduces the `codex-hooks` init template; and updates all docs and init templates to reflect the current post-memory product surface.

## Fixed

- `awm fetch` — dead code paths for `mem:<id>` key lookups cleaned up (no behavior change; keys already returned not-found in 1.1.0)
- `awm health` — stale `memory` tag removed from `canonical_tags.json`; `unknown_tags` check no longer flags memory-era pointer tags

## Added

### Architecture Diagrams

- `docs/architecture/awm-architecture-layers.excalidraw` — Layer diagram source with light and dark PNG exports
- `docs/architecture/awm-flow-diagram.excalidraw` — Flow diagram source with light and dark PNG exports
- `docs/maintainer-reference.md` — Architecture diagram pointers added

### Codex Hooks Init Template

- `codex-hooks` — New init template that seeds `.codex/hooks.json`, `.codex/config.toml`, and four hook scripts:
  - `awm-common.sh` — shared utility functions
  - `awm-prompt-guard.sh` — receipt guard for Codex prompt submissions
  - `awm-session-context.sh` — session-start context injection
  - `awm-stop-guard.sh` — task closure guard

### Tests

- `internal/contracts/v1/schema_files_test.go` — parity tests verifying schema files match registered command catalog
- `web/embed_test.go` — tests verifying embedded web assets resolve correctly
- `internal/service/backend/health_fix_ruleset_test.go` — additional health fixer coverage

## Changed

### Claude Hooks

- `awm-receipt-guard.sh` — improved error handling, more robust receipt detection
- `awm-session-context.sh` — updated context injection logic
- `awm-stop-guard.sh` — improved guard logic and error reporting

### Documentation

- `AGENTS.md` — updated for post-memory workflow; clarified AMM integration
- `CLAUDE.md` — AMM MCP tool references added; memory command references removed
- `README.md` — minor copy updates
- `docs/concepts.md` — memory references removed
- `docs/getting-started.md` — updated for three adoption modes (plans-only, governed, brokered)
- `docs/logging.md` — minor updates
- `docs/examples/AGENTS.md` and `docs/examples/CLAUDE.md` — aligned with current surface

### Init Templates

- `starter-contract` — `AGENTS.md`, `CLAUDE.md`, and `awm-rules.yaml` updated for memory removal
- `detailed-planning-enforcement` — `AGENTS.md`, `CLAUDE.md`, and `awm-rules.yaml` updated for memory removal

### Skill-Pack Docs

- `skills/awm-broker/SKILL.md` — AMM migration notes
- `skills/awm-broker/claude/README.md` — AMM integration notes
- `skills/awm-broker/codex/README.md` — AMM integration notes
- `skills/awm-broker/opencode/README.md` — AMM integration notes

### Web Dashboard

- Removed Memories navigation link from `index.html` and `status.html`
- Removed memory-related JavaScript handlers from `app.js`
- Removed memory-related CSS from `style.css`

### Spec

- `spec/v1/README.md` — updated tool count and surface descriptions to reflect 12-tool catalog
- `skills/awm-broker/assets/requests/mcp_history.json` — entity list updated (memory removed)

### Storage Adapters

- `internal/adapters/postgres/queries.go` — dead memory query builder stub marked
- `internal/adapters/sqlite/repository.go` — dead memory adapter stub marked
- `internal/storage/testutil/repositorycontract/repository_contract.go` — memory contract tests removed, adapter parity assertions updated

## Removed

- `web/memories.html` — Memories page fully removed from web dashboard
- `web/static/style.css` — memory-related CSS rules removed
- `web/static/app.js` — memory page JavaScript removed
- `spec/v1/shared.schema.json` — remaining memory-era definitions (130 lines) removed
- `spec/v1/cli.result.schema.json` — remaining memory result definitions (66 lines) removed
- `skills/awm-broker/assets/requests/mcp_memory.json` — MCP memory request template removed
- `skills/awm-broker/assets/requests/memory.json` — CLI memory request template removed
- `internal/service/backend/canonical_tags.json` — `memory` tag removed from embedded tag dictionary
- `.awm/awm-tags.yaml` — stale memory tag entry removed

## Admin/Operations

- No binary changes — the `awm`, `awm-mcp`, and `awm-web` binaries are unchanged from 1.1.0. This release is docs, hooks, tests, and web asset cleanup only.
- Database schema unchanged from 1.1.0.

## Deployment and Distribution

- Go install: `go install github.com/bonztm/agent-workflow-manager/cmd/awm@v1.1.1`
- Go install (MCP): `go install github.com/bonztm/agent-workflow-manager/cmd/awm-mcp@v1.1.1`
- Go install (Web): `go install github.com/bonztm/agent-workflow-manager/cmd/awm-web@v1.1.1`
- Source: `https://github.com/BonzTM/agent-workflow-manager`
- Prebuilt binaries: download `awm-binaries` artifact from GitHub Actions `Go Build` workflow.

```bash
go install github.com/bonztm/agent-workflow-manager/cmd/awm@v1.1.1
go install github.com/bonztm/agent-workflow-manager/cmd/awm-mcp@v1.1.1
go install github.com/bonztm/agent-workflow-manager/cmd/awm-web@v1.1.1
```

## Breaking Changes

- None. This is a patch release with no API, CLI, or contract changes.

## Known Issues

- N/A

## Compatibility and Migration

- Requires Go 1.26+ for `go install` or building from source.
- Direct upgrade from 1.1.0 or 1.0.0. No data migration required.
- Adopters upgrading from 1.0.0 should review `docs/deprecation/memory-removal.md` and the [1.1.0 release notes](RELEASE_NOTES_1.1.0.md) first.

## Full Changelog

- Compare changes: https://github.com/BonzTM/agent-workflow-manager/compare/1.1.0...1.1.1
- Full changelog: https://github.com/BonzTM/agent-workflow-manager/commits/1.1.1
