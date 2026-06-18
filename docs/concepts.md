# Concepts

This page defines the core terms used throughout awm. Read this if you're new or if a term in the docs isn't clear.

## Pointer

A pointer is an entry in awm's index that points to something in your codebase — a file, a function, a doc, a test, a rule. Each pointer has:

- **Key** — unique identifier in the format `project:path#anchor` (anchor is optional). Example: `my-cool-app:src/auth/login.go#handleTimeout`
- **Kind** — one of: `code`, `rule`, `doc`, `test`, `command`
- **Label** — short human-readable name
- **Description** — one-line summary of what this thing does
- **Tags** — flat list of canonical tags for scoping (e.g., `backend`, `auth`, `test`)
- **Content hash** — used to detect staleness when files change

Pointers are lightweight. They don't contain the full file content — just enough for awm to index repo artifacts, support `fetch`, power health/status reporting, and let plans point back to concrete code or docs.

## Receipt

When you call `context`, awm returns a receipt. A receipt is a scoped snapshot of everything relevant to your task. It contains:

- **Rules** — hard constraints the agent must follow
- **Plans** — active work plans for the project, with fetch keys for resumption
- **Initial scope paths** — any file paths the caller already knew at task start
- **Meta** — receipt ID, resolved tags, task metadata, and the context-time baseline used for later task-delta detection

The receipt ID is used as a handle for all subsequent operations (`fetch`, `work`, `review`, `verify`, `done`). It ties everything back to the original context snapshot without letting later execution mutate that history.

## Rule

A rule is a pointer with `kind: rule` that represents a behavioral constraint for agents. Rules have two extra properties:

- **Rule ID** — stable identifier (e.g., `rule_context_first`). Deterministic — survives re-syncs.
- **Enforcement** — `hard` (must follow) or `soft` (should follow)

Hard rules are always included with their full content in the receipt. Soft rules are summary-only and can be fetched on demand.

You author rules in `.awm/awm-rules.yaml` and sync them into awm. awm delivers the right rules at the right time based on task tags and phase.

## Plan

A plan is a durable work container that tracks what an agent is doing. Plans survive context compaction — when an agent loses its conversation history, `context` returns active plans with fetch keys, so the agent can resume where it left off.

Each plan has:

- **Plan key** — `plan:<receipt_id>` format, auto-derived from `receipt_id` if not provided
- **Title** — human-readable name
- **Objective** — what the plan aims to achieve
- **Kind** — free-form label (e.g., `bugfix`, `feature`)
- **Status** — `pending`, `in_progress`, `complete`, or `blocked`
- **Stages** — optional planning stages that track spec maturity:
  - `spec_outline` — initial high-level spec drafted
  - `refined_spec` — spec reviewed and refined with details
  - `implementation_plan` — concrete implementation steps defined
  Each stage has its own status (`pending`, `in_progress`, `complete`, `blocked`).
- **Scope** — in-scope/out-of-scope/constraints lists
- **Discovered paths** — repo-relative files the agent later found and explicitly declared through `work` so governed `review` / `done` can validate against them without mutating the original receipt

The stage fields are intentionally generic so repos can define richer planning contracts on top of them. A common pattern is to require root `kind=feature` plans, mirror stage status with top-level `stage:*` tasks, and treat leaf tasks with `acceptance_criteria` as the atomic units of work. This repo documents that pattern in `docs/feature-plans.md`.

Plans are created and updated via the `work` command. Updates can use `merge` mode (default, upserts tasks by key) or `replace` mode (replaces all tasks).

## Task

A task is a unit of work within a plan. Tasks have:

- **Key** — unique identifier within the plan (e.g., `implement-auth-refactor`, `verify:tests`)
- **Summary** — what needs to be done
- **Status** — `pending`, `in_progress`, `complete`, or `blocked`
- **Dependencies** — keys of other tasks this one depends on
- **Acceptance criteria** — conditions for completion
- **Outcome** — what happened (filled on completion)
- **Evidence** — references supporting the outcome

Tasks can reference a `parent_task_key` for grouping, and can be fetched individually via `fetch --key task:plan:<receipt-id>#<task-key>`. Repos that want stricter atomic-task planning can enforce parent/child structure and leaf-task acceptance criteria through repo-local `verify` checks.

Special task keys with built-in meaning:

| Key | Purpose |
|---|---|
| `verify:tests` | Confirms tests were run (required by default completion gate) |
| `verify:diff-review` | Optional manual diff inspection task |
| `review:cross-llm` | Default key for the thin `review` command — cross-LLM or human review |

The `review` command lowers to a single `work.tasks[]` merge update. Defaults when flags are omitted: `key=review:cross-llm`, `summary="Cross-LLM review"`, `status=complete`. With `--run`, awm executes the workflow task's `run` block and records `complete` or `blocked` automatically.

## Tag

Tags are flat labels used to scope pointers, rules, verification selectors, and workflow selectors. Examples: `backend`, `auth`, `test`, `frontend`.

awm normalizes tags through a canonical dictionary that maps aliases to a single form (e.g., `api` and `server` both map to `backend`). In the simpler context model, tags help route rules, verification, workflow selection, and init/onboarding guidance; they are no longer presented as a ranked retrieval engine.

The tag dictionary has two layers:
- **Embedded base** — ships with awm (`internal/service/backend/canonical_tags.json`), covers common aliases
- **Repo-local overrides** — `.awm/awm-tags.yaml` (auto-discovered), adds project-specific tags and aliases on top of the base

Use `--tags-file` on any command that does tag normalization to override discovery with an explicit path.

Tags replace the concept of "profiles" from other systems. They're simpler: just strings, no hierarchy, no inheritance.

## Phase

Every `context` call includes a phase:

- **plan** — spec/planning work
- **execute** — implementation work
- **review** — review/verification work

The phase helps downstream workflow selection and verification. It no longer implies ranked retrieval behavior.

## Scope Mode

Controls how strictly awm enforces that file-backed work stays within effective scope:

- **warn** (default) — violations are logged but the request is accepted
- **strict** — violations reject the request; the agent must declare or refresh scope before closing the task
Effective scope is the union of:

- receipt `initial_scope_paths`
- plan `discovered_paths`
- AWM-managed governance files that are allowed outside project code scope, including repo-root `AGENTS.md`, `CLAUDE.md`, and the canonical `.awm/**` contract files

`done` and runnable `review` use this effective scope together with the receipt baseline to validate what actually changed.

## Canonical Ruleset

The human-authored YAML file where you define your rules. awm discovers it automatically at `.awm/awm-rules.yaml` (preferred) or `awm-rules.yaml` in the project root. Use `--rules-file` on `sync`, `health --fix`, or `init` to override with an explicit path. If you maintain a repo-local tag dictionary separately, use `--tags-file` on `context`, `done`, `sync`, `health --fix`, `verify`, or `init` to supply it explicitly for canonical tag normalization. Format:

```yaml
version: awm.rules.v1
rules:
  - id: rule_name
    summary: One-line description
    content: Full rule text (optional, defaults to summary)
    enforcement: hard|soft (optional, defaults to hard)
    tags: [tag1, tag2] (optional)
```

awm reads this file during `sync` or `awm health --fix sync_ruleset` and upserts the rules as pointers. The canonical ruleset is the source of truth — awm stores and delivers, the file defines.

## Executable Verification Definitions

The human-authored YAML file where you define repo-local executable checks for `verify`. awm discovers it automatically at `.awm/awm-tests.yaml` (preferred) or `awm-tests.yaml` in the project root. Use `--tests-file` on `verify` to override with an explicit path.

`verify` selects repo-defined executable checks from receipt context, phase, changed files, and optional explicit test ids, then runs them sequentially.

Format:

```yaml
version: awm.tests.v1

defaults:
  cwd: .
  timeout_sec: 300

tests:
  - id: go-unit
    summary: Run Go unit tests for AWM packages
    command:
      argv: ["go", "test", "./cmd/...", "./internal/..."]
      env:
        GOFLAGS: "-count=1"
    select:
      phases: ["execute", "review"]
      tags_any: ["backend"]
      changed_paths_any: ["cmd/**", "internal/**"]
    expected:
      exit_code: 0

  - id: smoke
    summary: Run repo smoke checks on every verification pass
    command:
      argv: ["go", "test", "./cmd/...", "./internal/..."]
    select:
      always_run: true
```

v1 test definitions are argv-only. They support optional `command.env` entries for repo-defined environment variables and `select.always_run: true` for default smoke checks that should auto-select. `verify` reuses the existing `verify:tests` task key for definition-of-done updates when work context is present. `verify:diff-review` is optional workflow metadata, not a built-in awm completion gate.

## Workflow Gate Definitions

The human-authored YAML file where you define which work task keys must be complete before `done` should be considered complete. awm discovers it automatically at `.awm/awm-workflows.yaml` (preferred) or `awm-workflows.yaml` in the project root.

Format:

```yaml
version: awm.workflows.v1
completion:
  required_tasks:
    - key: verify:tests
      select:
        changed_paths_any: ["cmd/**", "internal/**", "go.mod", "go.sum"]

    - key: review:cross-llm
      summary: Cross-LLM review
      rerun_requires_new_fingerprint: true
      select:
        phases: ["review"]
        changed_paths_any: ["cmd/**", "internal/**", "spec/**"]
      run:
        # Edit argv to match your reviewer setup (provider, model, reasoning level, native
        # permission/sandbox flags, etc.)
        argv: ["scripts/awm-cross-review.sh", "--provider", "codex", "--yolo"]
        cwd: .
        timeout_sec: 1800
```

Runnable review gates are terminal checks, not inner-loop retries. AWM persists append-only review attempts and, when `rerun_requires_new_fingerprint: true`, only skips reruns after a passing attempt already assessed the current fingerprint; failed or interrupted same-fingerprint attempts rerun until any configured `max_attempts` budget is exhausted. The scoped fingerprint covers the effective scope for the task: receipt `initial_scope_paths`, any `plan.discovered_paths` recorded through `work`, and AWM-managed governance files that completion reporting already allows outside plan scope, including repo-root `AGENTS.md` and `CLAUDE.md`.

Selectors use the same shape as `verify` selection: `phases`, `tags_any`, `changed_paths_any`, and `always_run`.

**Fallback behavior:** If no workflow file exists or no completion requirements are declared, awm requires `verify:tests` by default. When a workflow declares requirements, only those task keys are enforced.

Init seeds a thin `required_tasks: []` skeleton. Adding keys like `review:cross-llm` is opt-in, and reviewer-specific script arguments such as provider choice, model, reasoning, and the shared `--yolo` high-trust shortcut belong in the workflow `run.argv`; for Claude, `--yolo` maps to dangerous permission bypass.

## File-Based Flags

Most CLI commands that accept text or list values support inline flags and file-based alternatives. JSON list/object inputs also support `--*-json` variants for one-shot calls without temp files:

| Inline flag | JSON inline | File alternative | Format |
|---|---|---|---|
| `--task-text` | - | `--task-file` | Plain text |
| `--content` | - | `--content-file` | Plain text |
| `--outcome` | - | `--outcome-file` | Plain text |
| `--key` (repeatable) | `--keys-json` | `--keys-file` | JSON string array |
| `--file-changed` (repeatable) | `--files-changed-json` | `--files-changed-file` | JSON string array |
| `--evidence-key` (repeatable) | `--evidence-keys-json` | `--evidence-keys-file` | JSON string array |
| `--evidence-path` (repeatable) | `--evidence-paths-json` | `--evidence-paths-file` | JSON string array |
| `--evidence` (repeatable, review) | `--evidence-json` | `--evidence-file` | JSON string array |
| `--related-key` (repeatable) | `--related-keys-json` | `--related-keys-file` | JSON string array |
| `--related-path` (repeatable) | `--related-paths-json` | `--related-paths-file` | JSON string array |
| `--expect` (repeatable) | `--expected-versions-json` | `--expected-versions-file` | JSON object (`{"key": "version"}`) |
| - | `--plan-json` | `--plan-file` | JSON object (work plan metadata) |
| - | `--tasks-json` | `--tasks-file` | JSON array of work tasks |

All file flags accept `-` for stdin.

`--tags-file` is reserved for the canonical tag dictionary override on commands that do runtime tag normalization.
