package repositorycontract

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	backendsvc "github.com/bonztm/agent-workflow-manager/internal/service/backend"
)

// ServiceFlowConfig configures RunServiceFlows: a label identifying the
// backend under test, an optional fixed project ID (a unique one is generated
// when blank), and the repository implementation to exercise.
type ServiceFlowConfig struct {
	BackendLabel string
	ProjectID    string
	Repo         core.Repository
}

// RunServiceFlows exercises end-to-end backend service flows (context, work,
// verify, done, pointer renames) against the configured repository inside a
// scratch git repo, failing t on any contract violation. All repository
// implementations are expected to pass identically.
func RunServiceFlows(t *testing.T, cfg ServiceFlowConfig) {
	t.Helper()

	ctx := context.Background()
	projectID := cfg.ProjectID
	if projectID == "" {
		projectID = "project." + cfg.BackendLabel + "." + time.Now().UTC().Format("20060102150405.000000000")
	}

	projectRoot := setupGitRepo(t, map[string]string{
		".awm/awm-rules.yaml": "version: awm.rules.v1\nrules:\n  - id: rule.runtime.default\n    summary: Runtime defaults remain receipt-scoped\n    content: Runtime default backend evidence must stay tied to the originating receipt.\n    enforcement: hard\n    tags: [runtime]\n",
		"docs/runtime.md":     "runtime pointer content",
	})
	t.Chdir(projectRoot)

	if _, err := cfg.Repo.UpsertPointerStubs(ctx, projectID, []core.PointerStub{{
		PointerKey:  "pointer.runtime.default",
		Path:        "docs/runtime.md",
		Kind:        "doc",
		Label:       "Runtime default backend",
		Description: "Runtime default backend path",
		Tags:        []string{"runtime", cfg.BackendLabel},
	}}); err != nil {
		t.Fatalf("seed runtime pointer: %v", err)
	}
	if _, err := cfg.Repo.SyncRulePointers(ctx, core.RulePointerSyncInput{
		ProjectID:  projectID,
		SourcePath: ".awm/awm-rules.yaml",
		Pointers: []core.RulePointer{{
			RuleID:      "rule.runtime.default",
			Summary:     "Runtime defaults remain receipt-scoped",
			Content:     "Runtime default backend evidence must stay tied to the originating receipt.",
			Enforcement: "hard",
			Tags:        []string{"runtime", cfg.BackendLabel},
		}},
	}); err != nil {
		t.Fatalf("seed runtime rule pointer: %v", err)
	}

	svc, err := backendsvc.New(cfg.Repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	getContextResult, apiErr := svc.Context(ctx, v1.ContextPayload{
		ProjectID:         projectID,
		TaskText:          "verify " + cfg.BackendLabel + " runtime backend path",
		Phase:             v1.PhaseExecute,
		InitialScopePaths: []string{"docs/runtime.md"},
	})
	if apiErr != nil {
		t.Fatalf("context API error: %+v", apiErr)
	}
	if getContextResult.Status != "ok" || getContextResult.Receipt == nil {
		t.Fatalf("unexpected context result: %+v", getContextResult)
	}
	receipt := getContextResult.Receipt
	pointerKeys := receiptPointerKeys(receipt)
	if len(pointerKeys) == 0 {
		t.Fatal("expected receipt to contain at least one rule pointer key")
	}
	if got := receipt.InitialScopePaths; !reflect.DeepEqual(got, []string{"docs/runtime.md"}) {
		t.Fatalf("unexpected receipt initial scope paths: got %v want %v", got, []string{"docs/runtime.md"})
	}
	scopeBeforeRename, err := cfg.Repo.FetchReceiptScope(ctx, core.ReceiptScopeQuery{
		ProjectID: projectID,
		ReceiptID: receipt.Meta.ReceiptID,
	})
	if err != nil {
		t.Fatalf("fetch receipt scope before pointer rename: %v", err)
	}
	if !reflect.DeepEqual(scopeBeforeRename.InitialScopePaths, []string{"docs/runtime.md"}) {
		t.Fatalf("unexpected stored initial scope paths before rename: got %v want %v", scopeBeforeRename.InitialScopePaths, []string{"docs/runtime.md"})
	}

	if _, renameErr := cfg.Repo.UpsertPointerStubs(ctx, projectID, []core.PointerStub{{
		PointerKey:  "pointer.runtime.default",
		Path:        "docs/runtime-renamed.md",
		Kind:        "doc",
		Label:       "Runtime default backend",
		Description: "Runtime default backend path renamed after receipt creation",
		Tags:        []string{"runtime", cfg.BackendLabel},
	}}); renameErr != nil {
		t.Fatalf("rename runtime pointer: %v", renameErr)
	}

	scopeAfterRename, err := cfg.Repo.FetchReceiptScope(ctx, core.ReceiptScopeQuery{
		ProjectID: projectID,
		ReceiptID: receipt.Meta.ReceiptID,
	})
	if err != nil {
		t.Fatalf("fetch receipt scope after pointer rename: %v", err)
	}
	if !reflect.DeepEqual(scopeAfterRename.InitialScopePaths, []string{"docs/runtime.md"}) {
		t.Fatalf("expected receipt scope snapshot to survive pointer rename, got %v", scopeAfterRename.InitialScopePaths)
	}

	_, err = cfg.Repo.SaveRunReceiptSummary(ctx, core.RunReceiptSummary{
		ProjectID:    projectID,
		ReceiptID:    receipt.Meta.ReceiptID,
		TaskText:     receipt.Meta.TaskText,
		Phase:        string(receipt.Meta.Phase),
		Status:       "accepted",
		ResolvedTags: append([]string(nil), receipt.Meta.ResolvedTags...),
		PointerKeys:  pointerKeys,
	})
	if err != nil {
		t.Fatalf("save receipt scope summary: %v", err)
	}

	reportResult, apiErr := svc.Done(ctx, v1.DonePayload{
		ProjectID:    projectID,
		ReceiptID:    receipt.Meta.ReceiptID,
		FilesChanged: []string{"docs/runtime.md"},
		Outcome:      cfg.BackendLabel + " flow accepted",
	})
	if apiErr != nil {
		t.Fatalf("done API error: %+v", apiErr)
	}
	if !reportResult.Accepted || reportResult.RunID <= 0 {
		t.Fatalf("unexpected done result: %+v", reportResult)
	}

	workResult, apiErr := svc.Work(ctx, v1.WorkPayload{
		ProjectID: projectID,
		PlanKey:   "plan:" + receipt.Meta.ReceiptID,
		ReceiptID: receipt.Meta.ReceiptID,
		Tasks: []v1.WorkTaskPayload{
			{Key: "docs/runtime.md", Summary: "Confirm runtime pointer flow", Status: v1.WorkItemStatusComplete},
		},
	})
	if apiErr != nil {
		t.Fatalf("work API error: %+v", apiErr)
	}
	if workResult.PlanKey != "plan:"+receipt.Meta.ReceiptID || workResult.PlanStatus != string(core.PlanStatusComplete) || workResult.Updated != 1 {
		t.Fatalf("unexpected work result: %+v", workResult)
	}

	workItems, err := cfg.Repo.ListWorkItems(ctx, core.FetchLookupQuery{
		ProjectID: projectID,
		ReceiptID: receipt.Meta.ReceiptID,
	})
	if err != nil {
		t.Fatalf("list work items: %v", err)
	}
	if len(workItems) != 1 {
		t.Fatalf("expected one work item, got %+v", workItems)
	}
	if workItems[0].ItemKey != "docs/runtime.md" || workItems[0].Status != core.WorkItemStatusComplete {
		t.Fatalf("unexpected persisted work item: %+v", workItems[0])
	}

	fetchLookup, err := cfg.Repo.LookupFetchState(ctx, core.FetchLookupQuery{
		ProjectID: projectID,
		ReceiptID: receipt.Meta.ReceiptID,
	})
	if err != nil {
		t.Fatalf("lookup fetch state: %v", err)
	}
	if fetchLookup.PlanStatus != core.PlanStatusComplete {
		t.Fatalf("unexpected plan status: %q", fetchLookup.PlanStatus)
	}
	if fetchLookup.RunID != int64(reportResult.RunID) {
		t.Fatalf("expected fetch lookup to return latest done run_id %d, got %d", reportResult.RunID, fetchLookup.RunID)
	}

	syncProjectRoot := setupGitRepo(t, map[string]string{
		".awm/awm-rules.yaml":     "version: awm.rules.v1\nrules:\n  - id: rule.runtime.default\n    summary: Runtime defaults remain receipt-scoped\n    content: Runtime default backend evidence must stay tied to the originating receipt.\n    enforcement: hard\n    tags: [runtime]\n",
		"docs/runtime-renamed.md": "runtime pointer content",
		"docs/new.md":             "new pointer candidate",
	})
	syncResult, apiErr := svc.Sync(ctx, v1.SyncPayload{
		ProjectID:   projectID,
		Mode:        "full",
		ProjectRoot: syncProjectRoot,
	})
	if apiErr != nil {
		t.Fatalf("sync API error: %+v", apiErr)
	}

	wantProcessed := []string{"docs/new.md", "docs/runtime-renamed.md"}
	if !reflect.DeepEqual(syncResult.ProcessedPaths, wantProcessed) {
		t.Fatalf("unexpected processed paths: got %v want %v", syncResult.ProcessedPaths, wantProcessed)
	}
	if syncResult.Updated != 1 {
		t.Fatalf("unexpected sync updated count: got %d want 1", syncResult.Updated)
	}
	if syncResult.NewCandidates != 1 {
		t.Fatalf("unexpected sync new_candidates count: got %d want 1", syncResult.NewCandidates)
	}
	if syncResult.MarkedStale != 1 || syncResult.DeletedMarkedStale != 0 {
		t.Fatalf("unexpected stale counters: %+v", syncResult)
	}
}

func receiptPointerKeys(receipt *v1.ContextReceipt) []string {
	pointerKeySet := make(map[string]struct{}, len(receipt.Rules))
	for _, rule := range receipt.Rules {
		if rule.Key != "" {
			pointerKeySet[rule.Key] = struct{}{}
		}
	}
	pointerKeys := make([]string, 0, len(pointerKeySet))
	for key := range pointerKeySet {
		pointerKeys = append(pointerKeys, key)
	}
	sort.Strings(pointerKeys)
	return pointerKeys
}

func setupGitRepo(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "repository-contract@example.com")
	runGit(t, root, "config", "user.name", "Repository Contract")

	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		abs := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
			t.Fatalf("mkdir %q: %v", abs, err)
		}
		if err := os.WriteFile(abs, []byte(files[p]), 0o600); err != nil {
			t.Fatalf("write file %q: %v", abs, err)
		}
	}

	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "seed")
	return root
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run git %v: %v\n%s", args, err, string(out))
	}
}
