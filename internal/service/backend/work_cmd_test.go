package backend

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func TestWork_PersistsCompletedWorkItemsAndDerivesPlanStatus(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{ProjectID: "project.alpha", ReceiptID: "receipt.abc123"}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Work(context.Background(), v1.WorkPayload{
		ProjectID: "project.alpha",
		PlanKey:   "plan:receipt.abc123",
		ReceiptID: "receipt.abc123",
		Tasks: []v1.WorkTaskPayload{
			{Key: "src/b.go", Status: v1.WorkItemStatusComplete},
			{Key: "src/a.go", Status: v1.WorkItemStatusComplete},
			{Key: "src/a.go", Status: v1.WorkItemStatusComplete},
		},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.PlanKey != "plan:receipt.abc123" || result.PlanStatus != core.PlanStatusComplete || result.Updated != 2 || result.TaskCount != 2 {
		t.Fatalf("unexpected work result: %+v", result)
	}
	if len(repo.fetchLookupCalls) != 0 {
		t.Fatalf("did not expect fetch lookup calls, got %d", len(repo.fetchLookupCalls))
	}
	if len(repo.workPlanUpsertCalls) != 2 {
		t.Fatalf("expected two work plan upsert calls after auto-close, got %d", len(repo.workPlanUpsertCalls))
	}
	if len(repo.workUpsertCalls) != 1 {
		t.Fatalf("expected one mirrored work upsert call, got %d", len(repo.workUpsertCalls))
	}
	if len(repo.workListCalls) != 0 {
		t.Fatalf("did not expect work list calls, got %d", len(repo.workListCalls))
	}

	call := repo.workPlanUpsertCalls[0]
	wantItems := []core.WorkItem{
		{ItemKey: "src/a.go", Status: core.WorkItemStatusComplete},
		{ItemKey: "src/b.go", Status: core.WorkItemStatusComplete},
	}
	if !reflect.DeepEqual(call.Tasks, wantItems) {
		t.Fatalf("unexpected work plan tasks: got %+v want %+v", call.Tasks, wantItems)
	}
	mirrorCall := repo.workUpsertCalls[0]
	if !reflect.DeepEqual(mirrorCall.Items, wantItems) {
		t.Fatalf("unexpected mirrored work items: got %+v want %+v", mirrorCall.Items, wantItems)
	}
	if call := repo.workPlanUpsertCalls[1]; call.Status != core.PlanStatusComplete || len(call.Tasks) != 0 {
		t.Fatalf("expected terminal status sync upsert, got %+v", call)
	}
}

func TestWork_AutoCloseSyncsStageMetadataFromStageTasks(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{ProjectID: "project.alpha", ReceiptID: "receipt.abc123"}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Work(context.Background(), v1.WorkPayload{
		ProjectID: "project.alpha",
		PlanKey:   "plan:receipt.abc123",
		ReceiptID: "receipt.abc123",
		Plan: &v1.WorkPlanPayload{
			Stages: &v1.WorkPlanStagesPayload{
				SpecOutline:        v1.WorkItemStatusInProgress,
				RefinedSpec:        v1.WorkItemStatusPending,
				ImplementationPlan: v1.WorkItemStatusPending,
			},
		},
		Tasks: []v1.WorkTaskPayload{
			{Key: "stage:spec-outline", Summary: "Spec outline", Status: v1.WorkItemStatusComplete},
			{Key: "stage:refined-spec", Summary: "Refined spec", Status: v1.WorkItemStatusComplete},
			{Key: "stage:implementation-plan", Summary: "Implementation plan", Status: v1.WorkItemStatusComplete},
			{Key: "impl:apply-fix", Summary: "Apply fix", Status: v1.WorkItemStatusComplete},
		},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.PlanStatus != core.PlanStatusComplete {
		t.Fatalf("unexpected plan status: %+v", result)
	}
	if len(repo.workPlanUpsertCalls) != 2 {
		t.Fatalf("expected two work plan upsert calls after auto-close, got %d", len(repo.workPlanUpsertCalls))
	}
	call := repo.workPlanUpsertCalls[1]
	if call.Status != core.PlanStatusComplete {
		t.Fatalf("expected terminal status sync upsert, got %+v", call)
	}
	wantStages := core.WorkPlanStages{
		SpecOutline:        core.WorkItemStatusComplete,
		RefinedSpec:        core.WorkItemStatusComplete,
		ImplementationPlan: core.WorkItemStatusComplete,
	}
	if !reflect.DeepEqual(call.Stages, wantStages) {
		t.Fatalf("expected terminal sync to reconcile stages: got %+v want %+v", call.Stages, wantStages)
	}
}

func TestWork_DerivesReceiptIDFromPlanKeyWhenReceiptIDOmitted(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{ProjectID: "project.alpha", ReceiptID: "receipt.abc123"}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Work(context.Background(), v1.WorkPayload{
		ProjectID: "project.alpha",
		PlanKey:   "plan:receipt.abc123",
		Tasks: []v1.WorkTaskPayload{
			{Key: "src/a.go", Status: v1.WorkItemStatusInProgress},
		},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.PlanKey != "plan:receipt.abc123" || result.PlanStatus != core.PlanStatusInProgress || result.Updated != 1 || result.TaskCount != 1 {
		t.Fatalf("unexpected work result: %+v", result)
	}
	if len(repo.fetchLookupCalls) != 0 {
		t.Fatalf("did not expect fetch lookup calls, got %d", len(repo.fetchLookupCalls))
	}
	if len(repo.workPlanUpsertCalls) != 1 {
		t.Fatalf("expected one work plan upsert call, got %d", len(repo.workPlanUpsertCalls))
	}
	if repo.workPlanUpsertCalls[0].ReceiptID != "receipt.abc123" {
		t.Fatalf("expected derived receipt id for plan upsert, got %q", repo.workPlanUpsertCalls[0].ReceiptID)
	}
	if len(repo.workUpsertCalls) != 1 {
		t.Fatalf("expected one mirrored work upsert call, got %d", len(repo.workUpsertCalls))
	}
	if repo.workUpsertCalls[0].ReceiptID != "receipt.abc123" {
		t.Fatalf("expected derived receipt id for mirrored upsert, got %q", repo.workUpsertCalls[0].ReceiptID)
	}
	if len(repo.workListCalls) != 0 {
		t.Fatalf("did not expect work list calls, got %d", len(repo.workListCalls))
	}
}

func TestWork_ReceiptIDOnlyAllowsStatusCheckAndDerivesPlanKey(t *testing.T) {
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Work(context.Background(), v1.WorkPayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.PlanKey != "plan:receipt.abc123" || result.PlanStatus != core.PlanStatusPending || result.Updated != 0 || result.TaskCount != 0 {
		t.Fatalf("unexpected work result: %+v", result)
	}
	if len(repo.workPlanUpsertCalls) != 1 {
		t.Fatalf("expected one work plan upsert call, got %d", len(repo.workPlanUpsertCalls))
	}
	if len(repo.workUpsertCalls) != 0 {
		t.Fatalf("did not expect work upsert calls, got %d", len(repo.workUpsertCalls))
	}
	if len(repo.workListCalls) != 0 {
		t.Fatalf("did not expect work list calls, got %d", len(repo.workListCalls))
	}
	upsert := repo.workPlanUpsertCalls[0]
	if upsert.PlanKey != "plan:receipt.abc123" || upsert.ReceiptID != "receipt.abc123" {
		t.Fatalf("unexpected plan upsert identifiers: %+v", upsert)
	}
}

func TestWork_UnknownReceiptWithTasksReturnsNotFoundWithoutPersistingPlan(t *testing.T) {
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Work(context.Background(), v1.WorkPayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.missing",
		Tasks: []v1.WorkTaskPayload{
			{Key: "impl:example", Summary: "Example task", Status: v1.WorkItemStatusPending},
		},
	})
	if apiErr == nil {
		t.Fatal("expected an API error for an unknown receipt")
	}
	if apiErr.Code != v1.ErrCodeNotFound {
		t.Fatalf("expected %s, got %s (%s)", v1.ErrCodeNotFound, apiErr.Code, apiErr.Message)
	}
	if apiErr.Message != "receipt scope was not found" {
		t.Fatalf("unexpected error message: %q", apiErr.Message)
	}
	if len(repo.workPlanUpsertCalls) != 0 {
		t.Fatalf("expected no plan upsert for an unknown receipt, got %d", len(repo.workPlanUpsertCalls))
	}
	if len(repo.workUpsertCalls) != 0 {
		t.Fatalf("expected no work item upsert for an unknown receipt, got %d", len(repo.workUpsertCalls))
	}
}

func TestWork_AutoOpensReceiptFromTaskText(t *testing.T) {
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Work(context.Background(), v1.WorkPayload{
		ProjectID: "project.alpha",
		TaskText:  "auto open a receipt from work",
		Tasks: []v1.WorkTaskPayload{
			{Key: "impl:auto-open", Summary: "Auto-open smoke", Status: v1.WorkItemStatusPending},
		},
	})
	if apiErr != nil {
		t.Fatalf("expected work to auto-open a receipt, got %+v", apiErr)
	}
	if !strings.HasPrefix(result.PlanKey, "plan:receipt-") {
		t.Fatalf("expected an auto-opened receipt-derived plan key, got %q", result.PlanKey)
	}
	if len(repo.receiptUpsertCalls) != 1 {
		t.Fatalf("expected one receipt scope upsert, got %d", len(repo.receiptUpsertCalls))
	}
	opened := repo.receiptUpsertCalls[0]
	if opened.TaskText != "auto open a receipt from work" || opened.Phase != "execute" {
		t.Fatalf("unexpected auto-opened receipt scope: %+v", opened)
	}
	if "plan:"+opened.ReceiptID != result.PlanKey {
		t.Fatalf("plan key %q does not match auto-opened receipt %q", result.PlanKey, opened.ReceiptID)
	}
	if len(repo.workUpsertCalls) != 1 || repo.workUpsertCalls[0].ReceiptID != opened.ReceiptID {
		t.Fatalf("expected task items attached to the auto-opened receipt, got %+v", repo.workUpsertCalls)
	}
}

func TestWork_ExplicitReceiptNeverAutoOpens(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{ProjectID: "project.alpha", ReceiptID: "receipt.abc123"}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Work(context.Background(), v1.WorkPayload{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		TaskText:  "task text must not override an explicit receipt",
		Tasks: []v1.WorkTaskPayload{
			{Key: "impl:explicit", Summary: "Explicit receipt", Status: v1.WorkItemStatusPending},
		},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(repo.receiptUpsertCalls) != 0 {
		t.Fatalf("expected no auto-open for an explicit receipt, got %d upserts", len(repo.receiptUpsertCalls))
	}
}

func TestWork_MissingPlanAndReceiptReturnsInvalidInput(t *testing.T) {
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Work(context.Background(), v1.WorkPayload{
		ProjectID: "project.alpha",
		Tasks: []v1.WorkTaskPayload{
			{Key: "src/a.go", Status: v1.WorkItemStatusComplete},
		},
	})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INVALID_INPUT" {
		t.Fatalf("unexpected API error code: %q", apiErr.Code)
	}
	if !strings.Contains(apiErr.Message, "plan_key, receipt_id, or task_text is required") {
		t.Fatalf("unexpected API error message: %q", apiErr.Message)
	}
	details, ok := apiErr.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected map details, got %T", apiErr.Details)
	}
	if details["plan_key"] != "" {
		t.Fatalf("unexpected plan_key detail: %#v", details)
	}
	if len(repo.fetchLookupCalls) != 0 {
		t.Fatalf("did not expect fetch lookup call, got %d", len(repo.fetchLookupCalls))
	}
}

func TestWork_InvalidPlanKeyFormatReturnsInvalidInput(t *testing.T) {
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Work(context.Background(), v1.WorkPayload{
		ProjectID: "project.alpha",
		PlanKey:   "plan.release.v1",
		Tasks: []v1.WorkTaskPayload{
			{Key: "src/a.go", Summary: "update", Status: v1.WorkItemStatusComplete},
		},
	})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INVALID_INPUT" {
		t.Fatalf("unexpected API error code: %q", apiErr.Code)
	}
	if !strings.Contains(apiErr.Message, "plan_key must use format plan:<receipt_id>") {
		t.Fatalf("unexpected API error message: %q", apiErr.Message)
	}
}

func TestWork_PlanKeyWhitespaceReturnsInvalidInput(t *testing.T) {
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Work(context.Background(), v1.WorkPayload{
		ProjectID: "project.alpha",
		PlanKey:   " plan:receipt.abc123 ",
		Tasks: []v1.WorkTaskPayload{
			{Key: "src/a.go", Summary: "update", Status: v1.WorkItemStatusComplete},
		},
	})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INVALID_INPUT" {
		t.Fatalf("unexpected API error code: %q", apiErr.Code)
	}
	if !strings.Contains(apiErr.Message, "plan_key must not include surrounding whitespace") {
		t.Fatalf("unexpected API error message: %q", apiErr.Message)
	}
}

func TestWork_PlanKeyReceiptMismatchReturnsInvalidInput(t *testing.T) {
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Work(context.Background(), v1.WorkPayload{
		ProjectID: "project.alpha",
		PlanKey:   "plan:receipt.abc123",
		ReceiptID: "receipt.other",
		Tasks: []v1.WorkTaskPayload{
			{Key: "src/a.go", Summary: "update", Status: v1.WorkItemStatusComplete},
		},
	})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INVALID_INPUT" {
		t.Fatalf("unexpected API error code: %q", apiErr.Code)
	}
	if !strings.Contains(apiErr.Message, "plan_key and receipt_id must reference the same receipt") {
		t.Fatalf("unexpected API error message: %q", apiErr.Message)
	}
}

func TestWork_UpsertPlanErrorMapsInternalError(t *testing.T) {
	repo := &fakeRepository{
		scopeResults:         []core.ReceiptScope{{ProjectID: "project.alpha", ReceiptID: "receipt.abc123"}},
		workPlanUpsertErrors: []error{errors.New("plan upsert failed")},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Work(context.Background(), v1.WorkPayload{
		ProjectID: "project.alpha",
		PlanKey:   "plan:receipt.abc123",
		ReceiptID: "receipt.abc123",
		Tasks: []v1.WorkTaskPayload{
			{Key: "src/a.go", Status: v1.WorkItemStatusComplete},
		},
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
	if details["operation"] != "upsert_work_plan" {
		t.Fatalf("unexpected operation detail: %#v", details)
	}
}

func TestWork_UpsertErrorMapsInternalError(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{ProjectID: "project.alpha", ReceiptID: "receipt.abc123"}},
		fetchLookupResults: []core.FetchLookup{{
			ProjectID: "project.alpha",
			ReceiptID: "receipt.abc123",
			RunID:     8,
		}},
		workUpsertErrors: []error{errors.New("upsert failed")},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Work(context.Background(), v1.WorkPayload{
		ProjectID: "project.alpha",
		PlanKey:   "plan:receipt.abc123",
		ReceiptID: "receipt.abc123",
		Tasks: []v1.WorkTaskPayload{
			{Key: "src/a.go", Status: v1.WorkItemStatusComplete},
		},
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
	if details["operation"] != "upsert_work_items" {
		t.Fatalf("unexpected operation detail: %#v", details)
	}
}

func TestWork_ListErrorsAreIgnoredWhenPlanRepositoryAvailable(t *testing.T) {
	repo := &fakeRepository{
		scopeResults:   []core.ReceiptScope{{ProjectID: "project.alpha", ReceiptID: "receipt.abc123"}},
		workListErrors: []error{errors.New("list failed")},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Work(context.Background(), v1.WorkPayload{
		ProjectID: "project.alpha",
		PlanKey:   "plan:receipt.abc123",
		ReceiptID: "receipt.abc123",
		Tasks: []v1.WorkTaskPayload{
			{Key: "src/a.go", Status: v1.WorkItemStatusComplete},
		},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.PlanStatus != core.PlanStatusComplete || result.Updated != 1 {
		t.Fatalf("unexpected work result: %+v", result)
	}
	if len(repo.workListCalls) != 0 {
		t.Fatalf("did not expect list_work_items calls, got %d", len(repo.workListCalls))
	}
}

func TestWorkPayloadTasks_UsesPayloadStatuses(t *testing.T) {
	payload := v1.WorkPayload{
		Tasks: []v1.WorkTaskPayload{
			{Key: " src/a.go ", Summary: "implement", Status: v1.WorkItemStatusInProgress, Outcome: "started"},
			{Key: "src/a.go", Summary: "blocked by dependency", Status: v1.WorkItemStatusBlocked, Outcome: "waiting"},
			{Key: "src/b.go", Summary: "verify", Status: v1.WorkItemStatusComplete, Outcome: "done"},
		},
	}

	items := workPayloadTasks(payload)
	want := []core.WorkItem{
		{ItemKey: "src/a.go", Summary: "blocked by dependency", Status: core.WorkItemStatusBlocked, Outcome: "waiting"},
		{ItemKey: "src/b.go", Summary: "verify", Status: core.WorkItemStatusComplete, Outcome: "done"},
	}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("unexpected normalized items: got %+v want %+v", items, want)
	}
}

func TestDerivePlanStatusFromWorkItems_Deterministic(t *testing.T) {
	tests := []struct {
		name  string
		items []core.WorkItem
		want  string
	}{
		{name: "empty defaults pending", items: nil, want: core.PlanStatusPending},
		{name: "all completed", items: []core.WorkItem{{Status: core.WorkItemStatusComplete}, {Status: core.WorkItemStatusComplete}}, want: core.PlanStatusComplete},
		{name: "all superseded", items: []core.WorkItem{{Status: core.WorkItemStatusSuperseded}, {Status: core.WorkItemStatusSuperseded}}, want: core.PlanStatusSuperseded},
		{name: "pending dominates completed", items: []core.WorkItem{{Status: core.WorkItemStatusComplete}, {Status: core.WorkItemStatusPending}}, want: core.PlanStatusPending},
		{name: "in progress dominates pending", items: []core.WorkItem{{Status: core.WorkItemStatusPending}, {Status: core.WorkItemStatusInProgress}}, want: core.PlanStatusInProgress},
		{name: "blocked dominates all", items: []core.WorkItem{{Status: core.WorkItemStatusComplete}, {Status: core.WorkItemStatusBlocked}, {Status: core.WorkItemStatusInProgress}}, want: core.PlanStatusBlocked},
		{name: "complete beats superseded when mixed", items: []core.WorkItem{{Status: core.WorkItemStatusSuperseded}, {Status: core.WorkItemStatusComplete}}, want: core.PlanStatusComplete},
		{name: "unknown treated as pending", items: []core.WorkItem{{Status: "unknown"}}, want: core.PlanStatusPending},
	}

	for _, tc := range tests {
		if got := derivePlanStatusFromWorkItems(tc.items); got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestWork_UpsertsPlanAndTasksWhenPlanRepositoryAvailable(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{ProjectID: "project.alpha", ReceiptID: "receipt.abc123"}},
		workPlanUpsertResult: []core.WorkPlanUpsertResult{{
			Plan: core.WorkPlan{
				ProjectID: "project.alpha",
				PlanKey:   "plan:receipt.abc123",
				Status:    core.PlanStatusInProgress,
				Tasks: []core.WorkItem{
					{ItemKey: "task.alpha", Status: core.WorkItemStatusInProgress},
				},
			},
			Updated: 1,
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Work(context.Background(), v1.WorkPayload{
		ProjectID: "project.alpha",
		PlanKey:   "plan:receipt.abc123",
		ReceiptID: "receipt.abc123",
		Mode:      v1.WorkPlanModeMerge,
		Plan: &v1.WorkPlanPayload{
			Title:     "Import Optimization",
			Objective: "Track execution centrally in awm",
		},
		Tasks: []v1.WorkTaskPayload{
			{
				Key:                "task.alpha",
				Summary:            "Audit pagination behavior",
				Status:             v1.WorkItemStatusInProgress,
				AcceptanceCriteria: []string{"No truncation above provider limits"},
			},
		},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	if result.PlanKey != "plan:receipt.abc123" || result.PlanStatus != core.PlanStatusInProgress || result.Updated != 1 || result.TaskCount != 1 {
		t.Fatalf("unexpected work result: %+v", result)
	}
	if len(repo.workPlanUpsertCalls) != 1 {
		t.Fatalf("expected one work plan upsert call, got %d", len(repo.workPlanUpsertCalls))
	}
	call := repo.workPlanUpsertCalls[0]
	if call.Mode != core.WorkPlanModeMerge {
		t.Fatalf("unexpected plan mode: %q", call.Mode)
	}
	if call.Title != "Import Optimization" {
		t.Fatalf("unexpected plan title: %q", call.Title)
	}
	if len(call.Tasks) != 1 {
		t.Fatalf("expected one task in plan upsert, got %d", len(call.Tasks))
	}
	if call.Tasks[0].ItemKey != "task.alpha" || call.Tasks[0].Summary != "Audit pagination behavior" {
		t.Fatalf("unexpected upserted task: %+v", call.Tasks[0])
	}
	if len(repo.workUpsertCalls) != 1 {
		t.Fatalf("expected mirrored work item upsert for receipt-scoped DoD checks")
	}
}

func TestFetchPlanItem_UsesStoredPlanRepositoryContent(t *testing.T) {
	repo := &fakeRepository{
		workPlanLookupResult: []core.WorkPlan{{
			ProjectID: "project.alpha",
			PlanKey:   "plan:receipt.abc123",
			ReceiptID: "receipt.abc123",
			Title:     "Import Optimization",
			Objective: "Consolidate planning state in awm",
			Status:    core.PlanStatusBlocked,
			Stages: core.WorkPlanStages{
				SpecOutline:        core.PlanStatusComplete,
				RefinedSpec:        core.PlanStatusInProgress,
				ImplementationPlan: core.PlanStatusPending,
			},
			Tasks: []core.WorkItem{
				{
					ItemKey:            "kh-22",
					Summary:            "Audit pagination",
					Status:             core.WorkItemStatusBlocked,
					BlockedReason:      "Awaiting upstream API quota",
					AcceptanceCriteria: []string{"Imports paginate beyond provider limits"},
				},
			},
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	item, found, err := svc.fetchPlanItem(context.Background(), "project.alpha", "plan:receipt.abc123", "receipt.abc123")
	if err != nil {
		t.Fatalf("fetchPlanItem returned error: %v", err)
	}
	if !found {
		t.Fatal("expected plan item to be found")
	}
	if item.Type != "plan" || item.Status != core.PlanStatusBlocked {
		t.Fatalf("unexpected fetch item metadata: %+v", item)
	}
	if !strings.Contains(item.Content, "\"objective\":\"Consolidate planning state in awm\"") {
		t.Fatalf("expected serialized plan content to include objective, got %s", item.Content)
	}
	if len(repo.workPlanLookupCalls) != 1 {
		t.Fatalf("expected one plan lookup call, got %d", len(repo.workPlanLookupCalls))
	}
	if repo.workPlanLookupCalls[0].PlanKey != "plan:receipt.abc123" {
		t.Fatalf("unexpected plan lookup key: %+v", repo.workPlanLookupCalls[0])
	}
}

func TestMakeContextPlans_UsesStoredPlanSummariesWhenAvailable(t *testing.T) {
	repo := &fakeRepository{
		workPlanListResults: [][]core.WorkPlanSummary{{
			{
				PlanKey:             "plan:receipt.abc123",
				Summary:             "Import Optimization (3 tasks)",
				Status:              core.PlanStatusInProgress,
				ActiveTaskKeys:      []string{"task.blocked", "task.active"},
				TaskCountTotal:      3,
				TaskCountPending:    1,
				TaskCountInProgress: 1,
				TaskCountBlocked:    1,
				TaskCountComplete:   0,
			},
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	plans := svc.makeContextPlans(context.Background(), "project.alpha", "receipt.abc123")
	if len(plans) != 1 {
		t.Fatalf("expected one persisted context plan, got %+v", plans)
	}
	if plans[0].Key != "plan:receipt.abc123" || plans[0].Status != v1.WorkItemStatusInProgress {
		t.Fatalf("unexpected context plan: %+v", plans[0])
	}
	if !reflect.DeepEqual(plans[0].FetchKeys, []string{"plan:receipt.abc123"}) {
		t.Fatalf("unexpected plan fetch keys: %+v", plans[0].FetchKeys)
	}
	if len(repo.workPlanLookupCalls) != 0 {
		t.Fatalf("did not expect per-plan lookups, got %d", len(repo.workPlanLookupCalls))
	}
}

func TestMakeContextPlans_UsesPlanOnlyFetchKeys(t *testing.T) {
	repo := &fakeRepository{
		workPlanListResults: [][]core.WorkPlanSummary{{
			{
				PlanKey:        "plan:receipt.abc123",
				Summary:        "Import Optimization",
				Status:         core.PlanStatusInProgress,
				ActiveTaskKeys: []string{strings.Repeat("t", 600), "task.short"},
			},
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	plans := svc.makeContextPlans(context.Background(), "project.alpha", "receipt.abc123")
	if len(plans) != 1 {
		t.Fatalf("expected one persisted context plan, got %+v", plans)
	}
	if !reflect.DeepEqual(plans[0].FetchKeys, []string{"plan:receipt.abc123"}) {
		t.Fatalf("unexpected filtered plan fetch keys: %+v", plans[0].FetchKeys)
	}
	if len(repo.workPlanListCalls) != 1 || repo.workPlanListCalls[0].Scope != string(v1.HistoryScopeCurrent) {
		t.Fatalf("expected current-scope work plan lookup, got %+v", repo.workPlanListCalls)
	}
}
