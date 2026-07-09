package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	"github.com/bonztm/agent-workflow-manager/internal/logging"
	"github.com/bonztm/agent-workflow-manager/internal/runtime"
)

func TestRunMCP_VersionFlagPrintsBanner(t *testing.T) {
	t.Setenv(runtime.LogSinkEnvVar, "discard")
	var stdout bytes.Buffer

	code := RunMCP([]string{"--version"}, nil, &stdout, nil)
	if code != 0 {
		t.Fatalf("unexpected exit code: got %d want 0", code)
	}
	text := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(text, "awm-mcp ") {
		t.Fatalf("unexpected version output: %q", text)
	}
}

func TestRunMCP_HelpFlagPrintsUsage(t *testing.T) {
	t.Setenv(runtime.LogSinkEnvVar, "discard")
	var stdout bytes.Buffer

	code := RunMCP([]string{"--help"}, nil, &stdout, nil)
	if code != 0 {
		t.Fatalf("unexpected exit code: got %d want 0", code)
	}
	text := stdout.String()
	if !strings.Contains(text, "awm-mcp - MCP JSON-RPC 2.0 stdio server") {
		t.Fatalf("unexpected help output: %q", text)
	}
	if !strings.Contains(text, "tools/call") {
		t.Fatalf("expected tools/call in help output: %q", text)
	}
}

func TestRunMCP_DefaultModeStartsServer(t *testing.T) {
	t.Setenv(runtime.LogSinkEnvVar, "discard")
	original := newMCPService
	t.Cleanup(func() { newMCPService = original })
	newMCPService = func(_ context.Context, _ logging.Logger) (core.Service, runtime.CleanupFunc, error) {
		return mcpMainFakeService{}, nil, nil
	}

	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}` + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunMCP(nil, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit code: got %d want 0 stderr=%q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}

	resp := decodeSingleJSONRPCResponse(t, stdout.Bytes())
	if resp.Error != nil {
		t.Fatalf("unexpected response error: %+v", resp.Error)
	}
	result := decodeInitializeResult(t, resp.Result)
	if result.ProtocolVersion != mcpProtocolVersion {
		t.Fatalf("unexpected protocol version: %q", result.ProtocolVersion)
	}
}

func TestPrintVersionWritesBinaryBanner(t *testing.T) {
	var out bytes.Buffer
	printVersion(&out, "awm-mcp")
	text := strings.TrimSpace(out.String())
	if !strings.HasPrefix(text, "awm-mcp ") {
		t.Fatalf("unexpected version output: %q", text)
	}
}

func fixedMCPNow() time.Time {
	return time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
}

type mcpMainFakeService struct{}

func (mcpMainFakeService) Context(_ context.Context, _ v1.ContextPayload) (v1.ContextResult, *core.APIError) {
	return v1.ContextResult{Status: "ok"}, nil
}

func (mcpMainFakeService) Fetch(_ context.Context, _ v1.FetchPayload) (v1.FetchResult, *core.APIError) {
	return v1.FetchResult{}, nil
}

func (mcpMainFakeService) Export(_ context.Context, payload v1.ExportPayload) (v1.ExportResult, *core.APIError) {
	return v1.ExportResult{
		Format:  payload.Format,
		Content: "# export",
		Document: &v1.ExportDocument{
			Kind: v1.ExportDocumentKindFetchBundle,
		},
	}, nil
}

func (mcpMainFakeService) Review(_ context.Context, _ v1.ReviewPayload) (v1.ReviewResult, *core.APIError) {
	return v1.ReviewResult{}, nil
}

func (mcpMainFakeService) Done(_ context.Context, _ v1.DonePayload) (v1.DoneResult, *core.APIError) {
	return v1.DoneResult{}, nil
}

func (mcpMainFakeService) Work(_ context.Context, _ v1.WorkPayload) (v1.WorkResult, *core.APIError) {
	return v1.WorkResult{}, nil
}

func (mcpMainFakeService) HistorySearch(_ context.Context, _ v1.HistorySearchPayload) (v1.HistorySearchResult, *core.APIError) {
	return v1.HistorySearchResult{}, nil
}

func (mcpMainFakeService) Sync(_ context.Context, _ v1.SyncPayload) (v1.SyncResult, *core.APIError) {
	return v1.SyncResult{}, nil
}

func (mcpMainFakeService) Health(_ context.Context, _ v1.HealthPayload) (v1.HealthResult, *core.APIError) {
	return v1.HealthResult{Mode: "check", Check: &v1.HealthCheckResult{}}, nil
}

func (mcpMainFakeService) Status(_ context.Context, _ v1.StatusPayload) (v1.StatusResult, *core.APIError) {
	return v1.StatusResult{}, nil
}

func (mcpMainFakeService) Verify(_ context.Context, _ v1.VerifyPayload) (v1.VerifyResult, *core.APIError) {
	return v1.VerifyResult{}, nil
}

func (mcpMainFakeService) Init(_ context.Context, _ v1.InitPayload) (v1.InitResult, *core.APIError) {
	return v1.InitResult{}, nil
}

func decodeSingleJSONRPCResponse(t *testing.T, raw []byte) JSONRPCResponse {
	t.Helper()
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		t.Fatal("expected JSON-RPC response output")
	}
	var resp JSONRPCResponse
	if err := json.Unmarshal([]byte(trimmed), &resp); err != nil {
		t.Fatalf("unmarshal JSON-RPC response: %v", err)
	}
	return resp
}

type initializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	Capabilities    struct {
		Tools map[string]any `json:"tools"`
	} `json:"capabilities"`
	ServerInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

func decodeInitializeResult(t *testing.T, value any) initializeResult {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal initialize result: %v", err)
	}
	var result initializeResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatalf("unmarshal initialize result: %v", err)
	}
	return result
}
