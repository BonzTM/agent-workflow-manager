package backend

import (
	"context"
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

func TestHealthCheck_DefaultsDeterministicOrderingAndCapping(t *testing.T) {
	maxFindings := 1
	repo := &fakeRepository{
		candidateResults: [][]core.CandidatePointer{{
			{
				Key:         "ptr:one",
				Path:        "internal/a.go",
				Label:       "duplicate",
				Description: "",
				Tags:        []string{"backend", "BAD_TAG"},
				IsStale:     true,
			},
			{
				Key:         "ptr:two",
				Path:        "internal/b.go",
				Label:       "duplicate",
				Description: "ok",
				Tags:        []string{"backend"},
			},
			{
				Key:         "ptr:three",
				Path:        "internal/c.go",
				Label:       "unique",
				Description: "",
				Tags:        []string{"bad tag"},
			},
		}},
		inventoryResults: []core.PointerInventory{
			{Path: "internal/a.go", IsStale: true},
			{Path: "internal/b.go"},
			{Path: "internal/c.go"},
		},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.runGitCommand = func(_ context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "ls-files --cached --others --exclude-standard":
			return "internal/a.go\ninternal/b.go\ninternal/c.go\n", nil
		case "ls-files --deleted":
			return "", nil
		default:
			t.Fatalf("unexpected git args: %v", args)
			return "", nil
		}
	}

	health, apiErr := svc.Health(context.Background(), v1.HealthPayload{
		ProjectID:           "project.alpha",
		MaxFindingsPerCheck: &maxFindings,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	result := health.Check
	if result == nil {
		t.Fatalf("expected check result, got %+v", health)
	}

	if result.Summary.OK {
		t.Fatalf("expected summary not ok: %+v", result.Summary)
	}
	if result.Summary.TotalFindings != 7 {
		t.Fatalf("unexpected total findings: got %d want 7", result.Summary.TotalFindings)
	}

	wantOrder := []string{
		"administrative_closeout_plans",
		"duplicate_labels",
		"empty_descriptions",
		"orphan_relations",
		"pending_quarantines",
		"stale_pointers",
		"stale_work_plans",
		"terminal_plan_status_drift",
		"unindexed_files",
		"unknown_tags",
	}
	gotOrder := make([]string, 0, len(result.Checks))
	for _, check := range result.Checks {
		gotOrder = append(gotOrder, check.Name)
		if len(check.Samples) > 1 {
			t.Fatalf("expected capped samples <=1 for %s, got %v", check.Name, check.Samples)
		}
	}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("unexpected check order: got %v want %v", gotOrder, wantOrder)
	}
	var duplicateLabels *v1.HealthCheckItem
	for i := range result.Checks {
		if result.Checks[i].Name == "duplicate_labels" {
			duplicateLabels = &result.Checks[i]
			break
		}
	}
	if duplicateLabels == nil || len(duplicateLabels.Samples) == 0 {
		t.Fatalf("expected include_details default to include samples for non-empty checks, got %+v", result.Checks)
	}
	if len(repo.candidateCalls) != 1 || !repo.candidateCalls[0].Unbounded {
		t.Fatalf("expected unbounded candidate health query, got %+v", repo.candidateCalls)
	}
}

func TestStatus_ReportsMissingCanonicalSources(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	svc, err := NewWithRuntimeStatus(&fakeRepository{}, root, RuntimeStatusSnapshot{
		Backend:                "sqlite",
		SQLitePath:             filepath.Join(root, ".awm", "context.db"),
		UsesImplicitSQLitePath: true,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Status(context.Background(), v1.StatusPayload{ProjectID: "project.alpha"})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Summary.Ready {
		t.Fatalf("expected missing sources to make status not ready")
	}
	if result.Project.Backend != "sqlite" {
		t.Fatalf("unexpected backend: %q", result.Project.Backend)
	}
	integrationIDs := make([]string, 0, len(result.Integrations))
	for _, integration := range result.Integrations {
		integrationIDs = append(integrationIDs, integration.ID)
	}
	for _, id := range []string{"starter-contract", "detailed-planning-enforcement", "verify-generic", "verify-go", "verify-python", "verify-rust", "verify-ts", "codex-hooks", "opencode-pack"} {
		if !containsString(integrationIDs, id) {
			t.Fatalf("expected integration %q in %+v", id, integrationIDs)
		}
	}
	missingCodes := make([]string, 0, len(result.Missing))
	for _, item := range result.Missing {
		missingCodes = append(missingCodes, item.Code)
	}
	for _, code := range []string{"rules_missing", "tags_missing", "tests_missing", "workflows_missing"} {
		if !containsString(missingCodes, code) {
			t.Fatalf("expected missing code %q in %+v", code, missingCodes)
		}
	}
}

func TestStatus_PreviewsContextAndLoadedSources(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	files := map[string]string{
		".awm/awm-rules.yaml":     "version: awm.rules.v1\nrules:\n  - summary: Keep tests green\n",
		".awm/awm-tags.yaml":      "version: awm.tags.v1\ncanonical_tags:\n  backend:\n    - svc\n",
		".awm/awm-tests.yaml":     "version: awm.tests.v1\ndefaults:\n  cwd: .\n  timeout_sec: 120\ntests:\n  - id: smoke\n    summary: Run smoke tests\n    command:\n      argv: [\"go\", \"test\", \"./...\"]\n",
		".awm/awm-workflows.yaml": "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: verify:tests\n",
	}
	for rel, contents := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	repo := &fakeRepository{
		candidateResults: [][]core.CandidatePointer{{
			candidate("rule:tests", ".awm/awm-rules.yaml", true, []string{"governance"}),
			candidate("code:status", "internal/service/backend/status.go", false, []string{"backend"}),
		}},
	}
	svc, err := NewWithRuntimeStatus(repo, root, RuntimeStatusSnapshot{
		Backend:                "sqlite",
		SQLitePath:             filepath.Join(root, ".awm", "context.db"),
		UsesImplicitSQLitePath: true,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Status(context.Background(), v1.StatusPayload{
		ProjectID: "project.alpha",
		TaskText:  "diagnose why context chose these pointers",
		Phase:     v1.PhaseExecute,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if !result.Summary.Ready {
		t.Fatalf("expected ready status, got missing %+v", result.Missing)
	}
	if result.Context == nil || result.Context.Status != "ok" {
		t.Fatalf("expected context preview, got %+v", result.Context)
	}
	if result.Context.RuleCount != 1 {
		t.Fatalf("expected 1 rule in context preview, got %+v", result.Context)
	}
	sourceKinds := make([]string, 0, len(result.Sources))
	for _, source := range result.Sources {
		sourceKinds = append(sourceKinds, source.Kind)
	}
	for _, kind := range []string{"rules", "tags", "tests", "workflows"} {
		if !containsString(sourceKinds, kind) {
			t.Fatalf("expected source kind %q in %+v", kind, result.Sources)
		}
	}
	if len(repo.candidateCalls) != 0 {
		t.Fatalf("did not expect indexed candidate queries for status preview, got %d", len(repo.candidateCalls))
	}
}

func TestStatus_WarnsAboutStaleAndAdministrativePlans(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	files := map[string]string{
		".awm/awm-rules.yaml":     "version: awm.rules.v1\nrules:\n  - summary: Keep tests green\n",
		".awm/awm-tags.yaml":      "version: awm.tags.v1\ncanonical_tags:\n  backend:\n    - svc\n",
		".awm/awm-tests.yaml":     "version: awm.tests.v1\ndefaults:\n  cwd: .\n  timeout_sec: 120\ntests: []\n",
		".awm/awm-workflows.yaml": "version: awm.workflows.v1\ncompletion:\n  required_tasks: []\n",
	}
	for rel, contents := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	now := time.Now().UTC()
	repo := &fakeRepository{
		workPlanListResults: [][]core.WorkPlanSummary{{
			{
				PlanKey:   "plan:receipt.stale",
				Status:    core.PlanStatusInProgress,
				ReceiptID: "receipt.stale",
				UpdatedAt: now.Add(-8 * 24 * time.Hour),
			},
			{
				PlanKey:   "plan:receipt.closeout",
				Status:    core.PlanStatusInProgress,
				ReceiptID: "receipt.closeout",
				UpdatedAt: now,
			},
		}},
		workPlanLookupResult: []core.WorkPlan{
			{
				ProjectID: "project.alpha",
				PlanKey:   "plan:receipt.stale",
				ReceiptID: "receipt.stale",
				Status:    core.PlanStatusInProgress,
				Tasks: []core.WorkItem{
					{ItemKey: "verify:tests", Status: core.WorkItemStatusComplete},
				},
			},
			{
				ProjectID: "project.alpha",
				PlanKey:   "plan:receipt.closeout",
				ReceiptID: "receipt.closeout",
				Status:    core.PlanStatusInProgress,
				Tasks: []core.WorkItem{
					{ItemKey: "strict-close", Summary: "Administrative closeout", Status: core.WorkItemStatusPending},
				},
			},
		},
	}
	svc, err := NewWithRuntimeStatus(repo, root, RuntimeStatusSnapshot{
		Backend:                "sqlite",
		SQLitePath:             filepath.Join(root, ".awm", "context.db"),
		UsesImplicitSQLitePath: true,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Status(context.Background(), v1.StatusPayload{ProjectID: "project.alpha"})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Summary.WarningCount != 3 {
		t.Fatalf("unexpected warning count: got %d want 3", result.Summary.WarningCount)
	}
	warningCodes := make([]string, 0, len(result.Warnings))
	for _, item := range result.Warnings {
		warningCodes = append(warningCodes, item.Code)
	}
	for _, code := range []string{"stale_work_plan", "terminal_plan_status_drift", "administrative_closeout_plan"} {
		if !containsString(warningCodes, code) {
			t.Fatalf("expected warning code %q in %+v", code, result.Warnings)
		}
	}
}

func TestHealthCheck_IncludeDetailsFalseOmitsSamples(t *testing.T) {
	repo := &fakeRepository{
		candidateResults: [][]core.CandidatePointer{{
			{
				Key:         "ptr:one",
				Path:        "internal/a.go",
				Label:       "duplicate",
				Description: "",
				IsStale:     true,
			},
			{
				Key:         "ptr:two",
				Path:        "internal/b.go",
				Label:       "duplicate",
				Description: "",
			},
		}},
		inventoryResults: []core.PointerInventory{
			{Path: "internal/a.go", IsStale: true},
			{Path: "internal/b.go"},
		},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.runGitCommand = func(_ context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "ls-files --cached --others --exclude-standard":
			return "internal/a.go\ninternal/b.go\n", nil
		case "ls-files --deleted":
			return "", nil
		default:
			t.Fatalf("unexpected git args: %v", args)
			return "", nil
		}
	}

	includeDetails := false
	health, apiErr := svc.Health(context.Background(), v1.HealthPayload{
		ProjectID:      "project.alpha",
		IncludeDetails: &includeDetails,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	result := health.Check
	if result == nil {
		t.Fatalf("expected check result, got %+v", health)
	}

	for _, check := range result.Checks {
		if len(check.Samples) != 0 {
			t.Fatalf("expected no samples when include_details=false, got check=%s samples=%v", check.Name, check.Samples)
		}
	}
}

func TestHealthCheck_EmptyIndexFlagsUnindexedFiles(t *testing.T) {
	repo := &fakeRepository{
		candidateResults: [][]core.CandidatePointer{{}},
		inventoryResults: []core.PointerInventory{},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.runGitCommand = func(_ context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "ls-files --cached --others --exclude-standard":
			return "README.md\ninternal/service/backend/service.go\n", nil
		case "ls-files --deleted":
			return "", nil
		default:
			t.Fatalf("unexpected git args: %v", args)
			return "", nil
		}
	}

	health, apiErr := svc.Health(context.Background(), v1.HealthPayload{
		ProjectID: "project.alpha",
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	result := health.Check
	if result == nil {
		t.Fatalf("expected check result, got %+v", health)
	}
	if result.Summary.OK {
		t.Fatalf("expected non-ok summary for empty index: %+v", result.Summary)
	}
	var unindexed *v1.HealthCheckItem
	for i := range result.Checks {
		if result.Checks[i].Name == "unindexed_files" {
			unindexed = &result.Checks[i]
			break
		}
	}
	if unindexed == nil {
		t.Fatalf("expected unindexed_files check in %+v", result.Checks)
	}
	if unindexed.Count != 2 {
		t.Fatalf("unexpected unindexed count: got %d want 2", unindexed.Count)
	}
	if len(unindexed.Samples) == 0 {
		t.Fatalf("expected unindexed file samples, got %+v", unindexed)
	}
}

func TestHealthCheck_RepositoryErrorMapsInternalError(t *testing.T) {
	repo := &fakeRepository{
		candidateErrors: []error{errors.New("candidate query failed")},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Health(context.Background(), v1.HealthPayload{ProjectID: "project.alpha"})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INTERNAL_ERROR" {
		t.Fatalf("unexpected error code: %s", apiErr.Code)
	}
	details, ok := apiErr.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected map details, got %T", apiErr.Details)
	}
	if details["operation"] != "fetch_candidate_pointers" {
		t.Fatalf("unexpected operation detail: %#v", details)
	}
}

func TestBuildIndexedPointerStubs_UsesRepoLocalCanonicalTags(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-tags.yaml"), []byte("version: awm.tags.v1\ncanonical_tags:\n  backend:\n    - svc\n"), 0o644); err != nil {
		t.Fatalf("write tags file: %v", err)
	}
	withWorkingDir(t, root)

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{"src/allowed.go"},
		}},
		saveResult: core.RunReceiptIDs{RunID: 110, ReceiptID: "receipt.abc123"},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	tagNormalizer, err := svc.loadCanonicalTagNormalizer(root, "")
	if err != nil {
		t.Fatalf("load canonical tags: %v", err)
	}
	stubs := buildIndexedPointerStubs("project.alpha", []v1.CompletionViolation{{
		Path:   "src/svc/new.go",
		Reason: "index unindexed file",
	}}, tagNormalizer)
	if len(stubs) != 1 {
		t.Fatalf("expected one indexed stub, got %+v", stubs)
	}
	if stubs[0].PointerKey != "project.alpha:src/svc/new.go" {
		t.Fatalf("unexpected pointer key: %q", stubs[0].PointerKey)
	}
	if stubs[0].Kind != "code" {
		t.Fatalf("unexpected stub kind: %q", stubs[0].Kind)
	}
	wantTags := []string{"backend", "code", "indexed", "new", "src"}
	if !reflect.DeepEqual(stubs[0].Tags, wantTags) {
		t.Fatalf("unexpected indexed tags: got %v want %v", stubs[0].Tags, wantTags)
	}
}

func TestComputeInventoryHealth_ComputesSummaryAndDetails(t *testing.T) {
	repo := &fakeRepository{
		inventoryResults: []core.PointerInventory{
			{Path: "src/covered.go", IsStale: false},
			{Path: "src/stale.go", IsStale: true},
			{Path: "docs/old.md", IsStale: true},
		},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	svc.runGitCommand = func(_ context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "ls-files --cached --others --exclude-standard":
			return "src/covered.go\nsrc/stale.go\nsrc/unindexed.go\ncmd/tool/main.go\n", nil
		case "ls-files --deleted":
			return "", nil
		default:
			t.Fatalf("unexpected git args: %v", args)
			return "", nil
		}
	}

	result, apiErr := svc.computeInventoryHealth(context.Background(), "project.alpha", "")
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(repo.inventoryCalls) != 1 || repo.inventoryCalls[0] != "project.alpha" {
		t.Fatalf("unexpected inventory calls: %v", repo.inventoryCalls)
	}
	if result.Summary.TotalFiles != 4 || result.Summary.IndexedFiles != 2 || result.Summary.UnindexedFiles != 2 || result.Summary.StaleFiles != 1 {
		t.Fatalf("unexpected inventory summary: %+v", result.Summary)
	}
	if !reflect.DeepEqual(result.UnindexedPaths, []string{"cmd/tool/main.go", "src/unindexed.go"}) {
		t.Fatalf("unexpected unindexed paths: %v", result.UnindexedPaths)
	}
	if !reflect.DeepEqual(result.StalePaths, []string{"src/stale.go"}) {
		t.Fatalf("unexpected stale paths: %v", result.StalePaths)
	}
	if !reflect.DeepEqual(result.UnindexedDirs, []string{"cmd/tool"}) {
		t.Fatalf("unexpected unindexed dirs: %v", result.UnindexedDirs)
	}
}

func TestComputeInventoryHealth_ExcludesManagedFilesFromTrackedSet(t *testing.T) {
	repo := &fakeRepository{
		inventoryResults: []core.PointerInventory{
			{Path: "src/covered.go", IsStale: false},
		},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	svc.runGitCommand = func(_ context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "ls-files --cached --others --exclude-standard":
			return ".gitignore\n.env.example\n.awm/awm-tests.yaml\n.awm/context.db-wal\nsrc/covered.go\nsrc/unindexed.go\n", nil
		case "ls-files --deleted":
			return "", nil
		default:
			t.Fatalf("unexpected git args: %v", args)
			return "", nil
		}
	}

	result, apiErr := svc.computeInventoryHealth(context.Background(), "project.alpha", "")
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Summary.TotalFiles != 2 || result.Summary.IndexedFiles != 1 || result.Summary.UnindexedFiles != 1 {
		t.Fatalf("unexpected inventory summary: %+v", result.Summary)
	}
	if !reflect.DeepEqual(result.UnindexedPaths, []string{"src/unindexed.go"}) {
		t.Fatalf("unexpected unindexed paths: %v", result.UnindexedPaths)
	}
}

func TestComputeInventoryHealth_ExcludesDeletedTrackedFiles(t *testing.T) {
	repo := &fakeRepository{
		inventoryResults: []core.PointerInventory{{Path: "src/live.go", IsStale: false}, {Path: "web/memories.html", IsStale: true}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	svc.runGitCommand = func(_ context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "ls-files --cached --others --exclude-standard":
			return "src/live.go\nweb/memories.html\n", nil
		case "ls-files --deleted":
			return "web/memories.html\n", nil
		default:
			t.Fatalf("unexpected git args: %v", args)
			return "", nil
		}
	}

	result, apiErr := svc.computeInventoryHealth(context.Background(), "project.alpha", "")
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Summary.TotalFiles != 1 || result.Summary.IndexedFiles != 1 || result.Summary.StaleFiles != 0 {
		t.Fatalf("unexpected inventory summary: %+v", result.Summary)
	}
	if len(result.StalePaths) != 0 {
		t.Fatalf("expected deleted tracked file to be excluded from stale paths, got %v", result.StalePaths)
	}
}

func TestComputeInventoryHealth_InventoryErrorMapsInternalError(t *testing.T) {
	repo := &fakeRepository{
		inventoryErrors: []error{errors.New("inventory failed")},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	svc.runGitCommand = func(_ context.Context, _ string, _ ...string) (string, error) {
		return "src/file.go\n", nil
	}

	_, apiErr := svc.computeInventoryHealth(context.Background(), "project.alpha", "")
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INTERNAL_ERROR" {
		t.Fatalf("unexpected API error code: %s", apiErr.Code)
	}
	details, ok := apiErr.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected map details, got %T", apiErr.Details)
	}
	if details["operation"] != "list_pointer_inventory" {
		t.Fatalf("unexpected operation detail: %#v", details)
	}
}
