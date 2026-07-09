package v1

import (
	"encoding/json"
	"reflect"
	"strings"
)

// CommandGroup classifies catalog commands into functional groups used to
// filter the catalog (see WorkflowCommandCatalog and MaintenanceCommandCatalog).
type CommandGroup string

const (
	// CommandGroupWorkflow marks commands that drive the day-to-day task
	// workflow (context, fetch, done, review, work, history, export).
	CommandGroupWorkflow CommandGroup = "workflow"
	// CommandGroupMaintenance marks commands that maintain or inspect project
	// state (sync, health, status, verify, init).
	CommandGroupMaintenance CommandGroup = "maintenance"
)

// CommandSpec describes one catalog command: its canonical Command name, CLI
// subcommand/usage/summary, group, JSON schema definition names, MCP tool
// title/description, and the payload decoder invoked via Decode.
type CommandSpec struct {
	Command         Command
	CLISubcommand   string
	CLIUsage        string
	CLISummary      string
	Group           CommandGroup
	InputSchemaDef  string
	ResultSchemaDef string
	ToolTitle       string
	ToolDescription string
	decode          func(json.RawMessage, ValidationDefaults) (any, *ErrorPayload)
}

// Decode parses and validates the raw JSON payload for this command using the
// spec's decoder, applying the normalized validation defaults. It returns the
// typed payload, or an ErrorPayload when the spec has no decoder or the payload
// fails decoding or validation.
func (s CommandSpec) Decode(raw json.RawMessage, defaults ValidationDefaults) (any, *ErrorPayload) {
	if s.decode == nil {
		return nil, validationError(ErrCodeInvalidCommand, "command is not recognized")
	}
	return s.decode(raw, normalizeValidationDefaults(defaults))
}

var commandCatalog = []CommandSpec{
	newCommandSpec(
		CommandContext,
		"context",
		"awm context [--project <id>] [--task-text <text>|--task-file <path>] [--tags-file <path>] [--scope-path <path>]... [--format <json|markdown>] [--out-file <path>] [--force[=true|false]] [--actor-harness <text>] [--actor-model <text>] [--actor-session <text>]",
		"Resolve a scoped receipt with rules, active work, and optional known scope paths.",
		CommandGroupWorkflow,
		"contextPayload",
		"contextResult",
		"Get Task Context",
		"Resolve task-scoped rules, active plans, and optional known scope paths.",
		func(raw json.RawMessage, defaults ValidationDefaults) (any, *ErrorPayload) {
			return decodeValidatedCommandPayload(raw, defaults,
				func(p *ContextPayload, defaults ValidationDefaults) {
					p.ProjectID = defaultProjectID(p.ProjectID, defaults)
				},
				validateContextPayload,
			)
		},
	),
	newCommandSpec(
		CommandFetch,
		"fetch",
		"awm fetch [--project <id>] [--key <pointer>]... [--keys-file <path>] [--keys-json <json>] [--receipt-id <id>] [--expect <key=version>]... [--expected-versions-file <path>] [--expected-versions-json <json>] [--format <json|markdown>] [--out-file <path>] [--force[=true|false]]",
		"Fetch pointer, plan, or task content by key, with optional version checks.",
		CommandGroupWorkflow,
		"fetchPayload",
		"fetchResult",
		"Fetch Stored Artifacts",
		"Fetch receipt, plan, or indexed artifacts by key with optional expected versions.",
		func(raw json.RawMessage, defaults ValidationDefaults) (any, *ErrorPayload) {
			return decodeValidatedCommandPayloadWithFields(raw, defaults,
				func(p *FetchPayload, defaults ValidationDefaults) {
					p.ProjectID = defaultProjectID(p.ProjectID, defaults)
				},
				validateFetchPayload,
			)
		},
	),
	newCommandSpec(
		CommandExport,
		"export",
		`awm run --in <request.json>  # {"command":"export",...}`,
		"Render AWM-owned structured artifacts as JSON or Markdown through the backend export surface.",
		CommandGroupWorkflow,
		"exportPayload",
		"exportResult",
		"Export Structured Artifacts",
		"Render AWM-owned context, fetch, history, or status data as stable JSON or Markdown content.",
		func(raw json.RawMessage, defaults ValidationDefaults) (any, *ErrorPayload) {
			return decodeValidatedCommandPayloadWithFields(raw, defaults,
				func(p *ExportPayload, defaults ValidationDefaults) {
					if p.Status != nil {
						p.ProjectID = defaultProjectIDForRoot(p.ProjectID, p.Status.ProjectRoot, defaults)
						return
					}
					p.ProjectID = defaultProjectID(p.ProjectID, defaults)
				},
				validateExportPayload,
			)
		},
	),
	newCommandSpec(
		CommandDone,
		"done",
		"awm done [--project <id>] [--receipt-id <id>|--plan-key <key>] [--outcome <text>|--outcome-file <path>] [--file-changed <path>]... [--files-changed-file <path>] [--files-changed-json <json>] [--no-file-changes[=true|false]] [--scope-mode <mode>] [--tags-file <path>] [--actor-harness <text>] [--actor-model <text>] [--actor-session <text>]",
		"Close a receipt, validate scope, and enforce configured completion task gates.",
		CommandGroupWorkflow,
		"donePayload",
		"doneResult",
		"Close Task",
		"Validate the task delta against effective scope and persist completion history, with explicit no-file closeout support when needed.",
		func(raw json.RawMessage, defaults ValidationDefaults) (any, *ErrorPayload) {
			return decodeValidatedCommandPayloadWithFields(raw, defaults,
				func(p *DonePayload, defaults ValidationDefaults) {
					p.ProjectID = defaultProjectID(p.ProjectID, defaults)
				},
				validateDonePayload,
			)
		},
	),
	newCommandSpec(
		CommandReview,
		"review",
		"awm review [--project <id>] [--receipt-id <id>|--plan-key <key>] [--run] [--key <task-key>] [--summary <text>] [--status <pending|in_progress|complete|blocked|superseded>] [--outcome <text>|--outcome-file <path>] [--blocked-reason <text>] [--evidence <text>]... [--evidence-file <path>|--evidence-json <json>] [--tags-file <path>] [--actor-harness <text>] [--actor-model <text>] [--actor-session <text>]",
		"Record or execute a single review gate such as `review:cross-llm` through the work tracker, using `--run` to satisfy runnable gates.",
		CommandGroupWorkflow,
		"reviewPayload",
		"reviewResult",
		"Record Review Gate",
		"Record or execute a single review task gate such as review:cross-llm through the work tracker; runnable gates require run=true for completion.",
		func(raw json.RawMessage, defaults ValidationDefaults) (any, *ErrorPayload) {
			return decodeValidatedCommandPayloadWithFields(raw, defaults,
				func(p *ReviewPayload, defaults ValidationDefaults) {
					p.ProjectID = defaultProjectID(p.ProjectID, defaults)
				},
				validateReviewPayload,
			)
		},
	),
	newCommandSpec(
		CommandWork,
		"work",
		"awm work [--project <id>] [--plan-key <key>|--receipt-id <id>|--task-text <text>] [--phase <plan|execute|review>] [--plan-title <text>] [--mode <merge|replace>] [--discovered-path <path>]... [--plan-file <path>|--plan-json <json>] [--tasks-file <path>|--tasks-json <json>] [--actor-harness <text>] [--actor-model <text>] [--actor-session <text>]",
		"Create or update structured plans and tasks that survive compaction.",
		CommandGroupWorkflow,
		"workPayload",
		"workResult",
		"Update Work Plan",
		"Create or update plan-scoped work state, tasks, and discovered paths.",
		func(raw json.RawMessage, defaults ValidationDefaults) (any, *ErrorPayload) {
			return decodeValidatedCommandPayload(raw, defaults,
				func(p *WorkPayload, defaults ValidationDefaults) {
					p.ProjectID = defaultProjectID(p.ProjectID, defaults)
				},
				validateWorkPayload,
			)
		},
	),
	newCommandSpec(
		CommandHistorySearch,
		"history",
		"awm history [--project <id>] [--entity <all|work|receipt|run>] [--query <text>|--query-file <path>] [--scope <current|deferred|completed|all>] [--kind <kind>] [--limit <n>] [--unbounded[=true|false]] [--format <json|markdown>] [--out-file <path>] [--force[=true|false]]",
		"Search recent work, receipt, and run history without direct database access.",
		CommandGroupWorkflow,
		"historySearchPayload",
		"historySearchResult",
		"Search History",
		"List or search work plans, receipts, and runs without direct database access.",
		func(raw json.RawMessage, defaults ValidationDefaults) (any, *ErrorPayload) {
			return decodeValidatedCommandPayload(raw, defaults,
				func(p *HistorySearchPayload, defaults ValidationDefaults) {
					p.ProjectID = defaultProjectID(p.ProjectID, defaults)
				},
				validateHistorySearchPayload,
			)
		},
	),
	newCommandSpec(
		CommandSync,
		"sync",
		"awm sync [--project <id>] [--mode changed|full|working_tree] [--git-range <range>] [--project-root <path>] [--rules-file <path>] [--tags-file <path>] [--insert-new-candidates[=true|false]]",
		"Refresh repository pointers and canonical rules from the working tree or git history.",
		CommandGroupMaintenance,
		"syncPayload",
		"syncResult",
		"Sync Project Inventory",
		"Refresh indexed repository pointers from git or the working tree.",
		func(raw json.RawMessage, defaults ValidationDefaults) (any, *ErrorPayload) {
			return decodeValidatedCommandPayload(raw, defaults,
				func(p *SyncPayload, defaults ValidationDefaults) {
					p.ProjectID = defaultProjectIDForRoot(p.ProjectID, p.ProjectRoot, defaults)
				},
				validateSyncPayload,
			)
		},
	),
	newCommandSpec(
		CommandHealth,
		"health",
		"awm health [--project <id>] [--include-details[=true|false]] [--max-findings-per-check <n>] | [--fix <name>]... [--dry-run[=true|false]] [--apply[=true|false]] [--project-root <path>] [--rules-file <path>] [--tags-file <path>]",
		"Check project health or run selected health fixers via `--fix`.",
		CommandGroupMaintenance,
		"healthPayload",
		"healthResult",
		"Inspect Or Fix Project Health",
		"Inspect AWM repository health or run selected repair actions through one public health surface.",
		func(raw json.RawMessage, defaults ValidationDefaults) (any, *ErrorPayload) {
			return decodeValidatedCommandPayloadWithFields(raw, defaults,
				func(p *HealthPayload, defaults ValidationDefaults) {
					p.ProjectID = defaultProjectIDForRoot(p.ProjectID, p.ProjectRoot, defaults)
				},
				validateHealthPayload,
			)
		},
	),
	newCommandSpec(
		CommandStatus,
		"status",
		"awm status [--project <id>] [--project-root <path>] [--rules-file <path>] [--tags-file <path>] [--tests-file <path>] [--workflows-file <path>] [--task-text <text>|--task-file <path>] [--phase <plan|execute|review>] [--format <json|markdown>] [--out-file <path>] [--force[=true|false]]",
		"Explain active project/runtime state, loaded AWM files, installed integrations, and governance inputs.",
		CommandGroupMaintenance,
		"statusPayload",
		"statusResult",
		"Inspect AWM Status",
		"Explain current project/runtime state, loaded AWM files, installed integrations, and governance inputs.",
		func(raw json.RawMessage, defaults ValidationDefaults) (any, *ErrorPayload) {
			return decodeValidatedCommandPayload(raw, defaults,
				func(p *StatusPayload, defaults ValidationDefaults) {
					p.ProjectID = defaultProjectIDForRoot(p.ProjectID, p.ProjectRoot, defaults)
				},
				validateStatusPayload,
			)
		},
	),
	newCommandSpec(
		CommandVerify,
		"verify",
		"awm verify [--project <id>] [--receipt-id <id>] [--plan-key <key>] [--phase <plan|execute|review>] [--test-id <id>]... [--file-changed <path>]... [--files-changed-file <path>|--files-changed-json <json>] [--tests-file <path>] [--tags-file <path>] [--dry-run] [--actor-harness <text>] [--actor-model <text>] [--actor-session <text>]",
		"Select and execute repo-defined verification checks from `.awm/awm-tests.yaml` or `awm-tests.yaml`.",
		CommandGroupMaintenance,
		"verifyPayload",
		"verifyResult",
		"Run Executable Verification",
		"Select and execute repo-defined verification checks from awm test definitions.",
		func(raw json.RawMessage, defaults ValidationDefaults) (any, *ErrorPayload) {
			return decodeValidatedCommandPayloadWithFields(raw, defaults,
				func(p *VerifyPayload, defaults ValidationDefaults) {
					p.ProjectID = defaultProjectID(p.ProjectID, defaults)
				},
				validateVerifyPayload,
			)
		},
	),
	newCommandSpec(
		CommandInit,
		"init",
		"awm init [--project <id>] [--project-root <path>] [--apply-template <id>]... [--rules-file <path>] [--tags-file <path>] [--persist-candidates[=true|false]] [--respect-gitignore[=true|false]] [--output-candidates-path <path>]",
		"Initialize repo-local AWM files, optionally apply additive templates, and scan a repository for initial pointer candidates.",
		CommandGroupMaintenance,
		"initPayload",
		"initResult",
		"Initialize Repository",
		"Scan a repository, initialize AWM files, and optionally apply additive templates.",
		func(raw json.RawMessage, defaults ValidationDefaults) (any, *ErrorPayload) {
			return decodeValidatedCommandPayloadWithFields(raw, defaults,
				func(p *InitPayload, defaults ValidationDefaults) {
					p.ProjectID = defaultProjectIDForRoot(p.ProjectID, p.ProjectRoot, defaults)
				},
				validateInitPayload,
			)
		},
	),
}
var commandCatalogByCommand = buildCommandCatalogByCommand(commandCatalog)

// CommandCatalog returns a copy of the full command catalog in its declared
// order; callers may modify the returned slice freely.
func CommandCatalog() []CommandSpec {
	out := make([]CommandSpec, len(commandCatalog))
	copy(out, commandCatalog)
	return out
}

// CommandSpecs returns a copy of the full command catalog; it is an alias for
// CommandCatalog.
func CommandSpecs() []CommandSpec {
	return CommandCatalog()
}

// WorkflowCommandCatalog returns the catalog commands in CommandGroupWorkflow,
// in declared order.
func WorkflowCommandCatalog() []CommandSpec {
	return commandCatalogByGroup(commandCatalog, CommandGroupWorkflow)
}

// MaintenanceCommandCatalog returns the catalog commands in
// CommandGroupMaintenance, in declared order.
func MaintenanceCommandCatalog() []CommandSpec {
	return commandCatalogByGroup(commandCatalog, CommandGroupMaintenance)
}

// LookupCommandSpec returns the catalog spec for the given command and whether
// the command is registered.
func LookupCommandSpec(command Command) (CommandSpec, bool) {
	spec, ok := commandCatalogByCommand[command]
	return spec, ok
}

// LookupCommand returns the catalog spec for the given command and whether it
// is registered; it is an alias for LookupCommandSpec.
func LookupCommand(command Command) (CommandSpec, bool) {
	return LookupCommandSpec(command)
}

// LookupCommandByCLISubcommand returns the catalog spec whose CLISubcommand
// matches the trimmed input, and whether a match was found.
func LookupCommandByCLISubcommand(subcommand string) (CommandSpec, bool) {
	trimmed := strings.TrimSpace(subcommand)
	for _, spec := range commandCatalog {
		if spec.CLISubcommand == trimmed {
			return spec, true
		}
	}
	return CommandSpec{}, false
}

// CommandNames returns the canonical command names of every catalog entry, in
// declared order.
func CommandNames() []string {
	out := make([]string, 0, len(commandCatalog))
	for _, spec := range commandCatalog {
		out = append(out, string(spec.Command))
	}
	return out
}

// CommandFromToolName maps an MCP tool name (trimmed) to its canonical catalog
// Command, returning false when the name is not a registered command.
func CommandFromToolName(tool string) (Command, bool) {
	spec, ok := LookupCommand(Command(strings.TrimSpace(tool)))
	if !ok {
		return "", false
	}
	return spec.Command, true
}

// BuildEnvelopeForCommand marshals a CommandEnvelope for the given command with
// the current contract Version, the trimmed request ID, and the raw payload.
func BuildEnvelopeForCommand(command Command, requestID string, payload json.RawMessage) ([]byte, error) {
	return json.Marshal(CommandEnvelope{
		Version:   Version,
		Command:   command,
		RequestID: strings.TrimSpace(requestID),
		Payload:   payload,
	})
}

// ProjectIDFromPayload extracts the trimmed project ID from any known command
// payload type, a map with a "project_id" key, or (via reflection) any struct
// with a string ProjectID field. It returns "" when no project ID is found.
func ProjectIDFromPayload(payload any) string {
	switch p := payload.(type) {
	case ContextPayload:
		return strings.TrimSpace(p.ProjectID)
	case FetchPayload:
		return strings.TrimSpace(p.ProjectID)
	case ExportPayload:
		return strings.TrimSpace(p.ProjectID)
	case DonePayload:
		return strings.TrimSpace(p.ProjectID)
	case ReviewPayload:
		return strings.TrimSpace(p.ProjectID)
	case WorkPayload:
		return strings.TrimSpace(p.ProjectID)
	case HistorySearchPayload:
		return strings.TrimSpace(p.ProjectID)
	case SyncPayload:
		return strings.TrimSpace(p.ProjectID)
	case HealthPayload:
		return strings.TrimSpace(p.ProjectID)
	case StatusPayload:
		return strings.TrimSpace(p.ProjectID)
	case VerifyPayload:
		return strings.TrimSpace(p.ProjectID)
	case InitPayload:
		return strings.TrimSpace(p.ProjectID)
	case map[string]any:
		if raw, ok := p["project_id"].(string); ok {
			return strings.TrimSpace(raw)
		}
	case map[string]string:
		return strings.TrimSpace(p["project_id"])
	default:
		// Reflect fallback: extracts ProjectID from arbitrary struct types not covered
		// by the explicit cases above. Provides forward-compatibility so callers can
		// pass new payload types without updating this switch. Tested in cli/run_test.go.
		value := reflect.ValueOf(payload)
		if !value.IsValid() {
			return ""
		}
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return ""
			}
			value = value.Elem()
		}
		if value.Kind() != reflect.Struct {
			return ""
		}
		field := value.FieldByName("ProjectID")
		if !field.IsValid() || field.Kind() != reflect.String {
			return ""
		}
		return strings.TrimSpace(field.String())
	}
	return ""
}

func decodeValidatedCommandPayload[Payload any](
	raw json.RawMessage,
	defaults ValidationDefaults,
	applyDefaults func(*Payload, ValidationDefaults),
	validate func(*Payload) error,
) (any, *ErrorPayload) {
	var payload Payload
	if err := decodeStrict(raw, &payload); err != nil {
		return nil, validationError(ErrCodeInvalidPayload, err.Error())
	}
	if applyDefaults != nil {
		applyDefaults(&payload, defaults)
	}
	if validate != nil {
		if err := validate(&payload); err != nil {
			return nil, validationError(ErrCodeInvalidPayload, err.Error())
		}
	}
	return payload, nil
}

func decodeValidatedCommandPayloadWithFields[Payload any](
	raw json.RawMessage,
	defaults ValidationDefaults,
	applyDefaults func(*Payload, ValidationDefaults),
	validate func(*Payload, map[string]json.RawMessage) error,
) (any, *ErrorPayload) {
	var payload Payload
	if err := decodeStrict(raw, &payload); err != nil {
		return nil, validationError(ErrCodeInvalidPayload, err.Error())
	}
	if applyDefaults != nil {
		applyDefaults(&payload, defaults)
	}
	fields, err := decodeObjectFields(raw)
	if err != nil {
		return nil, validationError(ErrCodeInvalidPayload, err.Error())
	}
	if validate != nil {
		if err := validate(&payload, fields); err != nil {
			return nil, validationError(ErrCodeInvalidPayload, err.Error())
		}
	}
	return payload, nil
}

func newCommandSpec(
	command Command,
	cliSubcommand string,
	cliUsage string,
	cliSummary string,
	group CommandGroup,
	inputSchemaDef string,
	resultSchemaDef string,
	toolTitle string,
	toolDescription string,
	decode func(json.RawMessage, ValidationDefaults) (any, *ErrorPayload),
) CommandSpec {
	return CommandSpec{
		Command:         command,
		CLISubcommand:   cliSubcommand,
		CLIUsage:        cliUsage,
		CLISummary:      cliSummary,
		Group:           group,
		InputSchemaDef:  inputSchemaDef,
		ResultSchemaDef: resultSchemaDef,
		ToolTitle:       toolTitle,
		ToolDescription: toolDescription,
		decode:          decode,
	}
}

func buildCommandCatalogByCommand(specs []CommandSpec) map[Command]CommandSpec {
	out := make(map[Command]CommandSpec, len(specs))
	for _, spec := range specs {
		out[spec.Command] = spec
	}
	return out
}

func commandCatalogByGroup(specs []CommandSpec, group CommandGroup) []CommandSpec {
	out := make([]CommandSpec, 0, len(specs))
	for _, spec := range specs {
		if spec.Group == group {
			out = append(out, spec)
		}
	}
	return out
}
