# AWM Agent Workflow — agent-workflow-manager

This extends [AGENTS.md](../AGENTS.md) with the AWM-managed workflow for agents that have `awm` available. All invariants and routing from AGENTS.md still apply.

See [awm-work-loop.md](awm-work-loop.md) for the full command reference.

## Source Of Truth

- Canonical rules: `.awm/awm-rules.yaml`
- Canonical tags: `.awm/awm-tags.yaml`
- Canonical verification: `.awm/awm-tests.yaml`
- Canonical workflow gates: `.awm/awm-workflows.yaml`
- `docs/examples/` contains generic starter templates, not this repo's contract.

## Fast Path

1. Read `AGENTS.md` and any tool-specific companion (e.g. `CLAUDE.md`).
2. If you need orientation before reading code, run `awm status --project agent-workflow-manager --task-text "<task>" --phase <plan|execute|review>`.
3. For non-trivial work (multi-step, multi-file, or governed), run `awm context --project agent-workflow-manager --task-text "<task>" --phase <plan|execute|review>`. Trivial single-file fixes can skip the AWM ceremony.
4. Follow the returned hard rules. Use `awm fetch` only for keys you need.
5. Run `awm work` for multi-step, multi-file, handoff-prone, or governed-scope-expanding work.
6. Run `awm verify` for code, config, contract, onboarding, or behavior changes.
7. Run `awm review --run` when `.awm/awm-workflows.yaml` requires a runnable review gate.
8. Run `awm done` for non-trivial work.

If `awm` is not on `PATH`, bootstrap the toolchain before continuing:

```bash
# 1. Ensure Go is available (requires sudo for /usr/local install)
if ! command -v go &>/dev/null; then
  curl -fsSL https://go.dev/dl/go1.26.0.linux-amd64.tar.gz -o /tmp/go.tar.gz \
    && sudo tar -C /usr/local -xzf /tmp/go.tar.gz \
    && rm /tmp/go.tar.gz
  export PATH="/usr/local/go/bin:$PATH"
fi

# 2. Build and install awm binaries from this repo
if ! command -v awm &>/dev/null; then
  go build -o /tmp/awm ./cmd/awm \
    && go build -o /tmp/awm-mcp ./cmd/awm-mcp \
    && sudo mv /tmp/awm /tmp/awm-mcp /usr/local/bin/
fi
```

Do not substitute `go run ./cmd/awm` unless you are explicitly testing source-build behavior.

When changing rules, tags, tests, workflows, onboarding, or tool-surface behavior, run `awm sync --project agent-workflow-manager --mode working_tree --insert-new-candidates` and `awm health --project agent-workflow-manager --include-details` before `done`.

## AWM-Specific Working Norms

- Do not silently widen governed scope. Re-run `context` or declare later-discovered files through `work.plan.discovered_paths`.
- `awm health` and `awm status` warnings about stale plans, plan-status drift, or plans left open only for administrative closeout are bookkeeping regressions. Clean them up before `done` when they are part of your task.
- Governed multi-step work in this repo uses the staged plan contract in `docs/feature-plans.md`, not thin ad hoc task lists.
- Governed root plans must always carry `spec_outline`, `refined_spec`, and `implementation_plan`; do not start implementation with only a vague root objective.
- The root plan owner acts as the orchestrator for multi-file or multi-step work: keep the whole-plan spec, scope, verification, review, and closeout there; keep leaf tasks narrow enough for low-context execution.
- When the runtime supports sub-agents, prefer delegating bounded leaf tasks so the orchestrator keeps the full-plan context. When it does not, execute the same leaf tasks sequentially and return to the root plan between them.
- Keep leaf tasks so tight that an assignee can succeed from the listed `references`, `acceptance_criteria`, and `depends_on` edges without inventing missing scope.
- If governed scope expands, declare new files through `awm work` before `awm review` or `awm done`.

## Skill Aliases

Tools with the AWM skill pack installed expose these shorthand commands:

| AWM CLI | Skill alias |
|---|---|
| `awm context` | `/awm-context [phase] <task>` |
| `awm work` | `/awm-work` |
| `awm verify` | `/awm-verify` |
| `awm review --run` | `/awm-review <id> {"run":true}` |
| `awm done` | `/awm-done` |

Direct CLI (`awm sync`, `awm health`, `awm history`, `awm status`) has no skill aliases — call those directly.
