package cli

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/commands"
	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	"github.com/bonztm/agent-workflow-manager/internal/logging"
	"github.com/bonztm/agent-workflow-manager/internal/runtime"
)

const (
	commandFetch = v1.CommandFetch
	commandWork  = v1.CommandWork
)

func Run(ctx context.Context, svc core.Service, in io.Reader, out io.Writer, now func() time.Time) int {
	return RunWithLogger(ctx, svc, in, out, now, nil)
}

func RunWithLogger(ctx context.Context, svc core.Service, in io.Reader, out io.Writer, now func() time.Time, logger logging.Logger) int {
	logger = logging.Normalize(logger)

	input, err := io.ReadAll(in)
	if err != nil {
		logger.Error(ctx, logging.EventCLIIngressRead, "ok", false, "error_code", v1.ErrCodeReadFailed)
		logger.Error(ctx, logging.EventCLIFailure, "stage", "read", "error_code", v1.ErrCodeReadFailed)
		writeEnvelope(out, v1.ResultEnvelope{
			Version:   v1.Version,
			Command:   "",
			RequestID: "",
			OK:        false,
			Timestamp: now().UTC().Format(time.RFC3339),
			Error:     &v1.ErrorPayload{Code: v1.ErrCodeReadFailed, Message: err.Error(), Source: v1.ErrSourceAdapter},
		})
		logger.Info(ctx, logging.EventCLIResult, "ok", false, "error_code", v1.ErrCodeReadFailed)
		return 1
	}
	logger.Info(ctx, logging.EventCLIIngressRead, "ok", true, "bytes", len(input))

	env, payload, valErr := v1.DecodeAndValidateCommandWithDefaults(input, validationDefaultsFromRuntime())
	projectID := projectIDFromPayload(payload)
	if valErr != nil {
		logger.Error(ctx, logging.EventCLIIngressValidate, "ok", false, "command", string(env.Command), "request_id", env.RequestID, "error_code", valErr.Code)
		logger.Error(ctx, logging.EventCLIFailure, "stage", "validate", "command", string(env.Command), "request_id", env.RequestID, "error_code", valErr.Code)
		writeEnvelope(out, v1.ResultEnvelope{
			Version:   v1.Version,
			Command:   env.Command,
			RequestID: env.RequestID,
			OK:        false,
			Timestamp: now().UTC().Format(time.RFC3339),
			Error:     valErr,
		})
		logger.Info(ctx, logging.EventCLIResult, "ok", false, "command", string(env.Command), "request_id", env.RequestID, "error_code", valErr.Code)
		return 1
	}
	validateFields := []any{"ok", true, "command", string(env.Command), "request_id", env.RequestID}
	if projectID != "" {
		validateFields = append(validateFields, "project_id", projectID)
	}
	logger.Info(ctx, logging.EventCLIIngressValidate, validateFields...)

	dispatchStartFields := []any{"phase", "start", "command", string(env.Command), "request_id", env.RequestID}
	if projectID != "" {
		dispatchStartFields = append(dispatchStartFields, "project_id", projectID)
	}
	logger.Info(ctx, logging.EventCLIDispatch, dispatchStartFields...)
	result, apiErr := dispatch(ctx, svc, env.Command, payload)
	if apiErr != nil {
		dispatchFailFields := []any{"phase", "finish", "ok", false, "command", string(env.Command), "request_id", env.RequestID, "error_code", apiErr.Code}
		if projectID != "" {
			dispatchFailFields = append(dispatchFailFields, "project_id", projectID)
		}
		logger.Error(ctx, logging.EventCLIDispatch, dispatchFailFields...)
		logger.Error(ctx, logging.EventCLIFailure, "stage", "dispatch", "command", string(env.Command), "request_id", env.RequestID, "error_code", apiErr.Code)
		writeEnvelope(out, v1.ResultEnvelope{
			Version:   v1.Version,
			Command:   env.Command,
			RequestID: env.RequestID,
			OK:        false,
			Timestamp: now().UTC().Format(time.RFC3339),
			Error:     apiErr.ToPayload(),
		})
		resultFields := []any{"ok", false, "command", string(env.Command), "request_id", env.RequestID, "error_code", apiErr.Code}
		if projectID != "" {
			resultFields = append(resultFields, "project_id", projectID)
		}
		logger.Info(ctx, logging.EventCLIResult, resultFields...)
		return 1
	}
	dispatchFinishFields := []any{"phase", "finish", "ok", true, "command", string(env.Command), "request_id", env.RequestID}
	if projectID != "" {
		dispatchFinishFields = append(dispatchFinishFields, "project_id", projectID)
	}
	logger.Info(ctx, logging.EventCLIDispatch, dispatchFinishFields...)

	writeEnvelope(out, v1.ResultEnvelope{
		Version:   v1.Version,
		Command:   env.Command,
		RequestID: env.RequestID,
		OK:        true,
		Timestamp: now().UTC().Format(time.RFC3339),
		Result:    result,
	})
	resultFields := []any{"ok", true, "command", string(env.Command), "request_id", env.RequestID}
	if projectID != "" {
		resultFields = append(resultFields, "project_id", projectID)
	}
	logger.Info(ctx, logging.EventCLIResult, resultFields...)
	return 0
}

func validationDefaultsFromRuntime() v1.ValidationDefaults {
	cfg := runtime.ConfigFromEnv()
	return v1.ValidationDefaults{
		ProjectID: cfg.EffectiveProjectID(),
	}
}

func dispatch(ctx context.Context, svc core.Service, command v1.Command, payload any) (any, *core.APIError) {
	return commands.Dispatch(ctx, svc, command, payload)
}

func writeEnvelope(out io.Writer, env v1.ResultEnvelope) {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	_ = enc.Encode(env)
}

func projectIDFromPayload(payload any) string {
	return commands.ProjectIDFromPayload(payload)
}
