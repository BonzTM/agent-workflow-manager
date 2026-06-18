package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	v1 "github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"

	adapthttp "github.com/bonztm/agent-workflow-manager/internal/adapters/http"
)

// mockService implements core.Service. Only HistorySearch, Export, and Status
// have real behaviour; every other method panics if called.
type mockService struct {
	historySearchFn func(context.Context, v1.HistorySearchPayload) (v1.HistorySearchResult, *core.APIError)
	exportFn        func(context.Context, v1.ExportPayload) (v1.ExportResult, *core.APIError)
	statusFn        func(context.Context, v1.StatusPayload) (v1.StatusResult, *core.APIError)
}

func (m *mockService) HistorySearch(ctx context.Context, p v1.HistorySearchPayload) (v1.HistorySearchResult, *core.APIError) {
	return m.historySearchFn(ctx, p)
}

func (m *mockService) Export(ctx context.Context, p v1.ExportPayload) (v1.ExportResult, *core.APIError) {
	return m.exportFn(ctx, p)
}

func (m *mockService) Status(ctx context.Context, p v1.StatusPayload) (v1.StatusResult, *core.APIError) {
	return m.statusFn(ctx, p)
}

// Unused methods — panic to surface accidental calls.

func (m *mockService) Context(context.Context, v1.ContextPayload) (v1.ContextResult, *core.APIError) {
	panic("unexpected call to Context")
}
func (m *mockService) Fetch(context.Context, v1.FetchPayload) (v1.FetchResult, *core.APIError) {
	panic("unexpected call to Fetch")
}
func (m *mockService) Review(context.Context, v1.ReviewPayload) (v1.ReviewResult, *core.APIError) {
	panic("unexpected call to Review")
}
func (m *mockService) Work(context.Context, v1.WorkPayload) (v1.WorkResult, *core.APIError) {
	panic("unexpected call to Work")
}
func (m *mockService) Done(context.Context, v1.DonePayload) (v1.DoneResult, *core.APIError) {
	panic("unexpected call to Done")
}
func (m *mockService) Sync(context.Context, v1.SyncPayload) (v1.SyncResult, *core.APIError) {
	panic("unexpected call to Sync")
}
func (m *mockService) Health(context.Context, v1.HealthPayload) (v1.HealthResult, *core.APIError) {
	panic("unexpected call to Health")
}
func (m *mockService) Verify(context.Context, v1.VerifyPayload) (v1.VerifyResult, *core.APIError) {
	panic("unexpected call to Verify")
}
func (m *mockService) Init(context.Context, v1.InitPayload) (v1.InitResult, *core.APIError) {
	panic("unexpected call to Init")
}

const testProject = "test-project"

func newHandler(svc core.Service) http.Handler {
	return adapthttp.New(svc, testProject, nil)
}

func TestListPlans(t *testing.T) {
	want := v1.HistorySearchResult{
		Entity: v1.HistoryEntityWork,
		Scope:  v1.HistoryScopeCurrent,
	}

	var gotPayload v1.HistorySearchPayload
	svc := &mockService{
		historySearchFn: func(_ context.Context, p v1.HistorySearchPayload) (v1.HistorySearchResult, *core.APIError) {
			gotPayload = p
			return want, nil
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
	rec := httptest.NewRecorder()
	newHandler(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q; want application/json", ct)
	}

	// Verify the payload forwarded to the service.
	if gotPayload.ProjectID != testProject {
		t.Errorf("ProjectID = %q; want %q", gotPayload.ProjectID, testProject)
	}
	if gotPayload.Entity != v1.HistoryEntityWork {
		t.Errorf("Entity = %q; want %q", gotPayload.Entity, v1.HistoryEntityWork)
	}
	if gotPayload.Scope != v1.HistoryScopeCurrent {
		t.Errorf("Scope = %q; want %q", gotPayload.Scope, v1.HistoryScopeCurrent)
	}

	var got v1.HistorySearchResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Entity != want.Entity {
		t.Errorf("body Entity = %q; want %q", got.Entity, want.Entity)
	}
}

func TestGetPlan(t *testing.T) {
	want := v1.ExportResult{
		Format:  v1.ExportFormatJSON,
		Content: `{"hello":"world"}`,
	}

	var gotPayload v1.ExportPayload
	svc := &mockService{
		exportFn: func(_ context.Context, p v1.ExportPayload) (v1.ExportResult, *core.APIError) {
			gotPayload = p
			return want, nil
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/plans/receipt-abc", nil)
	rec := httptest.NewRecorder()
	newHandler(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d", rec.Code, http.StatusOK)
	}

	// The handler must prepend "plan:" to a bare key.
	if len(gotPayload.Fetch.Keys) != 1 || gotPayload.Fetch.Keys[0] != "plan:receipt-abc" {
		t.Fatalf("Fetch.Keys = %v; want [plan:receipt-abc]", gotPayload.Fetch.Keys)
	}
	if gotPayload.ProjectID != testProject {
		t.Errorf("ProjectID = %q; want %q", gotPayload.ProjectID, testProject)
	}
	if gotPayload.Format != v1.ExportFormatJSON {
		t.Errorf("Format = %q; want %q", gotPayload.Format, v1.ExportFormatJSON)
	}

	var got v1.ExportResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Content != want.Content {
		t.Errorf("body Content = %q; want %q", got.Content, want.Content)
	}
}

func TestGetPlanWithFullKey(t *testing.T) {
	var gotPayload v1.ExportPayload
	svc := &mockService{
		exportFn: func(_ context.Context, p v1.ExportPayload) (v1.ExportResult, *core.APIError) {
			gotPayload = p
			return v1.ExportResult{Format: v1.ExportFormatJSON}, nil
		},
	}

	// When the caller already includes the "plan:" prefix, the handler must
	// not double-prefix it.
	req := httptest.NewRequest(http.MethodGet, "/api/plans/plan:receipt-abc", nil)
	rec := httptest.NewRecorder()
	newHandler(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d", rec.Code, http.StatusOK)
	}
	if len(gotPayload.Fetch.Keys) != 1 || gotPayload.Fetch.Keys[0] != "plan:receipt-abc" {
		t.Fatalf("Fetch.Keys = %v; want [plan:receipt-abc]", gotPayload.Fetch.Keys)
	}
}

func TestGetStatus(t *testing.T) {
	want := v1.StatusResult{
		Summary: v1.StatusSummary{Ready: true},
	}

	var gotPayload v1.StatusPayload
	svc := &mockService{
		statusFn: func(_ context.Context, p v1.StatusPayload) (v1.StatusResult, *core.APIError) {
			gotPayload = p
			return want, nil
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	newHandler(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d", rec.Code, http.StatusOK)
	}
	if gotPayload.ProjectID != testProject {
		t.Errorf("ProjectID = %q; want %q", gotPayload.ProjectID, testProject)
	}

	var got v1.StatusResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Summary.Ready != want.Summary.Ready {
		t.Errorf("body Summary.Ready = %v; want %v", got.Summary.Ready, want.Summary.Ready)
	}
}

func TestAPIError(t *testing.T) {
	tests := []struct {
		name       string
		code       string
		message    string
		wantStatus int
	}{
		{
			name:       "not found",
			code:       "NOT_FOUND",
			message:    "plan not found",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "validation error",
			code:       "VALIDATION_ERROR",
			message:    "bad input",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid payload",
			code:       "INVALID_PAYLOAD",
			message:    "missing field",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown code maps to 500",
			code:       "INTERNAL",
			message:    "something broke",
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiErr := &core.APIError{Code: tt.code, Message: tt.message}
			svc := &mockService{
				historySearchFn: func(context.Context, v1.HistorySearchPayload) (v1.HistorySearchResult, *core.APIError) {
					return v1.HistorySearchResult{}, apiErr
				},
			}

			req := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
			rec := httptest.NewRecorder()
			newHandler(svc).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d; want %d", rec.Code, tt.wantStatus)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Fatalf("Content-Type = %q; want application/json", ct)
			}

			var body map[string]string
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] != tt.code {
				t.Errorf("body error = %q; want %q", body["error"], tt.code)
			}
			if body["message"] != tt.message {
				t.Errorf("body message = %q; want %q", body["message"], tt.message)
			}
		})
	}
}

func TestHealthz(t *testing.T) {
	// /healthz should work with any (even nil-ish) service since it
	// never touches the service layer.
	svc := &mockService{}
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	newHandler(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q; want application/json", ct)
	}

	var body map[string]bool
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !body["ok"] {
		t.Errorf("body ok = %v; want true", body["ok"])
	}
}
