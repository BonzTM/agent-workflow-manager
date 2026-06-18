package backend

import (
	"context"
	"fmt"
	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestVerify_DryRunSelectsDeterministicallyWithoutPersistence(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	testsYAML := `version: awm.tests.v1
defaults:
  cwd: .
  timeout_sec: 45
tests:
  - id: alpha-unit
    summary: Run alpha verification
    command:
      argv: ["go", "test", "./internal/..."]
    select:
      phases: ["review"]
      tags_any: ["backend"]
      changed_paths_any: ["internal/**"]
    expected:
      exit_code: 0
  - id: gamma-smoke
    summary: Repo smoke test
    command:
      argv: ["echo", "noop"]
      env:
        AWM_VERIFY_TEST_MODE: smoke
    select:
      always_run: true
  - id: delta-unscoped
    summary: Unscoped test should not auto-select
    command:
      argv: ["echo", "noop"]
`
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-tests.yaml"), []byte(testsYAML), 0o644); err != nil {
		t.Fatalf("write tests file: %v", err)
	}

	withWorkingDir(t, root)

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:    "project.alpha",
			ReceiptID:    "receipt.abc123",
			Phase:        "execute",
			ResolvedTags: []string{"backend"},
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.runVerifyCommand = func(_ context.Context, _ string, _ verifyTestDefinition, _ map[string]string) verifyCommandRun {
		t.Fatal("runVerifyCommand should not be called for dry-run")
		return verifyCommandRun{}
	}

	result, apiErr := svc.Verify(context.Background(), v1.VerifyPayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		Phase:        v1.PhaseReview,
		FilesChanged: []string{"internal/service/backend/service.go"},
		DryRun:       true,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Status != v1.VerifyStatusDryRun {
		t.Fatalf("unexpected status: got %q want %q", result.Status, v1.VerifyStatusDryRun)
	}
	if got, want := result.SelectedTestIDs, []string{"alpha-unit", "gamma-smoke"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected selected test ids: got %v want %v", got, want)
	}
	if len(result.Selected) != 2 || result.Selected[0].TestID != "alpha-unit" || result.Selected[1].TestID != "gamma-smoke" {
		t.Fatalf("unexpected selected results: %+v", result.Selected)
	}
	if got, want := result.Selected[1].SelectionReasons, []string{"always_run=true"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected always_run selection reasons: got %v want %v", got, want)
	}
	if result.Passed {
		t.Fatalf("expected passed=false for dry run, got %+v", result.Passed)
	}
	if len(result.Results) != 0 {
		t.Fatalf("expected no executed results on dry run, got %+v", result.Results)
	}
	if result.BatchRunID != "" {
		t.Fatalf("expected no batch run id on dry run, got %q", result.BatchRunID)
	}
	if len(repo.verifySaveCalls) != 0 {
		t.Fatalf("expected no persisted verification batches, got %d", len(repo.verifySaveCalls))
	}
	if len(repo.workUpsertCalls) != 0 {
		t.Fatalf("expected no work upserts on dry run, got %d", len(repo.workUpsertCalls))
	}
}

func TestVerify_ExecutesPersistsAndUpdatesWork(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	testsYAML := `version: awm.tests.v1
defaults:
  cwd: .
  timeout_sec: 45
tests:
  - id: alpha-unit
    summary: Run alpha verification
    command:
      argv: ["go", "test", "./internal/..."]
    select:
      phases: ["review"]
      tags_any: ["backend"]
      changed_paths_any: ["internal/**"]
    expected:
      exit_code: 0
  - id: gamma-smoke
    summary: Run smoke verification
    command:
      argv: ["echo", "noop"]
    select:
      always_run: true
`
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-tests.yaml"), []byte(testsYAML), 0o644); err != nil {
		t.Fatalf("write tests file: %v", err)
	}

	withWorkingDir(t, root)

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:    "project.alpha",
			ReceiptID:    "receipt.abc123",
			Phase:        "execute",
			ResolvedTags: []string{"backend"},
		}},
		workUpsertResults: []int{1},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	base := time.Unix(1_700_000_000, 0).UTC()
	svc.runVerifyCommand = func(_ context.Context, _ string, def verifyTestDefinition, extraEnv map[string]string) verifyCommandRun {
		switch def.ID {
		case "alpha-unit":
			exitCode := 0
			return verifyCommandRun{
				ExitCode:   &exitCode,
				Stdout:     "alpha ok\n",
				StartedAt:  base,
				FinishedAt: base.Add(2 * time.Second),
			}
		case "gamma-smoke":
			if got := extraEnv["AWM_RECEIPT_ID"]; got != "receipt.abc123" {
				t.Fatalf("unexpected injected AWM_RECEIPT_ID: %+v", extraEnv)
			}
			if got := extraEnv["AWM_PLAN_KEY"]; got != "plan:receipt.abc123" {
				t.Fatalf("unexpected injected AWM_PLAN_KEY: %+v", extraEnv)
			}
			if got := extraEnv["AWM_VERIFY_PHASE"]; got != "review" {
				t.Fatalf("unexpected injected AWM_VERIFY_PHASE: %+v", extraEnv)
			}
			if got := extraEnv["AWM_VERIFY_TAGS_JSON"]; got != `["backend"]` {
				t.Fatalf("unexpected injected AWM_VERIFY_TAGS_JSON: %+v", extraEnv)
			}
			if got := extraEnv["AWM_VERIFY_FILES_CHANGED_JSON"]; got != `["internal/service/backend/service.go"]` {
				t.Fatalf("unexpected injected AWM_VERIFY_FILES_CHANGED_JSON: %+v", extraEnv)
			}
			exitCode := 0
			return verifyCommandRun{
				ExitCode:   &exitCode,
				Stdout:     "smoke ok\n",
				StartedAt:  base.Add(3 * time.Second),
				FinishedAt: base.Add(4 * time.Second),
			}
		default:
			t.Fatalf("unexpected test id: %s", def.ID)
			return verifyCommandRun{}
		}
	}

	result, apiErr := svc.Verify(context.Background(), v1.VerifyPayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		PlanKey:   "plan:receipt.abc123",
		Phase:     v1.PhaseReview,
		FilesChanged: []string{
			"internal/service/backend/service.go",
		},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Status != v1.VerifyStatusPassed {
		t.Fatalf("unexpected status: got %q want %q", result.Status, v1.VerifyStatusPassed)
	}
	if !result.Passed {
		t.Fatalf("expected passed=true, got %+v", result.Passed)
	}
	if result.BatchRunID == "" {
		t.Fatal("expected batch run id")
	}
	if got, want := result.SelectedTestIDs, []string{"alpha-unit", "gamma-smoke"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected selected test ids: got %v want %v", got, want)
	}
	if len(result.Results) != 2 {
		t.Fatalf("unexpected result count: %d", len(result.Results))
	}
	if result.Results[0].Status != v1.VerifyTestStatusPassed || result.Results[1].Status != v1.VerifyTestStatusPassed {
		t.Fatalf("unexpected verify results: %+v", result.Results)
	}
	if len(repo.verifySaveCalls) != 1 {
		t.Fatalf("expected one persisted verification batch, got %d", len(repo.verifySaveCalls))
	}

	saved := repo.verifySaveCalls[0]
	if saved.Status != "passed" || saved.TestsSourcePath != ".awm/awm-tests.yaml" {
		t.Fatalf("unexpected persisted batch: %+v", saved)
	}
	if saved.ReceiptID != "receipt.abc123" || saved.PlanKey != "plan:receipt.abc123" {
		t.Fatalf("unexpected persisted verify scope: %+v", saved)
	}
	if got, want := saved.SelectedTestIDs, []string{"alpha-unit", "gamma-smoke"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected persisted selected test ids: got %v want %v", got, want)
	}
	if len(saved.Results) != 2 {
		t.Fatalf("unexpected persisted results: %+v", saved.Results)
	}
	if saved.Results[0].TimeoutSec != 45 {
		t.Fatalf("expected default timeout inheritance, got %d", saved.Results[0].TimeoutSec)
	}
	if saved.Results[1].TimeoutSec != 45 {
		t.Fatalf("expected default timeout inheritance, got %d", saved.Results[1].TimeoutSec)
	}
	if len(repo.workUpsertCalls) != 1 {
		t.Fatalf("expected one work upsert call, got %d", len(repo.workUpsertCalls))
	}
	if got := repo.workUpsertCalls[0].Items[0].ItemKey; got != "verify:tests" {
		t.Fatalf("unexpected work item key: %q", got)
	}
	if got := repo.workUpsertCalls[0].Items[0].Status; got != core.WorkItemStatusComplete {
		t.Fatalf("unexpected work item status: %q", got)
	}
	if got := repo.workUpsertCalls[0].Items[0].Outcome; !strings.Contains(got, "2 verification tests passed") {
		t.Fatalf("unexpected work outcome: %q", got)
	}
	if got := repo.workUpsertCalls[0].Items[0].Evidence; len(got) != 3 || got[0] != "verifyrun:"+result.BatchRunID {
		t.Fatalf("unexpected work evidence: %v", got)
	}
}

func TestVerify_WorkEvidenceIsCapped(t *testing.T) {
	repo := &fakeRepository{
		workUpsertResults: []int{1},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	results := make([]v1.VerifyTestResult, 0, 256)
	for i := 0; i < 256; i++ {
		results = append(results, v1.VerifyTestResult{TestID: fmt.Sprintf("test-%03d", i)})
	}

	apiErr := svc.updateVerifyWork(context.Background(), "project.alpha", verifySelectionContext{
		ReceiptID: "receipt.abc123",
		PlanKey:   "plan:receipt.abc123",
	}, "verify-batch-1", results, true)
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(repo.workUpsertCalls) != 1 {
		t.Fatalf("expected one work upsert call, got %d", len(repo.workUpsertCalls))
	}
	got := repo.workUpsertCalls[0].Items[0].Evidence
	if len(got) != maxVerifyWorkEvidenceEntries {
		t.Fatalf("unexpected evidence count: got %d want %d", len(got), maxVerifyWorkEvidenceEntries)
	}
	if got[0] != "verifyrun:verify-batch-1" {
		t.Fatalf("unexpected first evidence entry: %q", got[0])
	}
}

func TestVerify_InjectsDerivedPlanKeyIntoCommandEnv(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	testsYAML := `version: awm.tests.v1
tests:
  - id: smoke
    summary: Run smoke verification
    command:
      argv: ["echo", "noop"]
    select:
      always_run: true
`
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-tests.yaml"), []byte(testsYAML), 0o644); err != nil {
		t.Fatalf("write tests file: %v", err)
	}

	withWorkingDir(t, root)

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID: "project.alpha",
			ReceiptID: "receipt.abc123",
			Phase:     "execute",
		}},
		workUpsertResults: []int{1},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	var gotEnv map[string]string
	svc.runVerifyCommand = func(_ context.Context, _ string, _ verifyTestDefinition, extraEnv map[string]string) verifyCommandRun {
		gotEnv = make(map[string]string, len(extraEnv))
		for key, value := range extraEnv {
			gotEnv[key] = value
		}
		exitCode := 0
		now := time.Now().UTC()
		return verifyCommandRun{
			ExitCode:   &exitCode,
			Stdout:     "smoke ok\n",
			StartedAt:  now,
			FinishedAt: now.Add(100 * time.Millisecond),
		}
	}

	result, apiErr := svc.Verify(context.Background(), v1.VerifyPayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Status != v1.VerifyStatusPassed || !result.Passed {
		t.Fatalf("unexpected verify result: %+v", result)
	}
	if gotEnv["AWM_RECEIPT_ID"] != "receipt.abc123" {
		t.Fatalf("unexpected injected AWM_RECEIPT_ID: %+v", gotEnv)
	}
	if gotEnv["AWM_PLAN_KEY"] != "plan:receipt.abc123" {
		t.Fatalf("unexpected derived AWM_PLAN_KEY: %+v", gotEnv)
	}
	if gotEnv["AWM_VERIFY_PHASE"] != "execute" {
		t.Fatalf("unexpected derived AWM_VERIFY_PHASE: %+v", gotEnv)
	}
	if gotEnv["AWM_VERIFY_TAGS_JSON"] != "[]" {
		t.Fatalf("unexpected derived AWM_VERIFY_TAGS_JSON: %+v", gotEnv)
	}
	if gotEnv["AWM_VERIFY_FILES_CHANGED_JSON"] != "[]" {
		t.Fatalf("unexpected derived AWM_VERIFY_FILES_CHANGED_JSON: %+v", gotEnv)
	}
}

func TestVerify_RejectsAlwaysRunCombinedWithSelectors(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	testsYAML := `version: awm.tests.v1
defaults:
  cwd: .
  timeout_sec: 45
tests:
  - id: invalid-default
    summary: Invalid always run selector mix
    command:
      argv: ["echo", "noop"]
    select:
      always_run: true
      phases: ["review"]
`
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-tests.yaml"), []byte(testsYAML), 0o644); err != nil {
		t.Fatalf("write tests file: %v", err)
	}

	withWorkingDir(t, root)

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{},
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Verify(context.Background(), v1.VerifyPayload{
		ProjectID: "project.alpha",
		Phase:     v1.PhaseReview,
	})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INVALID_INPUT" {
		t.Fatalf("unexpected error code: %s", apiErr.Code)
	}
}

func TestResolveProjectSourcePath_PreservesAbsoluteOverrides(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "repo")
	absolute := filepath.Join(t.TempDir(), "awm-tests.yaml")

	sourcePath, absolutePath, err := resolveProjectSourcePath(projectRoot, absolute)
	if err != nil {
		t.Fatalf("resolveProjectSourcePath returned error: %v", err)
	}
	if sourcePath != filepath.ToSlash(filepath.Clean(absolute)) {
		t.Fatalf("unexpected source path: got %q want %q", sourcePath, filepath.ToSlash(filepath.Clean(absolute)))
	}
	if absolutePath != filepath.Clean(absolute) {
		t.Fatalf("unexpected absolute path: got %q want %q", absolutePath, filepath.Clean(absolute))
	}
}

func TestRunVerifyCommand_AppliesCommandEnv(t *testing.T) {
	root := t.TempDir()
	def := verifyTestDefinition{
		Argv:       []string{os.Args[0], "-test.run=TestRunVerifyCommandHelperProcess", "--"},
		CWD:        ".",
		TimeoutSec: 5,
		Env: map[string]string{
			"GO_WANT_VERIFY_HELPER_PROCESS": "1",
			"AWM_VERIFY_ENV_CHECK":          "expected-value",
		},
	}

	run := runVerifyCommand(context.Background(), root, def, map[string]string{
		"AWM_RECEIPT_ID": "receipt.abc123",
		"AWM_PLAN_KEY":   "plan:receipt.abc123",
	})
	if run.Err != nil {
		t.Fatalf("unexpected command error: %v\nstdout=%q\nstderr=%q", run.Err, run.Stdout, run.Stderr)
	}
	if run.ExitCode == nil || *run.ExitCode != 0 {
		t.Fatalf("unexpected exit code: %+v", run.ExitCode)
	}
}

func TestRunVerifyCommand_DoesNotLoadDotEnvBackedAWMRuntimeEnv(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("AWM_PG_DSN=postgres://dotenv\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	def := verifyTestDefinition{
		Argv:       []string{os.Args[0], "-test.run=TestRunAWMCommandDotEnvHelperProcess", "--"},
		CWD:        ".",
		TimeoutSec: 5,
		Env: map[string]string{
			"GO_WANT_AWM_COMMAND_DOTENV_HELPER_PROCESS": "1",
			"AWM_EXPECTED_PG_DSN":                       "__EXPECT_EMPTY__",
			"AWM_EXPECTED_RECEIPT_ID":                   "receipt.abc123",
			"AWM_EXPECTED_PLAN_KEY":                     "plan:receipt.abc123",
		},
	}

	run := runVerifyCommand(context.Background(), root, def, map[string]string{
		"AWM_RECEIPT_ID": "receipt.abc123",
		"AWM_PLAN_KEY":   "plan:receipt.abc123",
	})
	if run.Err != nil {
		t.Fatalf("unexpected command error: %v\nstdout=%q\nstderr=%q", run.Err, run.Stdout, run.Stderr)
	}
	if run.ExitCode == nil || *run.ExitCode != 0 {
		t.Fatalf("unexpected exit code: %+v", run.ExitCode)
	}
}

func TestRunVerifyCommand_CommandEnvOverridesDotEnvBackedAWMRuntimeEnv(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("AWM_PG_DSN=postgres://dotenv\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	def := verifyTestDefinition{
		Argv:       []string{os.Args[0], "-test.run=TestRunAWMCommandDotEnvHelperProcess", "--"},
		CWD:        ".",
		TimeoutSec: 5,
		Env: map[string]string{
			"GO_WANT_AWM_COMMAND_DOTENV_HELPER_PROCESS": "1",
			"AWM_PG_DSN":          "postgres://command",
			"AWM_EXPECTED_PG_DSN": "postgres://command",
		},
	}

	run := runVerifyCommand(context.Background(), root, def, nil)
	if run.Err != nil {
		t.Fatalf("unexpected command error: %v\nstdout=%q\nstderr=%q", run.Err, run.Stdout, run.Stderr)
	}
	if run.ExitCode == nil || *run.ExitCode != 0 {
		t.Fatalf("unexpected exit code: %+v", run.ExitCode)
	}
}

func TestRunVerifyCommandHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_VERIFY_HELPER_PROCESS") != "1" {
		return
	}
	if got, want := os.Getenv("AWM_VERIFY_ENV_CHECK"), "expected-value"; got != want {
		fmt.Fprintf(os.Stderr, "unexpected env: got %q want %q\n", got, want)
		os.Exit(3)
	}
	if got, want := os.Getenv("AWM_RECEIPT_ID"), "receipt.abc123"; got != want {
		fmt.Fprintf(os.Stderr, "unexpected receipt env: got %q want %q\n", got, want)
		os.Exit(3)
	}
	if got, want := os.Getenv("AWM_PLAN_KEY"), "plan:receipt.abc123"; got != want {
		fmt.Fprintf(os.Stderr, "unexpected plan env: got %q want %q\n", got, want)
		os.Exit(3)
	}
	fmt.Fprintln(os.Stdout, "env ok")
	os.Exit(0)
}

func TestRunAWMCommandDotEnvHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_AWM_COMMAND_DOTENV_HELPER_PROCESS") != "1" {
		return
	}

	checks := map[string]string{
		"AWM_PG_DSN":     os.Getenv("AWM_EXPECTED_PG_DSN"),
		"AWM_RECEIPT_ID": os.Getenv("AWM_EXPECTED_RECEIPT_ID"),
		"AWM_PLAN_KEY":   os.Getenv("AWM_EXPECTED_PLAN_KEY"),
		"AWM_REVIEW_KEY": os.Getenv("AWM_EXPECTED_REVIEW_KEY"),
	}
	for key, want := range checks {
		if want == "" {
			continue
		}
		got := os.Getenv(key)
		if want == "__EXPECT_EMPTY__" {
			if got != "" {
				fmt.Fprintf(os.Stderr, "expected %s to be unset, got %q\n", key, got)
				os.Exit(3)
			}
			continue
		}
		if got != want {
			fmt.Fprintf(os.Stderr, "unexpected %s: got %q want %q\n", key, got, want)
			os.Exit(3)
		}
	}

	fmt.Fprintln(os.Stdout, "dotenv env ok")
	os.Exit(0)
}
