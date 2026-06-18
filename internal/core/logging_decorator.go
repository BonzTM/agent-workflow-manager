package core

import (
	"context"
	"strings"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/logging"
)

type loggingService struct {
	next   Service
	logger logging.Logger
	now    func() time.Time
}

var _ Service = (*loggingService)(nil)

func WithLogging(next Service, logger logging.Logger) Service {
	return WithLoggingClock(next, logger, time.Now)
}

func WithLoggingClock(next Service, logger logging.Logger, now func() time.Time) Service {
	if next == nil {
		return nil
	}
	if now == nil {
		now = time.Now
	}
	return &loggingService{
		next:   next,
		logger: logging.Normalize(logger),
		now:    now,
	}
}

func (s *loggingService) Context(ctx context.Context, payload v1.ContextPayload) (v1.ContextResult, *APIError) {
	return withOperation(ctx, s.now, s.logger, logging.OperationContext, payload.ProjectID, func() (v1.ContextResult, *APIError) {
		return s.next.Context(ctx, payload)
	})
}

func (s *loggingService) Fetch(ctx context.Context, payload v1.FetchPayload) (v1.FetchResult, *APIError) {
	return withOperation(ctx, s.now, s.logger, logging.OperationFetch, payload.ProjectID, func() (v1.FetchResult, *APIError) {
		return s.next.Fetch(ctx, payload)
	})
}

func (s *loggingService) Export(ctx context.Context, payload v1.ExportPayload) (v1.ExportResult, *APIError) {
	return withOperation(ctx, s.now, s.logger, logging.OperationExport, payload.ProjectID, func() (v1.ExportResult, *APIError) {
		return s.next.Export(ctx, payload)
	})
}


func (s *loggingService) Review(ctx context.Context, payload v1.ReviewPayload) (v1.ReviewResult, *APIError) {
	return withOperation(ctx, s.now, s.logger, logging.OperationReview, payload.ProjectID, func() (v1.ReviewResult, *APIError) {
		return s.next.Review(ctx, payload)
	})
}

func (s *loggingService) Work(ctx context.Context, payload v1.WorkPayload) (v1.WorkResult, *APIError) {
	return withOperation(ctx, s.now, s.logger, logging.OperationWork, payload.ProjectID, func() (v1.WorkResult, *APIError) {
		return s.next.Work(ctx, payload)
	})
}

func (s *loggingService) HistorySearch(ctx context.Context, payload v1.HistorySearchPayload) (v1.HistorySearchResult, *APIError) {
	return withOperation(ctx, s.now, s.logger, logging.OperationHistorySearch, payload.ProjectID, func() (v1.HistorySearchResult, *APIError) {
		return s.next.HistorySearch(ctx, payload)
	})
}

func (s *loggingService) Done(ctx context.Context, payload v1.DonePayload) (v1.DoneResult, *APIError) {
	return withOperation(ctx, s.now, s.logger, logging.OperationDone, payload.ProjectID, func() (v1.DoneResult, *APIError) {
		return s.next.Done(ctx, payload)
	})
}

func (s *loggingService) Sync(ctx context.Context, payload v1.SyncPayload) (v1.SyncResult, *APIError) {
	return withOperation(ctx, s.now, s.logger, logging.OperationSync, payload.ProjectID, func() (v1.SyncResult, *APIError) {
		return s.next.Sync(ctx, payload)
	})
}

func (s *loggingService) Health(ctx context.Context, payload v1.HealthPayload) (v1.HealthResult, *APIError) {
	return withOperation(ctx, s.now, s.logger, logging.OperationHealth, payload.ProjectID, func() (v1.HealthResult, *APIError) {
		return s.next.Health(ctx, payload)
	})
}

func (s *loggingService) Status(ctx context.Context, payload v1.StatusPayload) (v1.StatusResult, *APIError) {
	return withOperation(ctx, s.now, s.logger, logging.OperationStatus, payload.ProjectID, func() (v1.StatusResult, *APIError) {
		return s.next.Status(ctx, payload)
	})
}

func (s *loggingService) Verify(ctx context.Context, payload v1.VerifyPayload) (v1.VerifyResult, *APIError) {
	return withOperation(ctx, s.now, s.logger, logging.OperationVerify, payload.ProjectID, func() (v1.VerifyResult, *APIError) {
		return s.next.Verify(ctx, payload)
	})
}

func (s *loggingService) Init(ctx context.Context, payload v1.InitPayload) (v1.InitResult, *APIError) {
	return withOperation(ctx, s.now, s.logger, logging.OperationInit, payload.ProjectID, func() (v1.InitResult, *APIError) {
		return s.next.Init(ctx, payload)
	})
}

func withOperation[T any](ctx context.Context, now func() time.Time, logger logging.Logger, operation, projectID string, run func() (T, *APIError)) (T, *APIError) {
	startedAt := now()
	projectID = strings.TrimSpace(projectID)

	startFields := []any{"operation", operation}
	if projectID != "" {
		startFields = append(startFields, "project_id", projectID)
	}
	logger.Info(ctx, logging.EventServiceOperationStart, startFields...)

	result, apiErr := run()
	durationMS := now().Sub(startedAt).Milliseconds()
	if durationMS < 0 {
		durationMS = 0
	}

	finishFields := []any{
		"operation", operation,
		"duration_ms", durationMS,
		"ok", apiErr == nil,
	}
	if projectID != "" {
		finishFields = append(finishFields, "project_id", projectID)
	}
	if apiErr != nil && strings.TrimSpace(apiErr.Code) != "" {
		finishFields = append(finishFields, "error_code", apiErr.Code)
	}

	if apiErr != nil {
		logger.Error(ctx, logging.EventServiceOperationFinish, finishFields...)
	} else {
		logger.Info(ctx, logging.EventServiceOperationFinish, finishFields...)
	}

	return result, apiErr
}
