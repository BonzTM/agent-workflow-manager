package sqlite

import (
	"github.com/bonztm/agent-workflow-manager/internal/core"
	storagedomain "github.com/bonztm/agent-workflow-manager/internal/storage/domain"
)

const (
	workPlanListScopeCurrent   = storagedomain.WorkPlanListScopeCurrent
	workPlanListScopeDeferred  = storagedomain.WorkPlanListScopeDeferred
	workPlanListScopeCompleted = storagedomain.WorkPlanListScopeCompleted
	workPlanListScopeAll       = storagedomain.WorkPlanListScopeAll
)

type normalizedRunSummary = storagedomain.NormalizedRunSummary
type normalizedReceiptScope = storagedomain.NormalizedReceiptScope
type normalizedVerificationBatch = storagedomain.NormalizedVerificationBatch
type normalizedReviewAttempt = storagedomain.NormalizedReviewAttempt

func normalizeWorkPlanMode(raw core.WorkPlanMode) core.WorkPlanMode {
	return storagedomain.NormalizeWorkPlanMode(raw)
}

func normalizeWorkPlanListScope(raw string) string {
	return storagedomain.NormalizeWorkPlanListScope(raw)
}

func workPlanListSearchPattern(raw string) string {
	return storagedomain.WorkPlanListSearchPattern(raw)
}

func normalizeWorkPlanStages(raw core.WorkPlanStages) core.WorkPlanStages {
	return storagedomain.NormalizeWorkPlanStages(raw)
}

func normalizeWorkPlanTasks(tasks []core.WorkItem) []core.WorkItem {
	return storagedomain.NormalizeWorkPlanTasks(tasks)
}

func buildNextWorkPlanState(current core.WorkPlan, found bool, input core.WorkPlanUpsertInput, mode core.WorkPlanMode) core.WorkPlan {
	return storagedomain.BuildNextWorkPlanState(current, found, input, mode)
}

func mergeWorkPlanStages(current, incoming core.WorkPlanStages, mode core.WorkPlanMode) core.WorkPlanStages {
	return storagedomain.MergeWorkPlanStages(current, incoming, mode)
}

func normalizeRunReceiptSummary(input core.RunReceiptSummary) (normalizedRunSummary, error) {
	return storagedomain.NormalizeRunReceiptSummary(input)
}

func normalizeReceiptScope(input core.ReceiptScope) (normalizedReceiptScope, error) {
	return storagedomain.NormalizeReceiptScope(input)
}

func normalizeVerificationBatch(input core.VerificationBatch) (normalizedVerificationBatch, error) {
	return storagedomain.NormalizeVerificationBatch(input)
}

func normalizeReviewAttempt(input core.ReviewAttempt) (normalizedReviewAttempt, error) {
	return storagedomain.NormalizeReviewAttempt(input)
}

func normalizeSyncApplyInput(input core.SyncApplyInput) (core.SyncApplyInput, error) {
	return storagedomain.NormalizeSyncApplyInput(input)
}

func normalizeWorkItems(items []core.WorkItem) ([]core.WorkItem, error) {
	return storagedomain.NormalizeWorkItems(items)
}

func derivePlanStatus(items []core.WorkItem) string {
	return storagedomain.DerivePlanStatus(items)
}

func storageWorkItemStatus(raw string) string {
	return storagedomain.StorageWorkItemStatus(raw)
}

func normalizeWorkItemStatus(raw string) string {
	return storagedomain.NormalizeWorkItemStatus(raw)
}

func normalizeSyncPath(raw string) string {
	return storagedomain.NormalizeRepoPath(raw)
}
