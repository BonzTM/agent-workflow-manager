package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/logging"
)

func TestNewServiceWithLogger_DefaultsToSQLiteAndIsLoggingDecorated(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "runtime-default.sqlite")
	recorder := logging.NewRecorder()

	svc, cleanup, err := NewServiceWithLogger(context.Background(), Config{
		SQLitePath: dbPath,
	}, recorder)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	t.Cleanup(cleanup)

	result, apiErr := svc.Health(context.Background(), v1.HealthPayload{
		ProjectID: "project.alpha",
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Check == nil || result.Check.Summary.TotalFindings < 0 {
		t.Fatalf("unexpected summary: %+v", result)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected sqlite file at %q: %v", dbPath, err)
	}

	entries := recorder.Entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(entries))
	}
	if entries[0].Event != logging.EventServiceOperationStart {
		t.Fatalf("unexpected start event: %s", entries[0].Event)
	}
	if entries[1].Event != logging.EventServiceOperationFinish {
		t.Fatalf("unexpected finish event: %s", entries[1].Event)
	}
	if got := entries[1].Fields["ok"]; got != true {
		t.Fatalf("unexpected finish ok field: %v", got)
	}
	if got := entries[1].Fields["error_code"]; got != nil {
		t.Fatalf("unexpected finish error_code: %v", got)
	}
}

func TestNewServiceWithLogger_PostgresDSNTakesPrecedenceOverSQLite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "should-not-be-used.sqlite")

	_, _, err := NewServiceWithLogger(context.Background(), Config{
		PostgresDSN: "://invalid dsn",
		SQLitePath:  dbPath,
	}, logging.NewRecorder())
	if err == nil {
		t.Fatal("expected postgres initialization error")
	}
	if !strings.Contains(err.Error(), "initialize postgres repository") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected sqlite path to remain unused, stat err=%v", statErr)
	}
}

func TestNewServiceWithLogger_ImplicitRepoSQLiteDoesNotMutateGitIgnore(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	svc, cleanup, err := NewServiceWithLogger(context.Background(), Config{
		ProjectRoot:   root,
		ProjectIsRepo: true,
	}, logging.NewRecorder())
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	t.Cleanup(cleanup)

	result, apiErr := svc.Health(context.Background(), v1.HealthPayload{
		ProjectID: "project.alpha",
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if result.Check == nil || result.Check.Summary.TotalFindings < 0 {
		t.Fatalf("unexpected summary: %+v", result)
	}

	if _, err := os.Stat(filepath.Join(root, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("expected runtime service construction to leave .gitignore alone, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".awm", "context.db")); err != nil {
		t.Fatalf("expected default sqlite file: %v", err)
	}
}
