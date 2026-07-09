package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

const reviewAttemptEvidencePrefix = "reviewattempt:"

type reviewPlanState struct {
	PlanKey    string
	ReceiptID  string
	PlanStatus string
	TaskCount  int
	Task       *core.WorkItem
}

// Review records a review gate result for a work item. Without run=true it
// stores the caller-provided status as a snapshot; with run=true it executes
// the configured workflow review command, enforcing fingerprint-based skip
// rules and the gate's max-attempts budget before persisting the attempt.
func (s *Service) Review(ctx context.Context, payload v1.ReviewPayload) (v1.ReviewResult, *core.APIError) {
	if s == nil || s.repo == nil {
		return v1.ReviewResult{}, backendError(v1.ErrCodeInternalError, "service repository is not configured", nil)
	}

	normalized := v1.NormalizeReviewPayload(payload)
	state, apiErr := s.loadReviewPlanState(ctx, normalized.ProjectID, normalized.PlanKey, normalized.ReceiptID, normalized.Key)
	if apiErr != nil {
		return v1.ReviewResult{}, apiErr
	}
	attempts, apiErr := s.listReviewAttempts(ctx, normalized.ProjectID, state.ReceiptID, normalized.Key)
	if apiErr != nil {
		return v1.ReviewResult{}, apiErr
	}
	attemptsRun := len(attempts)
	passingRuns := countPassingReviewAttempts(attempts)

	if !normalized.Run {
		definitions, source, err := s.loadWorkflowCompletionRequirements(s.defaultProjectRoot(), "", normalized.TagsFile)
		if err != nil {
			return v1.ReviewResult{}, workflowDefinitionsAPIError(source.SourcePath, err)
		}
		if definition, ok := findWorkflowRequiredTaskDefinition(definitions, normalized.Key); ok && definition.Run != nil && normalized.Status == v1.WorkItemStatusComplete {
			return v1.ReviewResult{}, backendError(v1.ErrCodeInvalidInput, "runnable review gates require run=true to record a completed result", map[string]any{
				"review_key":           normalized.Key,
				"workflow_source_path": source.SourcePath,
			})
		}

		evidence := mergeReviewTaskEvidence(state.Task, normalized.Evidence, latestReviewAttemptRef(attempts))
		workResult, workErr := s.Work(ctx, v1.WorkPayload{
			ProjectID: normalized.ProjectID,
			PlanKey:   normalized.PlanKey,
			ReceiptID: normalized.ReceiptID,
			Tasks: []v1.WorkTaskPayload{{
				Key:           normalized.Key,
				Summary:       normalized.Summary,
				Status:        normalized.Status,
				Outcome:       normalized.Outcome,
				BlockedReason: normalized.BlockedReason,
				Evidence:      evidence,
			}},
		})
		if workErr != nil {
			return v1.ReviewResult{}, workErr
		}
		return v1.ReviewResultFromWork(normalized, workResult, normalized.Status, attemptsRun, 0, passingRuns, "", "", nil), nil
	}

	projectRoot := s.defaultProjectRoot()
	definitions, source, err := s.loadWorkflowCompletionRequirements(projectRoot, "", normalized.TagsFile)
	if err != nil {
		return v1.ReviewResult{}, workflowDefinitionsAPIError(source.SourcePath, err)
	}
	definition, ok := findWorkflowRequiredTaskDefinition(definitions, normalized.Key)
	if !ok {
		return v1.ReviewResult{}, backendError(v1.ErrCodeInvalidInput, "review key is not configured in workflow definitions", map[string]any{
			"review_key":           normalized.Key,
			"workflow_source_path": source.SourcePath,
		})
	}
	if definition.Run == nil {
		return v1.ReviewResult{}, backendError(v1.ErrCodeInvalidInput, "review key does not define a runnable workflow command", map[string]any{
			"review_key":           normalized.Key,
			"workflow_source_path": source.SourcePath,
		})
	}
	reviewSummary := firstNonEmpty(payload.Summary, definition.Summary, normalized.Summary)

	scope, apiErr := s.fetchReviewReceiptScope(ctx, normalized.ProjectID, state.ReceiptID)
	if apiErr != nil {
		return v1.ReviewResult{}, apiErr
	}
	plan, apiErr := s.loadEffectiveWorkPlan(ctx, normalized.ProjectID, state.ReceiptID, state.PlanKey)
	if apiErr != nil {
		return v1.ReviewResult{}, apiErr
	}
	effectiveScope := effectiveScopePaths(scope, plan)
	detectedFiles, reliableDetection, apiErr := s.detectReceiptChangedPaths(ctx, projectRoot, scope)
	if apiErr != nil {
		return v1.ReviewResult{}, apiErr
	}
	fingerprint, apiErr := computeReviewFingerprint(projectRoot, normalized.ProjectID, state.ReceiptID, normalized.Key, source.SourcePath, *definition.Run, scope, plan)
	if apiErr != nil {
		return v1.ReviewResult{}, apiErr
	}

	if definition.RerunRequiresNewFingerprint {
		if prior, ok := latestReviewAttemptByFingerprint(attempts, fingerprint); ok {
			if !reviewAttemptEligibleForFingerprintSkip(prior) {
				goto executeReview
			}
			workResult, snapErr := s.updateReviewSnapshot(ctx, normalized, state, reviewSummary, reviewStatusFromAttempt(prior), prior.Outcome, latestReviewAttemptRef([]core.ReviewAttempt{prior}))
			if snapErr != nil {
				return v1.ReviewResult{}, snapErr
			}
			return v1.ReviewResultFromWork(
				normalized,
				workResult,
				reviewStatusFromAttempt(prior),
				attemptsRun,
				definition.MaxAttempts,
				passingRuns,
				fingerprint,
				"review skipped because the current scoped fingerprint was already assessed",
				nil,
			), nil
		}
	}

	if definition.MaxAttempts > 0 && attemptsRun >= definition.MaxAttempts {
		outcome := fmt.Sprintf("Review gate blocked: max_attempts=%d exhausted after %d passing run(s).", definition.MaxAttempts, passingRuns)
		workResult, snapErr := s.updateReviewSnapshot(ctx, normalized, state, reviewSummary, v1.WorkItemStatusBlocked, outcome, latestReviewAttemptRef(attempts))
		if snapErr != nil {
			return v1.ReviewResult{}, snapErr
		}
		return v1.ReviewResultFromWork(
			normalized,
			workResult,
			v1.WorkItemStatusBlocked,
			attemptsRun,
			definition.MaxAttempts,
			passingRuns,
			fingerprint,
			"review skipped because max_attempts was exhausted",
			nil,
		), nil
	}

executeReview:
	runner := s.runReviewCommand
	if runner == nil {
		runner = runWorkflowReviewCommand
	}
	run := runner(
		ctx,
		projectRoot,
		*definition.Run,
		reviewCommandEnvironment(
			projectRoot,
			source.SourcePath,
			normalized,
			state,
			reviewSummary,
			attemptsRun+1,
			definition.MaxAttempts,
			passingRuns,
			fingerprint,
			effectiveScope,
			detectedFiles,
			reliableDetection,
		),
	)
	passed := reviewAttemptPassed(run)
	attemptStatus := reviewAttemptStorageStatus(run)

	attemptID, err := s.repo.SaveReviewAttempt(ctx, core.ReviewAttempt{
		ProjectID:          normalized.ProjectID,
		ReceiptID:          state.ReceiptID,
		PlanKey:            state.PlanKey,
		ReviewKey:          normalized.Key,
		Summary:            reviewSummary,
		Fingerprint:        fingerprint,
		Status:             attemptStatus,
		Passed:             passed,
		Outcome:            summarizeReviewCommandRun(run),
		WorkflowSourcePath: strings.TrimSpace(source.SourcePath),
		CommandArgv:        append([]string(nil), definition.Run.Argv...),
		CommandCWD:         strings.TrimSpace(definition.Run.CWD),
		TimeoutSec:         definition.Run.TimeoutSec,
		ExitCode:           run.ExitCode,
		TimedOut:           run.TimedOut,
		StdoutExcerpt:      excerptVerifyOutput(run.Stdout),
		StderrExcerpt:      excerptVerifyOutput(run.Stderr),
		CreatedAt:          run.FinishedAt,
		Actor:              actorFromPayload(normalized.Actor),
	})
	if err != nil {
		return v1.ReviewResult{}, backendError(v1.ErrCodeInternalError, "failed to persist review attempt", map[string]any{
			"review_key": normalized.Key,
			"error":      err.Error(),
		})
	}

	attemptsRun++
	if passed {
		passingRuns++
	}
	taskStatus := v1.WorkItemStatusBlocked
	if passed {
		taskStatus = v1.WorkItemStatusComplete
	}
	outcome := summarizeReviewOutcome(run, passed, attemptsRun, definition.MaxAttempts, passingRuns)
	workResult, apiErr := s.updateReviewSnapshot(ctx, normalized, state, reviewSummary, taskStatus, outcome, reviewAttemptRef(attemptID))
	if apiErr != nil {
		return v1.ReviewResult{}, backendError(apiErr.Code, "review gate ran but failed to update review task", map[string]any{
			"review_key":       normalized.Key,
			"wrapped_code":     apiErr.Code,
			"wrapped_message":  apiErr.Message,
			"workflow_command": append([]string(nil), definition.Run.Argv...),
		})
	}

	return v1.ReviewResultFromWork(
		normalized,
		workResult,
		taskStatus,
		attemptsRun,
		definition.MaxAttempts,
		passingRuns,
		fingerprint,
		"",
		reviewExecutionFromRun(source.SourcePath, *definition.Run, run),
	), nil
}

func runWorkflowReviewCommand(ctx context.Context, projectRoot string, command workflowRunDefinition, extraEnv map[string]string) verifyCommandRun {
	return runConfiguredCommand(ctx, projectRoot, command.Argv, command.CWD, command.TimeoutSec, runtimeCommandEnv(projectRoot), command.Env, extraEnv)
}

func reviewCommandEnvironment(projectRoot, workflowSourcePath string, payload v1.ReviewPayload, state reviewPlanState, reviewSummary string, attempt, maxAttempts, passingRuns int, fingerprint string, effectiveScope, detectedFiles []string, reliableDetection bool) map[string]string {
	receiptID := strings.TrimSpace(state.ReceiptID)
	if receiptID == "" {
		receiptID = strings.TrimSpace(payload.ReceiptID)
	}
	planKey := strings.TrimSpace(state.PlanKey)
	if planKey == "" {
		planKey = strings.TrimSpace(payload.PlanKey)
	}
	if planKey == "" && receiptID != "" {
		planKey = "plan:" + receiptID
	}

	env := map[string]string{
		"AWM_PROJECT_ID":           strings.TrimSpace(payload.ProjectID),
		"AWM_PROJECT_ROOT":         strings.TrimSpace(projectRoot),
		"AWM_RECEIPT_ID":           receiptID,
		"AWM_PLAN_KEY":             planKey,
		"AWM_REVIEW_KEY":           strings.TrimSpace(payload.Key),
		"AWM_REVIEW_SUMMARY":       reviewSummary,
		"AWM_REVIEW_ATTEMPT":       strconv.Itoa(attempt),
		"AWM_REVIEW_MAX_ATTEMPTS":  strconv.Itoa(maxAttempts),
		"AWM_REVIEW_PASSING_RUNS":  strconv.Itoa(passingRuns),
		"AWM_REVIEW_FINGERPRINT":   strings.TrimSpace(fingerprint),
		"AWM_WORKFLOW_SOURCE_PATH": strings.TrimSpace(workflowSourcePath),
	}
	if encodedScope, err := json.Marshal(normalizeCompletionPaths(effectiveScope)); err == nil {
		env["AWM_REVIEW_EFFECTIVE_SCOPE_PATHS_JSON"] = string(encodedScope)
	}
	env["AWM_REVIEW_BASELINE_CAPTURED"] = strconv.FormatBool(reliableDetection)
	if reliableDetection {
		if encodedChanged, err := json.Marshal(normalizeCompletionPaths(detectedFiles)); err == nil {
			env["AWM_REVIEW_CHANGED_PATHS_JSON"] = string(encodedChanged)
		}
		env["AWM_REVIEW_TASK_DELTA_SOURCE"] = "receipt_baseline"
	}
	return env
}

func (s *Service) loadReviewPlanState(ctx context.Context, projectID, planKey, receiptID, reviewKey string) (reviewPlanState, *core.APIError) {
	normalizedPlanKey := strings.TrimSpace(planKey)
	normalizedReceiptID := strings.TrimSpace(receiptID)
	if normalizedPlanKey == "" && normalizedReceiptID != "" {
		normalizedPlanKey = "plan:" + normalizedReceiptID
	}
	if normalizedPlanKey != "" {
		derivedReceiptID, ok := parsePlanFetchKey(normalizedPlanKey)
		if !ok {
			return reviewPlanState{}, backendError(v1.ErrCodeInvalidInput, "plan_key must use format plan:<receipt_id>", map[string]any{"plan_key": normalizedPlanKey})
		}
		if normalizedReceiptID == "" {
			normalizedReceiptID = derivedReceiptID
		}
	}

	state := reviewPlanState{
		PlanKey:    normalizedPlanKey,
		ReceiptID:  normalizedReceiptID,
		PlanStatus: core.PlanStatusPending,
	}

	if s.planRepo != nil && normalizedPlanKey != "" {
		plan, err := s.planRepo.LookupWorkPlan(ctx, core.WorkPlanLookupQuery{
			ProjectID: strings.TrimSpace(projectID),
			PlanKey:   normalizedPlanKey,
			ReceiptID: normalizedReceiptID,
		})
		switch {
		case err == nil:
			state.PlanKey = plan.PlanKey
			state.ReceiptID = plan.ReceiptID
			state.TaskCount = len(plan.Tasks)
			state.PlanStatus = normalizePlanStatus(plan.Status)
			if state.PlanStatus == core.PlanStatusPending {
				state.PlanStatus = derivePlanStatusFromWorkItems(normalizeWorkItems(plan.Tasks))
			}
			if task, ok := findWorkItemByKey(plan.Tasks, reviewKey); ok {
				state.Task = &task
			}
			return state, nil
		case !errors.Is(err, core.ErrWorkPlanNotFound):
			return reviewPlanState{}, workInternalError("lookup_work_plan", err)
		}
	}

	if normalizedReceiptID == "" {
		return state, nil
	}

	items, err := s.repo.ListWorkItems(ctx, core.FetchLookupQuery{
		ProjectID: strings.TrimSpace(projectID),
		ReceiptID: normalizedReceiptID,
	})
	if err != nil {
		return reviewPlanState{}, workInternalError("list_work_items", err)
	}
	state.TaskCount = len(items)
	state.PlanStatus = derivePlanStatusFromWorkItems(items)
	if task, ok := findWorkItemByKey(items, reviewKey); ok {
		state.Task = &task
	}
	return state, nil
}

func (s *Service) updateReviewSnapshot(ctx context.Context, payload v1.ReviewPayload, state reviewPlanState, summary string, status v1.WorkItemStatus, outcome, attemptRef string) (v1.WorkResult, *core.APIError) {
	evidence := mergeReviewTaskEvidence(state.Task, nil, attemptRef)
	return s.Work(ctx, v1.WorkPayload{
		ProjectID: payload.ProjectID,
		PlanKey:   payload.PlanKey,
		ReceiptID: payload.ReceiptID,
		Tasks: []v1.WorkTaskPayload{{
			Key:      payload.Key,
			Summary:  strings.TrimSpace(summary),
			Status:   status,
			Outcome:  strings.TrimSpace(outcome),
			Evidence: evidence,
		}},
	})
}

func (s *Service) fetchReviewReceiptScope(ctx context.Context, projectID, receiptID string) (core.ReceiptScope, *core.APIError) {
	scope, err := s.repo.FetchReceiptScope(ctx, core.ReceiptScopeQuery{
		ProjectID: strings.TrimSpace(projectID),
		ReceiptID: strings.TrimSpace(receiptID),
	})
	if err != nil {
		if errors.Is(err, core.ErrReceiptScopeNotFound) {
			return core.ReceiptScope{}, backendError(v1.ErrCodeNotFound, "receipt scope was not found", map[string]any{
				"project_id": strings.TrimSpace(projectID),
				"receipt_id": strings.TrimSpace(receiptID),
			})
		}
		return core.ReceiptScope{}, verifyInternalError("fetch_receipt_scope", err, "")
	}
	return scope, nil
}

func (s *Service) listReviewAttempts(ctx context.Context, projectID, receiptID, reviewKey string) ([]core.ReviewAttempt, *core.APIError) {
	attempts, err := s.repo.ListReviewAttempts(ctx, core.ReviewAttemptListQuery{
		ProjectID: strings.TrimSpace(projectID),
		ReceiptID: strings.TrimSpace(receiptID),
		ReviewKey: strings.TrimSpace(reviewKey),
	})
	if err != nil {
		return nil, backendError(v1.ErrCodeInternalError, "failed to list review attempts", map[string]any{
			"project_id": strings.TrimSpace(projectID),
			"receipt_id": strings.TrimSpace(receiptID),
			"review_key": strings.TrimSpace(reviewKey),
			"error":      err.Error(),
		})
	}
	return attempts, nil
}

type reviewFingerprintEntry struct {
	Path string
	Kind string
	Hash string
}

func computeReviewFingerprint(projectRoot, projectID, receiptID, reviewKey, workflowSourcePath string, command workflowRunDefinition, scope core.ReceiptScope, plan *core.WorkPlan) (string, *core.APIError) {
	hasher := sha256.New()
	writeFingerprintPart(hasher, "awm.review.fingerprint.v2")
	writeFingerprintPart(hasher, strings.TrimSpace(projectID))
	writeFingerprintPart(hasher, strings.TrimSpace(receiptID))
	writeFingerprintPart(hasher, strings.TrimSpace(reviewKey))
	writeFingerprintPart(hasher, strings.TrimSpace(workflowSourcePath))
	writeFingerprintPart(hasher, strings.TrimSpace(command.CWD))
	writeFingerprintPart(hasher, strconv.Itoa(command.TimeoutSec))

	for _, value := range command.Argv {
		writeFingerprintPart(hasher, strings.TrimSpace(value))
	}
	envKeys := make([]string, 0, len(command.Env))
	for key := range command.Env {
		envKeys = append(envKeys, key)
	}
	sort.Strings(envKeys)
	for _, key := range envKeys {
		writeFingerprintPart(hasher, key)
		writeFingerprintPart(hasher, command.Env[key])
	}
	runnerHashes, apiErr := workflowRunnerFingerprints(projectRoot, command)
	if apiErr != nil {
		return "", apiErr
	}
	for _, runnerHash := range runnerHashes {
		writeFingerprintPart(hasher, runnerHash)
	}

	entries, apiErr := collectReviewFingerprintEntries(projectRoot, effectiveScopePaths(scope, plan))
	if apiErr != nil {
		return "", apiErr
	}
	for _, entry := range entries {
		writeFingerprintPart(hasher, entry.Path)
		writeFingerprintPart(hasher, entry.Kind)
		if entry.Hash != "" {
			writeFingerprintPart(hasher, entry.Hash)
		}
	}

	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

func workflowRunnerFingerprints(projectRoot string, command workflowRunDefinition) ([]string, *core.APIError) {
	if len(command.Argv) == 0 {
		return nil, nil
	}
	baseDir := strings.TrimSpace(projectRoot)
	if cwd := strings.TrimSpace(command.CWD); cwd != "" {
		baseDir = filepath.Join(baseDir, filepath.FromSlash(cwd))
	}

	seen := map[string]struct{}{}
	hashes := make([]string, 0, len(command.Argv))
	for _, rawArg := range command.Argv {
		resolved, ok := resolveWorkflowArgumentFile(baseDir, rawArg)
		if !ok {
			continue
		}
		if _, exists := seen[resolved]; exists {
			continue
		}
		seen[resolved] = struct{}{}
		blob, err := os.ReadFile(resolved)
		switch {
		case errors.Is(err, os.ErrNotExist):
			continue
		case err != nil:
			return nil, backendError(v1.ErrCodeInternalError, "failed to read workflow runner file", map[string]any{
				"path":  rawArg,
				"error": err.Error(),
			})
		}
		sum := sha256.Sum256(blob)
		hashes = append(hashes, hex.EncodeToString(sum[:]))
	}
	sort.Strings(hashes)
	return hashes, nil
}

func resolveWorkflowArgumentFile(baseDir, raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", false
	}
	if !strings.Contains(value, "/") && !strings.Contains(value, string(filepath.Separator)) && !filepath.IsAbs(value) {
		return "", false
	}

	resolved := value
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(baseDir, filepath.FromSlash(resolved))
	}
	info, err := os.Stat(resolved)
	switch {
	case err != nil:
		return "", false
	case info.IsDir():
		return "", false
	default:
		return filepath.Clean(resolved), true
	}
}

func collectReviewFingerprintEntries(projectRoot string, scopePaths []string) ([]reviewFingerprintEntry, *core.APIError) {
	entries := make(map[string]reviewFingerprintEntry)
	orderedScope := append([]string(nil), scopePaths...)
	sort.Strings(orderedScope)
	for _, relativePath := range orderedScope {
		absolutePath := filepath.Join(projectRoot, filepath.FromSlash(relativePath))
		info, err := os.Stat(absolutePath)
		switch {
		case errors.Is(err, os.ErrNotExist):
			entries[relativePath] = reviewFingerprintEntry{Path: relativePath, Kind: "missing"}
			continue
		case err != nil:
			return nil, backendError(v1.ErrCodeInternalError, "failed to stat scoped review file", map[string]any{
				"path":  relativePath,
				"error": err.Error(),
			})
		case info.IsDir():
			dirEntries, apiErr := collectReviewDirectoryFingerprintEntries(projectRoot, relativePath)
			if apiErr != nil {
				return nil, apiErr
			}
			if len(dirEntries) == 0 {
				entries[relativePath] = reviewFingerprintEntry{Path: relativePath, Kind: "dir-empty"}
				continue
			}
			for _, entry := range dirEntries {
				entries[entry.Path] = entry
			}
			continue
		}

		blob, err := os.ReadFile(absolutePath)
		if err != nil {
			return nil, backendError(v1.ErrCodeInternalError, "failed to read scoped review file", map[string]any{
				"path":  relativePath,
				"error": err.Error(),
			})
		}
		sum := sha256.Sum256(blob)
		entries[relativePath] = reviewFingerprintEntry{Path: relativePath, Kind: "file", Hash: hex.EncodeToString(sum[:])}
	}

	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]reviewFingerprintEntry, 0, len(keys))
	for _, key := range keys {
		out = append(out, entries[key])
	}
	return out, nil
}

func collectReviewDirectoryFingerprintEntries(projectRoot, rootRelative string) ([]reviewFingerprintEntry, *core.APIError) {
	rootAbsolute := filepath.Join(projectRoot, filepath.FromSlash(rootRelative))
	entries := make([]reviewFingerprintEntry, 0)
	err := func() error {
		dirRoot, openErr := os.OpenRoot(rootAbsolute)
		if openErr != nil {
			return openErr
		}
		defer dirRoot.Close()
		rootFS := dirRoot.FS()
		return fs.WalkDir(rootFS, ".", func(current string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if current == "." {
					return nil
				}
				if entry.Name() == ".git" {
					return fs.SkipDir
				}
				return nil
			}
			if !entry.Type().IsRegular() {
				return nil
			}

			relativePath := filepath.ToSlash(filepath.Join(filepath.FromSlash(rootRelative), filepath.FromSlash(current)))
			normalizedPath := normalizeCompletionPath(relativePath)
			if normalizedPath == "" {
				return nil
			}
			blob, readErr := fs.ReadFile(rootFS, current)
			if readErr != nil {
				return readErr
			}
			sum := sha256.Sum256(blob)
			entries = append(entries, reviewFingerprintEntry{Path: normalizedPath, Kind: "file", Hash: hex.EncodeToString(sum[:])})
			return nil
		})
	}()
	if err != nil {
		return nil, backendError(v1.ErrCodeInternalError, "failed to walk scoped review directory", map[string]any{
			"path":  rootRelative,
			"error": err.Error(),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// writeFingerprintPart appends one normalized value plus a NUL separator to
// the fingerprint hash. hash.Hash.Write is documented to never return an
// error, so the returns are deliberately not checked (errcheck's
// std-error-handling exclusions agree).
func writeFingerprintPart(h hash.Hash, value string) {
	h.Write([]byte(strings.TrimSpace(value)))
	h.Write([]byte{0})
}

func findWorkItemByKey(items []core.WorkItem, reviewKey string) (core.WorkItem, bool) {
	normalizedKey := strings.TrimSpace(reviewKey)
	for _, item := range items {
		if strings.TrimSpace(item.ItemKey) != normalizedKey {
			continue
		}
		return item, true
	}
	return core.WorkItem{}, false
}

func mergeReviewTaskEvidence(task *core.WorkItem, explicit []string, latestAttemptRef string) []string {
	out := make([]string, 0, len(explicit)+1)
	if task != nil {
		for _, value := range task.Evidence {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" || strings.HasPrefix(trimmed, reviewAttemptEvidencePrefix) {
				continue
			}
			out = append(out, trimmed)
		}
	}
	for _, value := range explicit {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if trimmed := strings.TrimSpace(latestAttemptRef); trimmed != "" {
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func reviewAttemptRef(attemptID int64) string {
	if attemptID <= 0 {
		return ""
	}
	return reviewAttemptEvidencePrefix + strconv.FormatInt(attemptID, 10)
}

func latestReviewAttemptRef(attempts []core.ReviewAttempt) string {
	latest, ok := latestReviewAttempt(attempts)
	if !ok {
		return ""
	}
	return reviewAttemptRef(latest.AttemptID)
}

func latestReviewAttempt(attempts []core.ReviewAttempt) (core.ReviewAttempt, bool) {
	if len(attempts) == 0 {
		return core.ReviewAttempt{}, false
	}
	return attempts[len(attempts)-1], true
}

func latestReviewAttemptByFingerprint(attempts []core.ReviewAttempt, fingerprint string) (core.ReviewAttempt, bool) {
	needle := strings.TrimSpace(fingerprint)
	for _, attempt := range slices.Backward(attempts) {
		if strings.TrimSpace(attempt.Fingerprint) == needle {
			return attempt, true
		}
	}
	return core.ReviewAttempt{}, false
}

func reviewAttemptEligibleForFingerprintSkip(attempt core.ReviewAttempt) bool {
	return attempt.Passed
}

func countPassingReviewAttempts(attempts []core.ReviewAttempt) int {
	total := 0
	for _, attempt := range attempts {
		if attempt.Passed {
			total++
		}
	}
	return total
}

func reviewStatusFromAttempt(attempt core.ReviewAttempt) v1.WorkItemStatus {
	if attempt.Passed {
		return v1.WorkItemStatusComplete
	}
	return v1.WorkItemStatusBlocked
}

func reviewAttemptPassed(run verifyCommandRun) bool {
	return run.Err == nil || (run.ExitCode != nil && *run.ExitCode == 0)
}

func reviewAttemptStorageStatus(run verifyCommandRun) string {
	if reviewAttemptPassed(run) {
		return "passed"
	}
	return "failed"
}

func summarizeReviewOutcome(run verifyCommandRun, passed bool, attemptsRun, maxAttempts, passingRuns int) string {
	base := summarizeReviewCommandRun(run)
	if maxAttempts > 0 {
		if passed {
			return excerptVerifyOutput(fmt.Sprintf("Review gate passed (%d/%d attempts, %d passing run(s)): %s", attemptsRun, maxAttempts, passingRuns, base))
		}
		return excerptVerifyOutput(fmt.Sprintf("Review gate blocked (%d/%d attempts, %d passing run(s)): %s", attemptsRun, maxAttempts, passingRuns, base))
	}
	if passed {
		return excerptVerifyOutput(fmt.Sprintf("Review gate passed (%d passing run(s)): %s", passingRuns, base))
	}
	return excerptVerifyOutput(base)
}

func summarizeReviewCommandRun(run verifyCommandRun) string {
	if run.TimedOut {
		base := "Review gate timed out"
		if stderr := excerptVerifyOutput(run.Stderr); stderr != "" {
			return excerptVerifyOutput(base + ": " + stderr)
		}
		return base
	}

	if reviewAttemptPassed(run) {
		if stdout := excerptVerifyOutput(run.Stdout); stdout != "" {
			return stdout
		}
		if stderr := excerptVerifyOutput(run.Stderr); stderr != "" {
			return stderr
		}
		return "Review gate passed"
	}

	base := "Review gate failed"
	if run.ExitCode != nil {
		base = fmt.Sprintf("Review gate failed with exit code %d", *run.ExitCode)
	}
	if stdout := excerptVerifyOutput(run.Stdout); stdout != "" {
		return excerptVerifyOutput(base + ": " + stdout)
	}
	if stderr := excerptVerifyOutput(run.Stderr); stderr != "" {
		return excerptVerifyOutput(base + ": " + stderr)
	}
	if run.Err != nil {
		return excerptVerifyOutput(base + ": " + run.Err.Error())
	}
	return base
}

func reviewExecutionFromRun(sourcePath string, command workflowRunDefinition, run verifyCommandRun) *v1.ReviewExecution {
	execution := &v1.ReviewExecution{
		SourcePath:    strings.TrimSpace(sourcePath),
		CommandArgv:   append([]string(nil), command.Argv...),
		CommandCWD:    command.CWD,
		TimeoutSec:    command.TimeoutSec,
		TimedOut:      run.TimedOut,
		StdoutExcerpt: excerptVerifyOutput(run.Stdout),
		StderrExcerpt: excerptVerifyOutput(run.Stderr),
	}
	if run.ExitCode != nil {
		exitCode := *run.ExitCode
		execution.ExitCode = &exitCode
	}
	return execution
}

func workflowDefinitionsAPIError(sourcePath string, err error) *core.APIError {
	if errors.Is(err, os.ErrNotExist) {
		return backendError(v1.ErrCodeNotFound, "workflow definitions file was not found", map[string]any{"workflow_source_path": sourcePath})
	}
	return backendError(v1.ErrCodeInvalidInput, "workflow definitions are invalid", map[string]any{
		"workflow_source_path": sourcePath,
		"error":                err.Error(),
	})
}
