# AWM Feature Plans

This file defines the richer feature planning contract that this repo layers on top of AWM's built-in plan and task schema.

AWM remains the system of record for live plans and task state. This document makes the expected structure explicit in version control so spec maturity, refined specs, implementation outlines, and atomic tasks are visible outside work storage.

## When It Applies

Use this contract for net-new feature work and large capability expansions.

Do not force it onto small bugfixes, narrow maintenance tasks, review-only work, or workflow-governance changes. Those can keep using thinner AWM plans.

## Root Feature Plan

Create one root AWM plan with:

- `kind=feature`
- `objective`
- `in_scope`
- `out_of_scope`
- `constraints`
- `references`
- `stages.spec_outline`
- `stages.refined_spec`
- `stages.implementation_plan`

The root plan should describe the whole feature, not one implementation slice.

## Root Task Shape

Root feature plans must include these top-level tasks:

- `stage:spec-outline`
- `stage:refined-spec`
- `stage:implementation-plan`
- `verify:tests`

The three `stage:*` tasks are grouping tasks. They stay top-level and do not set `parent_task_key`.
Their task status should mirror the corresponding plan stage status.

Place concrete planning or implementation tasks underneath them with `parent_task_key`. Example child task keys:

- `spec:selection-contract`
- `spec:review-gate-behavior`
- `impl:cli-surface`
- `impl:mcp-surface`

## Atomic Tasks

Atomic tasks in this contract are leaf tasks: tasks with no children.

- Leaf tasks should be small enough to complete, block, or hand off independently.
- Leaf tasks must carry explicit `acceptance_criteria`.
- `depends_on` should describe real ordering constraints between leaf tasks, not broad stage relationships.
- Grouping tasks such as `stage:*` are not atomic tasks.

## Feature Streams

If a feature splits into parallel workstreams, create child plans with:

- `kind=feature_stream`
- `parent_plan_key=<root feature plan key>`
- their own `objective`
- their own `in_scope`
- their own `out_of_scope`
- their own `references`
- their own task list, including `verify:tests`

The root feature plan holds the whole feature contract. Child stream plans hold bounded slices of execution.

## Example

Root feature plan:

```bash
awm work --project <project-id> --receipt-id <feature-receipt-id> --mode merge   --plan-json '{
    "title":"History search export surface",
    "kind":"feature",
    "objective":"Add a first-class export surface for work, receipt, and run history without weakening current scope guarantees.",
    "status":"in_progress",
    "stages":{
      "spec_outline":"complete",
      "refined_spec":"complete",
      "implementation_plan":"in_progress"
    },
    "in_scope":["CLI export command", "MCP parity", "history run summaries"],
    "out_of_scope":["new storage backend", "UI dashboard"],
    "constraints":["Keep existing history-discovery payloads backward compatible"],
    "references":["README.md", "internal/service/backend/history.go", "cmd/awm/routes.go"]
  }'   --tasks-json '[
    {"key":"stage:spec-outline","summary":"Spec outline","status":"complete"},
    {"key":"spec:export-capabilities","summary":"Define required export capabilities","status":"complete","parent_task_key":"stage:spec-outline","acceptance_criteria":["Capabilities cover CLI, MCP, and done impact"]},
    {"key":"stage:refined-spec","summary":"Refined spec","status":"complete"},
    {"key":"spec:scope-constraints","summary":"Define scope and compatibility boundaries","status":"complete","parent_task_key":"stage:refined-spec","acceptance_criteria":["Scope guarantees and compatibility limits are explicit"]},
    {"key":"stage:implementation-plan","summary":"Implementation plan","status":"in_progress"},
    {"key":"impl:cli-surface","summary":"Add CLI export surface","status":"in_progress","parent_task_key":"stage:implementation-plan","acceptance_criteria":["CLI help, routes, and tests cover the new export surface"]},
    {"key":"impl:mcp-surface","summary":"Add MCP export parity","status":"pending","parent_task_key":"stage:implementation-plan","depends_on":["impl:cli-surface"],"acceptance_criteria":["MCP definitions and contract tests stay aligned with the CLI surface"]},
    {"key":"verify:tests","summary":"Run verification for feature work","status":"pending"}
  ]'
```

Child stream plan:

```bash
awm work --project <project-id> --receipt-id <stream-receipt-id> --mode merge   --plan-json '{
    "title":"History search export surface - MCP stream",
    "kind":"feature_stream",
    "parent_plan_key":"plan:<feature-receipt-id>",
    "objective":"Implement the MCP slice of the history export surface.",
    "status":"in_progress",
    "in_scope":["MCP tool definition", "MCP invoke coverage"],
    "out_of_scope":["CLI command help"],
    "references":["cmd/awm-mcp/main.go", "internal/adapters/mcp"]
  }'   --tasks-json '[
    {"key":"impl:mcp-tool","summary":"Expose the MCP export tool","status":"in_progress","acceptance_criteria":["Tool schema and invoke wiring are implemented"]},
    {"key":"impl:mcp-tests","summary":"Add MCP contract coverage","status":"pending","depends_on":["impl:mcp-tool"],"acceptance_criteria":["MCP invoke tests cover the export payload and response"]},
    {"key":"verify:tests","summary":"Run verification for the stream","status":"pending"}
  ]'
```

## Verification

Use `awm verify` with the active receipt or plan context so the repo-local validator can inspect the live plan:

```bash
awm verify --project <project-id> --receipt-id <receipt-id> --phase review --file-changed src/main.ts
```

That verify check executes `scripts/awm-feature-plan-validate.py` with the active receipt or plan context.

For plans in this feature schema, it fails when required metadata, stage grouping, hierarchy links, `verify:tests`, or leaf-task acceptance criteria are missing. For non-feature plans, or receipt contexts that do not materialize a concrete plan, the script exits cleanly and the gate is not selected as a blocker.

## Completion Gates

Keep feature-plan shape enforcement in `verify`, not `.awm/awm-workflows.yaml`.

- `.awm/awm-workflows.yaml` remains responsible for completion gates such as `verify:tests` and runnable review tasks.
- The feature-plan validator acts as an additional verify-time gate for richer planning discipline.
- If a future workflow needs a feature-specific final gate, add it carefully: workflow selectors do not currently distinguish thin plans from `kind=feature` plans by themselves.
