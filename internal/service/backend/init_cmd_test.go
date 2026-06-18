package backend

import (
	"context"
	"encoding/json"
	"errors"
	bootstrapkit "github.com/bonztm/agent-workflow-manager/internal/bootstrap"
	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInit_DefaultEphemeralAndDeterministicEnumeration(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatalf("mkdir dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "dir", "b.go"), []byte("package dir"), 0o644); err != nil {
		t.Fatalf("write b.go: %v", err)
	}

	respectGitIgnore := false
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID: "project.alpha",
			ReceiptID: "receipt.abc123",
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	if result.CandidatesPersisted {
		t.Fatalf("expected candidates_persisted=false by default")
	}
	if result.OutputCandidatesPath != "" {
		t.Fatalf("expected empty output path when not persisting, got %q", result.OutputCandidatesPath)
	}
	if result.CandidateCount != 2 {
		t.Fatalf("unexpected candidate count: got %d want 2", result.CandidateCount)
	}
	if result.IndexedStubs != 2 {
		t.Fatalf("unexpected indexed stub count: got %d want 2", result.IndexedStubs)
	}
	if len(repo.upsertStubCalls) != 1 || len(repo.upsertStubCalls[0]) != 2 {
		t.Fatalf("expected 2 stub upserts, got %+v", repo.upsertStubCalls)
	}
	defaultPersistPath := filepath.Join(root, ".awm", "init_candidates.json")
	if _, err := os.Stat(defaultPersistPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no persisted candidates file by default, stat err=%v", err)
	}

	again, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error on second run: %+v", apiErr)
	}
	if again.CandidateCount != 2 {
		t.Fatalf("expected deterministic candidate count across runs, got %d", again.CandidateCount)
	}
	if again.IndexedStubs != 2 {
		t.Fatalf("expected deterministic indexed stub count across runs, got %d", again.IndexedStubs)
	}
}

func TestInit_PersistCandidatesWritesDefaultAwmPath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatalf("mkdir dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "dir", "b.go"), []byte("package dir"), 0o644); err != nil {
		t.Fatalf("write b.go: %v", err)
	}

	persist := true
	respectGitIgnore := false
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID: "project.alpha",
			ReceiptID: "receipt.abc123",
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:         "project.alpha",
		ProjectRoot:       root,
		PersistCandidates: &persist,
		RespectGitIgnore:  &respectGitIgnore,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	outputPath := filepath.Join(root, ".awm", "init_candidates.json")
	if !result.CandidatesPersisted {
		t.Fatalf("expected candidates_persisted=true")
	}
	if result.OutputCandidatesPath != outputPath {
		t.Fatalf("unexpected output path: got %q want %q", result.OutputCandidatesPath, outputPath)
	}
	if result.CandidateCount != 2 {
		t.Fatalf("unexpected candidate count: got %d want 2", result.CandidateCount)
	}
	if result.IndexedStubs != 2 {
		t.Fatalf("unexpected indexed stub count: got %d want 2", result.IndexedStubs)
	}

	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	var parsed struct {
		Candidates []string `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse output file: %v", err)
	}
	wantCandidates := []string{"a.txt", "dir/b.go"}
	if !reflect.DeepEqual(parsed.Candidates, wantCandidates) {
		t.Fatalf("unexpected candidates: got %v want %v", parsed.Candidates, wantCandidates)
	}
}

func TestInit_CustomOutputPathAndWarningsDeterministic(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}

	output := "reports/candidates.json"
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID: "project.alpha",
			ReceiptID: "receipt.abc123",
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:            "project.alpha",
		ProjectRoot:          root,
		OutputCandidatesPath: &output,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	wantPath := filepath.Join(root, "reports", "candidates.json")
	if result.OutputCandidatesPath != wantPath {
		t.Fatalf("unexpected output path: got %q want %q", result.OutputCandidatesPath, wantPath)
	}
	if !result.CandidatesPersisted {
		t.Fatalf("expected candidates_persisted=true when output path is explicit")
	}
	if result.CandidateCount != 1 {
		t.Fatalf("unexpected candidate count: got %d want 1", result.CandidateCount)
	}
	wantWarnings := []string{"respect_gitignore fallback to filesystem walk"}
	if !reflect.DeepEqual(result.Warnings, wantWarnings) {
		t.Fatalf("unexpected warnings: got %v want %v", result.Warnings, wantWarnings)
	}
}

func TestInit_SeedsCanonicalScaffoldFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}

	respectGitIgnore := false
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	rulesRaw, err := os.ReadFile(filepath.Join(root, ".awm", "awm-rules.yaml"))
	if err != nil {
		t.Fatalf("read scaffolded rules file: %v", err)
	}
	if string(rulesRaw) != "version: awm.rules.v1\nrules: []\n" {
		t.Fatalf("unexpected scaffolded rules contents: %q", string(rulesRaw))
	}

	tagsRaw, err := os.ReadFile(filepath.Join(root, ".awm", "awm-tags.yaml"))
	if err != nil {
		t.Fatalf("read scaffolded tags file: %v", err)
	}
	if string(tagsRaw) != "version: awm.tags.v1\ncanonical_tags: {}\n" {
		t.Fatalf("unexpected scaffolded tags contents: %q", string(tagsRaw))
	}

	testsRaw, err := os.ReadFile(filepath.Join(root, ".awm", "awm-tests.yaml"))
	if err != nil {
		t.Fatalf("read scaffolded tests file: %v", err)
	}
	if string(testsRaw) != "version: awm.tests.v1\ndefaults:\n  cwd: .\n  timeout_sec: 300\ntests: []\n" {
		t.Fatalf("unexpected scaffolded tests contents: %q", string(testsRaw))
	}

	workflowsRaw, err := os.ReadFile(filepath.Join(root, ".awm", "awm-workflows.yaml"))
	if err != nil {
		t.Fatalf("read scaffolded workflows file: %v", err)
	}
	if string(workflowsRaw) != "version: awm.workflows.v1\ncompletion:\n  required_tasks: []\n" {
		t.Fatalf("unexpected scaffolded workflows contents: %q", string(workflowsRaw))
	}

	envExampleRaw, err := os.ReadFile(filepath.Join(root, ".env.example"))
	if err != nil {
		t.Fatalf("read scaffolded env example: %v", err)
	}
	wantEnvExample := "# AWM runtime configuration\n# Copy this file to .env to override local defaults.\nAWM_PROJECT_ID=myproject\nAWM_PROJECT_ROOT=/path/to/repo\nAWM_SQLITE_PATH=.awm/context.db\nAWM_PG_DSN=postgres://user:pass@localhost:5432/agents_context?sslmode=disable\nAWM_UNBOUNDED=false\nAWM_LOG_LEVEL=info\nAWM_LOG_SINK=stderr\n"
	if string(envExampleRaw) != wantEnvExample {
		t.Fatalf("unexpected scaffolded env example contents: %q", string(envExampleRaw))
	}

	gitignoreRaw, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read scaffolded gitignore: %v", err)
	}
	if string(gitignoreRaw) != ".awm/context.db\n.awm/context.db-shm\n.awm/context.db-wal\n" {
		t.Fatalf("unexpected scaffolded gitignore contents: %q", string(gitignoreRaw))
	}
}

func TestInit_ExcludesManagedFilesFromInitialCandidateIndex(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	files := map[string]string{
		"README.md":               "# hello\n",
		".env":                    "SECRET=value\n",
		".env.example":            "AWM_SQLITE_PATH=.awm/context.db\n",
		".gitignore":              ".awm/context.db\n",
		"awm-rules.yaml":          "version: awm.rules.v1\nrules: []\n",
		"awm-tests.yaml":          "version: awm.tests.v1\ndefaults:\n  cwd: .\n  timeout_sec: 60\ntests: []\n",
		"awm-workflows.yaml":      "version: awm.workflows.v1\ncompletion:\n  required_tasks: []\n",
		".awm/context.db":         "sqlite",
		".awm/context.db-wal":     "wal",
		".awm/context.db-shm":     "shm",
		".awm/awm-rules.yaml":     "version: awm.rules.v1\nrules: []\n",
		".awm/awm-tags.yaml":      "version: awm.tags.v1\ncanonical_tags: {}\n",
		".awm/awm-tests.yaml":     "version: awm.tests.v1\ndefaults:\n  cwd: .\n  timeout_sec: 300\ntests: []\n",
		".awm/awm-workflows.yaml": "version: awm.workflows.v1\ncompletion:\n  required_tasks: []\n",
	}
	for rel, contents := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	respectGitIgnore := false
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.CandidateCount != 1 || result.IndexedStubs != 1 {
		t.Fatalf("unexpected bootstrap counts: %+v", result)
	}
	if len(repo.upsertStubCalls) != 1 || len(repo.upsertStubCalls[0]) != 1 {
		t.Fatalf("expected exactly one stub upsert, got %+v", repo.upsertStubCalls)
	}
	if got := repo.upsertStubCalls[0][0].Path; got != "README.md" {
		t.Fatalf("unexpected indexed path: got %q want %q", got, "README.md")
	}
}

func TestInit_SeedsSuggestedCanonicalTagsWhenRepoSignalsThem(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "receipt"), 0o755); err != nil {
		t.Fatalf("mkdir receipt dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "receipt", "fetch_receipt.go"), []byte("package receipt"), 0o644); err != nil {
		t.Fatalf("write fetch_receipt.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "receipt", "report_receipt.go"), []byte("package receipt"), 0o644); err != nil {
		t.Fatalf("write report_receipt.go: %v", err)
	}

	respectGitIgnore := false
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	tagsRaw, err := os.ReadFile(filepath.Join(root, ".awm", "awm-tags.yaml"))
	if err != nil {
		t.Fatalf("read scaffolded tags file: %v", err)
	}

	doc := canonicalTagsDocumentV1{}
	if err := yaml.Unmarshal(tagsRaw, &doc); err != nil {
		t.Fatalf("parse scaffolded tags file: %v", err)
	}
	if doc.Version != canonicalTagsVersionV1 {
		t.Fatalf("unexpected scaffolded tags version: %q", doc.Version)
	}
	if _, ok := doc.CanonicalTags["receipt"]; !ok {
		t.Fatalf("expected inferred receipt tag, got %v", doc.CanonicalTags)
	}
}

func TestInit_PopulatesExistingBlankCanonicalTagsFileWithSuggestions(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal", "receipt"), 0o755); err != nil {
		t.Fatalf("mkdir receipt dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "receipt", "fetch_receipt.go"), []byte("package receipt"), 0o644); err != nil {
		t.Fatalf("write fetch_receipt.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "receipt", "report_receipt.go"), []byte("package receipt"), 0o644); err != nil {
		t.Fatalf("write report_receipt.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-tags.yaml"), []byte("version: awm.tags.v1\ncanonical_tags: {}\n"), 0o644); err != nil {
		t.Fatalf("write blank tags file: %v", err)
	}

	respectGitIgnore := false
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	tagsRaw, err := os.ReadFile(filepath.Join(root, ".awm", "awm-tags.yaml"))
	if err != nil {
		t.Fatalf("read scaffolded tags file: %v", err)
	}

	doc := canonicalTagsDocumentV1{}
	if err := yaml.Unmarshal(tagsRaw, &doc); err != nil {
		t.Fatalf("parse scaffolded tags file: %v", err)
	}
	if _, ok := doc.CanonicalTags["receipt"]; !ok {
		t.Fatalf("expected inferred receipt tag, got %v", doc.CanonicalTags)
	}
}

func TestInit_DoesNotOverwriteExistingCanonicalScaffoldFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	rulesContent := []byte("version: awm.rules.v1\nrules:\n  - summary: Keep tests green\n")
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-rules.yaml"), rulesContent, 0o644); err != nil {
		t.Fatalf("write existing rules file: %v", err)
	}
	tagsContent := []byte("version: awm.tags.v1\ncanonical_tags:\n  backend:\n    - svc\n")
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-tags.yaml"), tagsContent, 0o644); err != nil {
		t.Fatalf("write existing tags file: %v", err)
	}
	testsContent := []byte("version: awm.tests.v1\ndefaults:\n  cwd: tools\n  timeout_sec: 120\ntests:\n  - id: smoke\n    summary: Run smoke tests\n    command:\n      argv: [\"go\", \"test\", \"./...\"]\n")
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-tests.yaml"), testsContent, 0o644); err != nil {
		t.Fatalf("write existing tests file: %v", err)
	}
	workflowsContent := []byte("version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: verify:tests\n")
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-workflows.yaml"), workflowsContent, 0o644); err != nil {
		t.Fatalf("write existing workflows file: %v", err)
	}
	envExampleContent := []byte("AWM_SQLITE_PATH=.awm/existing.db\n")
	if err := os.WriteFile(filepath.Join(root, ".env.example"), envExampleContent, 0o644); err != nil {
		t.Fatalf("write existing env example: %v", err)
	}
	gitignoreContent := []byte("node_modules/\n.awm/context.db\n")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), gitignoreContent, 0o644); err != nil {
		t.Fatalf("write existing gitignore: %v", err)
	}

	respectGitIgnore := false
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	rulesRaw, err := os.ReadFile(filepath.Join(root, ".awm", "awm-rules.yaml"))
	if err != nil {
		t.Fatalf("read rules file: %v", err)
	}
	if !reflect.DeepEqual(rulesRaw, rulesContent) {
		t.Fatalf("rules file was overwritten: got %q want %q", string(rulesRaw), string(rulesContent))
	}

	tagsRaw, err := os.ReadFile(filepath.Join(root, ".awm", "awm-tags.yaml"))
	if err != nil {
		t.Fatalf("read tags file: %v", err)
	}
	if !reflect.DeepEqual(tagsRaw, tagsContent) {
		t.Fatalf("tags file was overwritten: got %q want %q", string(tagsRaw), string(tagsContent))
	}

	testsRaw, err := os.ReadFile(filepath.Join(root, ".awm", "awm-tests.yaml"))
	if err != nil {
		t.Fatalf("read tests file: %v", err)
	}
	if !reflect.DeepEqual(testsRaw, testsContent) {
		t.Fatalf("tests file was overwritten: got %q want %q", string(testsRaw), string(testsContent))
	}

	workflowsRaw, err := os.ReadFile(filepath.Join(root, ".awm", "awm-workflows.yaml"))
	if err != nil {
		t.Fatalf("read workflows file: %v", err)
	}
	if !reflect.DeepEqual(workflowsRaw, workflowsContent) {
		t.Fatalf("workflows file was overwritten: got %q want %q", string(workflowsRaw), string(workflowsContent))
	}

	envExampleRaw, err := os.ReadFile(filepath.Join(root, ".env.example"))
	if err != nil {
		t.Fatalf("read env example: %v", err)
	}
	wantEnvExample := "AWM_SQLITE_PATH=.awm/existing.db\n\n# AWM runtime configuration\nAWM_PROJECT_ID=myproject\nAWM_PROJECT_ROOT=/path/to/repo\nAWM_PG_DSN=postgres://user:pass@localhost:5432/agents_context?sslmode=disable\nAWM_UNBOUNDED=false\nAWM_LOG_LEVEL=info\nAWM_LOG_SINK=stderr\n"
	if string(envExampleRaw) != wantEnvExample {
		t.Fatalf("unexpected env example contents: got %q want %q", string(envExampleRaw), wantEnvExample)
	}

	gitignoreRaw, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	wantGitIgnore := "node_modules/\n.awm/context.db\n.awm/context.db-shm\n.awm/context.db-wal\n"
	if string(gitignoreRaw) != wantGitIgnore {
		t.Fatalf("unexpected gitignore contents: got %q want %q", string(gitignoreRaw), wantGitIgnore)
	}
}

func TestInit_DoesNotSeedPrimaryTestsFileWhenRootTestsFileExists(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	rootTestsContent := []byte("version: awm.tests.v1\ndefaults:\n  cwd: .\n  timeout_sec: 60\ntests:\n  - id: root-smoke\n    summary: Root tests file\n    command:\n      argv: [\"true\"]\n")
	if err := os.WriteFile(filepath.Join(root, "awm-tests.yaml"), rootTestsContent, 0o644); err != nil {
		t.Fatalf("write root tests file: %v", err)
	}

	respectGitIgnore := false
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	if _, err := os.Stat(filepath.Join(root, ".awm", "awm-tests.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no primary scaffold when root tests file exists, stat err=%v", err)
	}

	gotRootTestsContent, err := os.ReadFile(filepath.Join(root, "awm-tests.yaml"))
	if err != nil {
		t.Fatalf("read root tests file: %v", err)
	}
	if !reflect.DeepEqual(gotRootTestsContent, rootTestsContent) {
		t.Fatalf("root tests file was overwritten: got %q want %q", string(gotRootTestsContent), string(rootTestsContent))
	}
}

func TestInit_DoesNotSeedPrimaryWorkflowsFileWhenRootWorkflowsFileExists(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	rootWorkflowsContent := []byte("version: awm.workflows.v1\ncompletion:\n  required_tasks:\n    - key: review:cross-llm\n")
	if err := os.WriteFile(filepath.Join(root, "awm-workflows.yaml"), rootWorkflowsContent, 0o644); err != nil {
		t.Fatalf("write root workflows file: %v", err)
	}

	respectGitIgnore := false
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	if _, err := os.Stat(filepath.Join(root, ".awm", "awm-workflows.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no primary scaffold when root workflows file exists, stat err=%v", err)
	}

	gotRootWorkflowsContent, err := os.ReadFile(filepath.Join(root, "awm-workflows.yaml"))
	if err != nil {
		t.Fatalf("read root workflows file: %v", err)
	}
	if !reflect.DeepEqual(gotRootWorkflowsContent, rootWorkflowsContent) {
		t.Fatalf("root workflows file was overwritten: got %q want %q", string(gotRootWorkflowsContent), string(rootWorkflowsContent))
	}
}

func TestInit_ApplyStarterContractSeedsContractsAndIndexesThem(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	respectGitIgnore := false
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
		ApplyTemplates:   []string{"starter-contract"},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	if result.CandidateCount != 3 || result.IndexedStubs != 3 {
		t.Fatalf("unexpected bootstrap counts: %+v", result)
	}
	if len(repo.upsertStubCalls) != 1 {
		t.Fatalf("expected one stub upsert, got %+v", repo.upsertStubCalls)
	}
	gotPaths := make([]string, 0, len(repo.upsertStubCalls[0]))
	for _, stub := range repo.upsertStubCalls[0] {
		gotPaths = append(gotPaths, stub.Path)
	}
	if wantPaths := []string{"AGENTS.md", "CLAUDE.md", "README.md"}; !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("unexpected indexed paths: got %v want %v", gotPaths, wantPaths)
	}

	templateResult, ok := initTemplateResultByID(result.TemplateResults, "starter-contract")
	if !ok {
		t.Fatalf("expected starter-contract template result, got %+v", result.TemplateResults)
	}
	if wantCreated := []string{".awm/awm-work-loop.md", "AGENTS.md", "CLAUDE.md"}; !reflect.DeepEqual(templateResult.Created, wantCreated) {
		t.Fatalf("unexpected created paths: got %v want %v", templateResult.Created, wantCreated)
	}
	if wantUpdated := []string{".awm/awm-rules.yaml"}; !reflect.DeepEqual(templateResult.Updated, wantUpdated) {
		t.Fatalf("unexpected updated paths: got %v want %v", templateResult.Updated, wantUpdated)
	}

	agentsRaw, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if len(agentsRaw) == 0 {
		t.Fatalf("AGENTS.md is empty after starter-contract scaffold")
	}

	rulesRaw, err := os.ReadFile(filepath.Join(root, ".awm", "awm-rules.yaml"))
	if err != nil {
		t.Fatalf("read scaffolded rules: %v", err)
	}
	if !strings.Contains(string(rulesRaw), "rule_startup_context") {
		t.Fatalf("expected starter rules scaffold, got %q", string(rulesRaw))
	}
}

func TestInit_ApplyDetailedPlanningEnforcementSeedsFeaturePlanningScaffold(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	respectGitIgnore := false
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
		ApplyTemplates:   []string{"detailed-planning-enforcement"},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	templateResult, ok := initTemplateResultByID(result.TemplateResults, "detailed-planning-enforcement")
	if !ok {
		t.Fatalf("expected detailed-planning-enforcement template result, got %+v", result.TemplateResults)
	}
	if wantCreated := []string{"docs/feature-plans.md", "scripts/awm-feature-plan-validate.py"}; !reflect.DeepEqual(templateResult.Created, wantCreated) {
		t.Fatalf("unexpected created paths: got %v want %v", templateResult.Created, wantCreated)
	}
	if wantUpdated := []string{".awm/awm-rules.yaml", ".awm/awm-tests.yaml"}; !reflect.DeepEqual(templateResult.Updated, wantUpdated) {
		t.Fatalf("unexpected updated paths: got %v want %v", templateResult.Updated, wantUpdated)
	}
	if len(repo.upsertStubCalls) != 1 {
		t.Fatalf("expected one stub upsert, got %+v", repo.upsertStubCalls)
	}
	gotPaths := make([]string, 0, len(repo.upsertStubCalls[0]))
	for _, stub := range repo.upsertStubCalls[0] {
		gotPaths = append(gotPaths, stub.Path)
	}
	for _, required := range []string{"README.md", "docs/feature-plans.md", "scripts/awm-feature-plan-validate.py"} {
		if !containsString(gotPaths, required) {
			t.Fatalf("expected indexed template path %q in %v", required, gotPaths)
		}
	}

	rulesRaw, err := os.ReadFile(filepath.Join(root, ".awm", "awm-rules.yaml"))
	if err != nil {
		t.Fatalf("read rules scaffold: %v", err)
	}
	if !strings.Contains(string(rulesRaw), "rule_feature_plan_schema") {
		t.Fatalf("expected feature plan rule scaffold, got %q", string(rulesRaw))
	}

	testsRaw, err := os.ReadFile(filepath.Join(root, ".awm", "awm-tests.yaml"))
	if err != nil {
		t.Fatalf("read tests scaffold: %v", err)
	}
	if !strings.Contains(string(testsRaw), "id: feature-plan-validate") {
		t.Fatalf("expected feature plan verify scaffold, got %q", string(testsRaw))
	}

	planDocRaw, err := os.ReadFile(filepath.Join(root, "docs", "feature-plans.md"))
	if err != nil {
		t.Fatalf("read feature plan doc: %v", err)
	}
	if !strings.Contains(string(planDocRaw), "kind=feature_stream") {
		t.Fatalf("expected feature stream guidance, got %q", string(planDocRaw))
	}

	validatorInfo, err := os.Stat(filepath.Join(root, "scripts", "awm-feature-plan-validate.py"))
	if err != nil {
		t.Fatalf("stat feature plan validator: %v", err)
	}
	if validatorInfo.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected executable validator mode, got %v", validatorInfo.Mode().Perm())
	}
}

func TestInit_DetailedPlanningEnforcementUpgradesPristineStarterScaffolds(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	respectGitIgnore := false
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
		ApplyTemplates:   []string{"starter-contract", "verify-generic"},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error on first run: %+v", apiErr)
	}

	result, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
		ApplyTemplates:   []string{"detailed-planning-enforcement"},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error on second run: %+v", apiErr)
	}

	templateResult, ok := initTemplateResultByID(result.TemplateResults, "detailed-planning-enforcement")
	if !ok {
		t.Fatalf("expected detailed-planning-enforcement template result, got %+v", result.TemplateResults)
	}
	if wantCreated := []string{"docs/feature-plans.md", "scripts/awm-feature-plan-validate.py"}; !reflect.DeepEqual(templateResult.Created, wantCreated) {
		t.Fatalf("unexpected created paths: got %v want %v", templateResult.Created, wantCreated)
	}
	if wantUpdated := []string{".awm/awm-rules.yaml", ".awm/awm-tests.yaml"}; !reflect.DeepEqual(templateResult.Updated, wantUpdated) {
		t.Fatalf("unexpected updated paths: got %v want %v", templateResult.Updated, wantUpdated)
	}
	if templateResult.SkippedConflicts != nil {
		t.Fatalf("expected no template conflicts, got %+v", templateResult.SkippedConflicts)
	}

	testsRaw, err := os.ReadFile(filepath.Join(root, ".awm", "awm-tests.yaml"))
	if err != nil {
		t.Fatalf("read tests scaffold: %v", err)
	}
	for _, snippet := range []string{"id: feature-plan-help", "id: feature-plan-validate"} {
		if !strings.Contains(string(testsRaw), snippet) {
			t.Fatalf("expected upgraded tests scaffold to include %q, got %q", snippet, string(testsRaw))
		}
	}
}

func TestInit_ApplyVerifyProfilesReplacePristineTestsScaffold(t *testing.T) {
	t.Parallel()

	cases := []struct {
		templateID string
		snippets   []string
	}{
		{
			templateID: "verify-generic",
			snippets: []string{
				`id: smoke`,
				`argv: ["awm", "status", "--project-root", "."]`,
				`id: repo-diff-check`,
			},
		},
		{
			templateID: "verify-go",
			snippets: []string{
				`id: smoke`,
				`id: go-build`,
			},
		},
		{
			templateID: "verify-python",
			snippets: []string{
				`id: smoke`,
				`id: python-compile`,
			},
		},
		{
			templateID: "verify-rust",
			snippets: []string{
				`id: smoke`,
				`id: cargo-check`,
			},
		},
		{
			templateID: "verify-ts",
			snippets: []string{
				`id: smoke`,
				`id: ts-build`,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.templateID, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hello\n"), 0o644); err != nil {
				t.Fatalf("write README: %v", err)
			}

			respectGitIgnore := false
			repo := &fakeRepository{}
			svc, err := New(repo)
			if err != nil {
				t.Fatalf("new service: %v", err)
			}

			result, apiErr := svc.Init(context.Background(), v1.InitPayload{
				ProjectID:        "project.alpha",
				ProjectRoot:      root,
				RespectGitIgnore: &respectGitIgnore,
				ApplyTemplates:   []string{tc.templateID},
			})
			if apiErr != nil {
				t.Fatalf("unexpected API error: %+v", apiErr)
			}

			templateResult, ok := initTemplateResultByID(result.TemplateResults, tc.templateID)
			if !ok {
				t.Fatalf("expected %s template result, got %+v", tc.templateID, result.TemplateResults)
			}
			if wantUpdated := []string{".awm/awm-tests.yaml"}; !reflect.DeepEqual(templateResult.Updated, wantUpdated) {
				t.Fatalf("unexpected updated paths: got %v want %v", templateResult.Updated, wantUpdated)
			}

			testsRaw, err := os.ReadFile(filepath.Join(root, ".awm", "awm-tests.yaml"))
			if err != nil {
				t.Fatalf("read tests scaffold: %v", err)
			}
			for _, snippet := range tc.snippets {
				if !strings.Contains(string(testsRaw), snippet) {
					t.Fatalf("expected %s starter contents to include %q, got %q", tc.templateID, snippet, string(testsRaw))
				}
			}
		})
	}
}

func TestInit_ReapplyStarterContractTemplateIsNoOp(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	respectGitIgnore := false
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
		ApplyTemplates:   []string{"starter-contract"},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error on first run: %+v", apiErr)
	}

	again, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
		ApplyTemplates:   []string{"starter-contract"},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error on second run: %+v", apiErr)
	}

	templateResult, ok := initTemplateResultByID(again.TemplateResults, "starter-contract")
	if !ok {
		t.Fatalf("expected starter-contract template result, got %+v", again.TemplateResults)
	}
	if templateResult.Created != nil || templateResult.Updated != nil {
		t.Fatalf("expected no created or updated paths on rerun, got %+v", templateResult)
	}
	if wantUnchanged := []string{".awm/awm-rules.yaml", ".awm/awm-work-loop.md", "AGENTS.md", "CLAUDE.md"}; !reflect.DeepEqual(templateResult.Unchanged, wantUnchanged) {
		t.Fatalf("unexpected unchanged paths: got %v want %v", templateResult.Unchanged, wantUnchanged)
	}
}

func TestInit_TemplateConflictDoesNotOverwriteEditedFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# custom\n"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".awm", "awm-rules.yaml"), []byte(bootstrapkit.BlankRulesContents), 0o644); err != nil {
		t.Fatalf("write pristine rules: %v", err)
	}

	respectGitIgnore := false
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
		ApplyTemplates:   []string{"starter-contract"},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	templateResult, ok := initTemplateResultByID(result.TemplateResults, "starter-contract")
	if !ok {
		t.Fatalf("expected starter-contract template result, got %+v", result.TemplateResults)
	}
	if len(templateResult.SkippedConflicts) != 1 {
		t.Fatalf("expected one skipped conflict, got %+v", templateResult.SkippedConflicts)
	}
	if got := templateResult.SkippedConflicts[0]; got.Path != "AGENTS.md" || got.Reason != "existing file differs" {
		t.Fatalf("unexpected skipped conflict: %+v", got)
	}

	agentsRaw, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if string(agentsRaw) != "# custom\n" {
		t.Fatalf("AGENTS.md was overwritten: %q", string(agentsRaw))
	}
}

func TestInit_ApplyClaudeCommandPackIndexesCreatedFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	respectGitIgnore := false
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
		ApplyTemplates:   []string{"claude-command-pack"},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	if result.CandidateCount != 8 || result.IndexedStubs != 8 {
		t.Fatalf("unexpected bootstrap counts: %+v", result)
	}
	templateResult, ok := initTemplateResultByID(result.TemplateResults, "claude-command-pack")
	if !ok {
		t.Fatalf("expected claude-command-pack result, got %+v", result.TemplateResults)
	}
	if len(templateResult.Created) != 7 {
		t.Fatalf("expected 7 created files, got %+v", templateResult.Created)
	}

	gotPaths := make([]string, 0, len(repo.upsertStubCalls[0]))
	for _, stub := range repo.upsertStubCalls[0] {
		gotPaths = append(gotPaths, stub.Path)
	}
	for _, required := range []string{
		".claude/awm-broker/README.md",
		".claude/commands/awm-context.md",
		".claude/commands/awm-review.md",
	} {
		if !containsString(gotPaths, required) {
			t.Fatalf("expected indexed template path %q in %v", required, gotPaths)
		}
	}
}

func TestInit_ApplyOpenCodePackIndexesCreatedFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	respectGitIgnore := false
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
		ApplyTemplates:   []string{"opencode-pack"},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	if result.CandidateCount != 3 || result.IndexedStubs != 3 {
		t.Fatalf("unexpected bootstrap counts: %+v", result)
	}
	templateResult, ok := initTemplateResultByID(result.TemplateResults, "opencode-pack")
	if !ok {
		t.Fatalf("expected opencode-pack result, got %+v", result.TemplateResults)
	}
	if len(templateResult.Created) != 2 {
		t.Fatalf("expected 2 created files, got %+v", templateResult.Created)
	}

	gotPaths := make([]string, 0, len(repo.upsertStubCalls[0]))
	for _, stub := range repo.upsertStubCalls[0] {
		gotPaths = append(gotPaths, stub.Path)
	}
	for _, required := range []string{
		".opencode/awm-broker/README.md",
		".opencode/awm-broker/AGENTS.example.md",
	} {
		if !containsString(gotPaths, required) {
			t.Fatalf("expected indexed template path %q in %v", required, gotPaths)
		}
	}
}

func TestInit_CodexHooksSeedsRepoLocalHooksIdempotently(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	respectGitIgnore := false
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
		ApplyTemplates:   []string{"codex-hooks"},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	if result.CandidateCount != 7 || result.IndexedStubs != 7 {
		t.Fatalf("unexpected bootstrap counts: %+v", result)
	}
	templateResult, ok := initTemplateResultByID(result.TemplateResults, "codex-hooks")
	if !ok {
		t.Fatalf("expected codex-hooks result, got %+v", result.TemplateResults)
	}
	if wantCreated := []string{
		".codex/config.toml",
		".codex/hooks.json",
		".codex/hooks/awm-common.sh",
		".codex/hooks/awm-prompt-guard.sh",
		".codex/hooks/awm-session-context.sh",
		".codex/hooks/awm-stop-guard.sh",
	}; !reflect.DeepEqual(templateResult.Created, wantCreated) {
		t.Fatalf("unexpected created paths: got %v want %v", templateResult.Created, wantCreated)
	}

	gotPaths := make([]string, 0, len(repo.upsertStubCalls[0]))
	for _, stub := range repo.upsertStubCalls[0] {
		gotPaths = append(gotPaths, stub.Path)
	}
	for _, required := range []string{
		".codex/config.toml",
		".codex/hooks.json",
		".codex/hooks/awm-common.sh",
		".codex/hooks/awm-prompt-guard.sh",
		".codex/hooks/awm-session-context.sh",
		".codex/hooks/awm-stop-guard.sh",
	} {
		if !containsString(gotPaths, required) {
			t.Fatalf("expected indexed template path %q in %v", required, gotPaths)
		}
	}

	again, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
		ApplyTemplates:   []string{"codex-hooks"},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error on rerun: %+v", apiErr)
	}
	againResult, ok := initTemplateResultByID(again.TemplateResults, "codex-hooks")
	if !ok {
		t.Fatalf("expected codex-hooks result on rerun, got %+v", again.TemplateResults)
	}
	if againResult.Created != nil || againResult.Updated != nil {
		t.Fatalf("expected no created or updated paths on rerun, got %+v", againResult)
	}
	if wantUnchanged := []string{
		".codex/config.toml",
		".codex/hooks.json",
		".codex/hooks/awm-common.sh",
		".codex/hooks/awm-prompt-guard.sh",
		".codex/hooks/awm-session-context.sh",
		".codex/hooks/awm-stop-guard.sh",
	}; !reflect.DeepEqual(againResult.Unchanged, wantUnchanged) {
		t.Fatalf("unexpected unchanged paths on rerun: got %v want %v", againResult.Unchanged, wantUnchanged)
	}
}

func TestInit_ClaudeHooksMergesSettingsJSONIdempotently(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	settings := `{"permissions":{"allow":["Bash"]},"hooks":{"PostToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"echo read"}]}]}}`
	if err := os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	respectGitIgnore := false
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
		ApplyTemplates:   []string{"claude-hooks"},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}

	templateResult, ok := initTemplateResultByID(result.TemplateResults, "claude-hooks")
	if !ok {
		t.Fatalf("expected claude-hooks result, got %+v", result.TemplateResults)
	}
	if wantCreated := []string{
		".claude/hooks/awm-edit-state.sh",
		".claude/hooks/awm-receipt-guard.sh",
		".claude/hooks/awm-receipt-mark.sh",
		".claude/hooks/awm-session-context.sh",
		".claude/hooks/awm-stop-guard.sh",
	}; !reflect.DeepEqual(templateResult.Created, wantCreated) {
		t.Fatalf("unexpected created paths: got %v want %v", templateResult.Created, wantCreated)
	}
	if wantUpdated := []string{".claude/settings.json"}; !reflect.DeepEqual(templateResult.Updated, wantUpdated) {
		t.Fatalf("unexpected updated paths: got %v want %v", templateResult.Updated, wantUpdated)
	}

	settingsRaw, err := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(settingsRaw, &parsed); err != nil {
		t.Fatalf("parse settings.json: %v", err)
	}
	if _, ok := parsed["permissions"]; !ok {
		t.Fatalf("expected existing settings to remain, got %v", parsed)
	}
	hooks, ok := parsed["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("expected hooks object, got %T", parsed["hooks"])
	}
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Fatalf("expected PreToolUse hook to be merged, got %v", hooks)
	}
	postHooks, ok := hooks["PostToolUse"].([]any)
	if !ok || len(postHooks) < 2 {
		t.Fatalf("expected merged PostToolUse hooks, got %v", hooks["PostToolUse"])
	}

	again, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
		ApplyTemplates:   []string{"claude-hooks"},
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error on rerun: %+v", apiErr)
	}
	againResult, ok := initTemplateResultByID(again.TemplateResults, "claude-hooks")
	if !ok {
		t.Fatalf("expected claude-hooks result on rerun, got %+v", again.TemplateResults)
	}
	if againResult.Created != nil || againResult.Updated != nil {
		t.Fatalf("expected no created or updated paths on rerun, got %+v", againResult)
	}
	if wantUnchanged := []string{
		".claude/hooks/awm-edit-state.sh",
		".claude/hooks/awm-receipt-guard.sh",
		".claude/hooks/awm-receipt-mark.sh",
		".claude/hooks/awm-session-context.sh",
		".claude/hooks/awm-stop-guard.sh",
		".claude/settings.json",
	}; !reflect.DeepEqual(againResult.Unchanged, wantUnchanged) {
		t.Fatalf("unexpected unchanged paths on rerun: got %v want %v", againResult.Unchanged, wantUnchanged)
	}
}

func TestInit_RemovedClaudeReceiptGuardAliasReturnsInvalidInput(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	respectGitIgnore := false
	svc, err := New(&fakeRepository{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
		ApplyTemplates:   []string{"claude-receipt-guard"},
	})
	if apiErr == nil || apiErr.Code != "INVALID_INPUT" {
		t.Fatalf("expected invalid input for removed legacy template alias, got %+v", apiErr)
	}
}

func TestInit_UnknownTemplateReturnsInvalidInput(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	respectGitIgnore := false
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, apiErr := svc.Init(context.Background(), v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      root,
		RespectGitIgnore: &respectGitIgnore,
		ApplyTemplates:   []string{"missing-template"},
	})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INVALID_INPUT" {
		t.Fatalf("unexpected error code: %s", apiErr.Code)
	}
	if _, err := os.Stat(filepath.Join(root, ".awm")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no init side effects on invalid template, stat err=%v", err)
	}
}

func TestInit_PathCollectionErrorMapsInternalError(t *testing.T) {
	repo := &fakeRepository{}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	respectGitIgnore := false
	_, apiErr := svc.Init(ctx, v1.InitPayload{
		ProjectID:        "project.alpha",
		ProjectRoot:      t.TempDir(),
		RespectGitIgnore: &respectGitIgnore,
	})
	if apiErr == nil {
		t.Fatal("expected API error")
	}
	if apiErr.Code != "INTERNAL_ERROR" {
		t.Fatalf("unexpected error code: %s", apiErr.Code)
	}
	details, ok := apiErr.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected map details, got %T", apiErr.Details)
	}
	if details["operation"] != "collect_project_paths" {
		t.Fatalf("unexpected operation detail: %#v", details)
	}
}
