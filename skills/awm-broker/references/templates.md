# awm-broker Templates

## Recommended Loop

These examples assume installed `awm` and `awm-mcp` binaries are available on `PATH`.
Example payloads show explicit `project_id` values for clarity. In live usage you may omit `project_id` when `AWM_PROJECT_ID` is set or awm can infer the project from the effective repo root.
For Codex-first repo setup, the installed skill also ships `codex/README.md` and `codex/AGENTS.example.md`, `awm init --apply-template codex-pack` seeds repo-local companion copies under `.codex/awm-broker/`, and `awm init --apply-template codex-hooks` can additionally seed the current experimental Codex hook layer under `.codex/`.
For OpenCode-first repo setup, `scripts/install-skill-pack.sh --opencode` or `awm init --apply-template opencode-pack` seeds repo-local companion docs under `.opencode/awm-broker/`.

1. Run `awm context`.
2. Follow the returned rules block (or rule pointers) as hard requirements.
3. Note any `initial_scope_paths` already known at task start and treat them as the initial governed scope, not as a substitute for native repo search.
4. Run `fetch` only for indexed artifacts you actually need to hydrate, either by explicit keys or by `receipt_id` shorthand, which derives the plan fetch key.
5. Execute the task.
6. Run `work` with `receipt_id` (no `plan_key` required) to publish updates. Use `tasks` and include `verify:tests` for executable verification tracking; add other task keys when `.awm/awm-workflows.yaml` requires them. When governed file scope expands after `context`, append those repo-relative files to `plan.discovered_paths`.
7. Run `verify` before `done` when code, config, contract, onboarding, or behavior changes need deterministic repo checks.
8. Run `review` when you need one named workflow-gate outcome instead of assembling a broader `work` payload.
9. Run `done` with changed files for file-backed work when you know them, or omit / leave `files_changed` empty to let AWM derive the real task delta from the receipt baseline.
10. When resuming or auditing prior work, use direct CLI `awm history`, setting `--entity work` when you need work-specific `--scope` or `--kind` filters, then `awm fetch` the returned `fetch_keys`.

Command boundary:
- `verify` selects zero or more deterministic repo-defined checks from `.awm/awm-tests.yaml`.
- `review` records or runs exactly one named workflow gate from `.awm/awm-workflows.yaml`.

Maintenance note:
- When rules, tags, tests, workflows, onboarding, or tool-surface behavior change, run `awm sync --mode working_tree --insert-new-candidates` and then `awm health --include-details` before `done`.
- `awm health --fix <name>` applies the selected fixer by default.
- Add `--dry-run` to preview health fixes without changing state.
- Use `awm health --fix all` to run the default fixer set explicitly.

## CLI `context` request

```json
{
  "version": "awm.v1",
  "command": "context",
  "request_id": "req-context-001",
  "payload": {
    "project_id": "customer-portal",
    "task_text": "fix the profile settings save race so stale responses cannot overwrite newer edits",
    "phase": "execute"
  }
}
```

Run directly:

```bash
awm context --project customer-portal --task-text "fix the profile settings save race so stale responses cannot overwrite newer edits" --phase execute
```

Optional structured JSON automation:

```bash
awm validate --in assets/requests/context.execute.json
awm run --in assets/requests/context.execute.json
```

## MCP `context` input

```json
{
  "project_id": "customer-portal",
  "task_text": "fix the profile settings save race so stale responses cannot overwrite newer edits",
  "phase": "execute"
}
```

Run:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"context","arguments":'$(cat assets/requests/mcp_context.execute.json)'}}' | awm-mcp
```

## CLI `fetch` request

```json
{
  "version": "awm.v1",
  "command": "fetch",
  "request_id": "req-fetch-001",
  "payload": {
    "project_id": "customer-portal",
    "receipt_id": "replace-from-context-receipt",
    "keys": [
      "customer-portal:web/src/features/profile/ProfileForm.tsx",
      "customer-portal:web/src/features/profile/useSaveProfile.ts",
      "customer-portal:web/src/features/profile/useSaveProfile.test.tsx"
    ]
  }
}
```

Run directly:

```bash
awm fetch --project customer-portal --receipt-id replace-from-context-receipt --key customer-portal:web/src/features/profile/ProfileForm.tsx --key customer-portal:web/src/features/profile/useSaveProfile.ts --key customer-portal:web/src/features/profile/useSaveProfile.test.tsx
```

Optional structured JSON automation:

```bash
awm validate --in assets/requests/fetch.json
awm run --in assets/requests/fetch.json
```

## MCP `fetch` input

```json
{
  "project_id": "customer-portal",
  "receipt_id": "replace-from-context-receipt",
  "keys": [
    "customer-portal:web/src/features/profile/ProfileForm.tsx",
    "customer-portal:web/src/features/profile/useSaveProfile.ts",
    "customer-portal:web/src/features/profile/useSaveProfile.test.tsx"
  ]
}
```

Run:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"fetch","arguments":'$(cat assets/requests/mcp_fetch.json)'}}' | awm-mcp
```

## CLI `work` request

```json
{
  "version": "awm.v1",
  "command": "work",
  "request_id": "req-work-001",
  "payload": {
    "project_id": "customer-portal",
    "receipt_id": "replace-from-context-receipt",
    "mode": "merge",
    "plan": {
      "title": "Profile settings save race",
      "objective": "Prevent stale save responses from overwriting newer settings and verify the fix",
      "kind": "bugfix",
      "status": "in_progress",
      "discovered_paths": [
        "web/src/features/profile/useSaveProfile.test.tsx"
      ],
      "constraints": [
        "Preserve the existing save API contract",
        "Keep optimistic success feedback for the newest request"
      ],
      "references": [
        "customer-portal:web/src/features/profile/ProfileForm.tsx",
        "customer-portal:web/src/features/profile/useSaveProfile.ts"
      ]
    },
    "tasks": [
      {
        "key": "confirm-race-window",
        "summary": "Confirm how concurrent save requests can resolve out of order",
        "status": "complete",
        "references": [
          "customer-portal:web/src/features/profile/useSaveProfile.ts"
        ],
        "outcome": "Older requests can still commit state after a newer submission resolves."
      },
      {
        "key": "guard-stale-saves",
        "summary": "Ignore stale save responses and keep the newest request authoritative",
        "status": "in_progress",
        "depends_on": ["confirm-race-window"],
        "acceptance_criteria": [
          "Submitting profile changes twice quickly leaves the latest values in state",
          "Loading and success UI still resolve correctly for the newest request"
        ],
        "references": [
          "customer-portal:web/src/features/profile/ProfileForm.tsx",
          "customer-portal:web/src/features/profile/useSaveProfile.ts"
        ]
      },
      {
        "key": "verify:tests",
        "summary": "Run targeted frontend verification for profile save behavior",
        "status": "pending",
        "acceptance_criteria": [
          "Profile save regression tests pass",
          "Relevant smoke or build checks pass"
        ]
      }
    ]
  }
}
```

Run directly:

```bash
awm work --project customer-portal --receipt-id replace-from-context-receipt --mode merge --plan-json '{"title":"Profile settings save race","objective":"Prevent stale save responses from overwriting newer settings and verify the fix","kind":"bugfix","status":"in_progress","discovered_paths":["web/src/features/profile/useSaveProfile.test.tsx"]}' --tasks-json '[{"key":"guard-stale-saves","summary":"Ignore stale save responses and keep the newest request authoritative","status":"in_progress"},{"key":"verify:tests","summary":"Run targeted frontend verification for profile save behavior","status":"pending"}]'
```

The example above treats the regression test file as later-discovered governed scope through `plan.discovered_paths`, which is what `review` and `done` validate when work expands beyond the initial receipt.

Optional structured JSON automation:

```bash
awm validate --in assets/requests/work.json
awm run --in assets/requests/work.json
```

If the repo defines a richer feature-plan contract, use the same `work` surface with explicit stages and grouping tasks instead of inventing a separate planning system:

```json
{
  "version": "awm.v1",
  "command": "work",
  "request_id": "req-work-feature-001",
  "payload": {
    "project_id": "customer-portal",
    "receipt_id": "replace-from-context-receipt",
    "mode": "merge",
    "plan": {
      "title": "Offline downloads",
      "kind": "feature",
      "objective": "Ship bounded offline downloads across backend and frontend surfaces.",
      "status": "in_progress",
      "stages": {
        "spec_outline": "complete",
        "refined_spec": "complete",
        "implementation_plan": "in_progress"
      }
    },
    "tasks": [
      {
        "key": "stage:spec-outline",
        "summary": "Spec outline",
        "status": "complete"
      },
      {
        "key": "spec:capabilities",
        "summary": "Define required capabilities",
        "status": "complete",
        "parent_task_key": "stage:spec-outline",
        "acceptance_criteria": [
          "Capabilities cover UX, backend, and operator impact"
        ]
      },
      {
        "key": "stage:implementation-plan",
        "summary": "Implementation plan",
        "status": "in_progress"
      },
      {
        "key": "impl:backend-contract",
        "summary": "Implement the backend contract",
        "status": "in_progress",
        "parent_task_key": "stage:implementation-plan",
        "acceptance_criteria": [
          "Backend behavior and regression coverage are explicit"
        ]
      },
      {
        "key": "verify:tests",
        "summary": "Run verification for feature work",
        "status": "pending"
      }
    ]
  }
}
```

Repos can enforce that schema through a repo-local `verify` script that inspects the active plan via `AWM_PLAN_KEY` / `AWM_RECEIPT_ID` and verify selection metadata via `AWM_VERIFY_PHASE`, `AWM_VERIFY_TAGS_JSON`, and `AWM_VERIFY_FILES_CHANGED_JSON`.

## MCP `work` input

```json
{
  "project_id": "customer-portal",
  "receipt_id": "replace-from-context-receipt",
  "mode": "merge",
  "plan": {
    "title": "Profile settings save race",
    "objective": "Prevent stale save responses from overwriting newer settings and verify the fix",
    "kind": "bugfix",
    "status": "in_progress",
    "discovered_paths": [
      "web/src/features/profile/useSaveProfile.test.tsx"
    ]
  },
  "tasks": [
    {
      "key": "guard-stale-saves",
      "summary": "Ignore stale save responses and keep the newest request authoritative",
      "status": "in_progress"
    },
    {
      "key": "verify:tests",
      "summary": "Run targeted frontend verification for profile save behavior",
      "status": "pending"
    }
  ]
}
```

Run:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"work","arguments":'$(cat assets/requests/mcp_work.json)'}}' | awm-mcp
```

## CLI `review` request

```json
{
  "version": "awm.v1",
  "command": "review",
  "request_id": "req-review-001",
  "payload": {
    "project_id": "customer-portal",
    "receipt_id": "replace-from-context-receipt",
    "run": true
  }
}
```

Run directly:

```bash
awm review --project customer-portal --receipt-id replace-from-context-receipt --run
```

Optional structured JSON automation:

```bash
awm validate --in assets/requests/review.json
awm run --in assets/requests/review.json
```

## MCP `review` input

```json
{
  "project_id": "customer-portal",
  "receipt_id": "replace-from-context-receipt",
  "run": true
}
```

Run:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"review","arguments":'$(cat assets/requests/mcp_review.json)'}}' | awm-mcp
```

`review` is intentionally thin. It lowers to one `work.tasks[]` merge update. Omitted `key`, `summary`, and `status` default to `review:cross-llm`, `Cross-LLM review`, and `complete`. Prefer `run=true` when the repo workflow defines a runnable review gate because manual complete notes do not satisfy runnable gates. Use `status=blocked` plus `blocked_reason` when the review gate is waiting or failed, and reserve manual `status`, `outcome`, `blocked_reason`, and `evidence` fields for non-run mode. Put repo-local reviewer choices such as script arguments, provider selection, model ids, reasoning levels, and the shared `--yolo` high-trust shortcut in the workflow `run.argv` block; for Claude, `--yolo` maps to `--dangerously-skip-permissions`.

When work tasks are present, `done.scope_mode` controls gate behavior: `strict` enforces configured completion tasks from `.awm/awm-workflows.yaml`, and `warn` surfaces warnings. AWM uses the receipt baseline delta as the authoritative file set when it is available. If you also supply `files_changed`, AWM cross-checks that list and surfaces mismatches as violations. When changed files are present and no workflow gates are configured, AWM falls back to `verify:tests`; a detected empty delta behaves like a no-file closure but still honors explicit workflow gates.

## CLI `verify` request

```json
{
  "version": "awm.v1",
  "command": "verify",
  "request_id": "req-verify-001",
  "payload": {
    "project_id": "customer-portal",
    "receipt_id": "replace-from-context-receipt",
    "phase": "review",
    "files_changed": [
      "web/src/features/profile/ProfileForm.tsx",
      "web/src/features/profile/useSaveProfile.ts",
      "web/src/features/profile/useSaveProfile.test.tsx"
    ]
  }
}
```

Run directly:

```bash
awm verify --project customer-portal --receipt-id replace-from-context-receipt --phase review --file-changed web/src/features/profile/ProfileForm.tsx --file-changed web/src/features/profile/useSaveProfile.ts --file-changed web/src/features/profile/useSaveProfile.test.tsx
```

Optional structured JSON automation:

```bash
awm validate --in assets/requests/verify.json
awm run --in assets/requests/verify.json
```

## MCP `verify` input

```json
{
  "project_id": "customer-portal",
  "receipt_id": "replace-from-context-receipt",
  "phase": "review",
  "files_changed": [
    "web/src/features/profile/ProfileForm.tsx",
    "web/src/features/profile/useSaveProfile.ts",
    "web/src/features/profile/useSaveProfile.test.tsx"
  ]
}
```

Run:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"verify","arguments":'$(cat assets/requests/mcp_verify.json)'}}' | awm-mcp
```

## CLI `done` request

```json
{
  "version": "awm.v1",
  "command": "done",
  "request_id": "req-done-001",
  "payload": {
    "project_id": "customer-portal",
    "receipt_id": "replace-from-context-receipt",
    "files_changed": [
      "web/src/features/profile/ProfileForm.tsx",
      "web/src/features/profile/useSaveProfile.ts",
      "web/src/features/profile/useSaveProfile.test.tsx"
    ],
    "outcome": "Ignored stale profile save responses, preserved optimistic UI feedback, and added regression coverage."
  }
}
```

Run directly:

```bash
awm done --project customer-portal --receipt-id replace-from-context-receipt --file-changed web/src/features/profile/ProfileForm.tsx --file-changed web/src/features/profile/useSaveProfile.ts --file-changed web/src/features/profile/useSaveProfile.test.tsx --outcome "Ignored stale profile save responses, preserved optimistic UI feedback, and added regression coverage."
```

Optional structured JSON automation:

```bash
awm validate --in assets/requests/done.json
awm run --in assets/requests/done.json
```

## MCP `done` input

```json
{
  "project_id": "customer-portal",
  "receipt_id": "replace-from-context-receipt",
  "files_changed": [
    "web/src/features/profile/ProfileForm.tsx",
    "web/src/features/profile/useSaveProfile.ts",
    "web/src/features/profile/useSaveProfile.test.tsx"
  ],
  "outcome": "Ignored stale profile save responses, preserved optimistic UI feedback, and added regression coverage."
}
```

Run:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"done","arguments":'$(cat assets/requests/mcp_done.json)'}}' | awm-mcp
```

When the detected task delta is empty, the closeout is effectively no-file:

```json
{
  "version": "awm.v1",
  "command": "done",
  "request_id": "req-done-no-files-001",
  "payload": {
    "project_id": "customer-portal",
    "receipt_id": "replace-from-context-receipt",
    "outcome": "Drafted the rollout plan, recorded follow-up review work, and closed the no-file planning task."
  }
}
```

## CLI `awm history` request

```json
{
  "version": "awm.v1",
  "command": "history",
  "request_id": "req-history-001",
  "payload": {
    "project_id": "customer-portal",
    "entity": "all",
    "query": "profile save",
    "limit": 20
  }
}
```

Run directly:

```bash
awm history --project customer-portal --entity all --query "profile save" --limit 20
```

Optional structured JSON automation:

```bash
awm validate --in assets/requests/history.json
awm run --in assets/requests/history.json
```

Convenience CLI equivalents:

```bash
awm history --project customer-portal --entity work --scope current
awm history --project customer-portal --entity work --query "profile save"
awm history --project customer-portal --entity work --query "profile save" --scope completed --kind bugfix
awm history --project customer-portal --entity all --query "profile save" --limit 20
```

## CLI `export` request

`export` is an advanced backend-only surface for rendering stable JSON or Markdown artifacts from AWM-owned data. It is not part of the normal agent task loop — use `context`, `fetch`, `history`, or `status` for day-to-day work. Use `export` when you specifically need a rendered artifact file.

```json
{
  "version": "awm.v1",
  "command": "export",
  "request_id": "req-export-001",
  "payload": {
    "project_id": "customer-portal",
    "format": "markdown",
    "fetch": {
      "receipt_id": "replace-from-context-receipt"
    }
  }
}
```

Run via structured JSON automation:

```bash
awm run --in assets/requests/export.json
```

For ad hoc CLI rendering, the read-oriented convenience surfaces also accept `--format` and `--out-file`:

```bash
awm context --task-text "continue work" --phase execute --format markdown --out-file artifacts/context.md --force
awm fetch --receipt-id replace-from-context-receipt --format json --out-file artifacts/fetch.json --force
awm history --entity work --scope current --format markdown --out-file artifacts/history.md --force
awm status --format json --out-file artifacts/status.json --force
```

These convenience flags lower into the backend `export` command under the hood.

## MCP `export` input

```json
{
  "project_id": "customer-portal",
  "format": "markdown",
  "fetch": {
    "receipt_id": "replace-from-context-receipt"
  }
}
```

Run:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"export","arguments":'$(cat assets/requests/mcp_export.json)'}}' | awm-mcp
```
