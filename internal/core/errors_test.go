package core

import (
	"testing"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
)

func TestAPIErrorToPayloadIncludesSource(t *testing.T) {
	err := NewErrorWithSource(v1.ErrCodeInvalidPayload, "bad payload", v1.ErrSourceAdapter, map[string]any{"field": "payload"})
	p := err.ToPayload()
	if p == nil {
		t.Fatalf("expected payload")
	}
	if p.Source != v1.ErrSourceAdapter {
		t.Fatalf("unexpected source: got %q want %q", p.Source, v1.ErrSourceAdapter)
	}
}
