# Status project.alpha

ready=false missing=1 warnings=1

- Ready: false
- Missing Count: 1
- Warning Count: 1

## Project

- Project ID: project.alpha
- Project Root: /repo
- Detected Repo Root: /repo
- Backend: sqlite
- Postgres Configured: false
- SQLite Path: /repo/.awm/context.db
- Uses Implicit SQLite Path: true
- Unbounded: false

## Sources

### rules

- Source Path: .awm/awm-rules.yaml
- Absolute Path: /repo/.awm/awm-rules.yaml
- Exists: true
- Loaded: true
- Item Count: 3

#### Notes

- loaded from project root

## Integrations

### awm-broker

- Summary: Skill pack installed
- Installed: true
- Present Targets: 2
- Expected Targets: 2

## Context Preview

- Task: inspect export readiness
- Phase: execute
- Status: ok
- Resolved Tags: `backend`, `export`
- Rule Count: 3
- Plan Count: 1
- Initial Scope Paths: 1

## Missing

- `tests_file_missing` tests file not configured

## Warnings

- `stale_plan` plan needs status refresh
