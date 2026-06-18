package postgres

import (
	"reflect"
	"testing"

	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func TestNormalizeRulePointerSyncInput_DedupesAndDefaults(t *testing.T) {
	normalized, err := normalizeRulePointerSyncInput(core.RulePointerSyncInput{
		ProjectID:  " project.alpha ",
		SourcePath: " .awm/awm-rules.yaml ",
		Pointers: []core.RulePointer{
			{
				RuleID:      "rule.keep",
				Summary:     "Keep summary",
				Content:     "",
				Enforcement: "soft",
				Tags:        []string{"ops", "ops"},
			},
			{
				PointerKey:  "project.alpha:.awm/awm-rules.yaml#rule.keep",
				RuleID:      "rule.keep",
				Summary:     "Keep summary updated",
				Content:     "Keep content updated",
				Enforcement: "hard",
				Tags:        []string{"security"},
			},
		},
	})
	if err != nil {
		t.Fatalf("normalize rule pointer sync input: %v", err)
	}

	if normalized.ProjectID != "project.alpha" {
		t.Fatalf("unexpected project_id: %q", normalized.ProjectID)
	}
	if normalized.SourcePath != ".awm/awm-rules.yaml" {
		t.Fatalf("unexpected source_path: %q", normalized.SourcePath)
	}
	if len(normalized.Pointers) != 1 {
		t.Fatalf("unexpected pointer count: %d", len(normalized.Pointers))
	}

	pointer := normalized.Pointers[0]
	if pointer.PointerKey != "project.alpha:.awm/awm-rules.yaml#rule.keep" {
		t.Fatalf("unexpected pointer key: %q", pointer.PointerKey)
	}
	if pointer.Content != "Keep content updated" {
		t.Fatalf("unexpected content: %q", pointer.Content)
	}
	if pointer.Enforcement != "hard" {
		t.Fatalf("unexpected enforcement: %q", pointer.Enforcement)
	}
	wantTags := []string{"canonical-rule", "enforcement-hard", "rule", "security"}
	if !reflect.DeepEqual(pointer.Tags, wantTags) {
		t.Fatalf("unexpected tags: got %v want %v", pointer.Tags, wantTags)
	}
}

func TestNormalizeRulePointerSyncInput_RequiresProjectAndSourcePath(t *testing.T) {
	if _, err := normalizeRulePointerSyncInput(core.RulePointerSyncInput{SourcePath: ".awm/awm-rules.yaml"}); err == nil {
		t.Fatal("expected project_id validation error")
	}
	if _, err := normalizeRulePointerSyncInput(core.RulePointerSyncInput{ProjectID: "project.alpha"}); err == nil {
		t.Fatal("expected source_path validation error")
	}
}
