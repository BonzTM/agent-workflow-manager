package backend

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func TestHistorySearch_MapsPlanSummariesAndDefaults(t *testing.T) {
	repo := &fakeRepository{
		workPlanListResults: [][]core.WorkPlanSummary{{
			{
				PlanKey:             "plan:receipt.active123",
				ReceiptID:           "receipt.active123",
				Title:               "Init follow-up",
				Objective:           "Verify first-run onboarding and history search",
				Summary:             "Init follow-up (2 tasks)",
				Status:              core.PlanStatusInProgress,
				Kind:                "story",
				ParentPlanKey:       "plan:receipt.parent123",
				ActiveTaskKeys:      []string{"task.verify", "task.docs"},
				TaskCountTotal:      2,
				TaskCountPending:    0,
				TaskCountInProgress: 1,
				TaskCountBlocked:    0,
				TaskCountComplete:   1,
				UpdatedAt:           time.Date(2026, 3, 6, 15, 4, 5, 0, time.UTC),
			},
			{
				PlanKey:             "plan:receipt.done123",
				ReceiptID:           "receipt.done123",
				Title:               "Released v1",
				Objective:           "Ship the initial history surface",
				Summary:             "Released v1 (3 tasks)",
				Status:              core.PlanStatusComplete,
				Kind:                "feature",
				TaskCountTotal:      3,
				TaskCountPending:    0,
				TaskCountInProgress: 0,
				TaskCountBlocked:    0,
				TaskCountComplete:   3,
				UpdatedAt:           time.Date(2026, 3, 6, 16, 5, 6, 0, time.UTC),
			},
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.HistorySearch(context.Background(), v1.HistorySearchPayload{
		ProjectID: "project.alpha",
		Query:     " bootstrap ",
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(repo.workPlanListCalls) != 1 {
		t.Fatalf("expected one work plan list call, got %d", len(repo.workPlanListCalls))
	}
	if got := repo.workPlanListCalls[0]; got.ProjectID != "project.alpha" || got.Query != "bootstrap" || got.Scope != string(v1.HistoryScopeAll) || got.Limit != 20 || got.Unbounded {
		t.Fatalf("unexpected work plan list query: %+v", got)
	}
	if result.Entity != v1.HistoryEntityAll || result.Scope != "" || result.Query != "bootstrap" || result.Limit != 20 || result.Count != 2 {
		t.Fatalf("unexpected history result metadata: %+v", result)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected two items, got %+v", result.Items)
	}
	itemsByKey := map[string]v1.HistoryItem{}
	for _, item := range result.Items {
		itemsByKey[item.Key] = item
	}
	if item := itemsByKey["plan:receipt.active123"]; item.Entity != v1.HistoryEntityWork || item.Scope != v1.HistoryScopeCurrent || !reflect.DeepEqual(item.FetchKeys, []string{"plan:receipt.active123"}) {
		t.Fatalf("unexpected active plan summary: %+v", item)
	}
	if item := itemsByKey["plan:receipt.done123"]; item.Entity != v1.HistoryEntityWork || item.Scope != v1.HistoryScopeCompleted || !reflect.DeepEqual(item.FetchKeys, []string{"plan:receipt.done123"}) {
		t.Fatalf("unexpected completed plan summary: %+v", item)
	}
}

func TestHistorySearch_UsesExplicitScopeKindAndLimit(t *testing.T) {
	repo := &fakeRepository{
		workPlanListResults: [][]core.WorkPlanSummary{{}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.HistorySearch(context.Background(), v1.HistorySearchPayload{
		ProjectID: "project.alpha",
		Entity:    v1.HistoryEntityWork,
		Scope:     v1.HistoryScopeDeferred,
		Kind:      "story",
		Limit:     7,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if got := repo.workPlanListCalls[0]; got.Scope != string(v1.HistoryScopeDeferred) || got.Kind != "story" || got.Limit != 7 || got.Unbounded {
		t.Fatalf("unexpected explicit history query: %+v", got)
	}
	if result.Entity != v1.HistoryEntityWork || result.Scope != v1.HistoryScopeDeferred || result.Limit != 7 || result.Count != 0 || len(result.Items) != 0 {
		t.Fatalf("unexpected history result: %+v", result)
	}
}

func TestHistorySearch_UnboundedUsesAllScopeWithoutLimitCap(t *testing.T) {
	repo := &fakeRepository{
		workPlanListResults: [][]core.WorkPlanSummary{{}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	value := true
	result, apiErr := svc.HistorySearch(context.Background(), v1.HistorySearchPayload{
		ProjectID: "project.alpha",
		Query:     "bootstrap",
		Unbounded: &value,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if got := repo.workPlanListCalls[0]; !got.Unbounded || got.Limit != 0 || got.Scope != string(v1.HistoryScopeAll) {
		t.Fatalf("unexpected unbounded history query: %+v", got)
	}
	if result.Limit != 0 {
		t.Fatalf("expected unbounded history result limit 0, got %+v", result)
	}
}

func TestHistorySearch_AllEntitiesReturnsReceiptsRunsAndWork(t *testing.T) {
	repo := &fakeRepository{
		workPlanListResults: [][]core.WorkPlanSummary{{
			{
				PlanKey:   "plan:receipt.work123",
				ReceiptID: "receipt.work123",
				Summary:   "Current plan",
				Status:    core.PlanStatusInProgress,
				UpdatedAt: time.Date(2026, 3, 6, 10, 0, 0, 0, time.UTC),
			},
		}},
		receiptHistoryResults: [][]core.ReceiptHistorySummary{{
			{
				ReceiptID:       "receipt.receipt123",
				TaskText:        "Receipt history entry",
				Phase:           string(v1.PhaseReview),
				LatestRequestID: "req-12345678",
				LatestStatus:    "accepted",
				UpdatedAt:       time.Date(2026, 3, 6, 11, 0, 0, 0, time.UTC),
			},
		}},
		runHistoryResults: [][]core.RunHistorySummary{{
			{
				RunID:     33,
				ReceiptID: "receipt.run123",
				RequestID: "req-87654321",
				TaskText:  "Run history entry",
				Phase:     string(v1.PhaseExecute),
				Status:    "accepted",
				Outcome:   "Run completed",
				UpdatedAt: time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC),
			},
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.HistorySearch(context.Background(), v1.HistorySearchPayload{
		ProjectID: "project.alpha",
		Entity:    v1.HistoryEntityAll,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Entity != v1.HistoryEntityAll || result.Count != 3 || len(result.Items) != 3 {
		t.Fatalf("unexpected history result: %+v", result)
	}
	if result.Items[0].Entity != v1.HistoryEntityRun || result.Items[0].Key != "run:33" {
		t.Fatalf("expected run item first, got %+v", result.Items)
	}
	if result.Items[1].Entity != v1.HistoryEntityReceipt || result.Items[1].Key != "receipt:receipt.receipt123" {
		t.Fatalf("expected receipt item second, got %+v", result.Items)
	}
	if result.Items[2].Entity != v1.HistoryEntityWork || result.Items[2].Key != "plan:receipt.work123" {
		t.Fatalf("expected work item third, got %+v", result.Items)
	}
	if len(repo.workPlanListCalls) != 1 || repo.workPlanListCalls[0].Scope != string(v1.HistoryScopeAll) {
		t.Fatalf("unexpected work history query: %+v", repo.workPlanListCalls)
	}
	if len(repo.receiptHistoryCalls) != 1 || len(repo.runHistoryCalls) != 1 {
		t.Fatalf("expected one receipt/run history query, got receipts=%d runs=%d", len(repo.receiptHistoryCalls), len(repo.runHistoryCalls))
	}
}
