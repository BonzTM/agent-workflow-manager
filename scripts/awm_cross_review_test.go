package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAWMCrossReviewAcceptsWorkflowModelArgs(t *testing.T) {
	t.Parallel()

	args := runCrossReviewScript(t, []string{"--model", "gpt-5.4", "--reasoning-effort", "high", "--sandbox", "workspace-write"})
	assertArgSequence(t, args, "--model", "gpt-5.4")
	assertArgSequence(t, args, "-c", `model_reasoning_effort="high"`)
	assertArgSequence(t, args, "--sandbox", "workspace-write")
}

func TestAWMCrossReviewAcceptsCodexYolo(t *testing.T) {
	t.Parallel()

	args := runCrossReviewScript(t, []string{"--provider", "codex", "--yolo"})
	assertArgSequence(t, args, "--yolo")
	for _, arg := range args {
		if arg == "--sandbox" {
			t.Fatalf("did not expect --sandbox when --yolo is enabled: %q", args)
		}
	}
}

func TestAWMCrossReviewUsesDefaultModelArgs(t *testing.T) {
	t.Parallel()

	args := runCrossReviewScript(t, nil)
	assertArgSequence(t, args, "--model", "gpt-5.3-codex")
	assertArgSequence(t, args, "-c", `model_reasoning_effort="xhigh"`)
	assertArgSequence(t, args, "--sandbox", "read-only")
}

func TestAWMCrossReviewRunsClaudePrintMode(t *testing.T) {
	t.Parallel()

	args, prompt := runCrossReviewScriptWithPrompt(t, []string{"--provider", "claude", "--model", "sonnet"})
	assertArgSequence(t, args, "-p")
	assertArgSequence(t, args, "--model", "sonnet")
	assertArgSequence(t, args, "--output-format", "json")
	assertArgSequence(t, args, "--json-schema")
	if !strings.Contains(prompt, "Review the current task-scoped uncommitted changes") {
		t.Fatalf("expected review prompt to be piped to Claude, got %q", prompt)
	}
}

func TestAWMCrossReviewRunsClaudeWithDangerouslySkipPermissions(t *testing.T) {
	t.Parallel()

	args := runCrossReviewScript(t, []string{"--provider", "claude", "--dangerously-skip-permissions"})
	assertArgSequence(t, args, "-p")
	assertArgSequence(t, args, "--dangerously-skip-permissions")
}

func TestAWMCrossReviewMapsYoloToClaudeDangerousPermissions(t *testing.T) {
	t.Parallel()

	args := runCrossReviewScript(t, []string{"--provider", "claude", "--yolo"})
	assertArgSequence(t, args, "-p")
	assertArgSequence(t, args, "--dangerously-skip-permissions")
}

func TestAWMCrossReviewRejectsClaudeSandboxFlag(t *testing.T) {
	t.Parallel()

	output := runCrossReviewScriptExpectFailure(t, []string{"--provider", "claude", "--sandbox", "read-only"})
	if !strings.Contains(output, "--sandbox is only supported with --provider codex") {
		t.Fatalf("expected provider-specific sandbox failure, got %q", output)
	}
}

func TestAWMCrossReviewRejectsCodexDangerousPermissionsFlag(t *testing.T) {
	t.Parallel()

	output := runCrossReviewScriptExpectFailure(t, []string{"--provider", "codex", "--dangerously-skip-permissions"})
	if !strings.Contains(output, "--dangerously-skip-permissions is only supported with --provider claude") {
		t.Fatalf("expected provider-specific dangerous-permissions failure, got %q", output)
	}
}

func TestAWMCrossReviewAllowsCodexWhenDangerousPermissionsEnvIsFalse(t *testing.T) {
	t.Parallel()

	args := runCrossReviewScriptWithExtraEnv(t, []string{"--provider", "codex"}, map[string]string{
		"AWM_CROSS_REVIEW_DANGEROUSLY_SKIP_PERMISSIONS": "false",
	})
	assertArgSequence(t, args, "--model", "gpt-5.3-codex")
}

func TestAWMCrossReviewIncludesManagedAndDeletedScopedFiles(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	binDir := filepath.Join(tempRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	capturePath := filepath.Join(tempRoot, "codex-args.txt")
	promptPath := filepath.Join(tempRoot, "codex-prompt.txt")
	writeExecutable(t, filepath.Join(binDir, "codex"), `#!/usr/bin/env bash
set -euo pipefail
capture_path="${AWM_TEST_CAPTURE:?}"
prompt_path="${AWM_TEST_PROMPT:?}"
printf '%s\n' "$@" >"${capture_path}"
cat >"${prompt_path}"
output_path=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-last-message)
      output_path="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
printf '{"status":"pass","summary":"ok","findings":[]}\n' >"${output_path}"
`)
	writeExecutable(t, filepath.Join(binDir, "awm"), `#!/usr/bin/env bash
set -euo pipefail
cat <<'JSON'
{"result":{"items":[{"content":"{\"initial_scope_paths\":[\"docs/deleted.md\"]}"}]}}
JSON
`)

	projectRoot := filepath.Join(tempRoot, "project-root")
	if err := os.MkdirAll(filepath.Join(projectRoot, ".awm"), 0o755); err != nil {
		t.Fatalf("mkdir .awm: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, ".awm", "awm-tests.yaml"), []byte("version: awm.tests.v1\n"), 0o644); err != nil {
		t.Fatalf("write tests file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "docs", "deleted.md"), []byte("gone\n"), 0o644); err != nil {
		t.Fatalf("write deleted file: %v", err)
	}

	runGit(t, projectRoot, "init")
	runGit(t, projectRoot, "config", "user.email", "test@example.com")
	runGit(t, projectRoot, "config", "user.name", "Test User")
	runGit(t, projectRoot, "add", ".")
	runGit(t, projectRoot, "commit", "-m", "initial state")

	if err := os.Remove(filepath.Join(projectRoot, "docs", "deleted.md")); err != nil {
		t.Fatalf("remove deleted file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, ".awm", "awm-tests.yaml"), []byte("version: awm.tests.v1\nsmoke: []\n"), 0o644); err != nil {
		t.Fatalf("rewrite tests file: %v", err)
	}

	cmd := exec.Command("bash", "scripts/awm-cross-review.sh")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AWM_PROJECT_ID=agent-workflow-manager",
		"AWM_RECEIPT_ID=receipt-test",
		"AWM_PROJECT_ROOT="+projectRoot,
		"AWM_TEST_CAPTURE="+capturePath,
		"AWM_TEST_PROMPT="+promptPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cross-review script: %v\n%s", err, string(output))
	}
	if !strings.Contains(string(output), "PASS: ok") {
		t.Fatalf("unexpected script output: %s", string(output))
	}

	prompt, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("read captured prompt: %v", err)
	}
	promptText := string(prompt)
	if !strings.Contains(promptText, ".awm/awm-tests.yaml") {
		t.Fatalf("expected managed completion file in prompt, got %s", promptText)
	}
	if !strings.Contains(promptText, "docs/deleted.md") {
		t.Fatalf("expected deleted scoped file in prompt, got %s", promptText)
	}
	if !strings.Contains(promptText, "must not modify files by any means") || !strings.Contains(promptText, "do not use any command, tool, or redirection that writes to the filesystem") {
		t.Fatalf("expected explicit no-write review instructions in prompt, got %s", promptText)
	}
	if !strings.Contains(promptText, "- changed_detected: 2") || !strings.Contains(promptText, "- scoped_changed: 2") {
		t.Fatalf("expected scope counts in prompt, got %s", promptText)
	}
	if !strings.Contains(string(output), "scoped 2/2 changed files") {
		t.Fatalf("expected scope counts in output, got %s", string(output))
	}
}

func TestAWMCrossReviewFailsWhenRepoChangesExistButScopedSetIsEmpty(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	binDir := filepath.Join(tempRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	capturePath := filepath.Join(tempRoot, "codex-args.txt")
	writeExecutable(t, filepath.Join(binDir, "codex"), `#!/usr/bin/env bash
set -euo pipefail
capture_path="${AWM_TEST_CAPTURE:?}"
printf '%s\n' "$@" >"${capture_path}"
exit 0
`)
	writeExecutable(t, filepath.Join(binDir, "awm"), `#!/usr/bin/env bash
set -euo pipefail
cat <<'JSON'
{"result":{"items":[{"content":"{\"initial_scope_paths\":[\"docs/in-scope.md\"]}"}]}}
JSON
`)

	projectRoot := filepath.Join(tempRoot, "project-root")
	if err := os.MkdirAll(filepath.Join(projectRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "docs", "outside-scope.md"), []byte("draft\n"), 0o644); err != nil {
		t.Fatalf("write changed file: %v", err)
	}

	runGit(t, projectRoot, "init")
	runGit(t, projectRoot, "config", "user.email", "test@example.com")
	runGit(t, projectRoot, "config", "user.name", "Test User")
	runGit(t, projectRoot, "add", ".")
	runGit(t, projectRoot, "commit", "-m", "initial state")

	if err := os.WriteFile(filepath.Join(projectRoot, "docs", "outside-scope.md"), []byte("updated draft\n"), 0o644); err != nil {
		t.Fatalf("rewrite changed file: %v", err)
	}

	cmd := exec.Command("bash", "scripts/awm-cross-review.sh")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AWM_PROJECT_ID=agent-workflow-manager",
		"AWM_RECEIPT_ID=receipt-test",
		"AWM_PROJECT_ROOT="+projectRoot,
		"AWM_TEST_CAPTURE="+capturePath,
	)

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected cross-review script to fail when scoped set is empty, output=%s", string(output))
	}
	if !strings.Contains(string(output), "Review gate blocked before model execution") {
		t.Fatalf("expected empty-scope failure message, got %s", string(output))
	}
	if !strings.Contains(string(output), "1 changed file(s), 0 scoped change(s)") {
		t.Fatalf("expected changed/scoped counts in failure output, got %s", string(output))
	}
	if !strings.Contains(string(output), "declare missing files through awm work") {
		t.Fatalf("expected updated remediation hint, got %s", string(output))
	}
	if _, statErr := os.Stat(capturePath); !os.IsNotExist(statErr) {
		t.Fatalf("expected codex not to run on empty scoped review, stat err=%v", statErr)
	}
}

func TestAWMCrossReviewIncludesPlanDiscoveredPathsInEffectiveScope(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	binDir := filepath.Join(tempRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	capturePath := filepath.Join(tempRoot, "codex-args.txt")
	promptPath := filepath.Join(tempRoot, "codex-prompt.txt")
	writeExecutable(t, filepath.Join(binDir, "codex"), `#!/usr/bin/env bash
set -euo pipefail
capture_path="${AWM_TEST_CAPTURE:?}"
prompt_path="${AWM_TEST_PROMPT:?}"
printf '%s\n' "$@" >"${capture_path}"
cat >"${prompt_path}"
output_path=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-last-message)
      output_path="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
printf '{"status":"pass","summary":"ok","findings":[]}\n' >"${output_path}"
`)
	writeExecutable(t, filepath.Join(binDir, "awm"), `#!/usr/bin/env bash
set -euo pipefail
key=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --key)
      key="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
case "${key}" in
  receipt:*)
    cat <<'JSON'
{"result":{"items":[{"content":"{\"initial_scope_paths\":[]}"}]}}
JSON
    ;;
  plan:*)
    cat <<'JSON'
{"result":{"items":[{"content":"{\"discovered_paths\":[\"docs/discovered.md\"]}"}]}}
JSON
    ;;
  *)
    cat <<'JSON'
{"result":{"items":[]}}
JSON
    ;;
esac
`)

	projectRoot := filepath.Join(tempRoot, "project-root")
	if err := os.MkdirAll(filepath.Join(projectRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "docs", "discovered.md"), []byte("draft\n"), 0o644); err != nil {
		t.Fatalf("write discovered file: %v", err)
	}

	runGit(t, projectRoot, "init")
	runGit(t, projectRoot, "config", "user.email", "test@example.com")
	runGit(t, projectRoot, "config", "user.name", "Test User")
	runGit(t, projectRoot, "add", ".")
	runGit(t, projectRoot, "commit", "-m", "initial state")

	if err := os.WriteFile(filepath.Join(projectRoot, "docs", "discovered.md"), []byte("updated draft\n"), 0o644); err != nil {
		t.Fatalf("rewrite discovered file: %v", err)
	}

	cmd := exec.Command("bash", "scripts/awm-cross-review.sh")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AWM_PROJECT_ID=agent-workflow-manager",
		"AWM_RECEIPT_ID=receipt-test",
		"AWM_PLAN_KEY=plan:receipt-test",
		"AWM_PROJECT_ROOT="+projectRoot,
		"AWM_TEST_CAPTURE="+capturePath,
		"AWM_TEST_PROMPT="+promptPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cross-review script: %v\n%s", err, string(output))
	}
	if !strings.Contains(string(output), "PASS: ok") {
		t.Fatalf("unexpected script output: %s", string(output))
	}
	if !strings.Contains(string(output), "scoped 1/1 changed files") {
		t.Fatalf("expected scope counts in output, got %s", string(output))
	}

	prompt, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("read captured prompt: %v", err)
	}
	promptText := string(prompt)
	if !strings.Contains(promptText, "docs/discovered.md") {
		t.Fatalf("expected discovered scoped file in prompt, got %s", promptText)
	}
	if !strings.Contains(promptText, "initial_scope_paths") || !strings.Contains(promptText, "plan.discovered_paths") {
		t.Fatalf("expected effective-scope instructions in prompt, got %s", promptText)
	}
}

func TestAWMCrossReviewUsesInjectedTaskDeltaInsteadOfRepoDirtyState(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	binDir := filepath.Join(tempRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	capturePath := filepath.Join(tempRoot, "codex-args.txt")
	promptPath := filepath.Join(tempRoot, "codex-prompt.txt")
	writeExecutable(t, filepath.Join(binDir, "codex"), `#!/usr/bin/env bash
set -euo pipefail
capture_path="${AWM_TEST_CAPTURE:?}"
prompt_path="${AWM_TEST_PROMPT:?}"
printf '%s\n' "$@" >"${capture_path}"
cat >"${prompt_path}"
output_path=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-last-message)
      output_path="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
printf '{"status":"pass","summary":"ok","findings":[]}\n' >"${output_path}"
`)
	writeExecutable(t, filepath.Join(binDir, "awm"), `#!/usr/bin/env bash
echo "awm fetch fallback should not run when review env is injected" >&2
exit 97
`)

	projectRoot := filepath.Join(tempRoot, "project-root")
	if err := os.MkdirAll(filepath.Join(projectRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "docs", "in-scope.md"), []byte("draft\n"), 0o644); err != nil {
		t.Fatalf("write in-scope file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "docs", "unrelated.md"), []byte("draft\n"), 0o644); err != nil {
		t.Fatalf("write unrelated file: %v", err)
	}

	runGit(t, projectRoot, "init")
	runGit(t, projectRoot, "config", "user.email", "test@example.com")
	runGit(t, projectRoot, "config", "user.name", "Test User")
	runGit(t, projectRoot, "add", ".")
	runGit(t, projectRoot, "commit", "-m", "initial state")

	if err := os.WriteFile(filepath.Join(projectRoot, "docs", "in-scope.md"), []byte("updated draft\n"), 0o644); err != nil {
		t.Fatalf("rewrite in-scope file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "docs", "unrelated.md"), []byte("updated unrelated\n"), 0o644); err != nil {
		t.Fatalf("rewrite unrelated file: %v", err)
	}

	cmd := exec.Command("bash", "scripts/awm-cross-review.sh")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AWM_PROJECT_ID=agent-workflow-manager",
		"AWM_RECEIPT_ID=receipt-test",
		"AWM_PLAN_KEY=plan:receipt-test",
		"AWM_PROJECT_ROOT="+projectRoot,
		"AWM_REVIEW_CHANGED_PATHS_JSON=[\"docs/in-scope.md\"]",
		"AWM_REVIEW_EFFECTIVE_SCOPE_PATHS_JSON=[\"docs/in-scope.md\"]",
		"AWM_TEST_CAPTURE="+capturePath,
		"AWM_TEST_PROMPT="+promptPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cross-review script with injected task delta: %v\n%s", err, string(output))
	}
	if !strings.Contains(string(output), "PASS: ok") || !strings.Contains(string(output), "scoped 1/1 changed files") {
		t.Fatalf("unexpected script output: %s", string(output))
	}

	prompt, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("read captured prompt: %v", err)
	}
	promptText := string(prompt)
	if !strings.Contains(promptText, "docs/in-scope.md") {
		t.Fatalf("expected injected scoped file in prompt, got %s", promptText)
	}
	if strings.Contains(promptText, "docs/unrelated.md") {
		t.Fatalf("unexpected unrelated dirty file in prompt: %s", promptText)
	}
	if !strings.Contains(promptText, "- changed_detected: 1") || !strings.Contains(promptText, "- scoped_changed: 1") {
		t.Fatalf("expected injected scope counts in prompt, got %s", promptText)
	}
}

func TestAWMCrossReviewTreatsDirectoryScopeAsRecursive(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	binDir := filepath.Join(tempRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	capturePath := filepath.Join(tempRoot, "codex-args.txt")
	promptPath := filepath.Join(tempRoot, "codex-prompt.txt")
	writeExecutable(t, filepath.Join(binDir, "codex"), `#!/usr/bin/env bash
set -euo pipefail
capture_path="${AWM_TEST_CAPTURE:?}"
prompt_path="${AWM_TEST_PROMPT:?}"
printf '%s\n' "$@" >"${capture_path}"
cat >"${prompt_path}"
output_path=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-last-message)
      output_path="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
printf '{"status":"pass","summary":"ok","findings":[]}\n' >"${output_path}"
`)
	writeExecutable(t, filepath.Join(binDir, "awm"), `#!/usr/bin/env bash
echo "awm fetch fallback should not run when review env is injected" >&2
exit 97
`)

	projectRoot := filepath.Join(tempRoot, "project-root")
	if err := os.MkdirAll(filepath.Join(projectRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "docs", "in-scope.md"), []byte("draft\n"), 0o644); err != nil {
		t.Fatalf("write scoped file: %v", err)
	}

	runGit(t, projectRoot, "init")
	runGit(t, projectRoot, "config", "user.email", "test@example.com")
	runGit(t, projectRoot, "config", "user.name", "Test User")
	runGit(t, projectRoot, "add", ".")
	runGit(t, projectRoot, "commit", "-m", "initial state")

	if err := os.WriteFile(filepath.Join(projectRoot, "docs", "in-scope.md"), []byte("updated draft\n"), 0o644); err != nil {
		t.Fatalf("rewrite scoped file: %v", err)
	}

	cmd := exec.Command("bash", "scripts/awm-cross-review.sh")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AWM_PROJECT_ID=agent-workflow-manager",
		"AWM_RECEIPT_ID=receipt-test",
		"AWM_PLAN_KEY=plan:receipt-test",
		"AWM_PROJECT_ROOT="+projectRoot,
		"AWM_REVIEW_CHANGED_PATHS_JSON=[\"docs/in-scope.md\"]",
		"AWM_REVIEW_EFFECTIVE_SCOPE_PATHS_JSON=[\"docs\"]",
		"AWM_TEST_CAPTURE="+capturePath,
		"AWM_TEST_PROMPT="+promptPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cross-review script with directory scope: %v\n%s", err, string(output))
	}
	if !strings.Contains(string(output), "PASS: ok") || !strings.Contains(string(output), "scoped 1/1 changed files") {
		t.Fatalf("unexpected script output: %s", string(output))
	}

	prompt, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("read captured prompt: %v", err)
	}
	if !strings.Contains(string(prompt), "docs/in-scope.md") {
		t.Fatalf("expected directory-scoped file in prompt, got %s", string(prompt))
	}
}

func TestAWMCrossReviewBlocksInjectedUnscopedTaskDelta(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	binDir := filepath.Join(tempRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	capturePath := filepath.Join(tempRoot, "codex-args.txt")
	writeExecutable(t, filepath.Join(binDir, "codex"), `#!/usr/bin/env bash
set -euo pipefail
capture_path="${AWM_TEST_CAPTURE:?}"
printf '%s\n' "$@" >"${capture_path}"
exit 0
`)
	writeExecutable(t, filepath.Join(binDir, "awm"), `#!/usr/bin/env bash
echo "awm fetch fallback should not run when review env is injected" >&2
exit 97
`)

	projectRoot := filepath.Join(tempRoot, "project-root")
	if err := os.MkdirAll(filepath.Join(projectRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "docs", "in-scope.md"), []byte("draft\n"), 0o644); err != nil {
		t.Fatalf("write in-scope file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "docs", "outside.md"), []byte("draft\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	runGit(t, projectRoot, "init")
	runGit(t, projectRoot, "config", "user.email", "test@example.com")
	runGit(t, projectRoot, "config", "user.name", "Test User")
	runGit(t, projectRoot, "add", ".")
	runGit(t, projectRoot, "commit", "-m", "initial state")

	if err := os.WriteFile(filepath.Join(projectRoot, "docs", "in-scope.md"), []byte("updated draft\n"), 0o644); err != nil {
		t.Fatalf("rewrite in-scope file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "docs", "outside.md"), []byte("updated outside\n"), 0o644); err != nil {
		t.Fatalf("rewrite outside file: %v", err)
	}

	cmd := exec.Command("bash", "scripts/awm-cross-review.sh")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AWM_PROJECT_ID=agent-workflow-manager",
		"AWM_RECEIPT_ID=receipt-test",
		"AWM_PLAN_KEY=plan:receipt-test",
		"AWM_PROJECT_ROOT="+projectRoot,
		"AWM_REVIEW_CHANGED_PATHS_JSON=[\"docs/in-scope.md\",\"docs/outside.md\"]",
		"AWM_REVIEW_EFFECTIVE_SCOPE_PATHS_JSON=[\"docs/in-scope.md\"]",
		"AWM_TEST_CAPTURE="+capturePath,
	)

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected cross-review script to fail on injected unscoped delta, output=%s", string(output))
	}
	if !strings.Contains(string(output), "2 changed file(s), 1 scoped change(s), 1 unscoped change(s)") {
		t.Fatalf("expected injected unscoped counts in failure output, got %s", string(output))
	}
	if _, statErr := os.Stat(capturePath); !os.IsNotExist(statErr) {
		t.Fatalf("expected codex not to run on injected unscoped delta, stat err=%v", statErr)
	}
}

func TestAWMCrossReviewAllowsMissingPlanFetch(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	binDir := filepath.Join(tempRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	capturePath := filepath.Join(tempRoot, "codex-args.txt")
	promptPath := filepath.Join(tempRoot, "codex-prompt.txt")
	writeExecutable(t, filepath.Join(binDir, "codex"), `#!/usr/bin/env bash
set -euo pipefail
capture_path="${AWM_TEST_CAPTURE:?}"
prompt_path="${AWM_TEST_PROMPT:?}"
printf '%s\n' "$@" >"${capture_path}"
cat >"${prompt_path}"
output_path=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-last-message)
      output_path="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
printf '{"status":"pass","summary":"ok","findings":[]}\n' >"${output_path}"
`)
	writeExecutable(t, filepath.Join(binDir, "awm"), `#!/usr/bin/env bash
set -euo pipefail
key=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --key)
      key="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
if [[ "${key}" == receipt:* ]]; then
  cat <<'JSON'
{"result":{"items":[{"content":"{\"initial_scope_paths\":[\"docs/in-scope.md\"]}"}]}}
JSON
  exit 0
fi
exit 1
`)

	projectRoot := filepath.Join(tempRoot, "project-root")
	if err := os.MkdirAll(filepath.Join(projectRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "docs", "in-scope.md"), []byte("draft\n"), 0o644); err != nil {
		t.Fatalf("write in-scope file: %v", err)
	}

	runGit(t, projectRoot, "init")
	runGit(t, projectRoot, "config", "user.email", "test@example.com")
	runGit(t, projectRoot, "config", "user.name", "Test User")
	runGit(t, projectRoot, "add", ".")
	runGit(t, projectRoot, "commit", "-m", "initial state")

	if err := os.WriteFile(filepath.Join(projectRoot, "docs", "in-scope.md"), []byte("updated draft\n"), 0o644); err != nil {
		t.Fatalf("rewrite in-scope file: %v", err)
	}

	cmd := exec.Command("bash", "scripts/awm-cross-review.sh")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AWM_PROJECT_ID=agent-workflow-manager",
		"AWM_RECEIPT_ID=receipt-test",
		"AWM_PROJECT_ROOT="+projectRoot,
		"AWM_TEST_CAPTURE="+capturePath,
		"AWM_TEST_PROMPT="+promptPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cross-review script with missing plan fetch: %v\n%s", err, string(output))
	}
	if !strings.Contains(string(output), "PASS: ok") {
		t.Fatalf("unexpected script output: %s", string(output))
	}
}

func TestAWMCrossReviewUsesStructuredOutputWhenCodexExitsNonZero(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	binDir := filepath.Join(tempRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	capturePath := filepath.Join(tempRoot, "codex-args.txt")
	writeExecutable(t, filepath.Join(binDir, "codex"), `#!/usr/bin/env bash
set -euo pipefail
capture_path="${AWM_TEST_CAPTURE:?}"
printf '%s\n' "$@" >"${capture_path}"
output_path=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-last-message)
      output_path="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
cat >/dev/null
printf '{"status":"pass","summary":"ok","findings":[]}\n' >"${output_path}"
exit 2
`)
	writeExecutable(t, filepath.Join(binDir, "awm"), `#!/usr/bin/env bash
exit 0
`)

	projectRoot := filepath.Join(tempRoot, "project-root")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("mkdir project root: %v", err)
	}

	cmd := exec.Command("bash", "scripts/awm-cross-review.sh")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AWM_PROJECT_ID=agent-workflow-manager",
		"AWM_RECEIPT_ID=receipt-test",
		"AWM_PROJECT_ROOT="+projectRoot,
		"AWM_TEST_CAPTURE="+capturePath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected structured-output path to succeed despite codex exit code, err=%v\n%s", err, string(output))
	}
	if !strings.Contains(string(output), "PASS: ok") {
		t.Fatalf("unexpected script output: %s", string(output))
	}
}

func TestAWMCrossReviewFallsBackToCodexStdoutJSON(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	binDir := filepath.Join(tempRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	capturePath := filepath.Join(tempRoot, "codex-args.txt")
	writeExecutable(t, filepath.Join(binDir, "codex"), `#!/usr/bin/env bash
set -euo pipefail
capture_path="${AWM_TEST_CAPTURE:?}"
printf '%s\n' "$@" >"${capture_path}"
cat >/dev/null
printf '{"status":"pass","summary":"ok","findings":[]}\n'
exit 2
`)
	writeExecutable(t, filepath.Join(binDir, "awm"), `#!/usr/bin/env bash
exit 0
`)

	projectRoot := filepath.Join(tempRoot, "project-root")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("mkdir project root: %v", err)
	}

	cmd := exec.Command("bash", "scripts/awm-cross-review.sh")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AWM_PROJECT_ID=agent-workflow-manager",
		"AWM_RECEIPT_ID=receipt-test",
		"AWM_PROJECT_ROOT="+projectRoot,
		"AWM_TEST_CAPTURE="+capturePath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected stdout JSON fallback to succeed despite codex exit code, err=%v\n%s", err, string(output))
	}
	if !strings.Contains(string(output), "PASS: ok") {
		t.Fatalf("unexpected script output: %s", string(output))
	}
}

func TestAWMCrossReviewFallsBackToCodexStderrJSON(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	binDir := filepath.Join(tempRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	capturePath := filepath.Join(tempRoot, "codex-args.txt")
	writeExecutable(t, filepath.Join(binDir, "codex"), `#!/usr/bin/env bash
set -euo pipefail
capture_path="${AWM_TEST_CAPTURE:?}"
printf '%s\n' "$@" >"${capture_path}"
cat >/dev/null
printf 'codex\n' >&2
printf '{"status":"pass","summary":"ok","findings":[]}\n' >&2
exit 2
`)
	writeExecutable(t, filepath.Join(binDir, "awm"), `#!/usr/bin/env bash
exit 0
`)

	projectRoot := filepath.Join(tempRoot, "project-root")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("mkdir project root: %v", err)
	}

	cmd := exec.Command("bash", "scripts/awm-cross-review.sh")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AWM_PROJECT_ID=agent-workflow-manager",
		"AWM_RECEIPT_ID=receipt-test",
		"AWM_PROJECT_ROOT="+projectRoot,
		"AWM_TEST_CAPTURE="+capturePath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected stderr JSON fallback to succeed despite codex exit code, err=%v\n%s", err, string(output))
	}
	if !strings.Contains(string(output), "PASS: ok") {
		t.Fatalf("unexpected script output: %s", string(output))
	}
}

func TestAWMCrossReviewReportsMissingStructuredOutput(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	binDir := filepath.Join(tempRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	writeExecutable(t, filepath.Join(binDir, "codex"), `#!/usr/bin/env bash
set -euo pipefail
cat >/dev/null
echo "transient backend failure" >&2
exit 1
`)
	writeExecutable(t, filepath.Join(binDir, "awm"), `#!/usr/bin/env bash
exit 0
`)

	projectRoot := filepath.Join(tempRoot, "project-root")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("mkdir project root: %v", err)
	}

	cmd := exec.Command("bash", "scripts/awm-cross-review.sh")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AWM_PROJECT_ID=agent-workflow-manager",
		"AWM_RECEIPT_ID=receipt-test",
		"AWM_PROJECT_ROOT="+projectRoot,
	)

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected script to fail without structured output, output=%s", string(output))
	}
	if !strings.Contains(string(output), "FAIL: codex review did not produce structured output (exit 1).") {
		t.Fatalf("expected missing-output failure message, got %s", string(output))
	}
	if !strings.Contains(string(output), "transient backend failure") {
		t.Fatalf("expected codex stderr to be surfaced, got %s", string(output))
	}
}

func runCrossReviewScript(t *testing.T, reviewArgs []string) []string {
	t.Helper()

	args, _ := runCrossReviewScriptWithPromptAndEnv(t, reviewArgs, nil)
	return args
}

func runCrossReviewScriptWithExtraEnv(t *testing.T, reviewArgs []string, extraEnv map[string]string) []string {
	t.Helper()

	args, _ := runCrossReviewScriptWithPromptAndEnv(t, reviewArgs, extraEnv)
	return args
}

func runCrossReviewScriptExpectFailure(t *testing.T, reviewArgs []string) string {
	t.Helper()

	tempRoot := t.TempDir()
	binDir := filepath.Join(tempRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	writeExecutable(t, filepath.Join(binDir, "codex"), "#!/usr/bin/env bash\nexit 0\n")
	writeExecutable(t, filepath.Join(binDir, "claude"), "#!/usr/bin/env bash\nexit 0\n")
	writeExecutable(t, filepath.Join(binDir, "awm"), "#!/usr/bin/env bash\nexit 0\n")

	projectRoot := filepath.Join(tempRoot, "project-root")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("mkdir project root: %v", err)
	}

	cmdArgs := append([]string{"scripts/awm-cross-review.sh"}, reviewArgs...)
	cmd := exec.Command("bash", cmdArgs...)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AWM_PROJECT_ID=agent-workflow-manager",
		"AWM_RECEIPT_ID=receipt-test",
		"AWM_PROJECT_ROOT="+projectRoot,
	)

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected cross-review script to fail, output=%s", string(output))
	}
	return string(output)
}

func runCrossReviewScriptWithPrompt(t *testing.T, reviewArgs []string) ([]string, string) {
	t.Helper()

	return runCrossReviewScriptWithPromptAndEnv(t, reviewArgs, nil)
}

func runCrossReviewScriptWithPromptAndEnv(t *testing.T, reviewArgs []string, extraEnv map[string]string) ([]string, string) {
	t.Helper()

	tempRoot := t.TempDir()
	binDir := filepath.Join(tempRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	capturePath := filepath.Join(tempRoot, "reviewer-args.txt")
	promptPath := filepath.Join(tempRoot, "reviewer-prompt.txt")
	writeExecutable(t, filepath.Join(binDir, "codex"), `#!/usr/bin/env bash
set -euo pipefail
capture_path="${AWM_TEST_CAPTURE:?}"
prompt_path="${AWM_TEST_PROMPT:?}"
printf '%s\n' "$@" >"${capture_path}"
cat >"${prompt_path}"
output_path=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-last-message)
      output_path="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
printf '{"status":"pass","summary":"ok","findings":[]}\n' >"${output_path}"
`)
	writeExecutable(t, filepath.Join(binDir, "claude"), `#!/usr/bin/env bash
set -euo pipefail
capture_path="${AWM_TEST_CAPTURE:?}"
prompt_path="${AWM_TEST_PROMPT:?}"
printf '%s\n' "$@" >"${capture_path}"
cat >"${prompt_path}"
printf '{"status":"pass","summary":"ok","findings":[]}\n'
`)
	writeExecutable(t, filepath.Join(binDir, "awm"), `#!/usr/bin/env bash
exit 0
`)

	projectRoot := filepath.Join(tempRoot, "project-root")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("mkdir project root: %v", err)
	}

	cmdArgs := append([]string{"scripts/awm-cross-review.sh"}, reviewArgs...)
	cmd := exec.Command("bash", cmdArgs...)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AWM_PROJECT_ID=agent-workflow-manager",
		"AWM_RECEIPT_ID=receipt-test",
		"AWM_PROJECT_ROOT="+projectRoot,
		"AWM_TEST_CAPTURE="+capturePath,
		"AWM_TEST_PROMPT="+promptPath,
	)
	for key, value := range extraEnv {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cross-review script: %v\n%s", err, string(output))
	}
	if !strings.Contains(string(output), "PASS: ok") {
		t.Fatalf("unexpected script output: %s", string(output))
	}

	raw, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read captured codex args: %v", err)
	}
	promptRaw, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("read captured prompt: %v", err)
	}
	return splitNonEmptyLines(string(raw)), string(promptRaw)
}

func repoRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

func splitNonEmptyLines(raw string) []string {
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func assertArgSequence(t *testing.T, args []string, want ...string) {
	t.Helper()

	for i := 0; i+len(want) <= len(args); i++ {
		match := true
		for j := range want {
			if args[i+j] != want[j] {
				match = false
				break
			}
		}
		if match {
			return
		}
	}
	t.Fatalf("argument sequence %q not found in %q", want, args)
}
