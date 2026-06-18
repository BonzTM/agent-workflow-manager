package backend

import (
	"context"
	"strings"

	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func (s *Service) syncTerminalWorkPlanStatus(ctx context.Context, projectID, receiptID string, plan core.WorkPlan, items []core.WorkItem) (core.WorkPlan, bool, *core.APIError) {
	if s == nil || s.planRepo == nil {
		return plan, false, nil
	}

	planKey := strings.TrimSpace(plan.PlanKey)
	if planKey == "" {
		return plan, false, nil
	}

	normalizedItems := normalizeWorkItems(items)
	if len(normalizedItems) == 0 {
		return plan, false, nil
	}

	derivedStatus := derivePlanStatusFromWorkItems(normalizedItems)
	if !isTerminalPlanStatus(derivedStatus) {
		return plan, false, nil
	}
	derivedStages := deriveTerminalWorkPlanStages(plan.Stages, normalizedItems)

	currentStatus := normalizePlanStatus(plan.Status)
	if currentStatus == derivedStatus && workPlanStagesEqual(plan.Stages, derivedStages) {
		return plan, false, nil
	}

	upsertResult, err := s.planRepo.UpsertWorkPlan(ctx, core.WorkPlanUpsertInput{
		ProjectID: strings.TrimSpace(projectID),
		PlanKey:   planKey,
		ReceiptID: firstNonEmpty(strings.TrimSpace(receiptID), strings.TrimSpace(plan.ReceiptID)),
		Mode:      core.WorkPlanModeMerge,
		Status:    derivedStatus,
		Stages:    derivedStages,
	})
	if err != nil {
		return core.WorkPlan{}, false, workInternalError("autoclose_work_plan", err)
	}
	return upsertResult.Plan, true, nil
}

func deriveTerminalWorkPlanStages(existing core.WorkPlanStages, items []core.WorkItem) core.WorkPlanStages {
	derived := existing
	for _, item := range items {
		status := normalizeWorkItemStatus(item.Status)
		switch strings.TrimSpace(item.ItemKey) {
		case "stage:spec-outline":
			derived.SpecOutline = status
		case "stage:refined-spec":
			derived.RefinedSpec = status
		case "stage:implementation-plan":
			derived.ImplementationPlan = status
		}
	}
	return derived
}

func workPlanStagesEqual(left, right core.WorkPlanStages) bool {
	return normalizeWorkItemStatus(left.SpecOutline) == normalizeWorkItemStatus(right.SpecOutline) &&
		normalizeWorkItemStatus(left.RefinedSpec) == normalizeWorkItemStatus(right.RefinedSpec) &&
		normalizeWorkItemStatus(left.ImplementationPlan) == normalizeWorkItemStatus(right.ImplementationPlan)
}
