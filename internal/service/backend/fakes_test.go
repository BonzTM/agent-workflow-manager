package backend

import (
	"context"
	"sort"
	"strings"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

type fakeRepository struct {
	candidateResults       [][]core.CandidatePointer
	candidateErrors        []error
	inventoryResults       []core.PointerInventory
	inventoryErrors        []error
	scopeResults           []core.ReceiptScope
	scopeErrors            []error
	syncResults            []core.SyncApplyResult
	syncErrors             []error
	upsertStubResults      []int
	upsertStubErrors       []error
	fetchLookupResults     []core.FetchLookup
	fetchLookupErrors      []error
	pointerLookupResults   []core.CandidatePointer
	pointerLookupErrors    []error
	workUpsertResults      []int
	workUpsertErrors       []error
	workListResults        [][]core.WorkItem
	workListErrors         []error
	workPlanUpsertResult   []core.WorkPlanUpsertResult
	workPlanUpsertErrors   []error
	workPlanLookupResult   []core.WorkPlan
	workPlanLookupErrors   []error
	workPlanListResults    [][]core.WorkPlanSummary
	workPlanListErrors     []error
	receiptHistoryResults  [][]core.ReceiptHistorySummary
	receiptHistoryErrors   []error
	runHistoryResults      [][]core.RunHistorySummary
	runHistoryErrors       []error
	runHistoryLookup       []core.RunHistorySummary
	runHistoryLookupErrors []error
	verifySaveErrors       []error
	reviewAttemptResults   [][]core.ReviewAttempt
	reviewAttemptErrors    []error

	candidateCalls         []core.CandidatePointerQuery
	inventoryCalls         []string
	scopeCalls             []core.ReceiptScopeQuery
	receiptUpsertCalls     []core.ReceiptScope
	saveCalls              []core.RunReceiptSummary
	syncCalls              []core.SyncApplyInput
	upsertStubProjectIDs   []string
	upsertStubCalls        [][]core.PointerStub
	fetchLookupCalls       []core.FetchLookupQuery
	pointerLookupCalls     []core.PointerLookupQuery
	workUpsertCalls        []core.WorkItemsUpsertInput
	workListCalls          []core.FetchLookupQuery
	workPlanUpsertCalls    []core.WorkPlanUpsertInput
	workPlanLookupCalls    []core.WorkPlanLookupQuery
	workPlanListCalls      []core.WorkPlanListQuery
	receiptHistoryCalls    []core.ReceiptHistoryListQuery
	runHistoryCalls        []core.RunHistoryListQuery
	runHistoryLookupCalls  []core.RunHistoryLookupQuery
	verifySaveCalls        []core.VerificationBatch
	reviewAttemptCalls     []core.ReviewAttempt
	reviewAttemptListCalls []core.ReviewAttemptListQuery

	saveResult         core.RunReceiptIDs
	saveError          error
	receiptUpsertError error
	storedWorkPlans    map[string]core.WorkPlan
}

func (f *fakeRepository) FetchCandidatePointers(_ context.Context, input core.CandidatePointerQuery) ([]core.CandidatePointer, error) {
	f.candidateCalls = append(f.candidateCalls, input)
	idx := len(f.candidateCalls) - 1
	if idx < len(f.candidateErrors) && f.candidateErrors[idx] != nil {
		return nil, f.candidateErrors[idx]
	}
	if idx >= len(f.candidateResults) {
		return nil, nil
	}
	return append([]core.CandidatePointer(nil), f.candidateResults[idx]...), nil
}

func (f *fakeRepository) ListPointerInventory(_ context.Context, projectID string) ([]core.PointerInventory, error) {
	f.inventoryCalls = append(f.inventoryCalls, strings.TrimSpace(projectID))
	idx := len(f.inventoryCalls) - 1
	if idx < len(f.inventoryErrors) && f.inventoryErrors[idx] != nil {
		return nil, f.inventoryErrors[idx]
	}
	if len(f.inventoryResults) == 0 {
		return nil, nil
	}
	return append([]core.PointerInventory(nil), f.inventoryResults...), nil
}

func (f *fakeRepository) UpsertPointerStubs(_ context.Context, projectID string, stubs []core.PointerStub) (int, error) {
	f.upsertStubProjectIDs = append(f.upsertStubProjectIDs, strings.TrimSpace(projectID))
	copied := make([]core.PointerStub, 0, len(stubs))
	for _, stub := range stubs {
		copied = append(copied, core.PointerStub{
			PointerKey:  stub.PointerKey,
			Path:        stub.Path,
			Kind:        stub.Kind,
			Label:       stub.Label,
			Description: stub.Description,
			Tags:        append([]string(nil), stub.Tags...),
		})
	}
	f.upsertStubCalls = append(f.upsertStubCalls, copied)

	idx := len(f.upsertStubCalls) - 1
	if idx < len(f.upsertStubErrors) && f.upsertStubErrors[idx] != nil {
		return 0, f.upsertStubErrors[idx]
	}
	if idx < len(f.upsertStubResults) {
		return f.upsertStubResults[idx], nil
	}
	return len(copied), nil
}

func (f *fakeRepository) FetchReceiptScope(_ context.Context, input core.ReceiptScopeQuery) (core.ReceiptScope, error) {
	f.scopeCalls = append(f.scopeCalls, input)
	idx := len(f.scopeCalls) - 1
	if idx < len(f.scopeErrors) && f.scopeErrors[idx] != nil {
		return core.ReceiptScope{}, f.scopeErrors[idx]
	}
	if idx >= len(f.scopeResults) {
		return core.ReceiptScope{}, core.ErrReceiptScopeNotFound
	}
	scope := f.scopeResults[idx]
	scope.ResolvedTags = append([]string(nil), scope.ResolvedTags...)
	scope.PointerKeys = append([]string(nil), scope.PointerKeys...)
	scope.InitialScopePaths = append([]string(nil), scope.InitialScopePaths...)
	scope.BaselinePaths = append([]core.SyncPath(nil), scope.BaselinePaths...)
	return scope, nil
}

func (f *fakeRepository) UpsertReceiptScope(_ context.Context, input core.ReceiptScope) error {
	f.receiptUpsertCalls = append(f.receiptUpsertCalls, core.ReceiptScope{
		ProjectID:         strings.TrimSpace(input.ProjectID),
		ReceiptID:         strings.TrimSpace(input.ReceiptID),
		TaskText:          strings.TrimSpace(input.TaskText),
		Phase:             strings.TrimSpace(input.Phase),
		ResolvedTags:      append([]string(nil), input.ResolvedTags...),
		PointerKeys:       append([]string(nil), input.PointerKeys...),
		InitialScopePaths: append([]string(nil), input.InitialScopePaths...),
		BaselineCaptured:  input.BaselineCaptured,
		BaselinePaths:     append([]core.SyncPath(nil), input.BaselinePaths...),
	})
	return f.receiptUpsertError
}

func (f *fakeRepository) LookupFetchState(_ context.Context, input core.FetchLookupQuery) (core.FetchLookup, error) {
	f.fetchLookupCalls = append(f.fetchLookupCalls, input)
	idx := len(f.fetchLookupCalls) - 1
	if idx < len(f.fetchLookupErrors) && f.fetchLookupErrors[idx] != nil {
		return core.FetchLookup{}, f.fetchLookupErrors[idx]
	}
	if idx >= len(f.fetchLookupResults) {
		return core.FetchLookup{}, core.ErrFetchLookupNotFound
	}

	lookup := f.fetchLookupResults[idx]
	lookup.ProjectID = strings.TrimSpace(lookup.ProjectID)
	lookup.ReceiptID = strings.TrimSpace(lookup.ReceiptID)
	lookup.RunStatus = strings.TrimSpace(lookup.RunStatus)
	lookup.WorkItems = append([]core.WorkItem(nil), lookup.WorkItems...)
	return lookup, nil
}

func (f *fakeRepository) LookupPointerByKey(_ context.Context, input core.PointerLookupQuery) (core.CandidatePointer, error) {
	f.pointerLookupCalls = append(f.pointerLookupCalls, core.PointerLookupQuery{
		ProjectID:  strings.TrimSpace(input.ProjectID),
		PointerKey: strings.TrimSpace(input.PointerKey),
	})

	idx := len(f.pointerLookupCalls) - 1
	if idx < len(f.pointerLookupErrors) && f.pointerLookupErrors[idx] != nil {
		return core.CandidatePointer{}, f.pointerLookupErrors[idx]
	}
	if idx >= len(f.pointerLookupResults) {
		return core.CandidatePointer{}, core.ErrPointerLookupNotFound
	}
	return f.pointerLookupResults[idx], nil
}

func (f *fakeRepository) SaveReviewAttempt(_ context.Context, input core.ReviewAttempt) (int64, error) {
	f.reviewAttemptCalls = append(f.reviewAttemptCalls, input)
	idx := len(f.reviewAttemptCalls) - 1
	if idx < len(f.reviewAttemptErrors) && f.reviewAttemptErrors[idx] != nil {
		return 0, f.reviewAttemptErrors[idx]
	}
	return int64(idx + 1), nil
}

func (f *fakeRepository) ListReviewAttempts(_ context.Context, input core.ReviewAttemptListQuery) ([]core.ReviewAttempt, error) {
	f.reviewAttemptListCalls = append(f.reviewAttemptListCalls, core.ReviewAttemptListQuery{
		ProjectID: strings.TrimSpace(input.ProjectID),
		ReceiptID: strings.TrimSpace(input.ReceiptID),
		ReviewKey: strings.TrimSpace(input.ReviewKey),
	})
	idx := len(f.reviewAttemptListCalls) - 1
	if idx < len(f.reviewAttemptErrors) && f.reviewAttemptErrors[idx] != nil {
		return nil, f.reviewAttemptErrors[idx]
	}
	if idx < len(f.reviewAttemptResults) {
		return append([]core.ReviewAttempt(nil), f.reviewAttemptResults[idx]...), nil
	}
	if len(f.reviewAttemptCalls) == 0 {
		return nil, nil
	}
	out := make([]core.ReviewAttempt, 0, len(f.reviewAttemptCalls))
	for i, attempt := range f.reviewAttemptCalls {
		copied := attempt
		if copied.AttemptID == 0 {
			copied.AttemptID = int64(i + 1)
		}
		out = append(out, copied)
	}
	return out, nil
}

func (f *fakeRepository) UpsertWorkItems(_ context.Context, input core.WorkItemsUpsertInput) (int, error) {
	f.workUpsertCalls = append(f.workUpsertCalls, core.WorkItemsUpsertInput{
		ProjectID: strings.TrimSpace(input.ProjectID),
		ReceiptID: strings.TrimSpace(input.ReceiptID),
		Items:     append([]core.WorkItem(nil), input.Items...),
	})

	idx := len(f.workUpsertCalls) - 1
	if idx < len(f.workUpsertErrors) && f.workUpsertErrors[idx] != nil {
		return 0, f.workUpsertErrors[idx]
	}
	if idx < len(f.workUpsertResults) {
		return f.workUpsertResults[idx], nil
	}
	return len(input.Items), nil
}

func (f *fakeRepository) ListWorkItems(_ context.Context, input core.FetchLookupQuery) ([]core.WorkItem, error) {
	f.workListCalls = append(f.workListCalls, core.FetchLookupQuery{
		ProjectID: strings.TrimSpace(input.ProjectID),
		ReceiptID: strings.TrimSpace(input.ReceiptID),
	})

	idx := len(f.workListCalls) - 1
	if idx < len(f.workListErrors) && f.workListErrors[idx] != nil {
		return nil, f.workListErrors[idx]
	}
	if idx >= len(f.workListResults) {
		return nil, nil
	}
	return append([]core.WorkItem(nil), f.workListResults[idx]...), nil
}

func (f *fakeRepository) UpsertWorkPlan(_ context.Context, input core.WorkPlanUpsertInput) (core.WorkPlanUpsertResult, error) {
	f.workPlanUpsertCalls = append(f.workPlanUpsertCalls, input)
	idx := len(f.workPlanUpsertCalls) - 1
	if idx < len(f.workPlanUpsertErrors) && f.workPlanUpsertErrors[idx] != nil {
		return core.WorkPlanUpsertResult{}, f.workPlanUpsertErrors[idx]
	}
	if idx < len(f.workPlanUpsertResult) {
		f.storeWorkPlan(f.workPlanUpsertResult[idx].Plan)
		return f.workPlanUpsertResult[idx], nil
	}

	plan := f.buildStoredWorkPlan(input)
	if strings.TrimSpace(plan.Status) == "" {
		plan.Status = core.PlanStatusPending
	}
	f.storeWorkPlan(plan)
	return core.WorkPlanUpsertResult{
		Plan:    plan,
		Updated: len(input.Tasks),
	}, nil
}

func (f *fakeRepository) LookupWorkPlan(_ context.Context, input core.WorkPlanLookupQuery) (core.WorkPlan, error) {
	f.workPlanLookupCalls = append(f.workPlanLookupCalls, input)
	idx := len(f.workPlanLookupCalls) - 1
	if idx < len(f.workPlanLookupErrors) && f.workPlanLookupErrors[idx] != nil {
		return core.WorkPlan{}, f.workPlanLookupErrors[idx]
	}
	if idx >= len(f.workPlanLookupResult) {
		if plan, ok := f.lookupStoredWorkPlan(strings.TrimSpace(input.ProjectID), strings.TrimSpace(input.PlanKey)); ok {
			return plan, nil
		}
		return core.WorkPlan{}, core.ErrWorkPlanNotFound
	}
	return f.workPlanLookupResult[idx], nil
}

func (f *fakeRepository) ListWorkPlans(_ context.Context, input core.WorkPlanListQuery) ([]core.WorkPlanSummary, error) {
	f.workPlanListCalls = append(f.workPlanListCalls, input)
	idx := len(f.workPlanListCalls) - 1
	if idx < len(f.workPlanListErrors) && f.workPlanListErrors[idx] != nil {
		return nil, f.workPlanListErrors[idx]
	}
	if idx >= len(f.workPlanListResults) {
		return f.listStoredWorkPlans(strings.TrimSpace(input.ProjectID), strings.TrimSpace(input.Scope)), nil
	}
	return append([]core.WorkPlanSummary(nil), f.workPlanListResults[idx]...), nil
}

func (f *fakeRepository) buildStoredWorkPlan(input core.WorkPlanUpsertInput) core.WorkPlan {
	plan, found := f.lookupStoredWorkPlan(strings.TrimSpace(input.ProjectID), strings.TrimSpace(input.PlanKey))
	if !found || input.Mode == core.WorkPlanModeReplace {
		plan = core.WorkPlan{
			ProjectID: strings.TrimSpace(input.ProjectID),
			PlanKey:   strings.TrimSpace(input.PlanKey),
		}
	}

	plan.ProjectID = strings.TrimSpace(input.ProjectID)
	plan.PlanKey = strings.TrimSpace(input.PlanKey)
	if receiptID := strings.TrimSpace(input.ReceiptID); receiptID != "" || !found || input.Mode == core.WorkPlanModeReplace {
		plan.ReceiptID = receiptID
	}
	if title := strings.TrimSpace(input.Title); title != "" || input.Mode == core.WorkPlanModeReplace {
		plan.Title = title
	}
	if objective := strings.TrimSpace(input.Objective); objective != "" || input.Mode == core.WorkPlanModeReplace {
		plan.Objective = objective
	}
	if kind := strings.TrimSpace(input.Kind); kind != "" || input.Mode == core.WorkPlanModeReplace {
		plan.Kind = kind
	}
	if parentPlanKey := strings.TrimSpace(input.ParentPlanKey); parentPlanKey != "" || input.Mode == core.WorkPlanModeReplace {
		plan.ParentPlanKey = parentPlanKey
	}
	if status := strings.TrimSpace(input.Status); status != "" || input.Mode == core.WorkPlanModeReplace {
		plan.Status = status
	}
	if input.Mode == core.WorkPlanModeReplace || strings.TrimSpace(input.Stages.SpecOutline) != "" || strings.TrimSpace(input.Stages.RefinedSpec) != "" || strings.TrimSpace(input.Stages.ImplementationPlan) != "" {
		plan.Stages = mergeFakeWorkPlanStages(plan.Stages, input.Stages, input.Mode)
	}
	if input.Mode == core.WorkPlanModeReplace || len(input.InScope) > 0 {
		plan.InScope = append([]string(nil), input.InScope...)
	}
	if input.Mode == core.WorkPlanModeReplace || len(input.OutOfScope) > 0 {
		plan.OutOfScope = append([]string(nil), input.OutOfScope...)
	}
	if input.Mode == core.WorkPlanModeReplace || len(input.DiscoveredPaths) > 0 {
		plan.DiscoveredPaths = append([]string(nil), input.DiscoveredPaths...)
	}
	if input.Mode == core.WorkPlanModeReplace || len(input.Constraints) > 0 {
		plan.Constraints = append([]string(nil), input.Constraints...)
	}
	if input.Mode == core.WorkPlanModeReplace || len(input.References) > 0 {
		plan.References = append([]string(nil), input.References...)
	}
	if input.Mode == core.WorkPlanModeReplace || len(input.ExternalRefs) > 0 {
		plan.ExternalRefs = append([]string(nil), input.ExternalRefs...)
	}
	if input.Mode == core.WorkPlanModeReplace {
		plan.Tasks = append([]core.WorkItem(nil), input.Tasks...)
	} else if len(input.Tasks) > 0 {
		plan.Tasks = mergeFakeWorkPlanTasks(plan.Tasks, input.Tasks)
	}
	return plan
}

func (f *fakeRepository) storeWorkPlan(plan core.WorkPlan) {
	projectID := strings.TrimSpace(plan.ProjectID)
	planKey := strings.TrimSpace(plan.PlanKey)
	if projectID == "" || planKey == "" {
		return
	}
	if f.storedWorkPlans == nil {
		f.storedWorkPlans = make(map[string]core.WorkPlan)
	}
	f.storedWorkPlans[fakeWorkPlanStorageKey(projectID, planKey)] = plan
}

func (f *fakeRepository) lookupStoredWorkPlan(projectID, planKey string) (core.WorkPlan, bool) {
	if f == nil || f.storedWorkPlans == nil || projectID == "" || planKey == "" {
		return core.WorkPlan{}, false
	}
	plan, ok := f.storedWorkPlans[fakeWorkPlanStorageKey(projectID, planKey)]
	return plan, ok
}

func (f *fakeRepository) listStoredWorkPlans(projectID, scope string) []core.WorkPlanSummary {
	if f == nil || len(f.storedWorkPlans) == 0 || projectID == "" {
		return nil
	}

	out := make([]core.WorkPlanSummary, 0, len(f.storedWorkPlans))
	for _, plan := range f.storedWorkPlans {
		if strings.TrimSpace(plan.ProjectID) != projectID {
			continue
		}
		status := normalizePlanStatus(plan.Status)
		switch strings.TrimSpace(scope) {
		case string(v1.HistoryScopeCompleted):
			if !isTerminalPlanStatus(status) {
				continue
			}
		case string(v1.HistoryScopeCurrent), "":
			if isTerminalPlanStatus(status) {
				continue
			}
		}

		tasks := normalizeWorkItems(plan.Tasks)
		activeTaskKeys := make([]string, 0, len(tasks))
		summary := core.WorkPlanSummary{
			ReceiptID:     strings.TrimSpace(plan.ReceiptID),
			Title:         strings.TrimSpace(plan.Title),
			Objective:     strings.TrimSpace(plan.Objective),
			PlanKey:       strings.TrimSpace(plan.PlanKey),
			Summary:       strings.TrimSpace(plan.Title),
			Status:        status,
			Kind:          strings.TrimSpace(plan.Kind),
			ParentPlanKey: strings.TrimSpace(plan.ParentPlanKey),
		}
		for _, task := range tasks {
			switch normalizeWorkItemStatus(task.Status) {
			case core.WorkItemStatusPending:
				summary.TaskCountPending++
				activeTaskKeys = append(activeTaskKeys, task.ItemKey)
			case core.WorkItemStatusInProgress:
				summary.TaskCountInProgress++
				activeTaskKeys = append(activeTaskKeys, task.ItemKey)
			case core.WorkItemStatusBlocked:
				summary.TaskCountBlocked++
				activeTaskKeys = append(activeTaskKeys, task.ItemKey)
			case core.WorkItemStatusComplete:
				summary.TaskCountComplete++
			}
		}
		summary.TaskCountTotal = len(tasks)
		summary.ActiveTaskKeys = activeTaskKeys
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].PlanKey < out[j].PlanKey
	})
	return out
}

func fakeWorkPlanStorageKey(projectID, planKey string) string {
	return projectID + "\x00" + planKey
}

func mergeFakeWorkPlanStages(current, incoming core.WorkPlanStages, mode core.WorkPlanMode) core.WorkPlanStages {
	if mode == core.WorkPlanModeReplace {
		return incoming
	}
	next := current
	if value := strings.TrimSpace(incoming.SpecOutline); value != "" {
		next.SpecOutline = value
	}
	if value := strings.TrimSpace(incoming.RefinedSpec); value != "" {
		next.RefinedSpec = value
	}
	if value := strings.TrimSpace(incoming.ImplementationPlan); value != "" {
		next.ImplementationPlan = value
	}
	return next
}

func mergeFakeWorkPlanTasks(current, incoming []core.WorkItem) []core.WorkItem {
	byKey := make(map[string]core.WorkItem, len(current)+len(incoming))
	for _, item := range normalizeWorkItems(current) {
		byKey[item.ItemKey] = item
	}
	for _, item := range normalizeWorkItems(incoming) {
		byKey[item.ItemKey] = item
	}

	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	merged := make([]core.WorkItem, 0, len(keys))
	for _, key := range keys {
		merged = append(merged, byKey[key])
	}
	return merged
}

func (f *fakeRepository) ListReceiptHistory(_ context.Context, input core.ReceiptHistoryListQuery) ([]core.ReceiptHistorySummary, error) {
	f.receiptHistoryCalls = append(f.receiptHistoryCalls, input)
	idx := len(f.receiptHistoryCalls) - 1
	if idx < len(f.receiptHistoryErrors) && f.receiptHistoryErrors[idx] != nil {
		return nil, f.receiptHistoryErrors[idx]
	}
	if idx >= len(f.receiptHistoryResults) {
		return nil, nil
	}
	return append([]core.ReceiptHistorySummary(nil), f.receiptHistoryResults[idx]...), nil
}

func (f *fakeRepository) ListRunHistory(_ context.Context, input core.RunHistoryListQuery) ([]core.RunHistorySummary, error) {
	f.runHistoryCalls = append(f.runHistoryCalls, input)
	idx := len(f.runHistoryCalls) - 1
	if idx < len(f.runHistoryErrors) && f.runHistoryErrors[idx] != nil {
		return nil, f.runHistoryErrors[idx]
	}
	if idx >= len(f.runHistoryResults) {
		return nil, nil
	}
	return append([]core.RunHistorySummary(nil), f.runHistoryResults[idx]...), nil
}

func (f *fakeRepository) LookupRunHistory(_ context.Context, input core.RunHistoryLookupQuery) (core.RunHistorySummary, error) {
	f.runHistoryLookupCalls = append(f.runHistoryLookupCalls, input)
	idx := len(f.runHistoryLookupCalls) - 1
	if idx < len(f.runHistoryLookupErrors) && f.runHistoryLookupErrors[idx] != nil {
		return core.RunHistorySummary{}, f.runHistoryLookupErrors[idx]
	}
	if idx >= len(f.runHistoryLookup) {
		return core.RunHistorySummary{}, core.ErrFetchLookupNotFound
	}
	return f.runHistoryLookup[idx], nil
}

func (f *fakeRepository) SaveRunReceiptSummary(_ context.Context, input core.RunReceiptSummary) (core.RunReceiptIDs, error) {
	f.saveCalls = append(f.saveCalls, input)
	if f.saveError != nil {
		return core.RunReceiptIDs{}, f.saveError
	}
	if f.saveResult.RunID == 0 {
		return core.RunReceiptIDs{RunID: 1, ReceiptID: input.ReceiptID}, nil
	}
	return f.saveResult, nil
}

func (f *fakeRepository) SaveVerificationBatch(_ context.Context, input core.VerificationBatch) error {
	copied := core.VerificationBatch{
		BatchRunID:      strings.TrimSpace(input.BatchRunID),
		ProjectID:       strings.TrimSpace(input.ProjectID),
		ReceiptID:       strings.TrimSpace(input.ReceiptID),
		PlanKey:         strings.TrimSpace(input.PlanKey),
		Phase:           strings.TrimSpace(input.Phase),
		TestsSourcePath: strings.TrimSpace(input.TestsSourcePath),
		Status:          strings.TrimSpace(input.Status),
		Passed:          input.Passed,
		SelectedTestIDs: append([]string(nil), input.SelectedTestIDs...),
		CreatedAt:       input.CreatedAt,
	}
	copied.Results = make([]core.VerificationTestRun, 0, len(input.Results))
	for _, result := range input.Results {
		copied.Results = append(copied.Results, core.VerificationTestRun{
			BatchRunID:       strings.TrimSpace(result.BatchRunID),
			ProjectID:        strings.TrimSpace(result.ProjectID),
			TestID:           strings.TrimSpace(result.TestID),
			DefinitionHash:   strings.TrimSpace(result.DefinitionHash),
			Summary:          strings.TrimSpace(result.Summary),
			CommandArgv:      append([]string(nil), result.CommandArgv...),
			CommandCWD:       strings.TrimSpace(result.CommandCWD),
			TimeoutSec:       result.TimeoutSec,
			ExpectedExitCode: result.ExpectedExitCode,
			SelectionReasons: append([]string(nil), result.SelectionReasons...),
			Status:           strings.TrimSpace(result.Status),
			ExitCode:         result.ExitCode,
			DurationMS:       result.DurationMS,
			StdoutExcerpt:    strings.TrimSpace(result.StdoutExcerpt),
			StderrExcerpt:    strings.TrimSpace(result.StderrExcerpt),
			StartedAt:        result.StartedAt,
			FinishedAt:       result.FinishedAt,
		})
	}
	f.verifySaveCalls = append(f.verifySaveCalls, copied)
	idx := len(f.verifySaveCalls) - 1
	if idx < len(f.verifySaveErrors) && f.verifySaveErrors[idx] != nil {
		return f.verifySaveErrors[idx]
	}
	return nil
}

func (f *fakeRepository) ApplySync(_ context.Context, input core.SyncApplyInput) (core.SyncApplyResult, error) {
	f.syncCalls = append(f.syncCalls, input)
	idx := len(f.syncCalls) - 1
	if idx < len(f.syncErrors) && f.syncErrors[idx] != nil {
		return core.SyncApplyResult{}, f.syncErrors[idx]
	}
	if idx < len(f.syncResults) {
		return f.syncResults[idx], nil
	}
	return core.SyncApplyResult{}, nil
}
