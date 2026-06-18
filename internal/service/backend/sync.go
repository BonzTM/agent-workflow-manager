package backend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	bootstrapkit "github.com/bonztm/agent-workflow-manager/internal/bootstrap"
	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func (s *Service) Sync(ctx context.Context, payload v1.SyncPayload) (v1.SyncResult, *core.APIError) {
	if s == nil || s.repo == nil {
		return v1.SyncResult{}, backendError(v1.ErrCodeInternalError, "service repository is not configured", nil)
	}

	mode := normalizeSyncMode(payload.Mode)
	gitRange := normalizeSyncGitRange(mode, payload.GitRange)
	projectRoot := s.effectiveProjectRoot(payload.ProjectRoot)
	insertNewCandidates := effectiveInsertNewCandidates(payload.InsertNewCandidates)
	projectID := strings.TrimSpace(payload.ProjectID)

	paths, err := s.collectSyncPaths(ctx, mode, gitRange, projectRoot)
	if err != nil {
		return v1.SyncResult{}, syncInternalError(syncOperationFromError(err), err)
	}

	applied, err := s.repo.ApplySync(ctx, core.SyncApplyInput{
		ProjectID:           projectID,
		Mode:                mode,
		InsertNewCandidates: insertNewCandidates,
		Paths:               toCoreSyncPaths(paths),
	})
	if err != nil {
		return v1.SyncResult{}, syncInternalError("apply_sync", err)
	}

	if _, err := s.syncCanonicalRulesets(ctx, projectID, projectRoot, payload.RulesFile, payload.TagsFile, true); err != nil {
		return v1.SyncResult{}, syncInternalError("sync_ruleset", err)
	}

	indexedStubs := 0
	if insertNewCandidates {
		indexedStubs, err = s.upsertAutoIndexedPaths(ctx, projectID, projectRoot, payload.TagsFile, liveSyncPaths(paths))
		if err != nil {
			return v1.SyncResult{}, syncInternalError("upsert_pointer_stubs", err)
		}
	}

	return v1.SyncResult{
		Updated:            applied.Updated,
		MarkedStale:        applied.MarkedStale,
		NewCandidates:      indexedStubs,
		IndexedStubs:       indexedStubs,
		DeletedMarkedStale: applied.DeletedMarkedStale,
		ProcessedPaths:     processedSyncPaths(paths),
	}, nil
}

func resolveProjectSourcePath(projectRoot, raw string) (string, string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", fmt.Errorf("source path is required")
	}

	root := bootstrapkit.NormalizeProjectRoot(projectRoot)
	normalizedSlashes := strings.ReplaceAll(trimmed, "\\", "/")
	if strings.HasPrefix(normalizedSlashes, "/") || isWindowsAbsolutePath(normalizedSlashes) {
		absolutePath := filepath.Clean(trimmed)
		return path.Clean(normalizedSlashes), absolutePath, nil
	}

	relativePath := normalizeCompletionPath(trimmed)
	if relativePath == "" {
		return "", "", fmt.Errorf("source path must be repository-relative")
	}
	absolutePath := filepath.Clean(filepath.Join(root, filepath.FromSlash(relativePath)))
	return relativePath, absolutePath, nil
}

func (s *Service) collectSyncPaths(ctx context.Context, mode, gitRange, projectRoot string) ([]syncPathRecord, error) {
	switch mode {
	case syncModeFull:
		return s.collectFullSyncPaths(ctx, projectRoot)
	case syncModeWorkingTree:
		return s.collectWorkingTreeSyncPaths(ctx, projectRoot)
	default:
		return s.collectChangedSyncPaths(ctx, gitRange, projectRoot)
	}
}

func (s *Service) collectChangedSyncPaths(ctx context.Context, gitRange, projectRoot string) ([]syncPathRecord, error) {
	diffOutput, err := s.runGit(ctx, projectRoot, "diff", "--name-status", "--find-renames", gitRange)
	if err != nil {
		return nil, wrapSyncOperationError("git_diff", err)
	}

	paths, err := parseChangedNameStatus(diffOutput)
	if err != nil {
		return nil, wrapSyncOperationError("git_diff_parse", err)
	}
	if len(paths) == 0 {
		return nil, nil
	}

	livePaths := make([]string, 0, len(paths))
	for _, record := range paths {
		if record.Deleted {
			continue
		}
		livePaths = append(livePaths, record.Path)
	}

	hashByPath, err := s.resolveContentHashes(ctx, projectRoot, syncRangeEndRef(gitRange), livePaths)
	if err != nil {
		return nil, wrapSyncOperationError("git_ls_tree", err)
	}

	for i := range paths {
		if paths[i].Deleted {
			continue
		}
		contentHash := strings.TrimSpace(hashByPath[paths[i].Path])
		if contentHash == "" {
			return nil, wrapSyncOperationError("git_ls_tree", fmt.Errorf("missing blob hash for path %q", paths[i].Path))
		}
		paths[i].ContentHash = contentHash
	}
	return paths, nil
}

func (s *Service) collectWorkingTreeSyncPaths(ctx context.Context, projectRoot string) ([]syncPathRecord, error) {
	diffOutput, err := s.runGit(ctx, projectRoot, "diff", "--name-status", "--find-renames", "HEAD")
	if err != nil {
		return nil, wrapSyncOperationError("git_diff", err)
	}

	paths, err := parseChangedNameStatus(diffOutput)
	if err != nil {
		return nil, wrapSyncOperationError("git_diff_parse", err)
	}

	untrackedOutput, err := s.runGit(ctx, projectRoot, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, wrapSyncOperationError("git_ls_files", err)
	}

	byPath := make(map[string]syncPathRecord, len(paths))
	for _, record := range paths {
		if record.Path == "" {
			continue
		}
		byPath[record.Path] = record
	}
	for _, filePath := range parseInitCandidateGitPaths(untrackedOutput) {
		if filePath == "" {
			continue
		}
		byPath[filePath] = syncPathRecord{Path: filePath, Deleted: false}
	}

	if len(byPath) == 0 {
		return nil, nil
	}

	livePaths := make([]string, 0, len(byPath))
	for _, record := range byPath {
		if record.Deleted {
			continue
		}
		livePaths = append(livePaths, record.Path)
	}
	hashByPath, err := computeFileHashes(projectRoot, livePaths)
	if err != nil {
		return nil, wrapSyncOperationError("read_working_tree", err)
	}

	keys := make([]string, 0, len(byPath))
	for key := range byPath {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]syncPathRecord, 0, len(keys))
	for _, key := range keys {
		record := byPath[key]
		if !record.Deleted {
			record.ContentHash = strings.TrimSpace(hashByPath[record.Path])
			if record.ContentHash == "" {
				return nil, wrapSyncOperationError("read_working_tree", fmt.Errorf("missing content hash for path %q", record.Path))
			}
		}
		result = append(result, record)
	}

	return result, nil
}

func (s *Service) collectFullSyncPaths(ctx context.Context, projectRoot string) ([]syncPathRecord, error) {
	output, err := s.runGit(ctx, projectRoot, "ls-tree", "-r", "HEAD")
	if err != nil {
		return nil, wrapSyncOperationError("git_ls_tree", err)
	}

	hashByPath, err := parseLsTreeHashes(output)
	if err != nil {
		return nil, wrapSyncOperationError("git_ls_tree_parse", err)
	}

	keys := make([]string, 0, len(hashByPath))
	for key := range hashByPath {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]syncPathRecord, 0, len(keys))
	for _, key := range keys {
		out = append(out, syncPathRecord{
			Path:        key,
			ContentHash: hashByPath[key],
			Deleted:     false,
		})
	}
	return out, nil
}

func parseChangedNameStatus(output string) ([]syncPathRecord, error) {
	byPath := make(map[string]syncPathRecord)
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		columns := strings.Split(line, "\t")
		if len(columns) < 2 {
			return nil, fmt.Errorf("invalid name-status line: %q", line)
		}

		status := strings.TrimSpace(columns[0])
		switch {
		case strings.HasPrefix(status, "R"):
			if len(columns) < 3 {
				return nil, fmt.Errorf("invalid rename line: %q", line)
			}
			addSyncPathRecord(byPath, normalizeCompletionPath(columns[1]), true)
			addSyncPathRecord(byPath, normalizeCompletionPath(columns[2]), false)
		case strings.HasPrefix(status, "D"):
			addSyncPathRecord(byPath, normalizeCompletionPath(columns[1]), true)
		default:
			addSyncPathRecord(byPath, normalizeCompletionPath(columns[1]), false)
		}
	}

	keys := make([]string, 0, len(byPath))
	for key := range byPath {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]syncPathRecord, 0, len(keys))
	for _, key := range keys {
		out = append(out, byPath[key])
	}
	return out, nil
}

func addSyncPathRecord(byPath map[string]syncPathRecord, path string, deleted bool) {
	if path == "" {
		return
	}
	if isManagedProjectPath(path) {
		return
	}

	current, exists := byPath[path]
	if !exists {
		byPath[path] = syncPathRecord{Path: path, Deleted: deleted}
		return
	}

	if !deleted {
		current.Deleted = false
	}
	byPath[path] = current
}

func parseLsTreeHashes(output string) (map[string]string, error) {
	hashByPath := make(map[string]string)
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid ls-tree line: %q", line)
		}
		meta := strings.Fields(parts[0])
		if len(meta) < 3 {
			return nil, fmt.Errorf("invalid ls-tree metadata: %q", line)
		}
		if strings.TrimSpace(meta[1]) != "blob" {
			continue
		}

		contentHash := strings.TrimSpace(meta[2])
		if contentHash == "" {
			return nil, fmt.Errorf("missing blob hash in line: %q", line)
		}
		filePath := normalizeCompletionPath(parts[1])
		if filePath == "" {
			continue
		}
		if isManagedProjectPath(filePath) {
			continue
		}
		hashByPath[filePath] = contentHash
	}
	return hashByPath, nil
}

func (s *Service) resolveContentHashes(ctx context.Context, projectRoot, ref string, paths []string) (map[string]string, error) {
	normalizedPaths := normalizeCompletionPaths(paths)
	if len(normalizedPaths) == 0 {
		return map[string]string{}, nil
	}

	if strings.TrimSpace(ref) == "" {
		ref = "HEAD"
	}

	hashes, err := s.lookupHashesForRef(ctx, projectRoot, ref, normalizedPaths)
	if err != nil {
		if ref == "HEAD" {
			return nil, err
		}
		hashes, err = s.lookupHashesForRef(ctx, projectRoot, "HEAD", normalizedPaths)
		if err != nil {
			return nil, err
		}
	}

	if ref != "HEAD" {
		missing := missingPathsForHashes(normalizedPaths, hashes)
		if len(missing) > 0 {
			fallbackHashes, fallbackErr := s.lookupHashesForRef(ctx, projectRoot, "HEAD", missing)
			if fallbackErr != nil {
				return nil, fallbackErr
			}
			for key, value := range fallbackHashes {
				hashes[key] = value
			}
		}
	}

	if missing := missingPathsForHashes(normalizedPaths, hashes); len(missing) > 0 {
		return nil, fmt.Errorf("missing blob hashes for paths: %s", strings.Join(missing, ", "))
	}

	return hashes, nil
}

func (s *Service) lookupHashesForRef(ctx context.Context, projectRoot, ref string, paths []string) (map[string]string, error) {
	args := append([]string{"ls-tree", "-r", ref, "--"}, paths...)
	output, err := s.runGit(ctx, projectRoot, args...)
	if err != nil {
		return nil, err
	}
	return parseLsTreeHashes(output)
}

func missingPathsForHashes(paths []string, hashes map[string]string) []string {
	if len(paths) == 0 {
		return nil
	}

	missing := make([]string, 0, len(paths))
	for _, p := range paths {
		if strings.TrimSpace(hashes[p]) != "" {
			continue
		}
		missing = append(missing, p)
	}
	return missing
}

func computeFileHashes(projectRoot string, paths []string) (map[string]string, error) {
	normalizedPaths := normalizeCompletionPaths(paths)
	if len(normalizedPaths) == 0 {
		return map[string]string{}, nil
	}

	root := normalizeSyncProjectRoot(projectRoot)
	hashes := make(map[string]string, len(normalizedPaths))
	for _, relativePath := range normalizedPaths {
		fullPath := filepath.Join(root, filepath.FromSlash(relativePath))
		blob, err := os.ReadFile(fullPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", relativePath, err)
		}
		sum := sha256.Sum256(blob)
		hashes[relativePath] = hex.EncodeToString(sum[:])
	}

	return hashes, nil
}

func processedSyncPaths(paths []syncPathRecord) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	for _, record := range paths {
		out = append(out, record.Path)
	}
	return out
}

func liveSyncPaths(paths []syncPathRecord) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	for _, record := range paths {
		if record.Deleted {
			continue
		}
		out = append(out, record.Path)
	}
	return normalizeCompletionPaths(out)
}

func toCoreSyncPaths(paths []syncPathRecord) []core.SyncPath {
	if len(paths) == 0 {
		return nil
	}
	out := make([]core.SyncPath, 0, len(paths))
	for _, record := range paths {
		out = append(out, core.SyncPath{
			Path:        record.Path,
			ContentHash: record.ContentHash,
			Deleted:     record.Deleted,
		})
	}
	return out
}

func normalizeSyncMode(mode string) string {
	normalized := strings.TrimSpace(strings.ToLower(mode))
	switch normalized {
	case syncModeFull:
		return syncModeFull
	case syncModeWorkingTree:
		return syncModeWorkingTree
	default:
		return syncModeChanged
	}
}

func normalizeSyncGitRange(mode, gitRange string) string {
	normalized := strings.TrimSpace(gitRange)
	if mode == syncModeChanged && normalized == "" {
		return defaultSyncGitRange
	}
	return normalized
}

func syncRangeEndRef(gitRange string) string {
	trimmed := strings.TrimSpace(gitRange)
	if trimmed == "" {
		return "HEAD"
	}
	if strings.Contains(trimmed, "...") {
		parts := strings.SplitN(trimmed, "...", 2)
		end := strings.TrimSpace(parts[1])
		if end != "" {
			return end
		}
		return "HEAD"
	}
	if strings.Contains(trimmed, "..") {
		parts := strings.SplitN(trimmed, "..", 2)
		end := strings.TrimSpace(parts[1])
		if end != "" {
			return end
		}
	}
	return "HEAD"
}

func normalizeSyncProjectRoot(projectRoot string) string {
	trimmed := strings.TrimSpace(projectRoot)
	if trimmed == "" {
		return defaultSyncProjectDir
	}
	return trimmed
}

func effectiveInsertNewCandidates(insertNewCandidates *bool) bool {
	if insertNewCandidates == nil {
		return true
	}
	return *insertNewCandidates
}

func (s *Service) upsertAutoIndexedPaths(ctx context.Context, projectID, projectRoot, tagsFile string, paths []string) (int, error) {
	projectID = strings.TrimSpace(projectID)
	normalizedPaths := normalizeCompletionPaths(paths)
	if projectID == "" || len(normalizedPaths) == 0 {
		return 0, nil
	}

	inventory, err := s.repo.ListPointerInventory(ctx, projectID)
	if err != nil {
		return 0, err
	}

	indexedByPath := make(map[string]struct{}, len(inventory))
	for _, item := range inventory {
		normalizedPath := normalizeCompletionPath(item.Path)
		if normalizedPath == "" {
			continue
		}
		indexedByPath[normalizedPath] = struct{}{}
	}

	unindexedPaths := make([]string, 0, len(normalizedPaths))
	for _, filePath := range normalizedPaths {
		if _, exists := indexedByPath[filePath]; exists {
			continue
		}
		unindexedPaths = append(unindexedPaths, filePath)
	}
	if len(unindexedPaths) == 0 {
		return 0, nil
	}

	tagNormalizer, err := s.loadCanonicalTagNormalizer(projectRoot, tagsFile)
	if err != nil {
		return 0, err
	}

	violations := make([]v1.CompletionViolation, 0, len(unindexedPaths))
	for _, filePath := range unindexedPaths {
		violations = append(violations, v1.CompletionViolation{
			Path:   filePath,
			Reason: "index unindexed file",
		})
	}

	return s.repo.UpsertPointerStubs(ctx, projectID, buildIndexedPointerStubs(projectID, violations, tagNormalizer))
}

func (s *Service) runGit(ctx context.Context, projectRoot string, args ...string) (string, error) {
	if s != nil && s.runGitCommand != nil {
		return s.runGitCommand(ctx, projectRoot, args...)
	}
	return runGitCommand(ctx, projectRoot, args...)
}

func runGitCommand(ctx context.Context, projectRoot string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = normalizeSyncProjectRoot(projectRoot)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText == "" {
			stderrText = strings.TrimSpace(stdout.String())
		}
		if stderrText == "" {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderrText)
	}

	return stdout.String(), nil
}

func wrapSyncOperationError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return &syncOperationError{operation: operation, err: err}
}

func syncOperationFromError(err error) string {
	var opErr *syncOperationError
	if errors.As(err, &opErr) && strings.TrimSpace(opErr.operation) != "" {
		return opErr.operation
	}
	return "sync"
}

func syncInternalError(operation string, err error) *core.APIError {
	return backendError(v1.ErrCodeInternalError, "failed to sync project", map[string]any{
		"operation": operation,
		"error":     err.Error(),
	})
}
