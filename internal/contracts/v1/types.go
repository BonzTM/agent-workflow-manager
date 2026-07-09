package v1

import "encoding/json"

// Version is the contract version string stamped on every command and result envelope.
const Version = "awm.v1"

// Command names an AWM operation carried in a CommandEnvelope.
type Command string

const (
	// CommandContext requests task-scoped context (rules, plans) and issues a receipt.
	CommandContext Command = "context"
	// CommandFetch retrieves full payloads for pointer keys or a receipt's pointers.
	CommandFetch Command = "fetch"
	// CommandExport renders context, fetch, history, or status data as a document.
	CommandExport Command = "export"
	// CommandDone reports task completion for scope and definition-of-done checking.
	CommandDone Command = "done"
	// CommandReview requests a review-phase operation.
	CommandReview Command = "review"
	// CommandWork creates or updates a work plan and its tasks.
	CommandWork Command = "work"
	// CommandHistorySearch searches historical plans, receipts, and runs.
	CommandHistorySearch Command = "history"
	// CommandSync reconciles the stored index with the working tree.
	CommandSync Command = "sync"
	// CommandHealth runs health checks and optionally applies fixers.
	CommandHealth Command = "health"
	// CommandStatus reports project configuration and readiness.
	CommandStatus Command = "status"
	// CommandVerify selects and runs verification tests for a change.
	CommandVerify Command = "verify"
	// CommandInit initializes a project: scans candidates and applies templates.
	CommandInit Command = "init"
)

// Phase identifies the workflow stage a task is operating in.
type Phase string

const (
	// PhasePlan is the planning stage of a task.
	PhasePlan Phase = "plan"
	// PhaseExecute is the implementation stage of a task.
	PhaseExecute Phase = "execute"
	// PhaseReview is the review stage of a task.
	PhaseReview Phase = "review"
)

// ScopeMode controls how scope violations are treated when completing a task.
type ScopeMode string

const (
	// ScopeModeStrict rejects completion on scope violations.
	ScopeModeStrict ScopeMode = "strict"
	// ScopeModeWarn reports scope violations without rejecting completion.
	ScopeModeWarn ScopeMode = "warn"
)

// CommandEnvelope is the versioned wrapper for an inbound command and its raw payload.
type CommandEnvelope struct {
	Version   string          `json:"version"`
	Command   Command         `json:"command"`
	RequestID string          `json:"request_id"`
	Payload   json.RawMessage `json:"payload"`
}

// ErrorPayload describes a command failure with a machine-readable code and message.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Source  string `json:"source,omitempty"`
	Details any    `json:"details,omitempty"`
}

// ResultEnvelope is the versioned wrapper for a command outcome, carrying either a result or an error.
type ResultEnvelope struct {
	Version   string        `json:"version"`
	Command   Command       `json:"command"`
	RequestID string        `json:"request_id"`
	OK        bool          `json:"ok"`
	Timestamp string        `json:"timestamp"`
	Result    any           `json:"result,omitempty"`
	Error     *ErrorPayload `json:"error,omitempty"`
}

// ContextPayload is the input for the context command: the task and phase to resolve context for.
type ContextPayload struct {
	ProjectID         string   `json:"project_id"`
	TaskText          string   `json:"task_text"`
	Phase             Phase    `json:"phase"`
	TagsFile          string   `json:"tags_file,omitempty"`
	InitialScopePaths []string `json:"initial_scope_paths,omitempty"`
}

// FetchPayload is the input for the fetch command: keys or a receipt to resolve, with optional expected versions.
type FetchPayload struct {
	ProjectID        string            `json:"project_id"`
	Keys             []string          `json:"keys,omitempty"`
	ReceiptID        string            `json:"receipt_id,omitempty"`
	ExpectedVersions map[string]string `json:"expected_versions,omitempty"`
}

// ExportFormat selects the output encoding for the export command.
type ExportFormat string

const (
	// ExportFormatJSON renders the export document as JSON.
	ExportFormatJSON ExportFormat = "json"
	// ExportFormatMarkdown renders the export document as Markdown.
	ExportFormatMarkdown ExportFormat = "markdown"
)

// ExportContextSelector selects a context resolution to export, mirroring ContextPayload inputs.
type ExportContextSelector struct {
	TaskText          string   `json:"task_text"`
	Phase             Phase    `json:"phase"`
	TagsFile          string   `json:"tags_file,omitempty"`
	InitialScopePaths []string `json:"initial_scope_paths,omitempty"`
}

// ExportFetchSelector selects a fetch bundle to export, mirroring FetchPayload inputs.
type ExportFetchSelector struct {
	Keys             []string          `json:"keys,omitempty"`
	ReceiptID        string            `json:"receipt_id,omitempty"`
	ExpectedVersions map[string]string `json:"expected_versions,omitempty"`
}

// ExportHistorySelector selects a history search to export, mirroring HistorySearchPayload inputs.
type ExportHistorySelector struct {
	Entity    HistoryEntity `json:"entity,omitempty"`
	Query     string        `json:"query,omitempty"`
	Scope     HistoryScope  `json:"scope,omitempty"`
	Kind      string        `json:"kind,omitempty"`
	Limit     int           `json:"limit,omitempty"`
	Unbounded *bool         `json:"unbounded,omitempty"`
}

// ExportStatusSelector selects a status report to export, mirroring StatusPayload inputs.
type ExportStatusSelector struct {
	ProjectRoot   string `json:"project_root,omitempty"`
	RulesFile     string `json:"rules_file,omitempty"`
	TagsFile      string `json:"tags_file,omitempty"`
	TestsFile     string `json:"tests_file,omitempty"`
	WorkflowsFile string `json:"workflows_file,omitempty"`
	TaskText      string `json:"task_text,omitempty"`
	Phase         Phase  `json:"phase,omitempty"`
}

// ExportPayload is the input for the export command: a format plus exactly one selector for the data to export.
type ExportPayload struct {
	ProjectID string                 `json:"project_id"`
	Format    ExportFormat           `json:"format"`
	Context   *ExportContextSelector `json:"context,omitempty"`
	Fetch     *ExportFetchSelector   `json:"fetch,omitempty"`
	History   *ExportHistorySelector `json:"history,omitempty"`
	Status    *ExportStatusSelector  `json:"status,omitempty"`
}

// DonePayload is the input for the done command: the completed work's outcome, changed files, and scope mode.
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

// WorkItemStatus is the lifecycle state of a plan, task, or plan stage.
type WorkItemStatus string

const (
	// WorkItemStatusPending marks an item not yet started.
	WorkItemStatusPending WorkItemStatus = "pending"
	// WorkItemStatusInProgress marks an item currently being worked on.
	WorkItemStatusInProgress WorkItemStatus = "in_progress"
	// WorkItemStatusComplete marks an item finished.
	WorkItemStatusComplete WorkItemStatus = "complete"
	// WorkItemStatusBlocked marks an item that cannot proceed.
	WorkItemStatusBlocked WorkItemStatus = "blocked"
	// WorkItemStatusSuperseded marks an item replaced by other work.
	WorkItemStatusSuperseded WorkItemStatus = "superseded"
)

// WorkPlanMode controls how a work command combines submitted tasks with existing plan tasks.
type WorkPlanMode string

const (
	// WorkPlanModeMerge merges submitted tasks into the existing plan.
	WorkPlanModeMerge WorkPlanMode = "merge"
	// WorkPlanModeReplace replaces the plan's tasks with the submitted set.
	WorkPlanModeReplace WorkPlanMode = "replace"
)

// WorkPlanStagesPayload sets the status of a plan's spec and implementation stages.
type WorkPlanStagesPayload struct {
	SpecOutline        WorkItemStatus `json:"spec_outline,omitempty"`
	RefinedSpec        WorkItemStatus `json:"refined_spec,omitempty"`
	ImplementationPlan WorkItemStatus `json:"implementation_plan,omitempty"`
}

// WorkPlanPayload describes a plan's metadata, scope, and references for the work command.
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

// WorkTaskPayload describes a single task within a plan for the work command.
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

// WorkPayload is the input for the work command: a plan identity, update mode, plan fields, and tasks.
type WorkPayload struct {
	ProjectID string            `json:"project_id"`
	PlanKey   string            `json:"plan_key,omitempty"`
	PlanTitle string            `json:"plan_title,omitempty"`
	ReceiptID string            `json:"receipt_id,omitempty"`
	Mode      WorkPlanMode      `json:"mode,omitempty"`
	Plan      *WorkPlanPayload  `json:"plan,omitempty"`
	Tasks     []WorkTaskPayload `json:"tasks,omitempty"`
}

// HistoryScope filters history searches by an item's lifecycle bucket.
type HistoryScope string

const (
	// HistoryScopeCurrent limits results to active items.
	HistoryScopeCurrent HistoryScope = "current"
	// HistoryScopeDeferred limits results to deferred items.
	HistoryScopeDeferred HistoryScope = "deferred"
	// HistoryScopeCompleted limits results to completed items.
	HistoryScopeCompleted HistoryScope = "completed"
	// HistoryScopeAll includes items from every scope.
	HistoryScopeAll HistoryScope = "all"
)

// HistoryEntity selects which record type a history search returns.
type HistoryEntity string

const (
	// HistoryEntityAll searches across all entity types.
	HistoryEntityAll HistoryEntity = "all"
	// HistoryEntityWork searches work plans and tasks.
	HistoryEntityWork HistoryEntity = "work"
	// HistoryEntityReceipt searches context receipts.
	HistoryEntityReceipt HistoryEntity = "receipt"
	// HistoryEntityRun searches recorded runs.
	HistoryEntityRun HistoryEntity = "run"
)

// HistorySearchPayload is the input for the history command: entity, query, scope, and result limits.
type HistorySearchPayload struct {
	ProjectID string        `json:"project_id"`
	Entity    HistoryEntity `json:"entity,omitempty"`
	Query     string        `json:"query,omitempty"`
	Scope     HistoryScope  `json:"scope,omitempty"`
	Kind      string        `json:"kind,omitempty"`
	Limit     int           `json:"limit,omitempty"`
	Unbounded *bool         `json:"unbounded,omitempty"`
}

// SyncPayload is the input for the sync command: what to reconcile and where the source files live.
type SyncPayload struct {
	ProjectID           string `json:"project_id"`
	Mode                string `json:"mode,omitempty"`
	GitRange            string `json:"git_range,omitempty"`
	ProjectRoot         string `json:"project_root,omitempty"`
	RulesFile           string `json:"rules_file,omitempty"`
	TagsFile            string `json:"tags_file,omitempty"`
	InsertNewCandidates *bool  `json:"insert_new_candidates,omitempty"`
}

// HealthFixer names a remediation the health command can apply.
type HealthFixer string

const (
	// HealthFixerAll applies every available fixer.
	HealthFixerAll HealthFixer = "all"
	// HealthFixerSyncWorkingTree reconciles the index with the working tree.
	HealthFixerSyncWorkingTree HealthFixer = "sync_working_tree"
	// HealthFixerIndexUnindexedFile indexes files missing from the index.
	HealthFixerIndexUnindexedFile HealthFixer = "index_unindexed_files"
	// HealthFixerSyncRuleset reconciles the stored ruleset with the rules file.
	HealthFixerSyncRuleset HealthFixer = "sync_ruleset"
)

// HealthPayload is the input for the health command: reporting options and which fixers to plan or apply.
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

// StatusPayload is the input for the status command: project location, source file paths, and an optional context preview task.
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

// VerifyPayload is the input for the verify command: what changed and which tests to select or run.
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

// InitPayload is the input for the init command: project location, source files, and candidate/template options.
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

// ContextRule is a resolved rule delivered in a context receipt, with its enforcement level and optional content.
type ContextRule struct {
	RuleID      string `json:"rule_id"`
	Key         string `json:"key"`
	Summary     string `json:"summary"`
	Enforcement string `json:"enforcement"`
	Content     string `json:"content,omitempty"`
}

// ContextPlan is a plan stub delivered in a context receipt, with keys to fetch for full detail.
type ContextPlan struct {
	Key       string         `json:"key"`
	Summary   string         `json:"summary"`
	Status    WorkItemStatus `json:"status"`
	FetchKeys []string       `json:"fetch_keys,omitempty"`
}

// ContextPlanTaskCounts tallies a plan's tasks by status.
type ContextPlanTaskCounts struct {
	Total      int `json:"total"`
	Pending    int `json:"pending"`
	InProgress int `json:"in_progress"`
	Blocked    int `json:"blocked"`
	Complete   int `json:"complete"`
}

// ContextReceiptMeta records the identity and inputs of a context resolution.
type ContextReceiptMeta struct {
	ReceiptID        string   `json:"receipt_id"`
	ProjectID        string   `json:"project_id"`
	TaskText         string   `json:"task_text"`
	Phase            Phase    `json:"phase"`
	ResolvedTags     []string `json:"resolved_tags"`
	BaselineCaptured bool     `json:"baseline_captured"`
}

// ContextReceipt is the resolved context for a task: rules, plan stubs, scope paths, and receipt metadata.
type ContextReceipt struct {
	Rules             []ContextRule      `json:"rules"`
	Plans             []ContextPlan      `json:"plans"`
	InitialScopePaths []string           `json:"initial_scope_paths,omitempty"`
	Meta              ContextReceiptMeta `json:"_meta"`
}

// ContextResult is the output of the context command: a status and, on success, the receipt.
type ContextResult struct {
	Status  string          `json:"status"`
	Receipt *ContextReceipt `json:"receipt,omitempty"`
}

// FetchItem is one resolved pointer returned by the fetch command, including its content and version.
type FetchItem struct {
	Key     string `json:"key"`
	Type    string `json:"type"`
	Summary string `json:"summary"`
	Content string `json:"content,omitempty"`
	Status  string `json:"status,omitempty"`
	Version string `json:"version,omitempty"`
}

// FetchVersionMismatch reports a fetched key whose stored version differs from the expected version.
type FetchVersionMismatch struct {
	Key      string `json:"key"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

// FetchResult is the output of the fetch command: resolved items plus missing keys and version mismatches.
type FetchResult struct {
	Items             []FetchItem            `json:"items"`
	NotFound          []string               `json:"not_found,omitempty"`
	VersionMismatches []FetchVersionMismatch `json:"version_mismatches,omitempty"`
}

// ExportDocumentKind identifies which document variety an ExportDocument carries.
type ExportDocumentKind string

const (
	// ExportDocumentKindContext marks a document containing a context receipt.
	ExportDocumentKindContext ExportDocumentKind = "context"
	// ExportDocumentKindPlan marks a document containing a plan.
	ExportDocumentKindPlan ExportDocumentKind = "plan"
	// ExportDocumentKindReceipt marks a document containing a receipt.
	ExportDocumentKindReceipt ExportDocumentKind = "receipt"
	// ExportDocumentKindTask marks a document containing a task.
	ExportDocumentKindTask ExportDocumentKind = "task"
	// ExportDocumentKindRun marks a document containing a run.
	ExportDocumentKindRun ExportDocumentKind = "run"
	// ExportDocumentKindFetchBundle marks a document containing a fetch bundle.
	ExportDocumentKindFetchBundle ExportDocumentKind = "fetch_bundle"
	// ExportDocumentKindHistory marks a document containing history search results.
	ExportDocumentKindHistory ExportDocumentKind = "history"
	// ExportDocumentKindStatus marks a document containing a status report.
	ExportDocumentKindStatus ExportDocumentKind = "status"
)

// ExportPlanStages reports the status of a plan's spec and implementation stages in an export.
type ExportPlanStages struct {
	SpecOutline        WorkItemStatus `json:"spec_outline,omitempty"`
	RefinedSpec        WorkItemStatus `json:"refined_spec,omitempty"`
	ImplementationPlan WorkItemStatus `json:"implementation_plan,omitempty"`
}

// ExportTaskDocument is the exported representation of a single task, including dependencies and outcome.
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

// ExportPlanDocument is the exported representation of a plan with its metadata, scope, and tasks.
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

// ExportBaselinePath records one path captured in a receipt's baseline, with its content hash or deletion state.
type ExportBaselinePath struct {
	Path        string `json:"path"`
	Deleted     bool   `json:"deleted"`
	ContentHash string `json:"content_hash,omitempty"`
}

// ExportReceiptRunDocument summarizes the latest run attached to an exported receipt.
type ExportReceiptRunDocument struct {
	RunID      int64                `json:"run_id"`
	Status     string               `json:"status,omitempty"`
	PlanStatus WorkItemStatus       `json:"plan_status,omitempty"`
	Tasks      []ExportTaskDocument `json:"tasks,omitempty"`
}

// ExportReceiptDocument is the exported representation of a context receipt, including baseline and latest run.
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

// ExportRunDocument is the exported representation of a recorded run and its outcome.
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

// ExportBundleItemKind identifies which record type an ExportBundleItem carries.
type ExportBundleItemKind string

const (
	// ExportBundleItemKindPlan marks a bundle item carrying a plan.
	ExportBundleItemKindPlan ExportBundleItemKind = "plan"
	// ExportBundleItemKindReceipt marks a bundle item carrying a receipt.
	ExportBundleItemKindReceipt ExportBundleItemKind = "receipt"
	// ExportBundleItemKindTask marks a bundle item carrying a task.
	ExportBundleItemKindTask ExportBundleItemKind = "task"
	// ExportBundleItemKindRun marks a bundle item carrying a run.
	ExportBundleItemKindRun ExportBundleItemKind = "run"
	// ExportBundleItemKindPointer marks a bundle item carrying raw pointer content.
	ExportBundleItemKindPointer ExportBundleItemKind = "pointer"
	// ExportBundleItemKindRule marks a bundle item carrying a rule.
	ExportBundleItemKindRule ExportBundleItemKind = "rule"
)

// ExportBundleItem is one entry in an exported fetch bundle, holding exactly one record per its Kind.
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

// ExportBundleDocument is an exported fetch bundle: resolved items plus missing keys and version mismatches.
type ExportBundleDocument struct {
	RequestedKeys     []string               `json:"requested_keys,omitempty"`
	Items             []ExportBundleItem     `json:"items"`
	NotFound          []string               `json:"not_found,omitempty"`
	VersionMismatches []FetchVersionMismatch `json:"version_mismatches,omitempty"`
}

// ExportDocument is the structured export output, holding exactly one document variant per its Kind.
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

// ExportResult is the output of the export command: the rendered content and, for JSON, the structured document.
type ExportResult struct {
	Format   ExportFormat    `json:"format"`
	Document *ExportDocument `json:"document,omitempty"`
	Content  string          `json:"content"`
}

// CompletionViolation reports a changed path that violates the task's scope, with the reason.
type CompletionViolation struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// DoneResult is the output of the done command: whether completion was accepted and any violations found.
type DoneResult struct {
	Accepted               bool                  `json:"accepted"`
	Violations             []CompletionViolation `json:"violations"`
	DefinitionOfDoneIssues []string              `json:"definition_of_done_issues,omitempty"`
	RunID                  int                   `json:"run_id,omitempty"`
}

// WorkResult is the output of the work command: the plan's key, status, and counts of updated items.
type WorkResult struct {
	PlanKey    string `json:"plan_key"`
	PlanStatus string `json:"plan_status"`
	Updated    int    `json:"updated"`
	TaskCount  int    `json:"task_count,omitempty"`
}

// HistoryItem is one match from a history search, identifying the entity and its related keys.
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

// HistorySearchResult is the output of the history command: the effective search parameters and matched items.
type HistorySearchResult struct {
	Entity HistoryEntity `json:"entity"`
	Scope  HistoryScope  `json:"scope,omitempty"`
	Query  string        `json:"query,omitempty"`
	Limit  int           `json:"limit"`
	Count  int           `json:"count"`
	Items  []HistoryItem `json:"items"`
}

// SyncResult is the output of the sync command: counts of index updates and the paths processed.
type SyncResult struct {
	Updated            int      `json:"updated"`
	MarkedStale        int      `json:"marked_stale"`
	NewCandidates      int      `json:"new_candidates"`
	IndexedStubs       int      `json:"indexed_stubs"`
	DeletedMarkedStale int      `json:"deleted_marked_stale"`
	ProcessedPaths     []string `json:"processed_paths,omitempty"`
}

// HealthSummary is the top-line health verdict: overall OK flag and total finding count.
type HealthSummary struct {
	OK            bool `json:"ok"`
	TotalFindings int  `json:"total_findings"`
}

// HealthCheckItem reports one health check's findings with severity, count, and sample details.
type HealthCheckItem struct {
	Name     string   `json:"name"`
	Severity string   `json:"severity"`
	Count    int      `json:"count"`
	Samples  []string `json:"samples,omitempty"`
}

// HealthCheckResult is the check-mode output of the health command: a summary plus per-check findings.
type HealthCheckResult struct {
	Summary HealthSummary     `json:"summary"`
	Checks  []HealthCheckItem `json:"checks"`
}

// HealthFixAction records one fixer's planned or applied changes with a count and notes.
type HealthFixAction struct {
	Fixer HealthFixer `json:"fixer"`
	Count int         `json:"count"`
	Notes []string    `json:"notes,omitempty"`
}

// HealthFixResult is the fix-mode output of the health command: planned versus applied actions.
type HealthFixResult struct {
	DryRun         bool              `json:"dry_run"`
	PlannedActions []HealthFixAction `json:"planned_actions"`
	AppliedActions []HealthFixAction `json:"applied_actions"`
	Summary        string            `json:"summary"`
}

// HealthResult is the output of the health command, carrying check or fix results per its mode.
type HealthResult struct {
	Mode  string             `json:"mode"`
	Check *HealthCheckResult `json:"check,omitempty"`
	Fix   *HealthFixResult   `json:"fix,omitempty"`
}

// StatusSummary is the top-line status verdict: readiness plus missing and warning counts.
type StatusSummary struct {
	Ready        bool `json:"ready"`
	MissingCount int  `json:"missing_count"`
	WarningCount int  `json:"warning_count,omitempty"`
}

// StatusProject reports the project's identity, roots, and storage backend configuration.
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

// StatusSource reports whether one configured source file exists and loaded, and how many items it yielded.
type StatusSource struct {
	Kind         string   `json:"kind"`
	SourcePath   string   `json:"source_path"`
	AbsolutePath string   `json:"absolute_path,omitempty"`
	Exists       bool     `json:"exists"`
	Loaded       bool     `json:"loaded"`
	ItemCount    int      `json:"item_count,omitempty"`
	Notes        []string `json:"notes,omitempty"`
}

// StatusIntegration reports an agent integration's install state and which expected targets are missing.
type StatusIntegration struct {
	ID              string   `json:"id"`
	Summary         string   `json:"summary,omitempty"`
	Installed       bool     `json:"installed"`
	PresentTargets  int      `json:"present_targets"`
	ExpectedTargets int      `json:"expected_targets"`
	MissingTargets  []string `json:"missing_targets,omitempty"`
}

// StatusContextPreview reports the outcome of a trial context resolution run during status.
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

// StatusMissingItem describes a missing requirement or warning found during status, with a code and message.
type StatusMissingItem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// StatusResult is the output of the status command: summary, project info, sources, integrations, and issues.
type StatusResult struct {
	Summary      StatusSummary         `json:"summary"`
	Project      StatusProject         `json:"project"`
	Sources      []StatusSource        `json:"sources"`
	Integrations []StatusIntegration   `json:"integrations"`
	Context      *StatusContextPreview `json:"context,omitempty"`
	Missing      []StatusMissingItem   `json:"missing,omitempty"`
	Warnings     []StatusMissingItem   `json:"warnings,omitempty"`
}

// VerifyStatus is the overall outcome of a verify run.
type VerifyStatus string

const (
	// VerifyStatusDryRun indicates tests were selected but not executed.
	VerifyStatusDryRun VerifyStatus = "dry_run"
	// VerifyStatusNoTestsSelected indicates no tests matched the selection criteria.
	VerifyStatusNoTestsSelected VerifyStatus = "no_tests_selected"
	// VerifyStatusPassed indicates every executed test passed.
	VerifyStatusPassed VerifyStatus = "passed"
	// VerifyStatusFailed indicates at least one executed test did not pass.
	VerifyStatusFailed VerifyStatus = "failed"
)

// VerifyTestStatus is the outcome of a single test execution within a verify run.
type VerifyTestStatus string

const (
	// VerifyTestStatusPassed indicates the test passed.
	VerifyTestStatusPassed VerifyTestStatus = "passed"
	// VerifyTestStatusFailed indicates the test failed.
	VerifyTestStatusFailed VerifyTestStatus = "failed"
	// VerifyTestStatusTimedOut indicates the test exceeded its time limit.
	VerifyTestStatusTimedOut VerifyTestStatus = "timed_out"
	// VerifyTestStatusErrored indicates the test could not be executed.
	VerifyTestStatusErrored VerifyTestStatus = "errored"
	// VerifyTestStatusSkipped indicates the test was not run.
	VerifyTestStatusSkipped VerifyTestStatus = "skipped"
)

// VerifySelection explains why a test was selected for a verify run.
type VerifySelection struct {
	TestID           string   `json:"test_id"`
	Summary          string   `json:"summary"`
	SelectionReasons []string `json:"selection_reasons"`
}

// VerifyTestResult is the recorded outcome of one executed test, with exit code, duration, and output excerpts.
type VerifyTestResult struct {
	TestID         string           `json:"test_id"`
	Status         VerifyTestStatus `json:"status"`
	DefinitionHash string           `json:"definition_hash"`
	ExitCode       *int             `json:"exit_code,omitempty"`
	DurationMS     int              `json:"duration_ms,omitempty"`
	StdoutExcerpt  string           `json:"stdout_excerpt,omitempty"`
	StderrExcerpt  string           `json:"stderr_excerpt,omitempty"`
}

// VerifyResult is the output of the verify command: overall status, selected tests, and per-test results.
type VerifyResult struct {
	Status          VerifyStatus       `json:"status"`
	BatchRunID      string             `json:"batch_run_id,omitempty"`
	SelectedTestIDs []string           `json:"selected_test_ids"`
	Selected        []VerifySelection  `json:"selected"`
	Passed          bool               `json:"passed"`
	Results         []VerifyTestResult `json:"results,omitempty"`
}

// InitResult is the output of the init command: candidate and indexing counts plus template application results.
type InitResult struct {
	CandidateCount       int                  `json:"candidate_count"`
	IndexedStubs         int                  `json:"indexed_stubs"`
	CandidatesPersisted  bool                 `json:"candidates_persisted"`
	OutputCandidatesPath string               `json:"output_candidates_path,omitempty"`
	Warnings             []string             `json:"warnings,omitempty"`
	TemplateResults      []InitTemplateResult `json:"template_results,omitempty"`
}

// InitTemplateConflict reports a template path that was skipped, with the reason.
type InitTemplateConflict struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// InitTemplateResult reports the files a template created, updated, left unchanged, or skipped.
type InitTemplateResult struct {
	TemplateID       string                 `json:"template_id"`
	Created          []string               `json:"created,omitempty"`
	Updated          []string               `json:"updated,omitempty"`
	Unchanged        []string               `json:"unchanged,omitempty"`
	SkippedConflicts []InitTemplateConflict `json:"skipped_conflicts,omitempty"`
}
