package commands

import (
	"testing"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
)

func TestDispatchHandlersCoverCommandCatalog(t *testing.T) {
	specs := v1.CommandSpecs()
	if len(handlers) != len(specs) {
		t.Fatalf("handler catalog count mismatch: got %d want %d", len(handlers), len(specs))
	}

	for _, spec := range specs {
		if _, ok := handlers[spec.Command]; !ok {
			t.Fatalf("missing dispatch handler for %q", spec.Command)
		}
	}
}
