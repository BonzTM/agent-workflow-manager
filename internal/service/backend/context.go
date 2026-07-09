package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

const (
	contextVersion = "backend.context.v1"
)

// Context builds a scoped work receipt for a task: it derives canonical tags
// from the task text, selects the matching rules and pointers, captures a
// working-tree baseline, and persists the receipt scope for later completion
// checks.
func (s *Service) Context(ctx context.Context, payload v1.ContextPayload) (v1.ContextResult, *core.APIError) {
	if s == nil || s.repo == nil {
		return v1.ContextResult{}, backendError(v1.ErrCodeInternalError, "service repository is not configured", nil)
	}

	projectRoot := s.defaultProjectRoot()
	tagNormalizer, err := s.loadCanonicalTagNormalizer(projectRoot, payload.TagsFile)
	if err != nil {
		return v1.ContextResult{}, internalError("load_canonical_tags", err)
	}

	taskText := strings.TrimSpace(payload.TaskText)
	taskTags := tagNormalizer.canonicalTagsFromTaskText(taskText)
	selectedRules, ruleKeys, ruleTags, err := loadCanonicalContextRules(projectRoot, payload.ProjectID, tagNormalizer)
	if err != nil {
		return v1.ContextResult{}, internalError("load_canonical_rules", err)
	}

	rules := makeContextRules(selectedRules)
	resolvedTags := resolveTags(append(append([]string(nil), taskTags...), ruleTags...), tagNormalizer)
	initialScopePaths := normalizeCompletionPaths(payload.InitialScopePaths)

	baselinePaths, baselineErr := s.captureWorkingTreeBaseline(ctx, projectRoot)
	baselineCaptured := baselineErr == nil
	if baselineErr != nil {
		baselinePaths = nil
	}

	receiptID := deterministicReceiptID(payload, resolvedTags, rules, initialScopePaths, baselinePaths)
	plans := s.makeContextPlans(ctx, payload.ProjectID, receiptID)

	receipt := v1.ContextReceipt{
		Rules:             rules,
		Plans:             plans,
		InitialScopePaths: append([]string(nil), initialScopePaths...),
		Meta: v1.ContextReceiptMeta{
			ReceiptID:        receiptID,
			ProjectID:        payload.ProjectID,
			TaskText:         payload.TaskText,
			Phase:            payload.Phase,
			ResolvedTags:     resolvedTags,
			BaselineCaptured: baselineCaptured,
		},
	}

	if err := s.repo.UpsertReceiptScope(ctx, core.ReceiptScope{
		ProjectID:         strings.TrimSpace(payload.ProjectID),
		ReceiptID:         receiptID,
		TaskText:          strings.TrimSpace(payload.TaskText),
		Phase:             strings.TrimSpace(string(payload.Phase)),
		ResolvedTags:      append([]string(nil), resolvedTags...),
		PointerKeys:       append([]string(nil), ruleKeys...),
		InitialScopePaths: append([]string(nil), initialScopePaths...),
		BaselineCaptured:  baselineCaptured,
		BaselinePaths:     append([]core.SyncPath(nil), baselinePaths...),
		Actor:             actorFromPayload(payload.Actor),
	}); err != nil {
		return v1.ContextResult{}, internalError("persist_receipt_scope", err)
	}

	return v1.ContextResult{
		Status:  "ok",
		Receipt: &receipt,
	}, nil
}

// actorFromPayload converts the optional wire actor into its persisted form;
// a nil actor persists as the zero (unattributed) value.
func actorFromPayload(actor *v1.ActorRef) core.Actor {
	if actor == nil {
		return core.Actor{}
	}
	return core.Actor{
		Harness:   actor.Harness,
		Model:     actor.Model,
		SessionID: actor.SessionID,
	}
}

// actorRefFromCore converts a persisted actor back to its wire form; the zero
// (unattributed) value maps to nil so responses omit the block entirely.
func actorRefFromCore(actor core.Actor) *v1.ActorRef {
	if actor == (core.Actor{}) {
		return nil
	}
	return &v1.ActorRef{
		Harness:   actor.Harness,
		Model:     actor.Model,
		SessionID: actor.SessionID,
	}
}

func loadCanonicalContextRules(projectRoot, projectID string, tagNormalizer canonicalTagNormalizer) ([]core.CandidatePointer, []string, []string, error) {
	sources, err := discoverCanonicalRulesetSources(projectRoot, "")
	if err != nil {
		return nil, nil, nil, err
	}
	selected := make([]core.CandidatePointer, 0)
	ruleKeys := make([]string, 0)
	tagSet := make(map[string]struct{})
	for _, source := range sources {
		if !source.Exists {
			continue
		}
		rules, parseErr := parseCanonicalRulesetFile(source, projectID, tagNormalizer)
		if parseErr != nil {
			return nil, nil, nil, parseErr
		}
		for _, rule := range rules {
			selected = append(selected, core.CandidatePointer{
				Key:         rule.PointerKey,
				Path:        rule.SourcePath,
				Kind:        "rule",
				Label:       rule.Summary,
				Description: rule.Content,
				Tags:        append([]string(nil), rule.Tags...),
				IsRule:      true,
			})
			ruleKeys = append(ruleKeys, rule.PointerKey)
			for _, tag := range rule.Tags {
				tagSet[strings.TrimSpace(tag)] = struct{}{}
			}
		}
	}
	return selected, normalizeValues(ruleKeys), mapKeysSorted(tagSet), nil
}

func makeContextRules(selected []core.CandidatePointer) []v1.ContextRule {
	out := make([]v1.ContextRule, 0, len(selected))
	for _, entry := range selected {
		ruleKey := strings.TrimSpace(entry.Key)
		if ruleKey == "" {
			continue
		}

		summary := strings.TrimSpace(entry.Label)
		if summary == "" {
			summary = pointerSummary(entry)
		}

		rule := v1.ContextRule{
			RuleID:      ruleIDFromPointerKey(ruleKey),
			Key:         ruleKey,
			Summary:     summary,
			Enforcement: ruleEnforcementFromTags(entry.Tags),
		}

		content := strings.TrimSpace(entry.Description)
		if content != "" && content != summary {
			rule.Content = content
		}

		out = append(out, rule)
	}
	return out
}

func ruleIDFromPointerKey(pointerKey string) string {
	key := strings.TrimSpace(pointerKey)
	if key == "" {
		return ""
	}
	separator := strings.LastIndex(key, "#")
	if separator < 0 || separator >= len(key)-1 {
		return key
	}
	ruleID := strings.TrimSpace(key[separator+1:])
	if ruleID == "" {
		return key
	}
	return ruleID
}

func ruleEnforcementFromTags(tags []string) string {
	for _, tag := range normalizeCanonicalTags(tags) {
		switch tag {
		case ruleTagEnforcementSoft:
			return "soft"
		case ruleTagEnforcementHard:
			return "hard"
		}
	}
	return "required"
}

func (s *Service) makeContextPlans(ctx context.Context, projectID, receiptID string) []v1.ContextPlan {
	if s != nil && s.planRepo != nil {
		planRows, err := s.planRepo.ListWorkPlans(ctx, core.WorkPlanListQuery{
			ProjectID: strings.TrimSpace(projectID),
			Scope:     string(v1.HistoryScopeCurrent),
			Limit:     8,
		})
		if err == nil && len(planRows) > 0 {
			plans := make([]v1.ContextPlan, 0, len(planRows))
			for _, row := range planRows {
				planKey := strings.TrimSpace(row.PlanKey)
				if planKey == "" {
					continue
				}
				status := normalizePlanStatus(row.Status)
				summary := strings.TrimSpace(row.Summary)
				if summary == "" {
					summary = fmt.Sprintf("Plan %s is %s", planKey, status)
				}
				plans = append(plans, v1.ContextPlan{
					Key:       planKey,
					Summary:   summary,
					Status:    v1.WorkItemStatus(status),
					FetchKeys: contextPlanFetchKeys(planKey),
				})
			}
			if len(plans) > 0 {
				return plans
			}
		}
	}

	receiptID = strings.TrimSpace(receiptID)
	if receiptID == "" {
		return nil
	}
	status := v1.WorkItemStatusPending
	return []v1.ContextPlan{{
		Key:       "plan:" + receiptID,
		Summary:   fmt.Sprintf("Plan %s is %s", receiptID, status),
		Status:    status,
		FetchKeys: contextPlanFetchKeys("plan:" + receiptID),
	}}
}

func contextPlanFetchKeys(planKey string) []string {
	normalizedPlanKey := strings.TrimSpace(planKey)
	if normalizedPlanKey == "" {
		return nil
	}
	return []string{normalizedPlanKey}
}

func resolveTags(pointerTags []string, tagNormalizer canonicalTagNormalizer) []string {
	resolved := make(map[string]struct{}, len(pointerTags))
	for _, tag := range tagNormalizer.normalizeTags(pointerTags) {
		resolved[tag] = struct{}{}
	}
	return mapKeysSorted(resolved)
}

func deterministicReceiptID(payload v1.ContextPayload, resolvedTags []string, rules []v1.ContextRule, initialScopePaths []string, baselinePaths []core.SyncPath) string {
	var b strings.Builder
	b.WriteString(contextVersion)
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(payload.ProjectID))
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(payload.TaskText))
	b.WriteString("\n")
	b.WriteString(string(payload.Phase))
	b.WriteString("\n")

	for _, tag := range resolvedTags {
		b.WriteString("tag:")
		b.WriteString(tag)
		b.WriteString("\n")
	}
	for _, rule := range rules {
		b.WriteString("rule:")
		b.WriteString(rule.Key)
		b.WriteString("|")
		b.WriteString(rule.Summary)
		b.WriteString("|")
		b.WriteString(rule.Enforcement)
		b.WriteString("|")
		b.WriteString(rule.Content)
		b.WriteString("\n")
	}
	for _, relativePath := range normalizeCompletionPaths(initialScopePaths) {
		b.WriteString("initial_scope:")
		b.WriteString(relativePath)
		b.WriteString("\n")
	}
	for _, entry := range baselinePaths {
		b.WriteString("baseline:")
		b.WriteString(normalizeCompletionPath(entry.Path))
		b.WriteString("|")
		b.WriteString(strings.TrimSpace(entry.ContentHash))
		b.WriteString("|")
		if entry.Deleted {
			b.WriteString("deleted")
		} else {
			b.WriteString("live")
		}
		b.WriteString("\n")
	}

	digest := sha256.Sum256([]byte(b.String()))
	return "receipt-" + hex.EncodeToString(digest[:12])
}

func pointerSummary(pointer core.CandidatePointer) string {
	description := strings.TrimSpace(pointer.Description)
	if description == "" {
		description = strings.TrimSpace(pointer.Label)
	}
	if description == "" {
		description = strings.TrimSpace(pointer.Path)
	}
	if description == "" {
		description = strings.TrimSpace(pointer.Key)
	}
	return description
}

func mapKeysSorted(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func internalError(operation string, err error) *core.APIError {
	return backendError(v1.ErrCodeInternalError, "failed to resolve context", map[string]any{
		"operation": operation,
		"error":     err.Error(),
	})
}
