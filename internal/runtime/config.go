package runtime

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/bonztm/agent-workflow-manager/internal/projectid"
	"github.com/bonztm/agent-workflow-manager/internal/workspace"
)

const PostgresDSNEnvVar = "AWM_PG_DSN"
const SQLitePathEnvVar = "AWM_SQLITE_PATH"
const ProjectIDEnvVar = "AWM_PROJECT_ID"
const ProjectRootEnvVar = "AWM_PROJECT_ROOT"

type Config struct {
	PostgresDSN   string
	ProjectID     string
	SQLitePath    string
	ProjectRoot   string
	ProjectIsRepo bool
}

func ConfigFromEnv() Config {
	env := loadRuntimeEnv("", os.LookupEnv)
	return Config{
		PostgresDSN:   env.Get(PostgresDSNEnvVar),
		ProjectID:     env.Get(ProjectIDEnvVar),
		SQLitePath:    env.Get(SQLitePathEnvVar),
		ProjectRoot:   strings.TrimSpace(env.projectRoot),
		ProjectIsRepo: env.projectIsRepo,
	}
}

func (c Config) PostgresConfigured() bool {
	return strings.TrimSpace(c.PostgresDSN) != ""
}

func (c Config) EffectiveSQLitePath() string {
	if path := strings.TrimSpace(c.SQLitePath); path != "" {
		if filepath.IsAbs(path) {
			return filepath.Clean(path)
		}
		if base := c.effectiveProjectRoot(); base != "" {
			return filepath.Clean(filepath.Join(base, path))
		}
		return filepath.Clean(path)
	}

	if base := c.effectiveProjectRoot(); base != "" {
		return filepath.Join(base, filepath.FromSlash(workspace.DefaultSQLiteRelativePath))
	}
	return filepath.Join(os.TempDir(), "agent-workflow-manager-context.db")
}

func (c Config) UsesImplicitSQLitePath() bool {
	return strings.TrimSpace(c.SQLitePath) == ""
}

func (c Config) EffectiveProjectRoot() string {
	return c.effectiveProjectRoot()
}

func (c Config) EffectiveProjectID() string {
	if projectID := strings.TrimSpace(c.ProjectID); projectID != "" {
		return projectID
	}
	return projectid.FromRoot(c.effectiveProjectRoot())
}

func (c Config) effectiveProjectRoot() string {
	if root := strings.TrimSpace(c.ProjectRoot); root != "" {
		return filepath.Clean(root)
	}

	detected := workspace.DetectRoot("")
	if root := strings.TrimSpace(detected.Path); root != "" {
		return filepath.Clean(root)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Clean(cwd)
}
