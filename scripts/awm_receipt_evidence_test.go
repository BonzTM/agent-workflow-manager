package scripts

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestReceiptEvidenceScript drives scripts/awm-receipt-evidence.sh end to end
// against a hermetic git repo and SQLite store: a closed receipt covers one
// changed file, a second change stays uncovered, and only --enforce fails.
func TestReceiptEvidenceScript(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}

	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	script := filepath.Join(repoRoot, "scripts", "awm-receipt-evidence.sh")

	awmBin := filepath.Join(t.TempDir(), "awm")
	build := exec.Command("go", "build", "-o", awmBin, "./cmd/awm")
	build.Dir = repoRoot
	if out, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build awm: %v\n%s", buildErr, out)
	}

	projectRoot := t.TempDir()
	gitEnv := append(os.Environ(),
		"GIT_AUTHOR_NAME=evidence-test", "GIT_AUTHOR_EMAIL=evidence@test",
		"GIT_COMMITTER_NAME=evidence-test", "GIT_COMMITTER_EMAIL=evidence@test",
	)
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = projectRoot
		cmd.Env = gitEnv
		if out, gitErr := cmd.CombinedOutput(); gitErr != nil {
			t.Fatalf("git %v: %v\n%s", args, gitErr, out)
		}
	}
	writeFile := func(rel, content string) {
		t.Helper()
		if writeErr := os.WriteFile(filepath.Join(projectRoot, rel), []byte(content), 0o644); writeErr != nil {
			t.Fatalf("write %s: %v", rel, writeErr)
		}
	}

	runGit("init", "-q")
	writeFile("app.txt", "v1\n")
	writeFile("orphan.txt", "v1\n")
	runGit("add", ".")
	runGit("commit", "-q", "-m", "base")

	awmEnv := append(os.Environ(),
		"AWM_PROJECT_ROOT="+projectRoot,
		"AWM_PROJECT_ID=evidence-test",
		"AWM_SQLITE_PATH="+filepath.Join(t.TempDir(), "evidence.db"),
		"AWM_PG_DSN=",
		"AWM_LOG_SINK=discard",
	)
	runAWM := func(args ...string) []byte {
		t.Helper()
		cmd := exec.Command(awmBin, args...)
		cmd.Dir = projectRoot
		cmd.Env = awmEnv
		out, awmErr := cmd.Output()
		if awmErr != nil {
			t.Fatalf("awm %v: %v\n%s", args, awmErr, out)
		}
		return out
	}

	// Open the receipt before editing so the working-tree baseline captured
	// at auto-open time detects app.txt as this receipt's real delta.
	workOut := runAWM("work", "--task-text", "evidence coverage for app.txt",
		"--tasks-json", `[{"key":"impl:evidence","summary":"Change app.txt","status":"complete"}]`)
	var workEnvelope struct {
		Result struct {
			PlanKey string `json:"plan_key"`
		} `json:"result"`
	}
	if unmarshalErr := json.Unmarshal(workOut, &workEnvelope); unmarshalErr != nil {
		t.Fatalf("parse work result: %v\n%s", unmarshalErr, workOut)
	}
	planKey := strings.TrimSpace(workEnvelope.Result.PlanKey)
	if !strings.HasPrefix(planKey, "plan:receipt-") {
		t.Fatalf("expected auto-opened plan key, got %q", planKey)
	}

	writeFile("app.txt", "v2\n")

	runAWM("done", "--plan-key", planKey, "--file-changed", "app.txt",
		"--outcome", "evidence recorded for app.txt")

	// orphan.txt changes after the receipt closed, so no evidence covers it.
	writeFile("orphan.txt", "v2\n")

	runGit("add", ".")
	runGit("commit", "-q", "-m", "head")

	runScript := func(extra ...string) (string, error) {
		t.Helper()
		args := append([]string{script, "--base", "HEAD~1", "--head", "HEAD", "--awm-bin", awmBin}, extra...)
		cmd := exec.Command("bash", args...)
		cmd.Dir = projectRoot
		cmd.Env = awmEnv
		out, scriptErr := cmd.CombinedOutput()
		return string(out), scriptErr
	}

	advisory, advisoryErr := runScript()
	if advisoryErr != nil {
		t.Fatalf("advisory run must always exit 0, got %v\n%s", advisoryErr, advisory)
	}
	if !strings.Contains(advisory, "covered    app.txt") {
		t.Fatalf("expected app.txt to be covered:\n%s", advisory)
	}
	if !strings.Contains(advisory, "UNCOVERED  orphan.txt") {
		t.Fatalf("expected orphan.txt to be uncovered:\n%s", advisory)
	}
	if !strings.Contains(advisory, "1/2 changed files covered") {
		t.Fatalf("expected coverage summary:\n%s", advisory)
	}

	enforced, enforcedErr := runScript("--enforce")
	if enforcedErr == nil {
		t.Fatalf("expected --enforce to fail with uncovered files:\n%s", enforced)
	}
}
