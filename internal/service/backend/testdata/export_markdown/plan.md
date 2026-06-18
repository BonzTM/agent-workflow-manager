# Export Plan

Implement deterministic markdown

- Plan Key: plan:receipt-339e5cfde29fa58b2c5f1c16
- Receipt ID: receipt-339e5cfde29fa58b2c5f1c16
- Status: in_progress
- Title: Exportable AWM artifacts with JSON and Markdown renderers
- Objective: Ship export output across read surfaces.
- Kind: feature
- Parent Plan: plan:receipt-parent

## Stages

- implementation_plan=in_progress
- spec_outline=complete

## In Scope

- `internal/service/backend/export.go`
- `spec/v1/cli.result.schema.json`

## Constraints

- Keep contracts in lockstep
- Keep markdown deterministic

## References

- `AGENTS.md`
- `README.md`

## External References

- plan:receipt-parent

## Tasks

### Add golden tests

- Task Key: impl:tests-markdown-golden
- Plan Key: plan:receipt-339e5cfde29fa58b2c5f1c16
- Status: pending
- Summary: Add golden tests
- Parent Task: group:parity-tests-docs

#### Depends On

- impl:renderer-markdown-plan

#### Acceptance Criteria

- Golden fixtures stay stable across repeated runs

#### References

- `internal/service/backend/export_test.go`

#### External References

- verifyrun:verify-1

#### Evidence

- go test ./internal/service/backend
