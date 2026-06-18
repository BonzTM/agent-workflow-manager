package backend

import (
	"context"
	"errors"
	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFetch_PlanKeyReturnsLookupSummary(t *testing.T) {
	repo := &fakeRepository{
		workPlanLookupResult: []core.WorkPlan{{
			ProjectID: "project.alpha",
			PlanKey:   "plan:receipt.abc123",
			ReceiptID: "receipt.abc123",
			Title:     "Execution plan",
			Status:    core.PlanStatusComplete,
			UpdatedAt: time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC),
			Tasks: []core.WorkItem{{
				ItemKey: "src/a.go",
				Status:  core.WorkItemStatusComplete,
			}},
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	key := "plan:receipt.abc123"
	result, apiErr := svc.Fetch(context.Background(), v1.FetchPayload{
		ProjectID: "project.alpha",
		Keys:      []string{key},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected one fetch item, got %+v", result)
	}
	item := result.Items[0]
	if item.Key != key || item.Type != "plan" || item.Status != core.PlanStatusComplete {
		t.Fatalf("unexpected fetch item: %+v", item)
	}
	if strings.TrimSpace(item.Version) == "" {
		t.Fatalf("expected non-empty plan version: %+v", item)
	}
	if !strings.Contains(item.Content, "\"plan_key\":\"plan:receipt.abc123\"") {
		t.Fatalf("expected serialized plan content, got %q", item.Content)
	}
	if len(result.NotFound) != 0 || len(result.VersionMismatches) != 0 {
		t.Fatalf("unexpected fetch metadata: %+v", result)
	}
	if len(repo.workPlanLookupCalls) != 1 {
		t.Fatalf("expected one plan lookup call, got %d", len(repo.workPlanLookupCalls))
	}
	if repo.workPlanLookupCalls[0].ProjectID != "project.alpha" || repo.workPlanLookupCalls[0].ReceiptID != "receipt.abc123" {
		t.Fatalf("unexpected plan lookup query: %+v", repo.workPlanLookupCalls[0])
	}
}

func TestFetch_TaskKeyReturnsStoredTaskContent(t *testing.T) {
	repo := &fakeRepository{
		workPlanLookupResult: []core.WorkPlan{{
			ProjectID: "project.alpha",
			PlanKey:   "plan:receipt.abc123",
			ReceiptID: "receipt.abc123",
			Tasks: []core.WorkItem{
				{
					ItemKey:            "task.alpha",
					Summary:            "Split provider adapter",
					Status:             core.WorkItemStatusInProgress,
					ParentTaskKey:      "task.epic",
					AcceptanceCriteria: []string{"Adapter compiles"},
					ExternalRefs:       []string{"jira:WEB-11"},
				},
			},
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	key := "task:plan:receipt.abc123#task.alpha"
	result, apiErr := svc.Fetch(context.Background(), v1.FetchPayload{
		ProjectID: "project.alpha",
		Keys:      []string{key},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected one fetch item, got %+v", result)
	}
	item := result.Items[0]
	if item.Key != key || item.Type != "task" || item.Status != core.WorkItemStatusInProgress {
		t.Fatalf("unexpected task fetch item: %+v", item)
	}
	if !strings.Contains(item.Content, "\"parent_task_key\":\"task.epic\"") {
		t.Fatalf("expected task content payload, got %q", item.Content)
	}
	if len(repo.workPlanLookupCalls) != 1 {
		t.Fatalf("expected one plan lookup call, got %d", len(repo.workPlanLookupCalls))
	}
	if repo.workPlanLookupCalls[0].PlanKey != "plan:receipt.abc123" {
		t.Fatalf("unexpected plan lookup query: %+v", repo.workPlanLookupCalls[0])
	}
}

func TestFetch_PlanKeyMissingReturnsNotFoundWithoutLegacyLookup(t *testing.T) {
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	key := "plan:receipt.abc123"
	result, apiErr := svc.Fetch(context.Background(), v1.FetchPayload{
		ProjectID: "project.alpha",
		Keys:      []string{key},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(result.Items) != 0 {
		t.Fatalf("expected zero fetch items, got %+v", result)
	}
	if !reflect.DeepEqual(result.NotFound, []string{key}) {
		t.Fatalf("unexpected not_found list: %+v", result.NotFound)
	}
	if len(repo.workPlanLookupCalls) != 1 {
		t.Fatalf("expected one plan lookup call, got %d", len(repo.workPlanLookupCalls))
	}
	if len(repo.fetchLookupCalls) != 0 {
		t.Fatalf("did not expect legacy fetch lookup calls, got %d", len(repo.fetchLookupCalls))
	}
}

func TestFetch_ReceiptAndRunKeysReturnStructuredHistoryContent(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:    "project.alpha",
			ReceiptID:    "receipt.abc123",
			TaskText:     "Trace onboarding history",
			Phase:        string(v1.PhaseExecute),
			ResolvedTags: []string{"backend"},
			PointerKeys:  []string{"code:pointer"},
		}},
		fetchLookupResults: []core.FetchLookup{{
			ProjectID:  "project.alpha",
			ReceiptID:  "receipt.abc123",
			RunID:      17,
			RunStatus:  "accepted",
			PlanStatus: core.PlanStatusInProgress,
			WorkItems: []core.WorkItem{
				{ItemKey: "verify:tests", Status: core.WorkItemStatusComplete},
			},
			UpdatedAt: time.Date(2026, 3, 6, 18, 4, 5, 0, time.UTC),
		}},
		runHistoryLookup: []core.RunHistorySummary{{
			RunID:        17,
			ReceiptID:    "receipt.abc123",
			RequestID:    "req-12345678",
			TaskText:     "Trace onboarding history",
			Phase:        string(v1.PhaseExecute),
			Status:       "accepted",
			FilesChanged: []string{"internal/service/backend/service.go"},
			Outcome:      "Captured receipt and run history",
			UpdatedAt:    time.Date(2026, 3, 6, 18, 4, 5, 0, time.UTC),
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Fetch(context.Background(), v1.FetchPayload{
		ProjectID: "project.alpha",
		Keys:      []string{"receipt:receipt.abc123", "run:17"},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected two fetch items, got %+v", result)
	}
	if result.Items[0].Type != "receipt" || !strings.Contains(result.Items[0].Content, "\"receipt_id\":\"receipt.abc123\"") || !strings.Contains(result.Items[0].Content, "\"baseline_captured\":false") {
		t.Fatalf("unexpected receipt fetch item: %+v", result.Items[0])
	}
	if result.Items[1].Type != "run" || !strings.Contains(result.Items[1].Content, "\"run_id\":17") || !strings.Contains(result.Items[1].Content, "\"files_changed\":[\"internal/service/backend/service.go\"]") {
		t.Fatalf("unexpected run fetch item: %+v", result.Items[1])
	}
}

func TestFetchPayloadKeysWithReceipt_DerivesPlanKeyWhenKeysOmitted(t *testing.T) {
	keys := fetchPayloadKeysWithReceipt(nil, " receipt.abc123 ")
	if !reflect.DeepEqual(keys, []string{"plan:receipt.abc123"}) {
		t.Fatalf("unexpected derived keys: %+v", keys)
	}
}

func TestFetchPayloadKeysWithReceipt_PreservesExplicitKeys(t *testing.T) {
	keys := fetchPayloadKeysWithReceipt([]string{" mem:42 ", "plan:receipt.abc123"}, "receipt.other")
	if !reflect.DeepEqual(keys, []string{"mem:42", "plan:receipt.abc123"}) {
		t.Fatalf("unexpected normalized keys: %+v", keys)
	}
}

func TestFetchPayloadKeys_DerivesPlanKeyFromPayloadReceiptID(t *testing.T) {
	keys := fetchPayloadKeys(v1.FetchPayload{
		ReceiptID: " receipt.abc123 ",
	})
	if !reflect.DeepEqual(keys, []string{"plan:receipt.abc123"}) {
		t.Fatalf("unexpected derived keys: %+v", keys)
	}
}

func TestParsePlanFetchKey_RejectsMixedCasePrefix(t *testing.T) {
	if _, ok := parsePlanFetchKey("PLAN:receipt.abc123"); ok {
		t.Fatal("expected PLAN: prefix to be rejected")
	}
}

func TestParsePlanFetchKey_RejectsInvalidReceiptID(t *testing.T) {
	if _, ok := parsePlanFetchKey("plan:short"); ok {
		t.Fatal("expected invalid receipt_id suffix to be rejected")
	}
}

func TestParseTaskFetchKey_ParsesPlanAndTask(t *testing.T) {
	ref, ok := parseTaskFetchKey("task:plan:receipt.abc123#task.alpha")
	if !ok {
		t.Fatal("expected task fetch key to parse")
	}
	if ref.PlanKey != "plan:receipt.abc123" || ref.ReceiptID != "receipt.abc123" || ref.TaskKey != "task.alpha" {
		t.Fatalf("unexpected task fetch ref: %+v", ref)
	}
}

func TestParseTaskFetchKey_RejectsInvalidPlanKey(t *testing.T) {
	if _, ok := parseTaskFetchKey("task:plan:short#task.alpha"); ok {
		t.Fatal("expected task fetch key with invalid plan receipt to be rejected")
	}
}

func TestTaskFetchKey_SkipsOverlongCompositeKeys(t *testing.T) {
	planKey := "plan:" + strings.Repeat("r", 32)
	taskKey := strings.Repeat("t", 600)
	if got := taskFetchKey(planKey, taskKey); got != "" {
		t.Fatalf("expected overlong task fetch key to be dropped, got %q", got)
	}
}

func TestReceiptAndRunFetchKeys(t *testing.T) {
	if got := receiptFetchKey(" receipt.abc123 "); got != "receipt:receipt.abc123" {
		t.Fatalf("unexpected receipt fetch key: %q", got)
	}
	if receiptID, ok := parseReceiptFetchKey("receipt:receipt.abc123"); !ok || receiptID != "receipt.abc123" {
		t.Fatalf("unexpected parsed receipt key: %q %v", receiptID, ok)
	}
	if _, ok := parseReceiptFetchKey("receipt:short"); ok {
		t.Fatal("expected invalid receipt fetch key to be rejected")
	}
	if got := runFetchKey(17); got != "run:17" {
		t.Fatalf("unexpected run fetch key: %q", got)
	}
	if runID, ok := parseRunFetchKey("run:17"); !ok || runID != 17 {
		t.Fatalf("unexpected parsed run key: %d %v", runID, ok)
	}
	if _, ok := parseRunFetchKey("run:not-a-number"); ok {
		t.Fatal("expected invalid run fetch key to be rejected")
	}
}

func TestFetch_PointerKeyReturnsContentWhenReadable(t *testing.T) {
	tmpDir := t.TempDir()
	pointerPath := filepath.Join(tmpDir, "pointer.txt")
	pointerContent := "pointer payload for fetch"
	if err := os.WriteFile(pointerPath, []byte(pointerContent), 0o644); err != nil {
		t.Fatalf("write pointer content: %v", err)
	}

	repo := &fakeRepository{
		pointerLookupResults: []core.CandidatePointer{
			candidate("code:pointer", pointerPath, false, []string{"backend"}),
		},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	key := "code:pointer"
	result, apiErr := svc.Fetch(context.Background(), v1.FetchPayload{
		ProjectID: "project.alpha",
		Keys:      []string{key},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected one fetch item, got %+v", result)
	}
	item := result.Items[0]
	if item.Key != key || item.Type != "pointer" {
		t.Fatalf("unexpected pointer fetch item identity: %+v", item)
	}
	if item.Content != pointerContent {
		t.Fatalf("unexpected pointer content: got %q want %q", item.Content, pointerContent)
	}
	if strings.TrimSpace(item.Summary) == "" {
		t.Fatalf("expected non-empty pointer summary: %+v", item)
	}
	if strings.TrimSpace(item.Version) == "" {
		t.Fatalf("expected non-empty pointer version: %+v", item)
	}
	if len(result.NotFound) != 0 || len(result.VersionMismatches) != 0 {
		t.Fatalf("unexpected fetch metadata: %+v", result)
	}
	if len(repo.pointerLookupCalls) != 1 {
		t.Fatalf("expected one pointer lookup call, got %d", len(repo.pointerLookupCalls))
	}
	if repo.pointerLookupCalls[0].ProjectID != "project.alpha" || repo.pointerLookupCalls[0].PointerKey != key {
		t.Fatalf("unexpected pointer lookup query: %+v", repo.pointerLookupCalls[0])
	}
}

func TestFetch_PointerKeyReadsRelativePathFromServiceProjectRoot(t *testing.T) {
	projectRoot := t.TempDir()
	pointerPath := filepath.Join(projectRoot, "docs", "pointer.txt")
	if err := os.MkdirAll(filepath.Dir(pointerPath), 0o755); err != nil {
		t.Fatalf("mkdir pointer dir: %v", err)
	}
	pointerContent := "pointer payload for fetch"
	if err := os.WriteFile(pointerPath, []byte(pointerContent), 0o644); err != nil {
		t.Fatalf("write pointer content: %v", err)
	}

	otherDir := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(otherDir); err != nil {
		t.Fatalf("chdir away from project root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	repo := &fakeRepository{
		pointerLookupResults: []core.CandidatePointer{
			candidate("code:pointer", "docs/pointer.txt", false, []string{"backend"}),
		},
	}
	svc, err := NewWithProjectRoot(repo, projectRoot)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Fetch(context.Background(), v1.FetchPayload{
		ProjectID: "project.alpha",
		Keys:      []string{"code:pointer"},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected one fetch item, got %+v", result)
	}
	if result.Items[0].Content != pointerContent {
		t.Fatalf("unexpected pointer content: got %q want %q", result.Items[0].Content, pointerContent)
	}
}

func TestFetch_UnknownKeyReturnsNotFound(t *testing.T) {
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Fetch(context.Background(), v1.FetchPayload{
		ProjectID: "project.alpha",
		Keys:      []string{"unknown:receipt.abc123"},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(result.Items) != 0 {
		t.Fatalf("expected zero fetch items, got %+v", result.Items)
	}
	if !reflect.DeepEqual(result.NotFound, []string{"unknown:receipt.abc123"}) {
		t.Fatalf("unexpected not_found: %+v", result.NotFound)
	}
	if len(repo.fetchLookupCalls) != 0 {
		t.Fatalf("did not expect fetch lookup call, got %d", len(repo.fetchLookupCalls))
	}
	if len(repo.pointerLookupCalls) != 1 {
		t.Fatalf("expected one pointer lookup for unknown pointer key, got %d", len(repo.pointerLookupCalls))
	}
}

func TestFetch_PointerWithMissingPathReturnsNotFound(t *testing.T) {
	repo := &fakeRepository{
		pointerLookupResults: []core.CandidatePointer{{
			Key:         "code:stale-pointer",
			Path:        "docs/missing-pointer.txt",
			Kind:        "doc",
			Label:       "stale-pointer",
			Description: "stale pointer",
			Tags:        []string{"docs"},
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Fetch(context.Background(), v1.FetchPayload{
		ProjectID: "project.alpha",
		Keys:      []string{"code:stale-pointer"},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(result.Items) != 0 {
		t.Fatalf("expected zero fetch items for missing pointer, got %+v", result.Items)
	}
	if !reflect.DeepEqual(result.NotFound, []string{"code:stale-pointer"}) {
		t.Fatalf("unexpected not_found: %+v", result.NotFound)
	}
}

func TestFetch_LookupErrorMapsInternalError(t *testing.T) {
	repo := &fakeRepository{
		workPlanLookupErrors: []error{errors.New("lookup failed")},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Fetch(context.Background(), v1.FetchPayload{
		ProjectID: "project.alpha",
		Keys:      []string{"plan:receipt.abc123"},
	})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INTERNAL_ERROR" {
		t.Fatalf("unexpected API error code: %q", apiErr.Code)
	}
	details, ok := apiErr.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected map details, got %T", apiErr.Details)
	}
	if details["operation"] != "lookup_work_plan" {
		t.Fatalf("unexpected operation detail: %#v", details)
	}
}

func TestFetch_ReceiptLookupErrorMapsLegacyOperation(t *testing.T) {
	repo := &fakeRepository{
		fetchLookupErrors: []error{errors.New("legacy lookup failed")},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Fetch(context.Background(), v1.FetchPayload{
		ProjectID: "project.alpha",
		Keys:      []string{"receipt:receipt.abc123"},
	})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INTERNAL_ERROR" {
		t.Fatalf("unexpected API error code: %q", apiErr.Code)
	}
	details, ok := apiErr.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected map details, got %T", apiErr.Details)
	}
	if details["operation"] != "lookup_fetch_state" {
		t.Fatalf("unexpected operation detail: %#v", details)
	}
}

func TestFetch_VersionMismatchIsReported(t *testing.T) {
	repo := &fakeRepository{
		workPlanLookupResult: []core.WorkPlan{{
			ProjectID: "project.alpha",
			PlanKey:   "plan:receipt.abc123",
			ReceiptID: "receipt.abc123",
			Status:    core.PlanStatusComplete,
			UpdatedAt: time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC),
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	key := "plan:receipt.abc123"
	result, apiErr := svc.Fetch(context.Background(), v1.FetchPayload{
		ProjectID: "project.alpha",
		Keys:      []string{key},
		ExpectedVersions: map[string]string{
			key: "99",
		},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(result.VersionMismatches) != 1 {
		t.Fatalf("expected one version mismatch, got %+v", result.VersionMismatches)
	}
	mismatch := result.VersionMismatches[0]
	if mismatch.Key != key || mismatch.Expected != "99" || strings.TrimSpace(mismatch.Actual) == "" || mismatch.Actual == "99" {
		t.Fatalf("unexpected version mismatch: %+v", mismatch)
	}
}
