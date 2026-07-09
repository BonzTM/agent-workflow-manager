package runtime

import (
	"testing"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
)

func mapLookup(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	}
}

func TestResolveActor_ExplicitValuesAlwaysWin(t *testing.T) {
	env := map[string]string{
		ActorHarnessEnvVar: "codex",
		ActorModelEnvVar:   "gpt-5.5",
		ActorSessionEnvVar: "env-session",
	}
	got := ResolveActor(&v1.ActorRef{Harness: " claude-code ", Model: "claude-fable-5"}, mapLookup(env))
	if got == nil {
		t.Fatal("expected resolved actor")
	}
	if got.Harness != "claude-code" || got.Model != "claude-fable-5" {
		t.Fatalf("expected explicit values to win, got %+v", got)
	}
	if got.SessionID != "env-session" {
		t.Fatalf("expected env to fill blank session, got %+v", got)
	}
}

func TestResolveActor_HarnessAutoDetection(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"claude-code", map[string]string{"CLAUDECODE": "1"}, "claude-code"},
		{"claude-entrypoint", map[string]string{"CLAUDE_CODE_ENTRYPOINT": "cli"}, "claude-code"},
		{"codex", map[string]string{"CODEX_SANDBOX": "seatbelt"}, "codex"},
		{"opencode", map[string]string{"OPENCODE": "1"}, "opencode"},
	}
	for _, tc := range tests {
		got := ResolveActor(nil, mapLookup(tc.env))
		if got == nil || got.Harness != tc.want {
			t.Fatalf("%s: expected harness %q, got %+v", tc.name, tc.want, got)
		}
	}
}

func TestResolveActor_EnvHarnessBeatsAutoDetection(t *testing.T) {
	env := map[string]string{
		ActorHarnessEnvVar: "opencode",
		"CLAUDECODE":       "1",
	}
	got := ResolveActor(nil, mapLookup(env))
	if got == nil || got.Harness != "opencode" {
		t.Fatalf("expected AWM_ACTOR_HARNESS to beat auto-detection, got %+v", got)
	}
}

func TestResolveActor_NothingResolvesToNil(t *testing.T) {
	if got := ResolveActor(nil, mapLookup(nil)); got != nil {
		t.Fatalf("expected nil actor when nothing identifies one, got %+v", got)
	}
	if got := ResolveActor(&v1.ActorRef{Harness: "  "}, mapLookup(nil)); got != nil {
		t.Fatalf("expected nil actor for blank explicit fields, got %+v", got)
	}
}

func TestApplyActorDefaults_OnlyTouchesWorkflowPayloads(t *testing.T) {
	env := map[string]string{"CLAUDECODE": "1"}

	done, ok := ApplyActorDefaults(v1.DonePayload{Outcome: "x"}, mapLookup(env)).(v1.DonePayload)
	if !ok {
		t.Fatal("expected a done payload back")
	}
	if done.Actor == nil || done.Actor.Harness != "claude-code" {
		t.Fatalf("expected done payload actor default, got %+v", done.Actor)
	}

	fetch, ok := ApplyActorDefaults(v1.FetchPayload{ProjectID: "p"}, mapLookup(env)).(v1.FetchPayload)
	if !ok {
		t.Fatal("expected a fetch payload back")
	}
	if fetch.ProjectID != "p" {
		t.Fatalf("expected fetch payload to pass through, got %+v", fetch)
	}
}
