package cli

import (
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
)

type routeGroup string

const (
	routeGroupWorkflow    routeGroup = "workflow"
	routeGroupMaintenance routeGroup = "maintenance"
)

type routeBuilder func([]string, func() time.Time) (convenienceBuildResult, error)

type routeSpec struct {
	Name    string
	Usage   string
	Summary string
	Group   routeGroup
	Build   routeBuilder
	Hidden  bool
}

var (
	routeCatalog       = buildRouteCatalog()
	routeCatalogByName = buildRouteCatalogByName(routeCatalog)
)

func lookupRouteSpec(name string) (routeSpec, bool) {
	spec, ok := routeCatalogByName[name]
	return spec, ok
}

func matchConvenienceRoute(args []string) (routeSpec, int, bool) {
	if len(args) == 0 {
		return routeSpec{}, 0, false
	}
	spec, ok := lookupRouteSpec(args[0])
	return spec, 1, ok
}

func buildRouteCatalog() []routeSpec {
	specs := make([]routeSpec, 0, len(v1.CommandSpecs()))
	for _, spec := range v1.CommandSpecs() {
		builder, ok := canonicalRouteBuilder(spec.CLISubcommand)
		if !ok {
			continue
		}
		specs = append(specs, routeSpec{
			Name:    spec.CLISubcommand,
			Usage:   spec.CLIUsage,
			Summary: spec.CLISummary,
			Group:   routeGroupForCommand(spec.Group),
			Build:   builder,
		})
	}

	return specs
}

func canonicalRouteBuilder(name string) (routeBuilder, bool) {
	switch name {
	case "context":
		return buildContextRequest, true
	case "fetch":
		return buildFetchRequest, true
	case "work":
		return envelopeOnlyBuilder(buildWorkEnvelope), true
	case "history":
		return func(args []string, now func() time.Time) (convenienceBuildResult, error) {
			return buildHistorySearchRequest("history", args, now)
		}, true
	case "done":
		return envelopeOnlyBuilder(buildDoneEnvelope), true
	case "review":
		return envelopeOnlyBuilder(buildReviewEnvelope), true
	case "sync":
		return envelopeOnlyBuilder(buildSyncEnvelope), true
	case "health":
		return envelopeOnlyBuilder(buildHealthEnvelope), true
	case "status":
		return buildStatusRequest, true
	case "verify":
		return envelopeOnlyBuilder(buildVerifyEnvelope), true
	case "init":
		return envelopeOnlyBuilder(buildInitEnvelope), true
	default:
		return nil, false
	}
}

func envelopeOnlyBuilder(builder func([]string, func() time.Time) (v1.CommandEnvelope, error)) routeBuilder {
	return func(args []string, now func() time.Time) (convenienceBuildResult, error) {
		env, err := builder(args, now)
		if err != nil {
			return convenienceBuildResult{}, err
		}
		return convenienceBuildResult{Envelope: env}, nil
	}
}

func routeGroupForCommand(group v1.CommandGroup) routeGroup {
	switch group {
	case v1.CommandGroupMaintenance:
		return routeGroupMaintenance
	default:
		return routeGroupWorkflow
	}
}

func buildRouteCatalogByName(specs []routeSpec) map[string]routeSpec {
	out := make(map[string]routeSpec, len(specs))
	for _, spec := range specs {
		out[spec.Name] = spec
	}
	return out
}

func helpCommandsForGroup(group routeGroup) []helpCommand {
	out := make([]helpCommand, 0, len(routeCatalog))
	for _, spec := range routeCatalog {
		if spec.Group != group || spec.Hidden {
			continue
		}
		out = append(out, helpCommand{
			usage:   spec.Usage,
			summary: spec.Summary,
		})
	}
	return out
}
