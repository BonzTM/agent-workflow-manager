package bootstrap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeProjectRoot(t *testing.T) {
	if got, want := NormalizeProjectRoot(""), DefaultProjectRoot; got != want {
		t.Fatalf("unexpected default project root: got %q want %q", got, want)
	}

	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if got, want := NormalizeProjectRoot(root), root; got != want {
		t.Fatalf("unexpected normalized root: got %q want %q", got, want)
	}
}

func TestResolveOutputPath(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("mkdir project root: %v", err)
	}

	if got, ok := ResolveOutputPath(projectRoot, "", false); got != "" || ok {
		t.Fatalf("expected no output path when persistence disabled, got %q %v", got, ok)
	}
	if got, ok := ResolveOutputPath(projectRoot, "", true); !ok || got != filepath.Join(projectRoot, DefaultInitCandidatesPath) {
		t.Fatalf("unexpected default output path: got %q %v", got, ok)
	}
	if got, ok := ResolveOutputPath(projectRoot, "custom/candidates.json", false); !ok || got != filepath.Join(projectRoot, "custom", "candidates.json") {
		t.Fatalf("unexpected explicit relative output path: got %q %v", got, ok)
	}
}

func TestEnsureProjectScaffoldAndWriteCandidates(t *testing.T) {
	projectRoot := t.TempDir()
	if err := EnsureProjectScaffold(projectRoot, ""); err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	requiredFiles := []string{
		filepath.Join(projectRoot, ".env.example"),
		filepath.Join(projectRoot, canonicalRulesPrimarySourcePath),
		filepath.Join(projectRoot, verifyTestsPrimarySourcePath),
		filepath.Join(projectRoot, workflowPrimarySourcePath),
	}
	for _, filePath := range requiredFiles {
		if _, err := os.Stat(filePath); err != nil {
			t.Fatalf("expected scaffold file %s: %v", filePath, err)
		}
	}

	gitignoreRaw, err := os.ReadFile(filepath.Join(projectRoot, ".gitignore"))
	if err != nil {
		t.Fatalf("read scaffolded .gitignore: %v", err)
	}
	if got := string(gitignoreRaw); got != ".awm/context.db\n.awm/context.db-shm\n.awm/context.db-wal\n" {
		t.Fatalf("unexpected scaffolded .gitignore: %q", got)
	}

	outputPath := filepath.Join(projectRoot, "out", "candidates.json")
	if err := WriteCandidates(outputPath, []string{"README.md", "internal/bootstrap/scaffold.go"}); err != nil {
		t.Fatalf("write candidates: %v", err)
	}

	var payload struct {
		Candidates []string `json:"candidates"`
	}
	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read candidates: %v", err)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode candidates: %v", err)
	}
	if len(payload.Candidates) != 2 {
		t.Fatalf("unexpected candidates payload: %+v", payload)
	}
}
