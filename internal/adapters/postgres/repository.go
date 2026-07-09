package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bonztm/agent-workflow-manager/internal/core"
	storagedomain "github.com/bonztm/agent-workflow-manager/internal/storage/domain"
)

// Repository is the Postgres-backed implementation of the core repository
// interfaces. It owns a pgx connection pool and is safe for concurrent use.
type Repository struct {
	pool *pgxpool.Pool
}

var (
	_ core.Repository             = (*Repository)(nil)
	_ core.WorkPlanRepository     = (*Repository)(nil)
	_ core.HistoryRepository      = (*Repository)(nil)
	_ core.VerificationRepository = (*Repository)(nil)
)

// New connects to Postgres using cfg, verifies the connection with a ping,
// and applies any pending schema migrations. The returned Repository owns the
// pool; callers must Close it when done.
func New(ctx context.Context, cfg Config) (*Repository, error) {
	poolCfg, err := cfg.PoolConfig()
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	if pingErr := pool.Ping(ctx); pingErr != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", pingErr)
	}

	repo, err := NewWithPool(pool)
	if err != nil {
		pool.Close()
		return nil, err
	}
	if err := repo.Migrate(ctx); err != nil {
		repo.Close()
		return nil, fmt.Errorf("apply postgres migrations: %w", err)
	}

	return repo, nil
}

// NewWithPool wraps an existing pgx pool in a Repository without running
// migrations. It errors when pool is nil; the caller retains ownership of
// the pool's lifetime until Close is called.
func NewWithPool(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("postgres pool is required")
	}
	return &Repository{pool: pool}, nil
}

// rollbackTx releases tx if it is still open. It is intended for use in defer
// statements as best-effort cleanup: after a successful Commit, Rollback
// reports pgx.ErrTxClosed, and any other failure occurs on a path that is
// already returning an error, so there is nothing further to handle.
func rollbackTx(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx) //nolint:errcheck // best-effort rollback in defer; returns pgx.ErrTxClosed after a successful commit
}

// Close releases the underlying connection pool. It is a no-op on a nil or
// pool-less repository.
func (r *Repository) Close() {
	if r == nil || r.pool == nil {
		return
	}
	r.pool.Close()
}

// Migrate applies any pending embedded schema migrations to the connected
// database.
func (r *Repository) Migrate(ctx context.Context) error {
	if r == nil || r.pool == nil {
		return errors.New("postgres pool is required")
	}
	return ApplyMigrations(ctx, r.pool)
}

// FetchCandidatePointers returns the project's pointers that match the
// query's tag and staleness filters, ranked and truncated per the query
// limit. It requires a non-empty project ID.
func (r *Repository) FetchCandidatePointers(ctx context.Context, input core.CandidatePointerQuery) ([]core.CandidatePointer, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("postgres pool is required")
	}

	input.StaleFilter.StaleBefore = normalizeStaleBefore(input.StaleFilter.StaleBefore)

	query, args, err := buildCandidatePointersQuery(input)
	if err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query candidate pointers: %w", err)
	}
	defer rows.Close()

	results := make([]core.CandidatePointer, 0)
	for rows.Next() {
		var row struct {
			Key         string
			Path        string
			Anchor      string
			Kind        string
			Label       string
			Description string
			Tags        []string
			IsRule      bool
			IsStale     bool
			UpdatedAt   time.Time
		}
		if err := rows.Scan(
			&row.Key,
			&row.Path,
			&row.Anchor,
			&row.Kind,
			&row.Label,
			&row.Description,
			&row.Tags,
			&row.IsRule,
			&row.IsStale,
			&row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan candidate pointer: %w", err)
		}

		results = append(results, core.CandidatePointer{
			Key:         row.Key,
			Path:        row.Path,
			Anchor:      row.Anchor,
			Kind:        row.Kind,
			Label:       row.Label,
			Description: row.Description,
			Tags:        row.Tags,
			IsRule:      row.IsRule,
			IsStale:     row.IsStale,
			UpdatedAt:   row.UpdatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate candidate pointers: %w", err)
	}
	return storagedomain.SortAndLimitCandidatePointers(results, input, defaultCandidateLimit), nil
}

// ListPointerInventory returns every indexed pointer for the project,
// including stale entries, for inventory-style reporting.
func (r *Repository) ListPointerInventory(ctx context.Context, projectID string) ([]core.PointerInventory, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("postgres pool is required")
	}

	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, errors.New("project_id is required")
	}

	rows, err := r.pool.Query(ctx, `
SELECT
	path,
	BOOL_OR(is_stale) AS is_stale
FROM awm_pointers
WHERE project_id = $1
GROUP BY path
ORDER BY path ASC
`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query pointer inventory: %w", err)
	}
	defer rows.Close()

	results := make([]core.PointerInventory, 0)
	for rows.Next() {
		var row core.PointerInventory
		if err := rows.Scan(&row.Path, &row.IsStale); err != nil {
			return nil, fmt.Errorf("scan pointer inventory: %w", err)
		}
		row.Path = normalizeSyncPath(row.Path)
		if row.Path == "" {
			continue
		}
		results = append(results, row)
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
	if r == nil || r.pool == nil {
		return 0, errors.New("postgres pool is required")
	}

	normalized, err := normalizePointerStubs(projectID, stubs)
	if err != nil {
		return 0, err
	}
	if len(normalized) == 0 {
		return 0, nil
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer rollbackTx(ctx, tx)

	updated := 0
	for _, stub := range normalized {
		isRule := strings.EqualFold(stub.Kind, "rule")
		tag, execErr := tx.Exec(ctx, `
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
	is_stale,
	stale_at,
	content_hash,
	updated_at
)
VALUES (
	$1,
	$2,
	$3,
	'',
	$4,
	$5,
	$6,
	$7,
	$8,
	FALSE,
	NULL,
	NULL,
	NOW()
)
ON CONFLICT (project_id, pointer_key) DO UPDATE SET
	path = EXCLUDED.path,
	anchor = EXCLUDED.anchor,
	is_stale = FALSE,
	stale_at = NULL,
	updated_at = NOW()
`,
			strings.TrimSpace(projectID),
			stub.PointerKey,
			stub.Path,
			stub.Kind,
			stub.Label,
			stub.Description,
			nonNilStringList(stub.Tags),
			isRule,
		)
		if execErr != nil {
			return 0, fmt.Errorf("upsert pointer stubs: %w", execErr)
		}
		updated += int(tag.RowsAffected())
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit tx: %w", err)
	}
	return updated, nil
}

// FetchReceiptScope loads the stored scope for a receipt. It returns
// core.ErrReceiptScopeNotFound when no receipt matches the query.
func (r *Repository) FetchReceiptScope(ctx context.Context, input core.ReceiptScopeQuery) (core.ReceiptScope, error) {
	if r == nil || r.pool == nil {
		return core.ReceiptScope{}, errors.New("postgres pool is required")
	}

	query, args, err := buildFetchReceiptScopeQuery(input)
	if err != nil {
		return core.ReceiptScope{}, err
	}

	var row struct {
		ReceiptID         string
		TaskText          string
		Phase             string
		ResolvedTags      []string
		PointerKeys       []string
		InitialScopePaths []string
		BaselineCaptured  bool
		BaselinePathsJSON []byte
	}
	if scanErr := r.pool.QueryRow(ctx, query, args...).Scan(
		&row.ReceiptID,
		&row.TaskText,
		&row.Phase,
		&row.ResolvedTags,
		&row.PointerKeys,
		&row.InitialScopePaths,
		&row.BaselineCaptured,
		&row.BaselinePathsJSON,
	); scanErr != nil {
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return core.ReceiptScope{}, core.ErrReceiptScopeNotFound
		}
		return core.ReceiptScope{}, fmt.Errorf("query receipt scope: %w", scanErr)
	}

	baselinePaths, err := decodeSyncPathsJSON(row.BaselinePathsJSON)
	if err != nil {
		return core.ReceiptScope{}, fmt.Errorf("decode baseline_paths: %w", err)
	}

	return core.ReceiptScope{
		ProjectID:         strings.TrimSpace(input.ProjectID),
		ReceiptID:         row.ReceiptID,
		TaskText:          strings.TrimSpace(row.TaskText),
		Phase:             strings.TrimSpace(row.Phase),
		ResolvedTags:      normalizeStringList(row.ResolvedTags),
		PointerKeys:       normalizeStringList(row.PointerKeys),
		InitialScopePaths: normalizeStringList(row.InitialScopePaths),
		BaselineCaptured:  row.BaselineCaptured,
		BaselinePaths:     baselinePaths,
	}, nil
}

// LookupFetchState resolves a receipt (by ID or latest for the project) into
// its fetch-time state. It returns core.ErrFetchLookupNotFound when the
// receipt does not exist.
func (r *Repository) LookupFetchState(ctx context.Context, input core.FetchLookupQuery) (core.FetchLookup, error) {
	if r == nil || r.pool == nil {
		return core.FetchLookup{}, errors.New("postgres pool is required")
	}

	query, args, err := buildLookupFetchStateQuery(input)
	if err != nil {
		return core.FetchLookup{}, err
	}

	var row struct {
		ReceiptID string
		RunID     int64
		RunStatus string
		UpdatedAt time.Time
	}
	if scanErr := r.pool.QueryRow(ctx, query, args...).Scan(
		&row.ReceiptID,
		&row.RunID,
		&row.RunStatus,
		&row.UpdatedAt,
	); scanErr != nil {
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return core.FetchLookup{}, core.ErrFetchLookupNotFound
		}
		return core.FetchLookup{}, fmt.Errorf("query fetch lookup: %w", scanErr)
	}

	workItems, err := r.ListWorkItems(ctx, input)
	if err != nil {
		return core.FetchLookup{}, err
	}

	return core.FetchLookup{
		ProjectID:  strings.TrimSpace(input.ProjectID),
		ReceiptID:  row.ReceiptID,
		RunID:      row.RunID,
		RunStatus:  strings.TrimSpace(row.RunStatus),
		PlanStatus: derivePlanStatus(workItems),
		WorkItems:  workItems,
		UpdatedAt:  row.UpdatedAt.UTC(),
	}, nil
}

// LookupPointerByKey fetches a single pointer by its pointer key. It returns
// core.ErrPointerLookupNotFound when the key is not indexed for the project.
func (r *Repository) LookupPointerByKey(ctx context.Context, input core.PointerLookupQuery) (core.CandidatePointer, error) {
	if r == nil || r.pool == nil {
		return core.CandidatePointer{}, errors.New("postgres pool is required")
	}

	query, args, err := buildLookupPointerByKeyQuery(input)
	if err != nil {
		return core.CandidatePointer{}, err
	}

	var row struct {
		Key         string
		Path        string
		Anchor      string
		Kind        string
		Label       string
		Description string
		Tags        []string
		IsRule      bool
		IsStale     bool
		UpdatedAt   time.Time
	}
	if err := r.pool.QueryRow(ctx, query, args...).Scan(
		&row.Key,
		&row.Path,
		&row.Anchor,
		&row.Kind,
		&row.Label,
		&row.Description,
		&row.Tags,
		&row.IsRule,
		&row.IsStale,
		&row.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return core.CandidatePointer{}, core.ErrPointerLookupNotFound
		}
		return core.CandidatePointer{}, fmt.Errorf("query pointer lookup: %w", err)
	}

	return core.CandidatePointer{
		Key:         strings.TrimSpace(row.Key),
		Path:        normalizeSyncPath(row.Path),
		Anchor:      strings.TrimSpace(row.Anchor),
		Kind:        strings.TrimSpace(row.Kind),
		Label:       strings.TrimSpace(row.Label),
		Description: strings.TrimSpace(row.Description),
		Tags:        normalizeStringList(row.Tags),
		IsRule:      row.IsRule,
		IsStale:     row.IsStale,
		UpdatedAt:   row.UpdatedAt.UTC(),
	}, nil
}

// UpsertWorkItems inserts or updates the receipt's work items in a single
// transaction and reports how many rows changed.
func (r *Repository) UpsertWorkItems(ctx context.Context, input core.WorkItemsUpsertInput) (int, error) {
	if r == nil || r.pool == nil {
		return 0, errors.New("postgres pool is required")
	}

	query, args, err := buildUpsertWorkItemsQuery(input)
	if err != nil {
		return 0, err
	}

	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("upsert work items: %w", err)
	}

	return int(tag.RowsAffected()), nil
}

// ListWorkItems returns the work items recorded for the receipt resolved by
// the query, in stable item-key order.
func (r *Repository) ListWorkItems(ctx context.Context, input core.FetchLookupQuery) ([]core.WorkItem, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("postgres pool is required")
	}

	query, args, err := buildListWorkItemsQuery(input)
	if err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query work items: %w", err)
	}
	defer rows.Close()

	items := make([]core.WorkItem, 0)
	for rows.Next() {
		var item core.WorkItem
		if err := rows.Scan(&item.ItemKey, &item.Status, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan work item: %w", err)
		}
		item.ItemKey = normalizeSyncPath(item.ItemKey)
		item.Status = normalizeWorkItemStatus(item.Status)
		item.UpdatedAt = item.UpdatedAt.UTC()
		if item.ItemKey == "" {
			continue
		}
		items = append(items, item)
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
	if r == nil || r.pool == nil {
		return core.WorkPlanUpsertResult{}, errors.New("postgres pool is required")
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

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return core.WorkPlanUpsertResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer rollbackTx(ctx, tx)

	current, found, err := lookupWorkPlanRowTx(ctx, tx, projectID, planKey)
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
		tag, delErr := tx.Exec(ctx, `
DELETE FROM awm_work_plan_tasks
WHERE project_id = $1
	AND plan_key = $2
`, projectID, planKey)
		if delErr != nil {
			return core.WorkPlanUpsertResult{}, fmt.Errorf("delete work plan tasks: %w", delErr)
		}
		updated += int(tag.RowsAffected())
	}

	for _, task := range normalizedTasks {
		tag, taskErr := tx.Exec(ctx, `
INSERT INTO awm_work_plan_tasks (
	project_id,
	plan_key,
	task_key,
	summary,
	status,
	parent_task_key,
	depends_on,
	acceptance_criteria,
	references_list,
	external_refs,
	blocked_reason,
	outcome,
	evidence,
	created_at,
	updated_at
) VALUES (
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW(), NOW()
)
ON CONFLICT(project_id, plan_key, task_key) DO UPDATE SET
	summary = EXCLUDED.summary,
	status = EXCLUDED.status,
	parent_task_key = EXCLUDED.parent_task_key,
	depends_on = EXCLUDED.depends_on,
	acceptance_criteria = EXCLUDED.acceptance_criteria,
	references_list = EXCLUDED.references_list,
	external_refs = EXCLUDED.external_refs,
	blocked_reason = EXCLUDED.blocked_reason,
	outcome = EXCLUDED.outcome,
	evidence = EXCLUDED.evidence,
	updated_at = NOW()
`, projectID, planKey, task.ItemKey, strings.TrimSpace(task.Summary), storageWorkItemStatus(task.Status), strings.TrimSpace(task.ParentTaskKey), nonNilStringList(task.DependsOn), nonNilStringList(task.AcceptanceCriteria), nonNilStringList(task.References), nonNilStringList(task.ExternalRefs), strings.TrimSpace(task.BlockedReason), strings.TrimSpace(task.Outcome), nonNilStringList(task.Evidence))
		if taskErr != nil {
			return core.WorkPlanUpsertResult{}, fmt.Errorf("upsert work plan task: %w", taskErr)
		}
		updated += int(tag.RowsAffected())
	}

	if strings.TrimSpace(input.Status) == "" {
		tasks, listErr := listWorkPlanTasksTx(ctx, tx, projectID, planKey)
		if listErr != nil {
			return core.WorkPlanUpsertResult{}, listErr
		}
		derivedStatus := derivePlanStatus(tasks)
		if _, updateErr := tx.Exec(ctx, `
UPDATE awm_work_plans
SET status = $3, updated_at = NOW()
WHERE project_id = $1
	AND plan_key = $2
`, projectID, planKey, storageWorkItemStatus(derivedStatus)); updateErr != nil {
			return core.WorkPlanUpsertResult{}, fmt.Errorf("update work plan status: %w", updateErr)
		}
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
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
	if r == nil || r.pool == nil {
		return core.WorkPlan{}, errors.New("postgres pool is required")
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
		err := r.pool.QueryRow(ctx, `
SELECT plan_key
FROM awm_work_plans
WHERE project_id = $1
	AND receipt_id = $2
ORDER BY updated_at DESC, plan_key ASC
LIMIT 1
`, projectID, receiptID).Scan(&planKey)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return core.WorkPlan{}, core.ErrWorkPlanNotFound
			}
			return core.WorkPlan{}, fmt.Errorf("query work plan key by receipt: %w", err)
		}
	}

	plan, found, err := lookupWorkPlanRow(ctx, r.pool, projectID, planKey)
	if err != nil {
		return core.WorkPlan{}, err
	}
	if !found {
		return core.WorkPlan{}, core.ErrWorkPlanNotFound
	}

	tasks, err := listWorkPlanTasks(ctx, r.pool, projectID, planKey)
	if err != nil {
		return core.WorkPlan{}, err
	}
	plan.Tasks = tasks
	return plan, nil
}

// ListWorkPlans returns plan summaries (with task counts and active task
// keys) for the project, filtered by scope and optional search text.
func (r *Repository) ListWorkPlans(ctx context.Context, input core.WorkPlanListQuery) ([]core.WorkPlanSummary, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("postgres pool is required")
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

	var (
		query strings.Builder
		args  []any
	)
	query.WriteString(`
SELECT
	p.plan_key,
	p.receipt_id,
	p.title,
	p.objective,
	p.status,
	p.kind,
	p.parent_plan_key,
	COUNT(t.task_id)::bigint AS task_count_total,
	COUNT(*) FILTER (WHERE t.status = 'pending')::bigint AS task_count_pending,
	COUNT(*) FILTER (WHERE t.status = 'in_progress')::bigint AS task_count_in_progress,
	COUNT(*) FILTER (WHERE t.status = 'blocked')::bigint AS task_count_blocked,
	COUNT(*) FILTER (WHERE t.status = 'complete')::bigint AS task_count_completed,
	p.updated_at
FROM awm_work_plans p
LEFT JOIN awm_work_plan_tasks t
	ON t.project_id = p.project_id
	AND t.plan_key = p.plan_key
WHERE p.project_id = $1
`)
	args = append(args, projectID)
	argIndex := 2

	switch scope {
	case workPlanListScopeCurrent:
		query.WriteString("  AND p.status IN ('pending', 'in_progress')\n")
	case workPlanListScopeDeferred:
		query.WriteString("  AND p.status = 'blocked'\n")
	case workPlanListScopeCompleted:
		query.WriteString("  AND p.status IN ('complete', 'superseded')\n")
	}

	if trimmedKind := strings.TrimSpace(input.Kind); trimmedKind != "" {
		fmt.Fprintf(&query, "  AND p.kind = $%d\n", argIndex)
		args = append(args, trimmedKind)
		argIndex++
	}

	if searchPattern != "" {
		fmt.Fprintf(&query, `  AND (
	LOWER(p.plan_key) LIKE $%d ESCAPE '\'
	OR LOWER(COALESCE(p.receipt_id, '')) LIKE $%d ESCAPE '\'
	OR LOWER(COALESCE(p.title, '')) LIKE $%d ESCAPE '\'
	OR LOWER(COALESCE(p.objective, '')) LIKE $%d ESCAPE '\'
	OR LOWER(COALESCE(p.kind, '')) LIKE $%d ESCAPE '\'
	OR LOWER(COALESCE(p.parent_plan_key, '')) LIKE $%d ESCAPE '\'
	OR EXISTS (
		SELECT 1
		FROM awm_work_plan_tasks wt
		WHERE wt.project_id = p.project_id
			AND wt.plan_key = p.plan_key
			AND (
				LOWER(wt.task_key) LIKE $%d ESCAPE '\'
				OR LOWER(COALESCE(wt.summary, '')) LIKE $%d ESCAPE '\'
				OR LOWER(COALESCE(wt.outcome, '')) LIKE $%d ESCAPE '\'
				OR LOWER(COALESCE(wt.blocked_reason, '')) LIKE $%d ESCAPE '\'
			)
	)
)
`, argIndex, argIndex+1, argIndex+2, argIndex+3, argIndex+4, argIndex+5, argIndex+6, argIndex+7, argIndex+8, argIndex+9)
		for range 10 {
			args = append(args, searchPattern)
		}
		argIndex += 10
	}

	query.WriteString(`
GROUP BY p.plan_key, p.receipt_id, p.title, p.objective, p.status, p.kind, p.parent_plan_key, p.updated_at
ORDER BY p.updated_at DESC, p.plan_key ASC
`)
	if !input.Unbounded {
		fmt.Fprintf(&query, "LIMIT $%d\n", argIndex)
		args = append(args, limit)
	}

	rows, err := r.pool.Query(ctx, query.String(), args...)
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
			updatedAt           time.Time
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
			UpdatedAt:           updatedAt.UTC(),
		})
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterate work plans: %w", rowsErr)
	}

	if len(out) == 0 {
		return nil, nil
	}

	activeTaskKeysByPlan, err := listActiveWorkPlanTaskKeys(ctx, r.pool, projectID, workPlanSummaryKeys(out))
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
	if r == nil || r.pool == nil {
		return nil, errors.New("postgres pool is required")
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

	var (
		query strings.Builder
		args  []any
	)
	query.WriteString(`
SELECT
	r.receipt_id,
	r.task_text,
	r.phase,
	COALESCE(run.request_id, '') AS latest_request_id,
	COALESCE(run.status, '') AS latest_status,
	COALESCE(run.created_at, r.created_at) AS updated_at
FROM awm_receipts r
LEFT JOIN LATERAL (
	SELECT run_id, request_id, status, created_at
	FROM awm_runs
	WHERE project_id = r.project_id
		AND receipt_id = r.receipt_id
	ORDER BY created_at DESC, run_id DESC
	LIMIT 1
) run ON TRUE
WHERE r.project_id = $1
`)
	args = append(args, projectID)
	argIndex := 2

	if searchPattern != "" {
		fmt.Fprintf(&query, `  AND (
	LOWER(r.receipt_id) LIKE $%d ESCAPE '\'
	OR LOWER(COALESCE(r.task_text, '')) LIKE $%d ESCAPE '\'
	OR LOWER(COALESCE(r.phase, '')) LIKE $%d ESCAPE '\'
	OR EXISTS (
		SELECT 1
		FROM awm_runs rr
		WHERE rr.project_id = r.project_id
			AND rr.receipt_id = r.receipt_id
			AND (
				LOWER(COALESCE(rr.request_id, '')) LIKE $%d ESCAPE '\'
				OR LOWER(COALESCE(rr.status, '')) LIKE $%d ESCAPE '\'
				OR LOWER(COALESCE(rr.outcome, '')) LIKE $%d ESCAPE '\'
				OR LOWER(COALESCE(array_to_string(rr.files_changed, ' '), '')) LIKE $%d ESCAPE '\'
				OR LOWER(COALESCE(rr.summary_json::text, '')) LIKE $%d ESCAPE '\'
			)
	)
)
`, argIndex, argIndex+1, argIndex+2, argIndex+3, argIndex+4, argIndex+5, argIndex+6, argIndex+7)
		for range 8 {
			args = append(args, searchPattern)
		}
		argIndex += 8
	}

	query.WriteString(`
ORDER BY updated_at DESC, r.receipt_id ASC
`)
	if !input.Unbounded {
		fmt.Fprintf(&query, "LIMIT $%d\n", argIndex)
		args = append(args, limit)
	}

	rows, err := r.pool.Query(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query receipt history: %w", err)
	}
	defer rows.Close()

	out := make([]core.ReceiptHistorySummary, 0)
	for rows.Next() {
		var row core.ReceiptHistorySummary
		if err := rows.Scan(
			&row.ReceiptID,
			&row.TaskText,
			&row.Phase,
			&row.LatestRequestID,
			&row.LatestStatus,
			&row.UpdatedAt,
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
	if r == nil || r.pool == nil {
		return nil, errors.New("postgres pool is required")
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

	var (
		query strings.Builder
		args  []any
	)
	query.WriteString(`
SELECT
	run.run_id,
	run.receipt_id,
	run.request_id,
	COALESCE(r.task_text, '') AS task_text,
	COALESCE(r.phase, '') AS phase,
	run.status,
	run.files_changed,
	run.outcome,
	run.created_at
FROM awm_runs run
LEFT JOIN awm_receipts r
	ON r.project_id = run.project_id
	AND r.receipt_id = run.receipt_id
WHERE run.project_id = $1
`)
	args = append(args, projectID)
	argIndex := 2

	if searchPattern != "" {
		fmt.Fprintf(&query, `  AND (
	LOWER(COALESCE(run.request_id, '')) LIKE $%d ESCAPE '\'
	OR LOWER(COALESCE(run.status, '')) LIKE $%d ESCAPE '\'
	OR LOWER(COALESCE(run.outcome, '')) LIKE $%d ESCAPE '\'
	OR LOWER(COALESCE(array_to_string(run.files_changed, ' '), '')) LIKE $%d ESCAPE '\'
	OR LOWER(COALESCE(run.summary_json::text, '')) LIKE $%d ESCAPE '\'
	OR LOWER(COALESCE(r.task_text, '')) LIKE $%d ESCAPE '\'
	OR LOWER(COALESCE(r.phase, '')) LIKE $%d ESCAPE '\'
	OR LOWER(COALESCE(run.receipt_id, '')) LIKE $%d ESCAPE '\'
)
`, argIndex, argIndex+1, argIndex+2, argIndex+3, argIndex+4, argIndex+5, argIndex+6, argIndex+7)
		for range 8 {
			args = append(args, searchPattern)
		}
		argIndex += 8
	}

	query.WriteString(`
ORDER BY run.created_at DESC, run.run_id DESC
`)
	if !input.Unbounded {
		fmt.Fprintf(&query, "LIMIT $%d\n", argIndex)
		args = append(args, limit)
	}

	rows, err := r.pool.Query(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query run history: %w", err)
	}
	defer rows.Close()

	out := make([]core.RunHistorySummary, 0)
	for rows.Next() {
		var row core.RunHistorySummary
		if err := rows.Scan(
			&row.RunID,
			&row.ReceiptID,
			&row.RequestID,
			&row.TaskText,
			&row.Phase,
			&row.Status,
			&row.FilesChanged,
			&row.Outcome,
			&row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan run history: %w", err)
		}
		row.ReceiptID = strings.TrimSpace(row.ReceiptID)
		row.RequestID = strings.TrimSpace(row.RequestID)
		row.TaskText = strings.TrimSpace(row.TaskText)
		row.Phase = strings.TrimSpace(row.Phase)
		row.Status = strings.TrimSpace(row.Status)
		row.Outcome = strings.TrimSpace(row.Outcome)
		row.FilesChanged = normalizeStringList(row.FilesChanged)
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
	if r == nil || r.pool == nil {
		return core.RunHistorySummary{}, errors.New("postgres pool is required")
	}
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return core.RunHistorySummary{}, errors.New("project_id is required")
	}
	if input.RunID <= 0 {
		return core.RunHistorySummary{}, errors.New("run_id must be positive")
	}

	var row core.RunHistorySummary
	err := r.pool.QueryRow(ctx, `
SELECT
	run.run_id,
	run.receipt_id,
	run.request_id,
	COALESCE(r.task_text, '') AS task_text,
	COALESCE(r.phase, '') AS phase,
	run.status,
	run.files_changed,
	run.outcome,
	run.created_at
FROM awm_runs run
LEFT JOIN awm_receipts r
	ON r.project_id = run.project_id
	AND r.receipt_id = run.receipt_id
WHERE run.project_id = $1
	AND run.run_id = $2
`, projectID, input.RunID).Scan(
		&row.RunID,
		&row.ReceiptID,
		&row.RequestID,
		&row.TaskText,
		&row.Phase,
		&row.Status,
		&row.FilesChanged,
		&row.Outcome,
		&row.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return core.RunHistorySummary{}, core.ErrFetchLookupNotFound
		}
		return core.RunHistorySummary{}, fmt.Errorf("query run history lookup: %w", err)
	}
	row.ReceiptID = strings.TrimSpace(row.ReceiptID)
	row.RequestID = strings.TrimSpace(row.RequestID)
	row.TaskText = strings.TrimSpace(row.TaskText)
	row.Phase = strings.TrimSpace(row.Phase)
	row.Status = strings.TrimSpace(row.Status)
	row.Outcome = strings.TrimSpace(row.Outcome)
	row.FilesChanged = normalizeStringList(row.FilesChanged)
	return row, nil
}

func listActiveWorkPlanTaskKeys(ctx context.Context, q pgxRowsQuerier, projectID string, planKeys []string) (map[string][]string, error) {
	if len(planKeys) == 0 {
		return nil, nil
	}

	rows, err := q.Query(ctx, `
SELECT
	plan_key,
	task_key
FROM awm_work_plan_tasks
WHERE project_id = $1
	AND plan_key = ANY($2)
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
`, projectID, planKeys)
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
	if r == nil || r.pool == nil {
		return core.RunReceiptIDs{}, errors.New("postgres pool is required")
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

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return core.RunReceiptIDs{}, fmt.Errorf("begin tx: %w", err)
	}
	defer rollbackTx(ctx, tx)

	_, err = tx.Exec(ctx, `
INSERT INTO awm_receipts (
	receipt_id,
	project_id,
	task_text,
	phase,
	resolved_tags,
	pointer_keys,
	summary_json
) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
ON CONFLICT (receipt_id) DO UPDATE
SET
	project_id = EXCLUDED.project_id,
	task_text = EXCLUDED.task_text,
	phase = EXCLUDED.phase,
	resolved_tags = EXCLUDED.resolved_tags,
	pointer_keys = EXCLUDED.pointer_keys,
	summary_json = EXCLUDED.summary_json
`, normalized.ReceiptID, normalized.ProjectID, normalized.TaskText, normalized.Phase, nonNilStringList(normalized.ResolvedTags), nonNilStringList(normalized.PointerKeys), receiptJSON)
	if err != nil {
		return core.RunReceiptIDs{}, fmt.Errorf("upsert receipt summary: %w", err)
	}

	var runID int64
	err = tx.QueryRow(ctx, `
INSERT INTO awm_runs (
	project_id,
	request_id,
	receipt_id,
	status,
	files_changed,
	outcome,
	summary_json
) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
RETURNING run_id
`, normalized.ProjectID, normalized.RequestID, normalized.ReceiptID, normalized.Status, nonNilStringList(normalized.FilesChanged), normalized.Outcome, runJSON).Scan(&runID)
	if err != nil {
		return core.RunReceiptIDs{}, fmt.Errorf("insert run summary: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
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
	if r == nil || r.pool == nil {
		return errors.New("postgres pool is required")
	}

	normalized, err := normalizeReceiptScope(input)
	if err != nil {
		return err
	}

	baselinePathsJSON, err := encodeSyncPathsJSON(normalized.BaselinePaths)
	if err != nil {
		return fmt.Errorf("encode baseline_paths: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
INSERT INTO awm_receipts (
	receipt_id,
	project_id,
	task_text,
	phase,
	resolved_tags,
	pointer_keys,
	initial_scope_paths,
	baseline_captured,
	baseline_paths_json,
	summary_json
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, '{}'::jsonb)
ON CONFLICT (receipt_id) DO UPDATE
SET
	project_id = EXCLUDED.project_id,
	task_text = EXCLUDED.task_text,
	phase = EXCLUDED.phase,
	resolved_tags = EXCLUDED.resolved_tags,
	pointer_keys = EXCLUDED.pointer_keys,
	initial_scope_paths = EXCLUDED.initial_scope_paths,
	baseline_captured = EXCLUDED.baseline_captured,
	baseline_paths_json = EXCLUDED.baseline_paths_json
`, normalized.ReceiptID, normalized.ProjectID, normalized.TaskText, normalized.Phase, nonNilStringList(normalized.ResolvedTags), nonNilStringList(normalized.PointerKeys), nonNilStringList(normalized.InitialScopePaths), normalized.BaselineCaptured, baselinePathsJSON)
	if err != nil {
		return fmt.Errorf("upsert receipt scope: %w", err)
	}
	return nil
}

// SaveReviewAttempt appends a review attempt for a receipt and returns the
// attempt number assigned to it.
func (r *Repository) SaveReviewAttempt(ctx context.Context, input core.ReviewAttempt) (int64, error) {
	if r == nil || r.pool == nil {
		return 0, errors.New("postgres pool is required")
	}

	normalized, err := normalizeReviewAttempt(input)
	if err != nil {
		return 0, err
	}

	var attemptID int64
	err = r.pool.QueryRow(ctx, `
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
	command_argv,
	command_cwd,
	timeout_sec,
	exit_code,
	timed_out,
	stdout_excerpt,
	stderr_excerpt,
	created_at
) VALUES (
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
)
RETURNING attempt_id
`, normalized.ProjectID, normalized.ReceiptID, normalized.PlanKey, normalized.ReviewKey, normalized.Summary, normalized.Fingerprint, normalized.Status, normalized.Passed, normalized.Outcome, normalized.WorkflowSourcePath, nonNilStringListPreserveOrder(normalized.CommandArgv), normalized.CommandCWD, normalized.TimeoutSec, normalized.ExitCode, normalized.TimedOut, normalized.StdoutExcerpt, normalized.StderrExcerpt, normalized.CreatedAt).Scan(&attemptID)
	if err != nil {
		return 0, fmt.Errorf("insert review attempt: %w", err)
	}
	return attemptID, nil
}

// ListReviewAttempts returns the review attempts recorded for a receipt in
// ascending attempt order.
func (r *Repository) ListReviewAttempts(ctx context.Context, input core.ReviewAttemptListQuery) ([]core.ReviewAttempt, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("postgres pool is required")
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

	rows, err := r.pool.Query(ctx, `
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
	command_argv,
	command_cwd,
	timeout_sec,
	exit_code,
	timed_out,
	stdout_excerpt,
	stderr_excerpt,
	created_at
FROM awm_review_attempts
WHERE project_id = $1
  AND receipt_id = $2
  AND review_key = $3
ORDER BY created_at ASC, attempt_id ASC
`, strings.TrimSpace(input.ProjectID), strings.TrimSpace(input.ReceiptID), strings.TrimSpace(input.ReviewKey))
	if err != nil {
		return nil, fmt.Errorf("query review attempts: %w", err)
	}
	defer rows.Close()

	out := make([]core.ReviewAttempt, 0)
	for rows.Next() {
		var row core.ReviewAttempt
		var commandArgv []string
		var exitCode *int
		if err := rows.Scan(
			&row.AttemptID,
			&row.ProjectID,
			&row.ReceiptID,
			&row.PlanKey,
			&row.ReviewKey,
			&row.Summary,
			&row.Fingerprint,
			&row.Status,
			&row.Passed,
			&row.Outcome,
			&row.WorkflowSourcePath,
			&commandArgv,
			&row.CommandCWD,
			&row.TimeoutSec,
			&exitCode,
			&row.TimedOut,
			&row.StdoutExcerpt,
			&row.StderrExcerpt,
			&row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan review attempt: %w", err)
		}
		row.ProjectID = strings.TrimSpace(row.ProjectID)
		row.ReceiptID = strings.TrimSpace(row.ReceiptID)
		row.PlanKey = strings.TrimSpace(row.PlanKey)
		row.ReviewKey = strings.TrimSpace(row.ReviewKey)
		row.Summary = strings.TrimSpace(row.Summary)
		row.Fingerprint = strings.TrimSpace(row.Fingerprint)
		row.Status = strings.TrimSpace(row.Status)
		row.Outcome = strings.TrimSpace(row.Outcome)
		row.WorkflowSourcePath = strings.TrimSpace(row.WorkflowSourcePath)
		row.CommandArgv = normalizeStringListPreserveOrder(commandArgv)
		row.CommandCWD = strings.TrimSpace(row.CommandCWD)
		row.ExitCode = exitCode
		row.StdoutExcerpt = strings.TrimSpace(row.StdoutExcerpt)
		row.StderrExcerpt = strings.TrimSpace(row.StderrExcerpt)
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
	if r == nil || r.pool == nil {
		return errors.New("postgres pool is required")
	}

	normalized, err := normalizeVerificationBatch(input)
	if err != nil {
		return err
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer rollbackTx(ctx, tx)

	_, err = tx.Exec(ctx, `
INSERT INTO awm_verification_batches (
	batch_run_id,
	project_id,
	receipt_id,
	plan_key,
	phase,
	tests_source_path,
	status,
	passed,
	selected_test_ids,
	created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
`, normalized.BatchRunID, normalized.ProjectID, normalized.ReceiptID, normalized.PlanKey, normalized.Phase, normalized.TestsSourcePath, normalized.Status, normalized.Passed, nonNilStringListPreserveOrder(normalized.SelectedTestIDs), normalized.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert verification batch: %w", err)
	}

	for _, result := range normalized.Results {
		_, err := tx.Exec(ctx, `
INSERT INTO awm_verification_results (
	batch_run_id,
	project_id,
	test_id,
	definition_hash,
	summary,
	command_argv,
	command_cwd,
	timeout_sec,
	expected_exit_code,
	selection_reasons,
	status,
	exit_code,
	duration_ms,
	stdout_excerpt,
	stderr_excerpt,
	started_at,
	finished_at
) VALUES (
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17
)
`, normalized.BatchRunID, normalized.ProjectID, result.TestID, result.DefinitionHash, result.Summary, nonNilStringListPreserveOrder(result.CommandArgv), result.CommandCWD, result.TimeoutSec, result.ExpectedExitCode, nonNilStringListPreserveOrder(result.SelectionReasons), result.Status, result.ExitCode, result.DurationMS, result.StdoutExcerpt, result.StderrExcerpt, result.StartedAt, result.FinishedAt)
		if err != nil {
			return fmt.Errorf("insert verification result %s: %w", result.TestID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// ApplySync reconciles the pointer index with a working-tree snapshot in one
// transaction: deleted paths are marked stale, present paths get refreshed
// content hashes, and (optionally) unindexed paths become new candidates. It
// returns counts of each change applied.
func (r *Repository) ApplySync(ctx context.Context, input core.SyncApplyInput) (core.SyncApplyResult, error) {
	if r == nil || r.pool == nil {
		return core.SyncApplyResult{}, errors.New("postgres pool is required")
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

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return core.SyncApplyResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer rollbackTx(ctx, tx)

	result := core.SyncApplyResult{}

	if len(deletedPaths) > 0 {
		query, args, err := buildMarkDeletedPointersStaleQuery(normalized.ProjectID, deletedPaths)
		if err != nil {
			return core.SyncApplyResult{}, err
		}
		tag, err := tx.Exec(ctx, query, args...)
		if err != nil {
			return core.SyncApplyResult{}, fmt.Errorf("mark deleted pointers stale: %w", err)
		}
		result.DeletedMarkedStale = int(tag.RowsAffected())
	}

	if normalized.Mode == "full" {
		query, args, err := buildMarkMissingPointersStaleQuery(normalized.ProjectID, presentPaths)
		if err != nil {
			return core.SyncApplyResult{}, err
		}
		tag, err := tx.Exec(ctx, query, args...)
		if err != nil {
			return core.SyncApplyResult{}, fmt.Errorf("mark missing pointers stale: %w", err)
		}
		result.MarkedStale = int(tag.RowsAffected())
	}

	if len(presentRows) > 0 {
		query, args, err := buildRefreshPointersQuery(normalized.ProjectID, presentRows)
		if err != nil {
			return core.SyncApplyResult{}, err
		}
		tag, err := tx.Exec(ctx, query, args...)
		if err != nil {
			return core.SyncApplyResult{}, fmt.Errorf("refresh pointers: %w", err)
		}
		result.Updated = int(tag.RowsAffected())

		if normalized.InsertNewCandidates {
			query, args, err = buildInsertPointerCandidatesQuery(normalized.ProjectID, presentRows)
			if err != nil {
				return core.SyncApplyResult{}, err
			}
			tag, err = tx.Exec(ctx, query, args...)
			if err != nil {
				return core.SyncApplyResult{}, fmt.Errorf("insert pointer candidates: %w", err)
			}
			result.NewCandidates = int(tag.RowsAffected())
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return core.SyncApplyResult{}, fmt.Errorf("commit tx: %w", err)
	}

	return result, nil
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

func upsertWorkPlanRowTx(ctx context.Context, tx pgx.Tx, plan core.WorkPlan) error {
	receiptValue := any(nil)
	if strings.TrimSpace(plan.ReceiptID) != "" {
		receiptValue = strings.TrimSpace(plan.ReceiptID)
	}
	_, err := tx.Exec(ctx, `
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
	in_scope,
	out_of_scope,
	discovered_paths,
	constraints_list,
	references_list,
	external_refs,
	created_at,
	updated_at
) VALUES (
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, NOW(), NOW()
)
ON CONFLICT(project_id, plan_key) DO UPDATE SET
	receipt_id = EXCLUDED.receipt_id,
	title = EXCLUDED.title,
	objective = EXCLUDED.objective,
	kind = EXCLUDED.kind,
	parent_plan_key = EXCLUDED.parent_plan_key,
	status = EXCLUDED.status,
	stage_spec_outline = EXCLUDED.stage_spec_outline,
	stage_refined_spec = EXCLUDED.stage_refined_spec,
	stage_implementation_plan = EXCLUDED.stage_implementation_plan,
	in_scope = EXCLUDED.in_scope,
	out_of_scope = EXCLUDED.out_of_scope,
	discovered_paths = EXCLUDED.discovered_paths,
	constraints_list = EXCLUDED.constraints_list,
	references_list = EXCLUDED.references_list,
	external_refs = EXCLUDED.external_refs,
	updated_at = NOW()
`, strings.TrimSpace(plan.ProjectID), strings.TrimSpace(plan.PlanKey), receiptValue, strings.TrimSpace(plan.Title), strings.TrimSpace(plan.Objective), strings.ToLower(strings.TrimSpace(plan.Kind)), strings.TrimSpace(plan.ParentPlanKey), storageWorkItemStatus(plan.Status), storageWorkItemStatus(plan.Stages.SpecOutline), storageWorkItemStatus(plan.Stages.RefinedSpec), storageWorkItemStatus(plan.Stages.ImplementationPlan), nonNilStringList(plan.InScope), nonNilStringList(plan.OutOfScope), nonNilStringList(plan.DiscoveredPaths), nonNilStringList(plan.Constraints), nonNilStringList(plan.References), nonNilStringList(plan.ExternalRefs))
	if err != nil {
		return fmt.Errorf("upsert work plan: %w", err)
	}
	return nil
}

func lookupWorkPlanRowTx(ctx context.Context, tx pgx.Tx, projectID, planKey string) (core.WorkPlan, bool, error) {
	row := tx.QueryRow(ctx, `
SELECT
	COALESCE(receipt_id, ''),
	title,
	objective,
	kind,
	parent_plan_key,
	status,
	stage_spec_outline,
	stage_refined_spec,
	stage_implementation_plan,
	in_scope,
	out_of_scope,
	discovered_paths,
	constraints_list,
	references_list,
	external_refs,
	updated_at
FROM awm_work_plans
WHERE project_id = $1
	AND plan_key = $2
`, projectID, planKey)

	var (
		receiptID           string
		title               string
		objective           string
		kind                string
		parentPlanKey       string
		status              string
		stageSpecOutline    string
		stageRefinedSpec    string
		stageImplementation string
		inScope             []string
		outOfScope          []string
		discoveredPaths     []string
		constraints         []string
		references          []string
		externalRefs        []string
		updatedAt           time.Time
	)
	if err := row.Scan(&receiptID, &title, &objective, &kind, &parentPlanKey, &status, &stageSpecOutline, &stageRefinedSpec, &stageImplementation, &inScope, &outOfScope, &discoveredPaths, &constraints, &references, &externalRefs, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return core.WorkPlan{}, false, nil
		}
		return core.WorkPlan{}, false, fmt.Errorf("query work plan: %w", err)
	}

	return core.WorkPlan{
		ProjectID:       projectID,
		PlanKey:         planKey,
		ReceiptID:       strings.TrimSpace(receiptID),
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
		UpdatedAt:       updatedAt.UTC(),
	}, true, nil
}

func listWorkPlanTasksTx(ctx context.Context, tx pgx.Tx, projectID, planKey string) ([]core.WorkItem, error) {
	rows, err := tx.Query(ctx, `
SELECT
	task_key,
	summary,
	status,
	parent_task_key,
	depends_on,
	acceptance_criteria,
	references_list,
	external_refs,
	blocked_reason,
	outcome,
	evidence,
	updated_at
FROM awm_work_plan_tasks
WHERE project_id = $1
	AND plan_key = $2
ORDER BY task_key ASC
`, projectID, planKey)
	if err != nil {
		return nil, fmt.Errorf("query work plan tasks: %w", err)
	}
	defer rows.Close()

	items := make([]core.WorkItem, 0)
	for rows.Next() {
		var item core.WorkItem
		if err := rows.Scan(&item.ItemKey, &item.Summary, &item.Status, &item.ParentTaskKey, &item.DependsOn, &item.AcceptanceCriteria, &item.References, &item.ExternalRefs, &item.BlockedReason, &item.Outcome, &item.Evidence, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan work plan task: %w", err)
		}
		item.ItemKey = strings.TrimSpace(item.ItemKey)
		if item.ItemKey == "" {
			continue
		}
		item.Summary = strings.TrimSpace(item.Summary)
		item.Status = normalizeWorkItemStatus(item.Status)
		item.ParentTaskKey = strings.TrimSpace(item.ParentTaskKey)
		item.DependsOn = normalizeStringList(item.DependsOn)
		item.AcceptanceCriteria = normalizeStringList(item.AcceptanceCriteria)
		item.References = normalizeStringList(item.References)
		item.ExternalRefs = normalizeStringList(item.ExternalRefs)
		item.BlockedReason = strings.TrimSpace(item.BlockedReason)
		item.Outcome = strings.TrimSpace(item.Outcome)
		item.Evidence = normalizeStringList(item.Evidence)
		item.UpdatedAt = item.UpdatedAt.UTC()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate work plan tasks: %w", err)
	}
	return items, nil
}

func lookupWorkPlanRow(ctx context.Context, q pgxRowsQuerier, projectID, planKey string) (core.WorkPlan, bool, error) {
	row := q.QueryRow(ctx, `
SELECT
	COALESCE(receipt_id, ''),
	title,
	objective,
	kind,
	parent_plan_key,
	status,
	stage_spec_outline,
	stage_refined_spec,
	stage_implementation_plan,
	in_scope,
	out_of_scope,
	discovered_paths,
	constraints_list,
	references_list,
	external_refs,
	updated_at
FROM awm_work_plans
WHERE project_id = $1
	AND plan_key = $2
`, projectID, planKey)

	var (
		receiptID           string
		title               string
		objective           string
		kind                string
		parentPlanKey       string
		status              string
		stageSpecOutline    string
		stageRefinedSpec    string
		stageImplementation string
		inScope             []string
		outOfScope          []string
		discoveredPaths     []string
		constraints         []string
		references          []string
		externalRefs        []string
		updatedAt           time.Time
	)
	if err := row.Scan(&receiptID, &title, &objective, &kind, &parentPlanKey, &status, &stageSpecOutline, &stageRefinedSpec, &stageImplementation, &inScope, &outOfScope, &discoveredPaths, &constraints, &references, &externalRefs, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return core.WorkPlan{}, false, nil
		}
		return core.WorkPlan{}, false, fmt.Errorf("query work plan: %w", err)
	}

	return core.WorkPlan{
		ProjectID:       projectID,
		PlanKey:         planKey,
		ReceiptID:       strings.TrimSpace(receiptID),
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
		UpdatedAt:       updatedAt.UTC(),
	}, true, nil
}

type pgxRowsQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func listWorkPlanTasks(ctx context.Context, q pgxRowsQuerier, projectID, planKey string) ([]core.WorkItem, error) {
	rows, err := q.Query(ctx, `
SELECT
	task_key,
	summary,
	status,
	parent_task_key,
	depends_on,
	acceptance_criteria,
	references_list,
	external_refs,
	blocked_reason,
	outcome,
	evidence,
	updated_at
FROM awm_work_plan_tasks
WHERE project_id = $1
	AND plan_key = $2
ORDER BY task_key ASC
`, projectID, planKey)
	if err != nil {
		return nil, fmt.Errorf("query work plan tasks: %w", err)
	}
	defer rows.Close()

	items := make([]core.WorkItem, 0)
	for rows.Next() {
		var item core.WorkItem
		if err := rows.Scan(&item.ItemKey, &item.Summary, &item.Status, &item.ParentTaskKey, &item.DependsOn, &item.AcceptanceCriteria, &item.References, &item.ExternalRefs, &item.BlockedReason, &item.Outcome, &item.Evidence, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan work plan task: %w", err)
		}
		item.ItemKey = strings.TrimSpace(item.ItemKey)
		if item.ItemKey == "" {
			continue
		}
		item.Summary = strings.TrimSpace(item.Summary)
		item.Status = normalizeWorkItemStatus(item.Status)
		item.ParentTaskKey = strings.TrimSpace(item.ParentTaskKey)
		item.DependsOn = normalizeStringList(item.DependsOn)
		item.AcceptanceCriteria = normalizeStringList(item.AcceptanceCriteria)
		item.References = normalizeStringList(item.References)
		item.ExternalRefs = normalizeStringList(item.ExternalRefs)
		item.BlockedReason = strings.TrimSpace(item.BlockedReason)
		item.Outcome = strings.TrimSpace(item.Outcome)
		item.Evidence = normalizeStringList(item.Evidence)
		item.UpdatedAt = item.UpdatedAt.UTC()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate work plan tasks: %w", err)
	}
	return items, nil
}

func normalizeStringListPreserveOrder(values []string) []string {
	return storagedomain.NormalizeStringListPreserveOrder(values)
}

func nonNilStringListPreserveOrder(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return values
}

func encodeSyncPathsJSON(values []core.SyncPath) ([]byte, error) {
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("marshal sync path list: %w", err)
	}
	return raw, nil
}

func decodeSyncPathsJSON(raw []byte) ([]core.SyncPath, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var values []core.SyncPath
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("unmarshal sync path list: %w", err)
	}
	return storagedomain.NormalizeSyncPathList(values), nil
}

func newReceiptID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate receipt id: %w", err)
	}
	return fmt.Sprintf("receipt-%d-%s", time.Now().UTC().UnixNano(), hex.EncodeToString(b[:])), nil
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
