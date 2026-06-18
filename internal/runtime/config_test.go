package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/bonztm/agent-workflow-manager/internal/workspace"
)

func TestConfigFromEnv_ReadsExplicitSQLitePath(t *testing.T) {
	t.Setenv(PostgresDSNEnvVar, "")
	t.Setenv(SQLitePathEnvVar, " /tmp/custom-sqlite.db ")

	cfg := ConfigFromEnv()
	if cfg.PostgresConfigured() {
		t.Fatalf("expected postgres to be unconfigured")
	}
	if got, want := cfg.EffectiveSQLitePath(), filepath.Clean("/tmp/custom-sqlite.db"); got != want {
		t.Fatalf("unexpected sqlite path: got %q want %q", got, want)
	}
}

func TestConfigFromEnv_DefaultSQLitePathUsesRepoRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	subdir := filepath.Join(root, "internal", "runtime")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	restore := withWorkingDir(t, subdir)
	defer restore()

	t.Setenv(ProjectRootEnvVar, "")
	t.Setenv(ProjectIDEnvVar, "")
	t.Setenv(PostgresDSNEnvVar, "")
	t.Setenv(SQLitePathEnvVar, "")

	cfg := ConfigFromEnv()
	if !cfg.ProjectIsRepo {
		t.Fatal("expected repo root detection")
	}
	if got, want := cfg.ProjectRoot, root; got != want {
		t.Fatalf("unexpected project root: got %q want %q", got, want)
	}
	if got, want := cfg.EffectiveSQLitePath(), filepath.Join(root, filepath.FromSlash(workspace.DefaultSQLiteRelativePath)); got != want {
		t.Fatalf("unexpected sqlite path: got %q want %q", got, want)
	}
	if got, want := cfg.EffectiveProjectID(), filepath.Base(root); got != want {
		t.Fatalf("unexpected inferred project id: got %q want %q", got, want)
	}
}

func TestConfigFromEnv_LoadsDotEnvFromRepoRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("AWM_PROJECT_ID=dot-env-project\nAWM_PG_DSN=postgres://ctx:ctx@localhost:5432/awm?sslmode=disable\nAWM_SQLITE_PATH=.awm/custom.db\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	restore := withWorkingDir(t, root)
	defer restore()

	unsetEnv(t, ProjectRootEnvVar)
	unsetEnv(t, ProjectIDEnvVar)
	unsetEnv(t, PostgresDSNEnvVar)
	unsetEnv(t, SQLitePathEnvVar)

	cfg := ConfigFromEnv()
	if got, want := cfg.PostgresDSN, "postgres://ctx:ctx@localhost:5432/awm?sslmode=disable"; got != want {
		t.Fatalf("unexpected postgres dsn: got %q want %q", got, want)
	}
	if got, want := cfg.EffectiveProjectID(), "dot-env-project"; got != want {
		t.Fatalf("unexpected project id from .env: got %q want %q", got, want)
	}
	if got, want := cfg.EffectiveSQLitePath(), filepath.Join(root, ".awm", "custom.db"); got != want {
		t.Fatalf("unexpected sqlite path from .env: got %q want %q", got, want)
	}
}

func TestConfigFromEnv_ProcessEnvOverridesDotEnv(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("AWM_PG_DSN=postgres://dot-env\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	restore := withWorkingDir(t, root)
	defer restore()

	t.Setenv(PostgresDSNEnvVar, "postgres://process-env")

	cfg := ConfigFromEnv()
	if got, want := cfg.PostgresDSN, "postgres://process-env"; got != want {
		t.Fatalf("unexpected postgres dsn: got %q want %q", got, want)
	}
}

func TestConfigFromEnv_UsesExplicitProjectRootOutsideRepo(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("AWM_SQLITE_PATH=.awm/custom.db\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	outside := t.TempDir()
	restore := withWorkingDir(t, outside)
	defer restore()

	unsetEnv(t, PostgresDSNEnvVar)
	unsetEnv(t, SQLitePathEnvVar)
	t.Setenv(ProjectRootEnvVar, root)

	cfg := ConfigFromEnv()
	if got, want := cfg.ProjectRoot, root; got != want {
		t.Fatalf("unexpected project root: got %q want %q", got, want)
	}
	if got, want := cfg.EffectiveSQLitePath(), filepath.Join(root, ".awm", "custom.db"); got != want {
		t.Fatalf("unexpected sqlite path from explicit project root: got %q want %q", got, want)
	}
}

func TestConfigFromEnv_ExplicitProjectIDWinsOverInference(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	restore := withWorkingDir(t, root)
	defer restore()

	t.Setenv(ProjectIDEnvVar, "stable-project")

	cfg := ConfigFromEnv()
	if got, want := cfg.ProjectID, "stable-project"; got != want {
		t.Fatalf("unexpected configured project id: got %q want %q", got, want)
	}
	if got, want := cfg.EffectiveProjectID(), "stable-project"; got != want {
		t.Fatalf("unexpected effective project id: got %q want %q", got, want)
	}
}

func TestConfigFromEnv_InferredProjectIDSanitizesExplicitProjectRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "My Demo Repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	outside := t.TempDir()
	restore := withWorkingDir(t, outside)
	defer restore()

	unsetEnv(t, ProjectIDEnvVar)
	t.Setenv(ProjectRootEnvVar, root)

	cfg := ConfigFromEnv()
	if got, want := cfg.EffectiveProjectID(), "My-Demo-Repo"; got != want {
		t.Fatalf("unexpected sanitized project id: got %q want %q", got, want)
	}
}

func TestConfigFromEnv_InferredProjectIDFallsBackToASCIIHash(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "项目")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	outside := t.TempDir()
	restore := withWorkingDir(t, outside)
	defer restore()

	unsetEnv(t, ProjectIDEnvVar)
	t.Setenv(ProjectRootEnvVar, root)

	cfg := ConfigFromEnv()
	if got, want := cfg.EffectiveProjectID(), "project-"+shortSHA256("项目"); got != want {
		t.Fatalf("unexpected fallback project id: got %q want %q", got, want)
	}
}

func shortSHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:8]
}

func withWorkingDir(t *testing.T, dir string) func() {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %q: %v", dir, err)
	}
	return func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	}
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()

	previous, hadPrevious := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unsetenv %s: %v", key, err)
	}
	t.Cleanup(func() {
		var err error
		if hadPrevious {
			err = os.Setenv(key, previous)
		} else {
			err = os.Unsetenv(key)
		}
		if err != nil {
			t.Fatalf("restore env %s: %v", key, err)
		}
	})
}
