package backend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func TestSync_ChangedDefaultsAndDeterministicProcessedPaths(t *testing.T) {
	repo := &fakeRepository{
		syncResults: []core.SyncApplyResult{{
			Updated:            3,
			MarkedStale:        0,
			NewCandidates:      1,
			DeletedMarkedStale: 2,
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	var gitCalls []string
	svc.runGitCommand = func(_ context.Context, projectRoot string, args ...string) (string, error) {
		call := projectRoot + "::" + strings.Join(args, " ")
		gitCalls = append(gitCalls, call)
		switch strings.Join(args, " ") {
		case "diff --name-status --find-renames HEAD~1..HEAD":
			return "M\t ./b//two.go \nR100\told/path.go\tnew\\\\path.go\nD\tz/delete.go\nA\ta/one.go\nM\ta/one.go\n", nil
		case "ls-tree -r HEAD -- a/one.go b/two.go new/path.go":
			return "" +
				"100644 blob aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\ta/one.go\n" +
				"100644 blob bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\tb/two.go\n" +
				"100644 blob cccccccccccccccccccccccccccccccccccccccc\tnew/path.go\n", nil
		default:
			t.Fatalf("unexpected git call: %s", call)
		}
		return "", nil
	}

	result, apiErr := svc.Sync(context.Background(), v1.SyncPayload{
		ProjectID: "project.alpha",
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(gitCalls) != 2 {
		t.Fatalf("expected 2 git calls, got %d", len(gitCalls))
	}
	if gitCalls[0] != ".::diff --name-status --find-renames HEAD~1..HEAD" {
		t.Fatalf("unexpected first git call: %s", gitCalls[0])
	}
	if len(repo.syncCalls) != 1 {
		t.Fatalf("expected one apply sync call, got %d", len(repo.syncCalls))
	}
	call := repo.syncCalls[0]
	if call.Mode != "changed" {
		t.Fatalf("unexpected mode: %q", call.Mode)
	}
	if !call.InsertNewCandidates {
		t.Fatalf("expected insert_new_candidates default true")
	}

	wantPaths := []core.SyncPath{
		{Path: "a/one.go", ContentHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{Path: "b/two.go", ContentHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		{Path: "new/path.go", ContentHash: "cccccccccccccccccccccccccccccccccccccccc"},
		{Path: "old/path.go", Deleted: true},
		{Path: "z/delete.go", Deleted: true},
	}
	if !reflect.DeepEqual(call.Paths, wantPaths) {
		t.Fatalf("unexpected sync paths: got %#v want %#v", call.Paths, wantPaths)
	}

	wantProcessed := []string{"a/one.go", "b/two.go", "new/path.go", "old/path.go", "z/delete.go"}
	if !reflect.DeepEqual(result.ProcessedPaths, wantProcessed) {
		t.Fatalf("unexpected processed paths: got %v want %v", result.ProcessedPaths, wantProcessed)
	}
	if result.Updated != 3 || result.MarkedStale != 0 || result.NewCandidates != 3 || result.IndexedStubs != 3 || result.DeletedMarkedStale != 2 {
		t.Fatalf("unexpected result counts: %+v", result)
	}
	if len(repo.upsertStubCalls) != 1 {
		t.Fatalf("expected one stub upsert call, got %d", len(repo.upsertStubCalls))
	}
	if got := repo.upsertStubProjectIDs[0]; got != "project.alpha" {
		t.Fatalf("unexpected stub upsert project id: %q", got)
	}
	if got := len(repo.upsertStubCalls[0]); got != 3 {
		t.Fatalf("unexpected stub upsert count: %d", got)
	}
}

func TestSync_ExplicitInsertNewCandidatesFalseHonored(t *testing.T) {
	repo := &fakeRepository{
		syncResults: []core.SyncApplyResult{{Updated: 1}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	var gitCalls []string
	svc.runGitCommand = func(_ context.Context, projectRoot string, args ...string) (string, error) {
		call := projectRoot + "::" + strings.Join(args, " ")
		gitCalls = append(gitCalls, call)
		switch strings.Join(args, " ") {
		case "diff --name-status --find-renames base..main":
			return "M\tsrc/main.go\n", nil
		case "ls-tree -r main -- src/main.go":
			return "100644 blob dddddddddddddddddddddddddddddddddddddddd\tsrc/main.go\n", nil
		default:
			t.Fatalf("unexpected git call: %s", call)
		}
		return "", nil
	}

	result, apiErr := svc.Sync(context.Background(), v1.SyncPayload{
		ProjectID:           "project.alpha",
		Mode:                "changed",
		GitRange:            "base..main",
		InsertNewCandidates: new(false),
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(repo.syncCalls) != 1 {
		t.Fatalf("expected one apply sync call, got %d", len(repo.syncCalls))
	}
	if repo.syncCalls[0].InsertNewCandidates {
		t.Fatalf("expected explicit false insert_new_candidates to be honored")
	}
	if !reflect.DeepEqual(result.ProcessedPaths, []string{"src/main.go"}) {
		t.Fatalf("unexpected processed paths: %v", result.ProcessedPaths)
	}
	if result.NewCandidates != 0 || result.IndexedStubs != 0 {
		t.Fatalf("expected zero indexed stubs when disabled, got %+v", result)
	}
	if len(repo.upsertStubCalls) != 0 {
		t.Fatalf("did not expect stub upsert calls when disabled, got %d", len(repo.upsertStubCalls))
	}
	if len(gitCalls) != 2 || gitCalls[0] != ".::diff --name-status --find-renames base..main" {
		t.Fatalf("unexpected git calls: %v", gitCalls)
	}
}

func TestSync_FullModeMapsRepositoryCounters(t *testing.T) {
	repo := &fakeRepository{
		syncResults: []core.SyncApplyResult{{
			Updated:            7,
			MarkedStale:        3,
			NewCandidates:      2,
			DeletedMarkedStale: 0,
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	svc.runGitCommand = func(_ context.Context, projectRoot string, args ...string) (string, error) {
		if projectRoot != "/repo/root" {
			t.Fatalf("unexpected project root: %s", projectRoot)
		}
		if strings.Join(args, " ") != "ls-tree -r HEAD" {
			t.Fatalf("unexpected git args: %v", args)
		}
		return "" +
			"100644 blob ffffffffffffffffffffffffffffffffffffffff\tz/file.go\n" +
			"100644 blob 1111111111111111111111111111111111111111\ta/file.go\n", nil
	}

	result, apiErr := svc.Sync(context.Background(), v1.SyncPayload{
		ProjectID:   "project.alpha",
		Mode:        "full",
		ProjectRoot: "/repo/root",
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	if len(repo.syncCalls) != 1 {
		t.Fatalf("expected one apply sync call, got %d", len(repo.syncCalls))
	}
	call := repo.syncCalls[0]
	if call.Mode != "full" {
		t.Fatalf("unexpected mode: %q", call.Mode)
	}
	wantPaths := []core.SyncPath{
		{Path: "a/file.go", ContentHash: "1111111111111111111111111111111111111111"},
		{Path: "z/file.go", ContentHash: "ffffffffffffffffffffffffffffffffffffffff"},
	}
	if !reflect.DeepEqual(call.Paths, wantPaths) {
		t.Fatalf("unexpected paths: got %#v want %#v", call.Paths, wantPaths)
	}
	if result.Updated != 7 || result.MarkedStale != 3 || result.NewCandidates != 2 || result.IndexedStubs != 2 || result.DeletedMarkedStale != 0 {
		t.Fatalf("unexpected result counters: %+v", result)
	}
	if !reflect.DeepEqual(result.ProcessedPaths, []string{"a/file.go", "z/file.go"}) {
		t.Fatalf("unexpected processed paths: %v", result.ProcessedPaths)
	}
	if len(repo.upsertStubCalls) != 1 || len(repo.upsertStubCalls[0]) != 2 {
		t.Fatalf("expected 2 stub upserts, got %+v", repo.upsertStubCalls)
	}
}

func TestSync_RepositoryErrorMapsInternalError(t *testing.T) {
	repo := &fakeRepository{
		syncErrors: []error{errors.New("apply failed")},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	svc.runGitCommand = func(_ context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "diff --name-status --find-renames HEAD~1..HEAD":
			return "M\tsrc/main.go\n", nil
		case "ls-tree -r HEAD -- src/main.go":
			return "100644 blob 0123456789abcdef0123456789abcdef01234567\tsrc/main.go\n", nil
		default:
			t.Fatalf("unexpected git args: %v", args)
		}
		return "", nil
	}

	_, apiErr := svc.Sync(context.Background(), v1.SyncPayload{
		ProjectID: "project.alpha",
	})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INTERNAL_ERROR" {
		t.Fatalf("unexpected API error code: got %q want INTERNAL_ERROR", apiErr.Code)
	}
	details, ok := apiErr.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected map details, got %T", apiErr.Details)
	}
	if got := details["operation"]; got != "apply_sync" {
		t.Fatalf("unexpected operation detail: %#v", details)
	}
}

func TestSync_WorkingTreeModeIncludesUntrackedAndUsesFilesystemHashes(t *testing.T) {
	repo := &fakeRepository{
		syncResults: []core.SyncApplyResult{{Updated: 2, NewCandidates: 1}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, "src"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "src", "tracked.go"), []byte("package src\n\nfunc tracked() {}\n"), 0o644); err != nil {
		t.Fatalf("write tracked.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "src", "new.go"), []byte("package src\n\nfunc created() {}\n"), 0o644); err != nil {
		t.Fatalf("write new.go: %v", err)
	}

	var gitCalls []string
	svc.runGitCommand = func(_ context.Context, root string, args ...string) (string, error) {
		if root != projectRoot {
			t.Fatalf("unexpected project root: %s", root)
		}
		joined := strings.Join(args, " ")
		gitCalls = append(gitCalls, joined)
		switch joined {
		case "diff --name-status --find-renames HEAD":
			return "M\tsrc/tracked.go\nM\t.gitignore\n", nil
		case "ls-files --others --exclude-standard":
			return "src/new.go\n.awm/context.db-wal\n", nil
		default:
			t.Fatalf("unexpected git args: %s", joined)
		}
		return "", nil
	}

	result, apiErr := svc.Sync(context.Background(), v1.SyncPayload{
		ProjectID:   "project.alpha",
		Mode:        "working_tree",
		ProjectRoot: projectRoot,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(repo.syncCalls) != 1 {
		t.Fatalf("expected one apply sync call, got %d", len(repo.syncCalls))
	}
	call := repo.syncCalls[0]
	if call.Mode != "working_tree" {
		t.Fatalf("unexpected sync mode: %q", call.Mode)
	}
	if len(call.Paths) != 2 {
		t.Fatalf("expected 2 sync paths, got %d", len(call.Paths))
	}
	for _, p := range call.Paths {
		if p.Deleted {
			t.Fatalf("did not expect deleted path in working tree test: %+v", p)
		}
		if strings.TrimSpace(p.ContentHash) == "" {
			t.Fatalf("expected content hash for path %+v", p)
		}
	}
	if !reflect.DeepEqual(result.ProcessedPaths, []string{"src/new.go", "src/tracked.go"}) {
		t.Fatalf("unexpected processed paths: %v", result.ProcessedPaths)
	}
	if len(gitCalls) != 2 {
		t.Fatalf("expected two git calls, got %v", gitCalls)
	}
}
