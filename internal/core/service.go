package core

import (
	"context"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
)

// Service is the core API surface of the workflow manager. Each method
// handles one v1 operation, returning the operation's result or an
// *APIError describing the failure.
type Service interface {
	Context(context.Context, v1.ContextPayload) (v1.ContextResult, *APIError)
	Fetch(context.Context, v1.FetchPayload) (v1.FetchResult, *APIError)
	Export(context.Context, v1.ExportPayload) (v1.ExportResult, *APIError)
	Review(context.Context, v1.ReviewPayload) (v1.ReviewResult, *APIError)
	Work(context.Context, v1.WorkPayload) (v1.WorkResult, *APIError)
	HistorySearch(context.Context, v1.HistorySearchPayload) (v1.HistorySearchResult, *APIError)
	Done(context.Context, v1.DonePayload) (v1.DoneResult, *APIError)
	Sync(context.Context, v1.SyncPayload) (v1.SyncResult, *APIError)
	Health(context.Context, v1.HealthPayload) (v1.HealthResult, *APIError)
	Status(context.Context, v1.StatusPayload) (v1.StatusResult, *APIError)
	Verify(context.Context, v1.VerifyPayload) (v1.VerifyResult, *APIError)
	Init(context.Context, v1.InitPayload) (v1.InitResult, *APIError)
}
