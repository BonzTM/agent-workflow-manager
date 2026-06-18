# Init Templates

`awm init` accepts repeatable `--apply-template <id>` flags so you can start minimal, then opt into heavier scaffolding later without replacing edited repo files.

Example:

```bash
awm init \
  --apply-template starter-contract \
  --apply-template verify-generic \
  --apply-template claude-command-pack
```

Swap in `--apply-template codex-pack` when you want repo-local Codex companion docs under `.codex/awm-broker/` instead of Claude-specific command assets.
Add `--apply-template codex-hooks` when you want the current experimental repo-local Codex hook layer under `.codex/`.
Use `--apply-template opencode-pack` when you want repo-local OpenCode companion docs under `.opencode/awm-broker/`.
Use `scripts/install-skill-pack.sh --opencode` when you want to add those same docs to an existing repo without rerunning `init`.

For repos that want stricter feature planning, add `--apply-template detailed-planning-enforcement`. It can be applied directly or after `starter-contract` + `verify-generic`, and it only upgrades those scaffolds while they are still pristine.

Current built-ins:

- `starter-contract`
  - seeds `AGENTS.md` and `CLAUDE.md`
  - upgrades blank AWM rules scaffolds to a richer starter ruleset
- `detailed-planning-enforcement`
  - seeds `docs/feature-plans.md` and `scripts/awm-feature-plan-validate.py`
  - upgrades pristine `starter-contract` docs/rules and pristine blank or `verify-generic` test scaffolds to the richer feature-plan workflow
- `verify-generic`
  - upgrades blank AWM test scaffolds to a language-agnostic verify profile
  - uses `awm status` plus git checks so it works out of the box
- `verify-go`
  - upgrades blank AWM test scaffolds to a Go-oriented verify profile
- `verify-ts`
  - upgrades blank AWM test scaffolds to a TypeScript-oriented verify profile
- `verify-python`
  - upgrades blank AWM test scaffolds to a Python-oriented verify profile
- `verify-rust`
  - upgrades blank AWM test scaffolds to a Rust-oriented verify profile
- `codex-pack`
  - seeds `.codex/awm-broker/README.md` and `.codex/awm-broker/AGENTS.example.md`
  - adds repo-local Codex companion docs without pretending slash-command or hook parity
- `codex-hooks`
  - seeds `.codex/config.toml`, `.codex/hooks.json`, and `.codex/hooks/*`
  - includes `.codex/hooks/awm-session-context.sh`, `.codex/hooks/awm-prompt-guard.sh`, and `.codex/hooks/awm-stop-guard.sh`
  - enables Codex's current experimental lifecycle hooks for startup reminders, prompt-time context nudges, and a one-time closeout guard
  - stays narrower than Claude's hook pack and depends on current upstream experimental hook support
- `opencode-pack`
  - seeds `.opencode/awm-broker/README.md` and `.opencode/awm-broker/AGENTS.example.md`
  - adds repo-local OpenCode companion docs without claiming undocumented runtime integration behavior
  - does not add hooks; this repo does not currently document a verified native OpenCode hook mechanism
- `claude-command-pack`
  - seeds `.claude/commands/*` and `.claude/awm-broker/*`
- `claude-hooks`
  - merges additive Claude hook settings into `.claude/settings.json`
  - seeds `.claude/hooks/awm-receipt-guard.sh`, `.claude/hooks/awm-receipt-mark.sh`, `.claude/hooks/awm-session-context.sh`, `.claude/hooks/awm-edit-state.sh`, and `.claude/hooks/awm-stop-guard.sh`
  - injects the AWM loop at session start/compaction, keeps edits blocked until a task-bearing `/awm-context` or equivalent `context` request succeeds, nudges `/awm-work` once edits span files or governed scope expands, and blocks stop until edited work is reported
- `git-hooks-precommit`
  - seeds `.githooks/pre-commit`
  - forwards staged additions, modifications, renames, type changes, and deletions into `awm verify --phase review`

Safety contract:

- templates create missing files
- templates may replace AWM-owned blank scaffolds only while they are still pristine
- templates may merge known additive JSON fragments such as `.claude/settings.json`
- templates never delete files
- templates never overwrite user-edited files; conflicts are skipped
