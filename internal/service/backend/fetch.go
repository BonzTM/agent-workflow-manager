package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

// Fetch resolves the requested keys (plan:, receipt:, task:, run:, or pointer
// keys) into their current content, reporting keys that were not found and any
// mismatches against the caller's expected versions.
func (s *Service) Fetch(ctx context.Context, payload v1.FetchPayload) (v1.FetchResult, *core.APIError) {
	if s == nil || s.repo == nil {
		return v1.FetchResult{}, backendError(v1.ErrCodeInternalError, "service repository is not configured", nil)
	}

	projectID := strings.TrimSpace(payload.ProjectID)
	keys := fetchPayloadKeys(payload)

	items := make([]v1.FetchItem, 0, len(keys))
	notFound := make([]string, 0, len(keys))
	versionMismatches := make([]v1.FetchVersionMismatch, 0, len(keys))

	for _, key := range keys {
		var (
			item  v1.FetchItem
			found bool
			err   error
		)

		if taskRef, ok := parseTaskFetchKey(key); ok {
			item, found, err = s.fetchTaskItem(ctx, projectID, key, taskRef)
			if err != nil {
				return v1.FetchResult{}, fetchInternalError(fetchOperationFromError(err), err)
			}
		} else if receiptID, ok := parsePlanFetchKey(key); ok {
			item, found, err = s.fetchPlanItem(ctx, projectID, key, receiptID)
			if err != nil {
				return v1.FetchResult{}, fetchInternalError(fetchOperationFromError(err), err)
			}
		} else if receiptID, ok := parseReceiptFetchKey(key); ok {
			item, found, err = s.fetchReceiptItem(ctx, projectID, key, receiptID)
			if err != nil {
				return v1.FetchResult{}, fetchInternalError(fetchOperationFromError(err), err)
			}
		} else if runID, ok := parseRunFetchKey(key); ok {
			item, found, err = s.fetchRunItem(ctx, projectID, key, runID)
			if err != nil {
				return v1.FetchResult{}, fetchInternalError(fetchOperationFromError(err), err)
			}
		} else {
			item, found, err = s.fetchPointerItem(ctx, projectID, key)
			if err != nil {
				return v1.FetchResult{}, fetchInternalError("lookup_pointer_by_key", err)
			}
		}

		if !found {
			notFound = append(notFound, key)
			continue
		}
		items = append(items, item)

		expectedVersion := strings.TrimSpace(payload.ExpectedVersions[key])
		if expectedVersion != "" && expectedVersion != item.Version {
			versionMismatches = append(versionMismatches, v1.FetchVersionMismatch{
				Key:      key,
				Expected: expectedVersion,
				Actual:   item.Version,
			})
		}
	}

	result := v1.FetchResult{Items: items}
	if len(notFound) > 0 {
		result.NotFound = notFound
	}
	if len(versionMismatches) > 0 {
		result.VersionMismatches = versionMismatches
	}

	return result, nil
}

func fetchPayloadKeys(payload v1.FetchPayload) []string {
	return fetchPayloadKeysWithReceipt(payload.Keys, payload.ReceiptID)
}

func fetchPayloadKeysWithReceipt(keys []string, receiptID string) []string {
	normalizedKeys := normalizeValues(keys)
	if len(normalizedKeys) > 0 {
		return normalizedKeys
	}

	trimmedReceiptID := strings.TrimSpace(receiptID)
	if trimmedReceiptID == "" {
		return nil
	}

	return []string{"plan:" + trimmedReceiptID}
}

func parsePlanFetchKey(raw string) (string, bool) {
	key := strings.TrimSpace(raw)
	if !strings.HasPrefix(key, "plan:") {
		return "", false
	}
	receiptID := strings.TrimSpace(key[len("plan:"):])
	if !requestIDPattern.MatchString(receiptID) {
		return "", false
	}
	return receiptID, true
}

func parseReceiptFetchKey(raw string) (string, bool) {
	key := strings.TrimSpace(raw)
	if !strings.HasPrefix(key, "receipt:") {
		return "", false
	}
	receiptID := strings.TrimSpace(key[len("receipt:"):])
	if !requestIDPattern.MatchString(receiptID) {
		return "", false
	}
	return receiptID, true
}

func receiptFetchKey(receiptID string) string {
	normalizedReceiptID := strings.TrimSpace(receiptID)
	if normalizedReceiptID == "" {
		return ""
	}
	fetchKey := "receipt:" + normalizedReceiptID
	if len(fetchKey) > maxFetchKeyLength {
		return ""
	}
	return fetchKey
}

type taskFetchRef struct {
	PlanKey   string
	ReceiptID string
	TaskKey   string
}

func parseTaskFetchKey(raw string) (taskFetchRef, bool) {
	key := strings.TrimSpace(raw)
	if !strings.HasPrefix(key, "task:") {
		return taskFetchRef{}, false
	}

	remainder := strings.TrimSpace(key[len("task:"):])
	separator := strings.Index(remainder, "#")
	if separator <= 0 || separator >= len(remainder)-1 {
		return taskFetchRef{}, false
	}

	planKey := strings.TrimSpace(remainder[:separator])
	receiptID, ok := parsePlanFetchKey(planKey)
	if !ok {
		return taskFetchRef{}, false
	}
	taskKey := strings.TrimSpace(remainder[separator+1:])
	if taskKey == "" {
		return taskFetchRef{}, false
	}

	return taskFetchRef{
		PlanKey:   planKey,
		ReceiptID: receiptID,
		TaskKey:   taskKey,
	}, true
}

func taskFetchKey(planKey, taskKey string) string {
	normalizedPlanKey := strings.TrimSpace(planKey)
	normalizedTaskKey := strings.TrimSpace(taskKey)
	if normalizedPlanKey == "" || normalizedTaskKey == "" {
		return ""
	}
	fetchKey := "task:" + normalizedPlanKey + "#" + normalizedTaskKey
	if len(fetchKey) > maxFetchKeyLength {
		return ""
	}
	return fetchKey
}

func parseRunFetchKey(raw string) (int64, bool) {
	key := strings.TrimSpace(raw)
	if !strings.HasPrefix(key, "run:") {
		return 0, false
	}
	runIDText := strings.TrimSpace(key[len("run:"):])
	if runIDText == "" {
		return 0, false
	}
	runID, err := strconv.ParseInt(runIDText, 10, 64)
	if err != nil || runID <= 0 {
		return 0, false
	}
	return runID, true
}

func runFetchKey(runID int64) string {
	if runID <= 0 {
		return ""
	}
	fetchKey := fmt.Sprintf("run:%d", runID)
	if len(fetchKey) > maxFetchKeyLength {
		return ""
	}
	return fetchKey
}

func (s *Service) fetchPlanItem(ctx context.Context, projectID, key, receiptID string) (v1.FetchItem, bool, error) {
	lookupQuery := core.WorkPlanLookupQuery{
		ProjectID: projectID,
		PlanKey:   strings.TrimSpace(key),
		ReceiptID: strings.TrimSpace(receiptID),
	}
	plan, err := s.planRepo.LookupWorkPlan(ctx, lookupQuery)
	if err == nil {
		workItems := normalizeWorkItems(plan.Tasks)
		planStatus := normalizePlanStatus(plan.Status)
		if planStatus == core.PlanStatusPending {
			planStatus = derivePlanStatusFromWorkItems(workItems)
		}
		planSummary := strings.TrimSpace(plan.Title)
		if planSummary == "" {
			planSummary = fmt.Sprintf("Plan %s is %s", strings.TrimSpace(plan.PlanKey), planStatus)
		}
		contentJSON, mErr := json.Marshal(planForFetch(plan))
		if mErr != nil {
			return v1.FetchItem{}, false, mErr
		}

		version := indexEntryVersion(plan.PlanKey, planStatus, plan.UpdatedAt.UTC().String(), string(contentJSON))
		return v1.FetchItem{
			Key:     key,
			Type:    "plan",
			Summary: planSummary,
			Content: string(contentJSON),
			Status:  planStatus,
			Version: version,
		}, true, nil
	}

	if errors.Is(err, core.ErrWorkPlanNotFound) {
		return v1.FetchItem{}, false, nil
	}
	return v1.FetchItem{}, false, wrapFetchOperationError("lookup_work_plan", err)
}

func (s *Service) fetchReceiptItem(ctx context.Context, projectID, key, receiptID string) (v1.FetchItem, bool, error) {
	normalizedReceiptID := strings.TrimSpace(receiptID)
	if normalizedReceiptID == "" {
		return v1.FetchItem{}, false, nil
	}

	var (
		scope       core.ReceiptScope
		lookup      core.FetchLookup
		scopeFound  bool
		lookupFound bool
	)

	scope, err := s.repo.FetchReceiptScope(ctx, core.ReceiptScopeQuery{
		ProjectID: projectID,
		ReceiptID: normalizedReceiptID,
	})
	if err == nil {
		scopeFound = true
	} else if !errors.Is(err, core.ErrReceiptScopeNotFound) {
		return v1.FetchItem{}, false, wrapFetchOperationError("fetch_receipt_scope", err)
	}

	lookup, err = s.repo.LookupFetchState(ctx, core.FetchLookupQuery{
		ProjectID: projectID,
		ReceiptID: normalizedReceiptID,
	})
	if err == nil {
		lookupFound = true
	} else if !errors.Is(err, core.ErrFetchLookupNotFound) {
		return v1.FetchItem{}, false, wrapFetchOperationError("lookup_fetch_state", err)
	}

	if !scopeFound && !lookupFound {
		return v1.FetchItem{}, false, nil
	}

	contentJSON, err := json.Marshal(receiptForFetch(normalizedReceiptID, scope, scopeFound, lookup, lookupFound))
	if err != nil {
		return v1.FetchItem{}, false, err
	}

	summary := strings.TrimSpace(scope.TaskText)
	if summary == "" {
		summary = "Receipt " + normalizedReceiptID
	}
	status := ""
	versionUpdatedAt := ""
	if lookupFound {
		status = strings.TrimSpace(lookup.RunStatus)
		versionUpdatedAt = lookup.UpdatedAt.UTC().String()
	}
	if versionUpdatedAt == "" && scopeFound {
		versionUpdatedAt = strings.Join(scope.PointerKeys, ",")
	}

	return v1.FetchItem{
		Key:     key,
		Type:    "receipt",
		Summary: summary,
		Content: string(contentJSON),
		Status:  status,
		Version: indexEntryVersion(normalizedReceiptID, summary, status, versionUpdatedAt, string(contentJSON)),
	}, true, nil
}

func (s *Service) fetchRunItem(ctx context.Context, projectID, key string, runID int64) (v1.FetchItem, bool, error) {
	if runID <= 0 {
		return v1.FetchItem{}, false, nil
	}

	if s.historyRepo == nil {
		return v1.FetchItem{}, false, nil
	}

	row, err := s.historyRepo.LookupRunHistory(ctx, core.RunHistoryLookupQuery{
		ProjectID: projectID,
		RunID:     runID,
	})
	if err != nil {
		if errors.Is(err, core.ErrFetchLookupNotFound) {
			return v1.FetchItem{}, false, nil
		}
		return v1.FetchItem{}, false, wrapFetchOperationError("lookup_run_history", err)
	}

	contentJSON, err := json.Marshal(runForFetch(row))
	if err != nil {
		return v1.FetchItem{}, false, err
	}

	summary := strings.TrimSpace(row.Outcome)
	if summary == "" {
		summary = strings.TrimSpace(row.TaskText)
	}
	if summary == "" {
		summary = fmt.Sprintf("Run %d", row.RunID)
	}

	return v1.FetchItem{
		Key:     key,
		Type:    "run",
		Summary: summary,
		Content: string(contentJSON),
		Status:  strings.TrimSpace(row.Status),
		Version: indexEntryVersion(
			strconv.FormatInt(row.RunID, 10),
			strings.TrimSpace(row.RequestID),
			strings.TrimSpace(row.Status),
			row.UpdatedAt.UTC().String(),
			string(contentJSON),
		),
	}, true, nil
}

func (s *Service) fetchTaskItem(ctx context.Context, projectID, key string, ref taskFetchRef) (v1.FetchItem, bool, error) {
	normalizedPlanKey := strings.TrimSpace(ref.PlanKey)
	normalizedTaskKey := strings.TrimSpace(ref.TaskKey)
	if normalizedPlanKey == "" || normalizedTaskKey == "" {
		return v1.FetchItem{}, false, nil
	}

	if s.planRepo != nil {
		plan, err := s.planRepo.LookupWorkPlan(ctx, core.WorkPlanLookupQuery{
			ProjectID: projectID,
			PlanKey:   normalizedPlanKey,
		})
		if err == nil {
			for _, task := range normalizeWorkItems(plan.Tasks) {
				if task.ItemKey != normalizedTaskKey {
					continue
				}

				taskStatus := normalizeWorkItemStatus(task.Status)
				summary := strings.TrimSpace(task.Summary)
				if summary == "" {
					summary = fmt.Sprintf("Task %s in %s is %s", normalizedTaskKey, normalizedPlanKey, taskStatus)
				}
				contentJSON, mErr := json.Marshal(workTaskForFetch(plan.PlanKey, task))
				if mErr != nil {
					return v1.FetchItem{}, false, mErr
				}

				version := indexEntryVersion(
					normalizedPlanKey,
					normalizedTaskKey,
					taskStatus,
					task.UpdatedAt.UTC().String(),
					string(contentJSON),
				)
				return v1.FetchItem{
					Key:     key,
					Type:    "task",
					Summary: summary,
					Content: string(contentJSON),
					Status:  taskStatus,
					Version: version,
				}, true, nil
			}
		} else if !errors.Is(err, core.ErrWorkPlanNotFound) {
			return v1.FetchItem{}, false, wrapFetchOperationError("lookup_work_plan", err)
		}
	}

	if strings.TrimSpace(ref.ReceiptID) == "" {
		return v1.FetchItem{}, false, nil
	}
	workItems, err := s.repo.ListWorkItems(ctx, core.FetchLookupQuery{
		ProjectID: projectID,
		ReceiptID: ref.ReceiptID,
	})
	if err != nil {
		return v1.FetchItem{}, false, wrapFetchOperationError("list_work_items", err)
	}
	for _, task := range normalizeWorkItems(workItems) {
		if task.ItemKey != normalizedTaskKey {
			continue
		}
		taskStatus := normalizeWorkItemStatus(task.Status)
		summary := strings.TrimSpace(task.Summary)
		if summary == "" {
			summary = fmt.Sprintf("Task %s in %s is %s", normalizedTaskKey, normalizedPlanKey, taskStatus)
		}
		contentJSON, err := json.Marshal(workTaskForFetch(normalizedPlanKey, task))
		if err != nil {
			return v1.FetchItem{}, false, err
		}
		version := indexEntryVersion(
			normalizedPlanKey,
			normalizedTaskKey,
			taskStatus,
			task.UpdatedAt.UTC().String(),
			string(contentJSON),
		)
		return v1.FetchItem{
			Key:     key,
			Type:    "task",
			Summary: summary,
			Content: string(contentJSON),
			Status:  taskStatus,
			Version: version,
		}, true, nil
	}

	return v1.FetchItem{}, false, nil
}

func (s *Service) fetchPointerItem(ctx context.Context, projectID, key string) (v1.FetchItem, bool, error) {
	normalizedKey := strings.TrimSpace(key)
	if normalizedKey == "" {
		return v1.FetchItem{}, false, nil
	}

	pointer, err := s.repo.LookupPointerByKey(ctx, core.PointerLookupQuery{
		ProjectID:  projectID,
		PointerKey: normalizedKey,
	})
	if err != nil {
		if errors.Is(err, core.ErrPointerLookupNotFound) {
			return v1.FetchItem{}, false, nil
		}
		return v1.FetchItem{}, false, err
	}

	summary := pointerSummary(pointer)
	versionSeed := indexEntryVersion(pointer.Key, pointer.Path, pointer.Anchor, pointer.Kind, pointer.Label, summary, pointer.UpdatedAt.UTC().String())
	item := v1.FetchItem{
		Key:     key,
		Type:    pointerFetchType(pointer),
		Summary: summary,
		Version: versionSeed,
	}
	content, err := s.readPointerFetchContent(pointer.Path)
	switch {
	case err == nil:
		item.Content = content
		item.Version = indexEntryVersion(versionSeed, content)
	case errors.Is(err, os.ErrNotExist):
		return v1.FetchItem{}, false, nil
	default:
		return v1.FetchItem{}, false, err
	}

	return item, true, nil
}

func pointerFetchType(pointer core.CandidatePointer) string {
	if pointer.IsRule {
		return "rule"
	}
	return "pointer"
}

func (s *Service) readPointerFetchContent(pointerPath string) (string, error) {
	cleanPath := strings.TrimSpace(pointerPath)
	if cleanPath == "" {
		return "", os.ErrNotExist
	}
	if !filepath.IsAbs(cleanPath) {
		cleanPath = filepath.Join(s.defaultProjectRoot(), filepath.FromSlash(cleanPath))
	}
	cleanPath = filepath.Clean(cleanPath)
	content, err := os.ReadFile(cleanPath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func fetchInternalError(operation string, err error) *core.APIError {
	return backendError(v1.ErrCodeInternalError, "failed to fetch context", map[string]any{
		"operation": operation,
		"error":     err.Error(),
	})
}

func wrapFetchOperationError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return &fetchOperationError{operation: operation, err: err}
}

func fetchOperationFromError(err error) string {
	var opErr *fetchOperationError
	if errors.As(err, &opErr) && strings.TrimSpace(opErr.operation) != "" {
		return opErr.operation
	}
	return "lookup_work_plan"
}
