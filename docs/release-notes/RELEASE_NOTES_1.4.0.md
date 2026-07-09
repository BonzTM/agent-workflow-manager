# [1.4.0] Release Notes - 2026-07-09

## Release Summary

A repository-hardening release that brings awm to the same engineering standard as its sibling project, `agent-context-manager` — the two now share one verification gate, one CI shape, one release pipeline, and one set of governance conventions. **CI runs the full test suite for the first time**: the previous workflow only compiled the binaries, so tests, lint, the race detector, and vulnerability scanning never gated a change. Rolling out that gate immediately paid for itself, catching one SQL-injection vulnerability on a called path in the Postgres adapter and two standard-library vulnerabilities fixed by the current Go patch release.

There are **no changes to the command surface, contracts, storage schema, or MCP tools** — a drop-in upgrade from 1.3.0.

## Added

### The `make verify` gate

- One canonical verification entrypoint, identical locally and in CI: `make verify` runs tidy-check, format check (gofumpt + gci), golangci-lint, `go vet`, the test suite, the race detector, `govulncheck`, and the build, in that order.
- tidy-check is read-only (`go mod tidy -diff`): CI **fails** on committed `go.mod`/`go.sum` drift instead of silently auto-correcting it in the workspace. `make tidy` remains the local fixer.
- golangci-lint v2.12.2 and govulncheck are pinned as `go.mod` tool directives, so every developer and CI run uses identical tool versions — no global installs, no drift.

### CI that actually gates

- The new `ci.yml` runs `make verify` on every push and pull request, plus a six-target cross-compile check (linux, darwin, and windows on amd64/arm64) so a platform break surfaces at PR time rather than release time. The build-only `go-build.yml` is retired.

### Repository governance

- `.github/CODEOWNERS`, a pull-request template with the verification checklist, weekly grouped Dependabot updates for Go modules and GitHub Actions, and `.editorconfig` — the same meta set as agent-context-manager.

## Changed

### Release pipeline

- Release archives now ship a `.sha256` checksum alongside every `awm-<version>-<os>-<arch>` archive.
- The release workflow mirrors whichever tag form is missing onto the release commit: bare `X.Y.Z` remains the human-facing house convention, and the `vX.Y.Z` alias is what the Go toolchain requires — `go install github.com/bonztm/agent-workflow-manager/cmd/awm@latest` now resolves the actual release instead of a `main`-branch pseudo-version.
- Retroactive `v` aliases were pushed for every existing release tag (`v1.0.0` through `v1.3.0`). Only `v1.3.0` and later resolve as module versions — older release commits predate the 1.3.0 module-path rename — but the tag pages are now consistent across the project's history.
- `workflow_dispatch` can rebuild and re-upload assets for an existing tag.
- Displayed versions are v-less everywhere (`awm --version`, stamped binaries, release titles); the `v` prefix exists only on git tags.

### Code quality

- Codebase-wide lint remediation under the new policy, with behavior preserved (the full suite is green before and after): godoc contracts on every exported identifier (with mirrored wording across the SQLite and Postgres adapters), shadowed variables renamed, ~60 dead declarations removed, error returns handled or explicitly justified, and file permissions tightened for private state.
- The command dispatch layer now uses checked, typed payload handling: an internal payload-type mismatch returns a structured `INTERNAL_ERROR` instead of panicking the process.

## Security

- **`github.com/jackc/pgx/v5` 5.8.0 → 5.10.0.** The 5.9.2 floor fixes GO-2026-5004 (SQL injection via placeholder confusion with dollar-quoted string literals), which the new `govulncheck` gate flagged on a called path (`ListReviewAttempts`); Dependabot's first grouped update then took the driver to 5.10.0, alongside `modernc.org/sqlite` 1.46.1 → 1.53.0.
- **Go 1.26.5.** The `go` directive now tracks the current patch release, whose standard library fixes GO-2026-5856 (`crypto/tls`, reached via the awm-web server) and GO-2026-4970 (`os`, reached via the Postgres migration loader).

## Before you upgrade

- Nothing required. No schema migrations, no config changes, no flag changes — binaries are a drop-in replacement for 1.3.0.
- If you install via `go install …/cmd/awm@latest`, this is the first release that command will resolve directly (previously it tracked `main`). Pin `@v1.4.0` (or `@1.4.0`, which the dual tags make equivalent) for reproducible installs.
