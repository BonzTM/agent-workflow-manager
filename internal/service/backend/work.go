package backend

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func (s *Service) Work(ctx context.Context, payload v1.WorkPayload) (v1.WorkResult, *core.APIError) {
	if s == nil || s.repo == nil {
		return v1.WorkResult{}, backendError(v1.ErrCodeInternalError, "service repository is not configured", nil)
	}

	projectID := strings.TrimSpace(payload.ProjectID)
	rawPlanKey := payload.PlanKey
	planKey := strings.TrimSpace(rawPlanKey)
	receiptID := strings.TrimSpace(payload.ReceiptID)
	if rawPlanKey != "" && rawPlanKey != planKey {
		return v1.WorkResult{}, backendError(v1.ErrCodeInvalidInput, "plan_key must not include surrounding whitespace", map[string]any{
			"project_id": projectID,
			"plan_key":   rawPlanKey,
		})
	}
	if planKey == "" && receiptID != "" {
		planKey = "plan:" + receiptID
	}
	if planKey != "" {
		derivedReceiptID, ok := parsePlanFetchKey(planKey)
		if !ok {
			return v1.WorkResult{}, backendError(v1.ErrCodeInvalidInput, "plan_key must use format plan:<receipt_id>", map[string]any{
				"project_id": projectID,
				"plan_key":   planKey,
			})
		}
		if receiptID == "" {
			receiptID = derivedReceiptID
		} else if receiptID != derivedReceiptID {
			return v1.WorkResult{}, backendError(v1.ErrCodeInvalidInput, "plan_key and receipt_id must reference the same receipt", map[string]any{
				"project_id":          projectID,
				"plan_key":            planKey,
				"receipt_id":          receiptID,
				"plan_key_receipt_id": derivedReceiptID,
			})
		}
	}
	if planKey == "" {
		return v1.WorkResult{}, backendError(v1.ErrCodeInvalidInput, "plan_key or receipt_id is required", map[string]any{
			"project_id": projectID,
			"plan_key":   planKey,
		})
	}

	workItems := workPayloadTasks(payload)

	upsertInput := core.WorkPlanUpsertInput{
		ProjectID: projectID,
		PlanKey:   planKey,
		ReceiptID: receiptID,
		Mode:      normalizeWorkPlanMode(payload.Mode),
		Title:     strings.TrimSpace(payload.PlanTitle),
		Tasks:     workItems,
	}
	if payload.Plan != nil {
		upsertInput.Title = coalesceNonEmpty(strings.TrimSpace(payload.Plan.Title), upsertInput.Title)
		upsertInput.Objective = strings.TrimSpace(payload.Plan.Objective)
		upsertInput.Kind = strings.TrimSpace(payload.Plan.Kind)
		upsertInput.ParentPlanKey = strings.TrimSpace(payload.Plan.ParentPlanKey)
		upsertInput.Status = string(payload.Plan.Status)
		if payload.Plan.Stages != nil {
			upsertInput.Stages = core.WorkPlanStages{
				SpecOutline:        string(payload.Plan.Stages.SpecOutline),
				RefinedSpec:        string(payload.Plan.Stages.RefinedSpec),
				ImplementationPlan: string(payload.Plan.Stages.ImplementationPlan),
			}
		}
		upsertInput.InScope = normalizeValues(payload.Plan.InScope)
		upsertInput.OutOfScope = normalizeValues(payload.Plan.OutOfScope)
		upsertInput.DiscoveredPaths = normalizeCompletionPaths(payload.Plan.DiscoveredPaths)
		upsertInput.Constraints = normalizeValues(payload.Plan.Constraints)
		upsertInput.References = normalizeValues(payload.Plan.References)
		upsertInput.ExternalRefs = normalizeValues(payload.Plan.ExternalRefs)
	}

	upsertResult, err := s.planRepo.UpsertWorkPlan(ctx, upsertInput)
	if err != nil {
		return v1.WorkResult{}, workInternalError("upsert_work_plan", err)
	}

	if receiptID != "" && len(workItems) > 0 {
		if _, err := s.repo.UpsertWorkItems(ctx, core.WorkItemsUpsertInput{
			ProjectID: projectID,
			ReceiptID: receiptID,
			Items:     workItems,
		}); err != nil {
			return v1.WorkResult{}, workInternalError("upsert_work_items", err)
		}
	}

	if updatedPlan, changed, apiErr := s.syncTerminalWorkPlanStatus(ctx, projectID, receiptID, upsertResult.Plan, upsertResult.Plan.Tasks); apiErr != nil {
		return v1.WorkResult{}, apiErr
	} else if changed {
		upsertResult.Plan = updatedPlan
	}

	planStatus := normalizePlanStatus(upsertResult.Plan.Status)
	if planStatus == core.PlanStatusPending {
		planStatus = derivePlanStatusFromWorkItems(normalizeWorkItems(upsertResult.Plan.Tasks))
	}
	return v1.WorkResult{
		PlanKey:    upsertResult.Plan.PlanKey,
		PlanStatus: planStatus,
		Updated:    upsertResult.Updated,
		TaskCount:  len(upsertResult.Plan.Tasks),
	}, nil
}

func workPayloadTasks(payload v1.WorkPayload) []core.WorkItem {
	if len(payload.Tasks) == 0 {
		return nil
	}

	items := make([]core.WorkItem, 0, len(payload.Tasks))
	for _, task := range payload.Tasks {
		var parentTaskKey string
		var parentTaskKeyClear bool
		if task.ParentTaskKey != nil {
			parentTaskKey = strings.TrimSpace(*task.ParentTaskKey)
			if parentTaskKey == "" {
				parentTaskKeyClear = true
			}
		}
		items = append(items, core.WorkItem{
			ItemKey:            task.Key,
			Summary:            strings.TrimSpace(task.Summary),
			Status:             string(task.Status),
			ParentTaskKey:      parentTaskKey,
			ParentTaskKeyClear: parentTaskKeyClear,
			DependsOn:          normalizeValues(task.DependsOn),
			AcceptanceCriteria: normalizeValues(task.AcceptanceCriteria),
			References:         normalizeValues(task.References),
			ExternalRefs:       normalizeValues(task.ExternalRefs),
			BlockedReason:      strings.TrimSpace(task.BlockedReason),
			Outcome:            strings.TrimSpace(task.Outcome),
			Evidence:           normalizeValues(task.Evidence),
		})
	}

	return normalizeWorkItems(items)
}

func normalizeWorkPlanMode(mode v1.WorkPlanMode) core.WorkPlanMode {
	switch strings.TrimSpace(string(mode)) {
	case string(core.WorkPlanModeReplace):
		return core.WorkPlanModeReplace
	default:
		return core.WorkPlanModeMerge
	}
}

func coalesceNonEmpty(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	return strings.TrimSpace(fallback)
}

func planForFetch(plan core.WorkPlan) map[string]any {
	tasks := make([]map[string]any, 0, len(plan.Tasks))
	for _, task := range normalizeWorkItems(plan.Tasks) {
		tasks = append(tasks, workTaskForFetch(plan.PlanKey, task))
	}

	content := map[string]any{
		"plan_key": plan.PlanKey,
		"status":   normalizePlanStatus(plan.Status),
		"title":    strings.TrimSpace(plan.Title),
	}
	if strings.TrimSpace(plan.ReceiptID) != "" {
		content["receipt_id"] = strings.TrimSpace(plan.ReceiptID)
	}
	if strings.TrimSpace(plan.Objective) != "" {
		content["objective"] = strings.TrimSpace(plan.Objective)
	}
	if strings.TrimSpace(plan.Kind) != "" {
		content["kind"] = strings.TrimSpace(plan.Kind)
	}
	if strings.TrimSpace(plan.ParentPlanKey) != "" {
		content["parent_plan_key"] = strings.TrimSpace(plan.ParentPlanKey)
	}
	stages := map[string]any{}
	if strings.TrimSpace(plan.Stages.SpecOutline) != "" {
		stages["spec_outline"] = normalizeWorkItemStatus(plan.Stages.SpecOutline)
	}
	if strings.TrimSpace(plan.Stages.RefinedSpec) != "" {
		stages["refined_spec"] = normalizeWorkItemStatus(plan.Stages.RefinedSpec)
	}
	if strings.TrimSpace(plan.Stages.ImplementationPlan) != "" {
		stages["implementation_plan"] = normalizeWorkItemStatus(plan.Stages.ImplementationPlan)
	}
	if len(stages) > 0 {
		content["stages"] = stages
	}
	if len(plan.InScope) > 0 {
		content["in_scope"] = normalizeValues(plan.InScope)
	}
	if len(plan.OutOfScope) > 0 {
		content["out_of_scope"] = normalizeValues(plan.OutOfScope)
	}
	if len(plan.DiscoveredPaths) > 0 {
		content["discovered_paths"] = normalizeValues(plan.DiscoveredPaths)
	}
	if len(plan.Constraints) > 0 {
		content["constraints"] = normalizeValues(plan.Constraints)
	}
	if len(plan.References) > 0 {
		content["references"] = normalizeValues(plan.References)
	}
	if len(plan.ExternalRefs) > 0 {
		content["external_refs"] = normalizeValues(plan.ExternalRefs)
	}
	content["tasks"] = tasks
	return content
}

func workTaskForFetch(planKey string, task core.WorkItem) map[string]any {
	entry := map[string]any{
		"plan_key": planKey,
		"key":      task.ItemKey,
		"summary":  task.Summary,
		"status":   normalizeWorkItemStatus(task.Status),
	}
	if strings.TrimSpace(task.ParentTaskKey) != "" {
		entry["parent_task_key"] = strings.TrimSpace(task.ParentTaskKey)
	}
	if len(task.DependsOn) > 0 {
		entry["depends_on"] = normalizeValues(task.DependsOn)
	}
	if len(task.AcceptanceCriteria) > 0 {
		entry["acceptance_criteria"] = normalizeValues(task.AcceptanceCriteria)
	}
	if len(task.References) > 0 {
		entry["references"] = normalizeValues(task.References)
	}
	if len(task.ExternalRefs) > 0 {
		entry["external_refs"] = normalizeValues(task.ExternalRefs)
	}
	if strings.TrimSpace(task.BlockedReason) != "" {
		entry["blocked_reason"] = strings.TrimSpace(task.BlockedReason)
	}
	if strings.TrimSpace(task.Outcome) != "" {
		entry["outcome"] = strings.TrimSpace(task.Outcome)
	}
	if len(task.Evidence) > 0 {
		entry["evidence"] = normalizeValues(task.Evidence)
	}
	return entry
}

func receiptForFetch(receiptID string, scope core.ReceiptScope, scopeFound bool, lookup core.FetchLookup, lookupFound bool) map[string]any {
	content := map[string]any{
		"receipt_id": strings.TrimSpace(receiptID),
	}
	if scopeFound {
		if strings.TrimSpace(scope.TaskText) != "" {
			content["task_text"] = strings.TrimSpace(scope.TaskText)
		}
		if strings.TrimSpace(scope.Phase) != "" {
			content["phase"] = strings.TrimSpace(scope.Phase)
		}
		if len(scope.ResolvedTags) > 0 {
			content["resolved_tags"] = normalizeValues(scope.ResolvedTags)
		}
		if len(scope.PointerKeys) > 0 {
			content["pointer_keys"] = normalizeValues(scope.PointerKeys)
		}
		if len(scope.InitialScopePaths) > 0 {
			content["initial_scope_paths"] = normalizeValues(scope.InitialScopePaths)
		}
		content["baseline_captured"] = scope.BaselineCaptured
		if len(scope.BaselinePaths) > 0 {
			baseline := make([]map[string]any, 0, len(scope.BaselinePaths))
			for _, entry := range scope.BaselinePaths {
				item := map[string]any{
					"path":    normalizeCompletionPath(entry.Path),
					"deleted": entry.Deleted,
				}
				if strings.TrimSpace(entry.ContentHash) != "" {
					item["content_hash"] = strings.TrimSpace(entry.ContentHash)
				}
				baseline = append(baseline, item)
			}
			content["baseline_paths"] = baseline
		}
	}
	if lookupFound {
		latestRun := map[string]any{
			"run_id":      lookup.RunID,
			"status":      strings.TrimSpace(lookup.RunStatus),
			"plan_status": normalizePlanStatus(lookup.PlanStatus),
		}
		if len(lookup.WorkItems) > 0 {
			tasks := make([]map[string]any, 0, len(lookup.WorkItems))
			for _, task := range normalizeWorkItems(lookup.WorkItems) {
				tasks = append(tasks, workTaskForFetch("plan:"+strings.TrimSpace(receiptID), task))
			}
			if len(tasks) > 0 {
				latestRun["tasks"] = tasks
			}
		}
		content["latest_run"] = latestRun
	}
	return content
}

func runForFetch(run core.RunHistorySummary) map[string]any {
	content := map[string]any{
		"run_id": run.RunID,
		"status": strings.TrimSpace(run.Status),
	}
	if strings.TrimSpace(run.ReceiptID) != "" {
		content["receipt_id"] = strings.TrimSpace(run.ReceiptID)
	}
	if strings.TrimSpace(run.RequestID) != "" {
		content["request_id"] = strings.TrimSpace(run.RequestID)
	}
	if strings.TrimSpace(run.TaskText) != "" {
		content["task_text"] = strings.TrimSpace(run.TaskText)
	}
	if strings.TrimSpace(run.Phase) != "" {
		content["phase"] = strings.TrimSpace(run.Phase)
	}
	if len(run.FilesChanged) > 0 {
		content["files_changed"] = normalizeValues(run.FilesChanged)
	}
	if strings.TrimSpace(run.Outcome) != "" {
		content["outcome"] = strings.TrimSpace(run.Outcome)
	}
	if !run.UpdatedAt.IsZero() {
		content["updated_at"] = run.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return content
}

func workItemsFromPaths(paths []string) []core.WorkItem {
	normalizedPaths := normalizeCompletionPaths(paths)
	if len(normalizedPaths) == 0 {
		return nil
	}

	items := make([]core.WorkItem, 0, len(normalizedPaths))
	for _, itemKey := range normalizedPaths {
		items = append(items, core.WorkItem{
			ItemKey: itemKey,
			Status:  core.WorkItemStatusComplete,
		})
	}

	return normalizeWorkItems(items)
}

func normalizeWorkItems(items []core.WorkItem) []core.WorkItem {
	if len(items) == 0 {
		return nil
	}

	priority := map[string]int{
		core.WorkItemStatusComplete:   0,
		core.WorkItemStatusSuperseded: 0,
		core.WorkItemStatusPending:    1,
		core.WorkItemStatusInProgress: 2,
		core.WorkItemStatusBlocked:    3,
	}

	byItemKey := make(map[string]core.WorkItem, len(items))
	for _, item := range items {
		normalizedKey := normalizeCompletionPath(item.ItemKey)
		if normalizedKey == "" {
			continue
		}

		normalizedStatus := normalizeWorkItemStatus(item.Status)
		normalizedItem := core.WorkItem{
			ItemKey:            normalizedKey,
			Summary:            strings.TrimSpace(item.Summary),
			Status:             normalizedStatus,
			ParentTaskKey:      strings.TrimSpace(item.ParentTaskKey),
			DependsOn:          normalizeValues(item.DependsOn),
			AcceptanceCriteria: normalizeValues(item.AcceptanceCriteria),
			References:         normalizeValues(item.References),
			ExternalRefs:       normalizeValues(item.ExternalRefs),
			BlockedReason:      strings.TrimSpace(item.BlockedReason),
			Outcome:            strings.TrimSpace(item.Outcome),
			Evidence:           normalizeValues(item.Evidence),
			UpdatedAt:          item.UpdatedAt.UTC(),
		}
		current, exists := byItemKey[normalizedKey]
		if !exists || priority[normalizedStatus] >= priority[current.Status] {
			byItemKey[normalizedKey] = normalizedItem
		}
	}

	if len(byItemKey) == 0 {
		return nil
	}

	keys := make([]string, 0, len(byItemKey))
	for key := range byItemKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	normalized := make([]core.WorkItem, 0, len(keys))
	for _, key := range keys {
		normalized = append(normalized, byItemKey[key])
	}
	return normalized
}

func normalizeWorkItemStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case core.WorkItemStatusBlocked:
		return core.WorkItemStatusBlocked
	case core.WorkItemStatusInProgress:
		return core.WorkItemStatusInProgress
	case core.WorkItemStatusComplete, "completed":
		return core.WorkItemStatusComplete
	case core.WorkItemStatusSuperseded:
		return core.WorkItemStatusSuperseded
	default:
		return core.WorkItemStatusPending
	}
}

func normalizePlanStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case core.PlanStatusBlocked:
		return core.PlanStatusBlocked
	case core.PlanStatusInProgress:
		return core.PlanStatusInProgress
	case core.PlanStatusComplete, "completed":
		return core.PlanStatusComplete
	case core.PlanStatusSuperseded:
		return core.PlanStatusSuperseded
	default:
		return core.PlanStatusPending
	}
}

func derivePlanStatusFromWorkItems(items []core.WorkItem) string {
	if len(items) == 0 {
		return core.PlanStatusPending
	}

	hasPending := false
	hasInProgress := false
	hasBlocked := false
	hasComplete := false
	hasSuperseded := false

	for _, item := range items {
		switch normalizeWorkItemStatus(item.Status) {
		case core.WorkItemStatusBlocked:
			hasBlocked = true
		case core.WorkItemStatusInProgress:
			hasInProgress = true
		case core.WorkItemStatusComplete:
			hasComplete = true
		case core.WorkItemStatusSuperseded:
			hasSuperseded = true
		default:
			hasPending = true
		}
	}

	switch {
	case hasBlocked:
		return core.PlanStatusBlocked
	case hasInProgress:
		return core.PlanStatusInProgress
	case hasPending:
		return core.PlanStatusPending
	case hasComplete:
		return core.PlanStatusComplete
	case hasSuperseded:
		return core.PlanStatusSuperseded
	default:
		return core.PlanStatusPending
	}
}

func isTerminalWorkItemStatus(raw string) bool {
	switch normalizeWorkItemStatus(raw) {
	case core.WorkItemStatusComplete, core.WorkItemStatusSuperseded:
		return true
	default:
		return false
	}
}

func isTerminalPlanStatus(raw string) bool {
	switch normalizePlanStatus(raw) {
	case core.PlanStatusComplete, core.PlanStatusSuperseded:
		return true
	default:
		return false
	}
}

func workInternalError(operation string, err error) *core.APIError {
	return backendError(v1.ErrCodeInternalError, "failed to persist work state", map[string]any{
		"operation": operation,
		"error":     err.Error(),
	})
}
