package postgres

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func TestBuildCandidatePointersQuery_DeterministicInputs(t *testing.T) {
	staleBefore := time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)
	q := core.CandidatePointerQuery{
		ProjectID: "project-a",
		Limit:     0,
		StaleFilter: core.StaleFilter{
			AllowStale:  true,
			StaleBefore: &staleBefore,
		},
	}

	sql, args, err := buildCandidatePointersQuery(q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "ORDER BY p.pointer_key ASC") {
		t.Fatalf("unexpected ordering clause:\n%s", sql)
	}
	if strings.Contains(sql, "ts_rank_cd") || strings.Contains(sql, "websearch_to_tsquery") {
		t.Fatalf("did not expect SQL-specific ranking expressions:\n%s", sql)
	}
	wantArgs := []any{"project-a", staleBefore}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("unexpected args: got %#v want %#v", args, wantArgs)
	}
}

func TestBuildCandidatePointersQuery_DoesNotApplySQLRankingOrLimit(t *testing.T) {
	sql, args, err := buildCandidatePointersQuery(core.CandidatePointerQuery{
		ProjectID: "project-a",
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(sql, "LIMIT") {
		t.Fatalf("did not expect SQL limit clause:\n%s", sql)
	}
	if strings.Contains(sql, "search_vector") {
		t.Fatalf("did not expect SQL search-vector ranking predicate:\n%s", sql)
	}
	if got, want := args, []any{"project-a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected args: got %#v want %#v", got, want)
	}
}

func TestIsValidMigrationFilename(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{name: "0001_init.sql", valid: true},
		{name: "../0002_bad.sql", valid: false},
		{name: "0002-bad.sql", valid: false},
		{name: `migrations\0003_bad.sql`, valid: false},
	}

	for _, tc := range tests {
		if got := isValidMigrationFilename(tc.name); got != tc.valid {
			t.Fatalf("filename %q validity mismatch: got %v want %v", tc.name, got, tc.valid)
		}
	}
}

func TestBuildFetchReceiptScopeQuery_DeterministicOrdering(t *testing.T) {
	sql, args, err := buildFetchReceiptScopeQuery(core.ReceiptScopeQuery{
		ProjectID: " project-a ",
		ReceiptID: " receipt-123 ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "r.initial_scope_paths") || !strings.Contains(sql, "r.baseline_paths_json") {
		t.Fatalf("expected stored initial scope and baseline columns in query:\n%s", sql)
	}
	if !strings.Contains(sql, "r.pointer_keys") {
		t.Fatalf("expected receipt metadata columns in query:\n%s", sql)
	}
	if strings.Contains(sql, "awm_pointers p") || strings.Contains(sql, "unnest(r.pointer_keys)") {
		t.Fatalf("did not expect mutable pointer joins in query:\n%s", sql)
	}
	wantArgs := []any{"project-a", "receipt-123"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("unexpected args: got %#v want %#v", args, wantArgs)
	}
}

func TestBuildFetchReceiptScopeQuery_ValidatesInput(t *testing.T) {
	if _, _, err := buildFetchReceiptScopeQuery(core.ReceiptScopeQuery{ProjectID: "", ReceiptID: "receipt-123"}); err == nil {
		t.Fatal("expected project_id validation error")
	}
	if _, _, err := buildFetchReceiptScopeQuery(core.ReceiptScopeQuery{ProjectID: "project-a", ReceiptID: ""}); err == nil {
		t.Fatal("expected receipt_id validation error")
	}
}

func TestBuildMarkDeletedPointersStaleQuery_DeterministicAndValidated(t *testing.T) {
	sql, args, err := buildMarkDeletedPointersStaleQuery(" project-a ", []string{" b/path.go ", "a/path.go", "a/path.go", ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "UPDATE awm_pointers") || !strings.Contains(sql, "path = ANY") {
		t.Fatalf("unexpected SQL:\n%s", sql)
	}
	wantArgs := []any{"project-a", []string{"a/path.go", "b/path.go"}}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("unexpected args: got %#v want %#v", args, wantArgs)
	}

	if _, _, err := buildMarkDeletedPointersStaleQuery("", []string{"a/path.go"}); err == nil {
		t.Fatal("expected project_id validation error")
	}
	if _, _, err := buildMarkDeletedPointersStaleQuery("project-a", []string{"", " "}); err == nil {
		t.Fatal("expected deleted paths validation error")
	}
}

func TestBuildMarkMissingPointersStaleQuery_DeterministicAndValidated(t *testing.T) {
	sql, args, err := buildMarkMissingPointersStaleQuery(" project-a ", []string{" b/path.go ", "a/path.go", "a/path.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "UPDATE awm_pointers") || !strings.Contains(sql, "NOT (path = ANY") {
		t.Fatalf("unexpected SQL:\n%s", sql)
	}
	wantArgs := []any{"project-a", []string{"a/path.go", "b/path.go"}}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("unexpected args: got %#v want %#v", args, wantArgs)
	}

	if _, _, err := buildMarkMissingPointersStaleQuery("", []string{"a/path.go"}); err == nil {
		t.Fatal("expected project_id validation error")
	}
}

func TestBuildRefreshPointersQuery_DeterministicAndValidated(t *testing.T) {
	sql, args, err := buildRefreshPointersQuery(" project-a ", []core.SyncPath{
		{Path: " b/path.go ", ContentHash: "bbbb"},
		{Path: "a/path.go", ContentHash: "aaaa"},
		{Path: "a/path.go", ContentHash: "aaaa"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "UPDATE awm_pointers p") || !strings.Contains(sql, "content_hash") {
		t.Fatalf("unexpected SQL:\n%s", sql)
	}
	wantArgs := []any{"project-a", "a/path.go", "aaaa", "b/path.go", "bbbb"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("unexpected args: got %#v want %#v", args, wantArgs)
	}

	if _, _, err := buildRefreshPointersQuery("", []core.SyncPath{{Path: "a", ContentHash: "x"}}); err == nil {
		t.Fatal("expected project_id validation error")
	}
	if _, _, err := buildRefreshPointersQuery("project-a", []core.SyncPath{{Path: "a", ContentHash: ""}}); err == nil {
		t.Fatal("expected content_hash validation error")
	}
}

func TestBuildInsertPointerCandidatesQuery_DeterministicAndValidated(t *testing.T) {
	sql, args, err := buildInsertPointerCandidatesQuery(" project-a ", []core.SyncPath{
		{Path: " b/path.go ", ContentHash: "bbbb"},
		{Path: "a/path.go", ContentHash: "aaaa"},
		{Path: "a/path.go", ContentHash: "aaaa"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "INSERT INTO awm_pointer_candidates") || !strings.Contains(sql, "ON CONFLICT (project_id, path) DO NOTHING") {
		t.Fatalf("unexpected SQL:\n%s", sql)
	}
	wantArgs := []any{"project-a", "a/path.go", "aaaa", "b/path.go", "bbbb"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("unexpected args: got %#v want %#v", args, wantArgs)
	}

	if _, _, err := buildInsertPointerCandidatesQuery("", []core.SyncPath{{Path: "a", ContentHash: "x"}}); err == nil {
		t.Fatal("expected project_id validation error")
	}
	if _, _, err := buildInsertPointerCandidatesQuery("project-a", []core.SyncPath{{Path: "", ContentHash: "x"}}); err == nil {
		t.Fatal("expected path validation error")
	}
}

func TestBuildLookupFetchStateQuery_DeterministicAndValidated(t *testing.T) {
	sql, args, err := buildLookupFetchStateQuery(core.FetchLookupQuery{
		ProjectID: " project-a ",
		ReceiptID: " receipt-123 ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "FROM awm_receipts") || !strings.Contains(sql, "LEFT JOIN LATERAL") {
		t.Fatalf("unexpected SQL:\n%s", sql)
	}
	if !strings.Contains(sql, "ORDER BY created_at DESC, run_id DESC") {
		t.Fatalf("expected deterministic run ordering in lookup query:\n%s", sql)
	}
	wantArgs := []any{"project-a", "receipt-123"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("unexpected args: got %#v want %#v", args, wantArgs)
	}

	if _, _, err := buildLookupFetchStateQuery(core.FetchLookupQuery{ProjectID: "", ReceiptID: "receipt-123"}); err == nil {
		t.Fatal("expected project_id validation error")
	}
	if _, _, err := buildLookupFetchStateQuery(core.FetchLookupQuery{ProjectID: "project-a", ReceiptID: ""}); err == nil {
		t.Fatal("expected receipt_id validation error")
	}
}

func TestBuildLookupPointerByKeyQuery_DeterministicAndValidated(t *testing.T) {
	sql, args, err := buildLookupPointerByKeyQuery(core.PointerLookupQuery{
		ProjectID:  " project-a ",
		PointerKey: " pointer-123 ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "FROM awm_pointers") || !strings.Contains(sql, "pointer_key = $2") || !strings.Contains(sql, "is_stale = FALSE") {
		t.Fatalf("unexpected SQL:\n%s", sql)
	}
	wantArgs := []any{"project-a", "pointer-123"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("unexpected args: got %#v want %#v", args, wantArgs)
	}

	if _, _, err := buildLookupPointerByKeyQuery(core.PointerLookupQuery{ProjectID: "", PointerKey: "pointer-123"}); err == nil {
		t.Fatal("expected project_id validation error")
	}
	if _, _, err := buildLookupPointerByKeyQuery(core.PointerLookupQuery{ProjectID: "project-a", PointerKey: ""}); err == nil {
		t.Fatal("expected pointer_key validation error")
	}
}

func TestBuildListWorkItemsQuery_DeterministicAndValidated(t *testing.T) {
	sql, args, err := buildListWorkItemsQuery(core.FetchLookupQuery{
		ProjectID: " project-a ",
		ReceiptID: " receipt-123 ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "FROM awm_work_items") || !strings.Contains(sql, "ORDER BY item_key ASC") {
		t.Fatalf("unexpected SQL:\n%s", sql)
	}
	wantArgs := []any{"project-a", "receipt-123"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("unexpected args: got %#v want %#v", args, wantArgs)
	}

	if _, _, err := buildListWorkItemsQuery(core.FetchLookupQuery{ProjectID: "", ReceiptID: "receipt-123"}); err == nil {
		t.Fatal("expected project_id validation error")
	}
	if _, _, err := buildListWorkItemsQuery(core.FetchLookupQuery{ProjectID: "project-a", ReceiptID: ""}); err == nil {
		t.Fatal("expected receipt_id validation error")
	}
}

func TestBuildUpsertWorkItemsQuery_DeterministicAndValidated(t *testing.T) {
	sql, args, err := buildUpsertWorkItemsQuery(core.WorkItemsUpsertInput{
		ProjectID: " project-a ",
		ReceiptID: " receipt-123 ",
		Items: []core.WorkItem{
			{ItemKey: " src/b.go ", Status: core.WorkItemStatusComplete},
			{ItemKey: "src/a.go", Status: core.WorkItemStatusInProgress},
			{ItemKey: "src/a.go", Status: core.WorkItemStatusBlocked},
			{ItemKey: "src/c.go", Status: "unknown"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "INSERT INTO awm_work_items") || !strings.Contains(sql, "ON CONFLICT (project_id, receipt_id, item_key) DO UPDATE") {
		t.Fatalf("unexpected SQL:\n%s", sql)
	}
	if !strings.Contains(sql, "WITH incoming(item_key, status)") {
		t.Fatalf("expected deterministic VALUES CTE in SQL:\n%s", sql)
	}

	wantArgs := []any{
		"project-a",
		"receipt-123",
		"src/a.go", core.WorkItemStatusBlocked,
		"src/b.go", core.WorkItemStatusComplete,
		"src/c.go", core.WorkItemStatusPending,
	}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("unexpected args: got %#v want %#v", args, wantArgs)
	}

	if _, _, err := buildUpsertWorkItemsQuery(core.WorkItemsUpsertInput{ProjectID: "", ReceiptID: "receipt-123", Items: []core.WorkItem{{ItemKey: "src/a.go", Status: core.WorkItemStatusPending}}}); err == nil {
		t.Fatal("expected project_id validation error")
	}
	if _, _, err := buildUpsertWorkItemsQuery(core.WorkItemsUpsertInput{ProjectID: "project-a", ReceiptID: "", Items: []core.WorkItem{{ItemKey: "src/a.go", Status: core.WorkItemStatusPending}}}); err == nil {
		t.Fatal("expected receipt_id validation error")
	}
	if _, _, err := buildUpsertWorkItemsQuery(core.WorkItemsUpsertInput{ProjectID: "project-a", ReceiptID: "receipt-123", Items: []core.WorkItem{{ItemKey: "", Status: core.WorkItemStatusPending}}}); err == nil {
		t.Fatal("expected item key validation error")
	}
}

func TestNormalizeWorkItemStatus_NormalizesLegacyCompleted(t *testing.T) {
	if got := normalizeWorkItemStatus("completed"); got != core.WorkItemStatusComplete {
		t.Fatalf("legacy completed should normalize to complete, got %q", got)
	}
	if got := normalizeWorkItemStatus(core.WorkItemStatusComplete); got != core.WorkItemStatusComplete {
		t.Fatalf("canonical complete should remain complete, got %q", got)
	}
	if got := storageWorkItemStatus(core.WorkItemStatusComplete); got != core.WorkItemStatusComplete {
		t.Fatalf("storage status should remain canonical complete, got %q", got)
	}
}
