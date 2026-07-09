package runtime

import (
	"strings"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
)

const (
	// ActorHarnessEnvVar overrides the recorded agent harness identity.
	ActorHarnessEnvVar = "AWM_ACTOR_HARNESS"
	// ActorModelEnvVar overrides the recorded agent model identity.
	ActorModelEnvVar = "AWM_ACTOR_MODEL"
	// ActorSessionEnvVar overrides the recorded agent session identity.
	ActorSessionEnvVar = "AWM_ACTOR_SESSION"
)

// harnessDetectionEnvVars maps well-known harness runtime markers to the
// canonical harness name recorded when nothing more explicit identifies one.
// Ordered probes keep detection deterministic.
var harnessDetectionEnvVars = []struct {
	EnvVar  string
	Harness string
}{
	{"CLAUDECODE", "claude-code"},
	{"CLAUDE_CODE_ENTRYPOINT", "claude-code"},
	{"CODEX_SANDBOX", "codex"},
	{"OPENCODE", "opencode"},
}

// ResolveActor fills the unset fields of explicit from the AWM_ACTOR_* env
// vars, then fills a still-blank harness from harness auto-detection. Explicit
// values always win. It returns nil when no field resolves, so attribution
// stays fully opt-in for callers and adopters that never configure it.
func ResolveActor(explicit *v1.ActorRef, lookupEnv func(string) (string, bool)) *v1.ActorRef {
	if lookupEnv == nil {
		return normalizeActor(explicit)
	}

	resolved := v1.ActorRef{}
	if explicit != nil {
		resolved = *explicit
	}
	resolved.Harness = strings.TrimSpace(resolved.Harness)
	resolved.Model = strings.TrimSpace(resolved.Model)
	resolved.SessionID = strings.TrimSpace(resolved.SessionID)

	if resolved.Harness == "" {
		resolved.Harness = trimmedEnvValue(lookupEnv, ActorHarnessEnvVar)
	}
	if resolved.Model == "" {
		resolved.Model = trimmedEnvValue(lookupEnv, ActorModelEnvVar)
	}
	if resolved.SessionID == "" {
		resolved.SessionID = trimmedEnvValue(lookupEnv, ActorSessionEnvVar)
	}

	if resolved.Harness == "" {
		for _, probe := range harnessDetectionEnvVars {
			if value, ok := lookupEnv(probe.EnvVar); ok && strings.TrimSpace(value) != "" {
				resolved.Harness = probe.Harness
				break
			}
		}
	}

	return normalizeActor(&resolved)
}

// ApplyActorDefaults returns payload with actor defaults resolved for the
// workflow payload types that carry attribution; every other payload passes
// through unchanged. Callers at the adapter boundary use this so the backend
// only ever sees explicit actor values.
func ApplyActorDefaults(payload any, lookupEnv func(string) (string, bool)) any {
	switch p := payload.(type) {
	case v1.ContextPayload:
		p.Actor = ResolveActor(p.Actor, lookupEnv)
		return p
	case v1.WorkPayload:
		p.Actor = ResolveActor(p.Actor, lookupEnv)
		return p
	case v1.VerifyPayload:
		p.Actor = ResolveActor(p.Actor, lookupEnv)
		return p
	case v1.ReviewPayload:
		p.Actor = ResolveActor(p.Actor, lookupEnv)
		return p
	case v1.DonePayload:
		p.Actor = ResolveActor(p.Actor, lookupEnv)
		return p
	default:
		return payload
	}
}

func normalizeActor(actor *v1.ActorRef) *v1.ActorRef {
	if actor == nil {
		return nil
	}
	normalized := v1.ActorRef{
		Harness:   strings.TrimSpace(actor.Harness),
		Model:     strings.TrimSpace(actor.Model),
		SessionID: strings.TrimSpace(actor.SessionID),
	}
	if normalized == (v1.ActorRef{}) {
		return nil
	}
	return &normalized
}

func trimmedEnvValue(lookupEnv func(string) (string, bool), key string) string {
	value, ok := lookupEnv(key)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}
