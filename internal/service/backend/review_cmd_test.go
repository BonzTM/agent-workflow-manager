package backend

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func TestRunWorkflowReviewCommand_LoadsDotEnvBackedAWMRuntimeEnv(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("AWM_PG_DSN=postgres://dotenv\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	command := workflowRunDefinition{
		Argv:       []string{os.Args[0], "-test.run=TestRunAWMCommandDotEnvHelperProcess", "--"},
		CWD:        ".",
		TimeoutSec: 5,
		Env: map[string]string{
			"GO_WANT_AWM_COMMAND_DOTENV_HELPER_PROCESS": "1",
			"AWM_EXPECTED_PG_DSN":                       "postgres://dotenv",
			"AWM_EXPECTED_REVIEW_KEY":                   "review:cross-llm",
			"AWM_EXPECTED_PLAN_KEY":                     "plan:receipt.abc123",
		},
	}

	run := runWorkflowReviewCommand(context.Background(), root, command, map[string]string{
		"AWM_REVIEW_KEY": "review:cross-llm",
		"AWM_PLAN_KEY":   "plan:receipt.abc123",
	})
	if run.Err != nil {
		t.Fatalf("unexpected command error: %v\nstdout=%q\nstderr=%q", run.Err, run.Stdout, run.Stderr)
	}
	if run.ExitCode == nil || *run.ExitCode != 0 {
		t.Fatalf("unexpected exit code: %+v", run.ExitCode)
	}
}

func TestReview_RunExecutesWorkflowCommandAndRecordsCompleteTask(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	workflowsYAML := "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: review:cross-llm\n      summary: Cross-LLM review\n      run:\n        argv: [\"scripts/awm-cross-review.sh\"]\n        cwd: .\n        timeout_sec: 600\n        env:\n          AWM_REVIEW_PROVIDER: codex\n"
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte(workflowsYAML), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID: "project.alpha",
			ReceiptID: "receipt.abc123",
		}},
	}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	var gotCommand workflowRunDefinition
	var gotEnv map[string]string
	svc.runReviewCommand = func(_ context.Context, projectRoot string, command workflowRunDefinition, extraEnv map[string]string) verifyCommandRun {
		if projectRoot != root {
			t.Fatalf("unexpected project root: got %q want %q", projectRoot, root)
		}
		gotCommand = command
		gotEnv = extraEnv
		exitCode := 0
		now := time.Now().UTC()
		return verifyCommandRun{
			ExitCode:   &exitCode,
			Stdout:     "PASS: Cross-LLM review passed with no blocking findings.",
			StartedAt:  now,
			FinishedAt: now.Add(250 * time.Millisecond),
		}
	}

	result, apiErr := svc.Review(context.Background(), v1.ReviewPayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		Run:       true,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Executed || result.ReviewKey != v1.DefaultReviewTaskKey || result.ReviewStatus != v1.WorkItemStatusComplete {
		t.Fatalf("unexpected review result: %+v", result)
	}
	if result.AttemptsRun != 1 || result.MaxAttempts != 0 || result.PassingRuns != 1 {
		t.Fatalf("unexpected review attempt counts: %+v", result)
	}
	if result.Execution == nil || result.Execution.TimeoutSec != 600 || len(result.Execution.CommandArgv) != 1 || result.Execution.CommandArgv[0] != "scripts/awm-cross-review.sh" {
		t.Fatalf("unexpected execution payload: %+v", result.Execution)
	}
	if got := gotCommand.Env["AWM_REVIEW_PROVIDER"]; got != "codex" {
		t.Fatalf("unexpected command env: got %q want %q", got, "codex")
	}
	if gotEnv["AWM_PLAN_KEY"] != "plan:receipt.abc123" || gotEnv["AWM_REVIEW_KEY"] != v1.DefaultReviewTaskKey {
		t.Fatalf("unexpected injected env: %+v", gotEnv)
	}
	if gotEnv["AWM_REVIEW_ATTEMPT"] != "1" || gotEnv["AWM_REVIEW_MAX_ATTEMPTS"] != "0" {
		t.Fatalf("unexpected review attempt env: %+v", gotEnv)
	}
	if len(repo.reviewAttemptCalls) != 1 || repo.reviewAttemptCalls[0].Status != "passed" {
		t.Fatalf("expected one saved passing review attempt, got %+v", repo.reviewAttemptCalls)
	}
	if len(repo.workPlanUpsertCalls) != 2 || len(repo.workPlanUpsertCalls[0].Tasks) != 1 {
		t.Fatalf("expected review task upsert plus terminal status sync, got %+v", repo.workPlanUpsertCalls)
	}
	task := repo.workPlanUpsertCalls[0].Tasks[0]
	if task.ItemKey != v1.DefaultReviewTaskKey || task.Status != string(v1.WorkItemStatusComplete) {
		t.Fatalf("unexpected recorded review task: %+v", task)
	}
	if task.Summary != "Cross-LLM review" {
		t.Fatalf("unexpected review summary: %q", task.Summary)
	}
	if !strings.Contains(task.Outcome, "PASS:") {
		t.Fatalf("unexpected review outcome: %q", task.Outcome)
	}
	if repo.workPlanUpsertCalls[1].Status != core.PlanStatusComplete {
		t.Fatalf("expected terminal status sync upsert, got %+v", repo.workPlanUpsertCalls[1])
	}
}

func TestReview_RunInjectsEffectiveScopeAndTaskDeltaEnv(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	workflowsYAML := "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: review:cross-llm\n      summary: Cross-LLM review\n      run:\n        argv: [\"scripts/awm-cross-review.sh\"]\n        timeout_sec: 600\n"
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte(workflowsYAML), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}
	writeRepoFile(t, root, "src/scoped.go", "package src\n\nfunc scoped() string { return \"before\" }\n")
	writeRepoFile(t, root, "docs/discovered.md", "notes\n")
	baselinePaths := []string{".awm/awm-workflows.yaml", "docs/discovered.md", "src/scoped.go"}
	baselineHashes, err := computeFileHashes(root, baselinePaths)
	if err != nil {
		t.Fatalf("compute baseline hashes: %v", err)
	}
	writeRepoFile(t, root, "src/scoped.go", "package src\n\nfunc scoped() string { return \"after\" }\n")

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{"src/scoped.go"},
			BaselineCaptured:  true,
			BaselinePaths: []core.SyncPath{
				{Path: ".awm/awm-workflows.yaml", ContentHash: baselineHashes[".awm/awm-workflows.yaml"]},
				{Path: "docs/discovered.md", ContentHash: baselineHashes["docs/discovered.md"]},
				{Path: "src/scoped.go", ContentHash: baselineHashes["src/scoped.go"]},
			},
		}},
		workPlanLookupResult: []core.WorkPlan{
			{
				ProjectID:       "project.alpha",
				ReceiptID:       "receipt.abc123",
				PlanKey:         "plan:receipt.abc123",
				DiscoveredPaths: []string{"docs/discovered.md"},
			},
			{
				ProjectID:       "project.alpha",
				ReceiptID:       "receipt.abc123",
				PlanKey:         "plan:receipt.abc123",
				DiscoveredPaths: []string{"docs/discovered.md"},
			},
		},
	}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	var gotEnv map[string]string
	svc.runReviewCommand = func(_ context.Context, _ string, _ workflowRunDefinition, extraEnv map[string]string) verifyCommandRun {
		gotEnv = extraEnv
		exitCode := 0
		now := time.Now().UTC()
		return verifyCommandRun{
			ExitCode:   &exitCode,
			Stdout:     "PASS: Cross-LLM review passed with no blocking findings.",
			StartedAt:  now,
			FinishedAt: now.Add(250 * time.Millisecond),
		}
	}

	result, apiErr := svc.Review(context.Background(), v1.ReviewPayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		Run:       true,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Executed || result.ReviewStatus != v1.WorkItemStatusComplete {
		t.Fatalf("unexpected review result: %+v", result)
	}
	if gotEnv["AWM_REVIEW_BASELINE_CAPTURED"] != "true" || gotEnv["AWM_REVIEW_TASK_DELTA_SOURCE"] != "receipt_baseline" {
		t.Fatalf("unexpected baseline env: %+v", gotEnv)
	}

	var changedPaths []string
	if err := json.Unmarshal([]byte(gotEnv["AWM_REVIEW_CHANGED_PATHS_JSON"]), &changedPaths); err != nil {
		t.Fatalf("decode changed paths env: %v", err)
	}
	if !reflect.DeepEqual(changedPaths, []string{"src/scoped.go"}) {
		t.Fatalf("unexpected changed paths env: %v", changedPaths)
	}

	var effectiveScope []string
	if err := json.Unmarshal([]byte(gotEnv["AWM_REVIEW_EFFECTIVE_SCOPE_PATHS_JSON"]), &effectiveScope); err != nil {
		t.Fatalf("decode effective scope env: %v", err)
	}
	if !containsString(effectiveScope, "src/scoped.go") || !containsString(effectiveScope, "docs/discovered.md") {
		t.Fatalf("unexpected effective scope env: %v", effectiveScope)
	}
}

func TestReview_RunRecordsBlockedTaskWhenCommandFails(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	workflowsYAML := "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: review:cross-llm\n      run:\n        argv: [\"scripts/awm-cross-review.sh\"]\n        timeout_sec: 300\n"
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte(workflowsYAML), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID: "project.alpha",
			ReceiptID: "receipt.abc123",
		}},
	}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.runReviewCommand = func(_ context.Context, _ string, _ workflowRunDefinition, _ map[string]string) verifyCommandRun {
		exitCode := 1
		now := time.Now().UTC()
		return verifyCommandRun{
			ExitCode:   &exitCode,
			Stdout:     "FAIL: Missing schema update coverage.",
			StartedAt:  now,
			FinishedAt: now.Add(100 * time.Millisecond),
			Err:        errors.New("exit status 1"),
		}
	}

	result, apiErr := svc.Review(context.Background(), v1.ReviewPayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		Run:       true,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.ReviewStatus != v1.WorkItemStatusBlocked {
		t.Fatalf("expected blocked review status, got %+v", result)
	}
	if result.AttemptsRun != 1 || result.MaxAttempts != 0 || result.PassingRuns != 0 {
		t.Fatalf("unexpected review attempt counts: %+v", result)
	}
	if len(repo.reviewAttemptCalls) != 1 || repo.reviewAttemptCalls[0].Status != "failed" {
		t.Fatalf("expected one saved failed review attempt, got %+v", repo.reviewAttemptCalls)
	}
	if len(repo.workPlanUpsertCalls) != 1 || len(repo.workPlanUpsertCalls[0].Tasks) != 1 {
		t.Fatalf("expected one work plan upsert with one task, got %+v", repo.workPlanUpsertCalls)
	}
	task := repo.workPlanUpsertCalls[0].Tasks[0]
	if task.Status != string(v1.WorkItemStatusBlocked) {
		t.Fatalf("unexpected blocked task status: %+v", task)
	}
	if !strings.Contains(task.Outcome, "Missing schema update coverage") {
		t.Fatalf("unexpected blocked outcome: %q", task.Outcome)
	}
}

func TestReview_RunInjectsDerivedReceiptIDForPlanKeyOnlyRequests(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	workflowsYAML := "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: review:cross-llm\n      summary: Cross-LLM review\n      run:\n        argv: [\"scripts/awm-cross-review.sh\"]\n        timeout_sec: 600\n"
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte(workflowsYAML), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID: "project.alpha",
			ReceiptID: "receipt.abc123",
		}},
	}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	var gotEnv map[string]string
	svc.runReviewCommand = func(_ context.Context, _ string, _ workflowRunDefinition, extraEnv map[string]string) verifyCommandRun {
		gotEnv = extraEnv
		exitCode := 0
		now := time.Now().UTC()
		return verifyCommandRun{
			ExitCode:   &exitCode,
			Stdout:     "PASS: Derived receipt env works.",
			StartedAt:  now,
			FinishedAt: now.Add(100 * time.Millisecond),
		}
	}

	result, apiErr := svc.Review(context.Background(), v1.ReviewPayload{
		ProjectID: "project.alpha",
		PlanKey:   "plan:receipt.abc123",
		Run:       true,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.ReviewStatus != v1.WorkItemStatusComplete {
		t.Fatalf("unexpected review result: %+v", result)
	}
	if gotEnv["AWM_RECEIPT_ID"] != "receipt.abc123" {
		t.Fatalf("unexpected derived AWM_RECEIPT_ID: %+v", gotEnv)
	}
	if gotEnv["AWM_PLAN_KEY"] != "plan:receipt.abc123" {
		t.Fatalf("unexpected derived AWM_PLAN_KEY: %+v", gotEnv)
	}
}

func TestReview_RunRequiresWorkflowRunCommand(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	workflowsYAML := "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: review:cross-llm\n"
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte(workflowsYAML), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}

	repo := &fakeRepository{}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Review(context.Background(), v1.ReviewPayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		Run:       true,
	})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INVALID_INPUT" {
		t.Fatalf("unexpected error code: %+v", apiErr)
	}
}

func TestReview_RunRequiresConfiguredWorkflowKey(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	workflowsYAML := "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: review:human\n      run:\n        argv: [\"scripts/awm-human-review.sh\"]\n"
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte(workflowsYAML), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}

	repo := &fakeRepository{}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Review(context.Background(), v1.ReviewPayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		Key:       v1.DefaultReviewTaskKey,
		Run:       true,
	})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INVALID_INPUT" {
		t.Fatalf("unexpected error code: %+v", apiErr)
	}
	if !strings.Contains(apiErr.Message, "review key is not configured") {
		t.Fatalf("unexpected error message: %+v", apiErr)
	}
	if len(repo.workPlanUpsertCalls) != 0 {
		t.Fatalf("expected no work plan upsert, got %+v", repo.workPlanUpsertCalls)
	}
}

func TestReview_RunRejectsInvalidWorkflowDefinitions(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	workflowsYAML := "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: review:cross-llm\n      run:\n        argv: [\"scripts/awm-cross-review.sh\"]\n        bad_field: true\n"
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte(workflowsYAML), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}

	repo := &fakeRepository{}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Review(context.Background(), v1.ReviewPayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		Run:       true,
	})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INVALID_INPUT" {
		t.Fatalf("unexpected error code: %+v", apiErr)
	}
	if !strings.Contains(apiErr.Message, "workflow definitions are invalid") {
		t.Fatalf("unexpected error message: %+v", apiErr)
	}
	if len(repo.workPlanUpsertCalls) != 0 {
		t.Fatalf("expected no work plan upsert, got %+v", repo.workPlanUpsertCalls)
	}
}

func TestReview_RunRejectsZeroMaxAttempts(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	workflowsYAML := "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: review:cross-llm\n      max_attempts: 0\n      run:\n        argv: [\"scripts/awm-cross-review.sh\"]\n"
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte(workflowsYAML), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}

	repo := &fakeRepository{}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Review(context.Background(), v1.ReviewPayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		Run:       true,
	})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INVALID_INPUT" {
		t.Fatalf("unexpected error code: %+v", apiErr)
	}
	details, ok := apiErr.Details.(map[string]any)
	if !ok {
		t.Fatalf("unexpected error details type: %+v", apiErr)
	}
	detail, ok := details["error"].(string)
	if !ok {
		t.Fatalf("unexpected error detail type: %+v", apiErr)
	}
	if !strings.Contains(detail, "max_attempts must be 1..16 when provided") {
		t.Fatalf("unexpected error details: %+v", apiErr)
	}
}

func TestReview_RunSkipsDuplicateFingerprintWithoutExecutingRunner(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	workflowsYAML := "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: review:cross-llm\n      summary: Cross-LLM review\n      run:\n        argv: [\"scripts/awm-cross-review.sh\"]\n"
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte(workflowsYAML), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}

	scope := core.ReceiptScope{ProjectID: "project.alpha", ReceiptID: "receipt.abc123"}
	fingerprint, apiErr := computeReviewFingerprint(root, "project.alpha", "receipt.abc123", v1.DefaultReviewTaskKey, ".awm/awm-workflows.yaml", workflowRunDefinition{
		Argv:       []string{"scripts/awm-cross-review.sh"},
		CWD:        ".",
		TimeoutSec: 300,
	}, scope, nil)
	if apiErr != nil {
		t.Fatalf("compute fingerprint: %+v", apiErr)
	}

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{scope},
		reviewAttemptResults: [][]core.ReviewAttempt{{
			{
				AttemptID:   1,
				ProjectID:   "project.alpha",
				ReceiptID:   "receipt.abc123",
				ReviewKey:   v1.DefaultReviewTaskKey,
				Fingerprint: fingerprint,
				Status:      "passed",
				Passed:      true,
				Outcome:     "Review gate passed (1/2 attempts, 1 passing run(s)): PASS",
			},
		}},
	}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.runReviewCommand = func(context.Context, string, workflowRunDefinition, map[string]string) verifyCommandRun {
		t.Fatal("runner should not execute for duplicate fingerprint")
		return verifyCommandRun{}
	}

	result, apiErr := svc.Review(context.Background(), v1.ReviewPayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		Run:       true,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Executed || result.ReviewStatus != v1.WorkItemStatusComplete {
		t.Fatalf("unexpected duplicate review result: %+v", result)
	}
	if result.SkippedReason == "" || result.AttemptsRun != 1 || result.PassingRuns != 1 {
		t.Fatalf("unexpected duplicate review metadata: %+v", result)
	}
	if len(repo.reviewAttemptCalls) != 0 {
		t.Fatalf("expected no new review attempt save, got %+v", repo.reviewAttemptCalls)
	}
	if len(repo.workPlanUpsertCalls) != 2 || len(repo.workPlanUpsertCalls[0].Tasks) != 1 {
		t.Fatalf("expected review task upsert plus terminal status sync, got %+v", repo.workPlanUpsertCalls)
	}
	if got := repo.workPlanUpsertCalls[0].Tasks[0].Summary; got != "Cross-LLM review" {
		t.Fatalf("unexpected duplicate review summary: %q", got)
	}
	if repo.workPlanUpsertCalls[1].Status != core.PlanStatusComplete {
		t.Fatalf("expected terminal status sync upsert, got %+v", repo.workPlanUpsertCalls[1])
	}
}

func TestComputeReviewFingerprintIncludesCompletionManagedPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-tests.yaml"), []byte("version: awm.tests.v1\n"), 0o644); err != nil {
		t.Fatalf("write tests file: %v", err)
	}

	command := workflowRunDefinition{
		Argv:       []string{"scripts/awm-cross-review.sh"},
		TimeoutSec: 300,
	}
	first, apiErr := computeReviewFingerprint(root, "project.alpha", "receipt.abc123", v1.DefaultReviewTaskKey, ".awm/awm-workflows.yaml", command, core.ReceiptScope{}, nil)
	if apiErr != nil {
		t.Fatalf("compute first fingerprint: %+v", apiErr)
	}
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-tests.yaml"), []byte("version: awm.tests.v1\nsmoke: []\n"), 0o644); err != nil {
		t.Fatalf("rewrite tests file: %v", err)
	}
	second, apiErr := computeReviewFingerprint(root, "project.alpha", "receipt.abc123", v1.DefaultReviewTaskKey, ".awm/awm-workflows.yaml", command, core.ReceiptScope{}, nil)
	if apiErr != nil {
		t.Fatalf("compute second fingerprint: %+v", apiErr)
	}
	if first == second {
		t.Fatalf("expected managed completion files to affect fingerprint, got %q", first)
	}
}

func TestReview_RunRerunsAfterInterruptedSameFingerprintAttempt(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	workflowsYAML := "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: review:cross-llm\n      summary: Cross-LLM review\n      rerun_requires_new_fingerprint: true\n      run:\n        argv: [\"scripts/awm-cross-review.sh\"]\n        timeout_sec: 300\n"
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte(workflowsYAML), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID: "project.alpha",
			ReceiptID: "receipt.abc123",
		}},
		reviewAttemptResults: [][]core.ReviewAttempt{{
			{
				AttemptID:   1,
				ProjectID:   "project.alpha",
				ReceiptID:   "receipt.abc123",
				ReviewKey:   v1.DefaultReviewTaskKey,
				Fingerprint: "sha256:36ea12d376b79ca844bf8f7f89dd90475b6366e283ac5895b72672cbf3a5d72f",
				Status:      "failed",
				Passed:      false,
				Outcome:     "Review gate failed: signal: terminated",
			},
		}},
	}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	runnerCalls := 0
	svc.runReviewCommand = func(context.Context, string, workflowRunDefinition, map[string]string) verifyCommandRun {
		runnerCalls++
		exitCode := 0
		now := time.Now().UTC()
		return verifyCommandRun{
			ExitCode:   &exitCode,
			Stdout:     "PASS: Cross-LLM review passed after rerun.",
			StartedAt:  now,
			FinishedAt: now.Add(100 * time.Millisecond),
		}
	}

	result, apiErr := svc.Review(context.Background(), v1.ReviewPayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		Run:       true,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Executed || result.ReviewStatus != v1.WorkItemStatusComplete {
		t.Fatalf("expected interrupted duplicate fingerprint to rerun, got %+v", result)
	}
	if runnerCalls != 1 {
		t.Fatalf("expected runner to execute once, got %d", runnerCalls)
	}
	if len(repo.reviewAttemptCalls) != 1 || repo.reviewAttemptCalls[0].Status != "passed" {
		t.Fatalf("expected a new persisted passing attempt, got %+v", repo.reviewAttemptCalls)
	}
}

func TestReview_RunBlocksWhenMaxAttemptsAreExhausted(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	workflowsYAML := "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: review:cross-llm\n      summary: Cross-LLM review\n      max_attempts: 2\n      run:\n        argv: [\"scripts/awm-cross-review.sh\"]\n"
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte(workflowsYAML), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID: "project.alpha",
			ReceiptID: "receipt.abc123",
		}},
		reviewAttemptResults: [][]core.ReviewAttempt{{
			{AttemptID: 1, ProjectID: "project.alpha", ReceiptID: "receipt.abc123", ReviewKey: v1.DefaultReviewTaskKey, Fingerprint: "sha256:first", Status: "failed", Outcome: "first failure"},
			{AttemptID: 2, ProjectID: "project.alpha", ReceiptID: "receipt.abc123", ReviewKey: v1.DefaultReviewTaskKey, Fingerprint: "sha256:second", Status: "failed", Outcome: "second failure"},
		}},
	}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.runReviewCommand = func(context.Context, string, workflowRunDefinition, map[string]string) verifyCommandRun {
		t.Fatal("runner should not execute once max_attempts is exhausted")
		return verifyCommandRun{}
	}

	result, apiErr := svc.Review(context.Background(), v1.ReviewPayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		Run:       true,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Executed || result.ReviewStatus != v1.WorkItemStatusBlocked {
		t.Fatalf("unexpected exhausted review result: %+v", result)
	}
	if result.SkippedReason == "" || result.AttemptsRun != 2 || result.MaxAttempts != 2 {
		t.Fatalf("unexpected exhausted review metadata: %+v", result)
	}
	if len(repo.reviewAttemptCalls) != 0 {
		t.Fatalf("expected no new review attempt save, got %+v", repo.reviewAttemptCalls)
	}
	if len(repo.workPlanUpsertCalls) != 1 || len(repo.workPlanUpsertCalls[0].Tasks) != 1 {
		t.Fatalf("expected one work plan upsert with one task, got %+v", repo.workPlanUpsertCalls)
	}
	if got := repo.workPlanUpsertCalls[0].Tasks[0].Summary; got != "Cross-LLM review" {
		t.Fatalf("unexpected exhausted review summary: %q", got)
	}
}

func TestReportCompletionFlagsStaleReviewForCurrentFingerprint(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
		t.Fatalf("mkdir internal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "review.txt"), []byte("new content"), 0o644); err != nil {
		t.Fatalf("write scoped file: %v", err)
	}
	workflowsYAML := "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: review:cross-llm\n      select:\n        phases: [\"review\"]\n        changed_paths_any: [\"internal/**\"]\n      run:\n        argv: [\"scripts/awm-cross-review.sh\"]\n"
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte(workflowsYAML), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			Phase:             "review",
			InitialScopePaths: []string{"internal/review.txt"},
		}},
		workListResults: [][]core.WorkItem{{
			{
				ItemKey: v1.DefaultReviewTaskKey,
				Status:  string(v1.WorkItemStatusComplete),
				Outcome: "Review gate passed",
			},
		}},
		reviewAttemptResults: [][]core.ReviewAttempt{{
			{
				AttemptID:   1,
				ProjectID:   "project.alpha",
				ReceiptID:   "receipt.abc123",
				ReviewKey:   v1.DefaultReviewTaskKey,
				Fingerprint: "sha256:stale",
				Status:      "passed",
				Passed:      true,
				Outcome:     "Review gate passed",
			},
		}},
	}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{"internal/review.txt"},
		Outcome:      "done",
		ScopeMode:    v1.ScopeModeStrict,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Accepted {
		t.Fatalf("expected stale review to block strict report completion: %+v", result)
	}
	if len(result.DefinitionOfDoneIssues) != 1 || !strings.Contains(result.DefinitionOfDoneIssues[0], "stale for the current scoped fingerprint") {
		t.Fatalf("unexpected definition_of_done_issues: %+v", result.DefinitionOfDoneIssues)
	}
}

func TestReportCompletionFlagsStaleReviewForManagedGovernanceFileCurrentFingerprint(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-tests.yaml"), []byte("version: awm.tests.v1\n"), 0o644); err != nil {
		t.Fatalf("write tests file: %v", err)
	}
	workflowsYAML := "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: review:cross-llm\n      select:\n        phases: [\"review\"]\n        changed_paths_any: [\".awm/**\"]\n      run:\n        argv: [\"scripts/awm-cross-review.sh\"]\n"
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte(workflowsYAML), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}

	scope := core.ReceiptScope{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		Phase:     "review",
	}
	command := workflowRunDefinition{
		Argv:       []string{"scripts/awm-cross-review.sh"},
		TimeoutSec: 300,
	}
	staleFingerprint, apiErr := computeReviewFingerprint(root, "project.alpha", "receipt.abc123", v1.DefaultReviewTaskKey, ".awm/awm-workflows.yaml", command, scope, nil)
	if apiErr != nil {
		t.Fatalf("compute stale fingerprint: %+v", apiErr)
	}
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-tests.yaml"), []byte("version: awm.tests.v1\nsmoke: []\n"), 0o644); err != nil {
		t.Fatalf("rewrite tests file: %v", err)
	}

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{scope},
		workListResults: [][]core.WorkItem{{
			{
				ItemKey: v1.DefaultReviewTaskKey,
				Status:  string(v1.WorkItemStatusComplete),
				Outcome: "Review gate passed",
			},
		}},
		reviewAttemptResults: [][]core.ReviewAttempt{{
			{
				AttemptID:   1,
				ProjectID:   "project.alpha",
				ReceiptID:   "receipt.abc123",
				ReviewKey:   v1.DefaultReviewTaskKey,
				Fingerprint: staleFingerprint,
				Status:      "passed",
				Passed:      true,
				Outcome:     "Review gate passed",
			},
		}},
	}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{".awm/awm-tests.yaml"},
		Outcome:      "done",
		ScopeMode:    v1.ScopeModeStrict,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Accepted {
		t.Fatalf("expected stale managed governance review to block strict report completion: %+v", result)
	}
	if len(result.DefinitionOfDoneIssues) != 1 || !strings.Contains(result.DefinitionOfDoneIssues[0], "stale for the current scoped fingerprint") {
		t.Fatalf("unexpected definition_of_done_issues: %+v", result.DefinitionOfDoneIssues)
	}
}

func TestReview_ManualCompleteRejectsRunnableWorkflowGate(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	workflowsYAML := "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: review:cross-llm\n      summary: Cross-LLM review\n      run:\n        argv: [\"scripts/awm-cross-review.sh\"]\n"
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte(workflowsYAML), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}

	repo := &fakeRepository{}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Review(context.Background(), v1.ReviewPayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		Outcome:   "Manual review note",
	})
	if apiErr == nil {
		t.Fatal("expected manual complete review to be rejected for runnable gate")
	}
	if apiErr.Code != "INVALID_INPUT" {
		t.Fatalf("unexpected error code: %s", apiErr.Code)
	}
	if !strings.Contains(apiErr.Message, "run=true") {
		t.Fatalf("unexpected error message: %+v", apiErr)
	}
}

func TestReview_RunRerunsAfterFailedSameFingerprintAttempt(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	workflowsYAML := "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: review:cross-llm\n      summary: Cross-LLM review\n      rerun_requires_new_fingerprint: true\n      run:\n        argv: [\"scripts/awm-cross-review.sh\"]\n        timeout_sec: 300\n"
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte(workflowsYAML), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}

	scope := core.ReceiptScope{ProjectID: "project.alpha", ReceiptID: "receipt.abc123"}
	fingerprint, apiErr := computeReviewFingerprint(root, "project.alpha", "receipt.abc123", v1.DefaultReviewTaskKey, ".awm/awm-workflows.yaml", workflowRunDefinition{
		Argv:       []string{"scripts/awm-cross-review.sh"},
		TimeoutSec: 300,
	}, scope, nil)
	if apiErr != nil {
		t.Fatalf("compute fingerprint: %+v", apiErr)
	}

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{scope},
		reviewAttemptResults: [][]core.ReviewAttempt{{
			{
				AttemptID:   1,
				ProjectID:   "project.alpha",
				ReceiptID:   "receipt.abc123",
				ReviewKey:   v1.DefaultReviewTaskKey,
				Fingerprint: fingerprint,
				Status:      "failed",
				Passed:      false,
				Outcome:     "Review gate failed",
			},
		}},
	}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	runnerCalls := 0
	svc.runReviewCommand = func(context.Context, string, workflowRunDefinition, map[string]string) verifyCommandRun {
		runnerCalls++
		exitCode := 0
		now := time.Now().UTC()
		return verifyCommandRun{
			ExitCode:   &exitCode,
			Stdout:     "PASS: Cross-LLM review passed after rerun.",
			StartedAt:  now,
			FinishedAt: now.Add(100 * time.Millisecond),
		}
	}

	result, apiErr := svc.Review(context.Background(), v1.ReviewPayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		Run:       true,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Executed || result.ReviewStatus != v1.WorkItemStatusComplete {
		t.Fatalf("expected failed duplicate fingerprint to rerun, got %+v", result)
	}
	if runnerCalls != 1 {
		t.Fatalf("expected runner to execute once, got %d", runnerCalls)
	}
	if len(repo.reviewAttemptCalls) != 1 || repo.reviewAttemptCalls[0].Status != "passed" {
		t.Fatalf("expected a new persisted passing attempt, got %+v", repo.reviewAttemptCalls)
	}
}

func TestComputeReviewFingerprintChangesWhenScopedDirectoryChildChanges(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "scripts/awm-cross-review.sh", "#!/usr/bin/env bash\nexit 0\n")
	writeRepoFile(t, root, "src/nested/review.txt", "before\n")

	command := workflowRunDefinition{
		Argv:       []string{"scripts/awm-cross-review.sh"},
		TimeoutSec: 300,
	}
	scope := core.ReceiptScope{InitialScopePaths: []string{"src"}}
	first, apiErr := computeReviewFingerprint(root, "project.alpha", "receipt.abc123", v1.DefaultReviewTaskKey, ".awm/awm-workflows.yaml", command, scope, nil)
	if apiErr != nil {
		t.Fatalf("compute first fingerprint: %+v", apiErr)
	}
	writeRepoFile(t, root, "src/nested/review.txt", "after\n")
	second, apiErr := computeReviewFingerprint(root, "project.alpha", "receipt.abc123", v1.DefaultReviewTaskKey, ".awm/awm-workflows.yaml", command, scope, nil)
	if apiErr != nil {
		t.Fatalf("compute second fingerprint: %+v", apiErr)
	}
	if first == second {
		t.Fatalf("expected scoped directory child changes to affect fingerprint, got %q", first)
	}
}

func TestComputeReviewFingerprintChangesWhenWorkflowArgumentFileChanges(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "scripts/review_runner.py", "print('review')\n")
	writeRepoFile(t, root, "configs/review.json", "{\"level\":1}\n")

	command := workflowRunDefinition{
		Argv:       []string{"python3", "scripts/review_runner.py", "configs/review.json"},
		TimeoutSec: 300,
	}
	first, apiErr := computeReviewFingerprint(root, "project.alpha", "receipt.abc123", v1.DefaultReviewTaskKey, ".awm/awm-workflows.yaml", command, core.ReceiptScope{}, nil)
	if apiErr != nil {
		t.Fatalf("compute first fingerprint: %+v", apiErr)
	}
	writeRepoFile(t, root, "configs/review.json", "{\"level\":2}\n")
	second, apiErr := computeReviewFingerprint(root, "project.alpha", "receipt.abc123", v1.DefaultReviewTaskKey, ".awm/awm-workflows.yaml", command, core.ReceiptScope{}, nil)
	if apiErr != nil {
		t.Fatalf("compute second fingerprint: %+v", apiErr)
	}
	if first == second {
		t.Fatalf("expected workflow argument files to affect fingerprint, got %q", first)
	}
}
