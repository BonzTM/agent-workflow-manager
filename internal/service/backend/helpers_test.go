package backend

import (
	"encoding/json"
	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func candidate(key, path string, isRule bool, tags []string) core.CandidatePointer {
	return core.CandidatePointer{
		Key:         key,
		Path:        path,
		Kind:        "code",
		Label:       key,
		Description: "desc " + key,
		Tags:        append([]string(nil), tags...),
		IsRule:      isRule,
	}
}

func initTemplateResultByID(results []v1.InitTemplateResult, templateID string) (v1.InitTemplateResult, bool) {
	for _, result := range results {
		if result.TemplateID == templateID {
			return result, true
		}
	}
	return v1.InitTemplateResult{}, false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func pointerKeys(receipt *v1.ContextReceipt) []string {
	if receipt == nil {
		return nil
	}

	keys := make(map[string]struct{}, len(receipt.Rules))
	for _, entry := range receipt.Rules {
		key := strings.TrimSpace(entry.Key)
		if key == "" {
			continue
		}
		keys[key] = struct{}{}
	}
	return mapKeysSorted(keys)
}

func receiptIndexEntries(receipt *v1.ContextReceipt, index string) []map[string]any {
	payload := receiptJSONMap(receipt)
	if len(payload) == 0 {
		return nil
	}

	switch index {
	case "rules", "plans":
		return normalizeIndexEntries(payload[index])
	default:
		return nil
	}
}

func receiptJSONMap(receipt *v1.ContextReceipt) map[string]any {
	if receipt == nil {
		return nil
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return nil
	}
	payload := make(map[string]any)
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	return payload
}

func normalizeIndexEntries(raw any) []map[string]any {
	entries, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(entries))
	for _, rawEntry := range entries {
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func receiptIndexKeys(entries []map[string]any) []string {
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		key := strings.TrimSpace(entryString(entry, "key"))
		if key == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func containsAllStrings(haystack, needles []string) bool {
	available := make(map[string]struct{}, len(haystack))
	for _, value := range haystack {
		available[strings.TrimSpace(value)] = struct{}{}
	}
	for _, value := range needles {
		if _, ok := available[strings.TrimSpace(value)]; !ok {
			return false
		}
	}
	return true
}

func receiptMeta(receipt *v1.ContextReceipt) map[string]any {
	payload := receiptJSONMap(receipt)
	if len(payload) == 0 {
		return nil
	}
	if meta, ok := payload["_meta"].(map[string]any); ok {
		return meta
	}
	return nil
}

func entryString(entry map[string]any, field string) string {
	if entry == nil {
		return ""
	}
	return anyToString(entry[field])
}

func entryStringSlice(entry map[string]any, field string) []string {
	if entry == nil {
		return nil
	}
	raw := entry[field]
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.TrimSpace(anyToString(value))
		if normalized == "" {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

func anyToString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strings.TrimSpace(strconv.FormatFloat(typed, 'f', -1, 64))
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return ""
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func writeRepoFile(t *testing.T, root, relPath, contents string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(relPath), err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}

func syncPathPaths(paths []core.SyncPath) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, path.Path)
	}
	return out
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()

	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd %s: %v", previous, err)
		}
	})
}
