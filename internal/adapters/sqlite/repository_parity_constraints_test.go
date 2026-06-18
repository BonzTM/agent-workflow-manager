package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteMigrations_EnforcePointerLinkForeignKeys(t *testing.T) {
	ctx := context.Background()
	repo, err := New(ctx, Config{Path: filepath.Join(t.TempDir(), "ctx.sqlite")})
	if err != nil {
		t.Fatalf("new sqlite repository: %v", err)
	}
	defer func() { _ = repo.Close() }()

	if _, err := repo.db.ExecContext(ctx, `
INSERT INTO awm_pointers (project_id, pointer_key, path, anchor, kind, label, description, tags_json, is_rule, is_stale)
VALUES (?, ?, ?, '', 'code', ?, ?, '[]', 0, 0)
`, "project.alpha", "code:from", "internal/from.go", "From", "from pointer"); err != nil {
		t.Fatalf("insert source pointer: %v", err)
	}

	_, err = repo.db.ExecContext(ctx, `
INSERT INTO awm_pointer_links (project_id, from_key, to_key)
VALUES (?, ?, ?)
`, "project.alpha", "code:from", "code:missing")
	if err == nil {
		t.Fatal("expected foreign key error for orphan pointer link")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Fatalf("unexpected foreign key error: %v", err)
	}
}

func TestSQLiteDSNPragmas_ApplyToSecondConnection(t *testing.T) {
	ctx := context.Background()
	repo, err := New(ctx, Config{Path: filepath.Join(t.TempDir(), "ctx.sqlite")})
	if err != nil {
		t.Fatalf("new sqlite repository: %v", err)
	}
	defer func() { _ = repo.Close() }()

	repo.db.SetMaxOpenConns(2)

	conn1, err := repo.db.Conn(ctx)
	if err != nil {
		t.Fatalf("first connection: %v", err)
	}
	defer func() { _ = conn1.Close() }()

	conn2, err := repo.db.Conn(ctx)
	if err != nil {
		t.Fatalf("second connection: %v", err)
	}
	defer func() { _ = conn2.Close() }()

	var busyTimeout int
	if err := conn2.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy_timeout pragma: %v", err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("unexpected busy_timeout pragma: got %d want 5000", busyTimeout)
	}

	if _, err := conn1.ExecContext(ctx, `
INSERT INTO awm_pointers (project_id, pointer_key, path, anchor, kind, label, description, tags_json, is_rule, is_stale)
VALUES (?, ?, ?, '', 'code', ?, ?, '[]', 0, 0)
`, "project.alpha", "code:from", "internal/from.go", "From", "from pointer"); err != nil {
		t.Fatalf("insert source pointer: %v", err)
	}

	_, err = conn2.ExecContext(ctx, `
INSERT INTO awm_pointer_links (project_id, from_key, to_key)
VALUES (?, ?, ?)
`, "project.alpha", "code:from", "code:missing")
	if err == nil {
		t.Fatal("expected foreign key error on second connection")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Fatalf("unexpected foreign key error: %v", err)
	}
}
