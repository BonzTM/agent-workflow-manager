package postgres

import (
	"reflect"
	"testing"

	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func TestNormalizeRunReceiptSummary_NormalizesDefinitionOfDoneIssues(t *testing.T) {
	normalized, err := normalizeRunReceiptSummary(core.RunReceiptSummary{
		ProjectID: " project.alpha ",
		DefinitionOfDoneIssues: []string{
			" missing verify:tests ",
			"missing verify:diff-review",
			"missing verify:tests",
		},
	})
	if err != nil {
		t.Fatalf("normalize run receipt summary: %v", err)
	}

	wantIssues := []string{"missing verify:diff-review", "missing verify:tests"}
	if !reflect.DeepEqual(normalized.DefinitionOfDoneIssues, wantIssues) {
		t.Fatalf("unexpected normalized DoD issues: got %v want %v", normalized.DefinitionOfDoneIssues, wantIssues)
	}
	if normalized.Status != "accepted" {
		t.Fatalf("expected default status accepted, got %q", normalized.Status)
	}
	if normalized.Phase != "execute" {
		t.Fatalf("expected default phase execute, got %q", normalized.Phase)
	}
}
