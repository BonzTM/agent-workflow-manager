# [1.1.0] Release Notes - 2026-03-23

## Release Summary

This release removes AWM's built-in memory subsystem (`awm memory`) in favor of the dedicated [Agent Memory Manager (AMM)](https://github.com/bonztm/agent-memory-manager) project. AMM provides a purpose-built persistent memory substrate with 16 typed records, 5-layer architecture, ambient recall, scoped memory, and background reflection — capabilities well beyond what AWM's simple 4-category system offered. The `awm memory` command, its storage layer, and all memory-related contract types have been fully removed.

## Fixed

- N/A

## Added

- `docs/deprecation/memory-removal.md` — Documents the rationale, what was removed, and migration guidance for adopters moving from AWM memory to AMM.

## Changed

- **`awm context`** — Receipts no longer include a `memories` section or memory-derived tags in `resolvedTags`. The `Memories` field and `ContextMemory` type have been removed from `ContextReceipt`. Receipt IDs are now computed without memory input, producing different deterministic IDs for the same inputs compared to 1.0.0.
- **`awm status`** — Context preview no longer includes `MemoryCount`.
- **`awm health`** — The `weak_memories` check has been removed. The `unknown_tags` check now only inspects pointer tags. Health checks report 10 check categories (down from 11).
- **`awm history`** — The `memory` entity type has been removed. Valid entities are now `all`, `work`, `receipt`, and `run`.
- **`awm done`** — No longer persists `MemoryIDs` in run receipt summaries.
- **`awm fetch`** — No longer recognizes `mem:<id>` keys. Memory key lookups return not-found.
- **`awm export`** — Memory document rendering has been removed. The `ExportDocumentKindMemory` and `ExportBundleItemKindMemory` kinds no longer exist.
- **MCP tools** — 12 tools (down from 13). The `memory` tool has been removed from `mcp.tools.v1.json`.
- **Claude command pack** — `/awm-memory` slash command removed. The `claude-command-pack` init template now produces 7 files (down from 8).
- **Skill-pack docs** — All `awm-broker` references to `awm memory`, `/awm-memory`, and "durable memory" have been removed from Claude, Codex, and OpenCode companion docs.
- **Web dashboard** — The Memories page (`/memories.html`) and the `GET /api/memories` + `GET /api/memories/{key}` API routes have been removed.
- **Adoption modes** — Reduced from four to three: plans-only, governed workflow, full brokered flow. The `plans+memory` mode no longer exists.

## Removed

### Command and API Surface

- **`awm memory`** — Removed from CLI, MCP tool catalog (`mcp.tools.v1.json`), HTTP API, and command dispatch.
- **`/awm-memory`** — Slash command removed from Claude command pack and all init templates.
- **`/api/memories`** and **`/api/memories/{key}`** — HTTP API routes removed.
- **`mem:<id>` fetch keys** — `awm fetch` no longer resolves memory keys.
- **`--entity memory`** — No longer a valid entity for `awm history`.

### Contract Types

- `CommandMemory`, `MemoryPayload`, `MemoryCommandPayload`, `MemoryResult`, `MemoryValidation`, `MemoryCategory` — Removed from `contracts/v1`.
- `ContextMemory`, `ExportMemoryDocument`, `ExportDocumentKindMemory`, `ExportBundleItemKindMemory` — Removed.
- `HistoryEntityMemory` — Removed.
- `MemoryCount` — Removed from `StatusContextPreview`.
- `MemoryIDs` — Removed from `RunReceiptSummary` and `ReceiptScope`.
- `Memories` — Removed from `ContextReceipt`.
- `Memory` — Removed from `ExportDocument` and `ExportBundleItem`.
- `MemoryKeys` — Removed from `ExportReceiptDocument`.

### Core Interfaces

- `core.Service.Memory()` — Removed.
- `core.Repository.FetchActiveMemories()`, `.LookupMemoryByID()`, `.PersistMemory()` — Removed.
- `core.HistoryRepository.ListMemoryHistory()` — Removed.
- `core.ActiveMemory`, `ActiveMemoryQuery`, `MemoryLookupQuery`, `MemoryPersistence`, `MemoryPersistenceResult`, `MemoryValidation`, `MemoryHistoryListQuery`, `MemoryHistorySummary` — All removed.
- `core.ErrMemoryLookupNotFound` — Removed.

### Storage Adapters

- SQLite and Postgres repository methods for memory fetch, lookup, persist, and history — Removed.
- Postgres query builders for memory SQL — Removed.
- `NormalizeMemoryPersistence` — Removed from storage domain.

### Schema Files

- `spec/v1/shared.schema.json` — `memory` removed from command enum.
- `spec/v1/cli.command.schema.json` — Memory command definition, `memoryPayload`, and memory entity references removed.
- `spec/v1/cli.result.schema.json` — `memoryResult`, memory document kinds, and memory entity references removed.
- `spec/v1/mcp.tools.v1.json` — Memory tool definition removed.

## Admin/Operations

- Database migration DDL is **intentionally preserved**. The `awm_memories` and `awm_memory_candidates` tables remain in migration scripts for schema compatibility. Existing databases retain these tables as inert artifacts. No data migration is required.
- The `memory_ids` column in receipt scope and receipt summary tables continues to be written (as empty arrays) for schema stability. A future migration may drop these columns.

## Deployment and Distribution

- Go install: `go install github.com/bonztm/agent-workflow-manager/cmd/awm@v1.1.0`
- Go install (MCP): `go install github.com/bonztm/agent-workflow-manager/cmd/awm-mcp@v1.1.0`
- Go install (Web): `go install github.com/bonztm/agent-workflow-manager/cmd/awm-web@v1.1.0`
- Source: `https://github.com/BonzTM/agent-workflow-manager`
- Prebuilt binaries: download `awm-binaries` artifact from GitHub Actions `Go Build` workflow.

```bash
go install github.com/bonztm/agent-workflow-manager/cmd/awm@v1.1.0
go install github.com/bonztm/agent-workflow-manager/cmd/awm-mcp@v1.1.0
go install github.com/bonztm/agent-workflow-manager/cmd/awm-web@v1.1.0
```

## Breaking Changes

- **`awm memory` removed** — Any automation or script that calls `awm memory` will fail. Adopt [AMM](https://github.com/bonztm/agent-memory-manager) for durable agent memory.
- **`mem:<id>` fetch keys** — `awm fetch` no longer recognizes memory keys.
- **`/api/memories` HTTP routes** — Removed. The web dashboard memories page no longer functions.
- **`--entity memory`** — No longer a valid entity for `awm history`.
- **Receipt ID stability** — Receipt IDs for the same task text and phase will differ from 1.0.0 if the project previously had active memories. Receipt IDs are deterministic within a version but not guaranteed stable across versions.
- **MCP tool count** — Reduced from 13 to 12. Clients that enumerate tools by count may need updating.
- **`/awm-memory` slash command** — Removed from the Claude command pack.

## Known Issues

- N/A

## Compatibility and Migration

- Requires Go 1.26+ for `go install` or building from source.
- Direct upgrade from 1.0.0. No data migration required — existing databases work as-is.
- Adopters using `awm memory` should migrate to [Agent Memory Manager (AMM)](https://github.com/bonztm/agent-memory-manager) before upgrading.
- See `docs/deprecation/memory-removal.md` for detailed migration guidance.

## Full Changelog

- Compare changes: https://github.com/BonzTM/agent-workflow-manager/compare/1.0.0...1.1.0
- Full changelog: https://github.com/BonzTM/agent-workflow-manager/commits/1.1.0
