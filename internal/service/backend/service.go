package backend

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	bootstrapkit "github.com/bonztm/agent-workflow-manager/internal/bootstrap"
	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	"github.com/bonztm/agent-workflow-manager/internal/workspace"
)

const unboundedEnvVar = "AWM_UNBOUNDED"

const (
	syncModeChanged         = "changed"
	syncModeFull            = "full"
	syncModeWorkingTree     = "working_tree"
	defaultSyncGitRange     = "HEAD~1..HEAD"
	defaultSyncProjectDir   = "."
	defaultHealthDetails    = true
	defaultHealthFindings   = 100
	defaultVerifyTimeoutSec = 300

	requiredVerifyTestsKey = "verify:tests"

	defaultInitPersist      = false
	defaultInitRespectGit   = true
	maxInitWalkErrorSamples = 25
	maxFetchKeyLength       = 512
)

var healthTagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

type gitRunnerFunc func(ctx context.Context, projectRoot string, args ...string) (string, error)
type reviewRunnerFunc func(ctx context.Context, projectRoot string, command workflowRunDefinition, extraEnv map[string]string) verifyCommandRun

type RuntimeStatusSnapshot struct {
	Backend                string
	PostgresConfigured     bool
	SQLitePath             string
	UsesImplicitSQLitePath bool
}

type syncPathRecord struct {
	Path        string
	ContentHash string
	Deleted     bool
}

type syncOperationError struct {
	operation string
	err       error
}

type fetchOperationError struct {
	operation string
	err       error
}

type repositoryCore interface {
	FetchCandidatePointers(context.Context, core.CandidatePointerQuery) ([]core.CandidatePointer, error)
	ListPointerInventory(context.Context, string) ([]core.PointerInventory, error)
	UpsertPointerStubs(context.Context, string, []core.PointerStub) (int, error)
	UpsertReceiptScope(context.Context, core.ReceiptScope) error
	FetchReceiptScope(context.Context, core.ReceiptScopeQuery) (core.ReceiptScope, error)
	LookupFetchState(context.Context, core.FetchLookupQuery) (core.FetchLookup, error)
	LookupPointerByKey(context.Context, core.PointerLookupQuery) (core.CandidatePointer, error)
	SaveRunReceiptSummary(context.Context, core.RunReceiptSummary) (core.RunReceiptIDs, error)
	SaveReviewAttempt(context.Context, core.ReviewAttempt) (int64, error)
	ListReviewAttempts(context.Context, core.ReviewAttemptListQuery) ([]core.ReviewAttempt, error)
	UpsertWorkItems(context.Context, core.WorkItemsUpsertInput) (int, error)
	ListWorkItems(context.Context, core.FetchLookupQuery) ([]core.WorkItem, error)
	ApplySync(context.Context, core.SyncApplyInput) (core.SyncApplyResult, error)
	SyncRulePointers(context.Context, core.RulePointerSyncInput) (core.RulePointerSyncResult, error)
}

type workPlanStore interface {
	UpsertWorkPlan(context.Context, core.WorkPlanUpsertInput) (core.WorkPlanUpsertResult, error)
	LookupWorkPlan(context.Context, core.WorkPlanLookupQuery) (core.WorkPlan, error)
	ListWorkPlans(context.Context, core.WorkPlanListQuery) ([]core.WorkPlanSummary, error)
}

type historyStore interface {
	ListReceiptHistory(context.Context, core.ReceiptHistoryListQuery) ([]core.ReceiptHistorySummary, error)
	ListRunHistory(context.Context, core.RunHistoryListQuery) ([]core.RunHistorySummary, error)
	LookupRunHistory(context.Context, core.RunHistoryLookupQuery) (core.RunHistorySummary, error)
}

type verificationStore interface {
	SaveVerificationBatch(context.Context, core.VerificationBatch) error
}

func (e *syncOperationError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *syncOperationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *fetchOperationError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *fetchOperationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

type Service struct {
	repo             repositoryCore
	planRepo         workPlanStore
	historyRepo      historyStore
	verifyRepo       verificationStore
	runGitCommand    gitRunnerFunc
	runVerifyCommand verifyRunnerFunc
	runReviewCommand reviewRunnerFunc
	projectRoot      string
	runtimeStatus    RuntimeStatusSnapshot
}

func New(repo repositoryCore) (*Service, error) {
	return NewWithProjectRoot(repo, "")
}

func NewWithProjectRoot(repo repositoryCore, projectRoot string) (*Service, error) {
	return NewWithRuntimeStatus(repo, projectRoot, RuntimeStatusSnapshot{})
}

func NewWithRuntimeStatus(repo repositoryCore, projectRoot string, snapshot RuntimeStatusSnapshot) (*Service, error) {
	if repo == nil {
		return nil, fmt.Errorf("repository is required")
	}
	planRepo, ok := repo.(workPlanStore)
	if !ok {
		return nil, fmt.Errorf("work plan storage is required")
	}
	svc := &Service{
		repo:             repo,
		planRepo:         planRepo,
		runGitCommand:    runGitCommand,
		runVerifyCommand: runVerifyCommand,
		runReviewCommand: runWorkflowReviewCommand,
		projectRoot:      normalizeSyncProjectRoot(projectRoot),
		runtimeStatus:    snapshot,
	}
	if historyRepo, ok := repo.(historyStore); ok {
		svc.historyRepo = historyRepo
	}
	if verifyRepo, ok := repo.(verificationStore); ok {
		svc.verifyRepo = verifyRepo
	}
	return svc, nil
}

func (s *Service) defaultProjectRoot() string {
	if s != nil {
		if root := strings.TrimSpace(s.projectRoot); root != "" {
			return filepath.Clean(root)
		}
	}
	detected := workspace.DetectRoot("")
	if root := strings.TrimSpace(detected.Path); root != "" {
		return filepath.Clean(root)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return bootstrapkit.NormalizeProjectRoot("")
	}
	return filepath.Clean(cwd)
}

func (s *Service) effectiveProjectRoot(explicit string) string {
	if trimmed := strings.TrimSpace(explicit); trimmed != "" {
		return bootstrapkit.NormalizeProjectRoot(trimmed)
	}
	return s.defaultProjectRoot()
}

func normalizeCompletionPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		normalized := normalizeCompletionPath(raw)
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

func normalizeCompletionPath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	normalizedSlashes := strings.ReplaceAll(trimmed, "\\", "/")
	if strings.HasPrefix(normalizedSlashes, "/") || isWindowsAbsolutePath(normalizedSlashes) {
		return ""
	}
	cleaned := path.Clean(normalizedSlashes)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return cleaned
}

func isWindowsAbsolutePath(value string) bool {
	return len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && value[2] == '/'
}

func normalizeValues(values []string) []string {
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
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func notImplemented(op string) *core.APIError {
	return backendError(v1.ErrCodeNotImplemented, "service backend for operation is not wired yet", map[string]any{"operation": op})
}
