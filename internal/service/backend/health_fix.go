package backend

import (
	"context"
	"fmt"
	"strings"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

var defaultHealthFixers = []v1.HealthFixer{
	v1.HealthFixerSyncWorkingTree,
	v1.HealthFixerIndexUnindexedFile,
	v1.HealthFixerSyncRuleset,
}

func (s *Service) healthFix(ctx context.Context, payload v1.HealthPayload) (v1.HealthFixResult, *core.APIError) {
	if s == nil || s.repo == nil {
		return v1.HealthFixResult{}, backendError(v1.ErrCodeInternalError, "service repository is not configured", nil)
	}

	projectID := strings.TrimSpace(payload.ProjectID)
	projectRoot := s.effectiveProjectRoot(payload.ProjectRoot)
	rulesFile := strings.TrimSpace(payload.RulesFile)
	tagsFile := strings.TrimSpace(payload.TagsFile)
	dryRun := !effectiveHealthFixApply(payload.Apply)
	fixers := effectiveHealthFixers(payload.Fixers)

	plannedActions := make([]v1.HealthFixAction, 0, len(fixers))
	appliedActions := make([]v1.HealthFixAction, 0, len(fixers))
	totalPlanned := 0
	totalApplied := 0

	for _, fixer := range fixers {
		planned, applied, err := s.executeHealthFixer(ctx, projectID, projectRoot, rulesFile, tagsFile, fixer, dryRun)
		if err != nil {
			return v1.HealthFixResult{}, healthFixInternalError(string(fixer), err)
		}
		plannedActions = append(plannedActions, planned)
		totalPlanned += planned.Count
		if !dryRun {
			appliedActions = append(appliedActions, applied)
			totalApplied += applied.Count
		}
	}

	if dryRun {
		appliedActions = []v1.HealthFixAction{}
	}

	return v1.HealthFixResult{
		DryRun:         dryRun,
		PlannedActions: plannedActions,
		AppliedActions: appliedActions,
		Summary:        healthFixSummary(dryRun, len(fixers), totalPlanned, totalApplied),
	}, nil
}

func (s *Service) executeHealthFixer(ctx context.Context, projectID, projectRoot, rulesFile, tagsFile string, fixer v1.HealthFixer, dryRun bool) (v1.HealthFixAction, v1.HealthFixAction, error) {
	switch fixer {
	case v1.HealthFixerSyncWorkingTree:
		return s.runHealthFixSyncWorkingTree(ctx, projectID, projectRoot, rulesFile, tagsFile, dryRun)
	case v1.HealthFixerIndexUnindexedFile:
		return s.runHealthFixIndexUnindexedFiles(ctx, projectID, projectRoot, tagsFile, dryRun)
	case v1.HealthFixerSyncRuleset:
		return s.runHealthFixSyncRuleset(ctx, projectID, projectRoot, rulesFile, tagsFile, dryRun)
	default:
		return v1.HealthFixAction{}, v1.HealthFixAction{}, fmt.Errorf("unsupported fixer %q", fixer)
	}
}

func (s *Service) runHealthFixSyncWorkingTree(ctx context.Context, projectID, projectRoot, rulesFile, tagsFile string, dryRun bool) (v1.HealthFixAction, v1.HealthFixAction, error) {
	paths, err := s.collectSyncPaths(ctx, syncModeWorkingTree, "", projectRoot)
	if err != nil {
		return v1.HealthFixAction{}, v1.HealthFixAction{}, err
	}

	planned := v1.HealthFixAction{
		Fixer: v1.HealthFixerSyncWorkingTree,
		Count: len(paths),
		Notes: []string{
			fmt.Sprintf("mode=%s", syncModeWorkingTree),
			fmt.Sprintf("impacted_paths=%d", len(paths)),
		},
	}
	if dryRun {
		return planned, v1.HealthFixAction{}, nil
	}

	result, apiErr := s.Sync(ctx, v1.SyncPayload{
		ProjectID:   projectID,
		Mode:        syncModeWorkingTree,
		ProjectRoot: projectRoot,
		RulesFile:   rulesFile,
		TagsFile:    tagsFile,
	})
	if apiErr != nil {
		return v1.HealthFixAction{}, v1.HealthFixAction{}, fmt.Errorf("sync working tree: %s", apiErr.Message)
	}

	applied := v1.HealthFixAction{
		Fixer: v1.HealthFixerSyncWorkingTree,
		Count: len(result.ProcessedPaths),
		Notes: []string{
			fmt.Sprintf("updated=%d", result.Updated),
			fmt.Sprintf("marked_stale=%d", result.MarkedStale),
			fmt.Sprintf("new_candidates=%d", result.NewCandidates),
			fmt.Sprintf("deleted_marked_stale=%d", result.DeletedMarkedStale),
		},
	}
	return planned, applied, nil
}

func (s *Service) runHealthFixIndexUnindexedFiles(ctx context.Context, projectID, projectRoot, tagsFile string, dryRun bool) (v1.HealthFixAction, v1.HealthFixAction, error) {
	inventory, apiErr := s.computeInventoryHealth(ctx, projectID, projectRoot)
	if apiErr != nil {
		return v1.HealthFixAction{}, v1.HealthFixAction{}, fmt.Errorf("collect inventory health: %s", apiErr.Message)
	}

	unindexedPaths := normalizeValues(inventory.UnindexedPaths)
	planned := v1.HealthFixAction{
		Fixer: v1.HealthFixerIndexUnindexedFile,
		Count: len(unindexedPaths),
		Notes: []string{
			fmt.Sprintf("total_files=%d", inventory.Summary.TotalFiles),
			fmt.Sprintf("unindexed_files=%d", len(unindexedPaths)),
		},
	}
	if dryRun {
		return planned, v1.HealthFixAction{}, nil
	}

	tagNormalizer, err := s.loadCanonicalTagNormalizer(projectRoot, tagsFile)
	if err != nil {
		return v1.HealthFixAction{}, v1.HealthFixAction{}, fmt.Errorf("load canonical tags: %w", err)
	}

	violations := make([]v1.CompletionViolation, 0, len(unindexedPaths))
	for _, filePath := range unindexedPaths {
		violations = append(violations, v1.CompletionViolation{Path: filePath, Reason: "health unindexed file"})
	}
	stubs := buildIndexedPointerStubs(projectID, violations, tagNormalizer)
	upserted, err := s.repo.UpsertPointerStubs(ctx, projectID, stubs)
	if err != nil {
		return v1.HealthFixAction{}, v1.HealthFixAction{}, err
	}

	applied := v1.HealthFixAction{
		Fixer: v1.HealthFixerIndexUnindexedFile,
		Count: upserted,
		Notes: []string{
			fmt.Sprintf("unindexed_files=%d", len(unindexedPaths)),
			fmt.Sprintf("upserted_stubs=%d", upserted),
		},
	}
	return planned, applied, nil
}

func (s *Service) runHealthFixSyncRuleset(ctx context.Context, projectID, projectRoot, rulesFile, tagsFile string, dryRun bool) (v1.HealthFixAction, v1.HealthFixAction, error) {
	syncResult, err := s.syncCanonicalRulesets(ctx, projectID, projectRoot, rulesFile, tagsFile, !dryRun)
	if err != nil {
		return v1.HealthFixAction{}, v1.HealthFixAction{}, err
	}

	planned := v1.HealthFixAction{
		Fixer: v1.HealthFixerSyncRuleset,
		Count: syncResult.TotalRules,
		Notes: healthFixRulesetNotes(syncResult),
	}
	if dryRun {
		return planned, v1.HealthFixAction{}, nil
	}

	appliedCount := syncResult.TotalUpserted + syncResult.TotalMarkedStale
	applied := v1.HealthFixAction{
		Fixer: v1.HealthFixerSyncRuleset,
		Count: appliedCount,
		Notes: []string{
			fmt.Sprintf("upserted=%d", syncResult.TotalUpserted),
			fmt.Sprintf("removed=%d", syncResult.TotalMarkedStale),
		},
	}
	return planned, applied, nil
}

func healthFixRulesetNotes(result canonicalRulesetSyncResult) []string {
	notes := make([]string, 0, len(result.Sources)+2)
	for _, source := range result.Sources {
		if source.Exists {
			notes = append(notes, fmt.Sprintf("%s rules=%d", source.SourcePath, source.RuleCount))
			continue
		}
		notes = append(notes, fmt.Sprintf("%s missing", source.SourcePath))
	}
	notes = append(notes, fmt.Sprintf("total_rules=%d", result.TotalRules))
	return normalizeValues(notes)
}

func effectiveHealthFixApply(apply *bool) bool {
	if apply == nil {
		return false
	}
	return *apply
}

func effectiveHealthFixers(raw []v1.HealthFixer) []v1.HealthFixer {
	if len(raw) == 0 {
		return append([]v1.HealthFixer(nil), defaultHealthFixers...)
	}
	seen := make(map[v1.HealthFixer]struct{}, len(raw))
	fixers := make([]v1.HealthFixer, 0, len(raw))
	for _, fixer := range raw {
		expansion := []v1.HealthFixer{fixer}
		if fixer == v1.HealthFixerAll {
			expansion = defaultHealthFixers
		}
		for _, expanded := range expansion {
			if _, exists := seen[expanded]; exists {
				continue
			}
			seen[expanded] = struct{}{}
			fixers = append(fixers, expanded)
		}
	}
	return fixers
}

func healthFixSummary(dryRun bool, fixerCount, totalPlanned, totalApplied int) string {
	if dryRun {
		return fmt.Sprintf("dry-run complete: %d fixer(s), %d planned action(s)", fixerCount, totalPlanned)
	}
	return fmt.Sprintf("apply complete: %d fixer(s), %d planned action(s), %d applied action(s)", fixerCount, totalPlanned, totalApplied)
}

func healthFixInternalError(operation string, err error) *core.APIError {
	return backendError(v1.ErrCodeInternalError, "failed to apply health fixes", map[string]any{
		"operation": operation,
		"error":     err.Error(),
	})
}
