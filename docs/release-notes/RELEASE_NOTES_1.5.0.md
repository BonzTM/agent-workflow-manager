# [1.5.0] Release Notes - 2026-07-09

## Release Summary

The adoption release. Three themes, all aimed at making awm easier to pick up and harder to outgrow: **the audit layer now answers "who did what"** — every receipt, run, verification batch, and review attempt can carry the harness, model, and session that performed it, auto-detected for Claude Code, Codex, and OpenCode with zero configuration; **the minimum honest loop is now one command** — `awm work --task-text "fix the login bug"` opens its own receipt, so contributors who never learned the ceremony still land durable governed state; and **receipts can now vouch for a pull request** — an opt-in, advisory-by-default CI report names, for every file changed in a git range, the run, receipt, and actor that covered it. Two long-dormant pieces of storage are gone, which makes this the first release with breaking notes — read **Before you upgrade** if multiple binaries share one database.

## Added

### Actor attribution — who did what, recorded

The five workflow commands (`context`, `work`, `verify`, `review`, `done`) accept an optional actor identity (`harness`, `model`, `session_id`), resolved at the adapter boundary so the backend only ever sees explicit values:

1. explicit `--actor-harness` / `--actor-model` / `--actor-session` flags,
2. `AWM_ACTOR_HARNESS` / `AWM_ACTOR_MODEL` / `AWM_ACTOR_SESSION` env vars,
3. harness auto-detection: `CLAUDECODE`/`CLAUDE_CODE_ENTRYPOINT` → `claude-code`, `CODEX_SANDBOX` → `codex`, `OPENCODE` → `opencode`.

Migration `0016` persists actors on receipts, runs, verification batches, and review attempts in both backends; run history, run fetch content, and export documents expose them. When nothing resolves, records stay unattributed and behave exactly as before. The deterministic receipt id deliberately **excludes** the actor, so two different agents opening the same task converge on the same receipt — attribution never fragments the cross-agent handoff story.

### `work` opens its own receipt

With no `--plan-key` or `--receipt-id`, `awm work --task-text "<task>"` (optionally `--phase`, default `execute`) opens the receipt itself through the same path `context` uses: canonical rules, working-tree baseline captured at open time, deterministic id. A later `context` call for the same task converges on the same receipt. Explicit identifiers always win, so existing callers are unchanged. One honesty note: the baseline is captured when the receipt opens — repos relying on strict scope checks should keep opening receipts with `context` *before* editing.

### Receipt evidence for pull requests (opt-in, advisory by default)

- Run history items now carry `files_changed` and the recording actor.
- Migration `0017` records the repo's HEAD sha and branch on runs at `done` time — best-effort, with a short timeout; a missing git binary or repo never fails the closeout.
- `scripts/awm-receipt-evidence.sh` reports, for every file changed between `--base` and `--head`, whether a closed receipt's run lists it — naming the covering run, receipt, and actor. **It always exits 0** unless `--enforce` (or `AWM_EVIDENCE_ENFORCE=true`) opts into failing on uncovered files. Copy [docs/examples/receipt-evidence-workflow.yml](../examples/receipt-evidence-workflow.yml) into `.github/workflows/` to run it on pull requests — `awm init` never seeds it, and nothing becomes required unless you make it so.

### Template names survive renames

Removed or renamed init template names resolve through a deprecated-alias map to their current replacement (starting with `claude-receipt-guard` → `claude-hooks`), so `awm init --apply-template` commands recorded in docs, scripts, or muscle memory keep working across releases instead of failing the whole init.

## Fixed

- **No more silent temp-dir databases.** When no explicit `AWM_SQLITE_PATH` is set and no project root resolves, awm now fails with an error naming `AWM_SQLITE_PATH` and `AWM_PROJECT_ROOT` as remedies, instead of quietly writing durable state to the OS temp dir.
- **Friendly failure for unknown receipts in `work`.** A wrong or foreign receipt id now returns `NOT_FOUND: receipt scope was not found` before anything persists, instead of leaking `FOREIGN KEY constraint failed (787)` after the plan row had already been written (and orphaning it).
- **`awm status` can't drift from the template catalog.** The status integration list derives from the embedded catalog via the new `bootstrap.TemplateIDs()` rather than a hardcoded list.
- **Language verify profiles upgrade cleanly.** `detailed-planning-enforcement` now treats `verify-go`/`verify-ts`/`verify-python`/`verify-rust` output as pristine upgrade sources instead of silently conflict-skipping the tests file.
- **The Postgres integration suite runs again.** A leftover unused import had broken the `-tags integration` build entirely; with it restored, the two bit-rotted tests were fixed and the suite is green end to end against a live database.

## Removed

- **Breaking: the dormant memory subsystem's schema is dropped** (migration `0015`): the never-surfaced `awm_memories` and `awm_memory_candidates` tables and the receipts memory-ids column. No command, MCP tool, or contract ever exposed this data.
- **Breaking: the legacy `acm_*` → `awm_*` rename shim is removed** from both backends.

## Before you upgrade

- **Shared databases:** once any 1.5.0 binary touches a database, migrations `0015`–`0017` apply. `0016`/`0017` are additive (older binaries keep working), but `0015` drops a column that pre-1.5.0 binaries still read — **binaries older than 1.5.0 fail receipt reads against a migrated database.** Upgrade every binary that shares a database (`awm`, `awm-mcp`, `awm-web`, and any other hosts pointing at the same `AWM_PG_DSN`) together.
- **Pre-rename databases:** a database created before the ACM-to-AWM rename must run any 1.4.x binary once (which performs the in-place rename) before upgrading to 1.5.0. Fresh databases and databases already on `awm_*` naming are unaffected.
- Nothing else is required. Attribution, auto-open, and the evidence report are all opt-in or automatic-and-invisible when unused.
