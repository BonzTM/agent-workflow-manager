package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/bonztm/agent-workflow-manager/internal/core"
)

const (
	canonicalRulesVersionV1             = "awm.rules.v1"
	canonicalRulesetPrimarySourcePath   = ".awm/awm-rules.yaml"
	canonicalRulesetSecondarySourcePath = "awm-rules.yaml"

	ruleTagCanonical       = "canonical-rule"
	ruleTagEnforcementHard = "enforcement-hard"
	ruleTagEnforcementSoft = "enforcement-soft"
)

var (
	canonicalRulesetDefaultPaths = []string{
		canonicalRulesetPrimarySourcePath,
		canonicalRulesetSecondarySourcePath,
	}
	ruleIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
)

type canonicalRulesetSource struct {
	SourcePath   string
	AbsolutePath string
	Exists       bool
}

type canonicalRulesetDocumentV1 struct {
	Version string                   `yaml:"version"`
	Rules   []canonicalRulesetRuleV1 `yaml:"rules"`
}

type canonicalRulesetRuleV1 struct {
	ID          string   `yaml:"id"`
	Summary     string   `yaml:"summary"`
	Title       string   `yaml:"title"`
	Content     string   `yaml:"content"`
	Enforcement string   `yaml:"enforcement"`
	Tags        []string `yaml:"tags"`
}

type canonicalRulePointer struct {
	PointerKey  string
	SourcePath  string
	RuleID      string
	Summary     string
	Content     string
	Enforcement string
	Tags        []string
}

type canonicalRulesetSyncSourceResult struct {
	SourcePath  string
	Exists      bool
	RuleCount   int
	Upserted    int
	MarkedStale int
}

type canonicalRulesetSyncResult struct {
	Sources          []canonicalRulesetSyncSourceResult
	TotalRules       int
	TotalUpserted    int
	TotalMarkedStale int
}

func (s *Service) syncCanonicalRulesets(ctx context.Context, projectID, projectRoot, rulesFile, tagsFile string, apply bool) (canonicalRulesetSyncResult, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return canonicalRulesetSyncResult{}, errors.New("project_id is required")
	}

	tagNormalizer, err := s.loadCanonicalTagNormalizer(projectRoot, tagsFile)
	if err != nil {
		return canonicalRulesetSyncResult{}, fmt.Errorf("load canonical tags: %w", err)
	}

	sources, err := discoverCanonicalRulesetSources(projectRoot, rulesFile)
	if err != nil {
		return canonicalRulesetSyncResult{}, err
	}

	result := canonicalRulesetSyncResult{Sources: make([]canonicalRulesetSyncSourceResult, 0, len(sources))}
	for _, source := range sources {
		sourceResult := canonicalRulesetSyncSourceResult{
			SourcePath: source.SourcePath,
			Exists:     source.Exists,
		}

		var rules []canonicalRulePointer
		if source.Exists {
			parsed, parseErr := parseCanonicalRulesetFile(source, projectID, tagNormalizer)
			if parseErr != nil {
				return canonicalRulesetSyncResult{}, parseErr
			}
			rules = parsed
			sourceResult.RuleCount = len(rules)
			result.TotalRules += len(rules)
			if !apply {
				sourceResult.Upserted = len(rules)
			}
		}

		if apply {
			applied, applyErr := s.repo.SyncRulePointers(ctx, core.RulePointerSyncInput{
				ProjectID:  projectID,
				SourcePath: source.SourcePath,
				Pointers:   toCoreRulePointers(rules),
			})
			if applyErr != nil {
				return canonicalRulesetSyncResult{}, applyErr
			}
			sourceResult.Upserted = applied.Upserted
			sourceResult.MarkedStale = applied.MarkedStale
			result.TotalUpserted += applied.Upserted
			result.TotalMarkedStale += applied.MarkedStale
		}

		result.Sources = append(result.Sources, sourceResult)
	}

	return result, nil
}

func discoverCanonicalRulesetSources(projectRoot, rulesFile string) ([]canonicalRulesetSource, error) {
	sourcePaths := canonicalRulesetDefaultPaths
	if trimmedRulesFile := strings.TrimSpace(rulesFile); trimmedRulesFile != "" {
		sourcePaths = []string{trimmedRulesFile}
	}

	sources := make([]canonicalRulesetSource, 0, len(sourcePaths))
	for _, rawPath := range sourcePaths {
		sourcePath, absolutePath, err := resolveProjectSourcePath(projectRoot, rawPath)
		if err != nil {
			return nil, fmt.Errorf("stat canonical ruleset %s: %w", strings.TrimSpace(rawPath), err)
		}
		stat, err := os.Stat(absolutePath)
		var exists bool
		switch {
		case err == nil:
			exists = !stat.IsDir()
		case errors.Is(err, os.ErrNotExist):
			exists = false
		default:
			return nil, fmt.Errorf("stat canonical ruleset %s: %w", sourcePath, err)
		}
		sources = append(sources, canonicalRulesetSource{
			SourcePath:   sourcePath,
			AbsolutePath: absolutePath,
			Exists:       exists,
		})
	}
	sort.Slice(sources, func(i, j int) bool {
		return sources[i].SourcePath < sources[j].SourcePath
	})
	return sources, nil
}

func parseCanonicalRulesetFile(source canonicalRulesetSource, projectID string, tagNormalizer canonicalTagNormalizer) ([]canonicalRulePointer, error) {
	blob, err := os.ReadFile(source.AbsolutePath)
	if err != nil {
		return nil, fmt.Errorf("read canonical ruleset %s: %w", source.SourcePath, err)
	}

	decoder := yaml.NewDecoder(strings.NewReader(string(blob)))
	decoder.KnownFields(true)

	doc := canonicalRulesetDocumentV1{}
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse canonical ruleset %s: %w", source.SourcePath, err)
	}

	if strings.TrimSpace(doc.Version) != canonicalRulesVersionV1 {
		return nil, fmt.Errorf("canonical ruleset %s has unsupported version %q", source.SourcePath, strings.TrimSpace(doc.Version))
	}

	seenRuleIDs := make(map[string]struct{}, len(doc.Rules))
	rules := make([]canonicalRulePointer, 0, len(doc.Rules))
	for i, rawRule := range doc.Rules {
		summary := firstNonEmpty(rawRule.Summary, rawRule.Title, rawRule.Content)
		if summary == "" {
			return nil, fmt.Errorf("canonical ruleset %s rules[%d] requires summary/title/content", source.SourcePath, i)
		}

		enforcement, err := normalizeRuleEnforcement(rawRule.Enforcement)
		if err != nil {
			return nil, fmt.Errorf("canonical ruleset %s rules[%d] %w", source.SourcePath, i, err)
		}

		tags := normalizeCanonicalRulesetTags(tagNormalizer, rawRule.Tags, enforcement)
		ruleID, err := canonicalRuleID(rawRule.ID, source.SourcePath, summary, rawRule.Content, enforcement, tags, seenRuleIDs)
		if err != nil {
			return nil, fmt.Errorf("canonical ruleset %s rules[%d] %w", source.SourcePath, i, err)
		}

		content := strings.TrimSpace(rawRule.Content)
		if content == "" {
			content = summary
		}

		pointerKey := canonicalRulePointerKey(projectID, source.SourcePath, ruleID)
		rules = append(rules, canonicalRulePointer{
			PointerKey:  pointerKey,
			SourcePath:  source.SourcePath,
			RuleID:      ruleID,
			Summary:     summary,
			Content:     content,
			Enforcement: enforcement,
			Tags:        tags,
		})
	}

	sort.Slice(rules, func(i, j int) bool {
		return rules[i].PointerKey < rules[j].PointerKey
	})
	return rules, nil
}

func canonicalRulesetWarnings(result canonicalRulesetSyncResult) []string {
	warnings := make([]string, 0, len(result.Sources))
	for _, source := range result.Sources {
		if !source.Exists || source.RuleCount == 0 {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("ruleset discovered: %s (%d rules)", source.SourcePath, source.RuleCount))
	}
	return normalizeValues(warnings)
}

func canonicalRuleID(rawID, sourcePath, summary, content, enforcement string, tags []string, seen map[string]struct{}) (string, error) {
	ruleID := strings.TrimSpace(rawID)
	if ruleID != "" {
		if !ruleIDPattern.MatchString(ruleID) {
			return "", fmt.Errorf("rule id %q has invalid format", ruleID)
		}
		if _, exists := seen[ruleID]; exists {
			return "", fmt.Errorf("duplicate rule id %q", ruleID)
		}
		seen[ruleID] = struct{}{}
		return ruleID, nil
	}

	base := deterministicRuleID(sourcePath, summary, content, enforcement, tags)
	ruleID = base
	for suffix := 2; ; suffix++ {
		if _, exists := seen[ruleID]; !exists {
			seen[ruleID] = struct{}{}
			return ruleID, nil
		}
		ruleID = fmt.Sprintf("%s-%d", base, suffix)
	}
}

func deterministicRuleID(sourcePath, summary, content, enforcement string, tags []string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(sourcePath))
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(summary))
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(content))
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(enforcement))
	b.WriteString("\n")
	for _, tag := range normalizeValues(tags) {
		b.WriteString(tag)
		b.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(b.String()))
	return "rule-" + hex.EncodeToString(digest[:8])
}

func canonicalRulePointerKey(projectID, sourcePath, ruleID string) string {
	return fmt.Sprintf("%s:%s#%s", strings.TrimSpace(projectID), strings.TrimSpace(sourcePath), strings.TrimSpace(ruleID))
}

func normalizeRuleEnforcement(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "hard", nil
	}
	switch value {
	case "hard", "soft":
		return value, nil
	default:
		return "", errors.New("enforcement must be hard|soft")
	}
}

func normalizeCanonicalRulesetTags(tagNormalizer canonicalTagNormalizer, tags []string, enforcement string) []string {
	normalized := append([]string{}, tags...)
	normalized = append(normalized, "rule", ruleTagCanonical)
	if strings.EqualFold(enforcement, "soft") {
		normalized = append(normalized, ruleTagEnforcementSoft)
	} else {
		normalized = append(normalized, ruleTagEnforcementHard)
	}
	return tagNormalizer.normalizeTags(normalized)
}

func toCoreRulePointers(pointers []canonicalRulePointer) []core.RulePointer {
	if len(pointers) == 0 {
		return nil
	}
	out := make([]core.RulePointer, 0, len(pointers))
	for _, pointer := range pointers {
		out = append(out, core.RulePointer{
			PointerKey:  pointer.PointerKey,
			SourcePath:  pointer.SourcePath,
			RuleID:      pointer.RuleID,
			Summary:     pointer.Summary,
			Content:     pointer.Content,
			Enforcement: pointer.Enforcement,
			Tags:        append([]string(nil), pointer.Tags...),
		})
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		return trimmed
	}
	return ""
}
