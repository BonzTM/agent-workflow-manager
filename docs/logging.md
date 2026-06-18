# Logging Standards

## Goal

`agent-workflow-manager` uses centralized structured logging for runtime wiring, service execution, and adapter ingress/dispatch flow. Logging is contract-driven and enforced by deterministic tests.

## Logger Contract

- Shared package: `internal/logging`
- Interface: `logging.Logger` (`Info`, `Error`)
- Default runtime logger: JSON `slog` to `stderr`
- Test logger: `logging.Recorder` for deterministic assertions

## Event Names

### Service Decorator Events

- `service.operation.start`
- `service.operation.finish`

Required fields:

- `operation` (one of `context`, `fetch`, `done`, `review`, `work`, `history`, `sync`, `health`, `status`, `verify`, `init`)
- `project_id` when present in payload
- `duration_ms` on finish
- `ok` on finish
- `error_code` on finish when `ok=false`

### CLI Adapter Events

- `cli.ingress.read`
- `cli.ingress.validate`
- `cli.dispatch` (`phase=start|finish`)
- `cli.result`
- `cli.failure` (`stage=read|validate|dispatch`)

Required fields by stage:

- ingress read: `ok`
- ingress validate: `ok`, `command`, `request_id`, optional `project_id`, `error_code` when failed
- dispatch: `phase`, `command`, `request_id`, optional `project_id`, `ok` on finish, `error_code` on failed finish
- result: `ok`, `command` when available, `request_id` when available, optional `project_id`, `error_code` when failed
- failure: `stage`, plus stage-specific command/tool/request identifiers and `error_code`

### MCP Adapter Events

- `mcp.ingress.read`
- `mcp.ingress.validate`
- `mcp.dispatch` (`phase=start|finish`)
- `mcp.result`
- `mcp.failure` (`stage=validate|dispatch`)

Required fields by stage:

- ingress read: `ok`, `tool`
- ingress validate: `ok`, `tool`, optional `project_id`, `error_code` when failed
- dispatch: `phase`, `tool`, optional `project_id`, `ok` on finish, `error_code` on failed finish
- result: `ok`, `tool`, optional `project_id`, `error_code` when failed
- failure: `stage`, `tool`, optional `project_id`, `error_code`

## Runtime Wiring Rule

`internal/runtime.NewService*` must always return a service wrapped by `core.WithLogging(...)`, including the unconfigured backend path.

## Runtime Configuration Boundary

- Runtime logger configuration is bounded and env-driven:
  - `AWM_LOG_LEVEL`: `debug|info|warn|error` (default `info`)
  - `AWM_LOG_SINK`: `stderr|stdout|discard` (default `stderr`)
- Invalid or unset values must fall back to defaults (`info`, `stderr`).
- Configuration only controls emission threshold and sink destination.
- Contract constraints (must not drift):
  - existing event names,
  - required fields listed in this document,
  - `logging.Logger` interface shape (`Info`, `Error`),
  - deterministic recorder-based test assertions.

## Test Gate

Logging coverage is required and enforced through deterministic recorder-based tests:

- Service decorator tests call every `core.Service` method and require both start + finish logs with required fields.
- CLI and MCP adapter tests require ingress/validate/dispatch/result/failure event coverage for success and failure paths.
- Runtime tests require that services returned by `internal/runtime` emit service decorator logs.
