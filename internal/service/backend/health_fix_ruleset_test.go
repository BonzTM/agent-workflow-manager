package backend

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

type trackingRuleSyncRepository struct {
	*fakeRepository

	ruleSyncCalls   []core.RulePointerSyncInput
	ruleSyncResults []core.RulePointerSyncResult
	ruleSyncErrors  []error
}

func (r *trackingRuleSyncRepository) SyncRulePointers(_ context.Context, input core.RulePointerSyncInput) (core.RulePointerSyncResult, error) {
	copied := core.RulePointerSyncInput{
		ProjectID:  strings.TrimSpace(input.ProjectID),
		SourcePath: strings.TrimSpace(input.SourcePath),
		Pointers:   make([]core.RulePointer, 0, len(input.Pointers)),
	}
	for _, pointer := range input.Pointers {
		copied.Pointers = append(copied.Pointers, core.RulePointer{
			PointerKey:  pointer.PointerKey,
			SourcePath:  pointer.SourcePath,
			RuleID:      pointer.RuleID,
			Summary:     pointer.Summary,
			Content:     pointer.Content,
			Enforcement: pointer.Enforcement,
			Tags:        append([]string(nil), pointer.Tags...),
		})
	}
	r.ruleSyncCalls = append(r.ruleSyncCalls, copied)
	idx := len(r.ruleSyncCalls) - 1
	if idx < len(r.ruleSyncErrors) && r.ruleSyncErrors[idx] != nil {
		return core.RulePointerSyncResult{}, r.ruleSyncErrors[idx]
	}
	if idx < len(r.ruleSyncResults) {
		return r.ruleSyncResults[idx], nil
	}
	return core.RulePointerSyncResult{Upserted: len(copied.Pointers)}, nil
}

func TestHealthFix_DryRunPlansSafeFixers(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("tracked"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}

	repo := &trackingRuleSyncRepository{fakeRepository: &fakeRepository{
		inventoryResults: []core.PointerInventory{{Path: "tracked.txt", IsStale: false}},
	}}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.runGitCommand = func(_ context.Context, projectRoot string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "diff --name-status --find-renames HEAD":
			return "M\ttracked.txt\n", nil
		case "ls-files --others --exclude-standard":
			return "new.txt\n", nil
		case "ls-files --cached --others --exclude-standard":
			return "tracked.txt\nnew.txt\n", nil
		case "ls-files --deleted":
			return "", nil
		default:
			return "", nil
		}
	}

	apply := false
	health, apiErr := svc.Health(context.Background(), v1.HealthPayload{
		ProjectID:   "project.alpha",
		Apply:       &apply,
		ProjectRoot: root,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	result := health.Fix
	if result == nil {
		t.Fatalf("expected fix result, got %+v", health)
	}
	if !result.DryRun {
		t.Fatalf("expected dry_run=true, got false")
	}
	if len(result.PlannedActions) != 3 {
		t.Fatalf("expected 3 planned actions, got %d", len(result.PlannedActions))
	}
	if len(result.AppliedActions) != 0 {
		t.Fatalf("expected no applied actions on dry-run, got %d", len(result.AppliedActions))
	}
	if !strings.Contains(result.Summary, "dry-run") {
		t.Fatalf("unexpected summary: %q", result.Summary)
	}
	if len(repo.syncCalls) != 0 {
		t.Fatalf("did not expect sync apply calls on dry-run, got %d", len(repo.syncCalls))
	}
	if len(repo.upsertStubCalls) != 0 {
		t.Fatalf("did not expect upsert stubs on dry-run, got %d", len(repo.upsertStubCalls))
	}
	if len(repo.ruleSyncCalls) != 0 {
		t.Fatalf("did not expect rule sync calls on dry-run, got %d", len(repo.ruleSyncCalls))
	}
}

func TestHealthFix_ApplySyncRulesetUsesCanonicalParser(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	rulesetPath := filepath.Join(root, ".awm", "awm-rules.yaml")
	ruleset := strings.Join([]string{
		"version: awm.rules.v1",
		"rules:",
		"  - id: rule.explicit",
		"    summary: Explicit summary",
		"    content: Explicit content",
		"    enforcement: soft",
		"    tags:",
		"      - policy",
		"  - summary: Generated summary",
		"    content: Generated content",
	}, "\n") + "\n"
	if err := os.WriteFile(rulesetPath, []byte(ruleset), 0o644); err != nil {
		t.Fatalf("write ruleset: %v", err)
	}

	repo := &trackingRuleSyncRepository{
		fakeRepository:  &fakeRepository{},
		ruleSyncResults: []core.RulePointerSyncResult{{Upserted: 2, MarkedStale: 1}, {Upserted: 0, MarkedStale: 0}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	apply := true
	health, apiErr := svc.Health(context.Background(), v1.HealthPayload{
		ProjectID:   "project.alpha",
		Apply:       &apply,
		ProjectRoot: root,
		Fixers:      []v1.HealthFixer{v1.HealthFixerSyncRuleset},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	result := health.Fix
	if result == nil {
		t.Fatalf("expected fix result, got %+v", health)
	}
	if result.DryRun {
		t.Fatalf("expected dry_run=false, got true")
	}
	if len(result.PlannedActions) != 1 || len(result.AppliedActions) != 1 {
		t.Fatalf("unexpected actions: planned=%d applied=%d", len(result.PlannedActions), len(result.AppliedActions))
	}
	if result.PlannedActions[0].Count != 2 {
		t.Fatalf("unexpected planned rule count: %d", result.PlannedActions[0].Count)
	}
	if result.AppliedActions[0].Count != 3 {
		t.Fatalf("unexpected applied count: %d", result.AppliedActions[0].Count)
	}
	if len(repo.ruleSyncCalls) != 2 {
		t.Fatalf("expected 2 rule sync calls (.awm + root canonical sources), got %d", len(repo.ruleSyncCalls))
	}
	firstCall := repo.ruleSyncCalls[0]
	if firstCall.SourcePath != ".awm/awm-rules.yaml" {
		t.Fatalf("unexpected awm primary source path: %q", firstCall.SourcePath)
	}
	if len(firstCall.Pointers) != 2 {
		t.Fatalf("unexpected primary pointer count: %d", len(firstCall.Pointers))
	}
	explicitFound := false
	generatedFound := false
	softFound := false
	for _, pointer := range firstCall.Pointers {
		if pointer.RuleID == "rule.explicit" {
			explicitFound = true
			if pointer.Enforcement == "soft" {
				softFound = true
			}
		}
		if strings.HasPrefix(pointer.RuleID, "rule-") {
			generatedFound = true
		}
	}
	if !explicitFound {
		t.Fatalf("expected explicit rule id in primary sync payload: %+v", firstCall.Pointers)
	}
	if !generatedFound {
		t.Fatalf("expected deterministic generated rule id in primary sync payload: %+v", firstCall.Pointers)
	}
	if !softFound {
		t.Fatalf("expected explicit rule to preserve soft enforcement: %+v", firstCall.Pointers)
	}
	if !strings.Contains(result.Summary, "apply complete") {
		t.Fatalf("unexpected summary: %q", result.Summary)
	}
}

func TestHealthFix_AllFixerExpandsToDefaultFixers(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("tracked"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-rules.yaml"), []byte("version: awm.rules.v1\nrules:\n  - summary: Keep tests green\n"), 0o644); err != nil {
		t.Fatalf("write ruleset: %v", err)
	}

	repo := &trackingRuleSyncRepository{fakeRepository: &fakeRepository{
		inventoryResults: []core.PointerInventory{{Path: "tracked.txt", IsStale: false}},
	}}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.runGitCommand = func(_ context.Context, projectRoot string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "diff --name-status --find-renames HEAD":
			return "M\ttracked.txt\n", nil
		case "ls-files --others --exclude-standard":
			return "new.txt\n", nil
		case "ls-files --cached --others --exclude-standard":
			return "tracked.txt\nnew.txt\n", nil
		case "ls-files --deleted":
			return "", nil
		default:
			return "", nil
		}
	}

	apply := false
	health, apiErr := svc.Health(context.Background(), v1.HealthPayload{
		ProjectID:   "project.alpha",
		Apply:       &apply,
		ProjectRoot: root,
		Fixers:      []v1.HealthFixer{v1.HealthFixerAll},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	result := health.Fix
	if result == nil {
		t.Fatalf("expected fix result, got %+v", health)
	}
	if !result.DryRun {
		t.Fatalf("expected dry_run=true, got false")
	}
	if got := len(result.PlannedActions); got != len(defaultHealthFixers) {
		t.Fatalf("expected %d planned actions, got %d", len(defaultHealthFixers), got)
	}
	gotFixers := make([]v1.HealthFixer, 0, len(result.PlannedActions))
	for _, action := range result.PlannedActions {
		gotFixers = append(gotFixers, action.Fixer)
	}
	if !reflect.DeepEqual(gotFixers, defaultHealthFixers) {
		t.Fatalf("unexpected fixer expansion: got %v want %v", gotFixers, defaultHealthFixers)
	}
}

func TestSync_IntegratesCanonicalRulesetSync(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-rules.yaml"), []byte("version: awm.rules.v1\nrules:\n  - summary: Keep tests green\n"), 0o644); err != nil {
		t.Fatalf("write canonical ruleset: %v", err)
	}

	repo := &trackingRuleSyncRepository{
		fakeRepository: &fakeRepository{
			syncResults: []core.SyncApplyResult{{Updated: 1}},
		},
		ruleSyncResults: []core.RulePointerSyncResult{{Upserted: 1}, {Upserted: 0}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.runGitCommand = func(_ context.Context, projectRoot string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "ls-tree -r HEAD":
			return "100644 blob 1111111111111111111111111111111111111111\ttracked.txt\n", nil
		default:
			return "", nil
		}
	}

	_, apiErr := svc.Sync(context.Background(), v1.SyncPayload{
		ProjectID:   "project.alpha",
		ProjectRoot: root,
		Mode:        "full",
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(repo.ruleSyncCalls) != 2 {
		t.Fatalf("expected ruleset sync during sync flow across canonical sources, got %d calls", len(repo.ruleSyncCalls))
	}
	if len(repo.ruleSyncCalls[0].Pointers) != 1 {
		t.Fatalf("expected parsed canonical pointer in first sync call, got %d", len(repo.ruleSyncCalls[0].Pointers))
	}
}

func TestInit_ReportsDiscoveredRulesetArtifacts(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-rules.yaml"), []byte("version: awm.rules.v1\nrules:\n  - summary: Keep docs current\n"), 0o644); err != nil {
		t.Fatalf("write canonical ruleset: %v", err)
	}

	repo := &trackingRuleSyncRepository{fakeRepository: &fakeRepository{}}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	respectGitIgnore := false
	result, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	hasRulesetWarning := false
	for _, warning := range result.Warnings {
		if strings.Contains(warning, ".awm/awm-rules.yaml") {
			hasRulesetWarning = true
			break
		}
	}
	if !hasRulesetWarning {
		t.Fatalf("expected init warnings to mention canonical ruleset discovery, got %v", result.Warnings)
	}
}

func TestSync_RulesFileOverrideUsesOnlyExplicitSource(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-rules.yaml"), []byte("version: awm.rules.v1\nrules:\n  - summary: Default source\n"), 0o644); err != nil {
		t.Fatalf("write default canonical ruleset: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".awm", "canonical-ruleset.yaml"), []byte("version: awm.rules.v1\nrules:\n  - summary: Override source\n"), 0o644); err != nil {
		t.Fatalf("write override ruleset: %v", err)
	}

	repo := &trackingRuleSyncRepository{
		fakeRepository: &fakeRepository{
			syncResults: []core.SyncApplyResult{{Updated: 1}},
		},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.runGitCommand = func(_ context.Context, projectRoot string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "ls-tree -r HEAD":
			return "100644 blob 1111111111111111111111111111111111111111\ttracked.txt\n", nil
		default:
			return "", nil
		}
	}

	_, apiErr := svc.Sync(context.Background(), v1.SyncPayload{
		ProjectID:   "project.alpha",
		ProjectRoot: root,
		Mode:        "full",
		RulesFile:   ".awm/canonical-ruleset.yaml",
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	if len(repo.ruleSyncCalls) != 1 {
		t.Fatalf("expected one rule sync call for explicit rules_file, got %d", len(repo.ruleSyncCalls))
	}
	call := repo.ruleSyncCalls[0]
	if call.SourcePath != ".awm/canonical-ruleset.yaml" {
		t.Fatalf("unexpected source path: %q", call.SourcePath)
	}
	if len(call.Pointers) != 1 {
		t.Fatalf("expected one parsed pointer from explicit rules_file, got %d", len(call.Pointers))
	}
	if call.Pointers[0].Summary != "Override source" {
		t.Fatalf("unexpected pointer summary from explicit source: %q", call.Pointers[0].Summary)
	}
}
