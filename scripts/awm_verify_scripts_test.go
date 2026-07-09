package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAWMGoTestTargetsRunsChangedPackages(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	binDir := filepath.Join(tempRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	capturePath := filepath.Join(tempRoot, "go-args.txt")
	writeExecutable(t, filepath.Join(binDir, "go"), `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" >"${AWM_TEST_CAPTURE:?}"
`)

	cmd := exec.Command("python3", "scripts/awm-go-test-targets.py")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(baseScriptEnv(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AWM_TEST_CAPTURE="+capturePath,
		`AWM_VERIFY_FILES_CHANGED_JSON=["internal/service/backend/verify.go","cmd/awm/main_test.go","scripts/awm-tdd-guard.py"]`,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run go target script: %v\n%s", err, string(output))
	}

	argsRaw, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read captured go args: %v", err)
	}
	args := splitNonEmptyLines(string(argsRaw))
	if got, want := strings.Join(args, " "), "test -count=1 ./cmd/awm ./internal/service/backend"; got != want {
		t.Fatalf("unexpected go args: got %q want %q", got, want)
	}
}

func TestAWMGoTestTargetsSkipsWithoutGoPackages(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	binDir := filepath.Join(tempRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	capturePath := filepath.Join(tempRoot, "go-args.txt")
	writeExecutable(t, filepath.Join(binDir, "go"), `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" >"${AWM_TEST_CAPTURE:?}"
`)

	cmd := exec.Command("python3", "scripts/awm-go-test-targets.py")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(baseScriptEnv(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AWM_TEST_CAPTURE="+capturePath,
		`AWM_VERIFY_FILES_CHANGED_JSON=["README.md","docs/getting-started.md"]`,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run go target script: %v\n%s", err, string(output))
	}
	if !strings.Contains(string(output), "skip - no changed Go packages matched") {
		t.Fatalf("unexpected skip output: %s", string(output))
	}
	if _, statErr := os.Stat(capturePath); !os.IsNotExist(statErr) {
		t.Fatalf("expected go not to run, stat err=%v", statErr)
	}
}

func TestAWMTDDGuardSkipsWhenNoBehaviorGoPaths(t *testing.T) {
	t.Parallel()

	output := runTDDGuardScript(t, nil, map[string]string{
		`AWM_VERIFY_FILES_CHANGED_JSON`: `["README.md","docs/getting-started.md"]`,
	}, true)
	if !strings.Contains(output, "skip - no behavior-changing Go files matched") {
		t.Fatalf("unexpected skip output: %s", output)
	}
}

func TestAWMTDDGuardFailsWithoutTestDeltaOrExemption(t *testing.T) {
	t.Parallel()

	output := runTDDGuardScript(t, nil, map[string]string{
		`AWM_VERIFY_FILES_CHANGED_JSON`: `["internal/service/backend/verify.go"]`,
	}, false)
	if !strings.Contains(output, "behavior-changing Go files changed without a Go test-file delta") {
		t.Fatalf("unexpected failure output: %s", output)
	}
}

func TestAWMTDDGuardPassesWithCompletedTDDExemption(t *testing.T) {
	t.Parallel()

	planJSON := `{"title":"TDD exemption plan","tasks":[{"key":"tdd:exemption:behavior-preserving","summary":"Explicit exemption","status":"complete","outcome":"No externally observable behavior change."},{"key":"verify:tests","summary":"Run verify","status":"pending"}]}`
	output := runTDDGuardScript(t, map[string]string{
		"plan:receipt-test": planJSON,
	}, map[string]string{
		`AWM_PLAN_KEY`:                  `plan:receipt-test`,
		`AWM_VERIFY_FILES_CHANGED_JSON`: `["internal/service/backend/verify.go"]`,
	}, true)
	if !strings.Contains(output, "pass - completed tdd:exemption task found") {
		t.Fatalf("unexpected exemption output: %s", output)
	}
}

func TestAWMTDDGuardRequiresTDDRedForMultiStepPlans(t *testing.T) {
	t.Parallel()

	planJSON := `{"title":"Planned behavior change","tasks":[{"key":"impl:verify-context","summary":"Implement verify context","status":"in_progress"},{"key":"repo:tdd-policy","summary":"Wire TDD guard","status":"pending"},{"key":"verify:tests","summary":"Run verify","status":"pending"}]}`
	output := runTDDGuardScript(t, map[string]string{
		"plan:receipt-test": planJSON,
	}, map[string]string{
		`AWM_PLAN_KEY`:                  `plan:receipt-test`,
		`AWM_VERIFY_FILES_CHANGED_JSON`: `["internal/service/backend/verify.go","internal/service/backend/service_test.go"]`,
	}, false)
	if !strings.Contains(output, "planned behavior-changing Go work must complete a tdd:red task") {
		t.Fatalf("unexpected failure output: %s", output)
	}
}

func TestAWMTDDGuardRequiresTDDRedForSingleTaskPlans(t *testing.T) {
	t.Parallel()

	planJSON := `{"title":"Small planned behavior change","tasks":[{"key":"impl:verify-context","summary":"Implement verify context","status":"in_progress"},{"key":"verify:tests","summary":"Run verify","status":"pending"}]}`
	output := runTDDGuardScript(t, map[string]string{
		"plan:receipt-test": planJSON,
	}, map[string]string{
		`AWM_PLAN_KEY`:                  `plan:receipt-test`,
		`AWM_VERIFY_FILES_CHANGED_JSON`: `["internal/service/backend/verify.go","internal/service/backend/service_test.go"]`,
	}, false)
	if !strings.Contains(output, "planned behavior-changing Go work must complete a tdd:red task") {
		t.Fatalf("unexpected failure output: %s", output)
	}
}

func TestAWMTDDGuardPassesWithCompletedTDDRedForMultiStepPlans(t *testing.T) {
	t.Parallel()

	planJSON := `{"title":"Planned behavior change","tasks":[{"key":"tdd:red:verify-context","summary":"Capture the failing verify env tests","status":"complete","outcome":"Added failing service and script tests for verify context and TDD enforcement."},{"key":"impl:verify-context","summary":"Implement verify context","status":"in_progress"},{"key":"repo:tdd-policy","summary":"Wire TDD guard","status":"pending"},{"key":"verify:tests","summary":"Run verify","status":"pending"}]}`
	output := runTDDGuardScript(t, map[string]string{
		"plan:receipt-test": planJSON,
	}, map[string]string{
		`AWM_PLAN_KEY`:                  `plan:receipt-test`,
		`AWM_VERIFY_FILES_CHANGED_JSON`: `["internal/service/backend/verify.go","internal/service/backend/service_test.go"]`,
	}, true)
	if !strings.Contains(output, "pass - Go test delta present and TDD metadata satisfied") {
		t.Fatalf("unexpected success output: %s", output)
	}
}

func runTDDGuardScript(t *testing.T, planByKey, extraEnv map[string]string, wantSuccess bool) string {
	t.Helper()

	tempRoot := t.TempDir()
	binDir := filepath.Join(tempRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	var awmScript strings.Builder
	awmScript.WriteString("#!/usr/bin/env bash\nset -euo pipefail\n")
	awmScript.WriteString("key=\"\"\n")
	awmScript.WriteString("while [[ $# -gt 0 ]]; do\n")
	awmScript.WriteString("  case \"$1\" in\n")
	awmScript.WriteString("    --key)\n")
	awmScript.WriteString("      key=\"$2\"\n")
	awmScript.WriteString("      shift 2\n")
	awmScript.WriteString("      ;;\n")
	awmScript.WriteString("    *)\n")
	awmScript.WriteString("      shift\n")
	awmScript.WriteString("      ;;\n")
	awmScript.WriteString("  esac\n")
	awmScript.WriteString("done\n")
	if len(planByKey) == 0 {
		awmScript.WriteString("exit 1\n")
	} else {
		first := true
		for key, content := range planByKey {
			if first {
				awmScript.WriteString("if ")
				first = false
			} else {
				awmScript.WriteString("elif ")
			}
			awmScript.WriteString("[[ \"$key\" == \"" + key + "\" ]]; then\n")
			awmScript.WriteString("  cat <<'JSON'\n")
			awmScript.WriteString("{\"result\":{\"items\":[{\"content\":")
			awmScript.WriteString(shellSingleQuoteJSONString(content))
			awmScript.WriteString("}]}}\n")
			awmScript.WriteString("JSON\n")
			awmScript.WriteString("  exit 0\n")
		}
		awmScript.WriteString("fi\n")
		awmScript.WriteString("exit 1\n")
	}
	writeExecutable(t, filepath.Join(binDir, "awm"), awmScript.String())

	cmd := exec.Command("python3", "scripts/awm-tdd-guard.py")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(baseScriptEnv(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for key, value := range extraEnv {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	output, err := cmd.CombinedOutput()
	if wantSuccess && err != nil {
		t.Fatalf("run tdd guard script: %v\n%s", err, string(output))
	}
	if !wantSuccess && err == nil {
		t.Fatalf("expected tdd guard script to fail, output=%s", string(output))
	}
	return string(output)
}

func shellSingleQuoteJSONString(raw string) string {
	return strconv.Quote(raw)
}

func baseScriptEnv() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "AWM_") {
			continue
		}
		env = append(env, entry)
	}
	return env
}
