package unconfigured

import (
	"context"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

// Service is the fallback backend used when no storage is configured for the
// project; every operation fails with a not-implemented error so callers get
// a clear signal instead of partial behavior.
type Service struct{}

// New returns the unconfigured fallback Service.
func New() *Service {
	return &Service{}
}

// Context always fails with a not-implemented error because no backend is configured.
func (s *Service) Context(_ context.Context, _ v1.ContextPayload) (v1.ContextResult, *core.APIError) {
	return v1.ContextResult{}, notImplemented("context")
}

// Fetch always fails with a not-implemented error because no backend is configured.
func (s *Service) Fetch(_ context.Context, _ v1.FetchPayload) (v1.FetchResult, *core.APIError) {
	return v1.FetchResult{}, notImplemented("fetch")
}

// Export always fails with a not-implemented error because no backend is configured.
func (s *Service) Export(_ context.Context, _ v1.ExportPayload) (v1.ExportResult, *core.APIError) {
	return v1.ExportResult{}, notImplemented("export")
}

// Review always fails with a not-implemented error because no backend is configured.
func (s *Service) Review(_ context.Context, _ v1.ReviewPayload) (v1.ReviewResult, *core.APIError) {
	return v1.ReviewResult{}, notImplemented("review")
}

// Work always fails with a not-implemented error because no backend is configured.
func (s *Service) Work(_ context.Context, _ v1.WorkPayload) (v1.WorkResult, *core.APIError) {
	return v1.WorkResult{}, notImplemented("work")
}

// HistorySearch always fails with a not-implemented error because no backend is configured.
func (s *Service) HistorySearch(_ context.Context, _ v1.HistorySearchPayload) (v1.HistorySearchResult, *core.APIError) {
	return v1.HistorySearchResult{}, notImplemented("history")
}

// Done always fails with a not-implemented error because no backend is configured.
func (s *Service) Done(_ context.Context, _ v1.DonePayload) (v1.DoneResult, *core.APIError) {
	return v1.DoneResult{}, notImplemented("done")
}

// Sync always fails with a not-implemented error because no backend is configured.
func (s *Service) Sync(_ context.Context, _ v1.SyncPayload) (v1.SyncResult, *core.APIError) {
	return v1.SyncResult{}, notImplemented("sync")
}

// Health always fails with a not-implemented error because no backend is configured.
func (s *Service) Health(_ context.Context, _ v1.HealthPayload) (v1.HealthResult, *core.APIError) {
	return v1.HealthResult{}, notImplemented("health")
}

// Status always fails with a not-implemented error because no backend is configured.
func (s *Service) Status(_ context.Context, _ v1.StatusPayload) (v1.StatusResult, *core.APIError) {
	return v1.StatusResult{}, notImplemented("status")
}

// Verify always fails with a not-implemented error because no backend is configured.
func (s *Service) Verify(_ context.Context, _ v1.VerifyPayload) (v1.VerifyResult, *core.APIError) {
	return v1.VerifyResult{}, notImplemented("verify")
}

// Init always fails with a not-implemented error because no backend is configured.
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
