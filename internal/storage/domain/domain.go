package domain

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/core"
)

const (
	// WorkPlanListScopeCurrent selects only work plans that are actively in progress.
	WorkPlanListScopeCurrent = "current"
	// WorkPlanListScopeDeferred selects only work plans that have been deferred.
	WorkPlanListScopeDeferred = "deferred"
	// WorkPlanListScopeCompleted selects only work plans that have been completed.
	WorkPlanListScopeCompleted = "completed"
	// WorkPlanListScopeAll selects work plans regardless of state; it is also the
	// fallback scope when an unrecognized value is normalized.
	WorkPlanListScopeAll = "all"
	// DefaultPhase is the phase assigned when a raw phase value is empty or unrecognized.
	DefaultPhase = "execute"
)

// NormalizedRunSummary is a run receipt summary whose fields have been trimmed,
// deduplicated, and defaulted by NormalizeRunReceiptSummary, ready for storage.
type NormalizedRunSummary struct {
	ProjectID              string
	RequestID              string
	ReceiptID              string
	TaskText               string
	Phase                  string
	Status                 string
	ResolvedTags           []string
	PointerKeys            []string
	FilesChanged           []string
	DefinitionOfDoneIssues []string
	Outcome                string
	Actor                  core.Actor
}

// NormalizedReceiptScope is a receipt scope whose fields have been trimmed,
// deduplicated, and defaulted by NormalizeReceiptScope, ready for storage.
type NormalizedReceiptScope struct {
	ProjectID         string
	ReceiptID         string
	TaskText          string
	Phase             string
	ResolvedTags      []string
	PointerKeys       []string
	InitialScopePaths []string
	BaselineCaptured  bool
	BaselinePaths     []core.SyncPath
	Actor             core.Actor
}

// NormalizedVerificationBatch is a verification batch whose fields have been
// validated and normalized by NormalizeVerificationBatch, ready for storage.
type NormalizedVerificationBatch struct {
	BatchRunID      string
	ProjectID       string
	ReceiptID       string
	PlanKey         string
	Phase           string
	TestsSourcePath string
	Status          string
	Passed          bool
	SelectedTestIDs []string
	Results         []core.VerificationTestRun
	CreatedAt       time.Time
	Actor           core.Actor
}

// NormalizedReviewAttempt is a review attempt whose fields have been validated
// and normalized by NormalizeReviewAttempt, ready for storage.
type NormalizedReviewAttempt struct {
	ProjectID          string
	ReceiptID          string
	PlanKey            string
	ReviewKey          string
	Summary            string
	Fingerprint        string
	Status             string
	Passed             bool
	Outcome            string
	WorkflowSourcePath string
	CommandArgv        []string
	CommandCWD         string
	TimeoutSec         int
	ExitCode           *int
	TimedOut           bool
	StdoutExcerpt      string
	StderrExcerpt      string
	CreatedAt          time.Time
	Actor              core.Actor
}

// NormalizeWorkPlanMode maps a raw mode to WorkPlanModeReplace when it matches
// after trimming, and to WorkPlanModeMerge for every other value.
func NormalizeWorkPlanMode(raw core.WorkPlanMode) core.WorkPlanMode {
	switch strings.TrimSpace(string(raw)) {
	case string(core.WorkPlanModeReplace):
		return core.WorkPlanModeReplace
	default:
		return core.WorkPlanModeMerge
	}
}

// NormalizePhase maps a raw phase to one of "plan", "review", or "execute"
// (case-insensitive, trimmed), falling back to DefaultPhase for other values.
func NormalizePhase(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "plan":
		return "plan"
	case "review":
		return "review"
	case "execute":
		return "execute"
	default:
		return DefaultPhase
	}
}

// SortAndLimitCandidatePointers returns a copy of candidates sorted by trimmed
// path then key, truncated to input.Limit (or defaultLimit when Limit <= 0)
// unless input.Unbounded is set. It returns nil for an empty input slice.
func SortAndLimitCandidatePointers(candidates []core.CandidatePointer, input core.CandidatePointerQuery, defaultLimit int) []core.CandidatePointer {
	if len(candidates) == 0 {
		return nil
	}

	limit := input.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	sorted := append([]core.CandidatePointer(nil), candidates...)
	sort.Slice(sorted, func(i, j int) bool {
		leftPath := strings.TrimSpace(sorted[i].Path)
		rightPath := strings.TrimSpace(sorted[j].Path)
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		return sorted[i].Key < sorted[j].Key
	})
	if !input.Unbounded && len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted
}

// CandidateTagOverlap counts the distinct values that also appear in targets.
// Duplicate matches in values are counted once; empty inputs yield 0.
func CandidateTagOverlap(values, targets []string) int {
	if len(values) == 0 || len(targets) == 0 {
		return 0
	}
	targetSet := make(map[string]struct{}, len(targets))
	for _, value := range targets {
		targetSet[value] = struct{}{}
	}
	count := 0
	seen := map[string]struct{}{}
	for _, value := range values {
		if _, ok := targetSet[value]; !ok {
			continue
		}
		if _, dupe := seen[value]; dupe {
			continue
		}
		seen[value] = struct{}{}
		count++
	}
	return count
}

// NormalizeWorkPlanListScope maps a raw scope to one of the WorkPlanListScope*
// constants after trimming, falling back to WorkPlanListScopeAll.
func NormalizeWorkPlanListScope(raw string) string {
	switch strings.TrimSpace(raw) {
	case WorkPlanListScopeCurrent:
		return WorkPlanListScopeCurrent
	case WorkPlanListScopeDeferred:
		return WorkPlanListScopeDeferred
	case WorkPlanListScopeCompleted:
		return WorkPlanListScopeCompleted
	default:
		return WorkPlanListScopeAll
	}
}

// WorkPlanListSearchPattern converts a raw search term into a SQL LIKE pattern:
// lowercased, trimmed, with `\`, `%`, and `_` escaped, wrapped in `%...%`.
// It returns "" when the trimmed input is empty.
func WorkPlanListSearchPattern(raw string) string {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" {
		return ""
	}
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + replacer.Replace(trimmed) + "%"
}

// NormalizeWorkPlanStages normalizes each stage status via NormalizeWorkItemStatus,
// treating blank stages as WorkItemStatusPending.
func NormalizeWorkPlanStages(raw core.WorkPlanStages) core.WorkPlanStages {
	out := core.WorkPlanStages{
		SpecOutline:        NormalizeWorkItemStatus(raw.SpecOutline),
		RefinedSpec:        NormalizeWorkItemStatus(raw.RefinedSpec),
		ImplementationPlan: NormalizeWorkItemStatus(raw.ImplementationPlan),
	}
	if strings.TrimSpace(raw.SpecOutline) == "" {
		out.SpecOutline = core.WorkItemStatusPending
	}
	if strings.TrimSpace(raw.RefinedSpec) == "" {
		out.RefinedSpec = core.WorkItemStatusPending
	}
	if strings.TrimSpace(raw.ImplementationPlan) == "" {
		out.ImplementationPlan = core.WorkItemStatusPending
	}
	return out
}

// NormalizeWorkPlanTasks trims and normalizes each task, drops entries with an
// empty item key, deduplicates by key keeping the entry with the highest status
// priority (blocked > in_progress > pending > complete/superseded), and returns
// the result sorted by item key. It returns nil when nothing survives.
func NormalizeWorkPlanTasks(tasks []core.WorkItem) []core.WorkItem {
	if len(tasks) == 0 {
		return nil
	}

	priority := map[string]int{
		core.WorkItemStatusComplete:   0,
		core.WorkItemStatusSuperseded: 0,
		core.WorkItemStatusPending:    1,
		core.WorkItemStatusInProgress: 2,
		core.WorkItemStatusBlocked:    3,
	}
	byKey := make(map[string]core.WorkItem, len(tasks))
	for _, raw := range tasks {
		itemKey := strings.TrimSpace(raw.ItemKey)
		if itemKey == "" {
			continue
		}
		status := NormalizeWorkItemStatus(raw.Status)
		normalized := core.WorkItem{
			ItemKey:            itemKey,
			Summary:            strings.TrimSpace(raw.Summary),
			Status:             status,
			ParentTaskKey:      strings.TrimSpace(raw.ParentTaskKey),
			DependsOn:          NormalizeStringList(raw.DependsOn),
			AcceptanceCriteria: NormalizeStringList(raw.AcceptanceCriteria),
			References:         NormalizeStringList(raw.References),
			ExternalRefs:       NormalizeStringList(raw.ExternalRefs),
			BlockedReason:      strings.TrimSpace(raw.BlockedReason),
			Outcome:            strings.TrimSpace(raw.Outcome),
			Evidence:           NormalizeStringList(raw.Evidence),
			UpdatedAt:          raw.UpdatedAt.UTC(),
		}
		current, exists := byKey[itemKey]
		if !exists || priority[status] >= priority[current.Status] {
			byKey[itemKey] = normalized
		}
	}
	if len(byKey) == 0 {
		return nil
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]core.WorkItem, 0, len(keys))
	for _, key := range keys {
		out = append(out, byKey[key])
	}
	return out
}

// MergeIncomingWorkPlanTasks merges incoming tasks into current ones keyed by
// item key, sorted by key. In WorkPlanModeReplace it returns the normalized
// incoming set alone; in merge mode incoming fields overlay matching current
// tasks, and it returns nil when incoming is empty or has no valid keys.
func MergeIncomingWorkPlanTasks(current, incoming []core.WorkItem, mode core.WorkPlanMode) []core.WorkItem {
	if mode == core.WorkPlanModeReplace {
		return NormalizeWorkPlanTasks(incoming)
	}
	if len(incoming) == 0 {
		return nil
	}

	currentByKey := make(map[string]core.WorkItem, len(current))
	for _, item := range NormalizeWorkPlanTasks(current) {
		currentByKey[item.ItemKey] = item
	}

	mergedByKey := make(map[string]core.WorkItem, len(incoming))
	for _, raw := range incoming {
		itemKey := strings.TrimSpace(raw.ItemKey)
		if itemKey == "" {
			continue
		}
		base, found := currentByKey[itemKey]
		if prior, ok := mergedByKey[itemKey]; ok {
			base = prior
			found = true
		}
		mergedByKey[itemKey] = mergeWorkPlanTask(base, raw, found)
	}
	if len(mergedByKey) == 0 {
		return nil
	}

	keys := make([]string, 0, len(mergedByKey))
	for key := range mergedByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]core.WorkItem, 0, len(keys))
	for _, key := range keys {
		out = append(out, mergedByKey[key])
	}
	return out
}

func mergeWorkPlanTask(current, incoming core.WorkItem, found bool) core.WorkItem {
	itemKey := strings.TrimSpace(incoming.ItemKey)
	merged := core.WorkItem{}
	if found {
		merged = current
	}
	merged.ItemKey = itemKey

	if summary := strings.TrimSpace(incoming.Summary); summary != "" || !found {
		merged.Summary = summary
	}

	if status := strings.TrimSpace(incoming.Status); status != "" {
		merged.Status = NormalizeWorkItemStatus(status)
	} else if !found || strings.TrimSpace(merged.Status) == "" {
		merged.Status = core.WorkItemStatusPending
	} else {
		merged.Status = NormalizeWorkItemStatus(merged.Status)
	}

	if incoming.ParentTaskKeyClear {
		merged.ParentTaskKey = ""
	} else if parent := strings.TrimSpace(incoming.ParentTaskKey); parent != "" {
		merged.ParentTaskKey = parent
	}
	if incoming.DependsOn != nil {
		merged.DependsOn = NormalizeStringList(incoming.DependsOn)
	}
	if incoming.AcceptanceCriteria != nil {
		merged.AcceptanceCriteria = NormalizeStringList(incoming.AcceptanceCriteria)
	}
	if incoming.References != nil {
		merged.References = NormalizeStringList(incoming.References)
	}
	if incoming.ExternalRefs != nil {
		merged.ExternalRefs = NormalizeStringList(incoming.ExternalRefs)
	}

	if blocked := strings.TrimSpace(incoming.BlockedReason); blocked != "" {
		merged.BlockedReason = blocked
	} else if strings.TrimSpace(incoming.Status) != "" && NormalizeWorkItemStatus(incoming.Status) != core.WorkItemStatusBlocked {
		merged.BlockedReason = ""
	}

	if outcome := strings.TrimSpace(incoming.Outcome); outcome != "" {
		merged.Outcome = outcome
	}
	if incoming.Evidence != nil {
		merged.Evidence = NormalizeStringList(incoming.Evidence)
	}

	if !incoming.UpdatedAt.IsZero() {
		merged.UpdatedAt = incoming.UpdatedAt.UTC()
	} else if found {
		merged.UpdatedAt = current.UpdatedAt.UTC()
	}

	return merged
}

// BuildNextWorkPlanState computes the next persisted state of a work plan from
// the current state (used when found is true) and the upsert input. In merge
// mode only non-empty input fields overwrite current values; in
// WorkPlanModeReplace the plan is rebuilt from the input with pending defaults.
// Project ID, plan key, status, and stages are always normalized on the result.
func BuildNextWorkPlanState(current core.WorkPlan, found bool, input core.WorkPlanUpsertInput, mode core.WorkPlanMode) core.WorkPlan {
	projectID := strings.TrimSpace(input.ProjectID)
	planKey := strings.TrimSpace(input.PlanKey)
	next := core.WorkPlan{
		ProjectID: projectID,
		PlanKey:   planKey,
		Status:    core.PlanStatusPending,
		Stages: core.WorkPlanStages{
			SpecOutline:        core.PlanStatusPending,
			RefinedSpec:        core.PlanStatusPending,
			ImplementationPlan: core.PlanStatusPending,
		},
	}
	if found {
		next = current
	}
	if mode == core.WorkPlanModeReplace {
		next = core.WorkPlan{
			ProjectID: projectID,
			PlanKey:   planKey,
			Status:    core.PlanStatusPending,
			Stages: core.WorkPlanStages{
				SpecOutline:        core.PlanStatusPending,
				RefinedSpec:        core.PlanStatusPending,
				ImplementationPlan: core.PlanStatusPending,
			},
		}
	}

	trimmedReceipt := strings.TrimSpace(input.ReceiptID)
	if trimmedReceipt != "" || mode == core.WorkPlanModeReplace {
		next.ReceiptID = trimmedReceipt
	}

	trimmedTitle := strings.TrimSpace(input.Title)
	if trimmedTitle != "" || mode == core.WorkPlanModeReplace {
		next.Title = trimmedTitle
	}

	trimmedObjective := strings.TrimSpace(input.Objective)
	if trimmedObjective != "" || mode == core.WorkPlanModeReplace {
		next.Objective = trimmedObjective
	}

	trimmedKind := strings.TrimSpace(input.Kind)
	if trimmedKind != "" || mode == core.WorkPlanModeReplace {
		next.Kind = strings.ToLower(trimmedKind)
	}

	trimmedParentPlanKey := strings.TrimSpace(input.ParentPlanKey)
	if trimmedParentPlanKey != "" || mode == core.WorkPlanModeReplace {
		next.ParentPlanKey = trimmedParentPlanKey
	}

	trimmedStatus := strings.TrimSpace(input.Status)
	if trimmedStatus != "" {
		next.Status = NormalizeWorkItemStatus(trimmedStatus)
	}
	if mode == core.WorkPlanModeReplace && trimmedStatus == "" {
		next.Status = core.PlanStatusPending
	}

	if input.Stages.SpecOutline != "" || input.Stages.RefinedSpec != "" || input.Stages.ImplementationPlan != "" || mode == core.WorkPlanModeReplace {
		if mode == core.WorkPlanModeReplace {
			next.Stages = core.WorkPlanStages{}
		}
		next.Stages = MergeWorkPlanStages(next.Stages, input.Stages, mode)
	}
	next.Stages = NormalizeWorkPlanStages(next.Stages)

	if input.InScope != nil || mode == core.WorkPlanModeReplace {
		next.InScope = NormalizeStringList(input.InScope)
	}
	if input.OutOfScope != nil || mode == core.WorkPlanModeReplace {
		next.OutOfScope = NormalizeStringList(input.OutOfScope)
	}
	if input.DiscoveredPaths != nil || mode == core.WorkPlanModeReplace {
		next.DiscoveredPaths = NormalizeRepoPathList(input.DiscoveredPaths)
	}
	if input.Constraints != nil || mode == core.WorkPlanModeReplace {
		next.Constraints = NormalizeStringList(input.Constraints)
	}
	if input.References != nil || mode == core.WorkPlanModeReplace {
		next.References = NormalizeStringList(input.References)
	}
	if input.ExternalRefs != nil || mode == core.WorkPlanModeReplace {
		next.ExternalRefs = NormalizeStringList(input.ExternalRefs)
	}

	next.ProjectID = projectID
	next.PlanKey = planKey
	next.Status = NormalizeWorkItemStatus(next.Status)
	return next
}

// MergeWorkPlanStages overlays non-blank incoming stage statuses onto current,
// normalizing each applied value. In WorkPlanModeReplace all stages are taken
// from incoming, replacing the current values entirely.
func MergeWorkPlanStages(current, incoming core.WorkPlanStages, mode core.WorkPlanMode) core.WorkPlanStages {
	out := current
	if mode == core.WorkPlanModeReplace {
		out = core.WorkPlanStages{}
	}
	if strings.TrimSpace(incoming.SpecOutline) != "" || mode == core.WorkPlanModeReplace {
		out.SpecOutline = NormalizeWorkItemStatus(incoming.SpecOutline)
	}
	if strings.TrimSpace(incoming.RefinedSpec) != "" || mode == core.WorkPlanModeReplace {
		out.RefinedSpec = NormalizeWorkItemStatus(incoming.RefinedSpec)
	}
	if strings.TrimSpace(incoming.ImplementationPlan) != "" || mode == core.WorkPlanModeReplace {
		out.ImplementationPlan = NormalizeWorkItemStatus(incoming.ImplementationPlan)
	}
	return out
}

// NormalizeActor trims the optional actor identity fields; a zero actor stays
// zero so unattributed records persist exactly as before.
func NormalizeActor(input core.Actor) core.Actor {
	return core.Actor{
		Harness:   strings.TrimSpace(input.Harness),
		Model:     strings.TrimSpace(input.Model),
		SessionID: strings.TrimSpace(input.SessionID),
	}
}

// NormalizeRunReceiptSummary trims and defaults a run receipt summary: status
// defaults to "accepted" and phase to "execute" when blank, and list fields are
// deduplicated and sorted. It returns an error when project_id is empty.
func NormalizeRunReceiptSummary(input core.RunReceiptSummary) (NormalizedRunSummary, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return NormalizedRunSummary{}, errors.New("project_id is required")
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "accepted"
	}
	phase := strings.TrimSpace(input.Phase)
	if phase == "" {
		phase = "execute"
	}
	return NormalizedRunSummary{
		ProjectID:              projectID,
		RequestID:              strings.TrimSpace(input.RequestID),
		ReceiptID:              strings.TrimSpace(input.ReceiptID),
		TaskText:               strings.TrimSpace(input.TaskText),
		Phase:                  phase,
		Status:                 status,
		ResolvedTags:           NormalizeStringList(input.ResolvedTags),
		PointerKeys:            NormalizeStringList(input.PointerKeys),
		FilesChanged:           NormalizeStringList(input.FilesChanged),
		DefinitionOfDoneIssues: NormalizeStringList(input.DefinitionOfDoneIssues),
		Outcome:                strings.TrimSpace(input.Outcome),
		Actor:                  NormalizeActor(input.Actor),
	}, nil
}

// NormalizeReceiptScope trims and defaults a receipt scope: phase defaults to
// "execute" when blank, and tag, pointer, and path lists are normalized. It
// returns an error when project_id or receipt_id is empty.
func NormalizeReceiptScope(input core.ReceiptScope) (NormalizedReceiptScope, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return NormalizedReceiptScope{}, errors.New("project_id is required")
	}
	receiptID := strings.TrimSpace(input.ReceiptID)
	if receiptID == "" {
		return NormalizedReceiptScope{}, errors.New("receipt_id is required")
	}
	phase := strings.TrimSpace(input.Phase)
	if phase == "" {
		phase = "execute"
	}
	return NormalizedReceiptScope{
		ProjectID:         projectID,
		ReceiptID:         receiptID,
		TaskText:          strings.TrimSpace(input.TaskText),
		Phase:             phase,
		ResolvedTags:      NormalizeStringList(input.ResolvedTags),
		PointerKeys:       NormalizeStringList(input.PointerKeys),
		InitialScopePaths: NormalizeRepoPathList(input.InitialScopePaths),
		BaselineCaptured:  input.BaselineCaptured,
		BaselinePaths:     NormalizeSyncPathList(input.BaselinePaths),
		Actor:             NormalizeActor(input.Actor),
	}, nil
}

// NormalizeVerificationBatch validates and normalizes a verification batch.
// It requires project_id and batch_run_id, a status of passed|failed, and for
// each result a test_id, definition_hash, a recognized status, timeout_sec > 0,
// and expected_exit_code in 0..255. Zero timestamps default to the current time
// and finished_at is clamped to be no earlier than started_at.
func NormalizeVerificationBatch(input core.VerificationBatch) (NormalizedVerificationBatch, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return NormalizedVerificationBatch{}, errors.New("project_id is required")
	}
	batchRunID := strings.TrimSpace(input.BatchRunID)
	if batchRunID == "" {
		return NormalizedVerificationBatch{}, errors.New("batch_run_id is required")
	}
	status := strings.TrimSpace(input.Status)
	if status != "passed" && status != "failed" {
		return NormalizedVerificationBatch{}, errors.New("status must be passed|failed")
	}

	results := make([]core.VerificationTestRun, 0, len(input.Results))
	for i, raw := range input.Results {
		testID := strings.TrimSpace(raw.TestID)
		if testID == "" {
			return NormalizedVerificationBatch{}, fmt.Errorf("results[%d].test_id is required", i)
		}
		definitionHash := strings.TrimSpace(raw.DefinitionHash)
		if definitionHash == "" {
			return NormalizedVerificationBatch{}, fmt.Errorf("results[%d].definition_hash is required", i)
		}
		resultStatus := strings.TrimSpace(raw.Status)
		switch resultStatus {
		case "passed", "failed", "timed_out", "errored", "skipped":
		default:
			return NormalizedVerificationBatch{}, fmt.Errorf("results[%d].status is invalid", i)
		}
		timeoutSec := raw.TimeoutSec
		if timeoutSec <= 0 {
			return NormalizedVerificationBatch{}, fmt.Errorf("results[%d].timeout_sec must be > 0", i)
		}
		expectedExitCode := raw.ExpectedExitCode
		if expectedExitCode < 0 || expectedExitCode > 255 {
			return NormalizedVerificationBatch{}, fmt.Errorf("results[%d].expected_exit_code must be 0..255", i)
		}
		durationMS := max(raw.DurationMS, 0)
		startedAt := raw.StartedAt.UTC()
		if startedAt.IsZero() {
			startedAt = time.Now().UTC()
		}
		finishedAt := raw.FinishedAt.UTC()
		if finishedAt.IsZero() || finishedAt.Before(startedAt) {
			finishedAt = startedAt
		}
		results = append(results, core.VerificationTestRun{
			BatchRunID:       batchRunID,
			ProjectID:        projectID,
			TestID:           testID,
			DefinitionHash:   definitionHash,
			Summary:          strings.TrimSpace(raw.Summary),
			CommandArgv:      NormalizeStringListPreserveOrder(raw.CommandArgv),
			CommandCWD:       strings.TrimSpace(raw.CommandCWD),
			TimeoutSec:       timeoutSec,
			ExpectedExitCode: expectedExitCode,
			SelectionReasons: NormalizeStringListPreserveOrder(raw.SelectionReasons),
			Status:           resultStatus,
			ExitCode:         raw.ExitCode,
			DurationMS:       durationMS,
			StdoutExcerpt:    strings.TrimSpace(raw.StdoutExcerpt),
			StderrExcerpt:    strings.TrimSpace(raw.StderrExcerpt),
			StartedAt:        startedAt,
			FinishedAt:       finishedAt,
		})
	}

	createdAt := input.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	return NormalizedVerificationBatch{
		BatchRunID:      batchRunID,
		ProjectID:       projectID,
		ReceiptID:       strings.TrimSpace(input.ReceiptID),
		PlanKey:         strings.TrimSpace(input.PlanKey),
		Phase:           strings.TrimSpace(input.Phase),
		TestsSourcePath: strings.TrimSpace(input.TestsSourcePath),
		Status:          status,
		Passed:          input.Passed,
		SelectedTestIDs: NormalizeStringListPreserveOrder(input.SelectedTestIDs),
		Results:         results,
		CreatedAt:       createdAt,
		Actor:           NormalizeActor(input.Actor),
	}, nil
}

// NormalizeReviewAttempt validates and normalizes a review attempt. It requires
// project_id, receipt_id, review_key, and fingerprint, a status of
// passed|failed, timeout_sec > 0, and an exit code in 0..255 when provided.
// A zero created_at defaults to the current UTC time.
func NormalizeReviewAttempt(input core.ReviewAttempt) (NormalizedReviewAttempt, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return NormalizedReviewAttempt{}, errors.New("project_id is required")
	}
	receiptID := strings.TrimSpace(input.ReceiptID)
	if receiptID == "" {
		return NormalizedReviewAttempt{}, errors.New("receipt_id is required")
	}
	reviewKey := strings.TrimSpace(input.ReviewKey)
	if reviewKey == "" {
		return NormalizedReviewAttempt{}, errors.New("review_key is required")
	}
	fingerprint := strings.TrimSpace(input.Fingerprint)
	if fingerprint == "" {
		return NormalizedReviewAttempt{}, errors.New("fingerprint is required")
	}
	status := strings.TrimSpace(input.Status)
	if status != "passed" && status != "failed" {
		return NormalizedReviewAttempt{}, errors.New("status must be passed|failed")
	}
	timeoutSec := input.TimeoutSec
	if timeoutSec <= 0 {
		return NormalizedReviewAttempt{}, errors.New("timeout_sec must be > 0")
	}
	if input.ExitCode != nil {
		exitCode := *input.ExitCode
		if exitCode < 0 || exitCode > 255 {
			return NormalizedReviewAttempt{}, errors.New("exit_code must be 0..255 when provided")
		}
	}
	createdAt := input.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	return NormalizedReviewAttempt{
		ProjectID:          projectID,
		ReceiptID:          receiptID,
		PlanKey:            strings.TrimSpace(input.PlanKey),
		ReviewKey:          reviewKey,
		Summary:            strings.TrimSpace(input.Summary),
		Fingerprint:        fingerprint,
		Status:             status,
		Passed:             input.Passed,
		Outcome:            strings.TrimSpace(input.Outcome),
		WorkflowSourcePath: strings.TrimSpace(input.WorkflowSourcePath),
		CommandArgv:        NormalizeStringListPreserveOrder(input.CommandArgv),
		CommandCWD:         strings.TrimSpace(input.CommandCWD),
		TimeoutSec:         timeoutSec,
		ExitCode:           input.ExitCode,
		TimedOut:           input.TimedOut,
		StdoutExcerpt:      strings.TrimSpace(input.StdoutExcerpt),
		StderrExcerpt:      strings.TrimSpace(input.StderrExcerpt),
		CreatedAt:          createdAt,
		Actor:              NormalizeActor(input.Actor),
	}, nil
}

// NormalizeSyncApplyInput validates and normalizes a sync apply request. It
// requires project_id, defaults the mode to "changed", and rejects modes other
// than changed|full|working_tree. Paths are normalized, deduplicated (deletions
// do not override non-deleted entries), and sorted; non-deleted paths must
// carry a content hash.
func NormalizeSyncApplyInput(input core.SyncApplyInput) (core.SyncApplyInput, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return core.SyncApplyInput{}, errors.New("project_id is required")
	}

	mode := strings.TrimSpace(input.Mode)
	if mode == "" {
		mode = "changed"
	}
	if mode != "changed" && mode != "full" && mode != "working_tree" {
		return core.SyncApplyInput{}, errors.New("mode must be changed|full|working_tree")
	}

	pathByKey := make(map[string]core.SyncPath, len(input.Paths))
	for _, raw := range input.Paths {
		pathKey := NormalizeRepoPath(raw.Path)
		if pathKey == "" {
			continue
		}
		if raw.Deleted {
			current, ok := pathByKey[pathKey]
			if ok && !current.Deleted {
				continue
			}
			pathByKey[pathKey] = core.SyncPath{Path: pathKey, Deleted: true}
			continue
		}

		contentHash := strings.TrimSpace(raw.ContentHash)
		if contentHash == "" {
			return core.SyncApplyInput{}, fmt.Errorf("content_hash is required for path %q", pathKey)
		}
		pathByKey[pathKey] = core.SyncPath{
			Path:        pathKey,
			ContentHash: contentHash,
		}
	}

	keys := make([]string, 0, len(pathByKey))
	for key := range pathByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	paths := make([]core.SyncPath, 0, len(keys))
	for _, key := range keys {
		paths = append(paths, pathByKey[key])
	}
	return core.SyncApplyInput{
		ProjectID:           projectID,
		Mode:                mode,
		InsertNewCandidates: input.InsertNewCandidates,
		Paths:               paths,
	}, nil
}

// NormalizeWorkItems normalizes item keys as repo paths and statuses, then
// deduplicates by key keeping the highest-priority status (blocked >
// in_progress > pending > complete) and returns items sorted by key. It errors
// when any item key normalizes to empty and returns nil for empty input.
func NormalizeWorkItems(items []core.WorkItem) ([]core.WorkItem, error) {
	if len(items) == 0 {
		return nil, nil
	}

	priority := map[string]int{
		core.WorkItemStatusComplete:   0,
		core.WorkItemStatusPending:    1,
		core.WorkItemStatusInProgress: 2,
		core.WorkItemStatusBlocked:    3,
	}

	byKey := make(map[string]core.WorkItem, len(items))
	for _, raw := range items {
		itemKey := NormalizeRepoPath(raw.ItemKey)
		if itemKey == "" {
			return nil, errors.New("work item key is required")
		}
		status := NormalizeWorkItemStatus(raw.Status)

		current, exists := byKey[itemKey]
		if !exists || priority[status] >= priority[current.Status] {
			byKey[itemKey] = core.WorkItem{ItemKey: itemKey, Status: status}
		}
	}

	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]core.WorkItem, 0, len(keys))
	for _, key := range keys {
		out = append(out, byKey[key])
	}

	return out, nil
}

// DerivePlanStatus computes an aggregate plan status from item statuses with
// precedence blocked > in_progress > pending > complete > superseded, and
// returns PlanStatusPending when items is empty.
func DerivePlanStatus(items []core.WorkItem) string {
	if len(items) == 0 {
		return core.PlanStatusPending
	}

	hasPending := false
	hasInProgress := false
	hasBlocked := false
	hasCompleted := false
	hasSuperseded := false

	for _, item := range items {
		switch NormalizeWorkItemStatus(item.Status) {
		case core.WorkItemStatusBlocked:
			hasBlocked = true
		case core.WorkItemStatusInProgress:
			hasInProgress = true
		case core.WorkItemStatusComplete:
			hasCompleted = true
		case core.WorkItemStatusSuperseded:
			hasSuperseded = true
		default:
			hasPending = true
		}
	}

	switch {
	case hasBlocked:
		return core.PlanStatusBlocked
	case hasInProgress:
		return core.PlanStatusInProgress
	case hasPending:
		return core.PlanStatusPending
	case hasCompleted:
		return core.PlanStatusComplete
	case hasSuperseded:
		return core.PlanStatusSuperseded
	default:
		return core.PlanStatusPending
	}
}

// StorageWorkItemStatus returns the storage representation of a work item
// status; it is an alias for NormalizeWorkItemStatus.
func StorageWorkItemStatus(raw string) string {
	return NormalizeWorkItemStatus(raw)
}

// NormalizeWorkItemStatus maps a raw status (case-insensitive, trimmed;
// "completed" is accepted for complete) to a canonical core.WorkItemStatus*
// value, falling back to WorkItemStatusPending.
func NormalizeWorkItemStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case core.WorkItemStatusComplete, "completed":
		return core.WorkItemStatusComplete
	case core.WorkItemStatusSuperseded:
		return core.WorkItemStatusSuperseded
	case core.WorkItemStatusInProgress:
		return core.WorkItemStatusInProgress
	case core.WorkItemStatusBlocked:
		return core.WorkItemStatusBlocked
	default:
		return core.WorkItemStatusPending
	}
}

// NormalizeRepoPath trims a repository path, converts backslashes to forward
// slashes, and cleans it. It returns "" for empty input or paths that clean
// to ".".
func NormalizeRepoPath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	withSlashes := strings.ReplaceAll(trimmed, `\`, "/")
	cleaned := path.Clean(withSlashes)
	if cleaned == "." {
		return ""
	}
	return cleaned
}

// NormalizeRepoPathList normalizes each entry via NormalizeRepoPath, drops
// empties, deduplicates, and returns the result sorted. It returns nil for
// empty input.
func NormalizeRepoPathList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		normalized := NormalizeRepoPath(raw)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

// NormalizeSyncPathList normalizes sync paths, deduplicates by normalized path
// (last entry wins), clears content hashes on deleted entries, and returns the
// result sorted by path. It returns nil when nothing survives.
func NormalizeSyncPathList(values []core.SyncPath) []core.SyncPath {
	if len(values) == 0 {
		return nil
	}

	byPath := make(map[string]core.SyncPath, len(values))
	for _, raw := range values {
		path := NormalizeRepoPath(raw.Path)
		if path == "" {
			continue
		}
		entry := core.SyncPath{
			Path:        path,
			ContentHash: strings.TrimSpace(raw.ContentHash),
			Deleted:     raw.Deleted,
		}
		if entry.Deleted {
			entry.ContentHash = ""
		}
		byPath[path] = entry
	}
	if len(byPath) == 0 {
		return nil
	}

	keys := make([]string, 0, len(byPath))
	for key := range byPath {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]core.SyncPath, 0, len(keys))
	for _, key := range keys {
		out = append(out, byPath[key])
	}
	return out
}

// NormalizeStringList trims each value, drops empties, deduplicates, and
// returns the result sorted. It returns nil for empty input.
func NormalizeStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

// NormalizeStringListPreserveOrder trims each value and drops empties while
// keeping the original order and any duplicates. It returns nil when nothing
// survives.
func NormalizeStringListPreserveOrder(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
