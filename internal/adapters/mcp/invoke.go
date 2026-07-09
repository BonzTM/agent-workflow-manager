package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/bonztm/agent-workflow-manager/internal/commands"
	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	"github.com/bonztm/agent-workflow-manager/internal/logging"
	"github.com/bonztm/agent-workflow-manager/internal/runtime"
)

// ToolDef describes one MCP tool as advertised in a tools/list response:
// its name, human-readable description, and JSON Schema for the input.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolDefinitions returns the MCP tool definitions for every v1 command,
// each with an input schema referencing the shared v1 command schema.
func ToolDefinitions() []ToolDef {
	specs := v1.CommandSpecs()
	defs := make([]ToolDef, 0, len(specs))
	for _, spec := range specs {
		defs = append(defs, ToolDef{
			Name:        string(spec.Command),
			Description: spec.ToolDescription,
			InputSchema: schemaRef(commandSchemaID, spec.InputSchemaDef),
		})
	}
	return defs
}

const (
	schemaDraft202012 = "https://json-schema.org/draft/2020-12/schema"
	commandSchemaID   = "https://agent-workflow-manager.dev/spec/v1/cli.command.schema.json"
)

func schemaRef(schemaID, defName string) map[string]any {
	return map[string]any{
		"$schema": schemaDraft202012,
		"$ref":    fmt.Sprintf("%s#/$defs/%s", schemaID, defName),
	}
}

// Invoke executes the named MCP tool against svc with the raw JSON input and
// returns the command result, without emitting logs. Unknown tools and
// invalid payloads yield an *core.APIError instead of a result.
func Invoke(ctx context.Context, svc core.Service, tool string, input []byte) (any, *core.APIError) {
	return InvokeWithLogger(ctx, svc, tool, input, nil)
}

// InvokeWithLogger behaves like Invoke while emitting structured ingress,
// dispatch, and result events to logger (a nil logger is normalized to a
// no-op).
func InvokeWithLogger(ctx context.Context, svc core.Service, tool string, input []byte, logger logging.Logger) (any, *core.APIError) {
	logger = logging.Normalize(logger)
	logger.Info(ctx, logging.EventMCPIngressRead, "ok", true, "tool", tool, "bytes", len(input))

	command, ok := v1.CommandFromToolName(tool)
	if !ok {
		err := core.NewErrorWithSource(v1.ErrCodeUnknownTool, "tool is not supported in v1", v1.ErrSourceAdapter, map[string]any{"tool": tool})
		logger.Error(ctx, logging.EventMCPIngressValidate, "ok", false, "tool", tool, "error_code", err.Code)
		logger.Error(ctx, logging.EventMCPFailure, "stage", "validate", "tool", tool, "error_code", err.Code)
		logger.Info(ctx, logging.EventMCPResult, "ok", false, "tool", tool, "error_code", err.Code)
		return nil, err
	}
	rawProjectID := projectIDFromRawToolInput(input)
	defaults := validationDefaultsFromRuntime()
	effectiveProjectID := rawProjectID
	if effectiveProjectID == "" {
		effectiveProjectID = defaults.ProjectID
	}

	payload, apiErr := decodeValidatedToolPayload(command, json.RawMessage(input), defaults)
	if apiErr != nil {
		return mcpToolInputError(ctx, logger, tool, effectiveProjectID, apiErr)
	}

	normalizedProjectID := commands.ProjectIDFromPayload(payload)
	if normalizedProjectID == "" {
		normalizedProjectID = effectiveProjectID
	}
	logMCPValidateSuccess(ctx, logger, tool, normalizedProjectID)
	logMCPDispatchStart(ctx, logger, tool, normalizedProjectID)

	payload = runtime.ApplyActorDefaults(payload, os.LookupEnv)

	result, apiErr := commands.Dispatch(ctx, svc, command, payload)
	if apiErr != nil {
		return mcpDispatchError(ctx, logger, tool, normalizedProjectID, apiErr)
	}

	logMCPDispatchSuccess(ctx, logger, tool, normalizedProjectID)
	logMCPResultSuccess(ctx, logger, tool, normalizedProjectID)
	return result, nil
}

func decodeValidatedToolPayload(command v1.Command, raw json.RawMessage, defaults v1.ValidationDefaults) (any, *core.APIError) {
	blob, err := v1.BuildEnvelopeForCommand(command, "mcp.invoke", raw)
	if err != nil {
		return nil, core.NewErrorWithSource(v1.ErrCodeInvalidJSON, err.Error(), v1.ErrSourceAdapter, nil)
	}
	_, payload, valErr := v1.DecodeAndValidateCommandWithDefaults(blob, defaults)
	if valErr != nil {
		return nil, core.NewErrorWithSource(valErr.Code, valErr.Message, valErr.Source, nil)
	}
	return payload, nil
}

func validationDefaultsFromRuntime() v1.ValidationDefaults {
	cfg := runtime.ConfigFromEnv()
	return v1.ValidationDefaults{
		ProjectID: cfg.EffectiveProjectID(),
	}
}

func projectIDFromRawToolInput(input []byte) string {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(input, &payload); err != nil {
		return ""
	}
	raw, ok := payload["project_id"]
	if !ok {
		return ""
	}
	var projectID string
	if err := json.Unmarshal(raw, &projectID); err != nil {
		return ""
	}
	return strings.TrimSpace(projectID)
}

func mcpToolInputError(ctx context.Context, logger logging.Logger, tool, projectID string, apiErr *core.APIError) (any, *core.APIError) {
	fields := []any{"ok", false, "tool", tool, "error_code", apiErr.Code}
	if projectID != "" {
		fields = append(fields, "project_id", projectID)
	}
	logger.Error(ctx, logging.EventMCPIngressValidate, fields...)
	failureFields := []any{"stage", "validate", "tool", tool, "error_code", apiErr.Code}
	if projectID != "" {
		failureFields = append(failureFields, "project_id", projectID)
	}
	logger.Error(ctx, logging.EventMCPFailure, failureFields...)
	resultFields := []any{"ok", false, "tool", tool, "error_code", apiErr.Code}
	if projectID != "" {
		resultFields = append(resultFields, "project_id", projectID)
	}
	logger.Info(ctx, logging.EventMCPResult, resultFields...)
	return nil, apiErr
}

func mcpDispatchError(ctx context.Context, logger logging.Logger, tool, projectID string, apiErr *core.APIError) (any, *core.APIError) {
	dispatchFields := []any{"phase", "finish", "ok", false, "tool", tool, "error_code", apiErr.Code}
	if projectID != "" {
		dispatchFields = append(dispatchFields, "project_id", projectID)
	}
	logger.Error(ctx, logging.EventMCPDispatch, dispatchFields...)
	failureFields := []any{"stage", "dispatch", "tool", tool, "error_code", apiErr.Code}
	if projectID != "" {
		failureFields = append(failureFields, "project_id", projectID)
	}
	logger.Error(ctx, logging.EventMCPFailure, failureFields...)
	resultFields := []any{"ok", false, "tool", tool, "error_code", apiErr.Code}
	if projectID != "" {
		resultFields = append(resultFields, "project_id", projectID)
	}
	logger.Info(ctx, logging.EventMCPResult, resultFields...)
	return nil, apiErr
}

func logMCPValidateSuccess(ctx context.Context, logger logging.Logger, tool, projectID string) {
	fields := []any{"ok", true, "tool", tool}
	if projectID != "" {
		fields = append(fields, "project_id", projectID)
	}
	logger.Info(ctx, logging.EventMCPIngressValidate, fields...)
}

func logMCPDispatchStart(ctx context.Context, logger logging.Logger, tool, projectID string) {
	fields := []any{"phase", "start", "tool", tool}
	if projectID != "" {
		fields = append(fields, "project_id", projectID)
	}
	logger.Info(ctx, logging.EventMCPDispatch, fields...)
}

func logMCPDispatchSuccess(ctx context.Context, logger logging.Logger, tool, projectID string) {
	fields := []any{"phase", "finish", "ok", true, "tool", tool}
	if projectID != "" {
		fields = append(fields, "project_id", projectID)
	}
	logger.Info(ctx, logging.EventMCPDispatch, fields...)
}

func logMCPResultSuccess(ctx context.Context, logger logging.Logger, tool, projectID string) {
	fields := []any{"ok", true, "tool", tool}
	if projectID != "" {
		fields = append(fields, "project_id", projectID)
	}
	logger.Info(ctx, logging.EventMCPResult, fields...)
}
