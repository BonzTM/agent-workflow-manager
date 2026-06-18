# [1.1.2] Release Notes - 2026-03-24

## Release Summary

Fixes a bug where the `work` command could not clear `parent_task_key` once set on a task. Also enriches agent directives (`AGENTS.md`, `CONTRIBUTING.md`) with build commands, anti-patterns, Go style guidance, and decision authority for all agents. Hardens the plan validator to skip superseded tasks and tolerate stale gate-task parent relationships.

## Fixed

### Work Merge: parent_task_key Clear Support

- `work` task merge now distinguishes "field not provided" (preserve existing) from "field explicitly set to empty" (clear value)
- `WorkTaskPayload.ParentTaskKey` changed from `string` to `*string` in `internal/contracts/v1/types.go`
- `core.WorkItem` gains `ParentTaskKeyClear bool` signal field in `internal/core/repository.go`
- `mergeWorkPlanTask` in `internal/storage/domain/domain.go` respects the clear signal
- Conversion in `internal/service/backend/work.go` wires the pointer-to-clear mapping

### Plan Validator Hardening

- `scripts/awm-feature-plan-validate.py` — tasks with `status=superseded` are now excluded from `index_tasks`, preventing stale renamed tasks from causing validation errors
- Gate tasks (`verify:tests`, `review:*`) with stale `parent_task_key` values are tolerated instead of errored, since AWM does not currently support clearing `parent_task_key` on existing tasks

## Added

### Agent Directives

- `AGENTS.md` — **Build And Verify** section with `go build`, `go test`, `gofmt`, `go vet`, and Postgres integration test commands
- `AGENTS.md` — **Common Mistakes** section with 10 concrete anti-patterns: missing catalog updates, unpaired migrations, business logic in adapters, payload/schema drift, manual MCP wiring, stale `.awm/*.yaml`, catch-all tasks, `go run` in CI, unjustified dependencies, and multi-layer error logging
- `AGENTS.md` — **Decision Authority** section listing what agents can decide autonomously (file reading order, test strategy, internal refactoring, commit structure) vs. what requires human sign-off (new dependencies, new commands, architecture changes, compatibility changes, scope expansion, security, exported symbol changes)
- `CONTRIBUTING.md` — **Go Style And Patterns** section covering errors (`fmt.Errorf %w`, `errors.Is`/`errors.As`), logging (`log/slog`, injected loggers), package boundaries (`core`/`backend`/`adapters` layering), and style (`gofmt -s`, early returns, naming conventions), referencing the coding-handbook for rationale

### Tests

- `internal/storage/domain/domain_test.go` — `TestMergeIncomingWorkPlanTasksClearsParentTaskKeyWhenExplicit` and `TestMergeIncomingWorkPlanTasksPreservesParentTaskKeyWhenNotExplicit`

## Changed

- `docs/examples/CLAUDE.md` — replaced 25-line restated AWM workflow loop with a concise slash-command mapping table that links to `AGENTS.md`
- `CLAUDE.md` — kept as minimal routing file; build commands now live in `AGENTS.md` where all agents benefit
- Bootstrap template `awm-feature-plan-validate.py` — fully synced to current canonical version with both fixes (superseded filter + gate-task tolerance)
- `internal/contracts/v1/validate.go` — updated `parent_task_key` validation for `*string` type
- `internal/contracts/v1/validate_test.go` — updated assertion for `*string` parent_task_key

## Admin/Operations

- Binary rebuild required — the `work` command behavior changed. New binaries should be built and deployed.
- Database schema unchanged from 1.1.1.
- External repo copies of `awm-feature-plan-validate.py` (agent-memory-manager, soundspan) received the superseded-task filter fix.

## Deployment and Distribution

- Go install: `go install github.com/bonztm/agent-workflow-manager/cmd/awm@v1.1.2`
- Go install (MCP): `go install github.com/bonztm/agent-workflow-manager/cmd/awm-mcp@v1.1.2`
- Go install (Web): `go install github.com/bonztm/agent-workflow-manager/cmd/awm-web@v1.1.2`
- Source: `https://github.com/BonzTM/agent-workflow-manager`

```bash
go install github.com/bonztm/agent-workflow-manager/cmd/awm@v1.1.2
go install github.com/bonztm/agent-workflow-manager/cmd/awm-mcp@v1.1.2
go install github.com/bonztm/agent-workflow-manager/cmd/awm-web@v1.1.2
```

## Breaking Changes

- `WorkTaskPayload.ParentTaskKey` type changed from `string` to `*string`. Callers constructing `WorkTaskPayload` structs in Go code must use a `*string` pointer. JSON wire format is unchanged — `"parent_task_key": ""` now clears instead of being ignored.

## Known Issues

- AWM `work` command does not support clearing other string fields (`blocked_reason`, `outcome`) via empty string in the same way. These fields have their own semantic clear rules (e.g., `blocked_reason` clears when status changes to non-blocked). A future release may generalize the explicit-clear pattern.

## Compatibility and Migration

- Requires Go 1.26+ for `go install` or building from source.
- Direct upgrade from 1.1.1, 1.1.0, or 1.0.0. No data migration required.
- The wire format (JSON) is backwards-compatible: existing clients that never send `"parent_task_key": ""` are unaffected.
- Adopters with custom Go code that constructs `WorkTaskPayload` directly must update to use `*string` for `ParentTaskKey`.

## Full Changelog

- Compare changes: https://github.com/BonzTM/agent-workflow-manager/compare/1.1.1...1.1.2
- Full changelog: https://github.com/BonzTM/agent-workflow-manager/commits/1.1.2
