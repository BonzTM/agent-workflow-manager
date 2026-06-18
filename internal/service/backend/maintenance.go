package backend

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	bootstrapkit "github.com/bonztm/agent-workflow-manager/internal/bootstrap"
	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	"github.com/bonztm/agent-workflow-manager/internal/workspace"
)

func (s *Service) Health(ctx context.Context, payload v1.HealthPayload) (v1.HealthResult, *core.APIError) {
	isFixMode := len(payload.Fixers) > 0 || payload.Apply != nil || strings.TrimSpace(payload.ProjectRoot) != "" || strings.TrimSpace(payload.RulesFile) != "" || strings.TrimSpace(payload.TagsFile) != ""
	if isFixMode {
		result, apiErr := s.healthFix(ctx, payload)
		if apiErr != nil {
			return v1.HealthResult{}, apiErr
		}
		return v1.HealthResult{Mode: "fix", Fix: &result}, nil
	}

	result, apiErr := s.healthCheck(ctx, payload)
	if apiErr != nil {
		return v1.HealthResult{}, apiErr
	}
	return v1.HealthResult{Mode: "check", Check: &result}, nil
}

func (s *Service) healthCheck(ctx context.Context, payload v1.HealthPayload) (v1.HealthCheckResult, *core.APIError) {
	if s == nil || s.repo == nil {
		return v1.HealthCheckResult{}, backendError(v1.ErrCodeInternalError, "service repository is not configured", nil)
	}

	candidates, err := s.repo.FetchCandidatePointers(ctx, core.CandidatePointerQuery{
		ProjectID: strings.TrimSpace(payload.ProjectID),
		Unbounded: true,
		StaleFilter: core.StaleFilter{
			AllowStale: true,
		},
	})
	if err != nil {
		return v1.HealthCheckResult{}, healthCheckInternalError("fetch_candidate_pointers", err)
	}

	includeDetails := effectiveHealthIncludeDetails(payload.IncludeDetails)
	maxFindings := effectiveMaxFindingsPerCheck(payload.MaxFindingsPerCheck)
	inventory, apiErr := s.computeInventoryHealth(ctx, strings.TrimSpace(payload.ProjectID), "")
	if apiErr != nil {
		return v1.HealthCheckResult{}, healthCheckInternalError("inventory_health", fmt.Errorf("%s", apiErr.Message))
	}
	planDiagnostics, apiErr := s.collectPlanDiagnostics(ctx, strings.TrimSpace(payload.ProjectID), time.Now().UTC())
	if apiErr != nil {
		return v1.HealthCheckResult{}, apiErr
	}
	checks := buildHealthChecks(candidates, inventory.StalePaths, inventory.UnindexedPaths, planDiagnostics, includeDetails, maxFindings)

	totalFindings := 0
	for _, check := range checks {
		totalFindings += check.Count
	}

	return v1.HealthCheckResult{
		Summary: v1.HealthSummary{
			OK:            totalFindings == 0,
			TotalFindings: totalFindings,
		},
		Checks: checks,
	}, nil
}

func (s *Service) Init(ctx context.Context, payload v1.InitPayload) (v1.InitResult, *core.APIError) {
	if s == nil || s.repo == nil {
		return v1.InitResult{}, backendError(v1.ErrCodeInternalError, "service repository is not configured", nil)
	}

	projectRoot := s.effectiveProjectRoot(payload.ProjectRoot)
	outputPath, persistCandidates := bootstrapkit.ResolveOutputPath(projectRoot, derefString(payload.OutputCandidatesPath), effectivePersistCandidates(payload.PersistCandidates))
	excludedPaths := initManagedRelativePaths(projectRoot, outputPath, payload.RulesFile, payload.TagsFile)
	templates, err := bootstrapkit.ResolveTemplates(payload.ApplyTemplates)
	if err != nil {
		var unknown bootstrapkit.UnknownTemplateError
		if errors.As(err, &unknown) {
			return v1.InitResult{}, backendError(v1.ErrCodeInvalidInput, "unknown init template", map[string]any{"template_id": unknown.TemplateID})
		}
		return v1.InitResult{}, initInternalError("load_templates", err)
	}

	paths, warnings, err := s.collectInitCandidatePaths(ctx, projectRoot, outputPath, payload.RulesFile, payload.TagsFile, effectiveRespectGitIgnore(payload.RespectGitIgnore))
	if err != nil {
		return v1.InitResult{}, initInternalError("collect_project_paths", err)
	}

	if err := bootstrapkit.EnsureProjectScaffold(projectRoot, payload.RulesFile); err != nil {
		return v1.InitResult{}, initInternalError("seed_scaffold", err)
	}
	if err := syncInitCanonicalTagsFile(projectRoot, payload.TagsFile, paths); err != nil {
		return v1.InitResult{}, initInternalError("seed_tags", err)
	}

	templateResults := []v1.InitTemplateResult(nil)
	if len(templates) > 0 {
		appliedTemplates, err := bootstrapkit.ApplyTemplates(projectRoot, payload.ProjectID, templates)
		if err != nil {
			return v1.InitResult{}, initInternalError("apply_templates", err)
		}
		templateResults = appliedTemplates.TemplateResults
		paths = mergeInitTemplateCandidatePaths(paths, appliedTemplates.CandidatePaths, excludedPaths)
	}

	rulesetSync, err := s.syncCanonicalRulesets(ctx, strings.TrimSpace(payload.ProjectID), projectRoot, payload.RulesFile, payload.TagsFile, true)
	if err != nil {
		return v1.InitResult{}, initInternalError("parse_ruleset", err)
	}
	warnings = append(warnings, canonicalRulesetWarnings(rulesetSync)...)

	indexedStubs, err := s.upsertAutoIndexedPaths(ctx, strings.TrimSpace(payload.ProjectID), projectRoot, payload.TagsFile, paths)
	if err != nil {
		return v1.InitResult{}, initInternalError("upsert_pointer_stubs", err)
	}

	if persistCandidates {
		if err := bootstrapkit.WriteCandidates(outputPath, paths); err != nil {
			return v1.InitResult{}, initInternalError("write_candidates", err)
		}
	}

	warnings = normalizeValues(warnings)

	result := v1.InitResult{
		CandidateCount:      len(paths),
		IndexedStubs:        indexedStubs,
		CandidatesPersisted: persistCandidates,
		TemplateResults:     templateResults,
	}
	if persistCandidates {
		result.OutputCandidatesPath = outputPath
	}
	if len(warnings) > 0 {
		result.Warnings = warnings
	}
	return result, nil
}

func effectiveHealthIncludeDetails(includeDetails *bool) bool {
	if includeDetails == nil {
		return defaultHealthDetails
	}
	return *includeDetails
}

func effectiveMaxFindingsPerCheck(maxFindings *int) int {
	if maxFindings == nil || *maxFindings < 1 {
		return defaultHealthFindings
	}
	return *maxFindings
}

func buildHealthChecks(candidates []core.CandidatePointer, stalePaths, unindexedPaths []string, plans planDiagnostics, includeDetails bool, maxFindings int) []v1.HealthCheckItem {
	checks := []v1.HealthCheckItem{
		healthCheckItem("administrative_closeout_plans", "warn", plans.administrativeCloseout, includeDetails, maxFindings),
		healthCheckItem("duplicate_labels", "warn", duplicateLabelFindings(candidates), includeDetails, maxFindings),
		healthCheckItem("empty_descriptions", "warn", emptyDescriptionFindings(candidates), includeDetails, maxFindings),
		healthCheckItem("orphan_relations", "info", []string{}, includeDetails, maxFindings),
		healthCheckItem("pending_quarantines", "info", []string{}, includeDetails, maxFindings),
		healthCheckItem("stale_work_plans", "warn", plans.stale, includeDetails, maxFindings),
		healthCheckItem("stale_pointers", "warn", stalePaths, includeDetails, maxFindings),
		healthCheckItem("terminal_plan_status_drift", "warn", plans.terminalStatusDrift, includeDetails, maxFindings),
		healthCheckItem("unindexed_files", "warn", normalizeValues(unindexedPaths), includeDetails, maxFindings),
		healthCheckItem("unknown_tags", "warn", unknownTagFindings(candidates), includeDetails, maxFindings),
	}

	sort.Slice(checks, func(i, j int) bool {
		return checks[i].Name < checks[j].Name
	})
	return checks
}

func healthCheckItem(name, severity string, findings []string, includeDetails bool, maxFindings int) v1.HealthCheckItem {
	normalizedFindings := normalizeValues(findings)
	item := v1.HealthCheckItem{
		Name:     name,
		Severity: severity,
		Count:    len(normalizedFindings),
	}
	if includeDetails && len(normalizedFindings) > 0 {
		limit := min(len(normalizedFindings), maxFindings)
		item.Samples = append([]string(nil), normalizedFindings[:limit]...)
	}
	return item
}

func emptyDescriptionFindings(candidates []core.CandidatePointer) []string {
	out := make([]string, 0)
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.Description) != "" {
			continue
		}
		key := strings.TrimSpace(candidate.Key)
		if key == "" {
			key = normalizeCompletionPath(candidate.Path)
		}
		if key == "" {
			continue
		}
		out = append(out, key)
	}
	return out
}

func duplicateLabelFindings(candidates []core.CandidatePointer) []string {
	byLabel := make(map[string][]string)
	for _, candidate := range candidates {
		label := strings.TrimSpace(candidate.Label)
		if label == "" {
			continue
		}
		key := strings.TrimSpace(candidate.Key)
		if key == "" {
			key = normalizeCompletionPath(candidate.Path)
		}
		if key == "" {
			continue
		}
		byLabel[label] = append(byLabel[label], key)
	}

	labels := make([]string, 0, len(byLabel))
	for label := range byLabel {
		labels = append(labels, label)
	}
	sort.Strings(labels)

	out := make([]string, 0)
	for _, label := range labels {
		keys := normalizeValues(byLabel[label])
		if len(keys) < 2 {
			continue
		}
		for _, key := range keys {
			out = append(out, fmt.Sprintf("%s:%s", label, key))
		}
	}
	return out
}

func unknownTagFindings(candidates []core.CandidatePointer) []string {
	out := make([]string, 0)
	for _, candidate := range candidates {
		key := strings.TrimSpace(candidate.Key)
		if key == "" {
			key = normalizeCompletionPath(candidate.Path)
		}
		if key == "" {
			key = "pointer"
		}
		for _, tag := range candidate.Tags {
			tag = strings.TrimSpace(tag)
			if tag == "" || healthTagPattern.MatchString(tag) {
				continue
			}
			out = append(out, fmt.Sprintf("pointer:%s:%s", key, tag))
		}
	}
	return out
}

func effectiveRespectGitIgnore(respectGitIgnore *bool) bool {
	if respectGitIgnore == nil {
		return defaultInitRespectGit
	}
	return *respectGitIgnore
}

func effectivePersistCandidates(persistCandidates *bool) bool {
	if persistCandidates == nil {
		return defaultInitPersist
	}
	return *persistCandidates
}

func (s *Service) collectInitCandidatePaths(ctx context.Context, projectRoot, outputPath, rulesFile, tagsFile string, respectGitIgnore bool) ([]string, []string, error) {
	excludedPaths := initManagedRelativePaths(projectRoot, outputPath, rulesFile, tagsFile)
	warnings := make([]string, 0)

	if respectGitIgnore {
		gitOutput, err := s.runGit(ctx, projectRoot, "ls-files", "--cached", "--others", "--exclude-standard")
		if err == nil {
			return filterInitCandidatePaths(parseInitCandidateGitPaths(gitOutput), excludedPaths), warnings, nil
		}
		warnings = append(warnings, "respect_gitignore fallback to filesystem walk")
	}

	paths, walkWarnings, err := collectInitCandidatePathsFromWalk(ctx, projectRoot)
	if err != nil {
		return nil, nil, err
	}
	warnings = append(warnings, walkWarnings...)
	return filterInitCandidatePaths(paths, excludedPaths), warnings, nil
}

func parseInitCandidateGitPaths(output string) []string {
	lines := strings.Split(output, "\n")
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		normalized := normalizeCompletionPath(line)
		if normalized == "" {
			continue
		}
		if isManagedProjectPath(normalized) {
			continue
		}
		paths = append(paths, normalized)
	}
	return normalizeCompletionPaths(paths)
}

func collectInitCandidatePathsFromWalk(ctx context.Context, projectRoot string) ([]string, []string, error) {
	paths := make([]string, 0)
	warnings := make([]string, 0)

	err := filepath.WalkDir(projectRoot, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if len(warnings) < maxInitWalkErrorSamples {
				warnings = append(warnings, fmt.Sprintf("skip:%s", normalizeWalkWarningPath(projectRoot, current)))
			}
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".awm":
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}

		relative, relErr := filepath.Rel(projectRoot, current)
		if relErr != nil {
			if len(warnings) < maxInitWalkErrorSamples {
				warnings = append(warnings, fmt.Sprintf("skip:%s", normalizeWalkWarningPath(projectRoot, current)))
			}
			return nil
		}
		normalized := normalizeCompletionPath(relative)
		if normalized == "" {
			return nil
		}
		if isManagedProjectPath(normalized) {
			return nil
		}
		paths = append(paths, normalized)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return normalizeCompletionPaths(paths), normalizeValues(warnings), nil
}

func normalizeWalkWarningPath(projectRoot, candidatePath string) string {
	relative, err := filepath.Rel(projectRoot, candidatePath)
	if err == nil {
		if normalized := normalizeCompletionPath(relative); normalized != "" {
			return normalized
		}
	}
	cleaned := normalizeCompletionPath(candidatePath)
	if cleaned != "" {
		return cleaned
	}
	return strings.TrimSpace(candidatePath)
}

func initOutputRelativePath(projectRoot, outputPath string) string {
	relative, err := filepath.Rel(projectRoot, outputPath)
	if err != nil {
		return ""
	}
	normalized := normalizeCompletionPath(relative)
	if normalized == "" || normalized == "." || strings.HasPrefix(normalized, "../") {
		return ""
	}
	return normalized
}

func initManagedRelativePaths(projectRoot, outputPath, rulesFile, tagsFile string) []string {
	managed := make([]string, 0, len(canonicalRulesetDefaultPaths)+9)
	if relativeOutputPath := initOutputRelativePath(projectRoot, outputPath); relativeOutputPath != "" {
		managed = append(managed, relativeOutputPath)
	}
	managed = append(managed, ".gitignore", workspace.DotEnvExampleFileName)
	managed = append(managed, canonicalTagsDefaultFilePath)
	managed = append(managed, canonicalRulesetDefaultPaths...)
	managed = append(managed, workflowDefinitionsPrimarySourcePath, workflowDefinitionsSecondarySourcePath)
	managed = append(managed, verifyTestsPrimarySourcePath, verifyTestsSecondarySourcePath)
	if relativeRulesPath := initManagedRelativePath(projectRoot, rulesFile); relativeRulesPath != "" {
		managed = append(managed, relativeRulesPath)
	}
	if relativeTagsPath := initManagedRelativePath(projectRoot, tagsFile); relativeTagsPath != "" {
		managed = append(managed, relativeTagsPath)
	}
	return normalizeCompletionPaths(managed)
}

func initManagedRelativePath(projectRoot, rawPath string) string {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return ""
	}
	if filepath.IsAbs(trimmed) {
		return initOutputRelativePath(projectRoot, trimmed)
	}
	return normalizeCompletionPath(trimmed)
}

func filterInitCandidatePaths(paths []string, excludedPaths []string) []string {
	excluded := map[string]struct{}{}
	for _, excludedPath := range excludedPaths {
		if trimmedExcludedPath := strings.TrimSpace(excludedPath); trimmedExcludedPath != "" {
			excluded[trimmedExcludedPath] = struct{}{}
		}
	}

	filtered := make([]string, 0, len(paths))
	for _, candidatePath := range paths {
		if isManagedProjectPath(candidatePath) {
			continue
		}
		if _, skip := excluded[candidatePath]; skip {
			continue
		}
		filtered = append(filtered, candidatePath)
	}
	return filtered
}

func mergeInitTemplateCandidatePaths(existing []string, additional []string, excluded []string) []string {
	merged := append([]string(nil), existing...)
	merged = append(merged, additional...)
	return filterInitCandidatePaths(normalizeCompletionPaths(merged), excluded)
}

func isManagedProjectPath(raw string) bool {
	normalized := normalizeCompletionPath(raw)
	if normalized == "" {
		return false
	}

	switch normalized {
	case ".gitignore",
		workspace.DotEnvFileName,
		workspace.DotEnvExampleFileName,
		canonicalRulesetSecondarySourcePath,
		workflowDefinitionsSecondarySourcePath,
		verifyTestsSecondarySourcePath:
		return true
	}

	return normalized == ".awm" || strings.HasPrefix(normalized, ".awm/")
}

func healthCheckInternalError(operation string, err error) *core.APIError {
	return backendError(v1.ErrCodeInternalError, "failed to run health check", map[string]any{
		"operation": operation,
		"error":     err.Error(),
	})
}

func initInternalError(operation string, err error) *core.APIError {
	return backendError(v1.ErrCodeInternalError, "failed to initialize project state", map[string]any{
		"operation": operation,
		"error":     err.Error(),
	})
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
