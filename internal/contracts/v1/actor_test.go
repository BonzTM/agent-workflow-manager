package v1

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestDecodeAndValidateCommand_WorkflowPayloadsAcceptOptionalActor(t *testing.T) {
	actorJSON := `"actor":{"harness":"claude-code","model":"claude-fable-5","session_id":"session-0123456789"}`
	tests := []struct {
		command string
		body    string
	}{
		{"context", `"task_text":"fix bug","phase":"execute",` + actorJSON},
		{"work", `"receipt_id":"receipt.abc123",` + actorJSON},
		{"verify", `"receipt_id":"receipt.abc123",` + actorJSON},
		{"review", `"receipt_id":"receipt.abc123","key":"review:cross-llm",` + actorJSON},
		{"done", `"receipt_id":"receipt.abc123","outcome":"complete",` + actorJSON},
	}
	for _, tc := range tests {
		raw := fmt.Sprintf(`{
			"version":"awm.v1",
			"command":"%s",
			"request_id":"req-actor-1",
			"payload":{"project_id":"my-cool-app",%s}
		}`, tc.command, tc.body)
		_, _, errp := DecodeAndValidateCommand([]byte(raw))
		if errp != nil {
			t.Fatalf("%s: expected optional actor to be accepted, got %+v", tc.command, errp)
		}
	}
}

func TestDecodeAndValidateCommand_ActorFieldLimitsEnforced(t *testing.T) {
	long := strings.Repeat("x", 300)
	raw := fmt.Sprintf(`{
		"version":"awm.v1",
		"command":"context",
		"request_id":"req-actor-2",
		"payload":{"project_id":"my-cool-app","task_text":"fix bug","phase":"execute","actor":{"harness":"%s"}}
	}`, long)
	_, _, errp := DecodeAndValidateCommand([]byte(raw))
	if errp == nil {
		t.Fatal("expected overlong actor.harness to be rejected")
	}
}

func TestSchemas_ExposeActorDefinition(t *testing.T) {
	shared, err := os.ReadFile("../../../spec/v1/shared.schema.json")
	if err != nil {
		t.Fatalf("read shared schema: %v", err)
	}
	if !strings.Contains(string(shared), `"actor"`) {
		t.Fatal("shared.schema.json must define $defs.actor")
	}

	command, err := os.ReadFile("../../../spec/v1/cli.command.schema.json")
	if err != nil {
		t.Fatalf("read command schema: %v", err)
	}
	for _, def := range []string{"contextPayload", "workPayload", "verifyPayload", "reviewPayload", "donePayload"} {
		if !strings.Contains(string(command), def) {
			t.Fatalf("command schema is missing %s", def)
		}
	}
	if !strings.Contains(string(command), `shared.schema.json#/$defs/actor`) {
		t.Fatal("cli.command.schema.json payload defs must reference $defs.actor")
	}
}
