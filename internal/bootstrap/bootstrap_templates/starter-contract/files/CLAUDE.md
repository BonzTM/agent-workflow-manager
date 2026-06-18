# CLAUDE.md

Claude companion for a repo whose primary contract is `AGENTS.md`.

## Source Of Truth

- Follow `AGENTS.md` first.
- If this file conflicts with `AGENTS.md`, `AGENTS.md` wins.

## Claude Workflow

See [.awm/awm-work-loop.md](.awm/awm-work-loop.md) for the full command reference. Claude slash-command equivalents:

| AWM command | Claude slash command |
|---|---|
| `awm context` | `/awm-context` |
| `awm work` | `/awm-work` |
| `awm verify` | `/awm-verify` |
| `awm review --run` | `/awm-review` |
| `awm done` | `/awm-done` |

Direct CLI (`awm sync`, `awm health`, `awm history`, `awm status`, `awm fetch`) has no slash-command wrappers — call those directly.

## Notes

- If the receipt looks stale or too narrow, re-run `/awm-context` with a better task description.
- If governed scope expands, declare new files through `/awm-work` before `/awm-review` or `/awm-done`.
- Do not claim success when `/awm-verify` failed or was skipped for code changes.
- When blocked on a missing decision, surface it instead of improvising.
