package commands

import (
	"context"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

type handlerFunc func(context.Context, core.Service, any) (any, *core.APIError)

func typedHandler[T any](invoke func(context.Context, core.Service, T) (any, *core.APIError)) handlerFunc {
	return func(ctx context.Context, svc core.Service, payload any) (any, *core.APIError) {
		typed, ok := payload.(T)
		if !ok {
			return nil, core.NewErrorWithSource(v1.ErrCodeInternalError, "payload type does not match command", v1.ErrSourceDispatch, nil)
		}
		return invoke(ctx, svc, typed)
	}
}

var handlers = map[v1.Command]handlerFunc{
	v1.CommandContext: typedHandler(func(ctx context.Context, svc core.Service, payload v1.ContextPayload) (any, *core.APIError) {
		return svc.Context(ctx, payload)
	}),
	v1.CommandFetch: typedHandler(func(ctx context.Context, svc core.Service, payload v1.FetchPayload) (any, *core.APIError) {
		return svc.Fetch(ctx, payload)
	}),
	v1.CommandExport: typedHandler(func(ctx context.Context, svc core.Service, payload v1.ExportPayload) (any, *core.APIError) {
		return svc.Export(ctx, payload)
	}),
	v1.CommandDone: typedHandler(func(ctx context.Context, svc core.Service, payload v1.DonePayload) (any, *core.APIError) {
		return svc.Done(ctx, payload)
	}),
	v1.CommandReview: typedHandler(func(ctx context.Context, svc core.Service, payload v1.ReviewPayload) (any, *core.APIError) {
		return svc.Review(ctx, payload)
	}),
	v1.CommandWork: typedHandler(func(ctx context.Context, svc core.Service, payload v1.WorkPayload) (any, *core.APIError) {
		return svc.Work(ctx, payload)
	}),
	v1.CommandHistorySearch: typedHandler(func(ctx context.Context, svc core.Service, payload v1.HistorySearchPayload) (any, *core.APIError) {
		return svc.HistorySearch(ctx, payload)
	}),
	v1.CommandSync: typedHandler(func(ctx context.Context, svc core.Service, payload v1.SyncPayload) (any, *core.APIError) {
		return svc.Sync(ctx, payload)
	}),
	v1.CommandHealth: typedHandler(func(ctx context.Context, svc core.Service, payload v1.HealthPayload) (any, *core.APIError) {
		return svc.Health(ctx, payload)
	}),
	v1.CommandStatus: typedHandler(func(ctx context.Context, svc core.Service, payload v1.StatusPayload) (any, *core.APIError) {
		return svc.Status(ctx, payload)
	}),
	v1.CommandVerify: typedHandler(func(ctx context.Context, svc core.Service, payload v1.VerifyPayload) (any, *core.APIError) {
		return svc.Verify(ctx, payload)
	}),
	v1.CommandInit: typedHandler(func(ctx context.Context, svc core.Service, payload v1.InitPayload) (any, *core.APIError) {
		return svc.Init(ctx, payload)
	}),
}

// Dispatch routes a decoded command payload to the matching core.Service
// method. It returns an INVALID_COMMAND error for unrecognized commands and
// an INTERNAL_ERROR when the payload type does not match the command.
func Dispatch(ctx context.Context, svc core.Service, command v1.Command, payload any) (any, *core.APIError) {
	handler, ok := handlers[command]
	if !ok {
		return nil, core.NewErrorWithSource(v1.ErrCodeInvalidCommand, "command is not recognized", v1.ErrSourceDispatch, nil)
	}
	return handler(ctx, svc, payload)
}

// ProjectIDFromPayload extracts the project_id field from any known command
// payload type, returning "" when the payload carries none.
func ProjectIDFromPayload(payload any) string {
	return v1.ProjectIDFromPayload(payload)
}
