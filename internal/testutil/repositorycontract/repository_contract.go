package repositorycontract

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/core"
)

// ContractRepository is the full repository surface a backend must implement
// to be exercised by the parity contract suite.
type ContractRepository interface {
	core.Repository
	core.WorkPlanRepository
	core.HistoryRepository
}

// ContractConfig configures RunRepositoryParity: a label identifying the
// backend, an optional fixed project ID (a unique one is generated when
// blank), the repository under test, and whether to also run the end-to-end
// service flows.
type ContractConfig struct {
	BackendLabel        string
	ProjectID           string
	Repo                ContractRepository
	IncludeServiceFlows bool
}

// RunRepositoryParity runs the shared repository contract suite against
// cfg.Repo as subtests, so every backend exhibits identical behavior. It
// fails t when cfg.Repo is nil or any contract expectation is violated.
func RunRepositoryParity(t *testing.T, cfg ContractConfig) {
	t.Helper()

	if cfg.Repo == nil {
		t.Fatal("repository is required")
	}

	projectID := cfg.ProjectID
	if projectID == "" {
		projectID = fmt.Sprintf("project.%s.%d", cfg.BackendLabel, time.Now().UTC().UnixNano())
	}

	t.Run("candidate_pointer_inventory", func(t *testing.T) {
		runCandidatePointerRetrieval(t, projectID+".candidates", cfg.Repo)
	})
	t.Run("candidate_pointer_limits", func(t *testing.T) {
		runCandidatePointerLimits(t, projectID+".candidate-limits", cfg.Repo)
	})
	t.Run("lookup_round_trip", func(t *testing.T) {
		runLookupRoundTrip(t, projectID+".lookup", cfg.Repo)
	})
	t.Run("stale_lookup_hidden", func(t *testing.T) {
		runStaleLookupHidden(t, projectID+".stale-lookup", cfg.Repo)
	})
	t.Run("work_plan_round_trip", func(t *testing.T) {
		runWorkPlanRoundTrip(t, projectID+".workplan", cfg.Repo)
	})
	t.Run("work_plan_merge_preserves_task_metadata", func(t *testing.T) {
		runWorkPlanMergePreservesTaskMetadata(t, projectID+".workplan-merge", cfg.Repo)
	})
	t.Run("rule_pointer_sync_round_trip", func(t *testing.T) {
		runRulePointerSyncRoundTrip(t, projectID+".rule-sync", cfg.Repo)
	})
	t.Run("receipt_scope_snapshot_round_trip", func(t *testing.T) {
		runReceiptScopeSnapshotRoundTrip(t, projectID+".receipt-scope", cfg.Repo)
	})
	t.Run("review_attempt_round_trip", func(t *testing.T) {
		runReviewAttemptRoundTrip(t, projectID+".review", cfg.Repo)
	})
	t.Run("run_summary_round_trip", func(t *testing.T) {
		runRunSummaryRoundTrip(t, projectID+".runs", cfg.Repo)
	})
	t.Run("run_summary_preserves_definition_of_done_issues", func(t *testing.T) {
		runRunSummaryPreservesDefinitionOfDoneIssues(t, projectID+".runs-dod", cfg.Repo)
	})
	if cfg.IncludeServiceFlows {
		t.Run("service_flows", func(t *testing.T) {
			RunServiceFlows(t, ServiceFlowConfig{
				BackendLabel: cfg.BackendLabel,
				ProjectID:    projectID + ".service",
				Repo:         cfg.Repo,
			})
		})
	}
}

func runCandidatePointerRetrieval(t *testing.T, projectID string, repo ContractRepository) {
	t.Helper()
	ctx := context.Background()

	stubs := []core.PointerStub{
		{
			PointerKey:  "pointer.rule.plan",
			Path:        "docs/rules.md",
			Kind:        "rule",
			Label:       "Plan rule",
			Description: "Plan workflow rule",
			Tags:        []string{"plan", "workflow"},
		},
		{
			PointerKey:  "pointer.doc.plan",
			Path:        "docs/plan.md",
			Kind:        "doc",
			Label:       "Plan guide",
			Description: "Plan workflow guide",
			Tags:        []string{"guide", "plan"},
		},
		{
			PointerKey:  "pointer.code.execute",
			Path:        "internal/runtime/service.go",
			Kind:        "code",
			Label:       "Execute handler",
			Description: "Execute runtime handler",
			Tags:        []string{"execute", "runtime"},
		},
		{
			PointerKey:  "pointer.test.execute",
			Path:        "internal/runtime/service_test.go",
			Kind:        "test",
			Label:       "Execute tests",
			Description: "Execute runtime tests",
			Tags:        []string{"execute", "tests"},
		},
	}
	if _, err := repo.UpsertPointerStubs(ctx, projectID, stubs); err != nil {
		t.Fatalf("seed candidate pointers: %v", err)
	}

	planRows, err := repo.FetchCandidatePointers(ctx, core.CandidatePointerQuery{
		ProjectID: projectID,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("fetch plan candidates: %v", err)
	}
	if got := candidateKeys(planRows); !reflect.DeepEqual(got, []string{
		"pointer.doc.plan",
		"pointer.rule.plan",
		"pointer.code.execute",
		"pointer.test.execute",
	}) {
		t.Fatalf("unexpected candidate inventory order: got %v", got)
	}
}

func runCandidatePointerLimits(t *testing.T, projectID string, repo ContractRepository) {
	t.Helper()
	ctx := context.Background()

	stubs := []core.PointerStub{
		{
			PointerKey:  "pointer.doc.alpha",
			Path:        "docs/alpha.md",
			Kind:        "doc",
			Label:       "Alpha guide",
			Description: "Alpha guidance for review and planning",
			Tags:        []string{"alpha", "focus"},
		},
		{
			PointerKey:  "pointer.code.alpha",
			Path:        "internal/alpha/runtime.go",
			Kind:        "code",
			Label:       "Alpha runtime",
			Description: "Alpha execution handler",
			Tags:        []string{"alpha", "focus"},
		},
		{
			PointerKey:  "pointer.test.alpha",
			Path:        "internal/alpha/runtime_test.go",
			Kind:        "test",
			Label:       "Alpha tests",
			Description: "Alpha verification inventory",
			Tags:        []string{"alpha", "focus"},
		},
		{
			PointerKey:  "pointer.code.full",
			Path:        "internal/inventory/full.go",
			Kind:        "code",
			Label:       "Alpha beta inventory",
			Description: "Alpha beta inventory reference",
			Tags:        []string{"focus", "inventory"},
		},
		{
			PointerKey:  "pointer.code.tag",
			Path:        "internal/inventory/tag.go",
			Kind:        "code",
			Label:       "Inventory tag signal",
			Description: "Focus-only inventory signal",
			Tags:        []string{"focus"},
		},
		{
			PointerKey:  "pointer.code.text",
			Path:        "internal/inventory/text.go",
			Kind:        "code",
			Label:       "Alpha text signal",
			Description: "Text-only inventory signal",
			Tags:        []string{"inventory"},
		},
	}
	if _, err := repo.UpsertPointerStubs(ctx, projectID, stubs); err != nil {
		t.Fatalf("seed inventory candidate pointers: %v", err)
	}

	limitedRows, err := repo.FetchCandidatePointers(ctx, core.CandidatePointerQuery{
		ProjectID: projectID,
		Limit:     3,
	})
	if err != nil {
		t.Fatalf("fetch limited candidate pointers: %v", err)
	}
	if got := candidateKeys(limitedRows); !reflect.DeepEqual(got, []string{
		"pointer.doc.alpha",
		"pointer.code.alpha",
		"pointer.test.alpha",
	}) {
		t.Fatalf("unexpected limited candidate order: got %v", got)
	}

	unboundedRows, err := repo.FetchCandidatePointers(ctx, core.CandidatePointerQuery{
		ProjectID: projectID,
		Unbounded: true,
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("fetch unbounded candidate pointers: %v", err)
	}
	if len(unboundedRows) != 6 {
		t.Fatalf("expected six unbounded candidate pointers, got %d", len(unboundedRows))
	}
}

func runLookupRoundTrip(t *testing.T, projectID string, repo ContractRepository) {
	t.Helper()
	ctx := context.Background()

	if _, err := repo.UpsertPointerStubs(ctx, projectID, []core.PointerStub{{
		PointerKey:  "pointer.lookup",
		Path:        "docs/lookup.md",
		Kind:        "doc",
		Label:       "Lookup pointer",
		Description: "Pointer used for fetch expansion lookup tests",
		Tags:        []string{"fetch", "lookup"},
	}}); err != nil {
		t.Fatalf("seed lookup pointer: %v", err)
	}

	pointer, err := repo.LookupPointerByKey(ctx, core.PointerLookupQuery{
		ProjectID:  " " + projectID + " ",
		PointerKey: " pointer.lookup ",
	})
	if err != nil {
		t.Fatalf("lookup pointer by key: %v", err)
	}
	if pointer.Key != "pointer.lookup" || pointer.Path != "docs/lookup.md" || pointer.Kind != "doc" {
		t.Fatalf("unexpected pointer lookup result: %+v", pointer)
	}
	if !reflect.DeepEqual(pointer.Tags, []string{"fetch", "lookup"}) {
		t.Fatalf("unexpected pointer tags: %+v", pointer.Tags)
	}
}

func runStaleLookupHidden(t *testing.T, projectID string, repo ContractRepository) {
	t.Helper()
	ctx := context.Background()

	if _, err := repo.UpsertPointerStubs(ctx, projectID, []core.PointerStub{{
		PointerKey:  "pointer.stale.lookup",
		Path:        "docs/stale-lookup.md",
		Kind:        "doc",
		Label:       "Stale lookup pointer",
		Description: "Pointer used to verify stale rows are hidden from lookup",
		Tags:        []string{"fetch", "lookup"},
	}}); err != nil {
		t.Fatalf("seed stale lookup pointer: %v", err)
	}

	if _, err := repo.ApplySync(ctx, core.SyncApplyInput{
		ProjectID: projectID,
		Mode:      "full",
		Paths:     []core.SyncPath{},
	}); err != nil {
		t.Fatalf("mark stale pointer via full sync: %v", err)
	}

	_, err := repo.LookupPointerByKey(ctx, core.PointerLookupQuery{
		ProjectID:  projectID,
		PointerKey: "pointer.stale.lookup",
	})
	if err == nil {
		t.Fatal("expected stale pointer lookup to be hidden")
	}
	if !errors.Is(err, core.ErrPointerLookupNotFound) {
		t.Fatalf("expected ErrPointerLookupNotFound, got %v", err)
	}
}

func runWorkPlanRoundTrip(t *testing.T, projectID string, repo ContractRepository) {
	t.Helper()
	ctx := context.Background()

	result, err := repo.UpsertWorkPlan(ctx, core.WorkPlanUpsertInput{
		ProjectID:     projectID,
		PlanKey:       "plan:receipt.abc123",
		ReceiptID:     "receipt.abc123",
		Mode:          core.WorkPlanModeReplace,
		Title:         "Import Optimization",
		Objective:     "Keep task fetches compact",
		Kind:          "story",
		ParentPlanKey: "plan:receipt.parent123",
		DiscoveredPaths: []string{
			" ./cmd//awm/main.go ",
			"internal/contracts/v1/command_catalog.go",
			"cmd/awm/main.go",
		},
		ExternalRefs: []string{"jira:AWM-1"},
		Tasks: []core.WorkItem{
			{
				ItemKey:       "task.blocked",
				Summary:       "Resolve API limit issue",
				Status:        core.WorkItemStatusBlocked,
				ParentTaskKey: "task.epic",
				ExternalRefs:  []string{"linear:ENG-3"},
			},
			{
				ItemKey: "task.active",
				Summary: "Ship MCP parity",
				Status:  core.WorkItemStatusInProgress,
			},
			{
				ItemKey: "task.done",
				Summary: "Cut migration",
				Status:  core.WorkItemStatusComplete,
			},
		},
	})
	if err != nil {
		t.Fatalf("upsert work plan: %v", err)
	}
	if result.Plan.Kind != "story" || result.Plan.ParentPlanKey != "plan:receipt.parent123" {
		t.Fatalf("unexpected upserted plan metadata: %+v", result.Plan)
	}

	plan, err := repo.LookupWorkPlan(ctx, core.WorkPlanLookupQuery{
		ProjectID: projectID,
		PlanKey:   "plan:receipt.abc123",
	})
	if err != nil {
		t.Fatalf("lookup work plan: %v", err)
	}
	if plan.Kind != "story" || plan.ParentPlanKey != "plan:receipt.parent123" {
		t.Fatalf("unexpected plan hierarchy fields: %+v", plan)
	}
	if !reflect.DeepEqual(plan.ExternalRefs, []string{"jira:AWM-1"}) {
		t.Fatalf("unexpected plan external refs: %+v", plan.ExternalRefs)
	}
	if !reflect.DeepEqual(plan.DiscoveredPaths, []string{"cmd/awm/main.go", "internal/contracts/v1/command_catalog.go"}) {
		t.Fatalf("unexpected discovered paths: %+v", plan.DiscoveredPaths)
	}
	if len(plan.Tasks) != 3 {
		t.Fatalf("expected three tasks, got %+v", plan.Tasks)
	}
	if plan.Tasks[0].ItemKey != "task.active" || plan.Tasks[1].ItemKey != "task.blocked" || plan.Tasks[2].ItemKey != "task.done" {
		t.Fatalf("unexpected persisted task order: %+v", plan.Tasks)
	}
	if plan.Tasks[1].ParentTaskKey != "task.epic" {
		t.Fatalf("unexpected parent task key: %+v", plan.Tasks[1])
	}
	if !reflect.DeepEqual(plan.Tasks[1].ExternalRefs, []string{"linear:ENG-3"}) {
		t.Fatalf("unexpected task external refs: %+v", plan.Tasks[1].ExternalRefs)
	}

	summaries, err := repo.ListWorkPlans(ctx, core.WorkPlanListQuery{
		ProjectID: projectID,
		Limit:     8,
	})
	if err != nil {
		t.Fatalf("list work plans: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected one work plan summary, got %+v", summaries)
	}
	summary := summaries[0]
	if summary.Kind != "story" || summary.ParentPlanKey != "plan:receipt.parent123" {
		t.Fatalf("unexpected summary metadata: %+v", summary)
	}
	if summary.TaskCountTotal != 3 || summary.TaskCountBlocked != 1 || summary.TaskCountInProgress != 1 || summary.TaskCountComplete != 1 {
		t.Fatalf("unexpected summary counts: %+v", summary)
	}
	if !reflect.DeepEqual(summary.ActiveTaskKeys, []string{"task.blocked", "task.active"}) {
		t.Fatalf("unexpected active task keys: %+v", summary.ActiveTaskKeys)
	}
}

func runWorkPlanMergePreservesTaskMetadata(t *testing.T, projectID string, repo ContractRepository) {
	t.Helper()
	ctx := context.Background()

	_, err := repo.UpsertWorkPlan(ctx, core.WorkPlanUpsertInput{
		ProjectID: projectID,
		PlanKey:   "plan:receipt.merge123",
		ReceiptID: "receipt.merge123",
		Mode:      core.WorkPlanModeReplace,
		Tasks: []core.WorkItem{
			{
				ItemKey:            "impl.merge",
				Summary:            "Preserve task metadata",
				Status:             core.WorkItemStatusInProgress,
				ParentTaskKey:      "stage:implementation-plan",
				DependsOn:          []string{"spec:merge"},
				AcceptanceCriteria: []string{"acceptance survives status-only merge"},
				References:         []string{"docs/feature-plans.md"},
				ExternalRefs:       []string{"jira:AWM-42"},
				BlockedReason:      "waiting on follow-up",
				Evidence:           []string{"verifyrun:seed"},
			},
		},
	})
	if err != nil {
		t.Fatalf("seed merge-preservation plan: %v", err)
	}

	_, err = repo.UpsertWorkPlan(ctx, core.WorkPlanUpsertInput{
		ProjectID: projectID,
		PlanKey:   "plan:receipt.merge123",
		ReceiptID: "receipt.merge123",
		Mode:      core.WorkPlanModeMerge,
		Tasks: []core.WorkItem{
			{
				ItemKey:  "impl.merge",
				Summary:  "Preserve task metadata",
				Status:   core.WorkItemStatusComplete,
				Outcome:  "completed after verification",
				Evidence: []string{"verifyrun:final"},
			},
		},
	})
	if err != nil {
		t.Fatalf("merge work plan task: %v", err)
	}

	plan, err := repo.LookupWorkPlan(ctx, core.WorkPlanLookupQuery{
		ProjectID: projectID,
		PlanKey:   "plan:receipt.merge123",
	})
	if err != nil {
		t.Fatalf("lookup merged work plan: %v", err)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected one merged task, got %+v", plan.Tasks)
	}
	task := plan.Tasks[0]
	if task.Status != core.WorkItemStatusComplete || task.Outcome != "completed after verification" {
		t.Fatalf("unexpected merged task state: %+v", task)
	}
	if task.ParentTaskKey != "stage:implementation-plan" {
		t.Fatalf("expected parent task key to survive merge, got %+v", task)
	}
	if !reflect.DeepEqual(task.DependsOn, []string{"spec:merge"}) {
		t.Fatalf("expected depends_on to survive merge, got %+v", task.DependsOn)
	}
	if !reflect.DeepEqual(task.AcceptanceCriteria, []string{"acceptance survives status-only merge"}) {
		t.Fatalf("expected acceptance criteria to survive merge, got %+v", task.AcceptanceCriteria)
	}
	if !reflect.DeepEqual(task.References, []string{"docs/feature-plans.md"}) {
		t.Fatalf("expected references to survive merge, got %+v", task.References)
	}
	if !reflect.DeepEqual(task.ExternalRefs, []string{"jira:AWM-42"}) {
		t.Fatalf("expected external refs to survive merge, got %+v", task.ExternalRefs)
	}
	if task.BlockedReason != "" {
		t.Fatalf("expected blocked reason to clear after non-blocked merge update, got %+v", task)
	}
	if !reflect.DeepEqual(task.Evidence, []string{"verifyrun:final"}) {
		t.Fatalf("expected evidence to update on merge, got %+v", task.Evidence)
	}
}

func runRulePointerSyncRoundTrip(t *testing.T, projectID string, repo ContractRepository) {
	t.Helper()
	ctx := context.Background()

	sourcePath := ".awm/awm-rules.yaml"
	firstPointers := []core.RulePointer{
		{
			PointerKey:  projectID + ":.awm/awm-rules.yaml#rule.alpha",
			SourcePath:  sourcePath,
			RuleID:      "rule.alpha",
			Summary:     "alpha summary",
			Content:     "alpha content",
			Enforcement: "hard",
			Tags:        []string{"ops"},
		},
		{
			PointerKey:  projectID + ":.awm/awm-rules.yaml#rule.beta",
			SourcePath:  sourcePath,
			RuleID:      "rule.beta",
			Summary:     "beta summary",
			Content:     "beta content",
			Enforcement: "soft",
			Tags:        []string{"policy"},
		},
	}

	firstResult, err := repo.SyncRulePointers(ctx, core.RulePointerSyncInput{
		ProjectID:  projectID,
		SourcePath: sourcePath,
		Pointers:   firstPointers,
	})
	if err != nil {
		t.Fatalf("first sync rule pointers: %v", err)
	}
	if firstResult.Upserted != 2 || firstResult.MarkedStale != 0 {
		t.Fatalf("unexpected first sync result: %+v", firstResult)
	}

	if _, lookupErr := repo.LookupPointerByKey(ctx, core.PointerLookupQuery{ProjectID: projectID, PointerKey: firstPointers[0].PointerKey}); lookupErr != nil {
		t.Fatalf("lookup first active rule pointer: %v", lookupErr)
	}
	if _, lookupErr := repo.LookupPointerByKey(ctx, core.PointerLookupQuery{ProjectID: projectID, PointerKey: firstPointers[1].PointerKey}); lookupErr != nil {
		t.Fatalf("lookup second active rule pointer: %v", lookupErr)
	}

	secondResult, err := repo.SyncRulePointers(ctx, core.RulePointerSyncInput{
		ProjectID:  projectID,
		SourcePath: sourcePath,
		Pointers: []core.RulePointer{
			{
				PointerKey:  firstPointers[0].PointerKey,
				SourcePath:  sourcePath,
				RuleID:      "rule.alpha",
				Summary:     "alpha summary updated",
				Content:     "",
				Enforcement: "hard",
				Tags:        []string{"ops"},
			},
		},
	})
	if err != nil {
		t.Fatalf("second sync rule pointers: %v", err)
	}
	if secondResult.Upserted != 1 {
		t.Fatalf("unexpected second upsert count: %d", secondResult.Upserted)
	}
	if secondResult.MarkedStale != 1 {
		t.Fatalf("unexpected second removed count: %d", secondResult.MarkedStale)
	}

	active, err := repo.LookupPointerByKey(ctx, core.PointerLookupQuery{ProjectID: projectID, PointerKey: firstPointers[0].PointerKey})
	if err != nil {
		t.Fatalf("lookup updated active rule pointer: %v", err)
	}
	if active.Description != "alpha summary updated" {
		t.Fatalf("expected empty content fallback to summary, got %q", active.Description)
	}
	if !reflect.DeepEqual(active.Tags, []string{"canonical-rule", "enforcement-hard", "ops", "rule"}) {
		t.Fatalf("unexpected active tags: %+v", active.Tags)
	}

	_, err = repo.LookupPointerByKey(ctx, core.PointerLookupQuery{ProjectID: projectID, PointerKey: firstPointers[1].PointerKey})
	if err == nil {
		t.Fatal("expected stale rule pointer lookup to be hidden")
	}
	if !errors.Is(err, core.ErrPointerLookupNotFound) {
		t.Fatalf("expected ErrPointerLookupNotFound, got %v", err)
	}
}

func runReviewAttemptRoundTrip(t *testing.T, projectID string, repo ContractRepository) {
	t.Helper()
	ctx := context.Background()
	exitCode := 0

	if err := repo.UpsertReceiptScope(ctx, core.ReceiptScope{
		ProjectID: projectID,
		ReceiptID: "receipt-review-1234",
		TaskText:  "repository contract review attempt seed",
		Phase:     "review",
	}); err != nil {
		t.Fatalf("seed receipt scope: %v", err)
	}

	attemptID, err := repo.SaveReviewAttempt(ctx, core.ReviewAttempt{
		ProjectID:          projectID,
		ReceiptID:          "receipt-review-1234",
		PlanKey:            "plan:receipt-review-1234",
		ReviewKey:          "review:cross-llm",
		Summary:            "Cross-LLM review completed",
		Fingerprint:        "sha256:review-attempt",
		Status:             "passed",
		Passed:             true,
		Outcome:            "No blocking issues.",
		WorkflowSourcePath: ".awm/awm-workflows.yaml",
		CommandArgv:        []string{"sh", "-c", "true"},
		CommandCWD:         ".",
		TimeoutSec:         30,
		ExitCode:           &exitCode,
		StdoutExcerpt:      "ok",
		CreatedAt:          time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("save review attempt: %v", err)
	}
	if attemptID <= 0 {
		t.Fatalf("expected positive review attempt id, got %d", attemptID)
	}

	rows, err := repo.ListReviewAttempts(ctx, core.ReviewAttemptListQuery{
		ProjectID: projectID,
		ReceiptID: "receipt-review-1234",
		ReviewKey: "review:cross-llm",
	})
	if err != nil {
		t.Fatalf("list review attempts: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one review attempt, got %+v", rows)
	}
	if rows[0].AttemptID != attemptID || !rows[0].Passed || rows[0].Fingerprint != "sha256:review-attempt" {
		t.Fatalf("unexpected review attempt row: %+v", rows[0])
	}
}

func runReceiptScopeSnapshotRoundTrip(t *testing.T, projectID string, repo ContractRepository) {
	t.Helper()
	ctx := context.Background()

	if _, err := repo.UpsertPointerStubs(ctx, projectID, []core.PointerStub{{
		PointerKey:  "pointer.scope.runtime",
		Path:        "docs/runtime.md",
		Kind:        "doc",
		Label:       "Runtime scope",
		Description: "Original scoped runtime path",
		Tags:        []string{"runtime"},
	}}); err != nil {
		t.Fatalf("seed pointer stub: %v", err)
	}

	if err := repo.UpsertReceiptScope(ctx, core.ReceiptScope{
		ProjectID:         projectID,
		ReceiptID:         "receipt-scope-1234",
		TaskText:          "snapshot receipt scope",
		Phase:             "execute",
		ResolvedTags:      []string{"runtime"},
		PointerKeys:       []string{"pointer.scope.runtime"},
		InitialScopePaths: []string{" ./docs//runtime.md ", "docs/runtime.md"},
		BaselineCaptured:  true,
		BaselinePaths: []core.SyncPath{
			{Path: " internal/service/backend/context.go ", ContentHash: "ctx"},
			{Path: "./docs/runtime.md", ContentHash: "runtime"},
			{Path: "docs/runtime.md", ContentHash: "runtime"},
		},
	}); err != nil {
		t.Fatalf("seed receipt scope snapshot: %v", err)
	}

	if _, err := repo.UpsertPointerStubs(ctx, projectID, []core.PointerStub{{
		PointerKey:  "pointer.scope.runtime",
		Path:        "docs/runtime-renamed.md",
		Kind:        "doc",
		Label:       "Runtime scope",
		Description: "Renamed runtime path",
		Tags:        []string{"runtime"},
	}}); err != nil {
		t.Fatalf("rename pointer stub: %v", err)
	}

	got, err := repo.FetchReceiptScope(ctx, core.ReceiptScopeQuery{
		ProjectID: projectID,
		ReceiptID: "receipt-scope-1234",
	})
	if err != nil {
		t.Fatalf("fetch receipt scope: %v", err)
	}

	if got.ReceiptID != "receipt-scope-1234" || got.ProjectID != projectID || got.TaskText != "snapshot receipt scope" || got.Phase != "execute" {
		t.Fatalf("unexpected receipt scope metadata: %+v", got)
	}
	if !reflect.DeepEqual(got.PointerKeys, []string{"pointer.scope.runtime"}) {
		t.Fatalf("unexpected pointer keys: %+v", got.PointerKeys)
	}
	if !reflect.DeepEqual(got.InitialScopePaths, []string{"docs/runtime.md"}) {
		t.Fatalf("expected stored initial scope path snapshot, got %+v", got.InitialScopePaths)
	}
	if !got.BaselineCaptured {
		t.Fatalf("expected baseline_captured to round-trip as true")
	}
	if gotPaths := syncPaths(got.BaselinePaths); !reflect.DeepEqual(gotPaths, []string{"docs/runtime.md", "internal/service/backend/context.go"}) {
		t.Fatalf("unexpected baseline paths: got %v", gotPaths)
	}
}

func runRunSummaryRoundTrip(t *testing.T, projectID string, repo ContractRepository) {
	t.Helper()
	ctx := context.Background()

	runIDs, err := repo.SaveRunReceiptSummary(ctx, core.RunReceiptSummary{
		ProjectID:    projectID,
		RequestID:    "req-runsummary-12345",
		ReceiptID:    "receipt-runsummary-12345",
		TaskText:     "Verify repository contract parity",
		Phase:        "execute",
		Status:       "accepted",
		ResolvedTags: []string{"parity", "tests"},
		PointerKeys:  []string{"pointer.rule.plan"},
		FilesChanged: []string{"internal/testutil/repositorycontract/repository_contract.go"},
		Outcome:      "Parity suite persisted run summary.",
	})
	if err != nil {
		t.Fatalf("save run receipt summary: %v", err)
	}
	if runIDs.RunID <= 0 {
		t.Fatalf("expected positive run id, got %+v", runIDs)
	}

	rows, err := repo.ListRunHistory(ctx, core.RunHistoryListQuery{
		ProjectID: projectID,
		Query:     "repository contract parity",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("list run history: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one run history row, got %+v", rows)
	}
	if rows[0].RunID != runIDs.RunID || rows[0].ReceiptID != "receipt-runsummary-12345" {
		t.Fatalf("unexpected run history row: %+v", rows[0])
	}

	row, err := repo.LookupRunHistory(ctx, core.RunHistoryLookupQuery{
		ProjectID: projectID,
		RunID:     runIDs.RunID,
	})
	if err != nil {
		t.Fatalf("lookup run history: %v", err)
	}
	if row.RunID != runIDs.RunID || row.RequestID != "req-runsummary-12345" {
		t.Fatalf("unexpected run history lookup: %+v", row)
	}
	if !reflect.DeepEqual(row.FilesChanged, []string{"internal/testutil/repositorycontract/repository_contract.go"}) {
		t.Fatalf("unexpected run history files_changed: %+v", row.FilesChanged)
	}
}

func runRunSummaryPreservesDefinitionOfDoneIssues(t *testing.T, projectID string, repo ContractRepository) {
	t.Helper()
	ctx := context.Background()

	runIDs, err := repo.SaveRunReceiptSummary(ctx, core.RunReceiptSummary{
		ProjectID: projectID,
		ReceiptID: "receipt-runsummary-dod-12345",
		Status:    "accepted_with_warnings",
		DefinitionOfDoneIssues: []string{
			"issue1",
			"issue2",
		},
	})
	if err != nil {
		t.Fatalf("save run receipt summary with DoD issues: %v", err)
	}
	if runIDs.RunID <= 0 {
		t.Fatalf("expected positive run id, got %+v", runIDs)
	}

	row, err := repo.LookupRunHistory(ctx, core.RunHistoryLookupQuery{
		ProjectID: projectID,
		RunID:     runIDs.RunID,
	})
	if err != nil {
		t.Fatalf("lookup run history with DoD issues: %v", err)
	}
	if row.RunID != runIDs.RunID || row.ReceiptID != "receipt-runsummary-dod-12345" || row.Status != "accepted_with_warnings" {
		t.Fatalf("unexpected run history row for DoD issues summary: %+v", row)
	}

	rows, err := repo.ListRunHistory(ctx, core.RunHistoryListQuery{
		ProjectID: projectID,
		Query:     "issue1",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("list run history by DoD issue query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one run history row from DoD issue query, got %+v", rows)
	}
	if rows[0].RunID != runIDs.RunID {
		t.Fatalf("unexpected run id from DoD issue query: %+v", rows[0])
	}
}

func candidateKeys(rows []core.CandidatePointer) []string {
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.Key)
	}
	return keys
}

func syncPaths(rows []core.SyncPath) []string {
	if len(rows) == 0 {
		return nil
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Path)
	}
	return out
}
