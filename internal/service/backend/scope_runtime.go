package backend

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	"github.com/bonztm/agent-workflow-manager/internal/workspace"
)

func (s *Service) captureWorkingTreeBaseline(ctx context.Context, projectRoot string) ([]core.SyncPath, *core.APIError) {
	records, err := s.collectWorkingTreeSyncPaths(ctx, projectRoot)
	if err == nil {
		return syncPathRecordsToBaseline(records), nil
	}
	if projectRootContainsGitDir(projectRoot) {
		details := map[string]any{
			"project_root": strings.TrimSpace(projectRoot),
			"git_error":    err.Error(),
		}
		return nil, backendError(v1.ErrCodeInternalError, "failed to capture working-tree baseline", details)
	}

	paths, walkErr := s.captureWorkingTreeBaselineFromWalk(ctx, projectRoot)
	if walkErr == nil {
		return paths, nil
	}

	details := map[string]any{
		"project_root": strings.TrimSpace(projectRoot),
		"git_error":    err.Error(),
	}
	if walkErr != nil {
		details["walk_error"] = walkErr.Error()
	}
	return nil, backendError(v1.ErrCodeInternalError, "failed to capture working-tree baseline", details)
}

func projectRootContainsGitDir(projectRoot string) bool {
	root := strings.TrimSpace(projectRoot)
	if root == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(root, ".git"))
	return err == nil && info.IsDir()
}

func syncPathRecordsToBaseline(records []syncPathRecord) []core.SyncPath {
	if len(records) == 0 {
		return nil
	}
	paths := make([]core.SyncPath, 0, len(records))
	for _, record := range records {
		paths = append(paths, core.SyncPath{
			Path:        normalizeCompletionPath(record.Path),
			ContentHash: strings.TrimSpace(record.ContentHash),
			Deleted:     record.Deleted,
		})
	}
	return paths
}

func (s *Service) captureWorkingTreeBaselineFromWalk(ctx context.Context, projectRoot string) ([]core.SyncPath, error) {
	paths := make([]string, 0)
	err := filepath.WalkDir(projectRoot, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}

		relative, relErr := filepath.Rel(projectRoot, current)
		if relErr != nil {
			return relErr
		}
		normalizedPath := normalizeCompletionPath(relative)
		if baselineIgnoredPath(normalizedPath) {
			return nil
		}
		paths = append(paths, normalizedPath)
		return nil
	})
	if err != nil {
		return nil, err
	}

	normalizedPaths := normalizeCompletionPaths(paths)
	if len(normalizedPaths) == 0 {
		return nil, nil
	}
	hashByPath, err := computeFileHashes(projectRoot, normalizedPaths)
	if err != nil {
		return nil, err
	}

	out := make([]core.SyncPath, 0, len(normalizedPaths))
	for _, normalizedPath := range normalizedPaths {
		contentHash := strings.TrimSpace(hashByPath[normalizedPath])
		if contentHash == "" {
			return nil, errors.New("missing content hash for path " + normalizedPath)
		}
		out = append(out, core.SyncPath{
			Path:        normalizedPath,
			ContentHash: contentHash,
			Deleted:     false,
		})
	}
	return out, nil
}

func baselineIgnoredPath(raw string) bool {
	normalized := normalizeCompletionPath(raw)
	if normalized == "" {
		return true
	}
	switch normalized {
	case workspace.DotEnvFileName, ".awm/context.db", ".awm/context.db-shm", ".awm/context.db-wal":
		return true
	default:
		return false
	}
}

func (s *Service) detectReceiptChangedPaths(ctx context.Context, projectRoot string, scope core.ReceiptScope) ([]string, bool, *core.APIError) {
	if !scope.BaselineCaptured {
		return nil, false, nil
	}
	current, apiErr := s.captureWorkingTreeBaseline(ctx, projectRoot)
	if apiErr != nil {
		return nil, false, apiErr
	}
	return diffSyncPathStates(scope.BaselinePaths, current), true, nil
}

func diffSyncPathStates(baseline, current []core.SyncPath) []string {
	baselineByPath := make(map[string]core.SyncPath, len(baseline))
	for _, entry := range baseline {
		path := normalizeCompletionPath(entry.Path)
		if path == "" {
			continue
		}
		baselineByPath[path] = core.SyncPath{
			Path:        path,
			ContentHash: strings.TrimSpace(entry.ContentHash),
			Deleted:     entry.Deleted,
		}
	}

	currentByPath := make(map[string]core.SyncPath, len(current))
	for _, entry := range current {
		path := normalizeCompletionPath(entry.Path)
		if path == "" {
			continue
		}
		currentByPath[path] = core.SyncPath{
			Path:        path,
			ContentHash: strings.TrimSpace(entry.ContentHash),
			Deleted:     entry.Deleted,
		}
	}

	changed := make([]string, 0)
	seen := make(map[string]struct{}, len(baselineByPath)+len(currentByPath))
	for path, base := range baselineByPath {
		seen[path] = struct{}{}
		if cur, ok := currentByPath[path]; ok && syncPathStatesEqual(base, cur) {
			continue
		}
		changed = append(changed, path)
	}
	for path := range currentByPath {
		if _, ok := seen[path]; ok {
			continue
		}
		changed = append(changed, path)
	}
	sort.Strings(changed)
	return changed
}

func syncPathStatesEqual(a, b core.SyncPath) bool {
	return normalizeCompletionPath(a.Path) == normalizeCompletionPath(b.Path) &&
		strings.TrimSpace(a.ContentHash) == strings.TrimSpace(b.ContentHash) &&
		a.Deleted == b.Deleted
}

func effectiveScopePaths(scope core.ReceiptScope, plan *core.WorkPlan) []string {
	paths := append([]string(nil), scope.InitialScopePaths...)
	if plan != nil {
		paths = append(paths, plan.DiscoveredPaths...)
	}
	paths = append(paths, completionScopeManagedPaths()...)
	return normalizeCompletionPaths(paths)
}

func completionEffectiveScopePaths(scope core.ReceiptScope, plan *core.WorkPlan) []string {
	paths := append([]string(nil), scope.InitialScopePaths...)
	if plan != nil {
		paths = append(paths, plan.DiscoveredPaths...)
		paths = append(paths, plan.InScope...)
	}
	paths = append(paths, completionScopeManagedPaths()...)
	return normalizeCompletionPaths(paths)
}

func pathWithinScope(candidate string, scopePaths []string) bool {
	normalizedCandidate := normalizeCompletionPath(candidate)
	if normalizedCandidate == "" {
		return false
	}
	for _, rawScopePath := range scopePaths {
		scopePath := normalizeCompletionPath(rawScopePath)
		if scopePath == "" {
			continue
		}
		if normalizedCandidate == scopePath || strings.HasPrefix(normalizedCandidate, scopePath+"/") {
			return true
		}
	}
	return false
}

func resolveReceiptPlanSelection(projectID, receiptID, planKey string) (string, string, *core.APIError) {
	normalizedReceiptID := strings.TrimSpace(receiptID)
	normalizedPlanKey := strings.TrimSpace(planKey)
	if normalizedPlanKey == "" && normalizedReceiptID != "" {
		normalizedPlanKey = "plan:" + normalizedReceiptID
	}
	if normalizedPlanKey != "" {
		derivedReceiptID, ok := parsePlanFetchKey(normalizedPlanKey)
		if !ok {
			return "", "", backendError(v1.ErrCodeInvalidInput, "plan_key must use format plan:<receipt_id>", map[string]any{
				"project_id": strings.TrimSpace(projectID),
				"plan_key":   normalizedPlanKey,
			})
		}
		if normalizedReceiptID == "" {
			normalizedReceiptID = derivedReceiptID
		} else if normalizedReceiptID != derivedReceiptID {
			return "", "", backendError(v1.ErrCodeInvalidInput, "plan_key and receipt_id must reference the same receipt", map[string]any{
				"project_id":          strings.TrimSpace(projectID),
				"plan_key":            normalizedPlanKey,
				"receipt_id":          normalizedReceiptID,
				"plan_key_receipt_id": derivedReceiptID,
			})
		}
	}
	if normalizedReceiptID == "" {
		return "", "", backendError(v1.ErrCodeInvalidInput, "receipt_id or plan_key is required", map[string]any{
			"project_id": strings.TrimSpace(projectID),
			"plan_key":   normalizedPlanKey,
			"receipt_id": normalizedReceiptID,
		})
	}
	return normalizedReceiptID, normalizedPlanKey, nil
}

func (s *Service) loadEffectiveWorkPlan(ctx context.Context, projectID, receiptID, planKey string) (*core.WorkPlan, *core.APIError) {
	if s == nil || s.planRepo == nil {
		return nil, nil
	}

	normalizedPlanKey := strings.TrimSpace(planKey)
	normalizedReceiptID := strings.TrimSpace(receiptID)
	if normalizedPlanKey == "" && normalizedReceiptID != "" {
		normalizedPlanKey = "plan:" + normalizedReceiptID
	}
	if normalizedPlanKey == "" {
		return nil, nil
	}

	plan, err := s.planRepo.LookupWorkPlan(ctx, core.WorkPlanLookupQuery{
		ProjectID: strings.TrimSpace(projectID),
		PlanKey:   normalizedPlanKey,
		ReceiptID: normalizedReceiptID,
	})
	if err == nil {
		return &plan, nil
	}
	if errors.Is(err, core.ErrWorkPlanNotFound) {
		return nil, nil
	}
	return nil, backendError(v1.ErrCodeInternalError, "failed to load work plan", map[string]any{
		"project_id": strings.TrimSpace(projectID),
		"plan_key":   normalizedPlanKey,
		"receipt_id": normalizedReceiptID,
		"error":      err.Error(),
	})
}

func resolveDetectedFilesChanged(reliable bool, detected, supplied []string) []string {
	if reliable {
		return normalizeCompletionPaths(detected)
	}
	return normalizeCompletionPaths(supplied)
}

func resolveDoneFilesChanged(reliable bool, detected, supplied []string, noFileChanges bool) ([]string, *core.APIError) {
	if reliable {
		normalizedDetected := normalizeCompletionPaths(detected)
		if noFileChanges && len(normalizedDetected) > 0 {
			return nil, backendError(v1.ErrCodeInvalidInput, "no_file_changes cannot be used when AWM detected file changes", map[string]any{"files_changed": normalizedDetected})
		}
		return normalizedDetected, nil
	}
	if noFileChanges {
		return nil, nil
	}

	normalizedSupplied := normalizeCompletionPaths(supplied)
	if len(normalizedSupplied) == 0 {
		return nil, backendError(v1.ErrCodeInvalidInput, "automatic task delta detection is unavailable; provide files_changed or set no_file_changes", nil)
	}
	return normalizedSupplied, nil
}

func filesChangedMismatchViolations(reliable bool, detected, supplied []string) []v1.CompletionViolation {
	if !reliable {
		return nil
	}

	detectedPaths := normalizeCompletionPaths(detected)
	suppliedPaths := normalizeCompletionPaths(supplied)
	if len(suppliedPaths) == 0 {
		return nil
	}

	detectedSet := make(map[string]struct{}, len(detectedPaths))
	for _, filePath := range detectedPaths {
		detectedSet[filePath] = struct{}{}
	}
	suppliedSet := make(map[string]struct{}, len(suppliedPaths))
	for _, filePath := range suppliedPaths {
		suppliedSet[filePath] = struct{}{}
	}

	violations := make([]v1.CompletionViolation, 0, len(detectedPaths)+len(suppliedPaths))
	for _, filePath := range suppliedPaths {
		if _, ok := detectedSet[filePath]; ok {
			continue
		}
		violations = append(violations, v1.CompletionViolation{
			Path:   filePath,
			Reason: "path was supplied in files_changed but was not detected in the receipt baseline delta",
		})
	}
	for _, filePath := range detectedPaths {
		if _, ok := suppliedSet[filePath]; ok {
			continue
		}
		violations = append(violations, v1.CompletionViolation{
			Path:   filePath,
			Reason: "path was detected in the receipt baseline delta but was omitted from files_changed",
		})
	}
	return violations
}

func mergeCompletionViolations(groups ...[]v1.CompletionViolation) []v1.CompletionViolation {
	if len(groups) == 0 {
		return nil
	}

	reasonsByPath := make(map[string][]string)
	for _, group := range groups {
		for _, violation := range group {
			filePath := normalizeCompletionPath(violation.Path)
			reason := strings.TrimSpace(violation.Reason)
			if filePath == "" || reason == "" {
				continue
			}
			reasons := reasonsByPath[filePath]
			duplicate := false
			for _, existing := range reasons {
				if existing == reason {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			reasonsByPath[filePath] = append(reasons, reason)
		}
	}
	if len(reasonsByPath) == 0 {
		return nil
	}

	paths := make([]string, 0, len(reasonsByPath))
	for filePath := range reasonsByPath {
		paths = append(paths, filePath)
	}
	sort.Strings(paths)

	merged := make([]v1.CompletionViolation, 0, len(paths))
	for _, filePath := range paths {
		merged = append(merged, v1.CompletionViolation{
			Path:   filePath,
			Reason: strings.Join(reasonsByPath[filePath], "; "),
		})
	}
	return merged
}

func outOfScopeViolations(filesChanged, allowedPaths []string) []v1.CompletionViolation {
	normalizedAllowedPaths := normalizeCompletionPaths(allowedPaths)

	violations := make([]v1.CompletionViolation, 0)
	for _, filePath := range normalizeCompletionPaths(filesChanged) {
		if pathWithinScope(filePath, normalizedAllowedPaths) {
			continue
		}
		violations = append(violations, v1.CompletionViolation{
			Path:   filePath,
			Reason: "path is outside effective scope",
		})
	}
	return violations
}
