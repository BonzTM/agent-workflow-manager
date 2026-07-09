package backend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func TestDone_AcceptsInScopeAndPersistsSummary(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			TaskText:          "implement completion path",
			Phase:             "execute",
			ResolvedTags:      []string{"backend"},
			PointerKeys:       []string{"code:repo"},
			InitialScopePaths: []string{"internal/service/backend/service.go", "internal/core/repository.go"},
		}},
		saveResult: core.RunReceiptIDs{RunID: 42, ReceiptID: "receipt.abc123"},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{"internal/core/repository.go"},
		Outcome:      "completed",
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Accepted {
		t.Fatalf("expected accepted result: %+v", result)
	}
	if result.RunID != 42 {
		t.Fatalf("unexpected run id: got %d want 42", result.RunID)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("expected no violations, got %+v", result.Violations)
	}
	if len(repo.scopeCalls) != 1 {
		t.Fatalf("expected one scope lookup, got %d", len(repo.scopeCalls))
	}
	if len(repo.saveCalls) != 1 {
		t.Fatalf("expected one save call, got %d", len(repo.saveCalls))
	}
	if repo.saveCalls[0].Status != "accepted_with_warnings" {
		t.Fatalf("unexpected status persisted: %q", repo.saveCalls[0].Status)
	}
	wantIssues := []string{"required verification work item is missing: verify:tests"}
	if !reflect.DeepEqual(result.DefinitionOfDoneIssues, wantIssues) {
		t.Fatalf("unexpected DoD issues: got %v want %v", result.DefinitionOfDoneIssues, wantIssues)
	}
	if !reflect.DeepEqual(repo.saveCalls[0].DefinitionOfDoneIssues, wantIssues) {
		t.Fatalf("unexpected persisted DoD issues: got %v want %v", repo.saveCalls[0].DefinitionOfDoneIssues, wantIssues)
	}
	if repo.saveCalls[0].TaskText != "implement completion path" || repo.saveCalls[0].Phase != "execute" {
		t.Fatalf("expected scope metadata in persisted summary, got task=%q phase=%q", repo.saveCalls[0].TaskText, repo.saveCalls[0].Phase)
	}
}

func TestDone_WarnModeCrossChecksSuppliedFilesAgainstDetectedDelta(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "src/allowed.go", "package src\n\nfunc allowed() {}\n")
	withWorkingDir(t, root)

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{"src/allowed.go"},
			BaselineCaptured:  true,
			BaselinePaths: []core.SyncPath{{
				Path:        "src/allowed.go",
				ContentHash: "stale-hash",
			}},
		}},
		workListResults: [][]core.WorkItem{{
			{ItemKey: "verify:tests", Status: core.WorkItemStatusComplete},
		}},
		saveResult: core.RunReceiptIDs{RunID: 55, ReceiptID: "receipt.abc123"},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.runGitCommand = func(_ context.Context, projectRoot string, args ...string) (string, error) {
		if projectRoot != root && projectRoot != "." {
			t.Fatalf("unexpected project root: %q", projectRoot)
		}
		switch strings.Join(args, " ") {
		case "diff --name-status --find-renames HEAD":
			return "M\tsrc/allowed.go\n", nil
		case "ls-files --others --exclude-standard":
			return "", nil
		default:
			t.Fatalf("unexpected git args: %v", args)
		}
		return "", nil
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{"src/allowed.go", "src/extra.go"},
		Outcome:      "completed with explicit list",
		ScopeMode:    v1.ScopeModeWarn,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Accepted {
		t.Fatalf("expected accepted warn-mode result: %+v", result)
	}
	wantViolations := []v1.CompletionViolation{{
		Path:   "src/extra.go",
		Reason: "path was supplied in files_changed but was not detected in the receipt baseline delta",
	}}
	if !reflect.DeepEqual(result.Violations, wantViolations) {
		t.Fatalf("unexpected violations: got %+v want %+v", result.Violations, wantViolations)
	}
	if len(repo.saveCalls) != 1 {
		t.Fatalf("expected one persisted run summary, got %d", len(repo.saveCalls))
	}
	if got, want := repo.saveCalls[0].FilesChanged, []string{"src/allowed.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected persisted files_changed: got %v want %v", got, want)
	}
}

func TestDone_StrictModeRejectsDetectedFilesOmittedFromSuppliedList(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "src/one.go", "package src\n\nfunc one() {}\n")
	writeRepoFile(t, root, "src/two.go", "package src\n\nfunc two() {}\n")
	withWorkingDir(t, root)

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{"src/one.go", "src/two.go"},
			BaselineCaptured:  true,
			BaselinePaths: []core.SyncPath{
				{Path: "src/one.go", ContentHash: "baseline-one"},
				{Path: "src/two.go", ContentHash: "baseline-two"},
			},
		}},
		workListResults: [][]core.WorkItem{{
			{ItemKey: "verify:tests", Status: core.WorkItemStatusComplete},
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.runGitCommand = func(_ context.Context, projectRoot string, args ...string) (string, error) {
		if projectRoot != root && projectRoot != "." {
			t.Fatalf("unexpected project root: %q", projectRoot)
		}
		switch strings.Join(args, " ") {
		case "diff --name-status --find-renames HEAD":
			return "M\tsrc/one.go\nM\tsrc/two.go\n", nil
		case "ls-files --others --exclude-standard":
			return "", nil
		default:
			t.Fatalf("unexpected git args: %v", args)
		}
		return "", nil
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{"src/one.go"},
		Outcome:      "completed with incomplete explicit list",
		ScopeMode:    v1.ScopeModeStrict,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Accepted {
		t.Fatalf("expected strict-mode rejection: %+v", result)
	}
	wantViolations := []v1.CompletionViolation{{
		Path:   "src/two.go",
		Reason: "path was detected in the receipt baseline delta but was omitted from files_changed",
	}}
	if !reflect.DeepEqual(result.Violations, wantViolations) {
		t.Fatalf("unexpected violations: got %+v want %+v", result.Violations, wantViolations)
	}
	if len(repo.saveCalls) != 0 {
		t.Fatalf("did not expect persisted run summary on rejection, got %d", len(repo.saveCalls))
	}
}

func TestDone_OmittedFilesChangedUsesDetectedDelta(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "src/allowed.go", "package src\n\nfunc allowed() {}\n")
	withWorkingDir(t, root)

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{"src/allowed.go"},
			BaselineCaptured:  true,
			BaselinePaths: []core.SyncPath{{
				Path:        "src/allowed.go",
				ContentHash: "baseline",
			}},
		}},
		workListResults: [][]core.WorkItem{{
			{ItemKey: "verify:tests", Status: core.WorkItemStatusComplete},
		}},
		saveResult: core.RunReceiptIDs{RunID: 56, ReceiptID: "receipt.abc123"},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.runGitCommand = func(_ context.Context, projectRoot string, args ...string) (string, error) {
		if projectRoot != root && projectRoot != "." {
			t.Fatalf("unexpected project root: %q", projectRoot)
		}
		switch strings.Join(args, " ") {
		case "diff --name-status --find-renames HEAD":
			return "M\tsrc/allowed.go\n", nil
		case "ls-files --others --exclude-standard":
			return "", nil
		default:
			t.Fatalf("unexpected git args: %v", args)
		}
		return "", nil
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		Outcome:   "completed with detected delta only",
		ScopeMode: v1.ScopeModeStrict,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Accepted {
		t.Fatalf("expected acceptance with detected delta: %+v", result)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("expected no violations, got %+v", result.Violations)
	}
	if len(repo.saveCalls) != 1 {
		t.Fatalf("expected one persisted run summary, got %d", len(repo.saveCalls))
	}
	if got, want := repo.saveCalls[0].FilesChanged, []string{"src/allowed.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected persisted files_changed: got %v want %v", got, want)
	}
}

func TestDone_ExplicitNoFileChangesRejectsDetectedDelta(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "src/allowed.go", "package src\n\nfunc allowed() {}\n")
	withWorkingDir(t, root)

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{"src/allowed.go"},
			BaselineCaptured:  true,
			BaselinePaths: []core.SyncPath{{
				Path:        "src/allowed.go",
				ContentHash: "baseline",
			}},
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.runGitCommand = func(_ context.Context, projectRoot string, args ...string) (string, error) {
		if projectRoot != root && projectRoot != "." {
			t.Fatalf("unexpected project root: %q", projectRoot)
		}
		switch strings.Join(args, " ") {
		case "diff --name-status --find-renames HEAD":
			return "M\tsrc/allowed.go\n", nil
		case "ls-files --others --exclude-standard":
			return "", nil
		default:
			t.Fatalf("unexpected git args: %v", args)
		}
		return "", nil
	}

	_, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:     "project.alpha",
		ReceiptID:     "receipt.abc123",
		NoFileChanges: true,
		Outcome:       "completed with explicit no-file declaration",
	})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INVALID_INPUT" {
		t.Fatalf("unexpected API error code: %s", apiErr.Code)
	}
}

func TestDone_StrictModeRejectsOutOfScopeWithoutPersistence(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{"internal/service/backend/service.go"},
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{"README.md", "internal/service/backend/service.go"},
		Outcome:      "completed",
		ScopeMode:    v1.ScopeModeStrict,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Accepted {
		t.Fatalf("expected rejection: %+v", result)
	}
	wantViolations := []v1.CompletionViolation{{
		Path:   "README.md",
		Reason: "path is outside effective scope",
	}}
	if !reflect.DeepEqual(result.Violations, wantViolations) {
		t.Fatalf("unexpected violations: got %+v want %+v", result.Violations, wantViolations)
	}
	if len(repo.saveCalls) != 0 {
		t.Fatalf("did not expect save on rejection, got %d", len(repo.saveCalls))
	}
}

func TestDone_StrictModeAcceptsRootGovernanceContractsInManagedScope(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{"internal/service/backend/service.go"},
		}},
		workListResults: [][]core.WorkItem{{
			{ItemKey: "verify:tests", Status: core.WorkItemStatusComplete},
		}},
		saveResult: core.RunReceiptIDs{RunID: 77, ReceiptID: "receipt.abc123"},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{"AGENTS.md", "CLAUDE.md"},
		Outcome:      "completed",
		ScopeMode:    v1.ScopeModeStrict,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Accepted {
		t.Fatalf("expected acceptance: %+v", result)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("unexpected violations: %+v", result.Violations)
	}
	if len(repo.saveCalls) != 1 {
		t.Fatalf("expected one save call, got %d", len(repo.saveCalls))
	}
	if got := repo.saveCalls[0].FilesChanged; !reflect.DeepEqual(got, []string{"AGENTS.md", "CLAUDE.md"}) {
		t.Fatalf("unexpected persisted files_changed: got %v", got)
	}
}

func TestDone_PlanKeyOnlyUsesDerivedReceiptIDAndDiscoveredPaths(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{"src/initial.go"},
		}},
		workPlanLookupResult: []core.WorkPlan{{
			ProjectID:       "project.alpha",
			PlanKey:         "plan:receipt.abc123",
			ReceiptID:       "receipt.abc123",
			DiscoveredPaths: []string{"src/discovered.go"},
		}},
		workListResults: [][]core.WorkItem{{
			{ItemKey: "verify:tests", Status: core.WorkItemStatusComplete},
		}},
		saveResult: core.RunReceiptIDs{RunID: 88, ReceiptID: "receipt.abc123"},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		PlanKey:      "plan:receipt.abc123",
		FilesChanged: []string{"src/discovered.go"},
		Outcome:      "completed with plan-only context",
		ScopeMode:    v1.ScopeModeStrict,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Accepted {
		t.Fatalf("expected acceptance, got %+v", result)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("expected no violations, got %+v", result.Violations)
	}
	if len(repo.scopeCalls) != 1 || repo.scopeCalls[0].ReceiptID != "receipt.abc123" {
		t.Fatalf("expected derived receipt scope lookup, got %+v", repo.scopeCalls)
	}
	if len(repo.workListCalls) != 1 || repo.workListCalls[0].ReceiptID != "receipt.abc123" {
		t.Fatalf("expected derived receipt work-item lookup, got %+v", repo.workListCalls)
	}
	if len(repo.workPlanLookupCalls) != 1 || repo.workPlanLookupCalls[0].PlanKey != "plan:receipt.abc123" {
		t.Fatalf("unexpected plan lookup calls: %+v", repo.workPlanLookupCalls)
	}
	if len(repo.saveCalls) != 1 || repo.saveCalls[0].ReceiptID != "receipt.abc123" {
		t.Fatalf("expected persisted summary to use derived receipt id, got %+v", repo.saveCalls)
	}
}

func TestDone_StrictModeAcceptsManagedAwmFiles(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{"internal/core/repository.go"},
		}},
		workListResults: [][]core.WorkItem{{
			{ItemKey: "verify:tests", Status: core.WorkItemStatusComplete},
		}},
		saveResult: core.RunReceiptIDs{RunID: 72, ReceiptID: "receipt.abc123"},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		FilesChanged: []string{
			".awm/awm-rules.yaml",
			".awm/awm-tags.yaml",
			".awm/awm-tests.yaml",
			".awm/awm-workflows.yaml",
			".gitignore",
		},
		Outcome:   "updated AWM-managed onboarding files",
		ScopeMode: v1.ScopeModeStrict,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Accepted {
		t.Fatalf("expected strict-mode acceptance for AWM-managed files: %+v", result)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("expected no scope violations, got %+v", result.Violations)
	}
	if len(repo.saveCalls) != 1 {
		t.Fatalf("expected one persisted run summary, got %d", len(repo.saveCalls))
	}
}

func TestDone_StrictModeAcceptsPlanInScopePaths(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{"src/initial.go"},
		}},
		workPlanLookupResult: []core.WorkPlan{{
			ProjectID: "project.alpha",
			PlanKey:   "plan:receipt.abc123",
			ReceiptID: "receipt.abc123",
			InScope:   []string{"README.md", "docs"},
		}},
		workListResults: [][]core.WorkItem{{
			{ItemKey: "verify:tests", Status: core.WorkItemStatusComplete},
		}},
		saveResult: core.RunReceiptIDs{RunID: 91, ReceiptID: "receipt.abc123"},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		PlanKey:      "plan:receipt.abc123",
		FilesChanged: []string{"README.md", "docs/getting-started.md"},
		Outcome:      "completed within plan-owned scope",
		ScopeMode:    v1.ScopeModeStrict,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Accepted {
		t.Fatalf("expected acceptance: %+v", result)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("did not expect scope violations: %+v", result.Violations)
	}
	if len(repo.saveCalls) != 1 {
		t.Fatalf("expected closeout persistence, got %d", len(repo.saveCalls))
	}
}

func TestDone_StrictModeAcceptsCompletedVerifyTestsWithoutDiffReview(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{"internal/core/repository.go"},
		}},
		workListResults: [][]core.WorkItem{{
			{ItemKey: "verify:tests", Status: core.WorkItemStatusComplete},
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{"internal/core/repository.go"},
		Outcome:      "completed",
		ScopeMode:    v1.ScopeModeStrict,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Accepted {
		t.Fatalf("expected strict-mode acceptance: %+v", result)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("expected no scope violations, got %+v", result.Violations)
	}
	if got, want := result.DefinitionOfDoneIssues, []string(nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected DoD issues: got %v want %v", got, want)
	}
	if len(repo.saveCalls) != 1 {
		t.Fatalf("expected one persisted run summary, got %d", len(repo.saveCalls))
	}
}

func TestDone_WarnModeAcceptsIncompleteDefinitionOfDoneAndPersistsWarning(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{"internal/core/repository.go"},
		}},
		workListResults: [][]core.WorkItem{{
			{ItemKey: "verify:tests", Status: core.WorkItemStatusInProgress},
		}},
		saveResult: core.RunReceiptIDs{RunID: 73, ReceiptID: "receipt.abc123"},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{"internal/core/repository.go"},
		Outcome:      "completed",
		ScopeMode:    v1.ScopeModeWarn,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Accepted {
		t.Fatalf("expected warn-mode acceptance: %+v", result)
	}
	wantIssues := []string{
		"required verification work item is not complete: verify:tests (status=in_progress)",
	}
	if !reflect.DeepEqual(result.DefinitionOfDoneIssues, wantIssues) {
		t.Fatalf("unexpected DoD issues: got %v want %v", result.DefinitionOfDoneIssues, wantIssues)
	}
	if len(repo.saveCalls) != 1 {
		t.Fatalf("expected one persisted run summary, got %d", len(repo.saveCalls))
	}
	if repo.saveCalls[0].Status != "accepted_with_warnings" {
		t.Fatalf("unexpected persisted status: %q", repo.saveCalls[0].Status)
	}
	if !reflect.DeepEqual(repo.saveCalls[0].DefinitionOfDoneIssues, wantIssues) {
		t.Fatalf("unexpected persisted DoD issues: got %v want %v", repo.saveCalls[0].DefinitionOfDoneIssues, wantIssues)
	}
}

func TestDone_NoWorkItemsWarnModeFlagsMissingVerify(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{"internal/core/repository.go"},
		}},
		workListResults: [][]core.WorkItem{{}},
		saveResult:      core.RunReceiptIDs{RunID: 58, ReceiptID: "receipt.abc123"},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{"internal/core/repository.go"},
		Outcome:      "completed",
		ScopeMode:    v1.ScopeModeWarn,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Accepted {
		t.Fatalf("expected acceptance in warn mode when no work items exist: %+v", result)
	}
	wantIssues := []string{
		"required verification work item is missing: verify:tests",
	}
	if !reflect.DeepEqual(result.DefinitionOfDoneIssues, wantIssues) {
		t.Fatalf("unexpected DoD issues without work items: got %v want %v", result.DefinitionOfDoneIssues, wantIssues)
	}
	if len(repo.saveCalls) != 1 {
		t.Fatalf("expected one persisted run summary, got %d", len(repo.saveCalls))
	}
	if repo.saveCalls[0].Status != "accepted_with_warnings" {
		t.Fatalf("unexpected persisted status when no work items exist: %q", repo.saveCalls[0].Status)
	}
	if !reflect.DeepEqual(repo.saveCalls[0].DefinitionOfDoneIssues, wantIssues) {
		t.Fatalf("unexpected persisted DoD issues without work items: got %v want %v", repo.saveCalls[0].DefinitionOfDoneIssues, wantIssues)
	}
}

func TestDone_NoWorkItemsStrictRejectsMissingVerify(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{"internal/core/repository.go"},
		}},
		workListResults: [][]core.WorkItem{{}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{"internal/core/repository.go"},
		Outcome:      "completed",
		ScopeMode:    v1.ScopeModeStrict,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Accepted {
		t.Fatalf("expected strict-mode rejection without verify work: %+v", result)
	}
	wantIssues := []string{
		"required verification work item is missing: verify:tests",
	}
	if !reflect.DeepEqual(result.DefinitionOfDoneIssues, wantIssues) {
		t.Fatalf("unexpected DoD issues: got %v want %v", result.DefinitionOfDoneIssues, wantIssues)
	}
	if len(repo.saveCalls) != 0 {
		t.Fatalf("expected no persisted run summary on strict rejection, got %d", len(repo.saveCalls))
	}
}

func TestDone_BlankWorkflowDefinitionsFallBackToDefaultVerifyGate(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte("version: awm.workflows.v1\ncompletion:\n  required_tasks: []\n"), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{"internal/core/repository.go"},
		}},
		workListResults: [][]core.WorkItem{{}},
	}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{"internal/core/repository.go"},
		Outcome:      "completed",
		ScopeMode:    v1.ScopeModeStrict,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Accepted {
		t.Fatalf("expected strict-mode rejection without verify work: %+v", result)
	}
	wantIssues := []string{"required verification work item is missing: verify:tests"}
	if !reflect.DeepEqual(result.DefinitionOfDoneIssues, wantIssues) {
		t.Fatalf("unexpected DoD issues: got %v want %v", result.DefinitionOfDoneIssues, wantIssues)
	}
}

func TestDone_StrictModeRejectsMissingConfiguredWorkflowGate(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	workflowsYAML := "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: verify:tests\n      select:\n        changed_paths_any: [\"internal/**\"]\n    - key: review:cross-llm\n      select:\n        phases: [\"execute\", \"review\"]\n        changed_paths_any: [\"internal/**\"]\n"
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte(workflowsYAML), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			Phase:             "review",
			InitialScopePaths: []string{"internal/core/repository.go"},
		}},
		workListResults: [][]core.WorkItem{{
			{ItemKey: "verify:tests", Status: core.WorkItemStatusComplete},
		}},
	}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{"internal/core/repository.go"},
		Outcome:      "completed",
		ScopeMode:    v1.ScopeModeStrict,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Accepted {
		t.Fatalf("expected strict-mode rejection when configured review gate is missing: %+v", result)
	}
	wantIssues := []string{"required workflow work item is missing: review:cross-llm"}
	if !reflect.DeepEqual(result.DefinitionOfDoneIssues, wantIssues) {
		t.Fatalf("unexpected DoD issues: got %v want %v", result.DefinitionOfDoneIssues, wantIssues)
	}
}

func TestDone_ConfiguredWorkflowSelectorsCanNarrowRequiredGates(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	workflowsYAML := "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: review:cross-llm\n      select:\n        changed_paths_any: [\"internal/**\"]\n"
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte(workflowsYAML), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			Phase:             "review",
			InitialScopePaths: []string{"README.md"},
		}},
		workListResults: [][]core.WorkItem{{}},
		saveResult:      core.RunReceiptIDs{RunID: 88, ReceiptID: "receipt.abc123"},
	}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{"README.md"},
		Outcome:      "completed",
		ScopeMode:    v1.ScopeModeStrict,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Accepted {
		t.Fatalf("expected strict-mode acceptance when workflow selectors do not match: %+v", result)
	}
	if got, want := result.DefinitionOfDoneIssues, []string(nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected DoD issues: got %v want %v", got, want)
	}
	if len(repo.saveCalls) != 1 {
		t.Fatalf("expected one persisted run summary, got %d", len(repo.saveCalls))
	}
}

func TestDone_InvalidWorkflowDefinitionsReturnUserInputError(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	workflowsYAML := "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: review:cross-llm\n      run:\n        argv: [\"scripts/awm-cross-review.sh\"]\n        bad_field: true\n"
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte(workflowsYAML), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			Phase:             "review",
			InitialScopePaths: []string{"internal/core/repository.go"},
		}},
		workListResults: [][]core.WorkItem{{}},
	}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{"internal/core/repository.go"},
		Outcome:      "completed",
		ScopeMode:    v1.ScopeModeStrict,
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
}

func TestDone_ExplicitNoFileChangesAcceptsCleanlyInDefaultMode(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID: "project.alpha",
			ReceiptID: "receipt.abc123",
		}},
		saveResult: core.RunReceiptIDs{RunID: 74, ReceiptID: "receipt.abc123"},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:     "project.alpha",
		ReceiptID:     "receipt.abc123",
		NoFileChanges: true,
		Outcome:       "completed",
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Accepted {
		t.Fatalf("expected default-mode acceptance: %+v", result)
	}
	if got, want := result.DefinitionOfDoneIssues, []string(nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected DoD issues: got %v want %v", got, want)
	}
	if len(repo.saveCalls) != 1 {
		t.Fatalf("expected one persisted run summary, got %d", len(repo.saveCalls))
	}
	if repo.saveCalls[0].Status != "accepted" {
		t.Fatalf("unexpected persisted status: %q", repo.saveCalls[0].Status)
	}
}

func TestDone_ExplicitNoFileChangesStrictAcceptsWithoutDefaultVerifyGate(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID: "project.alpha",
			ReceiptID: "receipt.abc123",
		}},
		saveResult: core.RunReceiptIDs{RunID: 75, ReceiptID: "receipt.abc123"},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:     "project.alpha",
		ReceiptID:     "receipt.abc123",
		NoFileChanges: true,
		Outcome:       "completed",
		ScopeMode:     v1.ScopeModeStrict,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Accepted {
		t.Fatalf("expected strict-mode acceptance for legitimate no-file completion: %+v", result)
	}
	if got, want := result.DefinitionOfDoneIssues, []string(nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected DoD issues: got %v want %v", got, want)
	}
	if len(repo.saveCalls) != 1 {
		t.Fatalf("expected one persisted run summary on strict acceptance, got %d", len(repo.saveCalls))
	}
	if repo.saveCalls[0].Status != "accepted" {
		t.Fatalf("unexpected persisted status: %q", repo.saveCalls[0].Status)
	}
}

func TestDone_ExplicitNoFileChangesStillEnforcesMatchingWorkflowGate(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	workflowsYAML := "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: review:cross-llm\n      select:\n        always_run: true\n"
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), []byte(workflowsYAML), 0o644); err != nil {
		t.Fatalf("write workflows file: %v", err)
	}

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID: "project.alpha",
			ReceiptID: "receipt.abc123",
			Phase:     "review",
		}},
		saveResult: core.RunReceiptIDs{RunID: 76, ReceiptID: "receipt.abc123"},
	}
	svc, err := NewWithProjectRoot(repo, root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:     "project.alpha",
		ReceiptID:     "receipt.abc123",
		NoFileChanges: true,
		Outcome:       "planned",
		ScopeMode:     v1.ScopeModeStrict,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Accepted {
		t.Fatalf("expected strict-mode rejection when a matching no-file workflow gate is missing: %+v", result)
	}
	wantIssues := []string{"required workflow work item is missing: review:cross-llm"}
	if got, want := result.DefinitionOfDoneIssues, wantIssues; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected DoD issues: got %v want %v", got, want)
	}
	if len(repo.saveCalls) != 0 {
		t.Fatalf("expected no persisted run summary on strict rejection, got %d", len(repo.saveCalls))
	}
}

func TestDone_WithoutReliableBaselineRequiresExplicitIntent(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID: "project.alpha",
			ReceiptID: "receipt.abc123",
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		Outcome:   "completed",
	})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INVALID_INPUT" {
		t.Fatalf("unexpected API error code: %s", apiErr.Code)
	}
}

func TestDone_DefaultModeWarnAcceptsOutOfScopeAndPersistsSummary(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			TaskText:          "default warn scope check",
			Phase:             "execute",
			ResolvedTags:      []string{"backend"},
			PointerKeys:       []string{"code:repo"},
			InitialScopePaths: []string{"src/allowed.go"},
		}},
		saveResult: core.RunReceiptIDs{RunID: 64, ReceiptID: "receipt.abc123"},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{"src/allowed.go", "src/outside.go"},
		Outcome:      "completed",
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Accepted {
		t.Fatalf("expected accepted result in default warn mode: %+v", result)
	}
	if result.RunID != 64 {
		t.Fatalf("unexpected run id: got %d want 64", result.RunID)
	}
	if len(result.Violations) != 1 || result.Violations[0].Path != "src/outside.go" {
		t.Fatalf("unexpected violations: %+v", result.Violations)
	}
	if len(repo.saveCalls) != 1 {
		t.Fatalf("expected one persisted run summary, got %d", len(repo.saveCalls))
	}
	if repo.saveCalls[0].Status != "accepted_with_warnings" {
		t.Fatalf("unexpected persisted status: %q", repo.saveCalls[0].Status)
	}
}

func TestDone_UnknownReceiptReturnsNotFound(t *testing.T) {
	repo := &fakeRepository{
		scopeErrors: []error{core.ErrReceiptScopeNotFound},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.missing",
		FilesChanged: []string{"internal/core/repository.go"},
		Outcome:      "completed",
	})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "NOT_FOUND" {
		t.Fatalf("unexpected API error code: got %q want %q", apiErr.Code, "NOT_FOUND")
	}
	if len(repo.saveCalls) != 0 {
		t.Fatalf("did not expect save call, got %d", len(repo.saveCalls))
	}
}

func TestDone_FetchScopeErrorReturnsInternalError(t *testing.T) {
	repo := &fakeRepository{
		scopeErrors: []error{errors.New("db fetch failed")},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{"internal/core/repository.go"},
		Outcome:      "completed",
	})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INTERNAL_ERROR" {
		t.Fatalf("unexpected API error code: got %q want %q", apiErr.Code, "INTERNAL_ERROR")
	}
}

func TestDone_SaveErrorReturnsInternalError(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{"internal/core/repository.go"},
		}},
		saveError: errors.New("insert failed"),
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{"internal/core/repository.go"},
		Outcome:      "completed",
	})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INTERNAL_ERROR" {
		t.Fatalf("unexpected API error code: got %q want %q", apiErr.Code, "INTERNAL_ERROR")
	}
}

func TestDone_NormalizesFilesChangedDeterministically(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{"docs/readme.md", "src/pkg/file.go"},
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		FilesChanged: []string{
			" ./src//pkg/./file.go ",
			"docs\\readme.md",
			"src/pkg/../pkg/file.go",
			"docs/readme.md",
			" ",
		},
		Outcome: "completed",
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Accepted {
		t.Fatalf("expected accepted result: %+v", result)
	}
	if len(repo.saveCalls) != 1 {
		t.Fatalf("expected one save call, got %d", len(repo.saveCalls))
	}
	wantFiles := []string{"docs/readme.md", "src/pkg/file.go"}
	if !reflect.DeepEqual(repo.saveCalls[0].FilesChanged, wantFiles) {
		t.Fatalf("unexpected normalized files: got %v want %v", repo.saveCalls[0].FilesChanged, wantFiles)
	}
}

func TestDone_WarnModeAcceptsOutOfScopeAndPersistsSummary(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			TaskText:          "warn mode scope check",
			Phase:             "execute",
			ResolvedTags:      []string{"backend"},
			PointerKeys:       []string{"code:repo"},
			InitialScopePaths: []string{"src/allowed.go"},
		}},
		saveResult: core.RunReceiptIDs{RunID: 88, ReceiptID: "receipt.abc123"},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{"src/allowed.go", "src/outside.go"},
		Outcome:      "completed with exploratory touch",
		ScopeMode:    v1.ScopeModeWarn,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Accepted {
		t.Fatalf("expected accepted result in warn mode: %+v", result)
	}
	if result.RunID != 88 {
		t.Fatalf("unexpected run id: got %d want 88", result.RunID)
	}
	if len(result.Violations) != 1 || result.Violations[0].Path != "src/outside.go" {
		t.Fatalf("unexpected violations: %+v", result.Violations)
	}
	if len(repo.saveCalls) != 1 {
		t.Fatalf("expected one persisted run summary, got %d", len(repo.saveCalls))
	}
	if len(repo.upsertStubCalls) != 0 {
		t.Fatalf("did not expect pointer stub upserts in warn mode, got %+v", repo.upsertStubCalls)
	}
	if repo.saveCalls[0].Status != "accepted_with_warnings" {
		t.Fatalf("unexpected persisted status: %q", repo.saveCalls[0].Status)
	}
}

func TestDone_StrictModeAcceptsDirectoryScopedChildFile(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{"internal"},
		}},
		workListResults: [][]core.WorkItem{{
			{ItemKey: "verify:tests", Status: core.WorkItemStatusComplete},
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:    "project.alpha",
		ReceiptID:    "receipt.abc123",
		FilesChanged: []string{"internal/core/repository.go"},
		Outcome:      "completed",
		ScopeMode:    v1.ScopeModeStrict,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Accepted {
		t.Fatalf("expected strict-mode acceptance for directory-scoped child file: %+v", result)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("expected no scope violations, got %+v", result.Violations)
	}
}

func TestDone_StrictModeBlocksCompletedRunnableReviewWithoutPassingExecution(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
		t.Fatalf("mkdir internal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "review.txt"), []byte("draft\n"), 0o644); err != nil {
		t.Fatalf("write review file: %v", err)
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
			{ItemKey: v1.DefaultReviewTaskKey, Status: core.WorkItemStatusComplete, Outcome: "Manual note only"},
		}},
		reviewAttemptResults: [][]core.ReviewAttempt{nil},
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
		t.Fatalf("expected strict completion to reject runnable review without a passing execution: %+v", result)
	}
	if len(result.DefinitionOfDoneIssues) != 1 || !strings.Contains(result.DefinitionOfDoneIssues[0], "no passing execution") {
		t.Fatalf("unexpected definition_of_done_issues: %+v", result.DefinitionOfDoneIssues)
	}
}
