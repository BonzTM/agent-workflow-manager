package postgres

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/core"
	storagedomain "github.com/bonztm/agent-workflow-manager/internal/storage/domain"
)

const defaultCandidateLimit = 32

type sqlArgs struct {
	values []any
}

func (a *sqlArgs) add(v any) string {
	a.values = append(a.values, v)
	return fmt.Sprintf("$%d", len(a.values))
}

func buildCandidatePointersQuery(input core.CandidatePointerQuery) (string, []any, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return "", nil, errors.New("project_id is required")
	}

	args := &sqlArgs{}
	projectIDArg := args.add(projectID)

	predicates := make([]string, 0, 2)
	if !input.StaleFilter.AllowStale {
		predicates = append(predicates, "p.is_stale = FALSE")
	}
	if input.StaleFilter.StaleBefore != nil {
		staleBeforeArg := args.add(input.StaleFilter.StaleBefore.UTC())
		predicates = append(predicates, fmt.Sprintf("(p.is_stale = FALSE OR (p.stale_at IS NOT NULL AND p.stale_at <= %s))", staleBeforeArg))
	}

	var sb strings.Builder
	sb.WriteString(`
SELECT
	p.pointer_key,
	p.path,
	p.anchor,
	p.kind,
	p.label,
	p.description,
	p.tags,
	p.is_rule,
	p.is_stale,
	p.updated_at
FROM awm_pointers p
WHERE p.project_id = `)
	sb.WriteString(projectIDArg)

	if len(predicates) > 0 {
		sb.WriteString(" AND ")
		sb.WriteString(strings.Join(predicates, " AND "))
	}

	sb.WriteString(`
ORDER BY p.pointer_key ASC`)

	return sb.String(), args.values, nil
}

func buildFetchReceiptScopeQuery(input core.ReceiptScopeQuery) (string, []any, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return "", nil, errors.New("project_id is required")
	}
	receiptID := strings.TrimSpace(input.ReceiptID)
	if receiptID == "" {
		return "", nil, errors.New("receipt_id is required")
	}

	return `
SELECT
	r.receipt_id,
	r.task_text,
	r.phase,
	r.resolved_tags,
	r.pointer_keys,
	r.initial_scope_paths,
	r.baseline_captured,
	r.baseline_paths_json
FROM awm_receipts r
WHERE r.project_id = $1
	AND r.receipt_id = $2
`, []any{projectID, receiptID}, nil
}

func buildLookupFetchStateQuery(input core.FetchLookupQuery) (string, []any, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return "", nil, errors.New("project_id is required")
	}
	receiptID := strings.TrimSpace(input.ReceiptID)
	if receiptID == "" {
		return "", nil, errors.New("receipt_id is required")
	}

	return `
SELECT
	r.receipt_id,
	COALESCE(run.run_id, 0) AS run_id,
	COALESCE(run.status, '') AS run_status,
	COALESCE(run.created_at, r.created_at) AS updated_at
FROM awm_receipts r
LEFT JOIN LATERAL (
	SELECT run_id, status, created_at
	FROM awm_runs
	WHERE project_id = r.project_id
		AND receipt_id = r.receipt_id
	ORDER BY created_at DESC, run_id DESC
	LIMIT 1
) run ON TRUE
WHERE r.project_id = $1
	AND r.receipt_id = $2
`, []any{projectID, receiptID}, nil
}

func buildLookupPointerByKeyQuery(input core.PointerLookupQuery) (string, []any, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return "", nil, errors.New("project_id is required")
	}
	pointerKey := strings.TrimSpace(input.PointerKey)
	if pointerKey == "" {
		return "", nil, errors.New("pointer_key is required")
	}

	return `
SELECT
	pointer_key,
	path,
	anchor,
	kind,
	label,
	description,
	tags,
	is_rule,
	is_stale,
	updated_at
FROM awm_pointers
WHERE project_id = $1
	AND pointer_key = $2
	AND is_stale = FALSE
`, []any{projectID, pointerKey}, nil
}

func buildListWorkItemsQuery(input core.FetchLookupQuery) (string, []any, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return "", nil, errors.New("project_id is required")
	}
	receiptID := strings.TrimSpace(input.ReceiptID)
	if receiptID == "" {
		return "", nil, errors.New("receipt_id is required")
	}

	return `
SELECT
	item_key,
	status,
	updated_at
FROM awm_work_items
WHERE project_id = $1
	AND receipt_id = $2
ORDER BY item_key ASC
`, []any{projectID, receiptID}, nil
}

func buildUpsertWorkItemsQuery(input core.WorkItemsUpsertInput) (string, []any, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return "", nil, errors.New("project_id is required")
	}
	receiptID := strings.TrimSpace(input.ReceiptID)
	if receiptID == "" {
		return "", nil, errors.New("receipt_id is required")
	}

	items, err := normalizeWorkItems(input.Items)
	if err != nil {
		return "", nil, err
	}
	if len(items) == 0 {
		return "", nil, errors.New("work items are required")
	}

	args := &sqlArgs{}
	projectIDArg := args.add(projectID)
	receiptIDArg := args.add(receiptID)

	valuesRows := make([]string, 0, len(items))
	for _, item := range items {
		itemKeyArg := args.add(item.ItemKey)
		statusArg := args.add(storageWorkItemStatus(item.Status))
		valuesRows = append(valuesRows, fmt.Sprintf("(%s, %s)", itemKeyArg, statusArg))
	}

	query := `
WITH incoming(item_key, status) AS (
	VALUES ` + strings.Join(valuesRows, ", ") + `
)
INSERT INTO awm_work_items (
	project_id,
	receipt_id,
	item_key,
	status,
	created_at,
	updated_at
)
SELECT
	` + projectIDArg + `,
	` + receiptIDArg + `,
	i.item_key,
	i.status,
	NOW(),
	NOW()
FROM incoming i
ON CONFLICT (project_id, receipt_id, item_key) DO UPDATE
SET
	status = EXCLUDED.status,
	updated_at = NOW()
`

	return query, args.values, nil
}

func buildMarkDeletedPointersStaleQuery(projectID string, deletedPaths []string) (string, []any, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "", nil, errors.New("project_id is required")
	}
	normalizedPaths := normalizeSyncPaths(deletedPaths)
	if len(normalizedPaths) == 0 {
		return "", nil, errors.New("deleted paths are required")
	}

	return `
UPDATE awm_pointers
SET
	is_stale = TRUE,
	stale_at = NOW(),
	updated_at = NOW()
WHERE project_id = $1
	AND path = ANY($2::text[])
	AND is_stale = FALSE
`, []any{projectID, normalizedPaths}, nil
}

func buildMarkMissingPointersStaleQuery(projectID string, presentPaths []string) (string, []any, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "", nil, errors.New("project_id is required")
	}
	normalizedPaths := normalizeSyncPaths(presentPaths)
	if len(normalizedPaths) == 0 {
		return `
UPDATE awm_pointers
SET
	is_stale = TRUE,
	stale_at = NOW(),
	updated_at = NOW()
WHERE project_id = $1
	AND is_stale = FALSE
`, []any{projectID}, nil
	}

	return `
UPDATE awm_pointers
SET
	is_stale = TRUE,
	stale_at = NOW(),
	updated_at = NOW()
WHERE project_id = $1
	AND is_stale = FALSE
	AND NOT (path = ANY($2::text[]))
`, []any{projectID, normalizedPaths}, nil
}

func buildRefreshPointersQuery(projectID string, paths []core.SyncPath) (string, []any, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "", nil, errors.New("project_id is required")
	}
	normalizedPaths, err := normalizeSyncPathRows(paths, true)
	if err != nil {
		return "", nil, err
	}
	if len(normalizedPaths) == 0 {
		return "", nil, errors.New("paths are required")
	}

	args := &sqlArgs{}
	projectIDArg := args.add(projectID)
	valuesRows := make([]string, 0, len(normalizedPaths))
	for _, p := range normalizedPaths {
		pathArg := args.add(p.Path)
		hashArg := args.add(p.ContentHash)
		valuesRows = append(valuesRows, fmt.Sprintf("(%s, %s)", pathArg, hashArg))
	}

	query := `
WITH sync(path, content_hash) AS (
	VALUES ` + strings.Join(valuesRows, ", ") + `
)
UPDATE awm_pointers p
SET
	content_hash = sync.content_hash,
	is_stale = FALSE,
	stale_at = NULL,
	updated_at = NOW()
FROM sync
WHERE p.project_id = ` + projectIDArg + `
	AND p.path = sync.path
	AND (p.is_stale = TRUE OR p.content_hash IS DISTINCT FROM sync.content_hash)
`
	return query, args.values, nil
}

func buildInsertPointerCandidatesQuery(projectID string, paths []core.SyncPath) (string, []any, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "", nil, errors.New("project_id is required")
	}
	normalizedPaths, err := normalizeSyncPathRows(paths, true)
	if err != nil {
		return "", nil, err
	}
	if len(normalizedPaths) == 0 {
		return "", nil, errors.New("paths are required")
	}

	args := &sqlArgs{}
	projectIDArg := args.add(projectID)
	valuesRows := make([]string, 0, len(normalizedPaths))
	for _, p := range normalizedPaths {
		pathArg := args.add(p.Path)
		hashArg := args.add(p.ContentHash)
		valuesRows = append(valuesRows, fmt.Sprintf("(%s, %s)", pathArg, hashArg))
	}

	query := `
WITH sync(path, content_hash) AS (
	VALUES ` + strings.Join(valuesRows, ", ") + `
),
missing AS (
	SELECT s.path, s.content_hash
	FROM sync s
	LEFT JOIN awm_pointers p
		ON p.project_id = ` + projectIDArg + `
		AND p.path = s.path
	WHERE p.pointer_id IS NULL
)
INSERT INTO awm_pointer_candidates (
	project_id,
	path,
	content_hash
)
SELECT
	` + projectIDArg + `,
	m.path,
	m.content_hash
FROM missing m
ORDER BY m.path ASC
ON CONFLICT (project_id, path) DO NOTHING
`
	return query, args.values, nil
}

func normalizeStringList(values []string) []string {
	return storagedomain.NormalizeStringList(values)
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

func normalizeSyncPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		normalized := normalizeSyncPath(raw)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

func normalizeSyncPathRows(paths []core.SyncPath, requireHash bool) ([]core.SyncPath, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	byPath := make(map[string]core.SyncPath, len(paths))
	for _, raw := range paths {
		normalizedPath := normalizeSyncPath(raw.Path)
		if normalizedPath == "" {
			return nil, errors.New("path is required")
		}
		if raw.Deleted {
			continue
		}

		contentHash := strings.TrimSpace(raw.ContentHash)
		if requireHash && contentHash == "" {
			return nil, fmt.Errorf("content_hash is required for path %q", normalizedPath)
		}

		current := byPath[normalizedPath]
		if current.Path == "" || current.ContentHash == "" {
			byPath[normalizedPath] = core.SyncPath{
				Path:        normalizedPath,
				ContentHash: contentHash,
			}
		}
	}

	if len(byPath) == 0 {
		return nil, nil
	}

	keys := make([]string, 0, len(byPath))
	for path := range byPath {
		keys = append(keys, path)
	}
	sort.Strings(keys)

	out := make([]core.SyncPath, 0, len(keys))
	for _, key := range keys {
		out = append(out, byPath[key])
	}
	return out, nil
}

func normalizeSyncPath(raw string) string {
	return storagedomain.NormalizeRepoPath(raw)
}
