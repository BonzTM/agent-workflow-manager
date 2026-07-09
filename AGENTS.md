# AGENTS.md - agent-workflow-manager Contributor Contract

This is the authoritative contract for work in this repository.
Read this file first, then use the linked docs when you need deeper reference material.

## Quick Start

1. Read this file for repo rules and change routing.
2. See [CONTRIBUTING.md](CONTRIBUTING.md) for build, test, and PR workflow.
3. If your agent runtime provides AWM, see [.awm/AGENTS-AWM.md](.awm/AGENTS-AWM.md) for the enhanced workflow.  If you are unaware or unsure of what AWM is, do not read the file.
4. If using Claude, also read [CLAUDE.md](CLAUDE.md).

## Repo-Wide Invariants

- Schema lockstep: `internal/contracts/v1` and `spec/v1` move with the corresponding tests.
- CLI/MCP parity: `cmd/awm`, `cmd/awm-mcp`, the command catalog, generated tool metadata, and related tests stay aligned.
- Storage parity: SQLite and Postgres adapters, migrations, and parity-sensitive coverage stay aligned unless a difference is explicitly intentional and documented.
- Onboarding invariants: `init` must produce a usable first-run state from a clean repo.
- Docs sync: user-facing install, help, onboarding, or tool-surface changes update `README.md`, `docs/getting-started.md`, `docs/examples/*`, and `skills/awm-broker/**` together.

## Maintainer Routing

| If you are changing... | Start here | Also keep in sync | Verify or confirm with |
|---|---|---|---|
| Command payloads, results, or semantics | `internal/contracts/v1/`, `internal/service/backend/` | `spec/v1/`, `internal/commands/dispatch.go`, `cmd/awm/`, `cmd/awm-mcp/`, tests | [docs/maintainer-map.md](docs/maintainer-map.md) |
| CLI flags, help text, or routing | `cmd/awm/convenience.go`, `cmd/awm/routes.go` | `internal/contracts/v1/command_catalog.go`, `cmd/awm/*_test.go`, docs | [docs/maintainer-map.md](docs/maintainer-map.md) |
| Storage, migrations, or plan persistence | `internal/core/repository.go`, `internal/adapters/sqlite/`, `internal/adapters/postgres/` | repository contract tests, parity tests, integration tests | [docs/maintainer-map.md](docs/maintainer-map.md) |
| Review, verify, done, or workflow gates | `.awm/awm-workflows.yaml`, `internal/service/backend/{review,verify,completion,work}.go` | repo-local scripts, docs, examples, skill-pack assets | [docs/maintainer-map.md](docs/maintainer-map.md) |
| Init, sync, health, onboarding, or templates | `internal/bootstrap/`, `README.md`, `docs/getting-started.md` | `docs/examples/*`, template files, `skills/awm-broker/**` | [docs/maintainer-map.md](docs/maintainer-map.md) |
| Feature-planning contract | `docs/feature-plans.md`, `.awm/awm-tests.yaml`, `scripts/awm-feature-plan-validate.py` | maintainer docs, examples, workflow guidance | [docs/maintainer-map.md](docs/maintainer-map.md) |

## Build And Verify

`make verify` is THE gate — the same ordered sequence humans and CI run
(tidy, fmt-check, lint, vet, test, race, govulncheck, build). Run it before
every push.

```bash
# The full gate (required before push; CI runs the identical target)
make verify

# Individual steps during the inner loop
make build     # compile all packages + dist/ binaries
make test      # go test ./...
make race      # go test -race ./...
make lint      # golangci-lint (pinned via go.mod tool directive)
make fmt       # apply gofumpt + gci

# Run a specific package
go test ./internal/service/backend/...
```

Postgres integration tests require `AWM_PG_DSN`:

```bash
AWM_PG_DSN="postgres://..." go test ./internal/integration/...
```

## Working Norms

- Prefer small, reviewable changes over broad cleanup.
- Include tests for new behavior under `cmd/**` or `internal/**`.
- Do not invent compatibility promises or product requirements the repo does not define.
- If verification fails, fix it or report it clearly. Do not claim success.
- If a planned task or review gate becomes obsolete, mark it `superseded` instead of leaving it open or `blocked`.
- Planned behavior-changing Go work under `cmd/**` or `internal/**` should add a `tdd:red` task before implementation, or a `tdd:exemption` task with a concrete justification.
- Repo-local verify treats non-test Go changes under `cmd/**` or `internal/**` as behavior changes unless that exemption is present.

## Common Mistakes

Do not:

- **Add a command without updating the catalog.** Every command needs entries in `internal/contracts/v1/command_catalog.go`, `internal/contracts/v1/types.go`, `internal/contracts/v1/validate.go`, `internal/commands/dispatch.go`, `cmd/awm/routes.go`, and `cmd/awm/convenience.go`. Missing any one causes parity or routing failures.
- **Add a SQLite migration without a matching Postgres migration** (or vice versa). Both adapters under `internal/adapters/` must move together with equivalent semantics.
- **Put business logic in an adapter.** Adapters (`internal/adapters/`) translate between external protocols and `internal/core/` interfaces. Decision logic belongs in `internal/service/backend/`.
- **Change a payload type without updating `spec/v1/` schemas.** The `schema_files_test` will catch drift, but catching it at PR time wastes a review cycle.
- **Modify MCP tool wiring manually.** MCP tool definitions auto-generate from the command catalog. Update the catalog, not the MCP layer.
- **Edit `.awm/*.yaml` files without refreshing state.** If you change rules, tags, tests, or workflow config, the runtime must be synced before those changes take effect.
- **Create catch-all tasks** like `misc`, `polish`, `remaining`, or `cleanup`. Each task should describe one bounded deliverable.
- **Use `go run ./cmd/awm` in production or CI.** Build and install the binary. `go run` is only for testing source-build behavior.
- **Add dependencies without justification.** Stdlib first. Document why an existing dependency or the stdlib is insufficient before adding a new one.
- **Log errors at every layer.** Log once at the boundary that can act on the error. Return wrapped errors through intermediate layers.

## Decision Authority

Agents can decide autonomously:

- Which files to read and in what order
- Test strategy (unit vs integration) for new behavior, as long as tests exist
- Internal refactoring within a single package that does not change exported APIs
- Commit and PR structure for the current task

Agents must surface to a human before proceeding:

- **New dependencies** in `go.mod` — requires rationale and human approval
- **New commands or flags** — product decisions about the public surface
- **Architecture changes** — new packages, layer boundaries, interface contracts
- **Compatibility or migration changes** — anything that affects existing adopters
- **Scope expansion** beyond the original task — flag it, do not silently widen
- **Security-sensitive changes** — auth, secrets, permissions, or trust boundaries
- **Deleting or renaming exported symbols** — breaking change for importers

When in doubt, surface the decision. The cost of asking is low; the cost of an unauthorized architecture change is high.

## Reference Docs

- Architecture, package map, command checklist, test patterns, troubleshooting: [docs/maintainer-reference.md](docs/maintainer-reference.md)
- Detailed change routing: [docs/maintainer-map.md](docs/maintainer-map.md)
- Staged planning contract: [docs/feature-plans.md](docs/feature-plans.md)
- Product and adopter setup: [docs/getting-started.md](docs/getting-started.md)
- Schema and MCP contract overview: [spec/v1/README.md](spec/v1/README.md)
