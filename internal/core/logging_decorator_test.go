package core

import (
	"context"
	"testing"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/logging"
)

type decoratorFakeService struct {
	errorsByOperation map[string]*APIError
}

func (f decoratorFakeService) Context(_ context.Context, _ v1.ContextPayload) (v1.ContextResult, *APIError) {
	return v1.ContextResult{}, f.errorFor(logging.OperationContext)
}

func (f decoratorFakeService) Fetch(_ context.Context, _ v1.FetchPayload) (v1.FetchResult, *APIError) {
	return v1.FetchResult{}, f.errorFor(logging.OperationFetch)
}

func (f decoratorFakeService) Export(_ context.Context, _ v1.ExportPayload) (v1.ExportResult, *APIError) {
	return v1.ExportResult{}, f.errorFor(logging.OperationExport)
}

func (f decoratorFakeService) Review(_ context.Context, _ v1.ReviewPayload) (v1.ReviewResult, *APIError) {
	return v1.ReviewResult{}, f.errorFor(logging.OperationReview)
}

func (f decoratorFakeService) Work(_ context.Context, _ v1.WorkPayload) (v1.WorkResult, *APIError) {
	return v1.WorkResult{}, f.errorFor(logging.OperationWork)
}

func (f decoratorFakeService) HistorySearch(_ context.Context, _ v1.HistorySearchPayload) (v1.HistorySearchResult, *APIError) {
	return v1.HistorySearchResult{}, f.errorFor(logging.OperationHistorySearch)
}

func (f decoratorFakeService) Done(_ context.Context, _ v1.DonePayload) (v1.DoneResult, *APIError) {
	return v1.DoneResult{}, f.errorFor(logging.OperationDone)
}

func (f decoratorFakeService) Sync(_ context.Context, _ v1.SyncPayload) (v1.SyncResult, *APIError) {
	return v1.SyncResult{}, f.errorFor(logging.OperationSync)
}

func (f decoratorFakeService) Health(_ context.Context, payload v1.HealthPayload) (v1.HealthResult, *APIError) {
	if len(payload.Fixers) > 0 || payload.Apply != nil {
		return v1.HealthResult{}, f.errorFor(logging.OperationHealth)
	}
	return v1.HealthResult{}, f.errorFor(logging.OperationHealth)
}

func (f decoratorFakeService) Status(_ context.Context, _ v1.StatusPayload) (v1.StatusResult, *APIError) {
	return v1.StatusResult{}, f.errorFor(logging.OperationStatus)
}

func (f decoratorFakeService) Verify(_ context.Context, _ v1.VerifyPayload) (v1.VerifyResult, *APIError) {
	return v1.VerifyResult{}, f.errorFor(logging.OperationVerify)
}

func (f decoratorFakeService) Init(_ context.Context, _ v1.InitPayload) (v1.InitResult, *APIError) {
	return v1.InitResult{}, f.errorFor(logging.OperationInit)
}

func (f decoratorFakeService) errorFor(operation string) *APIError {
	if f.errorsByOperation == nil {
		return nil
	}
	return f.errorsByOperation[operation]
}

type fakeClock struct {
	values []time.Time
	index  int
}

func (c *fakeClock) Now() time.Time {
	if len(c.values) == 0 {
		return time.Time{}
	}
	if c.index >= len(c.values) {
		return c.values[len(c.values)-1]
	}
	value := c.values[c.index]
	c.index++
	return value
}

func TestWithLogging_LogsStartAndFinishForAllOperations(t *testing.T) {
	for _, tc := range decoratorOperationCases() {
		t.Run(tc.operation, func(t *testing.T) {
			recorder := logging.NewRecorder()
			clock := &fakeClock{
				values: []time.Time{
					time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC),
					time.Date(2026, 3, 4, 12, 0, 0, 5_000_000, time.UTC),
				},
			}
			svc := WithLoggingClock(decoratorFakeService{}, recorder, clock.Now)

			apiErr := tc.call(context.Background(), svc)
			if apiErr != nil {
				t.Fatalf("expected success, got API error: %+v", apiErr)
			}

			entries := recorder.Entries()
			if len(entries) != 2 {
				t.Fatalf("expected 2 log entries, got %d", len(entries))
			}
			assertOperationStartEntry(t, entries[0], tc.operation, "project.alpha")
			assertOperationFinishEntry(t, entries[1], tc.operation, "project.alpha", true, "", int64(5))
		})
	}
}

func TestWithLogging_LogsFailureCodeForAllOperations(t *testing.T) {
	for _, tc := range decoratorOperationCases() {
		t.Run(tc.operation, func(t *testing.T) {
			recorder := logging.NewRecorder()
			clock := &fakeClock{
				values: []time.Time{
					time.Date(2026, 3, 4, 12, 30, 0, 0, time.UTC),
					time.Date(2026, 3, 4, 12, 30, 0, 9_000_000, time.UTC),
				},
			}
			svc := WithLoggingClock(decoratorFakeService{
				errorsByOperation: map[string]*APIError{
					tc.operation: NewError("TEST_FAILURE", "forced failure", nil),
				},
			}, recorder, clock.Now)

			apiErr := tc.call(context.Background(), svc)
			if apiErr == nil {
				t.Fatal("expected API error")
			}
			if apiErr.Code != "TEST_FAILURE" {
				t.Fatalf("unexpected API error code: %s", apiErr.Code)
			}

			entries := recorder.Entries()
			if len(entries) != 2 {
				t.Fatalf("expected 2 log entries, got %d", len(entries))
			}
			assertOperationStartEntry(t, entries[0], tc.operation, "project.alpha")
			assertOperationFinishEntry(t, entries[1], tc.operation, "project.alpha", false, "TEST_FAILURE", int64(9))
		})
	}
}

type decoratorOperationCase struct {
	operation string
	call      func(context.Context, Service) *APIError
}

func decoratorOperationCases() []decoratorOperationCase {
	return []decoratorOperationCase{
		{
			operation: logging.OperationContext,
			call: func(ctx context.Context, svc Service) *APIError {
				_, apiErr := svc.Context(ctx, v1.ContextPayload{
					ProjectID: "project.alpha",
					TaskText:  "x",
					Phase:     v1.PhaseExecute,
				})
				return apiErr
			},
		},
		{
			operation: logging.OperationFetch,
			call: func(ctx context.Context, svc Service) *APIError {
				_, apiErr := svc.Fetch(ctx, v1.FetchPayload{
					ProjectID: "project.alpha",
					Keys:      []string{"docs/runtime.md"},
				})
				return apiErr
			},
		},
		{
			operation: logging.OperationExport,
			call: func(ctx context.Context, svc Service) *APIError {
				_, apiErr := svc.Export(ctx, v1.ExportPayload{
					ProjectID: "project.alpha",
					Format:    v1.ExportFormatMarkdown,
					Fetch: &v1.ExportFetchSelector{
						ReceiptID: "receipt-12345",
					},
				})
				return apiErr
			},
		},
		{
			operation: logging.OperationReview,
			call: func(ctx context.Context, svc Service) *APIError {
				_, apiErr := svc.Review(ctx, v1.ReviewPayload{
					ProjectID: "project.alpha",
					ReceiptID: "receipt-12345",
				})
				return apiErr
			},
		},
		{
			operation: logging.OperationWork,
			call: func(ctx context.Context, svc Service) *APIError {
				_, apiErr := svc.Work(ctx, v1.WorkPayload{
					ProjectID: "project.alpha",
					PlanKey:   "plan.alpha",
					Tasks: []v1.WorkTaskPayload{
						{Key: "docs/runtime.md", Summary: "x", Status: v1.WorkItemStatusPending},
					},
				})
				return apiErr
			},
		},
		{
			operation: logging.OperationHistorySearch,
			call: func(ctx context.Context, svc Service) *APIError {
				_, apiErr := svc.HistorySearch(ctx, v1.HistorySearchPayload{
					ProjectID: "project.alpha",
					Query:     "bootstrap",
					Scope:     v1.HistoryScopeAll,
				})
				return apiErr
			},
		},
		{
			operation: logging.OperationDone,
			call: func(ctx context.Context, svc Service) *APIError {
				_, apiErr := svc.Done(ctx, v1.DonePayload{
					ProjectID: "project.alpha",
					ReceiptID: "receipt-12345",
				})
				return apiErr
			},
		},
		{
			operation: logging.OperationSync,
			call: func(ctx context.Context, svc Service) *APIError {
				_, apiErr := svc.Sync(ctx, v1.SyncPayload{
					ProjectID: "project.alpha",
				})
				return apiErr
			},
		},
		{
			operation: logging.OperationHealth,
			call: func(ctx context.Context, svc Service) *APIError {
				_, apiErr := svc.Health(ctx, v1.HealthPayload{
					ProjectID: "project.alpha",
				})
				return apiErr
			},
		},
		{
			operation: logging.OperationStatus,
			call: func(ctx context.Context, svc Service) *APIError {
				_, apiErr := svc.Status(ctx, v1.StatusPayload{
					ProjectID: "project.alpha",
				})
				return apiErr
			},
		},
		{
			operation: logging.OperationInit,
			call: func(ctx context.Context, svc Service) *APIError {
				_, apiErr := svc.Init(ctx, v1.InitPayload{
					ProjectID: "project.alpha",
				})
				return apiErr
			},
		},
	}
}

func assertOperationStartEntry(t *testing.T, entry logging.Entry, operation, projectID string) {
	t.Helper()

	if entry.Level != "info" {
		t.Fatalf("unexpected start level: %s", entry.Level)
	}
	if entry.Event != logging.EventServiceOperationStart {
		t.Fatalf("unexpected start event: %s", entry.Event)
	}
	if got := entry.Fields["operation"]; got != operation {
		t.Fatalf("unexpected start operation: got %v want %s", got, operation)
	}
	if got := entry.Fields["project_id"]; got != projectID {
		t.Fatalf("unexpected start project_id: got %v want %s", got, projectID)
	}
}

func assertOperationFinishEntry(t *testing.T, entry logging.Entry, operation, projectID string, ok bool, errorCode string, durationMS int64) {
	t.Helper()

	if ok {
		if entry.Level != "info" {
			t.Fatalf("unexpected finish level: %s", entry.Level)
		}
	} else {
		if entry.Level != "error" {
			t.Fatalf("unexpected finish level: %s", entry.Level)
		}
	}
	if entry.Event != logging.EventServiceOperationFinish {
		t.Fatalf("unexpected finish event: %s", entry.Event)
	}
	if got := entry.Fields["operation"]; got != operation {
		t.Fatalf("unexpected finish operation: got %v want %s", got, operation)
	}
	if got := entry.Fields["project_id"]; got != projectID {
		t.Fatalf("unexpected finish project_id: got %v want %s", got, projectID)
	}
	if got := entry.Fields["ok"]; got != ok {
		t.Fatalf("unexpected finish ok: got %v want %v", got, ok)
	}
	if got := entry.Fields["duration_ms"]; got != durationMS {
		t.Fatalf("unexpected finish duration_ms: got %v want %d", got, durationMS)
	}
	if ok {
		if _, exists := entry.Fields["error_code"]; exists {
			t.Fatalf("did not expect error_code on success: %+v", entry.Fields)
		}
		return
	}
	if got := entry.Fields["error_code"]; got != errorCode {
		t.Fatalf("unexpected finish error_code: got %v want %s", got, errorCode)
	}
}
