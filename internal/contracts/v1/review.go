package v1

import "strings"

const (
	// DefaultReviewTaskKey is the review task key assumed when a review payload
	// does not name one.
	DefaultReviewTaskKey = "review:cross-llm"
	defaultReviewSummary = "Cross-LLM review"
)

// ReviewPayload is the input for the review command: it identifies the target
// receipt or plan, the review task key, and the status, outcome, and evidence
// to record; Run requests execution of a runnable review gate.
type ReviewPayload struct {
	ProjectID     string         `json:"project_id"`
	ReceiptID     string         `json:"receipt_id,omitempty"`
	PlanKey       string         `json:"plan_key,omitempty"`
	Key           string         `json:"key,omitempty"`
	Summary       string         `json:"summary,omitempty"`
	Status        WorkItemStatus `json:"status,omitempty"`
	Outcome       string         `json:"outcome,omitempty"`
	BlockedReason string         `json:"blocked_reason,omitempty"`
	Evidence      []string       `json:"evidence,omitempty"`
	Run           bool           `json:"run,omitempty"`
	TagsFile      string         `json:"tags_file,omitempty"`
	Actor         *ActorRef      `json:"actor,omitempty"`
}

// ReviewExecution describes one executed review gate command: the workflow
// source, argv, working directory, timeout, exit outcome, and output excerpts.
type ReviewExecution struct {
	SourcePath    string   `json:"source_path,omitempty"`
	CommandArgv   []string `json:"command_argv,omitempty"`
	CommandCWD    string   `json:"command_cwd,omitempty"`
	TimeoutSec    int      `json:"timeout_sec,omitempty"`
	ExitCode      *int     `json:"exit_code,omitempty"`
	TimedOut      bool     `json:"timed_out,omitempty"`
	StdoutExcerpt string   `json:"stdout_excerpt,omitempty"`
	StderrExcerpt string   `json:"stderr_excerpt,omitempty"`
}

// ReviewResult is the outcome of the review command: the updated plan state,
// the recorded review key and status, attempt counters, and execution details
// when a runnable gate was executed.
type ReviewResult struct {
	PlanKey       string           `json:"plan_key"`
	PlanStatus    string           `json:"plan_status"`
	Updated       int              `json:"updated"`
	TaskCount     int              `json:"task_count,omitempty"`
	ReviewKey     string           `json:"review_key"`
	ReviewStatus  WorkItemStatus   `json:"review_status"`
	AttemptsRun   int              `json:"attempts_run,omitempty"`
	MaxAttempts   int              `json:"max_attempts,omitempty"`
	PassingRuns   int              `json:"passing_runs,omitempty"`
	Fingerprint   string           `json:"fingerprint,omitempty"`
	SkippedReason string           `json:"skipped_reason,omitempty"`
	Executed      bool             `json:"executed,omitempty"`
	Execution     *ReviewExecution `json:"execution,omitempty"`
}

// NormalizeReviewPayload trims all string fields and applies defaults: Key
// falls back to DefaultReviewTaskKey, Status to blocked when a blocked reason
// is present and complete otherwise, and Summary to a key-derived default.
// Evidence entries are trimmed with empty values dropped.
func NormalizeReviewPayload(p ReviewPayload) ReviewPayload {
	normalized := ReviewPayload{
		ProjectID:     strings.TrimSpace(p.ProjectID),
		ReceiptID:     strings.TrimSpace(p.ReceiptID),
		PlanKey:       strings.TrimSpace(p.PlanKey),
		Key:           strings.TrimSpace(p.Key),
		Summary:       strings.TrimSpace(p.Summary),
		Status:        WorkItemStatus(strings.TrimSpace(string(p.Status))),
		Outcome:       strings.TrimSpace(p.Outcome),
		BlockedReason: strings.TrimSpace(p.BlockedReason),
		Run:           p.Run,
		TagsFile:      strings.TrimSpace(p.TagsFile),
		Actor:         p.Actor,
	}
	if normalized.Key == "" {
		normalized.Key = DefaultReviewTaskKey
	}
	if normalized.Status == "" {
		if normalized.BlockedReason != "" {
			normalized.Status = WorkItemStatusBlocked
		} else {
			normalized.Status = WorkItemStatusComplete
		}
	}
	if normalized.Summary == "" {
		normalized.Summary = defaultReviewSummaryForKey(normalized.Key)
	}
	if len(p.Evidence) > 0 {
		normalized.Evidence = make([]string, 0, len(p.Evidence))
		for _, value := range p.Evidence {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			normalized.Evidence = append(normalized.Evidence, trimmed)
		}
	}
	return normalized
}

// ReviewPayloadToWorkPayload converts a review payload into an equivalent
// merge-mode WorkPayload containing a single task for the normalized review
// key, so the review gate is recorded through the work tracker.
func ReviewPayloadToWorkPayload(p ReviewPayload) WorkPayload {
	normalized := NormalizeReviewPayload(p)
	return WorkPayload{
		ProjectID: normalized.ProjectID,
		ReceiptID: normalized.ReceiptID,
		PlanKey:   normalized.PlanKey,
		Mode:      WorkPlanModeMerge,
		Tasks: []WorkTaskPayload{
			{
				Key:           normalized.Key,
				Summary:       normalized.Summary,
				Status:        normalized.Status,
				Outcome:       normalized.Outcome,
				BlockedReason: normalized.BlockedReason,
				Evidence:      append([]string(nil), normalized.Evidence...),
			},
		},
	}
}

// ReviewResultFromWork builds a ReviewResult from the work-tracker outcome and
// review run metadata. An empty status falls back to the normalized payload
// status, fingerprint and skipped reason are trimmed, and Executed reflects
// whether execution details are present.
func ReviewResultFromWork(p ReviewPayload, work WorkResult, status WorkItemStatus, attemptsRun, maxAttempts, passingRuns int, fingerprint, skippedReason string, execution *ReviewExecution) ReviewResult {
	normalized := NormalizeReviewPayload(p)
	if status == "" {
		status = normalized.Status
	}
	return ReviewResult{
		PlanKey:       work.PlanKey,
		PlanStatus:    work.PlanStatus,
		Updated:       work.Updated,
		TaskCount:     work.TaskCount,
		ReviewKey:     normalized.Key,
		ReviewStatus:  status,
		AttemptsRun:   attemptsRun,
		MaxAttempts:   maxAttempts,
		PassingRuns:   passingRuns,
		Fingerprint:   strings.TrimSpace(fingerprint),
		SkippedReason: strings.TrimSpace(skippedReason),
		Executed:      execution != nil,
		Execution:     execution,
	}
}

func defaultReviewSummaryForKey(key string) string {
	if strings.TrimSpace(key) == DefaultReviewTaskKey {
		return defaultReviewSummary
	}
	return "Review gate: " + strings.TrimSpace(key)
}
