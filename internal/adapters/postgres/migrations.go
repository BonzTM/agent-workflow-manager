package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

var migrationFileRe = regexp.MustCompile(`^\d{4}_[a-z0-9_]+\.sql$`)

type migrationFile struct {
	Name string
	SQL  string
}

func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("postgres pool is required")
	}

	migrations, err := loadMigrations(embeddedMigrations)
	if err != nil {
		return err
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin migrations tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := migrateLegacyAcmSchema(ctx, tx); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
CREATE TABLE IF NOT EXISTS awm_schema_migrations (
	migration_name TEXT PRIMARY KEY,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`); err != nil {
		return fmt.Errorf("ensure schema migrations table: %w", err)
	}

	for _, migration := range migrations {
		var applied bool
		if err := tx.QueryRow(
			ctx,
			`SELECT EXISTS (SELECT 1 FROM awm_schema_migrations WHERE migration_name = $1)`,
			migration.Name,
		).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", migration.Name, err)
		}
		if applied {
			continue
		}

		if _, err := tx.Exec(ctx, migration.SQL); err != nil {
			return fmt.Errorf("apply migration %s: %w", migration.Name, err)
		}

		if _, err := tx.Exec(
			ctx,
			`INSERT INTO awm_schema_migrations (migration_name) VALUES ($1)`,
			migration.Name,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", migration.Name, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migrations tx: %w", err)
	}

	return nil
}

// migrateLegacyAcmSchema upgrades a pre-rename "acm_*" schema to the current
// "awm_*" naming in place. It runs before the schema-migrations ledger is
// consulted, so the recorded migration names line up and the standard migration
// loop then sees every migration as already applied. It is a no-op on fresh
// databases (no legacy ledger) and on databases that already use "awm_*".
func migrateLegacyAcmSchema(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `
DO $$
DECLARE
	r RECORD;
BEGIN
	IF to_regclass('acm_schema_migrations') IS NOT NULL
		AND to_regclass('awm_schema_migrations') IS NULL THEN
		FOR r IN
			SELECT tablename FROM pg_tables
			WHERE schemaname = current_schema() AND tablename LIKE 'acm\_%'
		LOOP
			EXECUTE format('ALTER TABLE %I RENAME TO %I',
				r.tablename, 'awm_' || substring(r.tablename FROM 5));
		END LOOP;
		FOR r IN
			SELECT indexname FROM pg_indexes
			WHERE schemaname = current_schema() AND indexname LIKE '%acm\_%'
		LOOP
			EXECUTE format('ALTER INDEX %I RENAME TO %I',
				r.indexname, replace(r.indexname, 'acm_', 'awm_'));
		END LOOP;
		UPDATE awm_schema_migrations
			SET migration_name = replace(migration_name, '_acm_', '_awm_');
	END IF;
END $$;`); err != nil {
		return fmt.Errorf("migrate legacy acm schema: %w", err)
	}
	return nil
}

func loadMigrations(src fs.FS) ([]migrationFile, error) {
	entries, err := fs.ReadDir(src, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations directory: %w", err)
	}

	migrations := make([]migrationFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !isValidMigrationFilename(name) {
			return nil, fmt.Errorf("invalid migration filename: %s", name)
		}

		content, err := fs.ReadFile(src, path.Join("migrations", name))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}

		sql := strings.TrimSpace(string(content))
		if sql == "" {
			return nil, fmt.Errorf("migration file is empty: %s", name)
		}

		migrations = append(migrations, migrationFile{
			Name: name,
			SQL:  sql,
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Name < migrations[j].Name
	})

	return migrations, nil
}

func isValidMigrationFilename(name string) bool {
	if strings.Contains(name, "/") || strings.Contains(name, `\`) {
		return false
	}
	return migrationFileRe.MatchString(name)
}
