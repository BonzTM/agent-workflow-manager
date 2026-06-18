package backend

import (
	"context"
	"math"
	"path"
	"sort"
	"strings"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

type inventoryHealthSummary struct {
	TotalFiles     int
	IndexedFiles   int
	UnindexedFiles int
	StaleFiles     int
	IndexedPercent float64
}

type inventoryHealthReport struct {
	Summary        inventoryHealthSummary
	UnindexedPaths []string
	StalePaths     []string
	UnindexedDirs  []string
}

func (s *Service) computeInventoryHealth(ctx context.Context, projectID, projectRoot string) (inventoryHealthReport, *core.APIError) {
	if s == nil || s.repo == nil {
		return inventoryHealthReport{}, backendError(v1.ErrCodeInternalError, "service repository is not configured", nil)
	}

	projectRoot = s.effectiveProjectRoot(projectRoot)
	paths, err := s.collectInventoryPaths(ctx, projectRoot)
	if err != nil {
		return inventoryHealthReport{}, inventoryInternalError("collect_project_paths", err)
	}

	inventory, err := s.repo.ListPointerInventory(ctx, strings.TrimSpace(projectID))
	if err != nil {
		return inventoryHealthReport{}, inventoryInternalError("list_pointer_inventory", err)
	}

	pointerByPath := make(map[string]core.PointerInventory, len(inventory))
	for _, item := range inventory {
		normalizedPath := normalizeCompletionPath(item.Path)
		if normalizedPath == "" {
			continue
		}
		current, exists := pointerByPath[normalizedPath]
		if !exists {
			pointerByPath[normalizedPath] = core.PointerInventory{Path: normalizedPath, IsStale: item.IsStale}
			continue
		}
		current.IsStale = current.IsStale || item.IsStale
		pointerByPath[normalizedPath] = current
	}

	indexedCount := 0
	unindexed := make([]string, 0)
	trackedPaths := make(map[string]struct{}, len(paths))
	for _, filePath := range paths {
		trackedPaths[filePath] = struct{}{}
		if _, ok := pointerByPath[filePath]; ok {
			indexedCount++
			continue
		}
		unindexed = append(unindexed, filePath)
	}

	stale := make([]string, 0)
	for filePath, item := range pointerByPath {
		if !item.IsStale {
			continue
		}
		if _, ok := trackedPaths[filePath]; !ok {
			continue
		}
		stale = append(stale, filePath)
	}
	sort.Strings(stale)

	unindexedDirs := unindexedDirectories(paths, pointerByPath)

	totalFiles := len(paths)
	indexedPercent := 100.0
	if totalFiles > 0 {
		indexedPercent = math.Round((float64(indexedCount)/float64(totalFiles)*100.0)*100.0) / 100.0
	}

	return inventoryHealthReport{
		Summary: inventoryHealthSummary{
			TotalFiles:     totalFiles,
			IndexedFiles:   indexedCount,
			UnindexedFiles: len(unindexed),
			StaleFiles:     len(stale),
			IndexedPercent: indexedPercent,
		},
		UnindexedPaths: normalizeValues(unindexed),
		StalePaths:     normalizeValues(stale),
		UnindexedDirs:  unindexedDirs,
	}, nil
}

func (s *Service) collectInventoryPaths(ctx context.Context, projectRoot string) ([]string, error) {
	gitOutput, err := s.runGit(ctx, projectRoot, "ls-files", "--cached", "--others", "--exclude-standard")
	if err == nil {
		paths := parseInitCandidateGitPaths(gitOutput)
		deletedOutput, deletedErr := s.runGit(ctx, projectRoot, "ls-files", "--deleted")
		if deletedErr != nil {
			return nil, deletedErr
		}
		deleted := make(map[string]struct{})
		for _, filePath := range parseInitCandidateGitPaths(deletedOutput) {
			deleted[filePath] = struct{}{}
		}
		if len(deleted) == 0 {
			return paths, nil
		}

		filtered := make([]string, 0, len(paths))
		for _, filePath := range paths {
			if _, isDeleted := deleted[filePath]; isDeleted {
				continue
			}
			filtered = append(filtered, filePath)
		}
		return filtered, nil
	}

	paths, _, walkErr := collectInitCandidatePathsFromWalk(ctx, projectRoot)
	if walkErr != nil {
		return nil, walkErr
	}
	return filterInitCandidatePaths(paths, nil), nil
}

func unindexedDirectories(paths []string, pointerByPath map[string]core.PointerInventory) []string {
	if len(paths) == 0 {
		return nil
	}

	dirTotal := make(map[string]int)
	dirCovered := make(map[string]int)
	for _, filePath := range paths {
		dir := path.Dir(filePath)
		if dir == "." || dir == "" {
			continue
		}
		dirTotal[dir]++
		if _, ok := pointerByPath[filePath]; ok {
			dirCovered[dir]++
		}
	}

	out := make([]string, 0)
	for dir, total := range dirTotal {
		if total <= 0 {
			continue
		}
		if dirCovered[dir] > 0 {
			continue
		}
		out = append(out, dir)
	}
	sort.Strings(out)
	return out
}

func inventoryInternalError(operation string, err error) *core.APIError {
	return backendError(v1.ErrCodeInternalError, "failed to compute inventory health", map[string]any{
		"operation": operation,
		"error":     err.Error(),
	})
}
