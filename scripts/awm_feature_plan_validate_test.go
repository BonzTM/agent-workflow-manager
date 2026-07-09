package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func cleanAWMEnv(overrides ...string) []string {
	base := os.Environ()
	filtered := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		switch {
		case strings.HasPrefix(entry, "AWM_PLAN_KEY="),
			strings.HasPrefix(entry, "AWM_RECEIPT_ID="),
			strings.HasPrefix(entry, "AWM_PROJECT_ID="):
			continue
		default:
			filtered = append(filtered, entry)
		}
	}
	filtered = append(filtered, overrides...)
	return filtered
}

func runFeaturePlanValidator(t *testing.T, awmScript string) (string, error) {
	t.Helper()

	tempRoot := t.TempDir()
	binDir := filepath.Join(tempRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	writeExecutable(t, filepath.Join(binDir, "awm"), awmScript)

	env := []string{
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"AWM_PROJECT_ID=agent-workflow-manager",
		"AWM_RECEIPT_ID=receipt-test",
	}

	cmd := exec.Command("python3", "scripts/awm-feature-plan-validate.py")
	cmd.Dir = repoRoot(t)
	cmd.Env = cleanAWMEnv(env...)

	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestAWMFeaturePlanValidateSkipsWhenReceiptPlanHasNoContent(t *testing.T) {
	t.Parallel()

	output, err := runFeaturePlanValidator(t, `#!/usr/bin/env bash
set -euo pipefail
cat <<'JSON'
{"result":{"items":[{"content":""}]}}
JSON
`)
	if err != nil {
		t.Fatalf("expected skip for unmaterialized receipt plan, got error %v\n%s", err, output)
	}
	if !strings.Contains(output, "skip - active plan plan:receipt-test has no materialized content") {
		t.Fatalf("expected unmaterialized-plan skip message, got %s", output)
	}
}

func TestAWMFeaturePlanValidateStillFailsWhenFeatureParentPlanHasNoContent(t *testing.T) {
	t.Parallel()

	output, err := runFeaturePlanValidator(t, `#!/usr/bin/env bash
set -euo pipefail
plan_key="${@: -1}"
case "${plan_key}" in
  plan:receipt-test)
    cat <<'JSON'
{"result":{"items":[{"content":"{\"kind\":\"feature_stream\",\"parent_plan_key\":\"plan:root\",\"title\":\"Stream\",\"objective\":\"Validate stream\",\"in_scope\":[\"scope\"],\"out_of_scope\":[\"other\"],\"references\":[\"ref\"],\"tasks\":[{\"key\":\"verify:tests\",\"summary\":\"Verify\"},{\"key\":\"impl:leaf\",\"summary\":\"Implement stream slice\",\"acceptance_criteria\":[\"A concrete output exists\",\"The result is proved\"],\"references\":[\"internal/service/backend/review.go\"]}]}"}]}}
JSON
    ;;
  plan:root)
    cat <<'JSON'
{"result":{"items":[{"content":""}]}}
JSON
    ;;
  *)
    echo "unexpected plan key: ${plan_key}" >&2
    exit 1
    ;;
esac
`)
	if err == nil {
		t.Fatalf("expected failure when a feature plan parent is unmaterialized, output=%s", output)
	}
	if !strings.Contains(output, "fetched plan plan:root had empty content") {
		t.Fatalf("expected strict parent-plan failure, got %s", output)
	}
}

func TestAWMFeaturePlanValidateAcceptsMaintenancePlanWithLeafReferences(t *testing.T) {
	t.Parallel()

	output, err := runFeaturePlanValidator(t, `#!/usr/bin/env bash
set -euo pipefail
cat <<'JSON'
{"result":{"items":[{"content":"{\"kind\":\"maintenance\",\"title\":\"Tight staged maintenance plan\",\"objective\":\"Refine repo planning discipline\",\"in_scope\":[\"rules\",\"docs\"],\"out_of_scope\":[\"runtime behavior\"],\"constraints\":[\"Keep the contract repo-local\"],\"references\":[\"docs/feature-plans.md\",\"scripts/awm-feature-plan-validate.py\"],\"stages\":{\"spec_outline\":\"complete\",\"refined_spec\":\"complete\",\"implementation_plan\":\"in_progress\"},\"tasks\":[{\"key\":\"stage:spec-outline\",\"summary\":\"Spec outline\",\"status\":\"complete\"},{\"key\":\"spec:contract-boundary\",\"summary\":\"Define the planning boundary\",\"status\":\"complete\",\"parent_task_key\":\"stage:spec-outline\",\"references\":[\"docs/feature-plans.md\"],\"acceptance_criteria\":[\"The contract states when staged plans are mandatory\",\"The scope boundary is explicit enough for another agent to follow\"]},{\"key\":\"stage:refined-spec\",\"summary\":\"Refined spec\",\"status\":\"complete\"},{\"key\":\"refine:validator-rules\",\"summary\":\"Define validator expectations\",\"status\":\"complete\",\"parent_task_key\":\"stage:refined-spec\",\"references\":[\"scripts/awm-feature-plan-validate.py\"],\"acceptance_criteria\":[\"Leaf-task requirements are mechanically checkable\",\"Stage ownership rules are explicit\"]},{\"key\":\"stage:implementation-plan\",\"summary\":\"Implementation plan\",\"status\":\"in_progress\"},{\"key\":\"impl:docs-rules\",\"summary\":\"Update docs and rules\",\"status\":\"in_progress\",\"parent_task_key\":\"stage:implementation-plan\",\"references\":[\".awm/awm-rules.yaml\",\"docs/feature-plans.md\"],\"acceptance_criteria\":[\"The staged plan contract is documented\",\"The repo-local rules match the documented contract\"]},{\"key\":\"verify:tests\",\"summary\":\"Run verification\",\"status\":\"pending\"}]}"}]}}
JSON
`)
	if err != nil {
		t.Fatalf("expected maintenance plan to validate, got %v\n%s", err, output)
	}
	if !strings.Contains(output, "validated staged plan hierarchy") {
		t.Fatalf("expected staged-plan success message, got %s", output)
	}
}

func TestAWMFeaturePlanValidateAcceptsAlternateTopLevelReviewGate(t *testing.T) {
	t.Parallel()

	output, err := runFeaturePlanValidator(t, `#!/usr/bin/env bash
set -euo pipefail
cat <<'JSON'
{"result":{"items":[{"content":"{\"kind\":\"governance\",\"title\":\"Alternate review gate\",\"objective\":\"Allow any top-level review gate key\",\"in_scope\":[\"rules\",\"docs\"],\"out_of_scope\":[\"runtime behavior\"],\"constraints\":[\"Keep the contract repo-local\"],\"references\":[\"docs/feature-plans.md\",\"scripts/awm-feature-plan-validate.py\"],\"stages\":{\"spec_outline\":\"complete\",\"refined_spec\":\"complete\",\"implementation_plan\":\"complete\"},\"tasks\":[{\"key\":\"stage:spec-outline\",\"summary\":\"Spec outline\",\"status\":\"complete\"},{\"key\":\"spec:contract-boundary\",\"summary\":\"Define the planning boundary\",\"status\":\"complete\",\"parent_task_key\":\"stage:spec-outline\",\"references\":[\"docs/feature-plans.md\"],\"acceptance_criteria\":[\"The contract states when staged plans are mandatory\",\"The scope boundary is explicit enough for another agent to follow\"]},{\"key\":\"stage:refined-spec\",\"summary\":\"Refined spec\",\"status\":\"complete\"},{\"key\":\"refine:validator-rules\",\"summary\":\"Define validator expectations\",\"status\":\"complete\",\"parent_task_key\":\"stage:refined-spec\",\"references\":[\"scripts/awm-feature-plan-validate.py\"],\"acceptance_criteria\":[\"Leaf-task requirements are mechanically checkable\",\"Stage ownership rules are explicit\"]},{\"key\":\"stage:implementation-plan\",\"summary\":\"Implementation plan\",\"status\":\"complete\"},{\"key\":\"impl:docs-rules\",\"summary\":\"Update docs and rules\",\"status\":\"complete\",\"parent_task_key\":\"stage:implementation-plan\",\"references\":[\".awm/awm-rules.yaml\",\"docs/feature-plans.md\"],\"acceptance_criteria\":[\"The staged plan contract is documented\",\"The repo-local rules match the documented contract\"]},{\"key\":\"verify:tests\",\"summary\":\"Run verification\",\"status\":\"complete\"},{\"key\":\"review:security\",\"summary\":\"Security review gate\",\"status\":\"pending\"}]}"}]}}
JSON
`)
	if err != nil {
		t.Fatalf("expected alternate review gate to validate, got %v\n%s", err, output)
	}
	if !strings.Contains(output, "validated staged plan hierarchy") {
		t.Fatalf("expected staged-plan success message, got %s", output)
	}
}

func TestAWMFeaturePlanValidateRejectsLeafWithoutReferences(t *testing.T) {
	t.Parallel()

	output, err := runFeaturePlanValidator(t, `#!/usr/bin/env bash
set -euo pipefail
cat <<'JSON'
{"result":{"items":[{"content":"{\"kind\":\"governance\",\"title\":\"Broken staged plan\",\"objective\":\"Show the validator failure\",\"in_scope\":[\"rules\"],\"out_of_scope\":[\"product behavior\"],\"constraints\":[\"Keep the failure focused\"],\"references\":[\"docs/feature-plans.md\"],\"stages\":{\"spec_outline\":\"complete\",\"refined_spec\":\"complete\",\"implementation_plan\":\"in_progress\"},\"tasks\":[{\"key\":\"stage:spec-outline\",\"summary\":\"Spec outline\",\"status\":\"complete\"},{\"key\":\"spec:contract-boundary\",\"summary\":\"Define the planning boundary\",\"status\":\"complete\",\"parent_task_key\":\"stage:spec-outline\",\"acceptance_criteria\":[\"The contract states when staged plans are mandatory\",\"Another agent can tell what to do next\"]},{\"key\":\"stage:refined-spec\",\"summary\":\"Refined spec\",\"status\":\"complete\"},{\"key\":\"refine:validator-rules\",\"summary\":\"Define validator expectations\",\"status\":\"complete\",\"parent_task_key\":\"stage:refined-spec\",\"references\":[\"scripts/awm-feature-plan-validate.py\"],\"acceptance_criteria\":[\"Leaf-task requirements are mechanically checkable\",\"Stage ownership rules are explicit\"]},{\"key\":\"stage:implementation-plan\",\"summary\":\"Implementation plan\",\"status\":\"in_progress\"},{\"key\":\"impl:docs-rules\",\"summary\":\"Update docs and rules\",\"status\":\"in_progress\",\"parent_task_key\":\"stage:implementation-plan\",\"references\":[\".awm/awm-rules.yaml\"],\"acceptance_criteria\":[\"The staged plan contract is documented\",\"The repo-local rules match the contract\"]},{\"key\":\"verify:tests\",\"summary\":\"Run verification\",\"status\":\"pending\"}]}"}]}}
JSON
`)
	if err == nil {
		t.Fatalf("expected staged plan without references to fail, output=%s", output)
	}
	if !strings.Contains(output, "leaf task spec:contract-boundary must include references") {
		t.Fatalf("expected missing-reference failure, got %s", output)
	}
}

func TestAWMFeaturePlanValidateRejectsUnsupportedKindForMultiStepPlan(t *testing.T) {
	t.Parallel()

	output, err := runFeaturePlanValidator(t, `#!/usr/bin/env bash
set -euo pipefail
cat <<'JSON'
{"result":{"items":[{"content":"{\"kind\":\"bugfix\",\"title\":\"Unsupported kind\",\"objective\":\"Show unsupported kind failure\",\"tasks\":[{\"key\":\"stage:spec-outline\",\"summary\":\"Spec outline\"},{\"key\":\"spec:contract-boundary\",\"summary\":\"Define the planning boundary\",\"parent_task_key\":\"stage:spec-outline\",\"references\":[\"docs/feature-plans.md\"],\"acceptance_criteria\":[\"The output is described\",\"The proof is described\"]},{\"key\":\"stage:refined-spec\",\"summary\":\"Refined spec\"},{\"key\":\"refine:validator-rules\",\"summary\":\"Define validator expectations\",\"parent_task_key\":\"stage:refined-spec\",\"references\":[\"scripts/awm-feature-plan-validate.py\"],\"acceptance_criteria\":[\"The output is described\",\"The proof is described\"]},{\"key\":\"stage:implementation-plan\",\"summary\":\"Implementation plan\"},{\"key\":\"impl:docs-rules\",\"summary\":\"Update docs and rules\",\"parent_task_key\":\"stage:implementation-plan\",\"references\":[\".awm/awm-rules.yaml\"],\"acceptance_criteria\":[\"The output is described\",\"The proof is described\"]},{\"key\":\"verify:tests\",\"summary\":\"Run verification\"}],\"stages\":{\"spec_outline\":\"pending\",\"refined_spec\":\"pending\",\"implementation_plan\":\"pending\"}}"}]}}
JSON
`)
	if err == nil {
		t.Fatalf("expected unsupported kind to fail, output=%s", output)
	}
	if !strings.Contains(output, `uses kind "bugfix" but does not match the thin-plan exemption`) {
		t.Fatalf("expected unsupported-kind failure, got %s", output)
	}
}

func TestAWMFeaturePlanValidateRejectsUnspecifiedKindForMultiStepPlan(t *testing.T) {
	t.Parallel()

	output, err := runFeaturePlanValidator(t, `#!/usr/bin/env bash
set -euo pipefail
cat <<'JSON'
{"result":{"items":[{"content":"{\"title\":\"Missing kind\",\"objective\":\"Show missing kind failure\",\"tasks\":[{\"key\":\"stage:spec-outline\",\"summary\":\"Spec outline\"},{\"key\":\"spec:contract-boundary\",\"summary\":\"Define the planning boundary\",\"parent_task_key\":\"stage:spec-outline\",\"references\":[\"docs/feature-plans.md\"],\"acceptance_criteria\":[\"The output is described\",\"The proof is described\"]},{\"key\":\"stage:refined-spec\",\"summary\":\"Refined spec\"},{\"key\":\"refine:validator-rules\",\"summary\":\"Define validator expectations\",\"parent_task_key\":\"stage:refined-spec\",\"references\":[\"scripts/awm-feature-plan-validate.py\"],\"acceptance_criteria\":[\"The output is described\",\"The proof is described\"]},{\"key\":\"stage:implementation-plan\",\"summary\":\"Implementation plan\"},{\"key\":\"impl:docs-rules\",\"summary\":\"Update docs and rules\",\"parent_task_key\":\"stage:implementation-plan\",\"references\":[\".awm/awm-rules.yaml\"],\"acceptance_criteria\":[\"The output is described\",\"The proof is described\"]},{\"key\":\"verify:tests\",\"summary\":\"Run verification\"}],\"stages\":{\"spec_outline\":\"pending\",\"refined_spec\":\"pending\",\"implementation_plan\":\"pending\"}}"}]}}
JSON
`)
	if err == nil {
		t.Fatalf("expected missing kind to fail, output=%s", output)
	}
	if !strings.Contains(output, `uses kind "unspecified" but does not match the thin-plan exemption`) {
		t.Fatalf("expected missing-kind failure, got %s", output)
	}
}

func TestAWMFeaturePlanValidateRejectsIntermediateGroupingTask(t *testing.T) {
	t.Parallel()

	output, err := runFeaturePlanValidator(t, `#!/usr/bin/env bash
set -euo pipefail
cat <<'JSON'
{"result":{"items":[{"content":"{\"kind\":\"maintenance\",\"title\":\"Intermediary grouping\",\"objective\":\"Reject extra hierarchy depth\",\"in_scope\":[\"docs\"],\"out_of_scope\":[\"runtime\"],\"constraints\":[\"Keep the failure focused\"],\"references\":[\"docs/feature-plans.md\"],\"stages\":{\"spec_outline\":\"complete\",\"refined_spec\":\"complete\",\"implementation_plan\":\"in_progress\"},\"tasks\":[{\"key\":\"stage:spec-outline\",\"summary\":\"Spec outline\",\"status\":\"complete\"},{\"key\":\"spec:contract-boundary\",\"summary\":\"Define the planning boundary\",\"status\":\"complete\",\"parent_task_key\":\"stage:spec-outline\",\"references\":[\"docs/feature-plans.md\"],\"acceptance_criteria\":[\"The output is described\",\"The proof is described\"]},{\"key\":\"stage:refined-spec\",\"summary\":\"Refined spec\",\"status\":\"complete\"},{\"key\":\"refine:validator-rules\",\"summary\":\"Define validator expectations\",\"status\":\"complete\",\"parent_task_key\":\"stage:refined-spec\",\"references\":[\"scripts/awm-feature-plan-validate.py\"],\"acceptance_criteria\":[\"The output is described\",\"The proof is described\"]},{\"key\":\"stage:implementation-plan\",\"summary\":\"Implementation plan\",\"status\":\"in_progress\"},{\"key\":\"impl:docs-group\",\"summary\":\"Break docs into a sub-group\",\"status\":\"in_progress\",\"parent_task_key\":\"stage:implementation-plan\",\"references\":[\"docs/feature-plans.md\"],\"acceptance_criteria\":[\"The output is described\",\"The proof is described\"]},{\"key\":\"impl:docs-rules\",\"summary\":\"Update docs and rules\",\"status\":\"pending\",\"parent_task_key\":\"impl:docs-group\",\"references\":[\".awm/awm-rules.yaml\"],\"acceptance_criteria\":[\"The output is described\",\"The proof is described\"]},{\"key\":\"verify:tests\",\"summary\":\"Run verification\",\"status\":\"pending\"}]}"}]}}
JSON
`)
	if err == nil {
		t.Fatalf("expected intermediary grouping task to fail, output=%s", output)
	}
	if !strings.Contains(output, "task impl:docs-group must be a direct leaf under one stage task, not a parent task") {
		t.Fatalf("expected intermediate-parent failure, got %s", output)
	}
}

func TestAWMFeaturePlanValidateRejectsWrongStagePlacement(t *testing.T) {
	t.Parallel()

	output, err := runFeaturePlanValidator(t, `#!/usr/bin/env bash
set -euo pipefail
cat <<'JSON'
{"result":{"items":[{"content":"{\"kind\":\"maintenance\",\"title\":\"Wrong stage placement\",\"objective\":\"Reject implementation work in refined spec stage\",\"in_scope\":[\"docs\"],\"out_of_scope\":[\"runtime\"],\"constraints\":[\"Keep the failure focused\"],\"references\":[\"docs/feature-plans.md\"],\"stages\":{\"spec_outline\":\"complete\",\"refined_spec\":\"complete\",\"implementation_plan\":\"in_progress\"},\"tasks\":[{\"key\":\"stage:spec-outline\",\"summary\":\"Spec outline\",\"status\":\"complete\"},{\"key\":\"spec:contract-boundary\",\"summary\":\"Define the planning boundary\",\"status\":\"complete\",\"parent_task_key\":\"stage:spec-outline\",\"references\":[\"docs/feature-plans.md\"],\"acceptance_criteria\":[\"The output is described\",\"The proof is described\"]},{\"key\":\"stage:refined-spec\",\"summary\":\"Refined spec\",\"status\":\"complete\"},{\"key\":\"impl:docs-rules\",\"summary\":\"Update docs and rules\",\"status\":\"complete\",\"parent_task_key\":\"stage:refined-spec\",\"references\":[\".awm/awm-rules.yaml\"],\"acceptance_criteria\":[\"The output is described\",\"The proof is described\"]},{\"key\":\"stage:implementation-plan\",\"summary\":\"Implementation plan\",\"status\":\"in_progress\"},{\"key\":\"impl:validator-tests\",\"summary\":\"Add validator tests\",\"status\":\"in_progress\",\"parent_task_key\":\"stage:implementation-plan\",\"references\":[\"scripts/awm_feature_plan_validate_test.go\"],\"acceptance_criteria\":[\"The output is described\",\"The proof is described\"]},{\"key\":\"verify:tests\",\"summary\":\"Run verification\",\"status\":\"pending\"}]}"}]}}
JSON
`)
	if err == nil {
		t.Fatalf("expected wrong-stage placement to fail, output=%s", output)
	}
	if !strings.Contains(output, "task impl:docs-rules is under stage:refined-spec but must use one of the prefixes refine:") {
		t.Fatalf("expected wrong-stage failure, got %s", output)
	}
}

func TestAWMFeaturePlanValidateRejectsLeafWithOneAcceptanceCriterion(t *testing.T) {
	t.Parallel()

	output, err := runFeaturePlanValidator(t, `#!/usr/bin/env bash
set -euo pipefail
cat <<'JSON'
{"result":{"items":[{"content":"{\"kind\":\"maintenance\",\"title\":\"One acceptance criterion\",\"objective\":\"Reject weak atomic task proof\",\"in_scope\":[\"docs\"],\"out_of_scope\":[\"runtime\"],\"constraints\":[\"Keep the failure focused\"],\"references\":[\"docs/feature-plans.md\"],\"stages\":{\"spec_outline\":\"complete\",\"refined_spec\":\"complete\",\"implementation_plan\":\"in_progress\"},\"tasks\":[{\"key\":\"stage:spec-outline\",\"summary\":\"Spec outline\",\"status\":\"complete\"},{\"key\":\"spec:contract-boundary\",\"summary\":\"Define the planning boundary\",\"status\":\"complete\",\"parent_task_key\":\"stage:spec-outline\",\"references\":[\"docs/feature-plans.md\"],\"acceptance_criteria\":[\"The output is described\",\"The proof is described\"]},{\"key\":\"stage:refined-spec\",\"summary\":\"Refined spec\",\"status\":\"complete\"},{\"key\":\"refine:validator-rules\",\"summary\":\"Define validator expectations\",\"status\":\"complete\",\"parent_task_key\":\"stage:refined-spec\",\"references\":[\"scripts/awm-feature-plan-validate.py\"],\"acceptance_criteria\":[\"The output is described\",\"The proof is described\"]},{\"key\":\"stage:implementation-plan\",\"summary\":\"Implementation plan\",\"status\":\"in_progress\"},{\"key\":\"impl:docs-rules\",\"summary\":\"Update docs and rules\",\"status\":\"in_progress\",\"parent_task_key\":\"stage:implementation-plan\",\"references\":[\".awm/awm-rules.yaml\"],\"acceptance_criteria\":[\"The output is described\"]},{\"key\":\"verify:tests\",\"summary\":\"Run verification\",\"status\":\"pending\"}]}"}]}}
JSON
`)
	if err == nil {
		t.Fatalf("expected one-criterion leaf to fail, output=%s", output)
	}
	if !strings.Contains(output, "leaf task impl:docs-rules must include at least 2 acceptance_criteria") {
		t.Fatalf("expected acceptance-criteria failure, got %s", output)
	}
}

func TestAWMFeaturePlanValidateSkipsThinPlanExemption(t *testing.T) {
	t.Parallel()

	output, err := runFeaturePlanValidator(t, `#!/usr/bin/env bash
set -euo pipefail
cat <<'JSON'
{"result":{"items":[{"content":"{\"title\":\"Thin plan\",\"objective\":\"Do one bounded documentation update\",\"tasks\":[{\"key\":\"impl:update-readme\",\"summary\":\"Update one README paragraph\",\"references\":[\"README.md\"],\"acceptance_criteria\":[\"The paragraph is updated\",\"The change is easy to verify\"]},{\"key\":\"verify:tests\",\"summary\":\"Run verification\"}]}"}]}}
JSON
`)
	if err != nil {
		t.Fatalf("expected thin plan exemption to skip, got %v\n%s", err, output)
	}
	if !strings.Contains(output, "matches the thin-plan exemption") {
		t.Fatalf("expected thin-plan skip message, got %s", output)
	}
}
