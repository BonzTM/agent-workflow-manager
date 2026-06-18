# Contributing to awm

Thank you for your interest in contributing to awm (agent-workflow-manager).

## Getting Started

1. Fork the repository and clone your fork.
2. Install Go 1.26+.
3. Run `go test ./...` to verify everything builds and passes.
4. Create a branch for your change.

## Development Workflow

```bash
# Build all binaries
go build -o dist/awm ./cmd/awm
go build -o dist/awm-mcp ./cmd/awm-mcp
go build -o dist/awm-web ./cmd/awm-web

# Run tests
go test ./...

# Run a specific package's tests
go test ./internal/service/backend/...
```

## Pull Requests

- Keep PRs focused on a single change.
- Include tests for new behavior under `cmd/**` or `internal/**`.
- Ensure `go test ./...` passes before submitting.
- Follow the existing code style — no linter configuration is imposed, but consistency with surrounding code is expected.

## What to Contribute

- Bug fixes with a failing test that demonstrates the fix.
- Documentation improvements.
- New init templates or agent integration assets.
- Storage adapter improvements (SQLite and Postgres must stay in parity).

## Go Style And Patterns

This repo follows the conventions in the [coding-handbook](../coding-handbook/golang/AGENTS.md). The key patterns as applied here:

### Errors

- Wrap with `fmt.Errorf("...: %w", err)` at package boundaries, not at every call site.
- Use `errors.Is` for sentinel matching and `errors.As` for typed errors.
- Error strings are lower-case, no trailing punctuation.
- Log errors once at the boundary that can act on them. Intermediate layers return wrapped errors.

### Logging

- Use `log/slog` throughout. Do not introduce global loggers.
- Standard fields: `operation`, `project_id`, `request_id` where available.
- JSON logs in production, text logs locally.

### Package Boundaries

- `internal/core/` defines interfaces. `internal/service/backend/` implements business logic. `internal/adapters/` translates between external protocols and core interfaces.
- Handlers do not query the database. Repositories do not contain transport logic. `main` does not absorb business rules.
- Define interfaces where they are consumed, not where they are implemented.
- Accept interfaces, return concrete types.

### Style

- `gofmt -s` is mandatory.
- Happy path on the left margin with early returns.
- Avoid naked returns, ignored errors without comment, and global mutable state.
- Package names are short, lower-case, descriptive. No `util`, `helpers`, `common`, or `misc` packages.
- Avoid stutter: `orders.Service` not `orders.OrderService`.

For the full rationale behind these patterns, see the [coding-handbook foundations](../coding-handbook/golang/foundations/).

## Architecture Notes

- `internal/contracts/v1` and `spec/v1` must move in lockstep — any payload, validation, or command change must update both plus their tests.
- CLI (`cmd/awm`) and MCP (`cmd/awm-mcp`) surfaces must stay in parity.
- SQLite and Postgres adapters must maintain behavioral equivalence.

See [docs/maintainer-reference.md](docs/maintainer-reference.md) and [docs/maintainer-map.md](docs/maintainer-map.md) for detailed architecture and change-routing guidance.

## License

By contributing, you agree that your contributions will be licensed under the [Apache License 2.0](LICENSE).
