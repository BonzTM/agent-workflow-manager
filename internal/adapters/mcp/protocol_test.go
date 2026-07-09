package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/buildinfo"
	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/logging"
)

func TestServerInitialize_ReturnsCapabilitiesAndServerInfo(t *testing.T) {
	server := NewServer(mcpMainFakeService{}, logging.NewRecorder())
	req := mustParseJSONRPCRequest(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}`)

	resp := server.handleInitialize(req)
	if resp.Error != nil {
		t.Fatalf("unexpected response error: %+v", resp.Error)
	}
	result := decodeInitializeResult(t, resp.Result)
	if result.ProtocolVersion != mcpProtocolVersion {
		t.Fatalf("unexpected protocol version: got %q want %q", result.ProtocolVersion, mcpProtocolVersion)
	}
	if result.ServerInfo.Name != mcpServerName {
		t.Fatalf("unexpected server name: %q", result.ServerInfo.Name)
	}
	if result.ServerInfo.Version != buildinfo.Version() {
		t.Fatalf("unexpected server version: got %q want %q", result.ServerInfo.Version, buildinfo.Version())
	}
	if result.Capabilities.Tools == nil {
		t.Fatal("expected tools capability")
	}
}

func TestServerInitializedNotification_NoResponse(t *testing.T) {
	server := NewServer(mcpMainFakeService{}, logging.NewRecorder())
	req := mustParseJSONRPCRequest(t, `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)

	resp := server.dispatch(req)
	if resp != nil {
		t.Fatalf("expected no response, got %+v", resp)
	}
	if !server.initialized {
		t.Fatal("expected initialized notification to flip server state")
	}
}

func TestServerToolsList_ReturnsAllTools(t *testing.T) {
	server := NewServer(mcpMainFakeService{}, logging.NewRecorder())
	req := mustParseJSONRPCRequest(t, `{"jsonrpc":"2.0","id":"list-1","method":"tools/list","params":{}}`)

	resp := server.handleToolsList(req)
	if resp.Error != nil {
		t.Fatalf("unexpected response error: %+v", resp.Error)
	}
	result := decodeToolsListResult(t, resp.Result)
	if len(result.Tools) != 12 {
		t.Fatalf("unexpected tool count: got %d want 12", len(result.Tools))
	}
	for _, tool := range result.Tools {
		if tool.Name == "" {
			t.Fatal("expected tool name")
		}
		if tool.Description == "" {
			t.Fatalf("expected description for tool %q", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Fatalf("expected input schema for tool %q", tool.Name)
		}
	}
	if got, want := result.Tools, ToolDefinitions(); len(got) != len(want) {
		t.Fatalf("unexpected tool count drift: got %d want %d", len(got), len(want))
	}
}

func TestServerToolsCall_ValidToolReturnsEnvelopeContent(t *testing.T) {
	server := NewServer(mcpMainFakeService{}, logging.NewRecorder())
	server.now = fixedMCPNow
	req := mustParseJSONRPCRequest(t, `{"jsonrpc":"2.0","id":"call-1","method":"tools/call","params":{"name":"context","arguments":{"project_id":"my-cool-app","task_text":"fix drift","phase":"execute"}}}`)

	resp := server.handleToolsCall(req)
	if resp.Error != nil {
		t.Fatalf("unexpected response error: %+v", resp.Error)
	}
	result := decodeToolsCallResult(t, resp.Result)
	if result.IsError {
		t.Fatalf("expected success result, got %+v", result)
	}
	envelope := decodeResultEnvelope(t, result.Content[0].Text)
	if !envelope.OK {
		t.Fatalf("expected ok result, got %+v", envelope.Error)
	}
	if envelope.Command != v1.CommandContext {
		t.Fatalf("unexpected command: %q", envelope.Command)
	}
	if envelope.RequestID != "call-1" {
		t.Fatalf("unexpected request id: %q", envelope.RequestID)
	}
	if envelope.Timestamp != fixedMCPNow().UTC().Format(time.RFC3339) {
		t.Fatalf("unexpected timestamp: %q", envelope.Timestamp)
	}
	var contextResult v1.ContextResult
	if err := json.Unmarshal(envelope.Result, &contextResult); err != nil {
		t.Fatalf("unmarshal context result: %v", err)
	}
	if contextResult.Status != "ok" {
		t.Fatalf("unexpected context result: %+v", contextResult)
	}
}

func TestServerToolsCall_UnknownToolReturnsApplicationErrorContent(t *testing.T) {
	server := NewServer(mcpMainFakeService{}, logging.NewRecorder())
	server.now = fixedMCPNow
	req := mustParseJSONRPCRequest(t, `{"jsonrpc":"2.0","id":"call-2","method":"tools/call","params":{"name":"bogus","arguments":{}}}`)

	resp := server.handleToolsCall(req)
	if resp.Error != nil {
		t.Fatalf("unexpected response error: %+v", resp.Error)
	}
	result := decodeToolsCallResult(t, resp.Result)
	if !result.IsError {
		t.Fatalf("expected application error result, got %+v", result)
	}
	envelope := decodeResultEnvelope(t, result.Content[0].Text)
	if envelope.OK {
		t.Fatal("expected failed envelope")
	}
	if envelope.Error == nil || envelope.Error.Code != v1.ErrCodeUnknownTool {
		t.Fatalf("unexpected envelope error: %+v", envelope.Error)
	}
}

func TestServerToolsCall_InvalidInputReturnsApplicationErrorContent(t *testing.T) {
	server := NewServer(mcpMainFakeService{}, logging.NewRecorder())
	server.now = fixedMCPNow
	req := mustParseJSONRPCRequest(t, `{"jsonrpc":"2.0","id":"call-3","method":"tools/call","params":{"name":"context","arguments":[]}}`)

	resp := server.handleToolsCall(req)
	if resp.Error != nil {
		t.Fatalf("unexpected response error: %+v", resp.Error)
	}
	result := decodeToolsCallResult(t, resp.Result)
	if !result.IsError {
		t.Fatalf("expected application error result, got %+v", result)
	}
	envelope := decodeResultEnvelope(t, result.Content[0].Text)
	if envelope.OK {
		t.Fatal("expected failed envelope")
	}
	if envelope.Error == nil || envelope.Error.Code != v1.ErrCodeInvalidPayload {
		t.Fatalf("unexpected envelope error: %+v", envelope.Error)
	}
}

func TestServerDispatch_UnknownMethodReturnsJSONRPCError(t *testing.T) {
	server := NewServer(mcpMainFakeService{}, logging.NewRecorder())
	req := mustParseJSONRPCRequest(t, `{"jsonrpc":"2.0","id":"req-unknown","method":"unknown/method"}`)

	resp := server.dispatch(req)
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error == nil || resp.Error.Code != JSONRPCMethodNotFound {
		t.Fatalf("unexpected response error: %+v", resp.Error)
	}
}

func TestServerServe_MalformedJSONReturnsParseError(t *testing.T) {
	server := NewServer(mcpMainFakeService{}, logging.NewRecorder())
	var out bytes.Buffer

	err := server.Serve(nil, strings.NewReader("{\n"), &out) //nolint:staticcheck // SA1012: deliberately passes a nil context to exercise Serve's documented nil-context fallback
	if err != nil {
		t.Fatalf("unexpected serve error: %v", err)
	}
	resp := decodeSingleJSONRPCResponse(t, out.Bytes())
	if resp.Error == nil || resp.Error.Code != JSONRPCParseError {
		t.Fatalf("unexpected response error: %+v", resp.Error)
	}
}

type toolsListResult struct {
	Tools []ToolDef `json:"tools"`
}

func decodeToolsListResult(t *testing.T, value any) toolsListResult {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal tools/list result: %v", err)
	}
	var result toolsListResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatalf("unmarshal tools/list result: %v", err)
	}
	return result
}

func mustParseJSONRPCRequest(t *testing.T, raw string) JSONRPCRequest {
	t.Helper()
	req, rpcErr := ParseJSONRPCRequest([]byte(raw))
	if rpcErr != nil {
		t.Fatalf("parse request: %+v", rpcErr)
	}
	return req
}

func decodeToolsCallResult(t *testing.T, value any) toolsCallResult {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal tools/call result: %v", err)
	}
	var result toolsCallResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatalf("unmarshal tools/call result: %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "text" {
		t.Fatalf("unexpected tools/call content: %+v", result.Content)
	}
	return result
}

type testResultEnvelope struct {
	Version   string           `json:"version"`
	Command   v1.Command       `json:"command"`
	RequestID string           `json:"request_id"`
	OK        bool             `json:"ok"`
	Timestamp string           `json:"timestamp"`
	Result    json.RawMessage  `json:"result,omitempty"`
	Error     *v1.ErrorPayload `json:"error,omitempty"`
}

func decodeResultEnvelope(t *testing.T, raw string) testResultEnvelope {
	t.Helper()
	var envelope testResultEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("unmarshal result envelope: %v", err)
	}
	return envelope
}
