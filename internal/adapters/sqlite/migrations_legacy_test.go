package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// TestSQLiteMigrations_UpgradesLegacyAcmSchemaInPlace verifies that a database
// created before the acm->awm rename is upgraded in place: legacy tables and
// the ledger are renamed, recorded data survives, and migrations are not re-run.
func TestSQLiteMigrations_UpgradesLegacyAcmSchemaInPlace(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy.sqlite")

	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Build a pre-rename "acm_*" database: a ledger that records every migration
	// under its old name plus a data table holding a row that must survive.
	if _, err := db.ExecContext(ctx, `
CREATE TABLE acm_schema_migrations (migration_name TEXT PRIMARY KEY, applied_at INTEGER NOT NULL DEFAULT (unixepoch()));
CREATE TABLE acm_pointers (pointer_id INTEGER PRIMARY KEY, pointer_key TEXT NOT NULL);
INSERT INTO acm_pointers (pointer_id, pointer_key) VALUES (1, 'keep-me');`); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
	for _, m := range migrations {
		legacyName := strings.Replace(m.Name, "_awm_", "_acm_", 1)
		if _, err := db.ExecContext(ctx,
			`INSERT INTO acm_schema_migrations (migration_name) VALUES (?)`, legacyName); err != nil {
			t.Fatalf("seed ledger row %s: %v", legacyName, err)
		}
	}

	if err := applyMigrations(ctx, db); err != nil {
		t.Fatalf("applyMigrations on legacy db: %v", err)
	}

	// Legacy ledger and data table no longer exist.
	assertSQLiteTableAbsent(ctx, t, db, "acm_schema_migrations")
	assertSQLiteTableAbsent(ctx, t, db, "acm_pointers")

	// Data preserved under the new name.
	var key string
	if err := db.QueryRowContext(ctx,
		`SELECT pointer_key FROM awm_pointers WHERE pointer_id = 1`).Scan(&key); err != nil {
		t.Fatalf("read migrated data: %v", err)
	}
	if key != "keep-me" {
		t.Fatalf("migrated data = %q, want %q", key, "keep-me")
	}

	// Ledger rewritten to awm_* names so no migration is considered pending.
	var acmNamed int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM awm_schema_migrations WHERE migration_name LIKE '%_acm_%'`).Scan(&acmNamed); err != nil {
		t.Fatalf("count legacy ledger names: %v", err)
	}
	if acmNamed != 0 {
		t.Fatalf("ledger still has %d acm-named migrations", acmNamed)
	}
	var total int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM awm_schema_migrations`).Scan(&total); err != nil {
		t.Fatalf("count ledger rows: %v", err)
	}
	if total != len(migrations) {
		t.Fatalf("ledger row count = %d, want %d", total, len(migrations))
	}
}

// TestSQLiteMigrations_LegacyGuardNoOpOnFreshDB verifies the guard does nothing
// on a database that has never seen the legacy schema.
func TestSQLiteMigrations_LegacyGuardNoOpOnFreshDB(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "fresh.sqlite")

	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := applyMigrations(ctx, db); err != nil {
		t.Fatalf("applyMigrations on fresh db: %v", err)
	}
	// Idempotent second run must also succeed.
	if err := applyMigrations(ctx, db); err != nil {
		t.Fatalf("second applyMigrations on fresh db: %v", err)
	}
	assertSQLiteTableAbsent(ctx, t, db, "acm_schema_migrations")
}

func assertSQLiteTableAbsent(ctx context.Context, t *testing.T, db *sql.DB, name string) {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n); err != nil {
		t.Fatalf("check table %s: %v", name, err)
	}
	if n != 0 {
		t.Fatalf("expected table %s to be absent, but it exists", name)
	}
}
