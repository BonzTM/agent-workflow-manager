# AWM Staged Plans

Despite the filename, this document defines the staged planning contract for governed multi-step work in `agent-workflow-manager`, not only net-new features.
AWM the product stays generic; this repo requires staged plans for governed multi-step work.

AWM remains the system of record for active plans and task state. This document makes the expected structure explicit in the repo so spec maturity, refined specs, implementation outlines, orchestration boundaries, and atomic execution tasks are visible in version control instead of living only in work storage.

## When It Applies

Use this contract for any multi-file or multi-step work in this repo, including:

- net-new feature work
- maintenance and refactors
- workflow and governance changes
- migrations or parity-sensitive rewrites

Thin plans remain acceptable only for:

- single-file, single-purpose fixes
- review-only notes
- no-file research or analysis tasks

Thin-plan exemption is by shape, not by intent. A thin plan stays exempt only when it remains a single bounded non-gate task with no stage metadata, no task hierarchy, and no child plans.

## Root Plan Kinds

Governed root plans use one of:

- `kind=feature`
- `kind=maintenance`
- `kind=governance`

The root plan describes the whole body of work, not one implementation slice.

## Root Plan Metadata

Every governed root plan must include:

- `title`
- `objective`
- `in_scope`
- `out_of_scope`
- `constraints`
- `references`
- `stages.spec_outline`
- `stages.refined_spec`
- `stages.implementation_plan`

Treat the root plan owner as the orchestrator for the whole effort. The orchestrator owns the objective, refined scope, dependency ordering, discovered paths, verification state, review state, and closeout.

## Root Task Shape

Governed root plans must include these top-level tasks:

- `stage:spec-outline`
- `stage:refined-spec`
- `stage:implementation-plan`
- `verify:tests`

The three `stage:*` tasks are grouping tasks. They stay top-level and do not set `parent_task_key`.
Their task status should mirror the corresponding plan stage status.
When AWM auto-closes a plan into a terminal status, it also reconciles those plan stage fields from the matching `stage:*` task statuses so completed plans do not retain stale stage metadata.

Add workflow-gate tasks such as `review:cross-llm` when the current work will need them, but the staged plan contract itself does not hardcode one review key.
Keep gate tasks top-level. Do not place `verify:tests` or review gates under any `stage:*` task.

## Stage Intent

- `stage:spec-outline` captures the problem, user-visible intent, invariants, and first-pass scope boundaries.
- `stage:refined-spec` turns the broad spec into concrete edit slices, dependency boundaries, and proof obligations.
- `stage:implementation-plan` contains only execution tasks that are small enough for low-context implementation or delegation.

Task-family prefixes are mandatory under each stage:

- children of `stage:spec-outline` use `spec:*`
- children of `stage:refined-spec` use `refine:*`
- children of `stage:implementation-plan` use `impl:*` or `tdd:*`

## Atomic Leaf Tasks

Atomic tasks in this contract are leaf tasks: tasks with no children.

Every non-gate leaf task must:

- be a direct child of one `stage:*` task
- include explicit `references` to the exact files, entrypoints, or docs the assignee starts from
- include explicit `acceptance_criteria`
- include at least 2 `acceptance_criteria`: one output criterion and one proof criterion
- stay small enough that another agent can execute it with minimal inference

In practice that means:

- no "misc cleanup", "wire remaining pieces", or "polish" leaf tasks
- no intermediary grouping tasks between a `stage:*` task and the executable leaf
- one bounded behavior or one tightly-coupled file set per leaf task
- 1-3 exact starting references per leaf
- `depends_on` only for real ordering constraints between leaf tasks
- acceptance criteria should state both the expected output and how the assignee proves the task is done

## Orchestration And Delegation

- The root plan owner is the orchestrator.
- The orchestrator keeps the whole-plan context and should not disappear into leaf-task implementation details.
- When the runtime supports sub-agents, delegate atomic leaf tasks to them so the orchestrator retains the root-plan context window.
- When sub-agents are unavailable, simulate the same pattern by executing one leaf task at a time and returning to the root plan before starting the next one.
- Use child stream plans only when a delegated lane needs its own durable plan state. This repo currently only standardizes `kind=feature_stream` for feature roots.

## Example

Root governance plan:

```bash
awm work --project agent-workflow-manager --receipt-id <receipt-id> --mode merge \
  --plan-json '{
    "title":"Review/verify boundary clarification",
    "kind":"governance",
    "objective":"Make verify versus review behavior explicit and tighten the repo-local planning contract for governed work.",
    "status":"in_progress",
    "stages":{
      "spec_outline":"complete",
      "refined_spec":"complete",
      "implementation_plan":"in_progress"
    },
    "in_scope":["command-boundary docs", "repo-local staged-plan rules", "validator enforcement"],
    "out_of_scope":["new product command", "bootstrap template rewrite"],
    "constraints":["Keep TDD and orchestration policy repo-local unless a product capability is intentionally generic", "Do not let docs drift from runnable repo behavior"],
    "references":["README.md", "docs/getting-started.md", "docs/feature-plans.md", "scripts/awm-feature-plan-validate.py"]
  }' \
  --tasks-json '[
    {"key":"stage:spec-outline","summary":"Spec outline","status":"complete"},
    {"key":"spec:verify-review-boundary","summary":"Define the behavioral boundary between verify and review","status":"complete","parent_task_key":"stage:spec-outline","references":["README.md","docs/getting-started.md"],"acceptance_criteria":["The distinction states when verify is required versus when review is required","The scope of each command is explicit enough that another agent does not infer missing semantics"]},
    {"key":"stage:refined-spec","summary":"Refined spec","status":"complete"},
    {"key":"refine:atomic-task-shape","summary":"Define the exact requirements for atomic leaf tasks","status":"complete","parent_task_key":"stage:refined-spec","references":["docs/feature-plans.md",".awm/awm-rules.yaml"],"acceptance_criteria":["Leaf-task requirements cover references, acceptance criteria, and bounded scope","The contract is strict enough for low-context delegation without inventing new task-schema fields"]},
    {"key":"stage:implementation-plan","summary":"Implementation plan","status":"in_progress"},
    {"key":"impl:validator-rules","summary":"Enforce the staged-plan contract in the repo-local validator","status":"pending","parent_task_key":"stage:implementation-plan","depends_on":["refine:atomic-task-shape"],"references":["scripts/awm-feature-plan-validate.py","scripts/awm_feature_plan_validate_test.go"],"acceptance_criteria":["Validator rejects governed plans that omit stages, stage tasks, leaf references, or leaf acceptance criteria","Tests cover at least one passing governed plan and one failing leaf-task case"]},
    {"key":"impl:readme-boundary","summary":"Update the README command boundary guidance","status":"pending","parent_task_key":"stage:implementation-plan","depends_on":["spec:verify-review-boundary"],"references":["README.md"],"acceptance_criteria":["README explains verify versus review without ambiguity","The recommended closeout sequence is explicit"]},
    {"key":"impl:getting-started-boundary","summary":"Update the getting-started command boundary guidance","status":"pending","parent_task_key":"stage:implementation-plan","depends_on":["spec:verify-review-boundary"],"references":["docs/getting-started.md"],"acceptance_criteria":["Getting-started explains when to use verify versus review","The guide keeps product behavior distinct from this repo's maintainer policy"]},
    {"key":"impl:maintainer-orchestration-docs","summary":"Update maintainer docs for the orchestrator and delegated-leaf-task model","status":"pending","parent_task_key":"stage:implementation-plan","depends_on":["refine:atomic-task-shape"],"references":["AGENTS.md","docs/maintainer-reference.md"],"acceptance_criteria":["Maintainer docs explain that the root plan owner is the orchestrator","Maintainer docs require leaf tasks that low-context agents can execute directly"]},
    {"key":"verify:tests","summary":"Run verification for the governed plan","status":"pending"}
  ]'
```

## Verification

Use `awm verify` with the active receipt or plan context so the repo-local validator can inspect the live plan:

```bash
awm verify --project agent-workflow-manager --receipt-id <receipt-id> --phase review --file-changed scripts/awm-feature-plan-validate.py
```

That verify check executes `scripts/awm-feature-plan-validate.py` with the active receipt or plan context.

For governed plans in this schema, it fails when required metadata, stage grouping, hierarchy links, `verify:tests`, leaf-task `references`, or leaf-task `acceptance_criteria` are missing. It also rejects materially planned work that still uses an unsupported or unspecified `kind`.

## Completion Gates

This repo keeps staged-plan shape enforcement in `verify`, not `.awm/awm-workflows.yaml`.

- `.awm/awm-workflows.yaml` remains responsible for completion gates such as `verify:tests` and runnable review tasks.
- The staged-plan validator acts as an additional verify-time gate for planning discipline.
- `review` still handles named workflow signoff gates; it is not a substitute for the staged planning contract.
