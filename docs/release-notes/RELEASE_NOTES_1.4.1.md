# [1.4.1] Release Notes - 2026-07-09

## Release Summary

A fast-follow patch for 1.4.0 focused on one thing: **every file awm writes into your project is now crash-atomic, and none of those writes can orphan a symlink.** If you keep scaffolded files — `AGENTS.md`, `.env.example`, `.gitignore`, tag documents — symlinked into a dotfiles repository, awm now writes through the link to its target; and if awm (or your machine) dies mid-write, you can never be left with a truncated or half-appended file. There are no command, contract, storage, or MCP changes — a drop-in upgrade from 1.4.0.

## Fixed

### Crash-atomic, symlink-preserving writes

A new shared write layer (`internal/fswrite`) now backs every production file write:

- **Atomic replaces**: content is written to a temp file in the target directory and renamed into place — after first resolving the symlink chain, so the rename replaces the link's final *target*, never the link itself. A dangling link (fresh dotfiles checkout) gets its target created; an existing target keeps its file permissions.
- **Atomic exclusive creates**: scaffold paths that must only create-if-missing keep exactly those semantics via an atomic hard-link publish — the fully-written temp file is linked into place, failing cleanly if the file appears concurrently. A partially-written file can never be observed.
- **Converted paths**: bootstrap template writes (create, update, replace-if-pristine), scaffolded workflow assets, the workspace `.gitignore` merge (which previously also pre-created an empty file before writing), and the `.env.example` append (now assembled in memory and written whole).

The change was driven through the repo's cross-LLM review gate, which rejected three intermediate versions for incomplete coverage; each gap is now closed and pinned by tests (symlink write-through, dangling-target creation, mode preservation, exclusive-create semantics, link-loop rejection).

## Before you upgrade

- Nothing required — drop-in from 1.4.0. If an earlier tool ever orphaned one of your symlinked files, relink it once; from this release forward awm preserves it.
