package bootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bonztm/agent-workflow-manager/internal/fswrite"
	"github.com/bonztm/agent-workflow-manager/internal/workspace"
)

const (
	// DefaultProjectRoot is the project root used when none is provided.
	DefaultProjectRoot = "."
	// DefaultInitCandidatesPath is the repo-relative path where init candidate
	// lists are persisted when no explicit output path is given.
	DefaultInitCandidatesPath       = ".awm/init_candidates.json"
	canonicalRulesPrimarySourcePath = ".awm/awm-rules.yaml"
	canonicalRulesSecondaryPath     = "awm-rules.yaml"
	verifyTestsPrimarySourcePath    = ".awm/awm-tests.yaml"
	verifyTestsSecondarySourcePath  = "awm-tests.yaml"
	workflowPrimarySourcePath       = ".awm/awm-workflows.yaml"
	workflowSecondarySourcePath     = "awm-workflows.yaml"
)

// NormalizeProjectRoot converts projectRoot to a cleaned absolute path,
// defaulting to DefaultProjectRoot when blank.
func NormalizeProjectRoot(projectRoot string) string {
	trimmed := strings.TrimSpace(projectRoot)
	if trimmed == "" {
		return DefaultProjectRoot
	}
	absRoot, err := filepath.Abs(trimmed)
	if err != nil {
		return filepath.Clean(trimmed)
	}
	return filepath.Clean(absRoot)
}

// EnsureProjectScaffold creates the .awm directory and the baseline runtime
// files (.gitignore entries, .env.example, blank tests/workflows scaffolds)
// under projectRoot, plus a blank canonical ruleset when rulesFile is blank
// and no ruleset exists yet. Existing files are left untouched.
func EnsureProjectScaffold(projectRoot, rulesFile string) error {
	if err := os.MkdirAll(filepath.Join(projectRoot, ".awm"), 0o755); err != nil { //nolint:gosec // G301: committed repo directory, standard permissions by design
		return err
	}
	if err := ensureRuntimeFiles(projectRoot); err != nil {
		return err
	}
	if err := ensureVerifyTestsScaffold(projectRoot); err != nil {
		return err
	}
	if err := ensureWorkflowDefinitionsScaffold(projectRoot); err != nil {
		return err
	}
	if strings.TrimSpace(rulesFile) != "" {
		return nil
	}
	exists, err := canonicalRulesetExists(projectRoot)
	if err != nil || exists {
		return err
	}
	return WriteScaffoldFile(
		filepath.Join(projectRoot, filepath.FromSlash(canonicalRulesPrimarySourcePath)),
		[]byte(BlankRulesContents),
	)
}

// ResolveOutputPath resolves where init candidates should be written. An
// explicit path wins (made absolute against projectRoot when relative);
// otherwise the default path is used when persistCandidates is true. The
// boolean reports whether anything should be written at all.
func ResolveOutputPath(projectRoot, explicitOutputPath string, persistCandidates bool) (string, bool) {
	if trimmed := strings.TrimSpace(explicitOutputPath); trimmed != "" {
		if filepath.IsAbs(trimmed) {
			return filepath.Clean(trimmed), true
		}
		return filepath.Clean(filepath.Join(projectRoot, trimmed)), true
	}
	if !persistCandidates {
		return "", false
	}
	return filepath.Clean(filepath.Join(projectRoot, DefaultInitCandidatesPath)), true
}

// WriteCandidates writes the candidate paths as an indented JSON document
// ({"candidates": [...]}) to outputPath, creating parent directories and
// overwriting any existing file.
func WriteCandidates(outputPath string, paths []string) error {
	payload := struct {
		Candidates []string `json:"candidates"`
	}{
		Candidates: append([]string(nil), paths...),
	}
	blob, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal candidates: %w", err)
	}
	blob = append(blob, '\n')
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil { //nolint:gosec // G301: committed repo directory, standard permissions by design
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := fswrite.Atomic(outputPath, blob, 0o644); err != nil {
		return fmt.Errorf("write candidate output: %w", err)
	}
	return nil
}

// WriteScaffoldFile creates targetPath with content, creating parent
// directories as needed. It is a no-op when the file already exists.
func WriteScaffoldFile(targetPath string, content []byte) error {
	err := fswrite.AtomicNew(targetPath, content, 0o644)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	return err
}

func ensureRuntimeFiles(projectRoot string) error {
	if err := workspace.EnsureGitIgnoreContains(projectRoot, workspace.SQLiteGitIgnoreEntries(workspace.DefaultSQLiteRelativePath)...); err != nil {
		return err
	}
	return ensureEnvExample(projectRoot)
}

func ensureEnvExample(projectRoot string) error {
	envExamplePath := filepath.Join(projectRoot, workspace.DotEnvExampleFileName)
	existingKeys := map[string]struct{}{}

	raw, err := os.ReadFile(envExamplePath)
	switch {
	case err == nil:
		for key := range workspace.ParseDotEnv(raw) {
			existingKeys[key] = struct{}{}
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return err
	}

	entries := []string{
		"# AWM runtime configuration",
		"# Copy this file to .env to override local defaults.",
		"AWM_PROJECT_ID=myproject",
		"AWM_PROJECT_ROOT=/path/to/repo",
		"AWM_SQLITE_PATH=.awm/context.db",
		"AWM_PG_DSN=postgres://user:pass@localhost:5432/agents_context?sslmode=disable",
		"AWM_UNBOUNDED=false",
		"AWM_LOG_LEVEL=info",
		"AWM_LOG_SINK=stderr",
	}

	if len(existingKeys) == 0 && len(raw) == 0 {
		content := strings.Join(entries, "\n") + "\n"
		return WriteScaffoldFile(envExamplePath, []byte(content))
	}

	missing := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry, "#") {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if _, ok := existingKeys[key]; ok {
			continue
		}
		missing = append(missing, entry)
	}
	if len(missing) == 0 {
		return nil
	}

	// Build the appended content in memory and write the whole file
	// atomically, so a crash can never leave a truncated or half-appended
	// .env.example (and a symlinked one keeps its link).
	var builder strings.Builder
	builder.Write(raw)
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		builder.WriteByte('\n')
	}
	if len(raw) > 0 {
		builder.WriteString("\n# AWM runtime configuration\n")
	}
	for _, entry := range missing {
		builder.WriteString(entry + "\n")
	}
	return fswrite.Atomic(envExamplePath, []byte(builder.String()), 0o644)
}

func ensureVerifyTestsScaffold(projectRoot string) error {
	exists, err := sourceExists(projectRoot, verifyTestsPrimarySourcePath, verifyTestsSecondarySourcePath)
	if err != nil || exists {
		return err
	}
	return WriteScaffoldFile(
		filepath.Join(projectRoot, filepath.FromSlash(verifyTestsPrimarySourcePath)),
		[]byte(BlankTestsContents),
	)
}

func ensureWorkflowDefinitionsScaffold(projectRoot string) error {
	exists, err := sourceExists(projectRoot, workflowPrimarySourcePath, workflowSecondarySourcePath)
	if err != nil || exists {
		return err
	}
	return WriteScaffoldFile(
		filepath.Join(projectRoot, filepath.FromSlash(workflowPrimarySourcePath)),
		[]byte(BlankWorkflowsContents),
	)
}

func canonicalRulesetExists(projectRoot string) (bool, error) {
	return sourceExists(projectRoot, canonicalRulesPrimarySourcePath, canonicalRulesSecondaryPath)
}

func sourceExists(projectRoot string, sourcePaths ...string) (bool, error) {
	for _, sourcePath := range sourcePaths {
		absolutePath := filepath.Clean(filepath.Join(projectRoot, filepath.FromSlash(sourcePath)))
		stat, err := os.Stat(absolutePath)
		switch {
		case err == nil:
			if !stat.IsDir() {
				return true, nil
			}
		case errors.Is(err, os.ErrNotExist):
			continue
		default:
			return false, err
		}
	}
	return false, nil
}
