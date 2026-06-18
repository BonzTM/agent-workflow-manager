# Memory Subsystem Removal

AWM's built-in memory subsystem (`awm memory`) has been removed in favor of [Agent Memory Manager (AMM)](https://github.com/bonztm/agent-memory-manager), a dedicated persistent memory substrate for agents.

## Why

AWM's memory was a simple 4-category system (decision, gotcha, pattern, preference) with a propose-then-promote lifecycle tied to receipts and work plans. AMM replaces it with a purpose-built memory system offering:

- 16 typed durable memory records (preference, fact, decision, episode, identity, procedure, constraint, and more)
- 5-layer memory architecture (working, history, compression, canonical, derived indexes)
- 9 retrieval modes including ambient recall
- Scoped memory (global, project, session) with orthogonal privacy levels
- Background reflection, compression, and maintenance workers
- FTS5 full-text search and provenance tracking
- Multi-signal scoring with 10-factor ranking

AWM's memory was tightly coupled to receipt scope and evidence pointers, which limited its usefulness as a general-purpose memory system. AMM operates independently and can serve any agent runtime.

## What Was Removed

The full memory subsystem was removed in a combined Phase 1+2+3 pass:

### Command surface
- `awm memory` command — removed from CLI, MCP tool catalog, HTTP API, and command dispatch
- `CommandMemory` constant and all memory payload/result/validation contract types
- `awm fetch mem:<id>` — no longer recognizes memory keys
- `awm export` — no longer renders memory documents
- `awm history --entity memory` — entity type removed; history search covers work, receipt, and run entities
- `GET /api/memories` and `GET /api/memories/{key}` HTTP routes — removed

### Context and receipt changes
- `awm context` no longer fetches or injects memories into receipts. The `Memories` field was removed from `ContextReceipt`. Memory-derived tags no longer appear in `resolvedTags`. `MemoryIDs` are no longer persisted in receipt scopes.
- `awm status` context preview no longer has a `MemoryCount` field.
- `awm done` no longer persists `MemoryIDs` in run summaries.
- Receipt IDs are computed without memory input, producing different deterministic IDs for the same inputs compared to pre-removal versions.

### Health checks
- `weak_memories` health check — removed entirely
- `unknown_tags` health check — no longer inspects memory tags (only pointer tags)

### Storage layer
- Repository interface methods removed: `FetchActiveMemories`, `LookupMemoryByID`, `PersistMemory`, `ListMemoryHistory`
- SQLite and Postgres adapter implementations for those methods — removed
- Postgres query builders for memory SQL — removed
- Storage domain `NormalizeMemoryPersistence` — removed
- Core types removed: `ActiveMemory`, `ActiveMemoryQuery`, `MemoryLookupQuery`, `MemoryPersistence`, `MemoryPersistenceResult`, `MemoryValidation`, `MemoryHistoryListQuery`, `MemoryHistorySummary`

### Database migrations preserved
The `awm_memories` and `awm_memory_candidates` table DDL in SQLite and Postgres migrations is **intentionally preserved**. Existing databases have these tables and removing the migration DDL would break fresh installs that replay migrations. The tables remain inert — no code reads from or writes to them.

## Action for Adopters

Install and configure [Agent Memory Manager (AMM)](https://github.com/bonztm/agent-memory-manager) for durable agent memory. Any scripts or automations that called `awm memory`, `awm fetch mem:<id>`, or the `/api/memories` HTTP endpoint will need to be migrated to AMM's API.

Memory data in existing AWM databases is no longer accessible through AWM's API. If you need to preserve historical memory data, export it directly from the database before upgrading.
