package cli

import (
	"testing"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
)

func TestRouteCatalogCoversCanonicalCommands(t *testing.T) {
	specs := v1.CommandSpecs()
	for _, spec := range specs {
		_, wantsRoute := canonicalRouteBuilder(spec.CLISubcommand)
		route, ok := lookupRouteSpec(spec.CLISubcommand)
		if !wantsRoute {
			if ok {
				t.Fatalf("expected no convenience route for %q", spec.CLISubcommand)
			}
			continue
		}
		if !ok {
			t.Fatalf("missing route for %q", spec.CLISubcommand)
		}
		if route.Usage != spec.CLIUsage {
			t.Fatalf("usage drift for %q: got %q want %q", spec.CLISubcommand, route.Usage, spec.CLIUsage)
		}
		if route.Summary != spec.CLISummary {
			t.Fatalf("summary drift for %q: got %q want %q", spec.CLISubcommand, route.Summary, spec.CLISummary)
		}
	}

	healthRoute, ok := lookupRouteSpec("health")
	if !ok {
		t.Fatal("missing route for health")
	}
	if healthRoute.Summary == "" {
		t.Fatal("expected health route summary")
	}
	if _, ok := lookupRouteSpec("export"); ok {
		t.Fatal("expected export convenience route to be absent")
	}
	for _, name := range []string{"get-context", "report-completion", "bootstrap", "doctor", "health-check", "health-fix"} {
		if _, ok := lookupRouteSpec(name); ok {
			t.Fatalf("expected removed legacy route %q to be absent", name)
		}
	}
}

func TestMatchConvenienceRoute_RejectsRemovedDirectAliases(t *testing.T) {
	for _, args := range [][]string{
		{"doctor"},
		{"history-search"},
		{"work-list"},
		{"work-search"},
	} {
		if _, _, ok := matchConvenienceRoute(args); ok {
			t.Fatalf("expected direct alias %q to be rejected", args[0])
		}
	}
}
