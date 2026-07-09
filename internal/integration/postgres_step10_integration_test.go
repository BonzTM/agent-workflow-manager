//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/logging"
	"github.com/bonztm/agent-workflow-manager/internal/runtime"
)

const (
	integrationDSNEnvVar = "AWM_PG_DSN"
)

func TestRuntimePostgresIntegration_Step10Evidence(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(integrationDSNEnvVar))
	if dsn == "" {
		t.Skipf("%s is required", integrationDSNEnvVar)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	svc, cleanup, err := runtime.NewServiceWithLogger(ctx, runtime.Config{PostgresDSN: dsn}, logging.NewDiscardLogger())
	if err != nil {
		t.Fatalf("new runtime service: %v", err)
	}
	t.Cleanup(cleanup)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect postgres for assertions: %v", err)
	}
	t.Cleanup(pool.Close)

	assertMigrationsApplied(t, ctx, pool)

	projectID := fmt.Sprintf("step10.integration.%d", time.Now().UTC().UnixNano())
	pointers := []seedPointer{
		{
			Key:         "rule.step10.integration",
			Path:        ".awm/awm-rules.yaml",
			Label:       "Step 10 integration rule",
			Description: "Postgres integration evidence must remain receipt scoped for runtime and done flows.",
			Tags:        []string{"postgres", "integration", "runtime", "done", "enforcement-hard"},
		},
		{
			Key:         "pointer.runtime.init",
			Path:        "internal/runtime/service_factory.go",
			Label:       "Runtime Postgres init path",
			Description: "Runtime wiring should initialize a migrated Postgres repository",
			Tags:        []string{"postgres", "runtime", "integration"},
		},
		{
			Key:         "pointer.service.report",
			Path:        "internal/service/backend/service.go",
			Label:       "Report completion write path",
			Description: "Report completion persists run summaries tied to receipt scope",
			Tags:        []string{"postgres", "done", "integration"},
		},
	}
	for _, pointer := range pointers {
		seedPointerRow(t, ctx, pool, projectID, pointer)
	}

	getResult, apiErr := svc.Context(ctx, v1.ContextPayload{
		ProjectID:         projectID,
		TaskText:          "postgres integration evidence for runtime migration and done",
		Phase:             v1.PhaseExecute,
		InitialScopePaths: []string{"internal/runtime/service_factory.go", "internal/service/backend/service.go"},
	})
	if apiErr != nil {
		t.Fatalf("context API error: %+v", apiErr)
	}
	if getResult.Status != "ok" {
		t.Fatalf("expected context status ok, got %q", getResult.Status)
	}
	if getResult.Receipt == nil {
		t.Fatal("expected non-nil receipt")
	}
	if got := getResult.Receipt.InitialScopePaths; !reflect.DeepEqual(got, []string{"internal/runtime/service_factory.go", "internal/service/backend/service.go"}) {
		t.Fatalf("unexpected initial scope paths: got %v", got)
	}
	pointerKeys := receiptPointerKeys(getResult.Receipt)
	if len(pointerKeys) == 0 {
		t.Fatal("expected at least one receipt rule pointer key")
	}

	upsertReceiptScope(t, ctx, pool, getResult.Receipt, nil)

	reportOutcome := "step-10 integration done accepted"
	reportResult, apiErr := svc.Done(ctx, v1.DonePayload{
		ProjectID:    projectID,
		ReceiptID:    getResult.Receipt.Meta.ReceiptID,
		FilesChanged: []string{"internal/runtime/service_factory.go"},
		Outcome:      reportOutcome,
	})
	if apiErr != nil {
		t.Fatalf("done API error: code=%s message=%s details=%v", apiErr.Code, apiErr.Message, apiErr.Details)
	}
	if !reportResult.Accepted {
		t.Fatalf("expected accepted completion, got violations: %+v", reportResult.Violations)
	}
	if reportResult.RunID <= 0 {
		t.Fatalf("expected run_id > 0, got %d", reportResult.RunID)
	}

	var persistedStatus string
	var persistedOutcome string
	var persistedFiles []string
	if err := pool.QueryRow(ctx, `
SELECT status, outcome, files_changed
FROM awm_runs
WHERE run_id = $1
`, reportResult.RunID).Scan(&persistedStatus, &persistedOutcome, &persistedFiles); err != nil {
		t.Fatalf("query persisted run summary: %v", err)
	}
	if persistedStatus != "accepted" {
		t.Fatalf("expected persisted run status accepted, got %q", persistedStatus)
	}
	if persistedOutcome != reportOutcome {
		t.Fatalf("expected persisted run outcome %q, got %q", reportOutcome, persistedOutcome)
	}
	if len(persistedFiles) == 0 || !slices.Contains(persistedFiles, "internal/runtime/service_factory.go") {
		t.Fatalf("expected persisted files_changed to include %q, got %v", "internal/runtime/service_factory.go", persistedFiles)
	}
}

func assertMigrationsApplied(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	var relName string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(to_regclass('public.awm_schema_migrations')::text, '')`).Scan(&relName); err != nil {
		t.Fatalf("query schema migration relation: %v", err)
	}
	if relName != "awm_schema_migrations" {
		t.Fatalf("expected awm_schema_migrations relation, got %q", relName)
	}

	rows, err := pool.Query(ctx, `SELECT migration_name FROM awm_schema_migrations ORDER BY migration_name`)
	if err != nil {
		t.Fatalf("query migration records: %v", err)
	}
	defer rows.Close()

	got := make([]string, 0, 4)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan migration record: %v", err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate migration records: %v", err)
	}

	want := []string{
		"0001_awm_foundation.sql",
		"0002_awm_propose_memory.sql",
		"0003_awm_sync.sql",
		"0004_awm_work_items.sql",
		"0005_awm_work_plans.sql",
		"0006_awm_work_plan_hierarchy.sql",
		"0007_awm_verification_runs.sql",
		"0008_awm_run_history_indexes.sql",
		"0010_awm_review_attempts.sql",
		"0011_awm_receipt_scope_pointer_paths.sql",
		"0012_awm_initial_scope_and_baselines.sql",
		"0013_awm_complete_status.sql",
		"0014_awm_superseded_status.sql",
		"0015_awm_drop_memory.sql",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected migration record set: got %v want %v", got, want)
	}
}

type seedPointer struct {
	Key         string
	Path        string
	Label       string
	Description string
	Tags        []string
}

func seedPointerRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID string, pointer seedPointer) {
	t.Helper()

	if _, err := pool.Exec(ctx, `
INSERT INTO awm_pointers (
	project_id,
	pointer_key,
	path,
	anchor,
	kind,
	label,
	description,
	tags,
	is_rule,
	is_stale
) VALUES ($1, $2, $3, '', 'code', $4, $5, $6, FALSE, FALSE)
ON CONFLICT (project_id, pointer_key) DO UPDATE
SET
	path = EXCLUDED.path,
	kind = EXCLUDED.kind,
	label = EXCLUDED.label,
	description = EXCLUDED.description,
	tags = EXCLUDED.tags,
	is_rule = EXCLUDED.is_rule,
	is_stale = EXCLUDED.is_stale,
	stale_at = NULL,
	updated_at = NOW()
`, projectID, pointer.Key, pointer.Path, pointer.Label, pointer.Description, pointer.Tags); err != nil {
		t.Fatalf("seed pointer %q: %v", pointer.Key, err)
	}
}

func upsertReceiptScope(t *testing.T, ctx context.Context, pool *pgxpool.Pool, receipt *v1.ContextReceipt, pointerKeys []string) {
	t.Helper()

	if _, err := pool.Exec(ctx, `
INSERT INTO awm_receipts (
	receipt_id,
	project_id,
	task_text,
	phase,
	resolved_tags,
	pointer_keys,
	summary_json
) VALUES ($1, $2, $3, $4, $5, $6, '{"source":"integration"}'::jsonb)
ON CONFLICT (receipt_id) DO UPDATE
SET
	project_id = EXCLUDED.project_id,
	task_text = EXCLUDED.task_text,
	phase = EXCLUDED.phase,
	resolved_tags = EXCLUDED.resolved_tags,
	pointer_keys = EXCLUDED.pointer_keys,
	summary_json = EXCLUDED.summary_json
`, receipt.Meta.ReceiptID, receipt.Meta.ProjectID, receipt.Meta.TaskText, string(receipt.Meta.Phase), receipt.Meta.ResolvedTags, pointerKeys); err != nil {
		t.Fatalf("upsert receipt scope: %v", err)
	}
}

func receiptPointerKeys(receipt *v1.ContextReceipt) []string {
	if receipt == nil {
		return nil
	}

	keys := make(map[string]struct{}, len(receipt.Rules))
	for _, rule := range receipt.Rules {
		key := strings.TrimSpace(rule.Key)
		if key != "" {
			keys[key] = struct{}{}
		}
	}
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}

func lookupPointerPathsByKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID string, pointerKeys []string) []string {
	t.Helper()
	if strings.TrimSpace(projectID) == "" || len(pointerKeys) == 0 {
		return nil
	}

	rows, err := pool.Query(ctx, `
SELECT path
FROM awm_pointers
WHERE project_id = $1
	AND pointer_key = ANY($2)
ORDER BY path ASC
`, projectID, pointerKeys)
	if err != nil {
		t.Fatalf("query pointer paths by key: %v", err)
	}
	defer rows.Close()

	out := make([]string, 0, len(pointerKeys))
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			t.Fatalf("scan pointer path by key: %v", err)
		}
		normalized := strings.TrimSpace(path)
		if normalized == "" {
			continue
		}
		out = append(out, normalized)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate pointer paths by key: %v", err)
	}
	return out
}
