package v1

import "encoding/json"

const Version = "awm.v1"

type Command string

const (
	CommandContext       Command = "context"
	CommandFetch         Command = "fetch"
	CommandExport        Command = "export"
	CommandDone          Command = "done"
	CommandReview        Command = "review"
	CommandWork          Command = "work"
	CommandHistorySearch Command = "history"
	CommandSync          Command = "sync"
	CommandHealth        Command = "health"
	CommandStatus        Command = "status"
	CommandVerify        Command = "verify"
	CommandInit          Command = "init"
)

type Phase string

const (
	PhasePlan    Phase = "plan"
	PhaseExecute Phase = "execute"
	PhaseReview  Phase = "review"
)

type ScopeMode string

const (
	ScopeModeStrict ScopeMode = "strict"
	ScopeModeWarn   ScopeMode = "warn"
)

type CommandEnvelope struct {
	Version   string          `json:"version"`
	Command   Command         `json:"command"`
	RequestID string          `json:"request_id"`
	Payload   json.RawMessage `json:"payload"`
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Source  string `json:"source,omitempty"`
	Details any    `json:"details,omitempty"`
}

type ResultEnvelope struct {
	Version   string        `json:"version"`
	Command   Command       `json:"command"`
	RequestID string        `json:"request_id"`
	OK        bool          `json:"ok"`
	Timestamp string        `json:"timestamp"`
	Result    any           `json:"result,omitempty"`
	Error     *ErrorPayload `json:"error,omitempty"`
}

type ContextPayload struct {
	ProjectID         string   `json:"project_id"`
	TaskText          string   `json:"task_text"`
	Phase             Phase    `json:"phase"`
	TagsFile          string   `json:"tags_file,omitempty"`
	InitialScopePaths []string `json:"initial_scope_paths,omitempty"`
}

type FetchPayload struct {
	ProjectID        string            `json:"project_id"`
	Keys             []string          `json:"keys,omitempty"`
	ReceiptID        string            `json:"receipt_id,omitempty"`
	ExpectedVersions map[string]string `json:"expected_versions,omitempty"`
}

type ExportFormat string

const (
	ExportFormatJSON     ExportFormat = "json"
	ExportFormatMarkdown ExportFormat = "markdown"
)

type ExportContextSelector struct {
	TaskText          string   `json:"task_text"`
	Phase             Phase    `json:"phase"`
	TagsFile          string   `json:"tags_file,omitempty"`
	InitialScopePaths []string `json:"initial_scope_paths,omitempty"`
}

type ExportFetchSelector struct {
	Keys             []string          `json:"keys,omitempty"`
	ReceiptID        string            `json:"receipt_id,omitempty"`
	ExpectedVersions map[string]string `json:"expected_versions,omitempty"`
}

type ExportHistorySelector struct {
	Entity    HistoryEntity `json:"entity,omitempty"`
	Query     string        `json:"query,omitempty"`
	Scope     HistoryScope  `json:"scope,omitempty"`
	Kind      string        `json:"kind,omitempty"`
	Limit     int           `json:"limit,omitempty"`
	Unbounded *bool         `json:"unbounded,omitempty"`
}

type ExportStatusSelector struct {
	ProjectRoot   string `json:"project_root,omitempty"`
	RulesFile     string `json:"rules_file,omitempty"`
	TagsFile      string `json:"tags_file,omitempty"`
	TestsFile     string `json:"tests_file,omitempty"`
	WorkflowsFile string `json:"workflows_file,omitempty"`
	TaskText      string `json:"task_text,omitempty"`
	Phase         Phase  `json:"phase,omitempty"`
}

type ExportPayload struct {
	ProjectID string                 `json:"project_id"`
	Format    ExportFormat           `json:"format"`
	Context   *ExportContextSelector `json:"context,omitempty"`
	Fetch     *ExportFetchSelector   `json:"fetch,omitempty"`
	History   *ExportHistorySelector `json:"history,omitempty"`
	Status    *ExportStatusSelector  `json:"status,omitempty"`
}

type DonePayload struct {
	ProjectID     string    `json:"project_id"`
	ReceiptID     string    `json:"receipt_id,omitempty"`
	PlanKey       string    `json:"plan_key,omitempty"`
	TagsFile      string    `json:"tags_file,omitempty"`
	FilesChanged  []string  `json:"files_changed,omitempty"`
	NoFileChanges bool      `json:"no_file_changes,omitempty"`
	Outcome       string    `json:"outcome"`
	ScopeMode     ScopeMode `json:"scope_mode,omitempty"`
}

type WorkItemStatus string

const (
	WorkItemStatusPending    WorkItemStatus = "pending"
	WorkItemStatusInProgress WorkItemStatus = "in_progress"
	WorkItemStatusComplete   WorkItemStatus = "complete"
	WorkItemStatusBlocked    WorkItemStatus = "blocked"
	WorkItemStatusSuperseded WorkItemStatus = "superseded"
)

type WorkPlanMode string

const (
	WorkPlanModeMerge   WorkPlanMode = "merge"
	WorkPlanModeReplace WorkPlanMode = "replace"
)

type WorkPlanStagesPayload struct {
	SpecOutline        WorkItemStatus `json:"spec_outline,omitempty"`
	RefinedSpec        WorkItemStatus `json:"refined_spec,omitempty"`
	ImplementationPlan WorkItemStatus `json:"implementation_plan,omitempty"`
}

type WorkPlanPayload struct {
	Title           string                 `json:"title,omitempty"`
	Objective       string                 `json:"objective,omitempty"`
	Kind            string                 `json:"kind,omitempty"`
	ParentPlanKey   string                 `json:"parent_plan_key,omitempty"`
	Status          WorkItemStatus         `json:"status,omitempty"`
	Stages          *WorkPlanStagesPayload `json:"stages,omitempty"`
	InScope         []string               `json:"in_scope,omitempty"`
	OutOfScope      []string               `json:"out_of_scope,omitempty"`
	DiscoveredPaths []string               `json:"discovered_paths,omitempty"`
	Constraints     []string               `json:"constraints,omitempty"`
	References      []string               `json:"references,omitempty"`
	ExternalRefs    []string               `json:"external_refs,omitempty"`
}

type WorkTaskPayload struct {
	Key                string         `json:"key"`
	Summary            string         `json:"summary"`
	Status             WorkItemStatus `json:"status"`
	ParentTaskKey      *string        `json:"parent_task_key,omitempty"`
	DependsOn          []string       `json:"depends_on,omitempty"`
	AcceptanceCriteria []string       `json:"acceptance_criteria,omitempty"`
	References         []string       `json:"references,omitempty"`
	ExternalRefs       []string       `json:"external_refs,omitempty"`
	BlockedReason      string         `json:"blocked_reason,omitempty"`
	Outcome            string         `json:"outcome,omitempty"`
	Evidence           []string       `json:"evidence,omitempty"`
}

type WorkPayload struct {
	ProjectID string            `json:"project_id"`
	PlanKey   string            `json:"plan_key,omitempty"`
	PlanTitle string            `json:"plan_title,omitempty"`
	ReceiptID string            `json:"receipt_id,omitempty"`
	Mode      WorkPlanMode      `json:"mode,omitempty"`
	Plan      *WorkPlanPayload  `json:"plan,omitempty"`
	Tasks     []WorkTaskPayload `json:"tasks,omitempty"`
}

type HistoryScope string

const (
	HistoryScopeCurrent   HistoryScope = "current"
	HistoryScopeDeferred  HistoryScope = "deferred"
	HistoryScopeCompleted HistoryScope = "completed"
	HistoryScopeAll       HistoryScope = "all"
)

type HistoryEntity string

const (
	HistoryEntityAll     HistoryEntity = "all"
	HistoryEntityWork    HistoryEntity = "work"
	HistoryEntityReceipt HistoryEntity = "receipt"
	HistoryEntityRun     HistoryEntity = "run"
)

type HistorySearchPayload struct {
	ProjectID string        `json:"project_id"`
	Entity    HistoryEntity `json:"entity,omitempty"`
	Query     string        `json:"query,omitempty"`
	Scope     HistoryScope  `json:"scope,omitempty"`
	Kind      string        `json:"kind,omitempty"`
	Limit     int           `json:"limit,omitempty"`
	Unbounded *bool         `json:"unbounded,omitempty"`
}

type SyncPayload struct {
	ProjectID           string `json:"project_id"`
	Mode                string `json:"mode,omitempty"`
	GitRange            string `json:"git_range,omitempty"`
	ProjectRoot         string `json:"project_root,omitempty"`
	RulesFile           string `json:"rules_file,omitempty"`
	TagsFile            string `json:"tags_file,omitempty"`
	InsertNewCandidates *bool  `json:"insert_new_candidates,omitempty"`
}

type HealthFixer string

const (
	HealthFixerAll                HealthFixer = "all"
	HealthFixerSyncWorkingTree    HealthFixer = "sync_working_tree"
	HealthFixerIndexUnindexedFile HealthFixer = "index_unindexed_files"
	HealthFixerSyncRuleset        HealthFixer = "sync_ruleset"
)

type HealthPayload struct {
	ProjectID           string        `json:"project_id"`
	IncludeDetails      *bool         `json:"include_details,omitempty"`
	MaxFindingsPerCheck *int          `json:"max_findings_per_check,omitempty"`
	Apply               *bool         `json:"apply,omitempty"`
	ProjectRoot         string        `json:"project_root,omitempty"`
	RulesFile           string        `json:"rules_file,omitempty"`
	TagsFile            string        `json:"tags_file,omitempty"`
	Fixers              []HealthFixer `json:"fixers,omitempty"`
}

type StatusPayload struct {
	ProjectID     string `json:"project_id"`
	ProjectRoot   string `json:"project_root,omitempty"`
	RulesFile     string `json:"rules_file,omitempty"`
	TagsFile      string `json:"tags_file,omitempty"`
	TestsFile     string `json:"tests_file,omitempty"`
	WorkflowsFile string `json:"workflows_file,omitempty"`
	TaskText      string `json:"task_text,omitempty"`
	Phase         Phase  `json:"phase,omitempty"`
}

type VerifyPayload struct {
	ProjectID    string   `json:"project_id"`
	ReceiptID    string   `json:"receipt_id,omitempty"`
	PlanKey      string   `json:"plan_key,omitempty"`
	Phase        Phase    `json:"phase,omitempty"`
	TestIDs      []string `json:"test_ids,omitempty"`
	FilesChanged []string `json:"files_changed,omitempty"`
	TestsFile    string   `json:"tests_file,omitempty"`
	TagsFile     string   `json:"tags_file,omitempty"`
	DryRun       bool     `json:"dry_run,omitempty"`
}

type InitPayload struct {
	ProjectID            string   `json:"project_id"`
	ProjectRoot          string   `json:"project_root"`
	RulesFile            string   `json:"rules_file,omitempty"`
	TagsFile             string   `json:"tags_file,omitempty"`
	PersistCandidates    *bool    `json:"persist_candidates,omitempty"`
	RespectGitIgnore     *bool    `json:"respect_gitignore,omitempty"`
	OutputCandidatesPath *string  `json:"output_candidates_path,omitempty"`
	ApplyTemplates       []string `json:"apply_templates,omitempty"`
}

type ContextRule struct {
	RuleID      string `json:"rule_id"`
	Key         string `json:"key"`
	Summary     string `json:"summary"`
	Enforcement string `json:"enforcement"`
	Content     string `json:"content,omitempty"`
}

type ContextPlan struct {
	Key       string         `json:"key"`
	Summary   string         `json:"summary"`
	Status    WorkItemStatus `json:"status"`
	FetchKeys []string       `json:"fetch_keys,omitempty"`
}

type ContextPlanTaskCounts struct {
	Total      int `json:"total"`
	Pending    int `json:"pending"`
	InProgress int `json:"in_progress"`
	Blocked    int `json:"blocked"`
	Complete   int `json:"complete"`
}

type ContextReceiptMeta struct {
	ReceiptID        string   `json:"receipt_id"`
	ProjectID        string   `json:"project_id"`
	TaskText         string   `json:"task_text"`
	Phase            Phase    `json:"phase"`
	ResolvedTags     []string `json:"resolved_tags"`
	BaselineCaptured bool     `json:"baseline_captured"`
}

type ContextReceipt struct {
	Rules             []ContextRule      `json:"rules"`
	Plans             []ContextPlan      `json:"plans"`
	InitialScopePaths []string           `json:"initial_scope_paths,omitempty"`
	Meta              ContextReceiptMeta `json:"_meta"`
}

type ContextResult struct {
	Status  string          `json:"status"`
	Receipt *ContextReceipt `json:"receipt,omitempty"`
}

type FetchItem struct {
	Key     string `json:"key"`
	Type    string `json:"type"`
	Summary string `json:"summary"`
	Content string `json:"content,omitempty"`
	Status  string `json:"status,omitempty"`
	Version string `json:"version,omitempty"`
}

type FetchVersionMismatch struct {
	Key      string `json:"key"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

type FetchResult struct {
	Items             []FetchItem            `json:"items"`
	NotFound          []string               `json:"not_found,omitempty"`
	VersionMismatches []FetchVersionMismatch `json:"version_mismatches,omitempty"`
}

type ExportDocumentKind string

const (
	ExportDocumentKindContext     ExportDocumentKind = "context"
	ExportDocumentKindPlan        ExportDocumentKind = "plan"
	ExportDocumentKindReceipt     ExportDocumentKind = "receipt"
	ExportDocumentKindTask        ExportDocumentKind = "task"
	ExportDocumentKindRun         ExportDocumentKind = "run"
	ExportDocumentKindFetchBundle ExportDocumentKind = "fetch_bundle"
	ExportDocumentKindHistory     ExportDocumentKind = "history"
	ExportDocumentKindStatus      ExportDocumentKind = "status"
)

type ExportPlanStages struct {
	SpecOutline        WorkItemStatus `json:"spec_outline,omitempty"`
	RefinedSpec        WorkItemStatus `json:"refined_spec,omitempty"`
	ImplementationPlan WorkItemStatus `json:"implementation_plan,omitempty"`
}

type ExportTaskDocument struct {
	PlanKey            string         `json:"plan_key,omitempty"`
	Key                string         `json:"key"`
	Summary            string         `json:"summary"`
	Status             WorkItemStatus `json:"status"`
	ParentTaskKey      string         `json:"parent_task_key,omitempty"`
	DependsOn          []string       `json:"depends_on,omitempty"`
	AcceptanceCriteria []string       `json:"acceptance_criteria,omitempty"`
	References         []string       `json:"references,omitempty"`
	ExternalRefs       []string       `json:"external_refs,omitempty"`
	BlockedReason      string         `json:"blocked_reason,omitempty"`
	Outcome            string         `json:"outcome,omitempty"`
	Evidence           []string       `json:"evidence,omitempty"`
}

type ExportPlanDocument struct {
	PlanKey         string               `json:"plan_key"`
	ReceiptID       string               `json:"receipt_id,omitempty"`
	Title           string               `json:"title,omitempty"`
	Objective       string               `json:"objective,omitempty"`
	Kind            string               `json:"kind,omitempty"`
	ParentPlanKey   string               `json:"parent_plan_key,omitempty"`
	Status          WorkItemStatus       `json:"status"`
	Stages          *ExportPlanStages    `json:"stages,omitempty"`
	InScope         []string             `json:"in_scope,omitempty"`
	OutOfScope      []string             `json:"out_of_scope,omitempty"`
	DiscoveredPaths []string             `json:"discovered_paths,omitempty"`
	Constraints     []string             `json:"constraints,omitempty"`
	References      []string             `json:"references,omitempty"`
	ExternalRefs    []string             `json:"external_refs,omitempty"`
	Tasks           []ExportTaskDocument `json:"tasks"`
}

type ExportBaselinePath struct {
	Path        string `json:"path"`
	Deleted     bool   `json:"deleted"`
	ContentHash string `json:"content_hash,omitempty"`
}

type ExportReceiptRunDocument struct {
	RunID      int64                `json:"run_id"`
	Status     string               `json:"status,omitempty"`
	PlanStatus WorkItemStatus       `json:"plan_status,omitempty"`
	Tasks      []ExportTaskDocument `json:"tasks,omitempty"`
}

type ExportReceiptDocument struct {
	ReceiptID         string                    `json:"receipt_id"`
	TaskText          string                    `json:"task_text,omitempty"`
	Phase             Phase                     `json:"phase,omitempty"`
	ResolvedTags      []string                  `json:"resolved_tags,omitempty"`
	PointerKeys       []string                  `json:"pointer_keys,omitempty"`
	InitialScopePaths []string                  `json:"initial_scope_paths,omitempty"`
	BaselineCaptured  bool                      `json:"baseline_captured"`
	BaselinePaths     []ExportBaselinePath      `json:"baseline_paths,omitempty"`
	LatestRun         *ExportReceiptRunDocument `json:"latest_run,omitempty"`
}

type ExportRunDocument struct {
	RunID        int64    `json:"run_id"`
	ReceiptID    string   `json:"receipt_id,omitempty"`
	RequestID    string   `json:"request_id,omitempty"`
	TaskText     string   `json:"task_text,omitempty"`
	Phase        Phase    `json:"phase,omitempty"`
	Status       string   `json:"status,omitempty"`
	FilesChanged []string `json:"files_changed,omitempty"`
	Outcome      string   `json:"outcome,omitempty"`
	UpdatedAt    string   `json:"updated_at,omitempty"`
}

type ExportBundleItemKind string

const (
	ExportBundleItemKindPlan    ExportBundleItemKind = "plan"
	ExportBundleItemKindReceipt ExportBundleItemKind = "receipt"
	ExportBundleItemKindTask    ExportBundleItemKind = "task"
	ExportBundleItemKindRun     ExportBundleItemKind = "run"
	ExportBundleItemKindPointer ExportBundleItemKind = "pointer"
	ExportBundleItemKindRule    ExportBundleItemKind = "rule"
)

type ExportBundleItem struct {
	Kind    ExportBundleItemKind   `json:"kind"`
	Key     string                 `json:"key"`
	Type    string                 `json:"type"`
	Summary string                 `json:"summary"`
	Status  string                 `json:"status,omitempty"`
	Version string                 `json:"version,omitempty"`
	Plan    *ExportPlanDocument    `json:"plan,omitempty"`
	Receipt *ExportReceiptDocument `json:"receipt,omitempty"`
	Task    *ExportTaskDocument    `json:"task,omitempty"`
	Run     *ExportRunDocument     `json:"run,omitempty"`
	Content string                 `json:"content,omitempty"`
}

type ExportBundleDocument struct {
	RequestedKeys     []string               `json:"requested_keys,omitempty"`
	Items             []ExportBundleItem     `json:"items"`
	NotFound          []string               `json:"not_found,omitempty"`
	VersionMismatches []FetchVersionMismatch `json:"version_mismatches,omitempty"`
}

type ExportDocument struct {
	Kind    ExportDocumentKind     `json:"kind"`
	Title   string                 `json:"title,omitempty"`
	Summary string                 `json:"summary,omitempty"`
	Context *ContextReceipt        `json:"context,omitempty"`
	Plan    *ExportPlanDocument    `json:"plan,omitempty"`
	Receipt *ExportReceiptDocument `json:"receipt,omitempty"`
	Task    *ExportTaskDocument    `json:"task,omitempty"`
	Run     *ExportRunDocument     `json:"run,omitempty"`
	Bundle  *ExportBundleDocument  `json:"bundle,omitempty"`
	History *HistorySearchResult   `json:"history,omitempty"`
	Status  *StatusResult          `json:"status,omitempty"`
}

type ExportResult struct {
	Format   ExportFormat    `json:"format"`
	Document *ExportDocument `json:"document,omitempty"`
	Content  string          `json:"content"`
}

type CompletionViolation struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type DoneResult struct {
	Accepted               bool                  `json:"accepted"`
	Violations             []CompletionViolation `json:"violations"`
	DefinitionOfDoneIssues []string              `json:"definition_of_done_issues,omitempty"`
	RunID                  int                   `json:"run_id,omitempty"`
}

type WorkResult struct {
	PlanKey    string `json:"plan_key"`
	PlanStatus string `json:"plan_status"`
	Updated    int    `json:"updated"`
	TaskCount  int    `json:"task_count,omitempty"`
}

type HistoryItem struct {
	Key           string                 `json:"key"`
	Entity        HistoryEntity          `json:"entity"`
	Summary       string                 `json:"summary"`
	Status        string                 `json:"status,omitempty"`
	Scope         HistoryScope           `json:"scope,omitempty"`
	PlanKey       string                 `json:"plan_key,omitempty"`
	ReceiptID     string                 `json:"receipt_id,omitempty"`
	RunID         int64                  `json:"run_id,omitempty"`
	RequestID     string                 `json:"request_id,omitempty"`
	Phase         Phase                  `json:"phase,omitempty"`
	Kind          string                 `json:"kind,omitempty"`
	ParentPlanKey string                 `json:"parent_plan_key,omitempty"`
	TaskCounts    *ContextPlanTaskCounts `json:"task_counts,omitempty"`
	FetchKeys     []string               `json:"fetch_keys,omitempty"`
	UpdatedAt     string                 `json:"updated_at"`
}

type HistorySearchResult struct {
	Entity HistoryEntity `json:"entity"`
	Scope  HistoryScope  `json:"scope,omitempty"`
	Query  string        `json:"query,omitempty"`
	Limit  int           `json:"limit"`
	Count  int           `json:"count"`
	Items  []HistoryItem `json:"items"`
}

type SyncResult struct {
	Updated            int      `json:"updated"`
	MarkedStale        int      `json:"marked_stale"`
	NewCandidates      int      `json:"new_candidates"`
	IndexedStubs       int      `json:"indexed_stubs"`
	DeletedMarkedStale int      `json:"deleted_marked_stale"`
	ProcessedPaths     []string `json:"processed_paths,omitempty"`
}

type HealthSummary struct {
	OK            bool `json:"ok"`
	TotalFindings int  `json:"total_findings"`
}

type HealthCheckItem struct {
	Name     string   `json:"name"`
	Severity string   `json:"severity"`
	Count    int      `json:"count"`
	Samples  []string `json:"samples,omitempty"`
}

type HealthCheckResult struct {
	Summary HealthSummary     `json:"summary"`
	Checks  []HealthCheckItem `json:"checks"`
}

type HealthFixAction struct {
	Fixer HealthFixer `json:"fixer"`
	Count int         `json:"count"`
	Notes []string    `json:"notes,omitempty"`
}

type HealthFixResult struct {
	DryRun         bool              `json:"dry_run"`
	PlannedActions []HealthFixAction `json:"planned_actions"`
	AppliedActions []HealthFixAction `json:"applied_actions"`
	Summary        string            `json:"summary"`
}

type HealthResult struct {
	Mode  string             `json:"mode"`
	Check *HealthCheckResult `json:"check,omitempty"`
	Fix   *HealthFixResult   `json:"fix,omitempty"`
}

type StatusSummary struct {
	Ready        bool `json:"ready"`
	MissingCount int  `json:"missing_count"`
	WarningCount int  `json:"warning_count,omitempty"`
}

type StatusProject struct {
	ProjectID              string `json:"project_id"`
	ProjectRoot            string `json:"project_root"`
	DetectedRepoRoot       string `json:"detected_repo_root,omitempty"`
	Backend                string `json:"backend"`
	PostgresConfigured     bool   `json:"postgres_configured,omitempty"`
	SQLitePath             string `json:"sqlite_path,omitempty"`
	UsesImplicitSQLitePath bool   `json:"uses_implicit_sqlite_path,omitempty"`
	Unbounded              bool   `json:"unbounded"`
}

type StatusSource struct {
	Kind         string   `json:"kind"`
	SourcePath   string   `json:"source_path"`
	AbsolutePath string   `json:"absolute_path,omitempty"`
	Exists       bool     `json:"exists"`
	Loaded       bool     `json:"loaded"`
	ItemCount    int      `json:"item_count,omitempty"`
	Notes        []string `json:"notes,omitempty"`
}

type StatusIntegration struct {
	ID              string   `json:"id"`
	Summary         string   `json:"summary,omitempty"`
	Installed       bool     `json:"installed"`
	PresentTargets  int      `json:"present_targets"`
	ExpectedTargets int      `json:"expected_targets"`
	MissingTargets  []string `json:"missing_targets,omitempty"`
}

type StatusContextPreview struct {
	TaskText              string   `json:"task_text,omitempty"`
	Phase                 Phase    `json:"phase,omitempty"`
	Status                string   `json:"status"`
	ResolvedTags          []string `json:"resolved_tags,omitempty"`
	RuleCount             int      `json:"rule_count,omitempty"`
	PlanCount             int      `json:"plan_count,omitempty"`
	InitialScopePathCount int      `json:"initial_scope_path_count,omitempty"`
	Error                 string   `json:"error,omitempty"`
}

type StatusMissingItem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type StatusResult struct {
	Summary      StatusSummary         `json:"summary"`
	Project      StatusProject         `json:"project"`
	Sources      []StatusSource        `json:"sources"`
	Integrations []StatusIntegration   `json:"integrations"`
	Context      *StatusContextPreview `json:"context,omitempty"`
	Missing      []StatusMissingItem   `json:"missing,omitempty"`
	Warnings     []StatusMissingItem   `json:"warnings,omitempty"`
}

type VerifyStatus string

const (
	VerifyStatusDryRun          VerifyStatus = "dry_run"
	VerifyStatusNoTestsSelected VerifyStatus = "no_tests_selected"
	VerifyStatusPassed          VerifyStatus = "passed"
	VerifyStatusFailed          VerifyStatus = "failed"
)

type VerifyTestStatus string

const (
	VerifyTestStatusPassed   VerifyTestStatus = "passed"
	VerifyTestStatusFailed   VerifyTestStatus = "failed"
	VerifyTestStatusTimedOut VerifyTestStatus = "timed_out"
	VerifyTestStatusErrored  VerifyTestStatus = "errored"
	VerifyTestStatusSkipped  VerifyTestStatus = "skipped"
)

type VerifySelection struct {
	TestID           string   `json:"test_id"`
	Summary          string   `json:"summary"`
	SelectionReasons []string `json:"selection_reasons"`
}

type VerifyTestResult struct {
	TestID         string           `json:"test_id"`
	Status         VerifyTestStatus `json:"status"`
	DefinitionHash string           `json:"definition_hash"`
	ExitCode       *int             `json:"exit_code,omitempty"`
	DurationMS     int              `json:"duration_ms,omitempty"`
	StdoutExcerpt  string           `json:"stdout_excerpt,omitempty"`
	StderrExcerpt  string           `json:"stderr_excerpt,omitempty"`
}

type VerifyResult struct {
	Status          VerifyStatus       `json:"status"`
	BatchRunID      string             `json:"batch_run_id,omitempty"`
	SelectedTestIDs []string           `json:"selected_test_ids"`
	Selected        []VerifySelection  `json:"selected"`
	Passed          bool               `json:"passed"`
	Results         []VerifyTestResult `json:"results,omitempty"`
}

type InitResult struct {
	CandidateCount       int                  `json:"candidate_count"`
	IndexedStubs         int                  `json:"indexed_stubs"`
	CandidatesPersisted  bool                 `json:"candidates_persisted"`
	OutputCandidatesPath string               `json:"output_candidates_path,omitempty"`
	Warnings             []string             `json:"warnings,omitempty"`
	TemplateResults      []InitTemplateResult `json:"template_results,omitempty"`
}

type InitTemplateConflict struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type InitTemplateResult struct {
	TemplateID       string                 `json:"template_id"`
	Created          []string               `json:"created,omitempty"`
	Updated          []string               `json:"updated,omitempty"`
	Unchanged        []string               `json:"unchanged,omitempty"`
	SkippedConflicts []InitTemplateConflict `json:"skipped_conflicts,omitempty"`
}
