package runtime

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/bonztm/agent-workflow-manager/internal/projectid"
	"github.com/bonztm/agent-workflow-manager/internal/workspace"
)

const (
	// PostgresDSNEnvVar is the environment variable holding the Postgres DSN;
	// when set (non-blank), Postgres is used instead of SQLite.
	PostgresDSNEnvVar = "AWM_PG_DSN"
	// SQLitePathEnvVar is the environment variable overriding the SQLite
	// database path.
	SQLitePathEnvVar = "AWM_SQLITE_PATH"
	// ProjectIDEnvVar is the environment variable overriding the derived
	// project identifier.
	ProjectIDEnvVar = "AWM_PROJECT_ID"
	// ProjectRootEnvVar is the environment variable overriding the detected
	// project root directory.
	ProjectRootEnvVar = "AWM_PROJECT_ROOT"
)

// Config holds the runtime settings that select the storage backend and
// identify the project a service instance operates on.
type Config struct {
	PostgresDSN   string
	ProjectID     string
	SQLitePath    string
	ProjectRoot   string
	ProjectIsRepo bool
}

// ConfigFromEnv builds a Config from the process environment (and the
// project's .env file via loadRuntimeEnv), reading the AWM_* variables.
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

// PostgresConfigured reports whether a non-blank Postgres DSN is set,
// meaning the Postgres backend should be used.
func (c Config) PostgresConfigured() bool {
	return strings.TrimSpace(c.PostgresDSN) != ""
}

// EffectiveSQLitePath resolves the SQLite database path: an explicit
// SQLitePath (absolute, or relative to the effective project root), else
// the default path under the project root, else a file in the OS temp dir.
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

// UsesImplicitSQLitePath reports whether no explicit SQLite path was
// configured, so EffectiveSQLitePath falls back to a derived location.
func (c Config) UsesImplicitSQLitePath() bool {
	return strings.TrimSpace(c.SQLitePath) == ""
}

// EffectiveProjectRoot resolves the project root: the configured
// ProjectRoot, else the detected workspace root, else the current working
// directory ("" if that cannot be determined).
func (c Config) EffectiveProjectRoot() string {
	return c.effectiveProjectRoot()
}

// EffectiveProjectID returns the configured ProjectID if set, otherwise an
// identifier derived from the effective project root's base name.
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
