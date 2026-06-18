package sqlite

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func (r *Repository) SyncRulePointers(ctx context.Context, input core.RulePointerSyncInput) (core.RulePointerSyncResult, error) {
	if r == nil || r.db == nil {
		return core.RulePointerSyncResult{}, fmt.Errorf("sqlite db is required")
	}

	normalized, err := normalizeRulePointerSyncInput(input)
	if err != nil {
		return core.RulePointerSyncResult{}, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return core.RulePointerSyncResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result := core.RulePointerSyncResult{}
	for _, pointer := range normalized.Pointers {
		tagsJSON, encodeErr := encodeStringList(nonNilStringList(pointer.Tags))
		if encodeErr != nil {
			return core.RulePointerSyncResult{}, fmt.Errorf("encode rule pointer tags: %w", encodeErr)
		}
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
VALUES (?, ?, ?, '', 'rule', ?, ?, ?, 1, 0, NULL, NULL, unixepoch())
ON CONFLICT(project_id, pointer_key) DO UPDATE SET
	path = excluded.path,
	anchor = excluded.anchor,
	kind = excluded.kind,
	label = excluded.label,
	description = excluded.description,
	tags_json = excluded.tags_json,
	is_rule = 1,
	is_stale = 0,
	stale_at = NULL,
	content_hash = NULL,
	updated_at = unixepoch()
`,
			normalized.ProjectID,
			pointer.PointerKey,
			normalized.SourcePath,
			pointer.Summary,
			pointer.Content,
			tagsJSON,
		)
		if execErr != nil {
			return core.RulePointerSyncResult{}, fmt.Errorf("upsert rule pointers: %w", execErr)
		}
		rowsAffected, rowsErr := tag.RowsAffected()
		if rowsErr != nil {
			return core.RulePointerSyncResult{}, fmt.Errorf("read rule pointer rows affected: %w", rowsErr)
		}
		result.Upserted += int(rowsAffected)
	}

	activeKeys := make([]string, 0, len(normalized.Pointers))
	for _, pointer := range normalized.Pointers {
		activeKeys = append(activeKeys, pointer.PointerKey)
	}

	if len(activeKeys) == 0 {
		tag, execErr := tx.ExecContext(ctx, `
DELETE FROM awm_pointers
WHERE project_id = ?
	AND path = ?
	AND is_rule = 1
`, normalized.ProjectID, normalized.SourcePath)
		if execErr != nil {
			return core.RulePointerSyncResult{}, fmt.Errorf("delete missing rule pointers: %w", execErr)
		}
		rowsAffected, rowsErr := tag.RowsAffected()
		if rowsErr != nil {
			return core.RulePointerSyncResult{}, fmt.Errorf("read deleted rule rows affected: %w", rowsErr)
		}
		result.MarkedStale = int(rowsAffected)
	} else {
		query := `
DELETE FROM awm_pointers
WHERE project_id = ?
	AND path = ?
	AND is_rule = 1
	AND pointer_key NOT IN (` + placeholders(len(activeKeys)) + `)
`
		args := make([]any, 0, 2+len(activeKeys))
		args = append(args, normalized.ProjectID, normalized.SourcePath)
		for _, key := range activeKeys {
			args = append(args, key)
		}
		tag, execErr := tx.ExecContext(ctx, query, args...)
		if execErr != nil {
			return core.RulePointerSyncResult{}, fmt.Errorf("delete missing rule pointers: %w", execErr)
		}
		rowsAffected, rowsErr := tag.RowsAffected()
		if rowsErr != nil {
			return core.RulePointerSyncResult{}, fmt.Errorf("read deleted rule rows affected: %w", rowsErr)
		}
		result.MarkedStale = int(rowsAffected)
	}

	if err := tx.Commit(); err != nil {
		return core.RulePointerSyncResult{}, fmt.Errorf("commit tx: %w", err)
	}
	return result, nil
}

func normalizeRulePointerSyncInput(input core.RulePointerSyncInput) (core.RulePointerSyncInput, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return core.RulePointerSyncInput{}, fmt.Errorf("project_id is required")
	}

	sourcePath := normalizeSyncPath(input.SourcePath)
	if sourcePath == "" {
		return core.RulePointerSyncInput{}, fmt.Errorf("source_path is required")
	}

	byKey := make(map[string]core.RulePointer, len(input.Pointers))
	for _, raw := range input.Pointers {
		ruleID := strings.TrimSpace(raw.RuleID)
		if ruleID == "" {
			ruleID = ruleIDFromPointerKey(raw.PointerKey)
		}
		if ruleID == "" {
			return core.RulePointerSyncInput{}, fmt.Errorf("rule_id is required")
		}

		pointerKey := strings.TrimSpace(raw.PointerKey)
		if pointerKey == "" {
			pointerKey = fmt.Sprintf("%s:%s#%s", projectID, sourcePath, ruleID)
		}

		summary := strings.TrimSpace(raw.Summary)
		if summary == "" {
			return core.RulePointerSyncInput{}, fmt.Errorf("summary is required for pointer %q", pointerKey)
		}
		content := strings.TrimSpace(raw.Content)
		if content == "" {
			content = summary
		}

		enforcement := normalizeRulePointerEnforcement(raw.Enforcement)
		tags := normalizeRulePointerTags(raw.Tags, enforcement)

		byKey[pointerKey] = core.RulePointer{
			PointerKey:  pointerKey,
			SourcePath:  sourcePath,
			RuleID:      ruleID,
			Summary:     summary,
			Content:     content,
			Enforcement: enforcement,
			Tags:        tags,
		}
	}

	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pointers := make([]core.RulePointer, 0, len(keys))
	for _, key := range keys {
		pointers = append(pointers, byKey[key])
	}

	return core.RulePointerSyncInput{
		ProjectID:  projectID,
		SourcePath: sourcePath,
		Pointers:   pointers,
	}, nil
}

func normalizeRulePointerEnforcement(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "soft":
		return "soft"
	default:
		return "hard"
	}
}

func normalizeRulePointerTags(tags []string, enforcement string) []string {
	all := append([]string{}, tags...)
	all = append(all, "rule", "canonical-rule")
	if enforcement == "soft" {
		all = append(all, "enforcement-soft")
	} else {
		all = append(all, "enforcement-hard")
	}
	return normalizeStringList(all)
}

func ruleIDFromPointerKey(pointerKey string) string {
	key := strings.TrimSpace(pointerKey)
	if key == "" {
		return ""
	}
	separator := strings.LastIndex(key, "#")
	if separator < 0 || separator >= len(key)-1 {
		return ""
	}
	return strings.TrimSpace(key[separator+1:])
}
