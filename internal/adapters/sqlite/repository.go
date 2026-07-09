package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" database/sql driver

	"github.com/bonztm/agent-workflow-manager/internal/core"
	storagedomain "github.com/bonztm/agent-workflow-manager/internal/storage/domain"
)

const defaultCandidateLimit = 32

// Repository is the SQLite-backed implementation of the core repository
// interfaces. It owns a *sql.DB handle and is safe for concurrent use.
type Repository struct {
	db *sql.DB
}

var (
	_ core.Repository             = (*Repository)(nil)
	_ core.WorkPlanRepository     = (*Repository)(nil)
	_ core.HistoryRepository      = (*Repository)(nil)
	_ core.VerificationRepository = (*Repository)(nil)
)

// New opens (creating if necessary) the SQLite database at cfg.Path, applies
// any pending schema migrations, and enables WAL journaling. The returned
// Repository owns the connection; callers must Close it when done.
func New(ctx context.Context, cfg Config) (*Repository, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	dbPath := cfg.NormalizedPath()
	if err := ensureParentDirectory(dbPath); err != nil {
		return nil, err
	}

	dsn := sqliteDSN(dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite db: %w", err)
	}
	if err := applyMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply sqlite migrations: %w", err)
	}
	if err := enableWAL(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable sqlite wal: %w", err)
	}

	return &Repository{db: db}, nil
}

func sqliteDSN(dbPath string) string {
	u := &url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(dbPath),
	}
	q := u.Query()
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	u.RawQuery = q.Encode()
	return u.String()
}

func enableWAL(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("sqlite db is required")
	}
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL`); err != nil {
		return err
	}
	return nil
}

// rollbackTx releases tx if it is still open. It is intended for use in defer
// statements as best-effort cleanup: after a successful Commit, Rollback
// reports sql.ErrTxDone, and any other failure occurs on a path that is
// already returning an error, so there is nothing further to handle.
func rollbackTx(tx *sql.Tx) {
	_ = tx.Rollback() //nolint:errcheck // best-effort rollback in defer; returns sql.ErrTxDone after a successful commit
}

// Close releases the underlying database handle. It is a no-op on a nil or
// already-unopened repository.
func (r *Repository) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

// FetchCandidatePointers returns the project's pointers that match the
// query's tag and staleness filters, ranked and truncated per the query
// limit. It requires a non-empty project ID.
func (r *Repository) FetchCandidatePointers(ctx context.Context, input core.CandidatePointerQuery) ([]core.CandidatePointer, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("sqlite db is required")
	}

	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return nil, errors.New("project_id is required")
	}

	input.StaleFilter.StaleBefore = normalizeStaleBefore(input.StaleFilter.StaleBefore)

	rows, err := r.db.QueryContext(ctx, `
SELECT
	pointer_key,
	path,
	anchor,
	kind,
	label,
	description,
	tags_json,
	is_rule,
	is_stale,
	stale_at,
	updated_at
FROM awm_pointers
WHERE project_id = ?
`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query candidate pointers: %w", err)
	}
	defer rows.Close()

	candidates := make([]core.CandidatePointer, 0)
	for rows.Next() {
		var (
			tagsJSON   string
			isRuleInt  int64
			isStaleInt int64
			staleAtSec sql.NullInt64
			updatedAt  int64
			row        core.CandidatePointer
		)
		if err := rows.Scan(
			&row.Key,
			&row.Path,
			&row.Anchor,
			&row.Kind,
			&row.Label,
			&row.Description,
			&tagsJSON,
			&isRuleInt,
			&isStaleInt,
			&staleAtSec,
			&updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan candidate pointer: %w", err)
		}

		tags, err := decodeStringList(tagsJSON)
		if err != nil {
			return nil, fmt.Errorf("decode candidate pointer tags: %w", err)
		}

		row.Tags = tags
		row.IsRule = isRuleInt != 0
		row.IsStale = isStaleInt != 0
		row.UpdatedAt = unixTime(updatedAt)

		var staleAt *time.Time
		if staleAtSec.Valid {
			t := unixTime(staleAtSec.Int64)
			staleAt = &t
		}
		if !matchesStaleFilter(row.IsStale, staleAt, input.StaleFilter) {
			continue
		}

		candidates = append(candidates, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate candidate pointers: %w", err)
	}
	return storagedomain.SortAndLimitCandidatePointers(candidates, input, defaultCandidateLimit), nil
}

// ListPointerInventory returns every indexed pointer for the project,
// including stale entries, for inventory-style reporting.
func (r *Repository) ListPointerInventory(ctx context.Context, projectID string) ([]core.PointerInventory, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("sqlite db is required")
	}

	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, errors.New("project_id is required")
	}

	rows, err := r.db.QueryContext(ctx, `
SELECT
	path,
	MAX(is_stale) AS is_stale
FROM awm_pointers
WHERE project_id = ?
GROUP BY path
ORDER BY path ASC
`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query pointer inventory: %w", err)
	}
	defer rows.Close()

	results := make([]core.PointerInventory, 0)
	for rows.Next() {
		var (
			pathValue  string
			isStaleInt int64
		)
		if err := rows.Scan(&pathValue, &isStaleInt); err != nil {
			return nil, fmt.Errorf("scan pointer inventory: %w", err)
		}
		normalizedPath := normalizeSyncPath(pathValue)
		if normalizedPath == "" {
			continue
		}
		results = append(results, core.PointerInventory{
			Path:    normalizedPath,
			IsStale: isStaleInt != 0,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pointer inventory: %w", err)
	}
	return results, nil
}

// UpsertPointerStubs inserts or refreshes auto-indexed pointer stubs for the
// project in a single transaction and reports how many rows changed. Stubs
// with empty paths are skipped; existing pointers are un-staled in place.
func (r *Repository) UpsertPointerStubs(ctx context.Context, projectID string, stubs []core.PointerStub) (int, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("sqlite db is required")
	}

	normalized, err := normalizePointerStubs(projectID, stubs)
	if err != nil {
		return 0, err
	}
	if len(normalized) == 0 {
		return 0, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer rollbackTx(tx)

	updated := 0
	for _, stub := range normalized {
		tagsJSON, encodeErr := encodeStringList(stub.Tags)
		if encodeErr != nil {
			return 0, fmt.Errorf("encode pointer stub tags: %w", encodeErr)
		}

		isRule := boolToInt(strings.EqualFold(stub.Kind, "rule"))
		tag, execErr := tx.ExecContext(ctx, `
INSERT INTO awm_pointers (
	project_id,
	pointer_key,
	path,
	anchor,
	kind,
	label,
	description,
	tags_json,
	is_rule,
	is_stale,
	stale_at,
	content_hash,
	updated_at
)
VALUES (?, ?, ?, '', ?, ?, ?, ?, ?, 0, NULL, NULL, unixepoch())
ON CONFLICT(project_id, pointer_key) DO UPDATE SET
	path = excluded.path,
	anchor = excluded.anchor,
	is_stale = 0,
	stale_at = NULL,
	updated_at = unixepoch()
`,
			strings.TrimSpace(projectID),
			stub.PointerKey,
			stub.Path,
			stub.Kind,
			stub.Label,
			stub.Description,
			tagsJSON,
			isRule,
		)
		if execErr != nil {
			return 0, fmt.Errorf("upsert pointer stubs: %w", execErr)
		}
		rowsAffected, rowsErr := tag.RowsAffected()
		if rowsErr != nil {
			return 0, fmt.Errorf("read pointer stub rows affected: %w", rowsErr)
		}
		updated += int(rowsAffected)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit tx: %w", err)
	}
	return updated, nil
}

// FetchReceiptScope loads the stored scope for a receipt. It returns
// core.ErrReceiptScopeNotFound when no receipt matches the query.
func (r *Repository) FetchReceiptScope(ctx context.Context, input core.ReceiptScopeQuery) (core.ReceiptScope, error) {
	if r == nil || r.db == nil {
		return core.ReceiptScope{}, errors.New("sqlite db is required")
	}

	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return core.ReceiptScope{}, errors.New("project_id is required")
	}
	receiptID := strings.TrimSpace(input.ReceiptID)
	if receiptID == "" {
		return core.ReceiptScope{}, errors.New("receipt_id is required")
	}

	var (
		taskText              string
		phase                 string
		resolvedTagsJSON      string
		pointerKeysJSON       string
		initialScopePathsJSON string
		baselineCaptured      int
		baselinePathsJSON     string
		actor                 core.Actor
	)
	err := r.db.QueryRowContext(
		ctx,
		`
SELECT task_text, phase, resolved_tags_json, pointer_keys_json, initial_scope_paths_json, baseline_captured, baseline_paths_json, actor_harness, actor_model, actor_session
FROM awm_receipts
WHERE project_id = ?
	AND receipt_id = ?
`,
		projectID,
		receiptID,
	).Scan(&taskText, &phase, &resolvedTagsJSON, &pointerKeysJSON, &initialScopePathsJSON, &baselineCaptured, &baselinePathsJSON, &actor.Harness, &actor.Model, &actor.SessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.ReceiptScope{}, core.ErrReceiptScopeNotFound
		}
		return core.ReceiptScope{}, fmt.Errorf("query receipt scope: %w", err)
	}

	resolvedTags, err := decodeStringList(resolvedTagsJSON)
	if err != nil {
		return core.ReceiptScope{}, fmt.Errorf("decode resolved_tags: %w", err)
	}
	pointerKeys, err := decodeStringList(pointerKeysJSON)
	if err != nil {
		return core.ReceiptScope{}, fmt.Errorf("decode pointer_keys: %w", err)
	}
	initialScopePaths, err := decodeStringList(initialScopePathsJSON)
	if err != nil {
		return core.ReceiptScope{}, fmt.Errorf("decode initial_scope_paths: %w", err)
	}
	baselinePaths, err := decodeSyncPathList(baselinePathsJSON)
	if err != nil {
		return core.ReceiptScope{}, fmt.Errorf("decode baseline_paths: %w", err)
	}

	return core.ReceiptScope{
		ProjectID:         projectID,
		ReceiptID:         receiptID,
		TaskText:          strings.TrimSpace(taskText),
		Phase:             strings.TrimSpace(phase),
		ResolvedTags:      resolvedTags,
		PointerKeys:       pointerKeys,
		InitialScopePaths: initialScopePaths,
		BaselineCaptured:  baselineCaptured == 1,
		BaselinePaths:     baselinePaths,
		Actor:             actor,
	}, nil
}

// LookupFetchState resolves a receipt (by ID or latest for the project) into
// its fetch-time state. It returns core.ErrFetchLookupNotFound when the
// receipt does not exist.
func (r *Repository) LookupFetchState(ctx context.Context, input core.FetchLookupQuery) (core.FetchLookup, error) {
	if r == nil || r.db == nil {
		return core.FetchLookup{}, errors.New("sqlite db is required")
	}

	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return core.FetchLookup{}, errors.New("project_id is required")
	}
	receiptID := strings.TrimSpace(input.ReceiptID)
	if receiptID == "" {
		return core.FetchLookup{}, errors.New("receipt_id is required")
	}

	var (
		lookupReceiptID string
		runID           int64
		runStatus       sql.NullString
		updatedAt       int64
	)
	err := r.db.QueryRowContext(ctx, `
SELECT
	r.receipt_id,
	COALESCE(run.run_id, 0) AS run_id,
	COALESCE(run.status, '') AS run_status,
	COALESCE(run.created_at, r.created_at) AS updated_at
FROM awm_receipts r
LEFT JOIN (
	SELECT run_id, status, created_at
	FROM awm_runs
	WHERE project_id = ?
		AND receipt_id = ?
	ORDER BY created_at DESC, run_id DESC
	LIMIT 1
) run ON 1 = 1
WHERE r.project_id = ?
	AND r.receipt_id = ?
`, projectID, receiptID, projectID, receiptID).Scan(&lookupReceiptID, &runID, &runStatus, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.FetchLookup{}, core.ErrFetchLookupNotFound
		}
		return core.FetchLookup{}, fmt.Errorf("query fetch lookup: %w", err)
	}

	workItems, err := r.ListWorkItems(ctx, core.FetchLookupQuery{
		ProjectID: projectID,
		ReceiptID: receiptID,
	})
	if err != nil {
		return core.FetchLookup{}, err
	}

	return core.FetchLookup{
		ProjectID:  projectID,
		ReceiptID:  lookupReceiptID,
		RunID:      runID,
		RunStatus:  strings.TrimSpace(runStatus.String),
		PlanStatus: derivePlanStatus(workItems),
		WorkItems:  workItems,
		UpdatedAt:  unixTime(updatedAt),
	}, nil
}

// LookupPointerByKey fetches a single pointer by its pointer key. It returns
// core.ErrPointerLookupNotFound when the key is not indexed for the project.
func (r *Repository) LookupPointerByKey(ctx context.Context, input core.PointerLookupQuery) (core.CandidatePointer, error) {
	if r == nil || r.db == nil {
		return core.CandidatePointer{}, errors.New("sqlite db is required")
	}

	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return core.CandidatePointer{}, errors.New("project_id is required")
	}
	pointerKey := strings.TrimSpace(input.PointerKey)
	if pointerKey == "" {
		return core.CandidatePointer{}, errors.New("pointer_key is required")
	}

	var (
		tagsJSON   string
		isRuleInt  int64
		isStaleInt int64
		updatedAt  int64
		row        core.CandidatePointer
	)
	err := r.db.QueryRowContext(ctx, `
SELECT
	pointer_key,
	path,
	anchor,
	kind,
	label,
	description,
	tags_json,
	is_rule,
	is_stale,
	updated_at
FROM awm_pointers
WHERE project_id = ?
	AND pointer_key = ?
	AND is_stale = 0
`, projectID, pointerKey).Scan(
		&row.Key,
		&row.Path,
		&row.Anchor,
		&row.Kind,
		&row.Label,
		&row.Description,
		&tagsJSON,
		&isRuleInt,
		&isStaleInt,
		&updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.CandidatePointer{}, core.ErrPointerLookupNotFound
		}
		return core.CandidatePointer{}, fmt.Errorf("query pointer lookup: %w", err)
	}

	tags, err := decodeStringList(tagsJSON)
	if err != nil {
		return core.CandidatePointer{}, fmt.Errorf("decode pointer tags: %w", err)
	}

	return core.CandidatePointer{
		Key:         strings.TrimSpace(row.Key),
		Path:        normalizeSyncPath(row.Path),
		Anchor:      strings.TrimSpace(row.Anchor),
		Kind:        strings.TrimSpace(row.Kind),
		Label:       strings.TrimSpace(row.Label),
		Description: strings.TrimSpace(row.Description),
		Tags:        normalizeStringList(tags),
		IsRule:      isRuleInt != 0,
		IsStale:     isStaleInt != 0,
		UpdatedAt:   unixTime(updatedAt),
	}, nil
}

// UpsertWorkItems inserts or updates the receipt's work items in a single
// transaction and reports how many rows changed.
func (r *Repository) UpsertWorkItems(ctx context.Context, input core.WorkItemsUpsertInput) (int, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("sqlite db is required")
	}

	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return 0, errors.New("project_id is required")
	}
	receiptID := strings.TrimSpace(input.ReceiptID)
	if receiptID == "" {
		return 0, errors.New("receipt_id is required")
	}

	items, err := normalizeWorkItems(input.Items)
	if err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, errors.New("work items are required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer rollbackTx(tx)

	updated := 0
	for _, item := range items {
		tag, execErr := tx.ExecContext(ctx, `
INSERT INTO awm_work_items (
	project_id,
	receipt_id,
	item_key,
	status,
	created_at,
	updated_at
) VALUES (?, ?, ?, ?, unixepoch(), unixepoch())
ON CONFLICT(project_id, receipt_id, item_key) DO UPDATE SET
	status = excluded.status,
	updated_at = unixepoch()
`, projectID, receiptID, item.ItemKey, storageWorkItemStatus(item.Status))
		if execErr != nil {
			return 0, fmt.Errorf("upsert work item: %w", execErr)
		}
		rowsAffected, rowsErr := tag.RowsAffected()
		if rowsErr != nil {
			return 0, fmt.Errorf("read work item rows affected: %w", rowsErr)
		}
		updated += int(rowsAffected)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit tx: %w", err)
	}

	return updated, nil
}

// ListWorkItems returns the work items recorded for the receipt resolved by
// the query, in stable item-key order.
func (r *Repository) ListWorkItems(ctx context.Context, input core.FetchLookupQuery) ([]core.WorkItem, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("sqlite db is required")
	}

	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return nil, errors.New("project_id is required")
	}
	receiptID := strings.TrimSpace(input.ReceiptID)
	if receiptID == "" {
		return nil, errors.New("receipt_id is required")
	}

	rows, err := r.db.QueryContext(ctx, `
SELECT item_key, status, updated_at
FROM awm_work_items
WHERE project_id = ?
	AND receipt_id = ?
ORDER BY item_key ASC
`, projectID, receiptID)
	if err != nil {
		return nil, fmt.Errorf("query work items: %w", err)
	}
	defer rows.Close()

	items := make([]core.WorkItem, 0)
	for rows.Next() {
		var (
			itemKey   string
			status    string
			updatedAt int64
		)
		if err := rows.Scan(&itemKey, &status, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan work item: %w", err)
		}

		normalizedKey := normalizeSyncPath(itemKey)
		if normalizedKey == "" {
			continue
		}

		items = append(items, core.WorkItem{
			ItemKey:   normalizedKey,
			Status:    normalizeWorkItemStatus(status),
			UpdatedAt: unixTime(updatedAt),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate work items: %w", err)
	}

	return items, nil
}

// UpsertWorkPlan creates or updates a work plan and its tasks in a single
// transaction, honoring the merge/replace mode, deriving the plan status from
// task states when none is supplied, and returning the stored plan along with
// the number of task rows changed.
func (r *Repository) UpsertWorkPlan(ctx context.Context, input core.WorkPlanUpsertInput) (core.WorkPlanUpsertResult, error) {
	if r == nil || r.db == nil {
		return core.WorkPlanUpsertResult{}, errors.New("sqlite db is required")
	}

	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return core.WorkPlanUpsertResult{}, errors.New("project_id is required")
	}
	planKey := strings.TrimSpace(input.PlanKey)
	if planKey == "" {
		return core.WorkPlanUpsertResult{}, errors.New("plan_key is required")
	}
	mode := normalizeWorkPlanMode(input.Mode)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return core.WorkPlanUpsertResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer rollbackTx(tx)

	current, found, err := lookupWorkPlanTx(ctx, tx, projectID, planKey)
	if err != nil {
		return core.WorkPlanUpsertResult{}, err
	}
	if found && mode == core.WorkPlanModeMerge && len(input.Tasks) > 0 {
		current.Tasks, err = listWorkPlanTasksTx(ctx, tx, projectID, planKey)
		if err != nil {
			return core.WorkPlanUpsertResult{}, err
		}
	}
	next := buildNextWorkPlanState(current, found, input, mode)
	if upsertErr := upsertWorkPlanRowTx(ctx, tx, next); upsertErr != nil {
		return core.WorkPlanUpsertResult{}, upsertErr
	}

	updated := 0
	normalizedTasks := storagedomain.MergeIncomingWorkPlanTasks(current.Tasks, input.Tasks, mode)
	if mode == core.WorkPlanModeReplace {
		tag, delErr := tx.ExecContext(ctx, `
DELETE FROM awm_work_plan_tasks
WHERE project_id = ?
	AND plan_key = ?
`, projectID, planKey)
		if delErr != nil {
			return core.WorkPlanUpsertResult{}, fmt.Errorf("delete work plan tasks: %w", delErr)
		}
		rowsAffected, rowsErr := tag.RowsAffected()
		if rowsErr != nil {
			return core.WorkPlanUpsertResult{}, fmt.Errorf("read work plan task delete rows affected: %w", rowsErr)
		}
		updated += int(rowsAffected)
	}

	for _, task := range normalizedTasks {
		dependsJSON, taskErr := encodeStringList(nonNilStringList(task.DependsOn))
		if taskErr != nil {
			return core.WorkPlanUpsertResult{}, fmt.Errorf("encode task depends_on: %w", taskErr)
		}
		acceptanceJSON, taskErr := encodeStringList(nonNilStringList(task.AcceptanceCriteria))
		if taskErr != nil {
			return core.WorkPlanUpsertResult{}, fmt.Errorf("encode task acceptance criteria: %w", taskErr)
		}
		referencesJSON, taskErr := encodeStringList(nonNilStringList(task.References))
		if taskErr != nil {
			return core.WorkPlanUpsertResult{}, fmt.Errorf("encode task references: %w", taskErr)
		}
		externalRefsJSON, taskErr := encodeStringList(nonNilStringList(task.ExternalRefs))
		if taskErr != nil {
			return core.WorkPlanUpsertResult{}, fmt.Errorf("encode task external refs: %w", taskErr)
		}
		evidenceJSON, taskErr := encodeStringList(nonNilStringList(task.Evidence))
		if taskErr != nil {
			return core.WorkPlanUpsertResult{}, fmt.Errorf("encode task evidence: %w", taskErr)
		}

		tag, taskErr := tx.ExecContext(ctx, `
INSERT INTO awm_work_plan_tasks (
	project_id,
	plan_key,
	task_key,
	summary,
	status,
	parent_task_key,
	depends_on_json,
	acceptance_criteria_json,
	references_json,
	external_refs_json,
	blocked_reason,
	outcome,
	evidence_json,
	created_at,
	updated_at
) VALUES (
	?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, unixepoch(), unixepoch()
)
ON CONFLICT(project_id, plan_key, task_key) DO UPDATE SET
	summary = excluded.summary,
	status = excluded.status,
	parent_task_key = excluded.parent_task_key,
	depends_on_json = excluded.depends_on_json,
	acceptance_criteria_json = excluded.acceptance_criteria_json,
	references_json = excluded.references_json,
	external_refs_json = excluded.external_refs_json,
	blocked_reason = excluded.blocked_reason,
	outcome = excluded.outcome,
	evidence_json = excluded.evidence_json,
	updated_at = unixepoch()
`, projectID, planKey, task.ItemKey, strings.TrimSpace(task.Summary), storageWorkItemStatus(task.Status), strings.TrimSpace(task.ParentTaskKey), dependsJSON, acceptanceJSON, referencesJSON, externalRefsJSON, strings.TrimSpace(task.BlockedReason), strings.TrimSpace(task.Outcome), evidenceJSON)
		if taskErr != nil {
			return core.WorkPlanUpsertResult{}, fmt.Errorf("upsert work plan task: %w", taskErr)
		}
		rowsAffected, rowsErr := tag.RowsAffected()
		if rowsErr != nil {
			return core.WorkPlanUpsertResult{}, fmt.Errorf("read work plan task upsert rows affected: %w", rowsErr)
		}
		updated += int(rowsAffected)
	}

	if strings.TrimSpace(input.Status) == "" {
		tasks, listErr := listWorkPlanTasksTx(ctx, tx, projectID, planKey)
		if listErr != nil {
			return core.WorkPlanUpsertResult{}, listErr
		}
		derivedStatus := derivePlanStatus(tasks)
		if _, updateErr := tx.ExecContext(ctx, `
UPDATE awm_work_plans
SET status = ?, updated_at = unixepoch()
WHERE project_id = ?
	AND plan_key = ?
`, storageWorkItemStatus(derivedStatus), projectID, planKey); updateErr != nil {
			return core.WorkPlanUpsertResult{}, fmt.Errorf("update work plan status: %w", updateErr)
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return core.WorkPlanUpsertResult{}, fmt.Errorf("commit tx: %w", commitErr)
	}

	plan, err := r.LookupWorkPlan(ctx, core.WorkPlanLookupQuery{
		ProjectID: projectID,
		PlanKey:   planKey,
	})
	if err != nil {
		return core.WorkPlanUpsertResult{}, err
	}
	return core.WorkPlanUpsertResult{
		Plan:    plan,
		Updated: updated,
	}, nil
}

// LookupWorkPlan loads a stored work plan and its tasks by plan key (or the
// project's most recent plan when no key is given). It returns
// core.ErrWorkPlanNotFound when no matching plan exists.
func (r *Repository) LookupWorkPlan(ctx context.Context, input core.WorkPlanLookupQuery) (core.WorkPlan, error) {
	if r == nil || r.db == nil {
		return core.WorkPlan{}, errors.New("sqlite db is required")
	}

	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return core.WorkPlan{}, errors.New("project_id is required")
	}
	planKey := strings.TrimSpace(input.PlanKey)
	receiptID := strings.TrimSpace(input.ReceiptID)
	if planKey == "" && receiptID == "" {
		return core.WorkPlan{}, errors.New("plan_key or receipt_id is required")
	}

	if planKey == "" {
		err := r.db.QueryRowContext(ctx, `
SELECT plan_key
FROM awm_work_plans
WHERE project_id = ?
	AND receipt_id = ?
ORDER BY updated_at DESC, plan_key ASC
LIMIT 1
`, projectID, receiptID).Scan(&planKey)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return core.WorkPlan{}, core.ErrWorkPlanNotFound
			}
			return core.WorkPlan{}, fmt.Errorf("query work plan key by receipt: %w", err)
		}
	}

	plan, found, err := lookupWorkPlan(ctx, r.db, projectID, planKey)
	if err != nil {
		return core.WorkPlan{}, err
	}
	if !found {
		return core.WorkPlan{}, core.ErrWorkPlanNotFound
	}
	tasks, err := listWorkPlanTasks(ctx, r.db, projectID, planKey)
	if err != nil {
		return core.WorkPlan{}, err
	}
	plan.Tasks = tasks
	return plan, nil
}

// ListWorkPlans returns plan summaries (with task counts and active task
// keys) for the project, filtered by scope and optional search text.
func (r *Repository) ListWorkPlans(ctx context.Context, input core.WorkPlanListQuery) ([]core.WorkPlanSummary, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("sqlite db is required")
	}

	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return nil, errors.New("project_id is required")
	}
	limit := input.Limit
	if !input.Unbounded && limit <= 0 {
		limit = 8
	}
	if !input.Unbounded && limit > 100 {
		limit = 100
	}

	scope := normalizeWorkPlanListScope(input.Scope)
	searchPattern := workPlanListSearchPattern(input.Query)

	var query strings.Builder
	query.WriteString(`
SELECT
	p.plan_key,
	p.receipt_id,
	p.title,
	p.objective,
	p.status,
	p.kind,
	p.parent_plan_key,
	COUNT(t.task_id) AS task_count_total,
	SUM(CASE WHEN t.status = 'pending' THEN 1 ELSE 0 END) AS task_count_pending,
	SUM(CASE WHEN t.status = 'in_progress' THEN 1 ELSE 0 END) AS task_count_in_progress,
	SUM(CASE WHEN t.status = 'blocked' THEN 1 ELSE 0 END) AS task_count_blocked,
	SUM(CASE WHEN t.status = 'complete' THEN 1 ELSE 0 END) AS task_count_completed,
	p.updated_at
FROM awm_work_plans p
LEFT JOIN awm_work_plan_tasks t
	ON t.project_id = p.project_id
	AND t.plan_key = p.plan_key
WHERE p.project_id = ?
`)
	args := []any{projectID}

	switch scope {
	case workPlanListScopeCurrent:
		query.WriteString("  AND p.status IN ('pending', 'in_progress')\n")
	case workPlanListScopeDeferred:
		query.WriteString("  AND p.status = 'blocked'\n")
	case workPlanListScopeCompleted:
		query.WriteString("  AND p.status IN ('complete', 'superseded')\n")
	}

	if trimmedKind := strings.TrimSpace(input.Kind); trimmedKind != "" {
		query.WriteString("  AND p.kind = ?\n")
		args = append(args, trimmedKind)
	}

	if searchPattern != "" {
		query.WriteString(`  AND (
	LOWER(p.plan_key) LIKE ? ESCAPE '\'
	OR LOWER(COALESCE(p.receipt_id, '')) LIKE ? ESCAPE '\'
	OR LOWER(COALESCE(p.title, '')) LIKE ? ESCAPE '\'
	OR LOWER(COALESCE(p.objective, '')) LIKE ? ESCAPE '\'
	OR LOWER(COALESCE(p.kind, '')) LIKE ? ESCAPE '\'
	OR LOWER(COALESCE(p.parent_plan_key, '')) LIKE ? ESCAPE '\'
	OR EXISTS (
		SELECT 1
		FROM awm_work_plan_tasks wt
		WHERE wt.project_id = p.project_id
			AND wt.plan_key = p.plan_key
			AND (
				LOWER(wt.task_key) LIKE ? ESCAPE '\'
				OR LOWER(COALESCE(wt.summary, '')) LIKE ? ESCAPE '\'
				OR LOWER(COALESCE(wt.outcome, '')) LIKE ? ESCAPE '\'
				OR LOWER(COALESCE(wt.blocked_reason, '')) LIKE ? ESCAPE '\'
			)
	)
)
`)
		for range 10 {
			args = append(args, searchPattern)
		}
	}

	query.WriteString(`
GROUP BY p.plan_key, p.receipt_id, p.title, p.objective, p.status, p.kind, p.parent_plan_key, p.updated_at
ORDER BY p.updated_at DESC, p.plan_key ASC
`)
	if !input.Unbounded {
		query.WriteString("LIMIT ?\n")
		args = append(args, limit)
	}

	rows, err := r.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query work plans: %w", err)
	}
	defer rows.Close()

	out := make([]core.WorkPlanSummary, 0)
	for rows.Next() {
		var (
			planKey             string
			receiptID           string
			title               string
			objective           string
			status              string
			kind                string
			parentPlanKey       string
			taskCountTotal      int64
			taskCountPending    int64
			taskCountInProgress int64
			taskCountBlocked    int64
			taskCountComplete   int64
			updatedAt           int64
		)
		if scanErr := rows.Scan(
			&planKey,
			&receiptID,
			&title,
			&objective,
			&status,
			&kind,
			&parentPlanKey,
			&taskCountTotal,
			&taskCountPending,
			&taskCountInProgress,
			&taskCountBlocked,
			&taskCountComplete,
			&updatedAt,
		); scanErr != nil {
			return nil, fmt.Errorf("scan work plan summary: %w", scanErr)
		}
		planKey = strings.TrimSpace(planKey)
		if planKey == "" {
			continue
		}
		normalizedStatus := normalizeWorkItemStatus(status)
		summary := strings.TrimSpace(title)
		if summary == "" {
			summary = strings.TrimSpace(objective)
		}
		if summary == "" {
			summary = fmt.Sprintf("Plan %s is %s", planKey, normalizedStatus)
		}
		if taskCountTotal > 0 {
			summary = fmt.Sprintf("%s (%d tasks)", summary, taskCountTotal)
		}
		out = append(out, core.WorkPlanSummary{
			ReceiptID:           strings.TrimSpace(receiptID),
			Title:               strings.TrimSpace(title),
			Objective:           strings.TrimSpace(objective),
			PlanKey:             planKey,
			Summary:             summary,
			Status:              normalizedStatus,
			Kind:                strings.TrimSpace(kind),
			ParentPlanKey:       strings.TrimSpace(parentPlanKey),
			TaskCountTotal:      int(taskCountTotal),
			TaskCountPending:    int(taskCountPending),
			TaskCountInProgress: int(taskCountInProgress),
			TaskCountBlocked:    int(taskCountBlocked),
			TaskCountComplete:   int(taskCountComplete),
			UpdatedAt:           unixTime(updatedAt),
		})
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterate work plans: %w", rowsErr)
	}

	if len(out) == 0 {
		return nil, nil
	}

	activeTaskKeysByPlan, err := listActiveWorkPlanTaskKeys(ctx, r.db, projectID, workPlanSummaryKeys(out))
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].ActiveTaskKeys = append([]string(nil), activeTaskKeysByPlan[out[i].PlanKey]...)
	}

	return out, nil
}

// ListReceiptHistory returns receipt summaries for the project, newest
// first, filtered by the query's scope, search text, and limit.
func (r *Repository) ListReceiptHistory(ctx context.Context, input core.ReceiptHistoryListQuery) ([]core.ReceiptHistorySummary, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("sqlite db is required")
	}
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return nil, errors.New("project_id is required")
	}
	limit := input.Limit
	if !input.Unbounded && limit <= 0 {
		limit = 20
	}
	if !input.Unbounded && limit > 100 {
		limit = 100
	}
	searchPattern := workPlanListSearchPattern(input.Query)

	var query strings.Builder
	query.WriteString(`
SELECT
	r.receipt_id,
	r.task_text,
	r.phase,
	COALESCE(run.request_id, '') AS latest_request_id,
	COALESCE(run.status, '') AS latest_status,
	COALESCE(run.created_at, r.created_at) AS updated_at
FROM awm_receipts r
LEFT JOIN (
	SELECT project_id, receipt_id, request_id, status, created_at,
		ROW_NUMBER() OVER (PARTITION BY project_id, receipt_id ORDER BY created_at DESC, run_id DESC) AS rn
	FROM awm_runs
) run
	ON run.project_id = r.project_id
	AND run.receipt_id = r.receipt_id
	AND run.rn = 1
WHERE r.project_id = ?
`)
	args := []any{projectID}

	if searchPattern != "" {
		query.WriteString(`  AND (
	LOWER(r.receipt_id) LIKE ? ESCAPE '\'
	OR LOWER(COALESCE(r.task_text, '')) LIKE ? ESCAPE '\'
	OR LOWER(COALESCE(r.phase, '')) LIKE ? ESCAPE '\'
	OR EXISTS (
		SELECT 1
		FROM awm_runs rr
		WHERE rr.project_id = r.project_id
			AND rr.receipt_id = r.receipt_id
			AND (
				LOWER(COALESCE(rr.request_id, '')) LIKE ? ESCAPE '\'
				OR LOWER(COALESCE(rr.status, '')) LIKE ? ESCAPE '\'
				OR LOWER(COALESCE(rr.outcome, '')) LIKE ? ESCAPE '\'
				OR LOWER(COALESCE(json_extract(rr.summary_json, '$.outcome'), '')) LIKE ? ESCAPE '\'
				OR LOWER(COALESCE(rr.files_changed_json, '')) LIKE ? ESCAPE '\'
			)
	)
)
`)
		for range 8 {
			args = append(args, searchPattern)
		}
	}

	query.WriteString(`
ORDER BY updated_at DESC, r.receipt_id ASC
`)
	if !input.Unbounded {
		query.WriteString("LIMIT ?\n")
		args = append(args, limit)
	}

	rows, err := r.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query receipt history: %w", err)
	}
	defer rows.Close()

	out := make([]core.ReceiptHistorySummary, 0)
	for rows.Next() {
		var (
			row       core.ReceiptHistorySummary
			updatedAt int64
		)
		if err := rows.Scan(
			&row.ReceiptID,
			&row.TaskText,
			&row.Phase,
			&row.LatestRequestID,
			&row.LatestStatus,
			&updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan receipt history: %w", err)
		}
		row.ReceiptID = strings.TrimSpace(row.ReceiptID)
		if row.ReceiptID == "" {
			continue
		}
		row.TaskText = strings.TrimSpace(row.TaskText)
		row.Phase = strings.TrimSpace(row.Phase)
		row.LatestRequestID = strings.TrimSpace(row.LatestRequestID)
		row.LatestStatus = strings.TrimSpace(row.LatestStatus)
		row.UpdatedAt = unixTime(updatedAt)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate receipt history: %w", err)
	}
	return out, nil
}

// ListRunHistory returns run summaries for the project, newest first,
// filtered by the query's scope, search text, and limit.
func (r *Repository) ListRunHistory(ctx context.Context, input core.RunHistoryListQuery) ([]core.RunHistorySummary, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("sqlite db is required")
	}
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return nil, errors.New("project_id is required")
	}
	limit := input.Limit
	if !input.Unbounded && limit <= 0 {
		limit = 20
	}
	if !input.Unbounded && limit > 100 {
		limit = 100
	}
	searchPattern := workPlanListSearchPattern(input.Query)

	var query strings.Builder
	query.WriteString(`
SELECT
	run.run_id,
	run.receipt_id,
	run.request_id,
	COALESCE(r.task_text, '') AS task_text,
	COALESCE(r.phase, '') AS phase,
	run.status,
	run.files_changed_json,
	run.outcome,
	run.actor_harness,
	run.actor_model,
	run.actor_session,
	run.created_at
FROM awm_runs run
LEFT JOIN awm_receipts r
	ON r.project_id = run.project_id
	AND r.receipt_id = run.receipt_id
WHERE run.project_id = ?
`)
	args := []any{projectID}

	if searchPattern != "" {
		query.WriteString(`  AND (
	LOWER(COALESCE(run.request_id, '')) LIKE ? ESCAPE '\'
	OR LOWER(COALESCE(run.status, '')) LIKE ? ESCAPE '\'
	OR LOWER(COALESCE(run.outcome, '')) LIKE ? ESCAPE '\'
	OR LOWER(COALESCE(run.files_changed_json, '')) LIKE ? ESCAPE '\'
	OR LOWER(COALESCE(run.summary_json, '')) LIKE ? ESCAPE '\'
	OR LOWER(COALESCE(r.task_text, '')) LIKE ? ESCAPE '\'
	OR LOWER(COALESCE(r.phase, '')) LIKE ? ESCAPE '\'
	OR LOWER(COALESCE(run.receipt_id, '')) LIKE ? ESCAPE '\'
)
`)
		for range 8 {
			args = append(args, searchPattern)
		}
	}

	query.WriteString(`
ORDER BY run.created_at DESC, run.run_id DESC
`)
	if !input.Unbounded {
		query.WriteString("LIMIT ?\n")
		args = append(args, limit)
	}

	rows, err := r.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query run history: %w", err)
	}
	defer rows.Close()

	out := make([]core.RunHistorySummary, 0)
	for rows.Next() {
		var (
			row              core.RunHistorySummary
			filesChangedJSON string
			updatedAt        int64
		)
		if err := rows.Scan(
			&row.RunID,
			&row.ReceiptID,
			&row.RequestID,
			&row.TaskText,
			&row.Phase,
			&row.Status,
			&filesChangedJSON,
			&row.Outcome,
			&row.Actor.Harness,
			&row.Actor.Model,
			&row.Actor.SessionID,
			&updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan run history: %w", err)
		}
		filesChanged, err := decodeStringList(filesChangedJSON)
		if err != nil {
			return nil, fmt.Errorf("decode run history files_changed: %w", err)
		}
		row.ReceiptID = strings.TrimSpace(row.ReceiptID)
		row.RequestID = strings.TrimSpace(row.RequestID)
		row.TaskText = strings.TrimSpace(row.TaskText)
		row.Phase = strings.TrimSpace(row.Phase)
		row.Status = strings.TrimSpace(row.Status)
		row.Outcome = strings.TrimSpace(row.Outcome)
		row.FilesChanged = filesChanged
		row.UpdatedAt = unixTime(updatedAt)
		if row.RunID <= 0 {
			continue
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run history: %w", err)
	}
	return out, nil
}

// LookupRunHistory loads a single run summary by run or receipt ID. It
// returns core.ErrFetchLookupNotFound when no matching run exists.
func (r *Repository) LookupRunHistory(ctx context.Context, input core.RunHistoryLookupQuery) (core.RunHistorySummary, error) {
	if r == nil || r.db == nil {
		return core.RunHistorySummary{}, errors.New("sqlite db is required")
	}
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return core.RunHistorySummary{}, errors.New("project_id is required")
	}
	if input.RunID <= 0 {
		return core.RunHistorySummary{}, errors.New("run_id must be positive")
	}

	var (
		row              core.RunHistorySummary
		filesChangedJSON string
		updatedAt        int64
	)
	err := r.db.QueryRowContext(ctx, `
SELECT
	run.run_id,
	run.receipt_id,
	run.request_id,
	COALESCE(r.task_text, '') AS task_text,
	COALESCE(r.phase, '') AS phase,
	run.status,
	run.files_changed_json,
	run.outcome,
	run.actor_harness,
	run.actor_model,
	run.actor_session,
	run.created_at
FROM awm_runs run
LEFT JOIN awm_receipts r
	ON r.project_id = run.project_id
	AND r.receipt_id = run.receipt_id
WHERE run.project_id = ?
	AND run.run_id = ?
`, projectID, input.RunID).Scan(
		&row.RunID,
		&row.ReceiptID,
		&row.RequestID,
		&row.TaskText,
		&row.Phase,
		&row.Status,
		&filesChangedJSON,
		&row.Outcome,
		&row.Actor.Harness,
		&row.Actor.Model,
		&row.Actor.SessionID,
		&updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.RunHistorySummary{}, core.ErrFetchLookupNotFound
		}
		return core.RunHistorySummary{}, fmt.Errorf("query run history lookup: %w", err)
	}
	filesChanged, err := decodeStringList(filesChangedJSON)
	if err != nil {
		return core.RunHistorySummary{}, fmt.Errorf("decode run history files_changed: %w", err)
	}
	row.ReceiptID = strings.TrimSpace(row.ReceiptID)
	row.RequestID = strings.TrimSpace(row.RequestID)
	row.TaskText = strings.TrimSpace(row.TaskText)
	row.Phase = strings.TrimSpace(row.Phase)
	row.Status = strings.TrimSpace(row.Status)
	row.Outcome = strings.TrimSpace(row.Outcome)
	row.FilesChanged = filesChanged
	row.UpdatedAt = unixTime(updatedAt)
	return row, nil
}

func listActiveWorkPlanTaskKeys(ctx context.Context, q sqlRowsQuerier, projectID string, planKeys []string) (map[string][]string, error) {
	if len(planKeys) == 0 {
		return nil, nil
	}

	query := `
SELECT
	plan_key,
	task_key
FROM awm_work_plan_tasks
WHERE project_id = ?
	AND plan_key IN (` + placeholders(len(planKeys)) + `)
	AND status NOT IN ('complete', 'superseded')
ORDER BY
	plan_key ASC,
	CASE status
		WHEN 'blocked' THEN 0
		WHEN 'in_progress' THEN 1
		WHEN 'pending' THEN 2
		ELSE 3
	END ASC,
	task_key ASC
`
	args := make([]any, 0, len(planKeys)+1)
	args = append(args, projectID)
	for _, planKey := range planKeys {
		args = append(args, planKey)
	}

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query active work plan task keys: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]string, len(planKeys))
	for rows.Next() {
		var planKey string
		var taskKey string
		if err := rows.Scan(&planKey, &taskKey); err != nil {
			return nil, fmt.Errorf("scan active work plan task key: %w", err)
		}
		planKey = strings.TrimSpace(planKey)
		taskKey = strings.TrimSpace(taskKey)
		if planKey == "" || taskKey == "" {
			continue
		}
		out[planKey] = append(out[planKey], taskKey)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active work plan task keys: %w", err)
	}

	return out, nil
}

// SaveRunReceiptSummary persists a completed run and its receipt summary in
// one transaction, generating any missing run/receipt IDs, and returns the
// IDs that were stored.
func (r *Repository) SaveRunReceiptSummary(ctx context.Context, input core.RunReceiptSummary) (core.RunReceiptIDs, error) {
	if r == nil || r.db == nil {
		return core.RunReceiptIDs{}, errors.New("sqlite db is required")
	}

	normalized, err := normalizeRunReceiptSummary(input)
	if err != nil {
		return core.RunReceiptIDs{}, err
	}
	if normalized.ReceiptID == "" {
		normalized.ReceiptID, err = newReceiptID()
		if err != nil {
			return core.RunReceiptIDs{}, err
		}
	}

	resolvedTagsJSON, err := encodeStringList(nonNilStringList(normalized.ResolvedTags))
	if err != nil {
		return core.RunReceiptIDs{}, err
	}
	pointerKeysJSON, err := encodeStringList(nonNilStringList(normalized.PointerKeys))
	if err != nil {
		return core.RunReceiptIDs{}, err
	}
	filesChangedJSON, err := encodeStringList(nonNilStringList(normalized.FilesChanged))
	if err != nil {
		return core.RunReceiptIDs{}, err
	}

	receiptJSON, err := json.Marshal(struct {
		TaskText     string   `json:"task_text"`
		Phase        string   `json:"phase"`
		ResolvedTags []string `json:"resolved_tags,omitempty"`
		PointerKeys  []string `json:"pointer_keys,omitempty"`
	}{
		TaskText:     normalized.TaskText,
		Phase:        normalized.Phase,
		ResolvedTags: normalized.ResolvedTags,
		PointerKeys:  normalized.PointerKeys,
	})
	if err != nil {
		return core.RunReceiptIDs{}, fmt.Errorf("marshal receipt summary: %w", err)
	}
	runJSON, err := json.Marshal(struct {
		RequestID              string   `json:"request_id,omitempty"`
		Status                 string   `json:"status"`
		FilesChanged           []string `json:"files_changed,omitempty"`
		DefinitionOfDoneIssues []string `json:"definition_of_done_issues,omitempty"`
		Outcome                string   `json:"outcome,omitempty"`
	}{
		RequestID:              normalized.RequestID,
		Status:                 normalized.Status,
		FilesChanged:           normalized.FilesChanged,
		DefinitionOfDoneIssues: normalized.DefinitionOfDoneIssues,
		Outcome:                normalized.Outcome,
	})
	if err != nil {
		return core.RunReceiptIDs{}, fmt.Errorf("marshal run summary: %w", err)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return core.RunReceiptIDs{}, fmt.Errorf("begin tx: %w", err)
	}
	defer rollbackTx(tx)

	_, err = tx.ExecContext(ctx, `
INSERT INTO awm_receipts (
	receipt_id,
	project_id,
	task_text,
	phase,
	resolved_tags_json,
	pointer_keys_json,
	summary_json,
	created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, unixepoch())
ON CONFLICT(receipt_id) DO UPDATE
SET
	project_id = excluded.project_id,
	task_text = excluded.task_text,
	phase = excluded.phase,
	resolved_tags_json = excluded.resolved_tags_json,
	pointer_keys_json = excluded.pointer_keys_json,
	summary_json = excluded.summary_json
`,
		normalized.ReceiptID,
		normalized.ProjectID,
		normalized.TaskText,
		normalized.Phase,
		resolvedTagsJSON,
		pointerKeysJSON,
		string(receiptJSON),
	)
	if err != nil {
		return core.RunReceiptIDs{}, fmt.Errorf("upsert receipt summary: %w", err)
	}

	insertRunResult, err := tx.ExecContext(ctx, `
INSERT INTO awm_runs (
	project_id,
	request_id,
	receipt_id,
	status,
	files_changed_json,
	outcome,
	actor_harness,
	actor_model,
	actor_session,
	summary_json,
	created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, unixepoch())
`,
		normalized.ProjectID,
		normalized.RequestID,
		normalized.ReceiptID,
		normalized.Status,
		filesChangedJSON,
		normalized.Outcome,
		normalized.Actor.Harness,
		normalized.Actor.Model,
		normalized.Actor.SessionID,
		string(runJSON),
	)
	if err != nil {
		return core.RunReceiptIDs{}, fmt.Errorf("insert run summary: %w", err)
	}
	runID, err := insertRunResult.LastInsertId()
	if err != nil {
		return core.RunReceiptIDs{}, fmt.Errorf("read run id: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return core.RunReceiptIDs{}, fmt.Errorf("commit tx: %w", err)
	}
	return core.RunReceiptIDs{
		RunID:     runID,
		ReceiptID: normalized.ReceiptID,
	}, nil
}

// UpsertReceiptScope stores or replaces the scope snapshot (task, phase,
// tags, pointer keys, and paths) recorded for a receipt.
func (r *Repository) UpsertReceiptScope(ctx context.Context, input core.ReceiptScope) error {
	if r == nil || r.db == nil {
		return errors.New("sqlite db is required")
	}

	normalized, err := normalizeReceiptScope(input)
	if err != nil {
		return err
	}

	resolvedTagsJSON, err := encodeStringList(normalized.ResolvedTags)
	if err != nil {
		return fmt.Errorf("encode resolved_tags: %w", err)
	}
	pointerKeysJSON, err := encodeStringList(normalized.PointerKeys)
	if err != nil {
		return fmt.Errorf("encode pointer_keys: %w", err)
	}
	initialScopePathsJSON, err := encodeStringList(normalized.InitialScopePaths)
	if err != nil {
		return fmt.Errorf("encode initial_scope_paths: %w", err)
	}
	baselinePathsJSON, err := encodeSyncPathList(normalized.BaselinePaths)
	if err != nil {
		return fmt.Errorf("encode baseline_paths: %w", err)
	}

	_, err = r.db.ExecContext(ctx, `
INSERT INTO awm_receipts (
	receipt_id,
	project_id,
	task_text,
	phase,
	resolved_tags_json,
	pointer_keys_json,
	initial_scope_paths_json,
	baseline_captured,
	baseline_paths_json,
	actor_harness,
	actor_model,
	actor_session,
	summary_json,
	created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '{}', unixepoch())
ON CONFLICT(receipt_id) DO UPDATE
SET
	project_id = excluded.project_id,
	task_text = excluded.task_text,
	phase = excluded.phase,
	resolved_tags_json = excluded.resolved_tags_json,
	pointer_keys_json = excluded.pointer_keys_json,
	initial_scope_paths_json = excluded.initial_scope_paths_json,
	baseline_captured = excluded.baseline_captured,
	baseline_paths_json = excluded.baseline_paths_json,
	actor_harness = excluded.actor_harness,
	actor_model = excluded.actor_model,
	actor_session = excluded.actor_session
`,
		normalized.ReceiptID,
		normalized.ProjectID,
		normalized.TaskText,
		normalized.Phase,
		resolvedTagsJSON,
		pointerKeysJSON,
		initialScopePathsJSON,
		boolToInt(normalized.BaselineCaptured),
		baselinePathsJSON,
		normalized.Actor.Harness,
		normalized.Actor.Model,
		normalized.Actor.SessionID,
	)
	if err != nil {
		return fmt.Errorf("upsert receipt scope: %w", err)
	}
	return nil
}

// SaveReviewAttempt appends a review attempt for a receipt and returns the
// attempt number assigned to it.
func (r *Repository) SaveReviewAttempt(ctx context.Context, input core.ReviewAttempt) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("sqlite db is required")
	}

	normalized, err := normalizeReviewAttempt(input)
	if err != nil {
		return 0, err
	}

	commandArgvJSON, err := encodeStringList(nonNilStringListPreserveOrder(normalized.CommandArgv))
	if err != nil {
		return 0, fmt.Errorf("encode review attempt command argv: %w", err)
	}

	result, err := r.db.ExecContext(ctx, `
INSERT INTO awm_review_attempts (
	project_id,
	receipt_id,
	plan_key,
	review_key,
	summary,
	fingerprint,
	status,
	passed,
	outcome,
	workflow_source_path,
	command_argv_json,
	command_cwd,
	timeout_sec,
	exit_code,
	timed_out,
	stdout_excerpt,
	stderr_excerpt,
	actor_harness,
	actor_model,
	actor_session,
	created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		normalized.ProjectID,
		normalized.ReceiptID,
		normalized.PlanKey,
		normalized.ReviewKey,
		normalized.Summary,
		normalized.Fingerprint,
		normalized.Status,
		boolToInt(normalized.Passed),
		normalized.Outcome,
		normalized.WorkflowSourcePath,
		commandArgvJSON,
		normalized.CommandCWD,
		normalized.TimeoutSec,
		normalized.ExitCode,
		boolToInt(normalized.TimedOut),
		normalized.StdoutExcerpt,
		normalized.StderrExcerpt,
		normalized.Actor.Harness,
		normalized.Actor.Model,
		normalized.Actor.SessionID,
		normalized.CreatedAt.Unix(),
	)
	if err != nil {
		return 0, fmt.Errorf("insert review attempt: %w", err)
	}
	attemptID, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read review attempt id: %w", err)
	}
	return attemptID, nil
}

// ListReviewAttempts returns the review attempts recorded for a receipt in
// ascending attempt order.
func (r *Repository) ListReviewAttempts(ctx context.Context, input core.ReviewAttemptListQuery) ([]core.ReviewAttempt, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("sqlite db is required")
	}
	if strings.TrimSpace(input.ProjectID) == "" {
		return nil, errors.New("project_id is required")
	}
	if strings.TrimSpace(input.ReceiptID) == "" {
		return nil, errors.New("receipt_id is required")
	}
	if strings.TrimSpace(input.ReviewKey) == "" {
		return nil, errors.New("review_key is required")
	}

	rows, err := r.db.QueryContext(ctx, `
SELECT
	attempt_id,
	project_id,
	receipt_id,
	plan_key,
	review_key,
	summary,
	fingerprint,
	status,
	passed,
	outcome,
	workflow_source_path,
	command_argv_json,
	command_cwd,
	timeout_sec,
	exit_code,
	timed_out,
	stdout_excerpt,
	stderr_excerpt,
	actor_harness,
	actor_model,
	actor_session,
	created_at
FROM awm_review_attempts
WHERE project_id = ?
  AND receipt_id = ?
  AND review_key = ?
ORDER BY created_at ASC, attempt_id ASC
`, strings.TrimSpace(input.ProjectID), strings.TrimSpace(input.ReceiptID), strings.TrimSpace(input.ReviewKey))
	if err != nil {
		return nil, fmt.Errorf("query review attempts: %w", err)
	}
	defer rows.Close()

	out := make([]core.ReviewAttempt, 0)
	for rows.Next() {
		var row core.ReviewAttempt
		var commandArgvJSON string
		var exitCode sql.NullInt64
		var passedInt int
		var timedOutInt int
		var createdAt int64
		if err := rows.Scan(
			&row.AttemptID,
			&row.ProjectID,
			&row.ReceiptID,
			&row.PlanKey,
			&row.ReviewKey,
			&row.Summary,
			&row.Fingerprint,
			&row.Status,
			&passedInt,
			&row.Outcome,
			&row.WorkflowSourcePath,
			&commandArgvJSON,
			&row.CommandCWD,
			&row.TimeoutSec,
			&exitCode,
			&timedOutInt,
			&row.StdoutExcerpt,
			&row.StderrExcerpt,
			&row.Actor.Harness,
			&row.Actor.Model,
			&row.Actor.SessionID,
			&createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan review attempt: %w", err)
		}
		commandArgv, err := decodeStringListPreserveOrder(commandArgvJSON)
		if err != nil {
			return nil, fmt.Errorf("decode review attempt command argv: %w", err)
		}
		row.ProjectID = strings.TrimSpace(row.ProjectID)
		row.ReceiptID = strings.TrimSpace(row.ReceiptID)
		row.PlanKey = strings.TrimSpace(row.PlanKey)
		row.ReviewKey = strings.TrimSpace(row.ReviewKey)
		row.Summary = strings.TrimSpace(row.Summary)
		row.Fingerprint = strings.TrimSpace(row.Fingerprint)
		row.Status = strings.TrimSpace(row.Status)
		row.Passed = passedInt != 0
		row.Outcome = strings.TrimSpace(row.Outcome)
		row.WorkflowSourcePath = strings.TrimSpace(row.WorkflowSourcePath)
		row.CommandArgv = normalizeStringListPreserveOrder(commandArgv)
		row.CommandCWD = strings.TrimSpace(row.CommandCWD)
		if exitCode.Valid {
			code := int(exitCode.Int64)
			row.ExitCode = &code
		}
		row.TimedOut = timedOutInt != 0
		row.StdoutExcerpt = strings.TrimSpace(row.StdoutExcerpt)
		row.StderrExcerpt = strings.TrimSpace(row.StderrExcerpt)
		row.CreatedAt = time.Unix(createdAt, 0).UTC()
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review attempts: %w", err)
	}
	return out, nil
}

// SaveVerificationBatch stores the verification results for a receipt in a
// single transaction, replacing any batch previously saved for it.
func (r *Repository) SaveVerificationBatch(ctx context.Context, input core.VerificationBatch) error {
	if r == nil || r.db == nil {
		return errors.New("sqlite db is required")
	}

	normalized, err := normalizeVerificationBatch(input)
	if err != nil {
		return err
	}

	selectedTestIDsJSON, err := encodeStringList(nonNilStringListPreserveOrder(normalized.SelectedTestIDs))
	if err != nil {
		return fmt.Errorf("encode selected test ids: %w", err)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer rollbackTx(tx)

	_, err = tx.ExecContext(ctx, `
INSERT INTO awm_verification_batches (
	batch_run_id,
	project_id,
	receipt_id,
	plan_key,
	phase,
	tests_source_path,
	status,
	passed,
	selected_test_ids_json,
	actor_harness,
	actor_model,
	actor_session,
	created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		normalized.BatchRunID,
		normalized.ProjectID,
		normalized.ReceiptID,
		normalized.PlanKey,
		normalized.Phase,
		normalized.TestsSourcePath,
		normalized.Status,
		boolToInt(normalized.Passed),
		selectedTestIDsJSON,
		normalized.Actor.Harness,
		normalized.Actor.Model,
		normalized.Actor.SessionID,
		normalized.CreatedAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("insert verification batch: %w", err)
	}

	for _, result := range normalized.Results {
		commandArgvJSON, err := encodeStringList(nonNilStringListPreserveOrder(result.CommandArgv))
		if err != nil {
			return fmt.Errorf("encode verification result command argv %s: %w", result.TestID, err)
		}
		selectionReasonsJSON, err := encodeStringList(nonNilStringListPreserveOrder(result.SelectionReasons))
		if err != nil {
			return fmt.Errorf("encode verification result selection reasons %s: %w", result.TestID, err)
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO awm_verification_results (
	batch_run_id,
	project_id,
	test_id,
	definition_hash,
	summary,
	command_argv_json,
	command_cwd,
	timeout_sec,
	expected_exit_code,
	selection_reasons_json,
	status,
	exit_code,
	duration_ms,
	stdout_excerpt,
	stderr_excerpt,
	started_at,
	finished_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
			normalized.BatchRunID,
			normalized.ProjectID,
			result.TestID,
			result.DefinitionHash,
			result.Summary,
			commandArgvJSON,
			result.CommandCWD,
			result.TimeoutSec,
			result.ExpectedExitCode,
			selectionReasonsJSON,
			result.Status,
			result.ExitCode,
			result.DurationMS,
			result.StdoutExcerpt,
			result.StderrExcerpt,
			result.StartedAt.Unix(),
			result.FinishedAt.Unix(),
		)
		if err != nil {
			return fmt.Errorf("insert verification result %s: %w", result.TestID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// ApplySync reconciles the pointer index with a working-tree snapshot in one
// transaction: deleted paths are marked stale, present paths get refreshed
// content hashes, and (optionally) unindexed paths become new candidates. It
// returns counts of each change applied.
func (r *Repository) ApplySync(ctx context.Context, input core.SyncApplyInput) (core.SyncApplyResult, error) {
	if r == nil || r.db == nil {
		return core.SyncApplyResult{}, errors.New("sqlite db is required")
	}

	normalized, err := normalizeSyncApplyInput(input)
	if err != nil {
		return core.SyncApplyResult{}, err
	}

	deletedPaths := make([]string, 0, len(normalized.Paths))
	presentPaths := make([]string, 0, len(normalized.Paths))
	presentRows := make([]core.SyncPath, 0, len(normalized.Paths))
	for _, syncPath := range normalized.Paths {
		if syncPath.Deleted {
			deletedPaths = append(deletedPaths, syncPath.Path)
			continue
		}
		presentPaths = append(presentPaths, syncPath.Path)
		presentRows = append(presentRows, syncPath)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return core.SyncApplyResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer rollbackTx(tx)

	out := core.SyncApplyResult{}
	if len(deletedPaths) > 0 {
		//nolint:gosec // G202: placeholders() only emits "?" markers; all values are bound parameters
		query := `
UPDATE awm_pointers
SET
	is_stale = 1,
	stale_at = unixepoch(),
	updated_at = unixepoch()
WHERE project_id = ?
	AND is_stale = 0
	AND path IN (` + placeholders(len(deletedPaths)) + `)
`
		args := make([]any, 0, len(deletedPaths)+1)
		args = append(args, normalized.ProjectID)
		for _, p := range deletedPaths {
			args = append(args, p)
		}
		tag, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return core.SyncApplyResult{}, fmt.Errorf("mark deleted pointers stale: %w", err)
		}
		rowsAffected, err := tag.RowsAffected()
		if err != nil {
			return core.SyncApplyResult{}, fmt.Errorf("read deleted stale rows affected: %w", err)
		}
		out.DeletedMarkedStale = int(rowsAffected)
	}

	if normalized.Mode == "full" {
		var (
			query string
			args  []any
		)
		if len(presentPaths) == 0 {
			query = `
UPDATE awm_pointers
SET
	is_stale = 1,
	stale_at = unixepoch(),
	updated_at = unixepoch()
WHERE project_id = ?
	AND is_stale = 0
`
			args = []any{normalized.ProjectID}
		} else {
			query = `
UPDATE awm_pointers
SET
	is_stale = 1,
	stale_at = unixepoch(),
	updated_at = unixepoch()
WHERE project_id = ?
	AND is_stale = 0
	AND path NOT IN (` + placeholders(len(presentPaths)) + `)
`
			args = make([]any, 0, len(presentPaths)+1)
			args = append(args, normalized.ProjectID)
			for _, p := range presentPaths {
				args = append(args, p)
			}
		}
		tag, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return core.SyncApplyResult{}, fmt.Errorf("mark missing pointers stale: %w", err)
		}
		rowsAffected, err := tag.RowsAffected()
		if err != nil {
			return core.SyncApplyResult{}, fmt.Errorf("read missing stale rows affected: %w", err)
		}
		out.MarkedStale = int(rowsAffected)
	}

	for _, row := range presentRows {
		tag, err := tx.ExecContext(ctx, `
UPDATE awm_pointers
SET
	content_hash = ?,
	is_stale = 0,
	stale_at = NULL,
	updated_at = unixepoch()
WHERE project_id = ?
	AND path = ?
	AND (is_stale = 1 OR IFNULL(content_hash, '') <> ?)
`,
			row.ContentHash,
			normalized.ProjectID,
			row.Path,
			row.ContentHash,
		)
		if err != nil {
			return core.SyncApplyResult{}, fmt.Errorf("refresh pointers: %w", err)
		}
		rowsAffected, err := tag.RowsAffected()
		if err != nil {
			return core.SyncApplyResult{}, fmt.Errorf("read refresh pointers rows affected: %w", err)
		}
		out.Updated += int(rowsAffected)
	}

	if normalized.InsertNewCandidates {
		for _, row := range presentRows {
			tag, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO awm_pointer_candidates (
	project_id,
	path,
	content_hash,
	created_at,
	updated_at,
	last_seen_at
) SELECT ?, ?, ?, unixepoch(), unixepoch(), unixepoch()
WHERE NOT EXISTS (
	SELECT 1
	FROM awm_pointers
	WHERE project_id = ?
		AND path = ?
)
`,
				normalized.ProjectID,
				row.Path,
				row.ContentHash,
				normalized.ProjectID,
				row.Path,
			)
			if err != nil {
				return core.SyncApplyResult{}, fmt.Errorf("insert pointer candidates: %w", err)
			}
			rowsAffected, err := tag.RowsAffected()
			if err != nil {
				return core.SyncApplyResult{}, fmt.Errorf("read pointer candidate rows affected: %w", err)
			}
			out.NewCandidates += int(rowsAffected)
		}
	}

	if err := tx.Commit(); err != nil {
		return core.SyncApplyResult{}, fmt.Errorf("commit tx: %w", err)
	}
	return out, nil
}

func normalizePointerStubs(projectID string, stubs []core.PointerStub) ([]core.PointerStub, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, errors.New("project_id is required")
	}
	if len(stubs) == 0 {
		return nil, nil
	}

	stubByKey := make(map[string]core.PointerStub, len(stubs))
	for _, raw := range stubs {
		normalizedPath := normalizeSyncPath(raw.Path)
		if normalizedPath == "" {
			continue
		}

		pointerKey := strings.TrimSpace(raw.PointerKey)
		if pointerKey == "" {
			pointerKey = fmt.Sprintf("%s:%s", projectID, normalizedPath)
		}

		kind := normalizePointerStubKind(raw.Kind)

		label := strings.TrimSpace(raw.Label)
		if label == "" {
			label = strings.TrimSpace(path.Base(normalizedPath))
			if label == "" || label == "." || label == "/" {
				label = normalizedPath
			}
		}

		description := strings.TrimSpace(raw.Description)
		if description == "" {
			description = "Auto-indexed pointer stub. Curate label, description, and tags."
		}

		tags := normalizeStringList(raw.Tags)
		if len(tags) == 0 {
			tags = []string{"auto-indexed", kind}
		}

		stubByKey[pointerKey] = core.PointerStub{
			PointerKey:  pointerKey,
			Path:        normalizedPath,
			Kind:        kind,
			Label:       label,
			Description: description,
			Tags:        tags,
		}
	}

	if len(stubByKey) == 0 {
		return nil, nil
	}

	keys := make([]string, 0, len(stubByKey))
	for key := range stubByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]core.PointerStub, 0, len(keys))
	for _, key := range keys {
		out = append(out, stubByKey[key])
	}
	return out, nil
}

func normalizePointerStubKind(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "rule":
		return "rule"
	case "doc":
		return "doc"
	case "test":
		return "test"
	case "command":
		return "command"
	default:
		return "code"
	}
}

func ensureParentDirectory(dbPath string) error {
	if dbPath == "" || dbPath == ":memory:" {
		return nil
	}
	parent := filepath.Dir(dbPath)
	if parent == "." || parent == "" {
		return nil
	}
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return fmt.Errorf("create sqlite parent directory: %w", err)
	}
	return nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}

func matchesStaleFilter(isStale bool, staleAt *time.Time, filter core.StaleFilter) bool {
	if !filter.AllowStale && isStale {
		return false
	}
	if filter.StaleBefore != nil {
		if !isStale {
			return true
		}
		if staleAt == nil {
			return false
		}
		return !staleAt.After(filter.StaleBefore.UTC())
	}
	return true
}

func encodeStringList(values []string) (string, error) {
	raw, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("marshal string list: %w", err)
	}
	return string(raw), nil
}

func decodeStringList(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(trimmed), &values); err != nil {
		return nil, fmt.Errorf("unmarshal string list: %w", err)
	}
	return normalizeStringList(values), nil
}

func decodeStringListPreserveOrder(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(trimmed), &values); err != nil {
		return nil, fmt.Errorf("unmarshal string list: %w", err)
	}
	return normalizeStringListPreserveOrder(values), nil
}

func encodeSyncPathList(values []core.SyncPath) (string, error) {
	raw, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("marshal sync path list: %w", err)
	}
	return string(raw), nil
}

func decodeSyncPathList(raw string) ([]core.SyncPath, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var values []core.SyncPath
	if err := json.Unmarshal([]byte(trimmed), &values); err != nil {
		return nil, fmt.Errorf("unmarshal sync path list: %w", err)
	}
	return storagedomain.NormalizeSyncPathList(values), nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func unixTime(sec int64) time.Time {
	if sec <= 0 {
		return time.Unix(0, 0).UTC()
	}
	return time.Unix(sec, 0).UTC()
}

func normalizeStringListPreserveOrder(values []string) []string {
	return storagedomain.NormalizeStringListPreserveOrder(values)
}

func normalizeStaleBefore(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}

func nonNilStringList(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return values
}

func nonNilStringListPreserveOrder(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return values
}

func upsertWorkPlanRowTx(ctx context.Context, tx *sql.Tx, plan core.WorkPlan) error {
	inScopeJSON, err := encodeStringList(nonNilStringList(plan.InScope))
	if err != nil {
		return fmt.Errorf("encode plan in_scope: %w", err)
	}
	outOfScopeJSON, err := encodeStringList(nonNilStringList(plan.OutOfScope))
	if err != nil {
		return fmt.Errorf("encode plan out_of_scope: %w", err)
	}
	discoveredPathsJSON, err := encodeStringList(nonNilStringList(plan.DiscoveredPaths))
	if err != nil {
		return fmt.Errorf("encode plan discovered_paths: %w", err)
	}
	constraintsJSON, err := encodeStringList(nonNilStringList(plan.Constraints))
	if err != nil {
		return fmt.Errorf("encode plan constraints: %w", err)
	}
	referencesJSON, err := encodeStringList(nonNilStringList(plan.References))
	if err != nil {
		return fmt.Errorf("encode plan references: %w", err)
	}
	externalRefsJSON, err := encodeStringList(nonNilStringList(plan.ExternalRefs))
	if err != nil {
		return fmt.Errorf("encode plan external refs: %w", err)
	}
	receiptValue := any(nil)
	if strings.TrimSpace(plan.ReceiptID) != "" {
		receiptValue = strings.TrimSpace(plan.ReceiptID)
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO awm_work_plans (
	project_id,
	plan_key,
	receipt_id,
	title,
	objective,
	kind,
	parent_plan_key,
	status,
	stage_spec_outline,
	stage_refined_spec,
	stage_implementation_plan,
	in_scope_json,
	out_of_scope_json,
	discovered_paths_json,
	constraints_json,
	references_json,
	external_refs_json,
	created_at,
	updated_at
) VALUES (
	?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, unixepoch(), unixepoch()
)
ON CONFLICT(project_id, plan_key) DO UPDATE SET
	receipt_id = excluded.receipt_id,
	title = excluded.title,
	objective = excluded.objective,
	kind = excluded.kind,
	parent_plan_key = excluded.parent_plan_key,
	status = excluded.status,
	stage_spec_outline = excluded.stage_spec_outline,
	stage_refined_spec = excluded.stage_refined_spec,
	stage_implementation_plan = excluded.stage_implementation_plan,
	in_scope_json = excluded.in_scope_json,
	out_of_scope_json = excluded.out_of_scope_json,
	discovered_paths_json = excluded.discovered_paths_json,
	constraints_json = excluded.constraints_json,
	references_json = excluded.references_json,
	external_refs_json = excluded.external_refs_json,
	updated_at = unixepoch()
`, strings.TrimSpace(plan.ProjectID), strings.TrimSpace(plan.PlanKey), receiptValue, strings.TrimSpace(plan.Title), strings.TrimSpace(plan.Objective), strings.ToLower(strings.TrimSpace(plan.Kind)), strings.TrimSpace(plan.ParentPlanKey), storageWorkItemStatus(plan.Status), storageWorkItemStatus(plan.Stages.SpecOutline), storageWorkItemStatus(plan.Stages.RefinedSpec), storageWorkItemStatus(plan.Stages.ImplementationPlan), inScopeJSON, outOfScopeJSON, discoveredPathsJSON, constraintsJSON, referencesJSON, externalRefsJSON); err != nil {
		return fmt.Errorf("upsert work plan: %w", err)
	}
	return nil
}

func lookupWorkPlanTx(ctx context.Context, tx *sql.Tx, projectID, planKey string) (core.WorkPlan, bool, error) {
	return lookupWorkPlan(ctx, tx, projectID, planKey)
}

type sqlRowsQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func lookupWorkPlan(ctx context.Context, q sqlRowsQuerier, projectID, planKey string) (core.WorkPlan, bool, error) {
	var (
		receiptID           sql.NullString
		title               string
		objective           string
		kind                string
		parentPlanKey       string
		status              string
		stageSpecOutline    string
		stageRefinedSpec    string
		stageImplementation string
		inScopeJSON         string
		outOfScopeJSON      string
		discoveredPathsJSON string
		constraintsJSON     string
		referencesJSON      string
		externalRefsJSON    string
		updatedAt           int64
	)
	if err := q.QueryRowContext(ctx, `
SELECT
	receipt_id,
	title,
	objective,
	kind,
	parent_plan_key,
	status,
	stage_spec_outline,
	stage_refined_spec,
	stage_implementation_plan,
	in_scope_json,
	out_of_scope_json,
	discovered_paths_json,
	constraints_json,
	references_json,
	external_refs_json,
	updated_at
FROM awm_work_plans
WHERE project_id = ?
	AND plan_key = ?
`, projectID, planKey).Scan(&receiptID, &title, &objective, &kind, &parentPlanKey, &status, &stageSpecOutline, &stageRefinedSpec, &stageImplementation, &inScopeJSON, &outOfScopeJSON, &discoveredPathsJSON, &constraintsJSON, &referencesJSON, &externalRefsJSON, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.WorkPlan{}, false, nil
		}
		return core.WorkPlan{}, false, fmt.Errorf("query work plan: %w", err)
	}
	inScope, err := decodeStringList(inScopeJSON)
	if err != nil {
		return core.WorkPlan{}, false, fmt.Errorf("decode plan in_scope: %w", err)
	}
	outOfScope, err := decodeStringList(outOfScopeJSON)
	if err != nil {
		return core.WorkPlan{}, false, fmt.Errorf("decode plan out_of_scope: %w", err)
	}
	discoveredPaths, err := decodeStringList(discoveredPathsJSON)
	if err != nil {
		return core.WorkPlan{}, false, fmt.Errorf("decode plan discovered_paths: %w", err)
	}
	constraints, err := decodeStringList(constraintsJSON)
	if err != nil {
		return core.WorkPlan{}, false, fmt.Errorf("decode plan constraints: %w", err)
	}
	references, err := decodeStringList(referencesJSON)
	if err != nil {
		return core.WorkPlan{}, false, fmt.Errorf("decode plan references: %w", err)
	}
	externalRefs, err := decodeStringList(externalRefsJSON)
	if err != nil {
		return core.WorkPlan{}, false, fmt.Errorf("decode plan external refs: %w", err)
	}

	return core.WorkPlan{
		ProjectID:       projectID,
		PlanKey:         planKey,
		ReceiptID:       strings.TrimSpace(receiptID.String),
		Title:           strings.TrimSpace(title),
		Objective:       strings.TrimSpace(objective),
		Kind:            strings.TrimSpace(kind),
		ParentPlanKey:   strings.TrimSpace(parentPlanKey),
		Status:          normalizeWorkItemStatus(status),
		Stages:          normalizeWorkPlanStages(core.WorkPlanStages{SpecOutline: stageSpecOutline, RefinedSpec: stageRefinedSpec, ImplementationPlan: stageImplementation}),
		InScope:         normalizeStringList(inScope),
		OutOfScope:      normalizeStringList(outOfScope),
		DiscoveredPaths: normalizeStringList(discoveredPaths),
		Constraints:     normalizeStringList(constraints),
		References:      normalizeStringList(references),
		ExternalRefs:    normalizeStringList(externalRefs),
		UpdatedAt:       unixTime(updatedAt),
	}, true, nil
}

func listWorkPlanTasksTx(ctx context.Context, tx *sql.Tx, projectID, planKey string) ([]core.WorkItem, error) {
	return listWorkPlanTasks(ctx, tx, projectID, planKey)
}

func listWorkPlanTasks(ctx context.Context, q sqlRowsQuerier, projectID, planKey string) ([]core.WorkItem, error) {
	rows, err := q.QueryContext(ctx, `
SELECT
	task_key,
	summary,
	status,
	parent_task_key,
	depends_on_json,
	acceptance_criteria_json,
	references_json,
	external_refs_json,
	blocked_reason,
	outcome,
	evidence_json,
	updated_at
FROM awm_work_plan_tasks
WHERE project_id = ?
	AND plan_key = ?
ORDER BY task_key ASC
`, projectID, planKey)
	if err != nil {
		return nil, fmt.Errorf("query work plan tasks: %w", err)
	}
	defer rows.Close()

	items := make([]core.WorkItem, 0)
	for rows.Next() {
		var (
			itemKey          string
			summary          string
			status           string
			parentTaskKey    string
			dependsOnJSON    string
			criteriaJSON     string
			referencesJSON   string
			externalRefsJSON string
			blockedReason    string
			outcome          string
			evidenceJSON     string
			updatedAt        int64
		)
		if err := rows.Scan(&itemKey, &summary, &status, &parentTaskKey, &dependsOnJSON, &criteriaJSON, &referencesJSON, &externalRefsJSON, &blockedReason, &outcome, &evidenceJSON, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan work plan task: %w", err)
		}
		itemKey = strings.TrimSpace(itemKey)
		if itemKey == "" {
			continue
		}
		dependsOn, err := decodeStringList(dependsOnJSON)
		if err != nil {
			return nil, fmt.Errorf("decode task depends_on: %w", err)
		}
		acceptanceCriteria, err := decodeStringList(criteriaJSON)
		if err != nil {
			return nil, fmt.Errorf("decode task acceptance criteria: %w", err)
		}
		references, err := decodeStringList(referencesJSON)
		if err != nil {
			return nil, fmt.Errorf("decode task references: %w", err)
		}
		externalRefs, err := decodeStringList(externalRefsJSON)
		if err != nil {
			return nil, fmt.Errorf("decode task external refs: %w", err)
		}
		evidence, err := decodeStringList(evidenceJSON)
		if err != nil {
			return nil, fmt.Errorf("decode task evidence: %w", err)
		}
		items = append(items, core.WorkItem{
			ItemKey:            itemKey,
			Summary:            strings.TrimSpace(summary),
			Status:             normalizeWorkItemStatus(status),
			ParentTaskKey:      strings.TrimSpace(parentTaskKey),
			DependsOn:          normalizeStringList(dependsOn),
			AcceptanceCriteria: normalizeStringList(acceptanceCriteria),
			References:         normalizeStringList(references),
			ExternalRefs:       normalizeStringList(externalRefs),
			BlockedReason:      strings.TrimSpace(blockedReason),
			Outcome:            strings.TrimSpace(outcome),
			Evidence:           normalizeStringList(evidence),
			UpdatedAt:          unixTime(updatedAt),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate work plan tasks: %w", err)
	}
	return items, nil
}

func workPlanSummaryKeys(rows []core.WorkPlanSummary) []string {
	if len(rows) == 0 {
		return nil
	}

	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		planKey := strings.TrimSpace(row.PlanKey)
		if planKey == "" {
			continue
		}
		keys = append(keys, planKey)
	}
	return keys
}
