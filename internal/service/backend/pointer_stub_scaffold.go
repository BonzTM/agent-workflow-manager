package backend

import (
	"fmt"
	"path"
	"strings"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func buildIndexedPointerStubs(projectID string, violations []v1.CompletionViolation, tagNormalizer canonicalTagNormalizer) []core.PointerStub {
	projectID = strings.TrimSpace(projectID)
	seenPath := make(map[string]struct{}, len(violations))
	stubs := make([]core.PointerStub, 0, len(violations))
	for _, violation := range violations {
		normalizedPath := normalizeCompletionPath(violation.Path)
		if normalizedPath == "" {
			continue
		}
		if _, exists := seenPath[normalizedPath]; exists {
			continue
		}
		seenPath[normalizedPath] = struct{}{}

		kind := inferPointerKindFromPath(normalizedPath)
		stubs = append(stubs, core.PointerStub{
			PointerKey:  fmt.Sprintf("%s:%s", projectID, normalizedPath),
			Path:        normalizedPath,
			Kind:        kind,
			Label:       normalizedPath,
			Description: fmt.Sprintf("Indexed %s pointer stub for %s. Curate label, description, and tags.", kind, normalizedPath),
			Tags:        inferPointerTagsFromPath(normalizedPath, kind, tagNormalizer),
		})
	}
	return stubs
}

func inferPointerKindFromPath(filePath string) string {
	pathValue := strings.ToLower(strings.TrimSpace(filePath))
	switch {
	case strings.Contains(pathValue, "/test/"),
		strings.Contains(pathValue, "/tests/"),
		strings.HasSuffix(pathValue, "_test.go"),
		strings.HasSuffix(pathValue, ".test.ts"),
		strings.HasSuffix(pathValue, ".test.tsx"),
		strings.HasSuffix(pathValue, ".spec.ts"),
		strings.HasSuffix(pathValue, ".spec.tsx"),
		strings.HasSuffix(pathValue, ".spec.js"),
		strings.HasSuffix(pathValue, ".spec.jsx"):
		return "test"
	case strings.HasPrefix(pathValue, "docs/"),
		strings.HasSuffix(pathValue, ".md"),
		strings.HasSuffix(pathValue, ".mdx"),
		strings.HasSuffix(pathValue, ".rst"),
		strings.HasSuffix(pathValue, ".adoc"):
		return "doc"
	case strings.HasPrefix(pathValue, "scripts/"),
		strings.HasSuffix(pathValue, ".sh"),
		strings.HasSuffix(pathValue, ".bash"),
		strings.HasSuffix(pathValue, ".ps1"),
		strings.HasSuffix(pathValue, ".bat"):
		return "command"
	default:
		return "code"
	}
}

func inferPointerTagsFromPath(filePath, kind string, tagNormalizer canonicalTagNormalizer) []string {
	tags := []string{"indexed", kind}
	baseName := strings.TrimSuffix(path.Base(filePath), path.Ext(filePath))
	if normalized := tagNormalizer.normalizeTag(baseName); healthTagPattern.MatchString(normalized) {
		tags = append(tags, normalized)
	}
	segments := strings.Split(path.Dir(filePath), "/")
	for _, segment := range segments {
		normalized := tagNormalizer.normalizeTag(segment)
		if !healthTagPattern.MatchString(normalized) {
			continue
		}
		tags = append(tags, normalized)
	}
	return tagNormalizer.normalizeTags(tags)
}
