package bootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bonztm/agent-workflow-manager/internal/workspace"
)

const (
	DefaultProjectRoot              = "."
	DefaultInitCandidatesPath       = ".awm/init_candidates.json"
	canonicalRulesPrimarySourcePath = ".awm/awm-rules.yaml"
	canonicalRulesSecondaryPath     = "awm-rules.yaml"
	verifyTestsPrimarySourcePath    = ".awm/awm-tests.yaml"
	verifyTestsSecondarySourcePath  = "awm-tests.yaml"
	workflowPrimarySourcePath       = ".awm/awm-workflows.yaml"
	workflowSecondarySourcePath     = "awm-workflows.yaml"
)

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

func EnsureProjectScaffold(projectRoot, rulesFile string) error {
	if err := os.MkdirAll(filepath.Join(projectRoot, ".awm"), 0o755); err != nil {
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
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(outputPath, blob, 0o644); err != nil {
		return fmt.Errorf("write candidate output: %w", err)
	}
	return nil
}

func WriteScaffoldFile(targetPath string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	defer file.Close()
	_, err = file.Write(content)
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

	file, err := os.OpenFile(envExamplePath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		if _, err := file.WriteString("\n"); err != nil {
			return err
		}
	}
	if len(raw) > 0 {
		if _, err := file.WriteString("\n# AWM runtime configuration\n"); err != nil {
			return err
		}
	}
	for _, entry := range missing {
		if _, err := file.WriteString(entry + "\n"); err != nil {
			return err
		}
	}
	return nil
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
