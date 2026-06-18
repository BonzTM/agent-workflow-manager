package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/buildinfo"
	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	"github.com/bonztm/agent-workflow-manager/internal/logging"
)

const (
	mcpProtocolVersion = "2025-03-26"
	mcpServerName      = "awm-mcp"
	mcpToolsCallID     = "mcp.tools/call"
)

type Server struct {
	svc         core.Service
	logger      logging.Logger
	initialized bool
	now         func() time.Time
}

type initializeParams struct {
	ProtocolVersion string         `json:"protocolVersion,omitempty"`
	Capabilities    map[string]any `json:"capabilities,omitempty"`
	ClientInfo      *struct {
		Name    string `json:"name,omitempty"`
		Version string `json:"version,omitempty"`
	} `json:"clientInfo,omitempty"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type toolsCallResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

func NewServer(svc core.Service, logger logging.Logger) *Server {
	return &Server{
		svc:    svc,
		logger: logging.Normalize(logger),
		now:    time.Now,
	}
}

func (s *Server) handleInitialize(req JSONRPCRequest) JSONRPCResponse {
	var params initializeParams
	if rpcErr := decodeOptionalParams(req.Params, &params); rpcErr != nil {
		return NewJSONRPCErrorResponse(req.ResponseID(), rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}
	s.initialized = true
	return NewJSONRPCResultResponse(req.ResponseID(), map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    mcpServerName,
			"version": buildinfo.Version(),
		},
	})
}

func (s *Server) handleToolsList(req JSONRPCRequest) JSONRPCResponse {
	var params struct{}
	if rpcErr := decodeOptionalParams(req.Params, &params); rpcErr != nil {
		return NewJSONRPCErrorResponse(req.ResponseID(), rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}
	return NewJSONRPCResultResponse(req.ResponseID(), map[string]any{"tools": ToolDefinitions()})
}

func (s *Server) handleToolsCall(req JSONRPCRequest) JSONRPCResponse {
	params, rpcErr := decodeToolsCallParams(req.Params)
	if rpcErr != nil {
		return NewJSONRPCErrorResponse(req.ResponseID(), rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}

	arguments := normalizeToolArguments(params.Arguments)
	result, apiErr := InvokeWithLogger(context.Background(), s.svc, params.Name, arguments, s.logger)
	envelope := toolCallEnvelope(params.Name, requestIDForEnvelope(req.ResponseID()), s.now(), result, apiErr)
	payload, err := json.Marshal(envelope)
	if err != nil {
		return NewJSONRPCErrorResponse(req.ResponseID(), JSONRPCInternalError, "internal error", err.Error())
	}

	callResult := toolsCallResult{
		Content: []textContent{{Type: "text", Text: string(payload)}},
		IsError: apiErr != nil,
	}
	return NewJSONRPCResultResponse(req.ResponseID(), callResult)
}

func (s *Server) dispatch(req JSONRPCRequest) *JSONRPCResponse {
	if rpcErr := validateJSONRPCRequest(req); rpcErr != nil {
		resp := NewJSONRPCErrorResponse(req.ResponseID(), rpcErr.Code, rpcErr.Message, rpcErr.Data)
		return &resp
	}

	if req.IsNotification() {
		s.handleNotification(req)
		return nil
	}

	var resp JSONRPCResponse
	switch req.Method {
	case "initialize":
		resp = s.handleInitialize(req)
	case "notifications/initialized":
		s.initialized = true
		resp = NewJSONRPCResultResponse(req.ResponseID(), map[string]any{})
	case "tools/list":
		resp = s.handleToolsList(req)
	case "tools/call":
		resp = s.handleToolsCall(req)
	default:
		resp = NewJSONRPCErrorResponse(req.ResponseID(), JSONRPCMethodNotFound, "method not found", req.Method)
	}
	return &resp
}

func (s *Server) handleNotification(req JSONRPCRequest) {
	if req.Method == "notifications/initialized" {
		s.initialized = true
	}
}

func decodeOptionalParams(raw json.RawMessage, target any) *JSONRPCError {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if err := json.Unmarshal(trimmed, target); err != nil {
		return &JSONRPCError{Code: JSONRPCInvalidParams, Message: "invalid params", Data: err.Error()}
	}
	return nil
}

func decodeToolsCallParams(raw json.RawMessage) (toolsCallParams, *JSONRPCError) {
	var params toolsCallParams
	if rpcErr := decodeOptionalParams(raw, &params); rpcErr != nil {
		return toolsCallParams{}, rpcErr
	}
	params.Name = strings.TrimSpace(params.Name)
	if params.Name == "" {
		return toolsCallParams{}, &JSONRPCError{Code: JSONRPCInvalidParams, Message: "invalid params", Data: "name is required"}
	}
	return params, nil
}

func normalizeToolArguments(raw json.RawMessage) []byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return []byte(`{}`)
	}
	return trimmed
}

func requestIDForEnvelope(id any) string {
	switch value := id.(type) {
	case string:
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	case int:
		return strconv.Itoa(value)
	case int8:
		return strconv.FormatInt(int64(value), 10)
	case int16:
		return strconv.FormatInt(int64(value), 10)
	case int32:
		return strconv.FormatInt(int64(value), 10)
	case int64:
		return strconv.FormatInt(value, 10)
	case uint:
		return strconv.FormatUint(uint64(value), 10)
	case uint8:
		return strconv.FormatUint(uint64(value), 10)
	case uint16:
		return strconv.FormatUint(uint64(value), 10)
	case uint32:
		return strconv.FormatUint(uint64(value), 10)
	case uint64:
		return strconv.FormatUint(value, 10)
	case float32:
		return strconv.FormatFloat(float64(value), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case json.Number:
		return value.String()
	}
	return mcpToolsCallID
}

func toolCallEnvelope(tool, requestID string, now time.Time, result any, apiErr *core.APIError) v1.ResultEnvelope {
	command, _ := v1.CommandFromToolName(tool)
	envelope := v1.ResultEnvelope{
		Version:   v1.Version,
		Command:   command,
		RequestID: strings.TrimSpace(requestID),
		OK:        apiErr == nil,
		Timestamp: now.UTC().Format(time.RFC3339),
		Result:    result,
	}
	if apiErr != nil {
		envelope.Result = nil
		envelope.Error = apiErr.ToPayload()
	}
	return envelope
}
