package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	"github.com/bonztm/agent-workflow-manager/internal/logging"
	"github.com/bonztm/agent-workflow-manager/internal/runtime"
	"github.com/bonztm/agent-workflow-manager/internal/service/unconfigured"
)

type fakeService struct{}

type capturingService struct {
	fakeService
	statusPayload v1.StatusPayload
}

func (f fakeService) Context(_ context.Context, _ v1.ContextPayload) (v1.ContextResult, *core.APIError) {
	return v1.ContextResult{Status: "ok"}, nil
}

func (f fakeService) Fetch(_ context.Context, _ v1.FetchPayload) (v1.FetchResult, *core.APIError) {
	return v1.FetchResult{Items: []v1.FetchItem{}}, nil
}

func (f fakeService) Export(_ context.Context, payload v1.ExportPayload) (v1.ExportResult, *core.APIError) {
	return v1.ExportResult{
		Format:  payload.Format,
		Content: "# export",
		Document: &v1.ExportDocument{
			Kind: v1.ExportDocumentKindFetchBundle,
		},
	}, nil
}

func (f fakeService) Review(_ context.Context, _ v1.ReviewPayload) (v1.ReviewResult, *core.APIError) {
	return v1.ReviewResult{
		PlanKey:      "plan:receipt-1234",
		PlanStatus:   "pending",
		Updated:      1,
		ReviewKey:    v1.DefaultReviewTaskKey,
		ReviewStatus: v1.WorkItemStatusComplete,
	}, nil
}

func (f fakeService) Work(_ context.Context, _ v1.WorkPayload) (v1.WorkResult, *core.APIError) {
	return v1.WorkResult{PlanKey: "plan:receipt-1234", PlanStatus: "pending", Updated: 1}, nil
}

func (f fakeService) HistorySearch(_ context.Context, _ v1.HistorySearchPayload) (v1.HistorySearchResult, *core.APIError) {
	return v1.HistorySearchResult{}, nil
}

func (f fakeService) Done(_ context.Context, _ v1.DonePayload) (v1.DoneResult, *core.APIError) {
	return v1.DoneResult{Accepted: true}, nil
}

func (f fakeService) Sync(_ context.Context, _ v1.SyncPayload) (v1.SyncResult, *core.APIError) {
	return v1.SyncResult{}, nil
}

func (f fakeService) Health(_ context.Context, payload v1.HealthPayload) (v1.HealthResult, *core.APIError) {
	if len(payload.Fixers) > 0 || payload.Apply != nil {
		return v1.HealthResult{Mode: "fix", Fix: &v1.HealthFixResult{}}, nil
	}
	return v1.HealthResult{Mode: "check", Check: &v1.HealthCheckResult{}}, nil
}

func (f fakeService) Status(_ context.Context, _ v1.StatusPayload) (v1.StatusResult, *core.APIError) {
	return v1.StatusResult{}, nil
}

func (f fakeService) Verify(_ context.Context, _ v1.VerifyPayload) (v1.VerifyResult, *core.APIError) {
	return v1.VerifyResult{}, nil
}

func (f fakeService) Init(_ context.Context, _ v1.InitPayload) (v1.InitResult, *core.APIError) {
	return v1.InitResult{}, nil
}

func (c *capturingService) Status(_ context.Context, payload v1.StatusPayload) (v1.StatusResult, *core.APIError) {
	c.statusPayload = payload
	return v1.StatusResult{}, nil
}

func TestInvoke_UnknownTool(t *testing.T) {
	_, err := Invoke(context.Background(), fakeService{}, "unknown", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Code != "UNKNOWN_TOOL" {
		t.Fatalf("unexpected code: %s", err.Code)
	}
}

func TestToolDefinitions_MatchPublicCommandCatalog(t *testing.T) {
	defs := ToolDefinitions()
	got := make([]string, 0, len(defs))
	for _, def := range defs {
		got = append(got, def.Name)
	}
	sort.Strings(got)

	want := v1.CommandNames()
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected tool definitions: got %v want %v", got, want)
	}
}

func TestInvoke_RemovedLegacyToolRejected(t *testing.T) {
	for _, tool := range []string{"get_context", "report_completion", "bootstrap"} {
		t.Run(tool, func(t *testing.T) {
			_, err := Invoke(context.Background(), fakeService{}, tool, []byte(`{}`))
			if err == nil {
				t.Fatal("expected error")
			}
			if err.Code != "UNKNOWN_TOOL" {
				t.Fatalf("unexpected code: %s", err.Code)
			}
		})
	}
}

func TestInvoke_Context(t *testing.T) {
	payload := []byte(`{"project_id":"my-cool-app","task_text":"x","phase":"execute"}`)
	result, err := Invoke(context.Background(), fakeService{}, "context", payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.(v1.ContextResult); !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
}

func TestInvoke_DefaultsProjectIDFromEnv(t *testing.T) {
	t.Setenv(runtime.ProjectIDEnvVar, "env-project")

	result, err := Invoke(context.Background(), fakeService{}, "context", []byte(`{"task_text":"x","phase":"execute"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.(v1.ContextResult); !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
}

func TestInvoke_ProjectRootInferenceOverridesEnvProjectID(t *testing.T) {
	t.Setenv(runtime.ProjectIDEnvVar, "env-project")
	projectRoot := filepath.Join(t.TempDir(), "Target Repo")
	svc := &capturingService{}

	result, err := Invoke(context.Background(), svc, "status", []byte(`{"project_root":"`+projectRoot+`"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.(v1.StatusResult); !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	if got, want := svc.statusPayload.ProjectID, "Target-Repo"; got != want {
		t.Fatalf("unexpected inferred project_id: got %q want %q", got, want)
	}
}

func TestInvoke_RejectsUnknownInputFields(t *testing.T) {
	_, err := Invoke(context.Background(), fakeService{}, "context", []byte(`{"project_id":"my-cool-app","task_text":"x","phase":"execute","extra":true}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Code != "INVALID_PAYLOAD" {
		t.Fatalf("unexpected code: %s", err.Code)
	}
}

func TestInvoke_RejectsInvalidJSON(t *testing.T) {
	_, err := Invoke(context.Background(), fakeService{}, "context", []byte(`{`))
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Code != "INVALID_JSON" {
		t.Fatalf("unexpected code: %s", err.Code)
	}
}

func TestInvoke_FetchAndWork(t *testing.T) {
	fetchResult, fetchErr := Invoke(context.Background(), fakeService{}, string(v1.CommandFetch), []byte(`{"project_id":"my-cool-app","keys":["docs/runtime.md"]}`))
	if fetchErr != nil {
		t.Fatalf("unexpected fetch error: %v", fetchErr)
	}
	if _, ok := fetchResult.(v1.FetchResult); !ok {
		t.Fatalf("unexpected fetch result type: %T", fetchResult)
	}

	workPayload := []byte(`{"project_id":"my-cool-app","plan_key":"plan:receipt-1234","tasks":[{"key":"x.go","summary":"x","status":"pending"}]}`)
	workResult, workErr := Invoke(context.Background(), fakeService{}, string(v1.CommandWork), workPayload)
	if workErr != nil {
		t.Fatalf("unexpected work error: %v", workErr)
	}
	if _, ok := workResult.(v1.WorkResult); !ok {
		t.Fatalf("unexpected work result type: %T", workResult)
	}

	reviewPayload := []byte(`{"project_id":"my-cool-app","receipt_id":"receipt-1234","outcome":"No blocking review findings."}`)
	reviewResult, reviewErr := Invoke(context.Background(), fakeService{}, string(v1.CommandReview), reviewPayload)
	if reviewErr != nil {
		t.Fatalf("unexpected review error: %v", reviewErr)
	}
	if _, ok := reviewResult.(v1.ReviewResult); !ok {
		t.Fatalf("unexpected review result type: %T", reviewResult)
	}

	exportPayload := []byte(`{"project_id":"my-cool-app","format":"markdown","fetch":{"receipt_id":"receipt-1234"}}`)
	exportResult, exportErr := Invoke(context.Background(), fakeService{}, string(v1.CommandExport), exportPayload)
	if exportErr != nil {
		t.Fatalf("unexpected export error: %v", exportErr)
	}
	if _, ok := exportResult.(v1.ExportResult); !ok {
		t.Fatalf("unexpected export result type: %T", exportResult)
	}
}

func TestInvoke_RemainingTools(t *testing.T) {
	tests := []struct {
		name    string
		tool    string
		payload []byte
		assert  func(t *testing.T, result any)
	}{
		{
			name:    "history",
			tool:    string(v1.CommandHistorySearch),
			payload: []byte(`{"project_id":"my-cool-app","entity":"work","query":"bootstrap"}`),
			assert: func(t *testing.T, result any) {
				t.Helper()
				if _, ok := result.(v1.HistorySearchResult); !ok {
					t.Fatalf("unexpected history result type: %T", result)
				}
			},
		},
		{
			name:    "sync",
			tool:    string(v1.CommandSync),
			payload: []byte(`{"project_id":"my-cool-app"}`),
			assert: func(t *testing.T, result any) {
				t.Helper()
				if _, ok := result.(v1.SyncResult); !ok {
					t.Fatalf("unexpected sync result type: %T", result)
				}
			},
		},
		{
			name:    "health",
			tool:    string(v1.CommandHealth),
			payload: []byte(`{"project_id":"my-cool-app"}`),
			assert: func(t *testing.T, result any) {
				t.Helper()
				health, ok := result.(v1.HealthResult)
				if !ok {
					t.Fatalf("unexpected health result type: %T", result)
				}
				if health.Mode != "check" || health.Check == nil {
					t.Fatalf("unexpected health check result: %+v", health)
				}
			},
		},
		{
			name:    "status",
			tool:    "status",
			payload: []byte(`{"project_id":"my-cool-app","project_root":"."}`),
			assert: func(t *testing.T, result any) {
				t.Helper()
				if _, ok := result.(v1.StatusResult); !ok {
					t.Fatalf("unexpected status result type: %T", result)
				}
			},
		},
		{
			name:    "verify",
			tool:    string(v1.CommandVerify),
			payload: []byte(`{"project_id":"my-cool-app","phase":"execute","files_changed":["go.mod"]}`),
			assert: func(t *testing.T, result any) {
				t.Helper()
				if _, ok := result.(v1.VerifyResult); !ok {
					t.Fatalf("unexpected verify result type: %T", result)
				}
			},
		},
		{
			name:    "init",
			tool:    string(v1.CommandInit),
			payload: []byte(`{"project_id":"my-cool-app","project_root":"."}`),
			assert: func(t *testing.T, result any) {
				t.Helper()
				if _, ok := result.(v1.InitResult); !ok {
					t.Fatalf("unexpected init result type: %T", result)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Invoke(context.Background(), fakeService{}, tc.tool, tc.payload)
			if err != nil {
				t.Fatalf("unexpected %s error: %v", tc.tool, err)
			}
			tc.assert(t, result)
		})
	}
}

func TestToolDefinitions_IncludeSchemaMetadata(t *testing.T) {
	defs := ToolDefinitions()
	if len(defs) != 12 {
		t.Fatalf("unexpected tool count: got %d want 12", len(defs))
	}

	expectedInputRefs := map[string]string{
		"context": "https://agent-workflow-manager.dev/spec/v1/cli.command.schema.json#/$defs/contextPayload",
		"fetch":   "https://agent-workflow-manager.dev/spec/v1/cli.command.schema.json#/$defs/fetchPayload",
		"export":  "https://agent-workflow-manager.dev/spec/v1/cli.command.schema.json#/$defs/exportPayload",
		"done":    "https://agent-workflow-manager.dev/spec/v1/cli.command.schema.json#/$defs/donePayload",
		"review":  "https://agent-workflow-manager.dev/spec/v1/cli.command.schema.json#/$defs/reviewPayload",
		"work":    "https://agent-workflow-manager.dev/spec/v1/cli.command.schema.json#/$defs/workPayload",
		"history": "https://agent-workflow-manager.dev/spec/v1/cli.command.schema.json#/$defs/historySearchPayload",
		"sync":    "https://agent-workflow-manager.dev/spec/v1/cli.command.schema.json#/$defs/syncPayload",
		"health":  "https://agent-workflow-manager.dev/spec/v1/cli.command.schema.json#/$defs/healthPayload",
		"status":  "https://agent-workflow-manager.dev/spec/v1/cli.command.schema.json#/$defs/statusPayload",
		"verify":  "https://agent-workflow-manager.dev/spec/v1/cli.command.schema.json#/$defs/verifyPayload",
		"init":    "https://agent-workflow-manager.dev/spec/v1/cli.command.schema.json#/$defs/initPayload",
	}
	for _, def := range defs {
		if got := def.InputSchema["$schema"]; got != schemaDraft202012 {
			t.Fatalf("tool %q missing input schema draft metadata: %v", def.Name, got)
		}
		if got := def.InputSchema["$ref"]; got != expectedInputRefs[def.Name] {
			t.Fatalf("tool %q unexpected input schema ref: %v", def.Name, got)
		}
	}
}

func TestToolDefinitions_MatchSpecFile(t *testing.T) {
	specPath := filepath.Join("..", "..", "..", "spec", "v1", "mcp.tools.v1.json")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec file: %v", err)
	}

	var spec struct {
		Version string    `json:"version"`
		Tools   []ToolDef `json:"tools"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("unmarshal spec file: %v", err)
	}
	if spec.Version != v1.Version {
		t.Fatalf("unexpected spec version: got %q want %q", spec.Version, v1.Version)
	}

	if got, want := ToolDefinitions(), spec.Tools; !reflect.DeepEqual(got, want) {
		t.Fatalf("tool definition drift detected\nruntime: %+v\nspec: %+v", got, want)
	}
}

func TestInvoke_UnconfiguredServiceReturnsNotImplemented(t *testing.T) {
	payload := []byte(`{"project_id":"my-cool-app","task_text":"x","phase":"execute"}`)
	_, err := Invoke(context.Background(), unconfigured.New(), "context", payload)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Code != "NOT_IMPLEMENTED" {
		t.Fatalf("unexpected code: %s", err.Code)
	}
}

func TestInvokeWithLogger_LogsIngressDispatchAndResultOnSuccess(t *testing.T) {
	recorder := logging.NewRecorder()
	payload := []byte(`{"project_id":"my-cool-app","task_text":"x","phase":"execute"}`)

	result, err := InvokeWithLogger(context.Background(), fakeService{}, "context", payload, recorder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.(v1.ContextResult); !ok {
		t.Fatalf("unexpected result type: %T", result)
	}

	entries := recorder.Entries()
	if len(entries) != 5 {
		t.Fatalf("expected 5 log entries, got %d", len(entries))
	}
	if entries[0].Event != logging.EventMCPIngressRead || entries[0].Fields["ok"] != true {
		t.Fatalf("unexpected ingress read entry: %+v", entries[0])
	}
	if entries[1].Event != logging.EventMCPIngressValidate || entries[1].Fields["ok"] != true {
		t.Fatalf("unexpected validate entry: %+v", entries[1])
	}
	if entries[2].Event != logging.EventMCPDispatch || entries[2].Fields["phase"] != "start" {
		t.Fatalf("unexpected dispatch start entry: %+v", entries[2])
	}
	if entries[3].Event != logging.EventMCPDispatch || entries[3].Fields["phase"] != "finish" || entries[3].Fields["ok"] != true {
		t.Fatalf("unexpected dispatch finish entry: %+v", entries[3])
	}
	if entries[4].Event != logging.EventMCPResult || entries[4].Fields["ok"] != true {
		t.Fatalf("unexpected result entry: %+v", entries[4])
	}
}

func TestInvokeWithLogger_LogsValidationFailure(t *testing.T) {
	recorder := logging.NewRecorder()
	_, err := InvokeWithLogger(context.Background(), fakeService{}, "context", []byte(`{`), recorder)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Code != "INVALID_JSON" {
		t.Fatalf("unexpected error code: %s", err.Code)
	}

	entries := recorder.Entries()
	if len(entries) != 4 {
		t.Fatalf("expected 4 log entries, got %d", len(entries))
	}
	if entries[1].Event != logging.EventMCPIngressValidate || entries[1].Fields["ok"] != false || entries[1].Fields["error_code"] != "INVALID_JSON" {
		t.Fatalf("unexpected validate failure entry: %+v", entries[1])
	}
	if entries[2].Event != logging.EventMCPFailure || entries[2].Fields["stage"] != "validate" {
		t.Fatalf("unexpected failure entry: %+v", entries[2])
	}
	if entries[3].Event != logging.EventMCPResult || entries[3].Fields["ok"] != false || entries[3].Fields["error_code"] != "INVALID_JSON" {
		t.Fatalf("unexpected result entry: %+v", entries[3])
	}
}

func TestInvokeWithLogger_LogsUnknownToolFailure(t *testing.T) {
	recorder := logging.NewRecorder()
	_, err := InvokeWithLogger(context.Background(), fakeService{}, "unknown", []byte(`{}`), recorder)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Code != "UNKNOWN_TOOL" {
		t.Fatalf("unexpected error code: %s", err.Code)
	}

	entries := recorder.Entries()
	if len(entries) != 4 {
		t.Fatalf("expected 4 log entries, got %d", len(entries))
	}
	if entries[1].Event != logging.EventMCPIngressValidate || entries[1].Fields["ok"] != false {
		t.Fatalf("unexpected validate entry: %+v", entries[1])
	}
	if entries[2].Event != logging.EventMCPFailure || entries[2].Fields["stage"] != "validate" {
		t.Fatalf("unexpected failure entry: %+v", entries[2])
	}
	if entries[3].Event != logging.EventMCPResult || entries[3].Fields["ok"] != false {
		t.Fatalf("unexpected result entry: %+v", entries[3])
	}
}

func TestInvokeWithLogger_LogsDispatchFailure(t *testing.T) {
	recorder := logging.NewRecorder()
	payload := []byte(`{"project_id":"my-cool-app","task_text":"x","phase":"execute"}`)

	_, err := InvokeWithLogger(context.Background(), unconfigured.New(), "context", payload, recorder)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Code != "NOT_IMPLEMENTED" {
		t.Fatalf("unexpected error code: %s", err.Code)
	}

	entries := recorder.Entries()
	if len(entries) != 6 {
		t.Fatalf("expected 6 log entries, got %d", len(entries))
	}
	if entries[3].Event != logging.EventMCPDispatch || entries[3].Fields["ok"] != false {
		t.Fatalf("unexpected dispatch failure entry: %+v", entries[3])
	}
	if entries[4].Event != logging.EventMCPFailure || entries[4].Fields["stage"] != "dispatch" {
		t.Fatalf("unexpected failure entry: %+v", entries[4])
	}
	if entries[5].Event != logging.EventMCPResult || entries[5].Fields["ok"] != false {
		t.Fatalf("unexpected result entry: %+v", entries[5])
	}
}
