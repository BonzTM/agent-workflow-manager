package unconfigured

import (
	"context"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

type Service struct{}

func New() *Service {
	return &Service{}
}

func (s *Service) Context(_ context.Context, _ v1.ContextPayload) (v1.ContextResult, *core.APIError) {
	return v1.ContextResult{}, notImplemented("context")
}

func (s *Service) Fetch(_ context.Context, _ v1.FetchPayload) (v1.FetchResult, *core.APIError) {
	return v1.FetchResult{}, notImplemented("fetch")
}

func (s *Service) Export(_ context.Context, _ v1.ExportPayload) (v1.ExportResult, *core.APIError) {
	return v1.ExportResult{}, notImplemented("export")
}

func (s *Service) Review(_ context.Context, _ v1.ReviewPayload) (v1.ReviewResult, *core.APIError) {
	return v1.ReviewResult{}, notImplemented("review")
}

func (s *Service) Work(_ context.Context, _ v1.WorkPayload) (v1.WorkResult, *core.APIError) {
	return v1.WorkResult{}, notImplemented("work")
}

func (s *Service) HistorySearch(_ context.Context, _ v1.HistorySearchPayload) (v1.HistorySearchResult, *core.APIError) {
	return v1.HistorySearchResult{}, notImplemented("history")
}

func (s *Service) Done(_ context.Context, _ v1.DonePayload) (v1.DoneResult, *core.APIError) {
	return v1.DoneResult{}, notImplemented("done")
}

func (s *Service) Sync(_ context.Context, _ v1.SyncPayload) (v1.SyncResult, *core.APIError) {
	return v1.SyncResult{}, notImplemented("sync")
}

func (s *Service) Health(_ context.Context, _ v1.HealthPayload) (v1.HealthResult, *core.APIError) {
	return v1.HealthResult{}, notImplemented("health")
}

func (s *Service) Status(_ context.Context, _ v1.StatusPayload) (v1.StatusResult, *core.APIError) {
	return v1.StatusResult{}, notImplemented("status")
}

func (s *Service) Verify(_ context.Context, _ v1.VerifyPayload) (v1.VerifyResult, *core.APIError) {
	return v1.VerifyResult{}, notImplemented("verify")
}

func (s *Service) Init(_ context.Context, _ v1.InitPayload) (v1.InitResult, *core.APIError) {
	return v1.InitResult{}, notImplemented("init")
}

func notImplemented(op string) *core.APIError {
	return core.NewErrorWithSource(
		v1.ErrCodeNotImplemented,
		"service backend for operation is not wired yet",
		v1.ErrSourceBackend,
		map[string]any{"operation": op},
	)
}
