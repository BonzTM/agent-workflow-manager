package commands

import (
	"context"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

type handlerFunc func(context.Context, core.Service, any) (any, *core.APIError)

var handlers = map[v1.Command]handlerFunc{
	v1.CommandContext: func(ctx context.Context, svc core.Service, payload any) (any, *core.APIError) {
		return svc.Context(ctx, payload.(v1.ContextPayload))
	},
	v1.CommandFetch: func(ctx context.Context, svc core.Service, payload any) (any, *core.APIError) {
		return svc.Fetch(ctx, payload.(v1.FetchPayload))
	},
	v1.CommandExport: func(ctx context.Context, svc core.Service, payload any) (any, *core.APIError) {
		return svc.Export(ctx, payload.(v1.ExportPayload))
	},
	v1.CommandDone: func(ctx context.Context, svc core.Service, payload any) (any, *core.APIError) {
		return svc.Done(ctx, payload.(v1.DonePayload))
	},
	v1.CommandReview: func(ctx context.Context, svc core.Service, payload any) (any, *core.APIError) {
		return svc.Review(ctx, payload.(v1.ReviewPayload))
	},
	v1.CommandWork: func(ctx context.Context, svc core.Service, payload any) (any, *core.APIError) {
		return svc.Work(ctx, payload.(v1.WorkPayload))
	},
	v1.CommandHistorySearch: func(ctx context.Context, svc core.Service, payload any) (any, *core.APIError) {
		return svc.HistorySearch(ctx, payload.(v1.HistorySearchPayload))
	},
	v1.CommandSync: func(ctx context.Context, svc core.Service, payload any) (any, *core.APIError) {
		return svc.Sync(ctx, payload.(v1.SyncPayload))
	},
	v1.CommandHealth: func(ctx context.Context, svc core.Service, payload any) (any, *core.APIError) {
		return svc.Health(ctx, payload.(v1.HealthPayload))
	},
	v1.CommandStatus: func(ctx context.Context, svc core.Service, payload any) (any, *core.APIError) {
		return svc.Status(ctx, payload.(v1.StatusPayload))
	},
	v1.CommandVerify: func(ctx context.Context, svc core.Service, payload any) (any, *core.APIError) {
		return svc.Verify(ctx, payload.(v1.VerifyPayload))
	},
	v1.CommandInit: func(ctx context.Context, svc core.Service, payload any) (any, *core.APIError) {
		return svc.Init(ctx, payload.(v1.InitPayload))
	},
}

func Dispatch(ctx context.Context, svc core.Service, command v1.Command, payload any) (any, *core.APIError) {
	handler, ok := handlers[command]
	if !ok {
		return nil, core.NewErrorWithSource(v1.ErrCodeInvalidCommand, "command is not recognized", v1.ErrSourceDispatch, nil)
	}
	return handler(ctx, svc, payload)
}

func ProjectIDFromPayload(payload any) string {
	return v1.ProjectIDFromPayload(payload)
}
