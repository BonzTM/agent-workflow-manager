package backend

import (
	"context"
	"errors"
	"fmt"
	"strings"

	bootstrapkit "github.com/bonztm/agent-workflow-manager/internal/bootstrap"
	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	"github.com/bonztm/agent-workflow-manager/internal/workspace"
)

// Done closes out a run receipt: it verifies the changed files against the
// receipt scope, evaluates definition-of-done requirements, and records a run
// summary when the closeout is accepted (or rejects it in strict scope mode).
func (s *Service) Done(ctx context.Context, payload v1.DonePayload) (v1.DoneResult, *core.APIError) {
	if s == nil || s.repo == nil {
		return v1.DoneResult{}, backendError(v1.ErrCodeInternalError, "service repository is not configured", nil)
	}

	receiptID, planKey, apiErr := resolveReceiptPlanSelection(payload.ProjectID, payload.ReceiptID, payload.PlanKey)
	if apiErr != nil {
		return v1.DoneResult{}, apiErr
	}

	scope, err := s.repo.FetchReceiptScope(ctx, core.ReceiptScopeQuery{
		ProjectID: payload.ProjectID,
		ReceiptID: receiptID,
	})
	if err != nil {
		if errors.Is(err, core.ErrReceiptScopeNotFound) {
			return v1.DoneResult{}, backendError(v1.ErrCodeNotFound, "receipt scope was not found", map[string]any{
				"project_id": strings.TrimSpace(payload.ProjectID),
				"receipt_id": receiptID,
			})
		}
		return v1.DoneResult{}, reportCompletionInternalError("fetch_receipt_scope", err)
	}

	plan, apiErr := s.loadEffectiveWorkPlan(ctx, payload.ProjectID, receiptID, planKey)
	if apiErr != nil {
		return v1.DoneResult{}, apiErr
	}

	detectedFiles, reliableDetection, apiErr := s.detectReceiptChangedPaths(ctx, s.defaultProjectRoot(), scope)
	if apiErr != nil {
		return v1.DoneResult{}, apiErr
	}
	filesChanged, apiErr := resolveDoneFilesChanged(reliableDetection, detectedFiles, payload.FilesChanged, payload.NoFileChanges)
	if apiErr != nil {
		return v1.DoneResult{}, apiErr
	}
	violations := mergeCompletionViolations(
		filesChangedMismatchViolations(reliableDetection, detectedFiles, payload.FilesChanged),
		outOfScopeViolations(filesChanged, completionEffectiveScopePaths(scope, plan)),
	)

	workItems, err := s.repo.ListWorkItems(ctx, core.FetchLookupQuery{
		ProjectID: payload.ProjectID,
		ReceiptID: receiptID,
	})
	if err != nil {
		return v1.DoneResult{}, reportCompletionInternalError("list_work_items", err)
	}
	if plan != nil {
		if _, _, syncErr := s.syncTerminalWorkPlanStatus(ctx, payload.ProjectID, receiptID, *plan, workItems); syncErr != nil {
			return v1.DoneResult{}, syncErr
		}
	}

	workflowRequirements, workflowSource, err := s.loadWorkflowCompletionRequirements(s.defaultProjectRoot(), "", payload.TagsFile)
	if err != nil {
		return v1.DoneResult{}, workflowDefinitionsAPIError(workflowSource.SourcePath, err)
	}
	requiredTaskKeys := defaultCompletionRequiredTaskKeys(len(filesChanged) > 0)
	var requiredWorkflowDefinitions []workflowRequiredTaskDefinition
	if hasConfiguredWorkflowRequiredTasks(workflowSource, workflowRequirements) {
		requiredWorkflowDefinitions = matchWorkflowRequiredTaskDefinitions(workflowRequirements, verifySelectionContext{
			Phase:        v1.Phase(strings.TrimSpace(scope.Phase)),
			Tags:         normalizeValues(scope.ResolvedTags),
			FilesChanged: filesChanged,
		})
		requiredTaskKeys = make([]string, 0, len(requiredWorkflowDefinitions))
		for _, definition := range requiredWorkflowDefinitions {
			requiredTaskKeys = append(requiredTaskKeys, definition.Key)
		}
	}

	definitionOfDoneIssues, apiErr := s.evaluateDefinitionOfDoneIssues(ctx, payload.ProjectID, receiptID, planKey, workItems, requiredTaskKeys, requiredWorkflowDefinitions, scope, workflowSource.SourcePath)
	if apiErr != nil {
		return v1.DoneResult{}, apiErr
	}
	scopeMode := effectiveScopeMode(payload.ScopeMode)
	if scopeMode == v1.ScopeModeStrict && (len(violations) > 0 || len(definitionOfDoneIssues) > 0) {
		return v1.DoneResult{
			Accepted:               false,
			Violations:             violations,
			DefinitionOfDoneIssues: definitionOfDoneIssues,
		}, nil
	}

	runStatus := "accepted"
	if len(violations) > 0 {
		runStatus = "accepted_with_warnings"
	}
	if len(definitionOfDoneIssues) > 0 && runStatus == "accepted" {
		runStatus = "accepted_with_warnings"
	}

	ids, err := s.repo.SaveRunReceiptSummary(ctx, core.RunReceiptSummary{
		ProjectID:              payload.ProjectID,
		ReceiptID:              receiptID,
		TaskText:               scope.TaskText,
		Phase:                  scope.Phase,
		ResolvedTags:           scope.ResolvedTags,
		PointerKeys:            scope.PointerKeys,
		Status:                 runStatus,
		FilesChanged:           filesChanged,
		DefinitionOfDoneIssues: definitionOfDoneIssues,
		Outcome:                strings.TrimSpace(payload.Outcome),
		Actor:                  actorFromPayload(payload.Actor),
	})
	if err != nil {
		return v1.DoneResult{}, reportCompletionInternalError("save_run_receipt_summary", err)
	}

	return v1.DoneResult{
		Accepted:               true,
		Violations:             violations,
		DefinitionOfDoneIssues: definitionOfDoneIssues,
		RunID:                  int(ids.RunID),
	}, nil
}

func completionScopeManagedPaths() []string {
	return normalizeCompletionPaths([]string{
		"AGENTS.md",
		"CLAUDE.md",
		".gitignore",
		workspace.DotEnvExampleFileName,
		bootstrapkit.DefaultInitCandidatesPath,
		canonicalTagsDefaultFilePath,
		canonicalRulesetPrimarySourcePath,
		canonicalRulesetSecondarySourcePath,
		workflowDefinitionsPrimarySourcePath,
		workflowDefinitionsSecondarySourcePath,
		verifyTestsPrimarySourcePath,
		verifyTestsSecondarySourcePath,
	})
}

func (s *Service) evaluateDefinitionOfDoneIssues(ctx context.Context, projectID, receiptID, planKey string, items []core.WorkItem, requiredTaskKeys []string, requiredDefinitions []workflowRequiredTaskDefinition, scope core.ReceiptScope, workflowSourcePath string) ([]string, *core.APIError) {
	normalizedRequiredTaskKeys := normalizeCompletionRequiredTaskKeys(requiredTaskKeys)
	if len(normalizedRequiredTaskKeys) == 0 {
		return nil, nil
	}

	normalizedItems := normalizeWorkItems(items)
	if len(normalizedItems) == 0 {
		issues := make([]string, 0, len(normalizedRequiredTaskKeys))
		for _, requiredKey := range normalizedRequiredTaskKeys {
			issues = append(issues, missingCompletionWorkItemIssue(requiredKey))
		}
		return issues, nil
	}

	statusByKey := make(map[string]string, len(normalizedItems))
	for _, item := range normalizedItems {
		statusByKey[item.ItemKey] = normalizeWorkItemStatus(item.Status)
	}

	issues := make([]string, 0, len(normalizedRequiredTaskKeys))
	for _, requiredKey := range normalizedRequiredTaskKeys {
		status, ok := statusByKey[requiredKey]
		if !ok {
			issues = append(issues, missingCompletionWorkItemIssue(requiredKey))
			continue
		}
		if status != core.WorkItemStatusComplete {
			issues = append(issues, incompleteCompletionWorkItemIssue(requiredKey, status))
		}
	}

	var currentPlan *core.WorkPlan
	for _, definition := range requiredDefinitions {
		if definition.Run == nil {
			continue
		}
		status, ok := statusByKey[definition.Key]
		if !ok || status != core.WorkItemStatusComplete {
			continue
		}
		attempts, apiErr := s.listReviewAttempts(ctx, projectID, receiptID, definition.Key)
		if apiErr != nil {
			return nil, apiErr
		}
		if len(attempts) == 0 {
			issues = append(issues, missingReviewExecutionIssue(definition.Key))
			continue
		}
		if !definition.RerunRequiresNewFingerprint {
			attempt, found := latestReviewAttempt(attempts)
			if !found || !attempt.Passed {
				issues = append(issues, missingReviewExecutionIssue(definition.Key))
			}
			continue
		}
		if currentPlan == nil {
			plan, planErr := s.loadEffectiveWorkPlan(ctx, projectID, receiptID, planKey)
			if planErr != nil {
				return nil, planErr
			}
			currentPlan = plan
		}
		fingerprint, apiErr := computeReviewFingerprint(s.defaultProjectRoot(), projectID, receiptID, definition.Key, workflowSourcePath, *definition.Run, scope, currentPlan)
		if apiErr != nil {
			return nil, apiErr
		}
		attempt, ok := latestReviewAttemptByFingerprint(attempts, fingerprint)
		if !ok || !attempt.Passed {
			issues = append(issues, staleReviewCompletionWorkItemIssue(definition.Key))
		}
	}

	if len(issues) == 0 {
		return nil, nil
	}
	return issues, nil
}

func missingCompletionWorkItemIssue(requiredKey string) string {
	if requiredKey == requiredVerifyTestsKey {
		return "required verification work item is missing: " + requiredKey
	}
	return "required workflow work item is missing: " + requiredKey
}

func incompleteCompletionWorkItemIssue(requiredKey, status string) string {
	if requiredKey == requiredVerifyTestsKey {
		return fmt.Sprintf("required verification work item is not complete: %s (status=%s)", requiredKey, status)
	}
	return fmt.Sprintf("required workflow work item is not complete: %s (status=%s)", requiredKey, status)
}

func staleReviewCompletionWorkItemIssue(requiredKey string) string {
	return "required workflow review is stale for the current scoped fingerprint: " + requiredKey
}

func missingReviewExecutionIssue(requiredKey string) string {
	return "required workflow review has no passing execution: " + requiredKey
}

func reportCompletionInternalError(operation string, err error) *core.APIError {
	return backendError(v1.ErrCodeInternalError, "failed to finish task closeout", map[string]any{
		"operation": operation,
		"error":     err.Error(),
	})
}

func effectiveScopeMode(mode v1.ScopeMode) v1.ScopeMode {
	switch mode {
	case v1.ScopeModeStrict:
		return v1.ScopeModeStrict
	case v1.ScopeModeWarn:
		return v1.ScopeModeWarn
	default:
		return v1.ScopeModeWarn
	}
}
