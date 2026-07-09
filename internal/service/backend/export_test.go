package backend

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func TestExportFetchPlanRendersStructuredJSON(t *testing.T) {
	repo := &fakeRepository{
		workPlanLookupResult: []core.WorkPlan{{
			ProjectID: "project.alpha",
			PlanKey:   "plan:receipt-12345678",
			ReceiptID: "receipt-12345678",
			Title:     "Export renderer implementation",
			Objective: "Ship the backend export renderer",
			Kind:      "feature",
			Status:    core.PlanStatusInProgress,
			Stages: core.WorkPlanStages{
				SpecOutline:        core.WorkItemStatusComplete,
				RefinedSpec:        core.WorkItemStatusComplete,
				ImplementationPlan: core.WorkItemStatusInProgress,
			},
			InScope:      []string{"internal/service/backend/export.go"},
			References:   []string{"AGENTS.md"},
			Constraints:  []string{"Keep renderers deterministic"},
			ExternalRefs: []string{"plan:receipt-12345678"},
			Tasks: []core.WorkItem{
				{
					ItemKey:            "impl:renderer-json-content",
					Summary:            "Render stable JSON content",
					Status:             core.WorkItemStatusInProgress,
					AcceptanceCriteria: []string{"export content is deterministic"},
				},
			},
		}},
	}

	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Export(context.Background(), v1.ExportPayload{
		ProjectID: "project.alpha",
		Format:    v1.ExportFormatJSON,
		Fetch: &v1.ExportFetchSelector{
			Keys: []string{"plan:receipt-12345678"},
		},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Document == nil || result.Document.Kind != v1.ExportDocumentKindPlan {
		t.Fatalf("expected plan export document, got %+v", result.Document)
	}
	if result.Document.Plan == nil {
		t.Fatal("expected plan payload")
	}
	if got := result.Document.Plan.PlanKey; got != "plan:receipt-12345678" {
		t.Fatalf("unexpected plan key: %q", got)
	}
	if got := len(result.Document.Plan.Tasks); got != 1 {
		t.Fatalf("unexpected task count: got %d want 1", got)
	}

	var rendered v1.ExportDocument
	if err := json.Unmarshal([]byte(result.Content), &rendered); err != nil {
		t.Fatalf("unmarshal rendered content: %v", err)
	}
	if rendered.Kind != v1.ExportDocumentKindPlan {
		t.Fatalf("unexpected rendered kind: %q", rendered.Kind)
	}
	if rendered.Plan == nil || rendered.Plan.Tasks[0].Key != "impl:renderer-json-content" {
		t.Fatalf("unexpected rendered plan payload: %+v", rendered.Plan)
	}
}

func TestExportContextMarkdownIncludesReceiptSections(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, ".awm/awm-rules.yaml", strings.Join([]string{
		"version: awm.rules.v1",
		"rules:",
		"  - id: export_rule",
		"    summary: Export rules stay deterministic",
		"    content: Render AWM-owned artifacts with stable section ordering.",
		"    enforcement: hard",
		"    tags: [backend]",
		"",
	}, "\n"))
	withWorkingDir(t, root)

	repo := &fakeRepository{
		workPlanListResults: [][]core.WorkPlanSummary{{
			{
				PlanKey:   "plan:receipt-12345678",
				Summary:   "Exportable AWM artifacts",
				Status:    core.PlanStatusInProgress,
				ReceiptID: "receipt-12345678",
			},
		}},
	}

	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Export(context.Background(), v1.ExportPayload{
		ProjectID: "project.alpha",
		Format:    v1.ExportFormatMarkdown,
		Context: &v1.ExportContextSelector{
			TaskText:          "continue export renderer implementation",
			Phase:             v1.PhaseExecute,
			InitialScopePaths: []string{"internal/service/backend/export.go"},
		},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Document == nil || result.Document.Kind != v1.ExportDocumentKindContext {
		t.Fatalf("expected context export document, got %+v", result.Document)
	}
	if result.Document.Context == nil || result.Document.Context.Meta.TaskText == "" {
		t.Fatalf("expected context receipt payload, got %+v", result.Document.Context)
	}
	for _, needle := range []string{"# Context", "## Rules", "## Plans", "## Initial Scope"} {
		if !strings.Contains(result.Content, needle) {
			t.Fatalf("expected markdown to contain %q, got:\n%s", needle, result.Content)
		}
	}
}

func TestExportHistoryMarkdownIncludesWorkMetadata(t *testing.T) {
	repo := &fakeRepository{
		workPlanListResults: [][]core.WorkPlanSummary{{
			{
				PlanKey:             "plan:receipt-12345678",
				ReceiptID:           "receipt-12345678",
				Summary:             "Export renderer implementation",
				Status:              core.PlanStatusInProgress,
				Kind:                "feature",
				ParentPlanKey:       "plan:receipt-parent",
				TaskCountTotal:      4,
				TaskCountPending:    1,
				TaskCountInProgress: 2,
				TaskCountBlocked:    1,
				TaskCountComplete:   0,
				UpdatedAt:           time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
			},
		}},
	}

	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Export(context.Background(), v1.ExportPayload{
		ProjectID: "project.alpha",
		Format:    v1.ExportFormatMarkdown,
		History: &v1.ExportHistorySelector{
			Entity: v1.HistoryEntityWork,
			Scope:  v1.HistoryScopeCurrent,
			Limit:  10,
		},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Document == nil || result.Document.Kind != v1.ExportDocumentKindHistory {
		t.Fatalf("expected history export document, got %+v", result.Document)
	}
	if result.Document.History == nil || result.Document.History.Count != 1 {
		t.Fatalf("unexpected history payload: %+v", result.Document.History)
	}
	if !strings.Contains(result.Content, "Task Counts: total=4 pending=1 in_progress=2 blocked=1 complete=0") {
		t.Fatalf("expected task counts in markdown, got:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "plan:receipt-12345678") {
		t.Fatalf("expected plan key in markdown, got:\n%s", result.Content)
	}
}

func TestExportStatusMarkdownIncludesProjectAndContextPreview(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	writeRepoFile(t, root, ".awm/awm-rules.yaml", "version: awm.rules.v1\nrules:\n  - summary: Keep export deterministic\n")
	writeRepoFile(t, root, ".awm/awm-tags.yaml", "version: awm.tags.v1\ncanonical_tags:\n  backend:\n    - export\n")
	writeRepoFile(t, root, ".awm/awm-tests.yaml", "version: awm.tests.v1\ndefaults:\n  cwd: .\n  timeout_sec: 120\ntests:\n  - id: smoke\n    summary: Run smoke tests\n    command:\n      argv: [\"go\", \"test\", \"./...\"]\n")
	writeRepoFile(t, root, ".awm/awm-workflows.yaml", "version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: verify:tests\n")
	withWorkingDir(t, root)

	repo := &fakeRepository{}
	svc, err := NewWithRuntimeStatus(repo, root, RuntimeStatusSnapshot{
		Backend:                "sqlite",
		SQLitePath:             filepath.Join(root, ".awm", "context.db"),
		UsesImplicitSQLitePath: true,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Export(context.Background(), v1.ExportPayload{
		ProjectID: "project.alpha",
		Format:    v1.ExportFormatMarkdown,
		Status: &v1.ExportStatusSelector{
			TaskText: "inspect export readiness",
			Phase:    v1.PhaseExecute,
		},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Document == nil || result.Document.Kind != v1.ExportDocumentKindStatus {
		t.Fatalf("expected status export document, got %+v", result.Document)
	}
	if result.Document.Status == nil || result.Document.Status.Project.ProjectID != "project.alpha" {
		t.Fatalf("unexpected status payload: %+v", result.Document.Status)
	}
	if result.Document.Status.Context == nil || result.Document.Status.Context.Status != "ok" {
		t.Fatalf("expected context preview in status payload, got %+v", result.Document.Status.Context)
	}
	for _, needle := range []string{"# Status", "## Project", "## Sources", "## Context Preview"} {
		if !strings.Contains(result.Content, needle) {
			t.Fatalf("expected markdown to contain %q, got:\n%s", needle, result.Content)
		}
	}
}

func TestRenderExportMarkdown_Golden(t *testing.T) {
	tests := []struct {
		name   string
		golden string
		doc    *v1.ExportDocument
	}{
		{
			name:   "plan",
			golden: "plan.md",
			doc: &v1.ExportDocument{
				Kind:    v1.ExportDocumentKindPlan,
				Title:   "Export Plan",
				Summary: "Implement deterministic markdown",
				Plan: &v1.ExportPlanDocument{
					PlanKey:       "plan:receipt-339e5cfde29fa58b2c5f1c16",
					ReceiptID:     "receipt-339e5cfde29fa58b2c5f1c16",
					Title:         "Exportable AWM artifacts with JSON and Markdown renderers",
					Objective:     "Ship export output across read surfaces.",
					Kind:          "feature",
					ParentPlanKey: "plan:receipt-parent",
					Status:        v1.WorkItemStatusInProgress,
					Stages: &v1.ExportPlanStages{
						SpecOutline:        v1.WorkItemStatusComplete,
						ImplementationPlan: v1.WorkItemStatusInProgress,
					},
					InScope:      []string{"internal/service/backend/export.go", "spec/v1/cli.result.schema.json"},
					Constraints:  []string{"Keep markdown deterministic", "Keep contracts in lockstep"},
					References:   []string{"AGENTS.md", "README.md"},
					ExternalRefs: []string{"plan:receipt-parent"},
					Tasks: []v1.ExportTaskDocument{{
						PlanKey:            "plan:receipt-339e5cfde29fa58b2c5f1c16",
						Key:                "impl:tests-markdown-golden",
						Summary:            "Add golden tests",
						Status:             v1.WorkItemStatusPending,
						ParentTaskKey:      "group:parity-tests-docs",
						DependsOn:          []string{"impl:renderer-markdown-plan"},
						AcceptanceCriteria: []string{"Golden fixtures stay stable across repeated runs"},
						References:         []string{"internal/service/backend/export_test.go"},
						ExternalRefs:       []string{"verifyrun:verify-1"},
						Evidence:           []string{"go test ./internal/service/backend"},
					}},
				},
			},
		},
		{
			name:   "receipt",
			golden: "receipt.md",
			doc: &v1.ExportDocument{
				Kind:    v1.ExportDocumentKindReceipt,
				Title:   "Receipt receipt-339e5cfde29fa58b2c5f1c16",
				Summary: "Continue implementing export",
				Receipt: &v1.ExportReceiptDocument{
					ReceiptID:         "receipt-339e5cfde29fa58b2c5f1c16",
					TaskText:          "Continue implementing export",
					Phase:             v1.PhaseExecute,
					ResolvedTags:      []string{"backend", "export"},
					PointerKeys:       []string{"project.alpha:internal/service/backend/export.go#renderer"},
					InitialScopePaths: []string{"internal/service/backend/export.go"},
					BaselineCaptured:  true,
					BaselinePaths: []v1.ExportBaselinePath{{
						Path:        "internal/service/backend/export.go",
						ContentHash: "sha256:abc123",
					}},
					LatestRun: &v1.ExportReceiptRunDocument{
						RunID:      44,
						Status:     "completed",
						PlanStatus: v1.WorkItemStatusInProgress,
						Tasks: []v1.ExportTaskDocument{{
							Key:     "impl:tests-markdown-golden",
							Summary: "Add golden tests",
							Status:  v1.WorkItemStatusComplete,
							Outcome: "Added deterministic fixtures.",
						}},
					},
				},
			},
		},
		{
			name:   "bundle",
			golden: "bundle.md",
			doc: &v1.ExportDocument{
				Kind:    v1.ExportDocumentKindFetchBundle,
				Title:   "Fetch bundle",
				Summary: "1 item(s), 0 missing",
				Bundle: &v1.ExportBundleDocument{
					RequestedKeys: []string{"project.alpha:docs/export.md#renderer"},
					Items: []v1.ExportBundleItem{
						{
							Kind:    v1.ExportBundleItemKindPointer,
							Key:     "project.alpha:docs/export.md#renderer",
							Type:    "pointer",
							Summary: "Renderer notes",
							Status:  "indexed",
							Version: "sha256:def456",
							Content: "raw pointer content",
						},
					},
					NotFound: []string{},
				},
			},
		},
		{
			name:   "history",
			golden: "history.md",
			doc: &v1.ExportDocument{
				Kind:    v1.ExportDocumentKindHistory,
				Title:   "History work",
				Summary: "1 item(s) matching \"export\"",
				History: &v1.HistorySearchResult{
					Entity: v1.HistoryEntityWork,
					Scope:  v1.HistoryScopeCurrent,
					Query:  "export",
					Limit:  10,
					Count:  1,
					Items: []v1.HistoryItem{{
						Key:           "plan:receipt-339e5cfde29fa58b2c5f1c16",
						Entity:        v1.HistoryEntityWork,
						Summary:       "Exportable AWM artifacts",
						Status:        "in_progress",
						Scope:         v1.HistoryScopeCurrent,
						PlanKey:       "plan:receipt-339e5cfde29fa58b2c5f1c16",
						ReceiptID:     "receipt-339e5cfde29fa58b2c5f1c16",
						RequestID:     "req-work-123",
						Phase:         v1.PhaseExecute,
						Kind:          "feature",
						ParentPlanKey: "plan:receipt-parent",
						TaskCounts: &v1.ContextPlanTaskCounts{
							Total:      4,
							Pending:    1,
							InProgress: 2,
							Complete:   1,
						},
						FetchKeys: []string{"plan:receipt-339e5cfde29fa58b2c5f1c16", "run:44"},
						UpdatedAt: "2026-03-11T13:30:00Z",
					}},
				},
			},
		},
		{
			name:   "status",
			golden: "status.md",
			doc: &v1.ExportDocument{
				Kind:    v1.ExportDocumentKindStatus,
				Title:   "Status project.alpha",
				Summary: "ready=false missing=1 warnings=1",
				Status: &v1.StatusResult{
					Summary: v1.StatusSummary{
						Ready:        false,
						MissingCount: 1,
						WarningCount: 1,
					},
					Project: v1.StatusProject{
						ProjectID:              "project.alpha",
						ProjectRoot:            "/repo",
						DetectedRepoRoot:       "/repo",
						Backend:                "sqlite",
						SQLitePath:             "/repo/.awm/context.db",
						UsesImplicitSQLitePath: true,
					},
					Sources: []v1.StatusSource{{
						Kind:         "rules",
						SourcePath:   ".awm/awm-rules.yaml",
						AbsolutePath: "/repo/.awm/awm-rules.yaml",
						Exists:       true,
						Loaded:       true,
						ItemCount:    3,
						Notes:        []string{"loaded from project root"},
					}},
					Integrations: []v1.StatusIntegration{{
						ID:              "awm-broker",
						Summary:         "Skill pack installed",
						Installed:       true,
						PresentTargets:  2,
						ExpectedTargets: 2,
					}},
					Context: &v1.StatusContextPreview{
						TaskText:              "inspect export readiness",
						Phase:                 v1.PhaseExecute,
						Status:                "ok",
						ResolvedTags:          []string{"backend", "export"},
						RuleCount:             3,
						PlanCount:             1,
						InitialScopePathCount: 1,
					},
					Missing: []v1.StatusMissingItem{{
						Code:    "tests_file_missing",
						Message: "tests file not configured",
					}},
					Warnings: []v1.StatusMissingItem{{
						Code:    "stale_plan",
						Message: "plan needs status refresh",
					}},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertMarkdownGolden(t, tc.golden, renderExportMarkdown(tc.doc))
		})
	}
}

func TestExportHistoryMarkdownFixtureHasNoLegacyMemoryFetchKeys(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "export_markdown", "history.md"))
	if err != nil {
		t.Fatalf("read history markdown fixture: %v", err)
	}
	if strings.Contains(string(raw), "mem:") {
		t.Fatalf("history markdown fixture still contains legacy memory fetch keys:\n%s", string(raw))
	}
}

func assertMarkdownGolden(t *testing.T, golden, got string) {
	t.Helper()

	path := filepath.Join("testdata", "export_markdown", golden)
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	normalizedGot := normalizeGoldenMarkdown(got)
	normalizedWant := normalizeGoldenMarkdown(string(want))
	if normalizedGot != normalizedWant {
		t.Fatalf("markdown golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", golden, normalizedGot, normalizedWant)
	}
}

func normalizeGoldenMarkdown(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.TrimSuffix(value, "\n")
}
