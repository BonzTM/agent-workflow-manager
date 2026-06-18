package backend

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/core"
)

const stalePlanWarningAge = 7 * 24 * time.Hour

type planDiagnostics struct {
	stale                  []string
	terminalStatusDrift    []string
	administrativeCloseout []string
}

func (s *Service) collectPlanDiagnostics(ctx context.Context, projectID string, now time.Time) (planDiagnostics, *core.APIError) {
	if s == nil || s.planRepo == nil {
		return planDiagnostics{}, nil
	}

	rows, err := s.planRepo.ListWorkPlans(ctx, core.WorkPlanListQuery{
		ProjectID: strings.TrimSpace(projectID),
		Scope:     "all",
		Unbounded: true,
	})
	if err != nil {
		return planDiagnostics{}, internalError("list_work_plans", err)
	}

	diagnostics := planDiagnostics{}
	for _, row := range rows {
		status := normalizePlanStatus(row.Status)
		if isTerminalPlanStatus(status) {
			continue
		}

		planKey := strings.TrimSpace(row.PlanKey)
		if planKey == "" {
			continue
		}

		if !row.UpdatedAt.IsZero() && now.Sub(row.UpdatedAt.UTC()) >= stalePlanWarningAge {
			diagnostics.stale = append(diagnostics.stale, fmt.Sprintf("%s status=%s updated_at=%s", planKey, status, row.UpdatedAt.UTC().Format(time.RFC3339)))
		}

		plan, err := s.planRepo.LookupWorkPlan(ctx, core.WorkPlanLookupQuery{
			ProjectID: strings.TrimSpace(projectID),
			PlanKey:   planKey,
			ReceiptID: strings.TrimSpace(row.ReceiptID),
		})
		if err != nil {
			if errors.Is(err, core.ErrWorkPlanNotFound) {
				continue
			}
			return planDiagnostics{}, internalError("lookup_work_plan", err)
		}

		tasks := normalizeWorkItems(plan.Tasks)
		if len(tasks) == 0 {
			continue
		}

		openTasks := make([]core.WorkItem, 0, len(tasks))
		for _, task := range tasks {
			if isTerminalWorkItemStatus(task.Status) {
				continue
			}
			openTasks = append(openTasks, task)
		}

		if len(openTasks) == 0 {
			derived := derivePlanStatusFromWorkItems(tasks)
			diagnostics.terminalStatusDrift = append(diagnostics.terminalStatusDrift, fmt.Sprintf("%s status=%s should_be=%s", planKey, status, derived))
			continue
		}

		if len(openTasks) == 1 && looksAdministrativeCloseoutTask(openTasks[0]) {
			diagnostics.administrativeCloseout = append(diagnostics.administrativeCloseout, fmt.Sprintf("%s remaining_task=%s", planKey, openTasks[0].ItemKey))
		}
	}

	return diagnostics, nil
}

func looksAdministrativeCloseoutTask(task core.WorkItem) bool {
	text := strings.ToLower(strings.TrimSpace(task.ItemKey + " " + task.Summary))
	if text == "" {
		return false
	}

	for _, needle := range []string{
		"strict-close",
		"closeout",
		"close out",
		"report completion",
		"report_completion",
		"report-completion",
		"close plan",
		"close task",
		"administrative close",
	} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
