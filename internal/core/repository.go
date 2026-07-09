package core

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrReceiptScopeNotFound is returned when no receipt scope matches the query.
	ErrReceiptScopeNotFound = errors.New("receipt scope not found")
	// ErrFetchLookupNotFound is returned when no fetch state matches the query.
	ErrFetchLookupNotFound = errors.New("fetch lookup not found")
	// ErrPointerLookupNotFound is returned when no pointer matches the lookup key.
	ErrPointerLookupNotFound = errors.New("pointer lookup not found")
	// ErrWorkPlanNotFound is returned when no work plan matches the query.
	ErrWorkPlanNotFound = errors.New("work plan not found")
)

const (
	// WorkItemStatusPending marks a work item that has not been started.
	WorkItemStatusPending = "pending"
	// WorkItemStatusInProgress marks a work item that is actively being worked.
	WorkItemStatusInProgress = "in_progress"
	// WorkItemStatusBlocked marks a work item that cannot proceed.
	WorkItemStatusBlocked = "blocked"
	// WorkItemStatusComplete marks a work item that is finished.
	WorkItemStatusComplete = "complete"
	// WorkItemStatusSuperseded marks a work item replaced by newer work.
	WorkItemStatusSuperseded = "superseded"

	// PlanStatusPending marks a work plan that has not been started.
	PlanStatusPending = "pending"
	// PlanStatusInProgress marks a work plan that is actively being worked.
	PlanStatusInProgress = "in_progress"
	// PlanStatusBlocked marks a work plan that cannot proceed.
	PlanStatusBlocked = "blocked"
	// PlanStatusComplete marks a work plan that is finished.
	PlanStatusComplete = "complete"
	// PlanStatusSuperseded marks a work plan replaced by a newer plan.
	PlanStatusSuperseded = "superseded"
)

// StaleFilter controls whether stale pointers are included in a query and,
// optionally, the cutoff time before which pointers count as stale.
type StaleFilter struct {
	AllowStale  bool
	StaleBefore *time.Time
}

// CandidatePointerQuery describes the filters used to fetch candidate
// pointers for a project, including an optional result limit.
type CandidatePointerQuery struct {
	ProjectID   string
	Limit       int
	Unbounded   bool
	StaleFilter StaleFilter
}

// CandidatePointer is a context pointer candidate returned from the
// repository, describing a documented path or rule and its staleness.
type CandidatePointer struct {
	Key         string
	Path        string
	Anchor      string
	Kind        string
	Label       string
	Description string
	Tags        []string
	IsRule      bool
	IsStale     bool
	UpdatedAt   time.Time
}

// PointerInventory reports a tracked pointer path and whether it is stale.
type PointerInventory struct {
	Path    string
	IsStale bool
}

// PointerStub is a minimal pointer record used to seed new pointer entries
// via UpsertPointerStubs.
type PointerStub struct {
	PointerKey  string
	Path        string
	Kind        string
	Label       string
	Description string
	Tags        []string
}

// VCSInfo optionally records the version-control state observed when a run
// was recorded: the HEAD commit and branch. The zero value means unrecorded.
type VCSInfo struct {
	Sha    string
	Branch string
}

// Actor optionally identifies the agent behind a persisted action: the
// harness (e.g. claude-code, codex, opencode), the model, and a
// harness-scoped session identifier. The zero value means unattributed.
type Actor struct {
	Harness   string
	Model     string
	SessionID string
}

// RunReceiptSummary captures the outcome of a single run for receipt
// persistence, including resolved scope and definition-of-done issues.
type RunReceiptSummary struct {
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
	Actor                  Actor
	VCS                    VCSInfo
}

// RunReceiptIDs identifies a persisted run receipt by its run ID and
// receipt ID.
type RunReceiptIDs struct {
	RunID     int64
	ReceiptID string
}

// ReceiptScopeQuery identifies a receipt scope by project and receipt ID.
type ReceiptScopeQuery struct {
	ProjectID string
	ReceiptID string
}

// ReceiptScope is the persisted scope for a receipt: its task, resolved
// tags, pointer keys, initial paths, and any captured baseline paths.
type ReceiptScope struct {
	ProjectID         string
	ReceiptID         string
	TaskText          string
	Phase             string
	ResolvedTags      []string
	PointerKeys       []string
	InitialScopePaths []string
	BaselineCaptured  bool
	BaselinePaths     []SyncPath
	Actor             Actor
}

// FetchLookupQuery identifies the fetch state for a receipt within a project.
type FetchLookupQuery struct {
	ProjectID string
	ReceiptID string
}

// PointerLookupQuery identifies a single pointer by project and pointer key.
type PointerLookupQuery struct {
	ProjectID  string
	PointerKey string
}

// WorkItem is a single trackable task within a work plan, including its
// status, dependencies, acceptance criteria, and outcome evidence.
type WorkItem struct {
	ItemKey            string
	Summary            string
	Status             string
	ParentTaskKey      string
	ParentTaskKeyClear bool // explicitly clear ParentTaskKey during merge
	DependsOn          []string
	AcceptanceCriteria []string
	References         []string
	ExternalRefs       []string
	BlockedReason      string
	Outcome            string
	Evidence           []string
	UpdatedAt          time.Time
}

// FetchLookup is the persisted fetch state for a receipt, including run and
// plan status plus the receipt's work items.
type FetchLookup struct {
	ProjectID  string
	ReceiptID  string
	RunID      int64
	RunStatus  string
	PlanStatus string
	WorkItems  []WorkItem
	UpdatedAt  time.Time
}

// SyncPath describes one path in a sync operation together with its content
// hash and whether the path was deleted.
type SyncPath struct {
	Path        string
	ContentHash string
	Deleted     bool
}

// SyncApplyInput describes a sync operation to apply over a set of paths,
// optionally inserting new candidate pointers for unknown paths.
type SyncApplyInput struct {
	ProjectID           string
	Mode                string
	InsertNewCandidates bool
	Paths               []SyncPath
}

// SyncApplyResult reports the counts of pointers affected by a sync apply.
type SyncApplyResult struct {
	Updated            int
	MarkedStale        int
	NewCandidates      int
	DeletedMarkedStale int
}

// RulePointer is a rule-backed pointer parsed from a rules source file,
// carrying the rule's content and enforcement level.
type RulePointer struct {
	PointerKey  string
	SourcePath  string
	RuleID      string
	Summary     string
	Content     string
	Enforcement string
	Tags        []string
}

// RulePointerSyncInput describes the rule pointers to sync for a single
// source path within a project.
type RulePointerSyncInput struct {
	ProjectID  string
	SourcePath string
	Pointers   []RulePointer
}

// RulePointerSyncResult reports the counts of rule pointers upserted and
// marked stale by a rule pointer sync.
type RulePointerSyncResult struct {
	Upserted    int
	MarkedStale int
}

// WorkItemsUpsertInput describes the work items to upsert for a receipt.
type WorkItemsUpsertInput struct {
	ProjectID string
	ReceiptID string
	Items     []WorkItem
}

// WorkPlanMode selects how a work plan upsert combines with existing state.
type WorkPlanMode string

const (
	// WorkPlanModeMerge merges the incoming plan fields into the existing plan.
	WorkPlanModeMerge WorkPlanMode = "merge"
	// WorkPlanModeReplace replaces the existing plan with the incoming plan.
	WorkPlanModeReplace WorkPlanMode = "replace"
)

// WorkPlanStages holds the staged planning documents for a work plan, from
// spec outline through implementation plan.
type WorkPlanStages struct {
	SpecOutline        string
	RefinedSpec        string
	ImplementationPlan string
}

// WorkPlan is a persisted plan with its objective, scope, constraints,
// references, and tasks.
type WorkPlan struct {
	ProjectID       string
	PlanKey         string
	ReceiptID       string
	Title           string
	Objective       string
	Kind            string
	ParentPlanKey   string
	Status          string
	Stages          WorkPlanStages
	InScope         []string
	OutOfScope      []string
	DiscoveredPaths []string
	Constraints     []string
	References      []string
	ExternalRefs    []string
	Tasks           []WorkItem
	UpdatedAt       time.Time
}

// WorkPlanUpsertInput describes a work plan create-or-update request,
// including the merge/replace mode to apply.
type WorkPlanUpsertInput struct {
	ProjectID       string
	PlanKey         string
	ReceiptID       string
	Mode            WorkPlanMode
	Title           string
	Objective       string
	Kind            string
	ParentPlanKey   string
	Status          string
	Stages          WorkPlanStages
	InScope         []string
	OutOfScope      []string
	DiscoveredPaths []string
	Constraints     []string
	References      []string
	ExternalRefs    []string
	Tasks           []WorkItem
}

// WorkPlanUpsertResult reports the stored plan and the number of records
// updated by the upsert.
type WorkPlanUpsertResult struct {
	Plan    WorkPlan
	Updated int
}

// WorkPlanLookupQuery identifies a work plan by project plus plan key or
// receipt ID.
type WorkPlanLookupQuery struct {
	ProjectID string
	PlanKey   string
	ReceiptID string
}

// WorkPlanListQuery describes the filters used to list work plans,
// including an optional result limit.
type WorkPlanListQuery struct {
	ProjectID string
	Scope     string
	Query     string
	Kind      string
	Limit     int
	Unbounded bool
}

// WorkPlanSummary is a condensed view of a work plan with per-status task
// counts and active task keys.
type WorkPlanSummary struct {
	ReceiptID           string
	Title               string
	Objective           string
	PlanKey             string
	Summary             string
	Status              string
	Kind                string
	ParentPlanKey       string
	ActiveTaskKeys      []string
	TaskCountTotal      int
	TaskCountPending    int
	TaskCountInProgress int
	TaskCountBlocked    int
	TaskCountComplete   int
	UpdatedAt           time.Time
}

// ReceiptHistoryListQuery describes the filters used to list receipt
// history, including an optional result limit.
type ReceiptHistoryListQuery struct {
	ProjectID string
	Query     string
	Limit     int
	Unbounded bool
}

// ReceiptHistorySummary is a condensed view of a receipt's latest recorded
// state.
type ReceiptHistorySummary struct {
	ReceiptID       string
	TaskText        string
	Phase           string
	LatestRequestID string
	LatestStatus    string
	UpdatedAt       time.Time
}

// RunHistoryListQuery describes the filters used to list run history,
// including an optional result limit.
type RunHistoryListQuery struct {
	ProjectID string
	Query     string
	Limit     int
	Unbounded bool
}

// RunHistoryLookupQuery identifies a single run history entry by project
// and run ID.
type RunHistoryLookupQuery struct {
	ProjectID string
	RunID     int64
}

// RunHistorySummary is a condensed view of a single persisted run.
type RunHistorySummary struct {
	RunID        int64
	ReceiptID    string
	RequestID    string
	TaskText     string
	Phase        string
	Status       string
	FilesChanged []string
	Outcome      string
	UpdatedAt    time.Time
	Actor        Actor
	VCS          VCSInfo
}

// ReviewAttempt records one execution of a review command, including the
// command invocation, exit state, and captured output excerpts.
type ReviewAttempt struct {
	AttemptID          int64
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
	Actor              Actor
}

// ReviewAttemptListQuery describes the filters used to list review attempts
// for a receipt and review key.
type ReviewAttemptListQuery struct {
	ProjectID string
	ReceiptID string
	ReviewKey string
}

// VerificationBatch records one batch of executable verification test runs
// and its overall pass/fail status.
type VerificationBatch struct {
	BatchRunID      string
	ProjectID       string
	ReceiptID       string
	PlanKey         string
	Phase           string
	TestsSourcePath string
	Status          string
	Passed          bool
	SelectedTestIDs []string
	Results         []VerificationTestRun
	CreatedAt       time.Time
	Actor           Actor
}

// VerificationTestRun records one test execution within a verification
// batch, including the command, exit state, and captured output excerpts.
type VerificationTestRun struct {
	BatchRunID       string
	ProjectID        string
	TestID           string
	DefinitionHash   string
	Summary          string
	CommandArgv      []string
	CommandCWD       string
	TimeoutSec       int
	ExpectedExitCode int
	SelectionReasons []string
	Status           string
	ExitCode         *int
	DurationMS       int
	StdoutExcerpt    string
	StderrExcerpt    string
	StartedAt        time.Time
	FinishedAt       time.Time
}

// Repository is the persistence contract required by core services: pointer
// inventory, receipt scopes, run receipts, review attempts, work items, and
// sync operations.
type Repository interface {
	FetchCandidatePointers(context.Context, CandidatePointerQuery) ([]CandidatePointer, error)
	ListPointerInventory(context.Context, string) ([]PointerInventory, error)
	UpsertPointerStubs(context.Context, string, []PointerStub) (int, error)
	UpsertReceiptScope(context.Context, ReceiptScope) error
	FetchReceiptScope(context.Context, ReceiptScopeQuery) (ReceiptScope, error)
	LookupFetchState(context.Context, FetchLookupQuery) (FetchLookup, error)
	LookupPointerByKey(context.Context, PointerLookupQuery) (CandidatePointer, error)
	SaveRunReceiptSummary(context.Context, RunReceiptSummary) (RunReceiptIDs, error)
	SaveReviewAttempt(context.Context, ReviewAttempt) (int64, error)
	ListReviewAttempts(context.Context, ReviewAttemptListQuery) ([]ReviewAttempt, error)
	UpsertWorkItems(context.Context, WorkItemsUpsertInput) (int, error)
	ListWorkItems(context.Context, FetchLookupQuery) ([]WorkItem, error)
	ApplySync(context.Context, SyncApplyInput) (SyncApplyResult, error)
	SyncRulePointers(context.Context, RulePointerSyncInput) (RulePointerSyncResult, error)
}

// WorkPlanRepository is an optional extension for richer plan/task persistence.
// Implementations may be asserted from Repository by services that support plan-aware workflows.
type WorkPlanRepository interface {
	UpsertWorkPlan(context.Context, WorkPlanUpsertInput) (WorkPlanUpsertResult, error)
	LookupWorkPlan(context.Context, WorkPlanLookupQuery) (WorkPlan, error)
	ListWorkPlans(context.Context, WorkPlanListQuery) ([]WorkPlanSummary, error)
}

// HistoryRepository is an optional extension for receipt and run history
// queries. Implementations may be asserted from Repository.
type HistoryRepository interface {
	ListReceiptHistory(context.Context, ReceiptHistoryListQuery) ([]ReceiptHistorySummary, error)
	ListRunHistory(context.Context, RunHistoryListQuery) ([]RunHistorySummary, error)
	LookupRunHistory(context.Context, RunHistoryLookupQuery) (RunHistorySummary, error)
}

// VerificationRepository is an optional extension for durable executable verification storage.
type VerificationRepository interface {
	SaveVerificationBatch(context.Context, VerificationBatch) error
}
