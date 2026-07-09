package backend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	bootstrapkit "github.com/bonztm/agent-workflow-manager/internal/bootstrap"
	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	"github.com/bonztm/agent-workflow-manager/internal/workspace"
)

var statusTemplateIDs = []string{
	"claude-command-pack",
	"claude-hooks",
	"codex-pack",
	"codex-hooks",
	"opencode-pack",
	"detailed-planning-enforcement",
	"git-hooks-precommit",
	"starter-contract",
	"verify-generic",
	"verify-go",
	"verify-python",
	"verify-rust",
	"verify-ts",
}

// Status reports project readiness: which configuration sources (rules, tags,
// tests, workflows) exist and load cleanly, what is missing, how the storage
// backend was resolved, and — given a task — which tests and gates would run.
func (s *Service) Status(ctx context.Context, payload v1.StatusPayload) (v1.StatusResult, *core.APIError) {
	if s == nil || s.repo == nil {
		return v1.StatusResult{}, backendError(v1.ErrCodeInternalError, "service repository is not configured", nil)
	}

	projectRoot := s.effectiveProjectRoot(payload.ProjectRoot)
	detectedRoot := workspace.DetectRoot(projectRoot)

	result := v1.StatusResult{
		Project: v1.StatusProject{
			ProjectID:              strings.TrimSpace(payload.ProjectID),
			ProjectRoot:            projectRoot,
			DetectedRepoRoot:       strings.TrimSpace(detectedRoot.Path),
			Backend:                firstNonEmpty(strings.TrimSpace(s.runtimeStatus.Backend), "unknown"),
			PostgresConfigured:     s.runtimeStatus.PostgresConfigured,
			SQLitePath:             strings.TrimSpace(s.runtimeStatus.SQLitePath),
			UsesImplicitSQLitePath: s.runtimeStatus.UsesImplicitSQLitePath,
			Unbounded:              effectiveUnbounded(nil),
		},
	}

	if !detectedRoot.IsRepo {
		result.Missing = append(result.Missing, v1.StatusMissingItem{
			Code:    "project_root_not_repo",
			Message: "project root is not inside a git repository",
		})
	}

	ruleSources, ruleMissing := s.statusRules(projectRoot, payload.RulesFile, payload.TagsFile, payload.ProjectID)
	tagSource, tagMissing := s.statusTags(projectRoot, payload.TagsFile)
	testSource, testMissing := s.statusTests(projectRoot, payload.TestsFile, payload.TagsFile)
	workflowSource, workflowMissing := s.statusWorkflows(projectRoot, payload.WorkflowsFile, payload.TagsFile)

	result.Sources = append(result.Sources, ruleSources...)
	result.Sources = append(result.Sources, tagSource)
	result.Sources = append(result.Sources, testSource)
	result.Sources = append(result.Sources, workflowSource)
	result.Missing = append(result.Missing, ruleMissing...)
	result.Missing = append(result.Missing, tagMissing...)
	result.Missing = append(result.Missing, testMissing...)
	result.Missing = append(result.Missing, workflowMissing...)

	sort.Slice(result.Sources, func(i, j int) bool {
		if result.Sources[i].Kind != result.Sources[j].Kind {
			return result.Sources[i].Kind < result.Sources[j].Kind
		}
		return result.Sources[i].SourcePath < result.Sources[j].SourcePath
	})

	integrations, err := statusIntegrations(projectRoot)
	if err != nil {
		result.Missing = append(result.Missing, v1.StatusMissingItem{
			Code:    "template_catalog_unavailable",
			Message: fmt.Sprintf("init template catalog is unavailable: %v", err),
		})
	} else {
		result.Integrations = integrations
	}

	if strings.TrimSpace(payload.TaskText) != "" {
		contextPreview := s.statusContextPreview(ctx, payload, projectRoot)
		result.Context = &contextPreview
		if contextPreview.Error != "" {
			result.Missing = append(result.Missing, v1.StatusMissingItem{
				Code:    "context_preview_error",
				Message: contextPreview.Error,
			})
		}
	}

	warnings, apiErr := s.statusPlanWarnings(ctx, strings.TrimSpace(payload.ProjectID))
	if apiErr != nil {
		return v1.StatusResult{}, apiErr
	}
	result.Warnings = warnings

	result.Missing = dedupeStatusMissing(result.Missing)
	result.Summary = v1.StatusSummary{
		Ready:        len(result.Missing) == 0,
		MissingCount: len(result.Missing),
		WarningCount: len(result.Warnings),
	}
	return result, nil
}

func (s *Service) statusRules(projectRoot, rulesFile, tagsFile, projectID string) ([]v1.StatusSource, []v1.StatusMissingItem) {
	sources, err := discoverCanonicalRulesetSources(projectRoot, rulesFile)
	if err != nil {
		return []v1.StatusSource{{
				Kind:   "rules",
				Loaded: false,
				Notes:  []string{err.Error()},
			}},
			[]v1.StatusMissingItem{{
				Code:    "rules_source_invalid",
				Message: err.Error(),
			}}
	}

	tagNormalizer, tagErr := s.loadCanonicalTagNormalizer(projectRoot, tagsFile)
	allItems := make([]v1.StatusSource, 0, len(sources))
	missing := make([]v1.StatusMissingItem, 0)
	totalRules := 0
	for _, source := range sources {
		item := v1.StatusSource{
			Kind:         "rules",
			SourcePath:   source.SourcePath,
			AbsolutePath: source.AbsolutePath,
			Exists:       source.Exists,
		}
		switch {
		case !source.Exists:
		case tagErr != nil:
			item.Notes = []string{fmt.Sprintf("load canonical tags: %v", tagErr)}
		default:
			rules, parseErr := parseCanonicalRulesetFile(source, strings.TrimSpace(projectID), tagNormalizer)
			if parseErr != nil {
				item.Notes = []string{parseErr.Error()}
			} else {
				item.Loaded = true
				item.ItemCount = len(rules)
				totalRules += len(rules)
			}
		}
		allItems = append(allItems, item)
	}

	// Collapse alternate-path sources (e.g. .awm/awm-rules.yaml and
	// awm-rules.yaml) into one status line. Keep the best representative:
	// prefer loaded > exists > first discovered.
	items := collapseAlternateSourceItems(allItems)

	if totalRules == 0 {
		switch {
		case len(sources) == 0:
			missing = append(missing, v1.StatusMissingItem{Code: "rules_missing", Message: "no canonical rules files were discovered"})
		case anyStatusSourceExists(items):
			missing = append(missing, v1.StatusMissingItem{Code: "rules_unloaded", Message: "canonical rules files exist but could not be loaded"})
		default:
			paths := make([]string, 0, len(items))
			for _, item := range items {
				if item.SourcePath != "" {
					paths = append(paths, item.SourcePath)
				}
			}
			missing = append(missing, v1.StatusMissingItem{
				Code:    "rules_missing",
				Message: "no canonical rules files were found at " + strings.Join(paths, " or "),
			})
		}
	}

	return items, missing
}

func (s *Service) statusTags(projectRoot, tagsFile string) (v1.StatusSource, []v1.StatusMissingItem) {
	source, err := discoverCanonicalTagsSource(projectRoot, tagsFile)
	if err != nil {
		return v1.StatusSource{
				Kind:   "tags",
				Loaded: false,
				Notes:  []string{err.Error()},
			}, []v1.StatusMissingItem{{
				Code:    "tags_source_invalid",
				Message: err.Error(),
			}}
	}

	item := v1.StatusSource{
		Kind:         "tags",
		SourcePath:   source.SourcePath,
		AbsolutePath: source.AbsolutePath,
		Exists:       source.Exists,
	}
	if !source.Exists {
		return item, []v1.StatusMissingItem{{
			Code:    "tags_missing",
			Message: "repo-local canonical tags file is missing at " + source.SourcePath,
		}}
	}

	document, err := parseCanonicalTagsFile(source)
	if err != nil {
		item.Notes = []string{err.Error()}
		return item, []v1.StatusMissingItem{{
			Code:    "tags_invalid",
			Message: err.Error(),
		}}
	}
	item.Loaded = true
	item.ItemCount = len(document.CanonicalTags)
	return item, nil
}

func (s *Service) statusTests(projectRoot, testsFile, tagsFile string) (v1.StatusSource, []v1.StatusMissingItem) {
	source, err := discoverVerifyTestsSource(projectRoot, testsFile)
	if err != nil {
		return v1.StatusSource{
				Kind:   "tests",
				Loaded: false,
				Notes:  []string{err.Error()},
			}, []v1.StatusMissingItem{{
				Code:    "tests_source_invalid",
				Message: err.Error(),
			}}
	}

	item := v1.StatusSource{
		Kind:         "tests",
		SourcePath:   source.SourcePath,
		AbsolutePath: source.AbsolutePath,
		Exists:       source.Exists,
	}
	if !source.Exists {
		return item, []v1.StatusMissingItem{{
			Code:    "tests_missing",
			Message: "verification definitions file is missing at " + source.SourcePath,
		}}
	}

	definitions, loadedSource, err := s.loadVerifyDefinitions(projectRoot, testsFile, tagsFile)
	if loadedSource.SourcePath != "" {
		item.SourcePath = loadedSource.SourcePath
		item.AbsolutePath = loadedSource.AbsolutePath
		item.Exists = loadedSource.Exists
	}
	if err != nil {
		item.Notes = []string{err.Error()}
		return item, []v1.StatusMissingItem{{
			Code:    "tests_invalid",
			Message: err.Error(),
		}}
	}
	item.Loaded = true
	item.ItemCount = len(definitions)
	if len(definitions) == 0 {
		return item, []v1.StatusMissingItem{{
			Code:    "tests_empty",
			Message: "verification definitions loaded but no tests are configured",
		}}
	}
	return item, nil
}

func (s *Service) statusWorkflows(projectRoot, workflowsFile, tagsFile string) (v1.StatusSource, []v1.StatusMissingItem) {
	source, err := discoverWorkflowDefinitionsSource(projectRoot, workflowsFile)
	if err != nil {
		return v1.StatusSource{
				Kind:   "workflows",
				Loaded: false,
				Notes:  []string{err.Error()},
			}, []v1.StatusMissingItem{{
				Code:    "workflows_source_invalid",
				Message: err.Error(),
			}}
	}

	item := v1.StatusSource{
		Kind:         "workflows",
		SourcePath:   source.SourcePath,
		AbsolutePath: source.AbsolutePath,
		Exists:       source.Exists,
	}
	if !source.Exists {
		return item, []v1.StatusMissingItem{{
			Code:    "workflows_missing",
			Message: "workflow definitions file is missing at " + source.SourcePath,
		}}
	}

	definitions, loadedSource, err := s.loadWorkflowCompletionRequirements(projectRoot, workflowsFile, tagsFile)
	if loadedSource.SourcePath != "" {
		item.SourcePath = loadedSource.SourcePath
		item.AbsolutePath = loadedSource.AbsolutePath
		item.Exists = loadedSource.Exists
	}
	if err != nil {
		item.Notes = []string{err.Error()}
		return item, []v1.StatusMissingItem{{
			Code:    "workflows_invalid",
			Message: err.Error(),
		}}
	}
	item.Loaded = true
	item.ItemCount = len(definitions)
	return item, nil
}

func (s *Service) statusContextPreview(ctx context.Context, payload v1.StatusPayload, projectRoot string) v1.StatusContextPreview {
	taskText := strings.TrimSpace(payload.TaskText)
	phase := payload.Phase
	if phase == "" {
		phase = v1.PhaseExecute
	}
	result := v1.StatusContextPreview{
		TaskText: taskText,
		Phase:    phase,
		Status:   "unavailable",
	}

	tagNormalizer, err := s.loadCanonicalTagNormalizer(projectRoot, payload.TagsFile)
	if err != nil {
		result.Error = fmt.Sprintf("load canonical tags: %v", err)
		return result
	}

	taskTags := tagNormalizer.canonicalTagsFromTaskText(taskText)
	selectedRules, _, ruleTags, err := loadCanonicalContextRules(projectRoot, payload.ProjectID, tagNormalizer)
	if err != nil {
		result.Error = fmt.Sprintf("load canonical rules: %v", err)
		return result
	}

	rules := makeContextRules(selectedRules)
	resolvedTags := resolveTags(append(append([]string(nil), taskTags...), ruleTags...), tagNormalizer)
	receiptID := deterministicReceiptID(v1.ContextPayload{
		ProjectID: payload.ProjectID,
		TaskText:  taskText,
		Phase:     phase,
	}, resolvedTags, rules, nil, nil)
	plans := s.makeContextPlans(ctx, payload.ProjectID, receiptID)

	result.Status = "ok"
	result.ResolvedTags = resolvedTags
	result.RuleCount = len(rules)
	result.PlanCount = len(plans)
	return result
}

func statusIntegrations(projectRoot string) ([]v1.StatusIntegration, error) {
	templates, err := bootstrapkit.ResolveTemplates(statusTemplateIDs)
	if err != nil {
		return nil, err
	}

	// First pass: collect grouped targets per template and build a cross-
	// template target frequency map so we know which targets are unique.
	type templateTargetInfo struct {
		template bootstrapkit.Template
		groups   [][]string
	}
	infos := make([]templateTargetInfo, 0, len(templates))
	canonFreq := make(map[string]int) // canonical target key -> number of templates claiming it

	for _, template := range templates {
		targets := make([]string, 0, len(template.Operations))
		for _, op := range template.Operations {
			target := strings.TrimSpace(op.Target)
			if target != "" {
				targets = append(targets, target)
			}
		}
		groups := groupAlternateTargets(targets)
		infos = append(infos, templateTargetInfo{template: template, groups: groups})
		for _, g := range groups {
			key := canonicalTargetKey(g[0])
			canonFreq[key]++
		}
	}

	// Second pass: compute presence, installed, and missing for each template.
	out := make([]v1.StatusIntegration, 0, len(infos))
	for _, info := range infos {
		presentGroups := 0
		hasUniquePresent := false
		missingTargets := make([]string, 0)
		for _, g := range info.groups {
			found := false
			for _, target := range g {
				if _, err := os.Stat(filepath.Join(projectRoot, filepath.FromSlash(target))); err == nil {
					found = true
					break
				}
			}
			if found {
				presentGroups++
				// A target is unique to this template if no other template claims it.
				if canonFreq[canonicalTargetKey(g[0])] == 1 {
					hasUniquePresent = true
				}
			} else {
				missingTargets = append(missingTargets, g[0])
			}
		}

		// A template is installed only if all grouped targets are present
		// AND it has at least one target unique to it. Templates whose
		// targets are all shared with other templates (e.g. verify-*)
		// cannot be reliably detected as installed by file presence alone.
		allPresent := len(info.groups) > 0 && presentGroups == len(info.groups)
		installed := allPresent && hasUniquePresent

		out = append(out, v1.StatusIntegration{
			ID:              info.template.ID,
			Summary:         strings.TrimSpace(info.template.Summary),
			Installed:       installed,
			PresentTargets:  presentGroups,
			ExpectedTargets: len(info.groups),
			MissingTargets:  missingTargets,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// groupAlternateTargets groups template operation targets so that paths
// differing only by a ".awm/" prefix are treated as alternate locations for
// the same logical file. For example, ".awm/awm-rules.yaml" and
// "awm-rules.yaml" become one group. Targets without an alternate are their
// own group. Order within each group puts the .awm/ variant first (preferred).
func groupAlternateTargets(targets []string) [][]string {
	grouped := make(map[string][]string) // canonical -> [paths]
	order := make([]string, 0, len(targets))

	for _, target := range targets {
		canon := canonicalTargetKey(target)
		if _, exists := grouped[canon]; !exists {
			order = append(order, canon)
		}
		grouped[canon] = append(grouped[canon], target)
	}

	result := make([][]string, 0, len(order))
	for _, canon := range order {
		paths := grouped[canon]
		// Sort so .awm/ prefixed paths come first (preferred location).
		sort.Slice(paths, func(i, j int) bool {
			iDotAWM := strings.HasPrefix(paths[i], ".awm/")
			jDotAWM := strings.HasPrefix(paths[j], ".awm/")
			if iDotAWM != jDotAWM {
				return iDotAWM
			}
			return paths[i] < paths[j]
		})
		result = append(result, paths)
	}
	return result
}

// canonicalTargetKey returns a normalized key for grouping alternate target
// paths. It strips a leading ".awm/" prefix so that "awm-rules.yaml" and
// ".awm/awm-rules.yaml" produce the same key.
func canonicalTargetKey(target string) string {
	return strings.TrimPrefix(target, ".awm/")
}

// collapseAlternateSourceItems groups status source items whose paths differ
// only by a ".awm/" prefix (alternate locations for the same logical file)
// and keeps the best representative per group: loaded > exists > first.
func collapseAlternateSourceItems(items []v1.StatusSource) []v1.StatusSource {
	type group struct {
		best  v1.StatusSource
		score int // 2=loaded, 1=exists, 0=neither
	}
	groups := make(map[string]*group)
	order := make([]string, 0, len(items))

	for _, item := range items {
		key := canonicalTargetKey(item.SourcePath)
		score := 0
		if item.Loaded {
			score = 2
		} else if item.Exists {
			score = 1
		}
		if g, ok := groups[key]; ok {
			if score > g.score {
				g.best = item
				g.score = score
			}
		} else {
			groups[key] = &group{best: item, score: score}
			order = append(order, key)
		}
	}

	out := make([]v1.StatusSource, 0, len(order))
	for _, key := range order {
		out = append(out, groups[key].best)
	}
	return out
}

func anyStatusSourceExists(items []v1.StatusSource) bool {
	for _, item := range items {
		if item.Exists {
			return true
		}
	}
	return false
}

func dedupeStatusMissing(items []v1.StatusMissingItem) []v1.StatusMissingItem {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]v1.StatusMissingItem, 0, len(items))
	for _, item := range items {
		key := strings.TrimSpace(item.Code) + "\x00" + strings.TrimSpace(item.Message)
		if key == "\x00" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func (s *Service) statusPlanWarnings(ctx context.Context, projectID string) ([]v1.StatusMissingItem, *core.APIError) {
	diagnostics, apiErr := s.collectPlanDiagnostics(ctx, projectID, time.Now().UTC())
	if apiErr != nil {
		return nil, apiErr
	}

	warnings := make([]v1.StatusMissingItem, 0, len(diagnostics.stale)+len(diagnostics.terminalStatusDrift)+len(diagnostics.administrativeCloseout))
	for _, finding := range diagnostics.stale {
		warnings = append(warnings, v1.StatusMissingItem{
			Code:    "stale_work_plan",
			Message: finding,
		})
	}
	for _, finding := range diagnostics.terminalStatusDrift {
		warnings = append(warnings, v1.StatusMissingItem{
			Code:    "terminal_plan_status_drift",
			Message: finding,
		})
	}
	for _, finding := range diagnostics.administrativeCloseout {
		warnings = append(warnings, v1.StatusMissingItem{
			Code:    "administrative_closeout_plan",
			Message: finding,
		})
	}
	return dedupeStatusMissing(warnings), nil
}
