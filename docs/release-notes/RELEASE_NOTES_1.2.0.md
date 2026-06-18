# [1.2.0] Release Notes - 2026-03-26

## Release Summary

Architectural retrofit and code quality pass. The MCP server migrates to JSON-RPC 2.0, CLI and MCP routing move into internal adapters behind ultra-thin entrypoints, error codes are centralized, and four new reference docs ship. On the quality side: a cross-cutting status constant duplication is resolved, the monolithic backend test file is split into per-command files, hand-rolled helpers are replaced with Go builtins, storage parity coverage is strengthened, and cmd/ entrypoints gain smoke tests.

## Breaking Changes

- **`awm-mcp` is now a JSON-RPC 2.0 stdio server.** The `awm-mcp tools` and `awm-mcp invoke` subcommands are removed. Clients must send JSON-RPC requests (`initialize`, `tools/list`, `tools/call`) over line-delimited stdin/stdout. See `docs/mcp-reference.md` for the new protocol.

## Added

### MCP Protocol

- JSON-RPC 2.0 stdio MCP server — `awm-mcp` implements `initialize`, `tools/list`, `tools/call` over line-delimited JSON on stdin/stdout
- `internal/adapters/mcp/jsonrpc.go` — JSON-RPC 2.0 request/response types, parse/serialize helpers, standard error codes
- `internal/adapters/mcp/protocol.go` — MCP method dispatch
- `internal/adapters/mcp/server.go` — line-delimited stdio server loop
- `internal/adapters/mcp/jsonrpc_test.go`, `protocol_test.go`, `app_test.go` — full test coverage

### Error Infrastructure

- `internal/contracts/v1/errors.go` — centralized error code constants (`ErrCodeInvalidJSON`, `ErrCodeUnknownTool`, etc.) and error source constants (`ErrSourceValidation`, `ErrSourceDispatch`, `ErrSourceBackend`, `ErrSourceAdapter`)
- `Source` field on `ErrorPayload` and `core.APIError` — traces error origin for debugging
- `core.NewErrorWithSource()` constructor — creates errors with explicit source tagging
- `internal/service/backend/errors.go` — backend error helper stamping `ErrSourceBackend`

### Tests

- `internal/contracts/v1/command_catalog_test.go` — catalog completeness test asserting all 12 specs have non-empty metadata, no duplicates
- `internal/contracts/v1/errors_test.go` — error code uniqueness and format tests, `ErrorPayload.Source` JSON round-trip
- `internal/core/errors_test.go` — `APIError.ToPayload()` source propagation test
- `cmd/awm/main_test.go` — CLI entrypoint smoke tests (`--help`, `--version`, unknown subcommand exit codes)
- `cmd/awm-mcp/main_test.go` — MCP entrypoint smoke tests (`--help`, `--version`)
- `cmd/awm-web/main_test.go` — web entrypoint smoke test (`staticFS` returns valid filesystem)
- 2 new shared parity contract subtests (rule sync roundtrip, DoD JSON roundtrip) — both SQLite and Postgres adapters run them automatically

### Documentation

- `docs/architecture.md` — layer diagram, request lifecycle, error propagation, storage parity, command catalog reference
- `docs/cli-reference.md` — full CLI command reference extracted from README
- `docs/mcp-reference.md` — MCP protocol reference rewritten for JSON-RPC 2.0
- `docs/integration.md` — generic integration guide with handshake flow, tool calling, runtime configuration snippets

## Changed

### Architecture

- `cmd/awm/main.go` — reduced to 11-line thin shell delegating to `cli.RunCLI()`
- `cmd/awm-mcp/main.go` — reduced to 11-line thin shell delegating to `mcp.RunMCP()`
- CLI routing (`convenience.go`, `routes.go`) moved from `cmd/awm/` to `internal/adapters/cli/`
- MCP dispatch logic moved from `cmd/awm-mcp/` to `internal/adapters/mcp/`
- `internal/adapters/mcp/invoke.go` — `ToolDef` updated to MCP format (`inputSchema` camelCase, `title` and `output_schema` removed)
- `spec/v1/mcp.tools.v1.json` — tool contract schema updated to match JSON-RPC tool format
- Ad-hoc error code string literals replaced with `v1.ErrCode*` constants across validation, dispatch, and backend
- All backend error creation routed through `backendError()` helper to stamp `ErrSourceBackend`

### Documentation

- `README.md` — CLI Reference and MCP sections replaced with summaries linking to new dedicated docs
- `docs/getting-started.md` — cross-references to new CLI, MCP, and integration docs
- `skills/awm-broker/assets/requests/mcp_*.json` — all 8 MCP example payloads converted to JSON-RPC 2.0 `tools/call` requests
- `skills/awm-broker/{claude,codex,opencode}/README.md` — `awm-mcp invoke` references replaced with JSON-RPC protocol guidance

## Fixed

### Status Constant Normalization

- Removed dead `WorkItemStatusCompleted` and `PlanStatusCompleted` Go constant aliases from `core/repository.go` — migration 13 already normalizes `completed` → `complete` in both storage backends with CHECK constraints
- Collapsed all dual-case switch branches in `work.go`, `history.go`, and `domain.go` that previously handled both `Complete` and `Completed` constants
- Normalization functions (`NormalizeWorkItemStatus`, `normalizeWorkItemStatus`, `normalizePlanStatus`) retain literal `"completed"` string matching as a read-time safety net for any legacy persisted data

## Refactored

### Test Infrastructure

- Split monolithic `service_test.go` (7645 lines, 150 tests) into 12 focused files:
  - `fakes_test.go` — shared `fakeRepository` and work plan helpers
  - `helpers_test.go` — shared utility functions
  - 10 per-command files: `context_cmd_test.go`, `completion_cmd_test.go`, `sync_cmd_test.go`, `health_status_cmd_test.go`, `verify_cmd_test.go`, `init_cmd_test.go`, `fetch_cmd_test.go`, `work_cmd_test.go`, `history_cmd_test.go`, `review_cmd_test.go`
- Promoted 2 SQLite-specific parity tests to shared contract in `repositorycontract/repository_contract.go`

### Code Modernization

- Replaced hand-rolled `minInt`, `maxZero`, and `maxInt` helpers with Go 1.21+ builtin `min()` and `max()` — deleted from `shared_helpers.go` and `export.go`
- Added doc comment to `ProjectIDFromPayload` reflect fallback explaining its forward-compatibility purpose

## Removed

- `awm-mcp tools` subcommand — replaced by `tools/list` JSON-RPC method
- `awm-mcp invoke` subcommand — replaced by `tools/call` JSON-RPC method
- `internal/service/backend/service_test.go` — split into 12 focused per-command test files
- `internal/adapters/sqlite/repository_rules_test.go` — test promoted to shared parity contract
- `internal/adapters/sqlite/repository_run_summary_test.go` — test promoted to shared parity contract

## Admin/Operations

- Binary rebuild required — the MCP protocol changed from custom invoke to JSON-RPC 2.0.
- Database schema unchanged from 1.1.2.
- No data migration required.

## Deployment and Distribution

```bash
go install github.com/bonztm/agent-workflow-manager/cmd/awm@v1.2.0
go install github.com/bonztm/agent-workflow-manager/cmd/awm-mcp@v1.2.0
go install github.com/bonztm/agent-workflow-manager/cmd/awm-web@v1.2.0
```

- Source: `https://github.com/BonzTM/agent-workflow-manager`

## Compatibility and Migration

- Requires Go 1.26+ for `go install` or building from source.
- Direct upgrade from 1.1.x or 1.0.0. No data migration required.
- **MCP clients must migrate** from `awm-mcp invoke <tool>` to JSON-RPC 2.0 `tools/call` requests. See `docs/mcp-reference.md`.
- CLI usage is unchanged — all 12 commands work identically.
- Wire format (JSON payloads) is unchanged for all commands.

## Full Changelog

- Compare changes: https://github.com/BonzTM/agent-workflow-manager/compare/1.1.2...1.2.0
- Full changelog: https://github.com/BonzTM/agent-workflow-manager/commits/1.2.0
