package backend

import (
	"context"
	"errors"
	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestContext_NormalPathReturnsOKAndReceipt(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, ".awm/awm-rules.yaml", strings.Join([]string{
		"version: awm.rules.v1",
		"rules:",
		"  - id: rule_startup",
		"    summary: Startup rule",
		"    content: Keep the context receipt deterministic.",
		"    enforcement: hard",
		"    tags: [governance]",
		"",
	}, "\n"))
	withWorkingDir(t, root)

	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	payload := v1.ContextPayload{
		ProjectID:         "project.alpha",
		TaskText:          "implement deterministic get context flow",
		Phase:             v1.PhaseExecute,
		InitialScopePaths: []string{"internal/service/backend/context.go", "spec/v1/README.md"},
	}

	result, apiErr := svc.Context(context.Background(), payload)
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Status != "ok" {
		t.Fatalf("unexpected status: %q", result.Status)
	}
	if result.Receipt == nil {
		t.Fatal("expected receipt")
	}
	if result.Receipt.Meta.ReceiptID == "" {
		t.Fatal("expected non-empty receipt_id")
	}
	if got := result.Receipt.InitialScopePaths; !reflect.DeepEqual(got, payload.InitialScopePaths) {
		t.Fatalf("unexpected initial scope paths: got %v want %v", got, payload.InitialScopePaths)
	}

	rules := receiptIndexEntries(result.Receipt, "rules")
	if got, want := receiptIndexKeys(rules), []string{"project.alpha:.awm/awm-rules.yaml#rule_startup"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected rule index keys: got %v want %v", got, want)
	}
	if len(rules) != 1 {
		t.Fatalf("expected one rule entry, got %d", len(rules))
	}
	if got, want := entryString(rules[0], "rule_id"), "rule_startup"; got != want {
		t.Fatalf("unexpected stable rule_id: got %q want %q", got, want)
	}
	plans := receiptIndexEntries(result.Receipt, "plans")
	if len(plans) != 1 {
		t.Fatalf("unexpected plan index count: got %d want 1", len(plans))
	}
	if got := entryString(plans[0], "key"); got != "plan:"+result.Receipt.Meta.ReceiptID {
		t.Fatalf("unexpected plan key: got %q want %q", got, "plan:"+result.Receipt.Meta.ReceiptID)
	}
	if got := entryString(plans[0], "status"); got != core.PlanStatusPending {
		t.Fatalf("unexpected plan status: got %q want %q", got, core.PlanStatusPending)
	}
	if got := strings.TrimSpace(entryString(plans[0], "summary")); got == "" {
		t.Fatalf("expected non-empty plan summary entry: %+v", plans[0])
	}

	meta := receiptMeta(result.Receipt)
	if got := strings.TrimSpace(anyToString(meta["receipt_id"])); got != result.Receipt.Meta.ReceiptID {
		t.Fatalf("unexpected receipt meta receipt_id: got %q want %q", got, result.Receipt.Meta.ReceiptID)
	}
	if !result.Receipt.Meta.BaselineCaptured {
		t.Fatal("expected baseline_captured to be true")
	}

	if len(repo.candidateCalls) != 0 {
		t.Fatalf("did not expect candidate queries, got %d", len(repo.candidateCalls))
	}
	if len(repo.receiptUpsertCalls) != 1 {
		t.Fatalf("expected 1 receipt scope upsert, got %d", len(repo.receiptUpsertCalls))
	}
	if got := repo.receiptUpsertCalls[0].ReceiptID; got != result.Receipt.Meta.ReceiptID {
		t.Fatalf("unexpected persisted receipt_id: got %q want %q", got, result.Receipt.Meta.ReceiptID)
	}
	if got := repo.receiptUpsertCalls[0].ProjectID; got != payload.ProjectID {
		t.Fatalf("unexpected persisted project_id: got %q want %q", got, payload.ProjectID)
	}
	if got := repo.receiptUpsertCalls[0].Phase; got != string(payload.Phase) {
		t.Fatalf("unexpected persisted phase: got %q want %q", got, payload.Phase)
	}
	if got := repo.receiptUpsertCalls[0].TaskText; got != payload.TaskText {
		t.Fatalf("unexpected persisted task_text: got %q want %q", got, payload.TaskText)
	}
	if got := repo.receiptUpsertCalls[0].PointerKeys; !reflect.DeepEqual(got, []string{"project.alpha:.awm/awm-rules.yaml#rule_startup"}) {
		t.Fatalf("unexpected persisted pointer keys: got %v want %v", got, []string{"project.alpha:.awm/awm-rules.yaml#rule_startup"})
	}
	wantPersistedPaths := []string{"internal/service/backend/context.go", "spec/v1/README.md"}
	if got := repo.receiptUpsertCalls[0].InitialScopePaths; !reflect.DeepEqual(got, wantPersistedPaths) {
		t.Fatalf("unexpected persisted initial scope paths: got %v want %v", got, wantPersistedPaths)
	}

	repo2 := &fakeRepository{}
	svc2, err := New(repo2)
	if err != nil {
		t.Fatalf("new service 2: %v", err)
	}
	result2, apiErr2 := svc2.Context(context.Background(), payload)
	if apiErr2 != nil {
		t.Fatalf("unexpected API error on second run: %+v", apiErr2)
	}
	if result2.Receipt == nil {
		t.Fatal("expected second receipt")
	}
	if result2.Receipt.Meta.ReceiptID != result.Receipt.Meta.ReceiptID {
		t.Fatalf("expected deterministic receipt_id, got %q and %q", result.Receipt.Meta.ReceiptID, result2.Receipt.Meta.ReceiptID)
	}
}

func TestContext_FallsBackToFilesystemBaselineWhenGitUnavailable(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, ".awm/awm-rules.yaml", "version: awm.rules.v1\nrules:\n  - id: rule_startup\n    summary: Startup rule\n    content: Keep the context receipt deterministic.\n    enforcement: hard\n")
	writeRepoFile(t, root, "src/main.go", "package src\n\nfunc main() {}\n")
	withWorkingDir(t, root)

	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.runGitCommand = func(_ context.Context, _ string, _ ...string) (string, error) {
		return "", errors.New("git unavailable")
	}

	result, apiErr := svc.Context(context.Background(), v1.ContextPayload{
		ProjectID: "project.alpha",
		TaskText:  "fallback baseline",
		Phase:     v1.PhaseExecute,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Receipt == nil {
		t.Fatal("expected receipt")
	}
	if !result.Receipt.Meta.BaselineCaptured {
		t.Fatal("expected filesystem fallback baseline to be captured")
	}
	if len(repo.receiptUpsertCalls) != 1 {
		t.Fatalf("expected one receipt scope upsert, got %d", len(repo.receiptUpsertCalls))
	}
	if !repo.receiptUpsertCalls[0].BaselineCaptured {
		t.Fatal("expected persisted receipt scope baseline to be captured")
	}
	gotPaths := syncPathPaths(repo.receiptUpsertCalls[0].BaselinePaths)
	wantPaths := []string{".awm/awm-rules.yaml", "src/main.go"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("unexpected fallback baseline paths: got %v want %v", gotPaths, wantPaths)
	}
}

func TestContext_AllowsCanonicalRulesFromManagedPaths(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, ".awm/awm-rules.yaml", strings.Join([]string{
		"version: awm.rules.v1",
		"rules:",
		"  - id: rule_hard",
		"    summary: Managed hard rule",
		"    content: Keep managed rules visible.",
		"    enforcement: hard",
		"    tags: [governance, enforcement-hard]",
		"",
	}, "\n"))
	writeRepoFile(t, root, "awm-rules.yaml", strings.Join([]string{
		"version: awm.rules.v1",
		"rules:",
		"  - id: rule_root",
		"    summary: Root soft rule",
		"    content: Keep fallback rules visible.",
		"    enforcement: soft",
		"    tags: [governance, enforcement-soft]",
		"",
	}, "\n"))
	withWorkingDir(t, root)

	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Context(context.Background(), v1.ContextPayload{
		ProjectID: "project.alpha",
		TaskText:  "enforce repo hard rules",
		Phase:     v1.PhaseExecute,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Status != "ok" || result.Receipt == nil {
		t.Fatalf("unexpected result: %+v", result)
	}

	if got, want := receiptIndexKeys(receiptIndexEntries(result.Receipt, "rules")), []string{"project.alpha:.awm/awm-rules.yaml#rule_hard", "project.alpha:awm-rules.yaml#rule_root"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected rule keys: got %v want %v", got, want)
	}
}

func TestContext_WithoutRulesStillReturnsReceipt(t *testing.T) {
	repo := &fakeRepository{
		candidateResults: [][]core.CandidatePointer{{}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Context(context.Background(), v1.ContextPayload{
		ProjectID: "project.alpha",
		TaskText:  "minimal context should still succeed",
		Phase:     v1.PhaseExecute,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Status != "ok" || result.Receipt == nil {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.Receipt.Rules) != 0 {
		t.Fatalf("expected no rules, got %+v", result.Receipt.Rules)
	}
}

func TestContext_LoadsCanonicalRulesWithoutIndexedPointers(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	rules := strings.Join([]string{
		"version: awm.rules.v1",
		"rules:",
		"  - id: rule_simple_context",
		"    summary: Always verify before done",
		"    content: Verify executable changes before closing the receipt.",
		"    enforcement: hard",
		"    tags: [verify, workflow]",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-rules.yaml"), []byte(rules), 0o644); err != nil {
		t.Fatalf("write rules file: %v", err)
	}
	withWorkingDir(t, root)

	repo := &fakeRepository{
		candidateResults: [][]core.CandidatePointer{{}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Context(context.Background(), v1.ContextPayload{
		ProjectID: "project.alpha",
		TaskText:  "verify direct ruleset loading",
		Phase:     v1.PhaseExecute,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Status != "ok" || result.Receipt == nil {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got, want := receiptIndexKeys(receiptIndexEntries(result.Receipt, "rules")), []string{"project.alpha:.awm/awm-rules.yaml#rule_simple_context"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected rule keys: got %v want %v", got, want)
	}
	if len(repo.candidateCalls) != 0 {
		t.Fatalf("did not expect indexed candidate queries when canonical rules exist, got %d", len(repo.candidateCalls))
	}
	if len(repo.receiptUpsertCalls) != 1 {
		t.Fatalf("expected one receipt scope upsert, got %d", len(repo.receiptUpsertCalls))
	}
	if got, want := repo.receiptUpsertCalls[0].PointerKeys, []string{"project.alpha:.awm/awm-rules.yaml#rule_simple_context"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected persisted pointer keys: got %v want %v", got, want)
	}
}

func TestContext_PersistsEmptyReceiptScope(t *testing.T) {
	root := t.TempDir()
	withWorkingDir(t, root)

	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Context(context.Background(), v1.ContextPayload{
		ProjectID: "project.alpha",
		TaskText:  "persist empty receipt scope",
		Phase:     v1.PhaseExecute,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Status != "ok" || result.Receipt == nil {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(repo.receiptUpsertCalls) != 1 {
		t.Fatalf("expected one receipt scope upsert, got %d", len(repo.receiptUpsertCalls))
	}
}

func TestContext_PhaseAndCanonicalTagsThreadedToRuleQueries(t *testing.T) {
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Context(context.Background(), v1.ContextPayload{
		ProjectID: "project.alpha",
		TaskText:  "policy API tests for review",
		Phase:     v1.PhaseReview,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Status != "ok" || result.Receipt == nil {
		t.Fatalf("unexpected result: %+v", result)
	}
	wantQueryTags := []string{"backend", "governance", "review", "test"}
	if len(repo.candidateCalls) != 0 {
		t.Fatalf("did not expect candidate calls, got %d", len(repo.candidateCalls))
	}
	if !containsAllStrings(result.Receipt.Meta.ResolvedTags, wantQueryTags) {
		t.Fatalf("expected resolved tags to contain %v, got %v", wantQueryTags, result.Receipt.Meta.ResolvedTags)
	}
}

func TestContext_DefaultRepoTagsFileDiscoveryMergesCanonicalAliases(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-tags.yaml"), []byte("version: awm.tags.v1\ncanonical_tags:\n  backend:\n    - svc\n"), 0o644); err != nil {
		t.Fatalf("write tags file: %v", err)
	}
	withWorkingDir(t, root)

	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID:         "project.alpha",
			ReceiptID:         "receipt.abc123",
			InitialScopePaths: []string{},
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Context(context.Background(), v1.ContextPayload{
		ProjectID: "project.alpha",
		TaskText:  "fix svc bootstrap gap",
		Phase:     v1.PhaseExecute,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Status != "ok" {
		t.Fatalf("unexpected status: %+v", result)
	}
	if result.Receipt == nil {
		t.Fatal("expected receipt")
	}
	if len(repo.candidateCalls) != 0 {
		t.Fatalf("did not expect candidate queries, got %d", len(repo.candidateCalls))
	}
}

func TestContext_DoesNotFallbackToFilesystemBaselineWhenGitUnavailableInsideGitRepo(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, ".awm/awm-rules.yaml", "version: awm.rules.v1\nrules:\n  - id: rule_startup\n    summary: Startup rule\n    content: Keep the context receipt deterministic.\n    enforcement: hard\n")
	writeRepoFile(t, root, "src/main.go", "package src\n\nfunc main() {}\n")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	withWorkingDir(t, root)

	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.runGitCommand = func(_ context.Context, _ string, _ ...string) (string, error) {
		return "", errors.New("git unavailable")
	}

	result, apiErr := svc.Context(context.Background(), v1.ContextPayload{
		ProjectID: "project.alpha",
		TaskText:  "fallback baseline blocked in git repo",
		Phase:     v1.PhaseExecute,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Receipt == nil {
		t.Fatal("expected receipt")
	}
	if result.Receipt.Meta.BaselineCaptured {
		t.Fatal("expected baseline capture to stay unavailable when git metadata exists but git calls fail")
	}
	if len(repo.receiptUpsertCalls) != 1 {
		t.Fatalf("expected one receipt scope upsert, got %d", len(repo.receiptUpsertCalls))
	}
	if repo.receiptUpsertCalls[0].BaselineCaptured {
		t.Fatal("expected persisted baseline capture to remain false")
	}
	if len(repo.receiptUpsertCalls[0].BaselinePaths) != 0 {
		t.Fatalf("expected no persisted baseline paths, got %+v", repo.receiptUpsertCalls[0].BaselinePaths)
	}
}
