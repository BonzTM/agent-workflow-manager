# [1.5.1] Release Notes - 2026-07-09

## Release Summary

A fast-follow patch for 1.5.0, produced by auditing a live environment against everything that release shipped. Three small gaps, all closed: **`init` now surfaces the new attribution settings in `.env.example`**, this repo's own seeded integration assets catch up with the 1.5.0 templates they had drifted from, and the last piece of pre-rename guidance pointing agents at the removed memory capability is gone. There are no command, contract, storage, or MCP changes — a drop-in upgrade from 1.5.0.

## Fixed

### `.env.example` surfaces the attribution settings

1.5.0 added `AWM_ACTOR_HARNESS` / `AWM_ACTOR_MODEL` / `AWM_ACTOR_SESSION` but the env-example scaffold never mentioned them, so adopters had no discovery path short of reading the CLI reference. Both scaffold paths now include them: fresh projects get the keys seeded, and projects with an existing `.env.example` gain the missing keys appended on the next `init` re-run.

### Seeded companion assets catch up with their templates

Init templates deliberately never overwrite files you may have edited — which means previously seeded copies don't pick up template improvements on their own. This repo's own installed assets had drifted that way: `.claude/commands/awm-work.md`, `.codex/awm-broker/README.md`, and `.opencode/awm-broker/README.md` were missing the 1.5.0 `work --task-text` auto-open guidance (and the codex copy an earlier MCP JSON-RPC export note). They now match the current template and skill-pack content. If you adopted awm before 1.5.0, your seeded copies have the same drift — refresh them from `skills/awm-broker/` or re-seed in a scratch checkout and diff.

### No more guidance pointing at the removed memory capability

`rule_capture_durable_decisions` in this repo's canonical rules still told agents to record durable decisions "in AMM" — the pre-rename product name, referring to the memory subsystem whose schema 1.5.0 removed. The rule now directs durable decisions to plan task evidence and repo docs.

## Before you upgrade

- Nothing required — drop-in from 1.5.0. The 1.5.0 shared-database notes still apply if you are coming from earlier than 1.5.0.
- Worth doing after upgrading: re-run `awm init` (it only appends missing `.env.example` keys), then `awm sync --mode full --insert-new-candidates` and `awm health` — the audit that produced this patch also found a stale pointer inventory, which sync clears.
