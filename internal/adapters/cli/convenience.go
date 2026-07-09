package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	"github.com/bonztm/agent-workflow-manager/internal/logging"
	"github.com/bonztm/agent-workflow-manager/internal/runtime"
)

type serviceFactory func(context.Context, logging.Logger) (core.Service, runtime.CleanupFunc, error)

type adapterRunner func(context.Context, core.Service, io.Reader, io.Writer, func() time.Time, logging.Logger) int

type convenienceBuildResult struct {
	Envelope  v1.CommandEnvelope
	RawOutput *rawOutputOptions
}

type rawOutputOptions struct {
	OutFile string
	Force   bool
}

func runConvenienceWithDeps(
	ctx context.Context,
	logger logging.Logger,
	subcommand string,
	args []string,
	out io.Writer,
	now func() time.Time,
	newService serviceFactory,
	runAdapter adapterRunner,
) int {
	logger = logging.Normalize(logger)
	logger.Info(ctx, logging.EventAWMRun, "stage", "start", "subcommand", subcommand)

	if out == nil {
		out = os.Stdout
	}
	if now == nil {
		now = time.Now
	}
	if newService == nil {
		newService = runtime.NewServiceFromEnvWithLogger
	}
	if runAdapter == nil {
		runAdapter = RunWithLogger
	}

	requestSpec, err := buildConvenienceRequest(subcommand, args, now)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			logger.Info(ctx, logging.EventAWMRun, "stage", "finish", "subcommand", subcommand, "exit_code", 0)
			return 0
		}
		logger.Error(ctx, logging.EventAWMRun, "stage", "parse_flags", "subcommand", subcommand, "ok", false, "error_code", v1.ErrCodeInvalidFlags)
		fmt.Fprintf(os.Stderr, "failed to parse %s flags: %v\n", subcommand, err)
		return 2
	}

	request, err := json.Marshal(requestSpec.Envelope)
	if err != nil {
		logger.Error(ctx, logging.EventAWMRun, "stage", "marshal", "subcommand", subcommand, "ok", false, "error_code", v1.ErrCodeInternalError)
		fmt.Fprintf(os.Stderr, "failed to build request: %v\n", err)
		return 1
	}

	svc, closeService, err := newService(ctx, logger)
	if err != nil {
		logger.Error(ctx, logging.EventAWMRun, "stage", "service_init", "subcommand", subcommand, "ok", false, "error_code", v1.ErrCodeServiceInitFailed)
		fmt.Fprintf(os.Stderr, "failed to initialize service: %v\n", err)
		return 1
	}
	defer closeService()

	adapterOut := out
	var rawBuffer bytes.Buffer
	if requestSpec.RawOutput != nil {
		adapterOut = &rawBuffer
	}

	code := runAdapter(ctx, svc, bytes.NewReader(request), adapterOut, now, logger)
	if requestSpec.RawOutput != nil {
		if code != 0 {
			if _, copyErr := io.Copy(out, &rawBuffer); copyErr != nil {
				logger.Error(ctx, logging.EventAWMRun, "stage", "raw_output_copy", "subcommand", subcommand, "ok", false, "error_code", v1.ErrCodeWriteFailed)
			}
			logger.Info(ctx, logging.EventAWMRun, "stage", "finish", "subcommand", subcommand, "exit_code", code)
			return code
		}

		content, err := extractExportContent(rawBuffer.Bytes())
		if err != nil {
			logger.Error(ctx, logging.EventAWMRun, "stage", "raw_output_parse", "subcommand", subcommand, "ok", false, "error_code", v1.ErrCodeInternalError)
			fmt.Fprintf(os.Stderr, "failed to parse export output: %v\n", err)
			return 1
		}
		if err := emitRawExportContent(out, content, *requestSpec.RawOutput); err != nil {
			logger.Error(ctx, logging.EventAWMRun, "stage", "raw_output_write", "subcommand", subcommand, "ok", false, "error_code", v1.ErrCodeWriteFailed)
			fmt.Fprintf(os.Stderr, "failed to write export output: %v\n", err)
			return 1
		}
	}
	logger.Info(ctx, logging.EventAWMRun, "stage", "finish", "subcommand", subcommand, "exit_code", code)
	return code
}

func buildConvenienceEnvelope(subcommand string, args []string, now func() time.Time) (v1.CommandEnvelope, error) {
	request, err := buildConvenienceRequest(subcommand, args, now)
	if err != nil {
		return v1.CommandEnvelope{}, err
	}
	return request.Envelope, nil
}

func buildConvenienceRequest(subcommand string, args []string, now func() time.Time) (convenienceBuildResult, error) {
	route, ok := lookupRouteSpec(subcommand)
	if !ok {
		return convenienceBuildResult{}, fmt.Errorf("unknown subcommand: %s", subcommand)
	}
	return route.Build(args, now)
}

func buildContextRequest(args []string, now func() time.Time) (convenienceBuildResult, error) {
	return buildContextCommandRequest(
		"context",
		"awm context [--project <id>] [--task-text <text>|--task-file <path>] [--tags-file <path>] [--scope-path <path>]... [--format <json|markdown>] [--out-file <path>] [--force[=true|false]]",
		"awm context --task-text \"Add sync checks\" --phase execute",
		v1.CommandContext,
		args,
		now,
	)
}

func buildContextCommandRequest(subcommand, usage, example string, command v1.Command, args []string, now func() time.Time) (convenienceBuildResult, error) {
	fs := newCommandFlagSet(
		subcommand,
		usage,
		example,
	)
	projectID, requestID := addProjectAndRequestFlags(fs)
	taskText := fs.String("task-text", "", "task description")
	taskFile := fs.String("task-file", "", "file containing task text ('-' for stdin)")
	phase := fs.String("phase", string(v1.PhaseExecute), "phase: plan|execute|review")
	tagsFile := fs.String("tags-file", "", "explicit canonical tag dictionary file path (overrides default discovery)")
	exportFlags := addReadSurfaceExportFlags(fs)
	var scopePaths repeatedStringFlag
	fs.Var(&scopePaths, "scope-path", "known repository-relative path to seed into initial scope (repeatable)")
	if err := parseCommandFlags(fs, args); err != nil {
		return convenienceBuildResult{}, err
	}
	trimmedTaskText := strings.TrimSpace(*taskText)
	trimmedTaskFile := strings.TrimSpace(*taskFile)
	if trimmedTaskText != "" && trimmedTaskFile != "" {
		return convenienceBuildResult{}, errors.New("use only one of --task-text or --task-file")
	}
	if trimmedTaskText == "" {
		if trimmedTaskFile == "" {
			return convenienceBuildResult{}, errors.New("--task-text or --task-file is required")
		}
		blob, err := readTextFile(trimmedTaskFile)
		if err != nil {
			return convenienceBuildResult{}, fmt.Errorf("read --task-file %s: %w", trimmedTaskFile, err)
		}
		trimmedTaskText = strings.TrimSpace(blob)
	}
	if trimmedTaskText == "" {
		return convenienceBuildResult{}, errors.New("task text must not be empty")
	}
	if err := exportFlags.Validate(); err != nil {
		return convenienceBuildResult{}, err
	}

	payload := v1.ContextPayload{
		ProjectID: strings.TrimSpace(*projectID),
		TaskText:  trimmedTaskText,
		Phase:     v1.Phase(strings.TrimSpace(*phase)),
		TagsFile:  strings.TrimSpace(*tagsFile),
	}
	if values := scopePaths.Values(); len(values) > 0 {
		payload.InitialScopePaths = append([]string(nil), values...)
	}

	if exportFlags.Enabled() {
		exportPayload := v1.ExportPayload{
			ProjectID: payload.ProjectID,
			Format:    exportFlags.Format,
			Context: &v1.ExportContextSelector{
				TaskText:          payload.TaskText,
				Phase:             payload.Phase,
				TagsFile:          payload.TagsFile,
				InitialScopePaths: append([]string(nil), payload.InitialScopePaths...),
			},
		}
		env, err := buildEnvelope(v1.CommandExport, *requestID, exportPayload, now)
		if err != nil {
			return convenienceBuildResult{}, err
		}
		return convenienceBuildResult{Envelope: env, RawOutput: exportFlags.RawOutput()}, nil
	}

	env, err := buildEnvelope(command, *requestID, payload, now)
	if err != nil {
		return convenienceBuildResult{}, err
	}
	return convenienceBuildResult{Envelope: env}, nil
}

func buildFetchRequest(args []string, now func() time.Time) (convenienceBuildResult, error) {
	fs := newCommandFlagSet(
		"fetch",
		"awm fetch [--project <id>] [--key <pointer>]... [--keys-file <path>] [--keys-json <json>] [--receipt-id <id>] [--expect <key=version>]... [--expected-versions-file <path>] [--expected-versions-json <json>] [--format <json|markdown>] [--out-file <path>] [--force[=true|false]]",
		"awm fetch --key plan:req-12345678 --expect plan:req-12345678=v3",
	)
	projectID, requestID := addProjectAndRequestFlags(fs)
	receiptID := fs.String("receipt-id", "", "receipt ID to fetch via shorthand")
	keysFile := fs.String("keys-file", "", "JSON file containing an array of keys")
	keysJSON := fs.String("keys-json", "", "inline JSON array of keys")
	expectedVersionsFile := fs.String("expected-versions-file", "", "JSON file containing an object map of key->version")
	expectedVersionsJSON := fs.String("expected-versions-json", "", "inline JSON object map of key->version")
	exportFlags := addReadSurfaceExportFlags(fs)
	var keys repeatedStringFlag
	var expects repeatedStringFlag
	fs.Var(&keys, "key", "key to fetch (repeatable)")
	fs.Var(&expects, "expect", "expected version in key=version form (repeatable)")
	if err := parseCommandFlags(fs, args); err != nil {
		return convenienceBuildResult{}, err
	}
	if err := exportFlags.Validate(); err != nil {
		return convenienceBuildResult{}, err
	}
	allKeys := keys.Values()
	if trimmedKeysFile := strings.TrimSpace(*keysFile); trimmedKeysFile != "" {
		fileKeys, err := readStringListFromFile(trimmedKeysFile, "--keys-file")
		if err != nil {
			return convenienceBuildResult{}, err
		}
		allKeys = mergeUnique(allKeys, fileKeys)
	}
	if trimmedKeysJSON := strings.TrimSpace(*keysJSON); trimmedKeysJSON != "" {
		inlineKeys, err := readStringListFromJSON(trimmedKeysJSON, "--keys-json")
		if err != nil {
			return convenienceBuildResult{}, err
		}
		allKeys = mergeUnique(allKeys, inlineKeys)
	}

	expectedVersions := map[string]string{}
	if trimmedExpectedVersionsFile := strings.TrimSpace(*expectedVersionsFile); trimmedExpectedVersionsFile != "" {
		fileMap, err := readStringMapFromFile(trimmedExpectedVersionsFile, "--expected-versions-file")
		if err != nil {
			return convenienceBuildResult{}, err
		}
		maps.Copy(expectedVersions, fileMap)
	}
	if trimmedExpectedVersionsJSON := strings.TrimSpace(*expectedVersionsJSON); trimmedExpectedVersionsJSON != "" {
		inlineMap, err := readStringMapFromJSON(trimmedExpectedVersionsJSON, "--expected-versions-json")
		if err != nil {
			return convenienceBuildResult{}, err
		}
		maps.Copy(expectedVersions, inlineMap)
	}
	for _, expectedArg := range expects {
		key, version, err := parseExpectedVersion(expectedArg)
		if err != nil {
			return convenienceBuildResult{}, err
		}
		expectedVersions[key] = version
	}

	payload := v1.FetchPayload{
		ProjectID: strings.TrimSpace(*projectID),
		Keys:      allKeys,
		ReceiptID: strings.TrimSpace(*receiptID),
	}
	if len(allKeys) == 0 {
		payload.Keys = nil
	}
	if len(expectedVersions) > 0 {
		payload.ExpectedVersions = expectedVersions
	}

	if exportFlags.Enabled() {
		exportPayload := v1.ExportPayload{
			ProjectID: payload.ProjectID,
			Format:    exportFlags.Format,
			Fetch: &v1.ExportFetchSelector{
				Keys:             append([]string(nil), payload.Keys...),
				ReceiptID:        payload.ReceiptID,
				ExpectedVersions: cloneStringMap(payload.ExpectedVersions),
			},
		}
		env, err := buildEnvelope(v1.CommandExport, *requestID, exportPayload, now)
		if err != nil {
			return convenienceBuildResult{}, err
		}
		return convenienceBuildResult{Envelope: env, RawOutput: exportFlags.RawOutput()}, nil
	}

	env, err := buildEnvelope(v1.CommandFetch, *requestID, payload, now)
	if err != nil {
		return convenienceBuildResult{}, err
	}
	return convenienceBuildResult{Envelope: env}, nil
}

func buildHistorySearchRequest(subcommand string, args []string, now func() time.Time) (convenienceBuildResult, error) {
	usageLine := "awm history [--project <id>] [--entity <all|work|receipt|run>] [--query <text>|--query-file <path>] [--scope <current|deferred|completed|all>] [--kind <kind>] [--limit <n>] [--unbounded[=true|false]] [--format <json|markdown>] [--out-file <path>] [--force[=true|false]]"
	example := "awm history --entity work --scope current --query \"MCP parity\""
	defaultEntity := v1.HistoryEntityAll

	fs := newCommandFlagSet(subcommand, usageLine, example)
	projectID, requestID := addProjectAndRequestFlags(fs)
	entity := fs.String("entity", string(defaultEntity), "history entity: all|work|receipt|run")
	query := fs.String("query", "", "search text applied to plan, receipt, and run summaries")
	queryFile := fs.String("query-file", "", "file containing search text ('-' for stdin)")
	limit := fs.Int("limit", 20, "maximum number of plans to return (1-100)")
	unbounded := optionalBoolFlag{}
	scope := fs.String("scope", "", "work-history scope when --entity=work: current|deferred|completed|all")
	kind := fs.String("kind", "", "optional work-plan kind filter when --entity=work")
	exportFlags := addReadSurfaceExportFlags(fs)
	fs.Var(&unbounded, "unbounded", "remove built-in history result caps (optional bool)")
	if err := parseCommandFlags(fs, args); err != nil {
		return convenienceBuildResult{}, err
	}
	if err := exportFlags.Validate(); err != nil {
		return convenienceBuildResult{}, err
	}
	entityValue := strings.TrimSpace(*entity)
	trimmedQuery := strings.TrimSpace(*query)
	trimmedQueryFile := strings.TrimSpace(*queryFile)
	trimmedScope := strings.TrimSpace(*scope)
	trimmedKind := strings.TrimSpace(*kind)
	if trimmedQuery != "" && trimmedQueryFile != "" {
		return convenienceBuildResult{}, errors.New("use only one of --query or --query-file")
	}
	if trimmedQuery == "" && trimmedQueryFile != "" {
		blob, err := readTextFile(trimmedQueryFile)
		if err != nil {
			return convenienceBuildResult{}, fmt.Errorf("read --query-file %s: %w", trimmedQueryFile, err)
		}
		trimmedQuery = strings.TrimSpace(blob)
	}
	payload := v1.HistorySearchPayload{
		ProjectID: strings.TrimSpace(*projectID),
		Entity:    v1.HistoryEntity(entityValue),
		Query:     trimmedQuery,
	}
	if payload.Entity == v1.HistoryEntityWork {
		payload.Scope = v1.HistoryScope(trimmedScope)
	}
	if trimmedKind != "" {
		if payload.Entity != v1.HistoryEntityWork {
			return convenienceBuildResult{}, errors.New("kind is only supported when entity=work")
		}
		payload.Kind = trimmedKind
	}
	if trimmedScope != "" && payload.Entity != v1.HistoryEntityWork {
		return convenienceBuildResult{}, errors.New("scope is only supported when entity=work")
	}
	if *limit > 0 {
		payload.Limit = *limit
	}
	if unbounded.set {
		value := unbounded.value
		payload.Unbounded = &value
	}

	if exportFlags.Enabled() {
		exportPayload := v1.ExportPayload{
			ProjectID: payload.ProjectID,
			Format:    exportFlags.Format,
			History: &v1.ExportHistorySelector{
				Entity:    payload.Entity,
				Query:     payload.Query,
				Scope:     payload.Scope,
				Kind:      payload.Kind,
				Limit:     payload.Limit,
				Unbounded: payload.Unbounded,
			},
		}
		env, err := buildEnvelope(v1.CommandExport, *requestID, exportPayload, now)
		if err != nil {
			return convenienceBuildResult{}, err
		}
		return convenienceBuildResult{Envelope: env, RawOutput: exportFlags.RawOutput()}, nil
	}

	env, err := buildEnvelope(v1.CommandHistorySearch, *requestID, payload, now)
	if err != nil {
		return convenienceBuildResult{}, err
	}
	return convenienceBuildResult{Envelope: env}, nil
}

func buildStatusRequest(args []string, now func() time.Time) (convenienceBuildResult, error) {
	return buildStatusRequestForCommand(
		"status",
		"awm status [--project <id>] [--project-root <path>] [--rules-file <path>] [--tags-file <path>] [--tests-file <path>] [--workflows-file <path>] [--task-text <text>|--task-file <path>] [--phase <plan|execute|review>] [--format <json|markdown>] [--out-file <path>] [--force[=true|false]]",
		"awm status --task-text \"add review gate\" --phase execute",
		args,
		now,
	)
}

func buildStatusRequestForCommand(commandName, usageLine, example string, args []string, now func() time.Time) (convenienceBuildResult, error) {
	fs := newCommandFlagSet(
		commandName,
		usageLine,
		example,
	)
	projectID, requestID := addProjectAndRequestFlags(fs)
	projectRoot := fs.String("project-root", "", "project root")
	rulesFile := fs.String("rules-file", "", "explicit canonical rules file path (overrides default discovery)")
	tagsFile := fs.String("tags-file", "", "explicit canonical tag dictionary file path (overrides default discovery)")
	testsFile := fs.String("tests-file", "", "explicit verify tests file path (overrides default discovery)")
	workflowsFile := fs.String("workflows-file", "", "explicit workflow definitions file path (overrides default discovery)")
	taskText := fs.String("task-text", "", "optional task text to preview simplified context load")
	taskFile := fs.String("task-file", "", "optional file containing task text ('-' for stdin)")
	phase := fs.String("phase", string(v1.PhaseExecute), "phase: plan|execute|review")
	exportFlags := addReadSurfaceExportFlags(fs)
	if err := parseCommandFlags(fs, args); err != nil {
		return convenienceBuildResult{}, err
	}
	if err := exportFlags.Validate(); err != nil {
		return convenienceBuildResult{}, err
	}
	trimmedTaskText := strings.TrimSpace(*taskText)
	trimmedTaskFile := strings.TrimSpace(*taskFile)
	if trimmedTaskText != "" && trimmedTaskFile != "" {
		return convenienceBuildResult{}, errors.New("use only one of --task-text or --task-file")
	}
	if trimmedTaskText == "" && trimmedTaskFile != "" {
		blob, err := readTextFile(trimmedTaskFile)
		if err != nil {
			return convenienceBuildResult{}, fmt.Errorf("read --task-file %s: %w", trimmedTaskFile, err)
		}
		trimmedTaskText = strings.TrimSpace(blob)
	}

	payload := v1.StatusPayload{
		ProjectID:     strings.TrimSpace(*projectID),
		ProjectRoot:   strings.TrimSpace(*projectRoot),
		RulesFile:     strings.TrimSpace(*rulesFile),
		TagsFile:      strings.TrimSpace(*tagsFile),
		TestsFile:     strings.TrimSpace(*testsFile),
		WorkflowsFile: strings.TrimSpace(*workflowsFile),
		TaskText:      trimmedTaskText,
		Phase:         v1.Phase(strings.TrimSpace(*phase)),
	}
	if payload.TaskText == "" {
		payload.Phase = ""
	}

	if exportFlags.Enabled() {
		exportPayload := v1.ExportPayload{
			ProjectID: payload.ProjectID,
			Format:    exportFlags.Format,
			Status: &v1.ExportStatusSelector{
				ProjectRoot:   payload.ProjectRoot,
				RulesFile:     payload.RulesFile,
				TagsFile:      payload.TagsFile,
				TestsFile:     payload.TestsFile,
				WorkflowsFile: payload.WorkflowsFile,
				TaskText:      payload.TaskText,
				Phase:         payload.Phase,
			},
		}
		env, err := buildEnvelope(v1.CommandExport, *requestID, exportPayload, now)
		if err != nil {
			return convenienceBuildResult{}, err
		}
		return convenienceBuildResult{Envelope: env, RawOutput: exportFlags.RawOutput()}, nil
	}

	env, err := buildEnvelope(v1.CommandStatus, *requestID, payload, now)
	if err != nil {
		return convenienceBuildResult{}, err
	}
	return convenienceBuildResult{Envelope: env}, nil
}

func buildWorkEnvelope(args []string, now func() time.Time) (v1.CommandEnvelope, error) {
	fs := newCommandFlagSet(
		"work",
		"awm work [--project <id>] [--plan-key <key>|--receipt-id <id>] [--plan-title <text>] [--mode <merge|replace>] [--discovered-path <path>]... [--plan-file <path>|--plan-json <json>] [--tasks-file <path>|--tasks-json <json>]",
		"awm work --receipt-id req-12345678 --tasks-json '[{\"key\":\"verify:tests\",\"summary\":\"Run tests\",\"status\":\"pending\"}]'",
	)
	projectID, requestID := addProjectAndRequestFlags(fs)
	planKey := fs.String("plan-key", "", "plan key")
	planTitle := fs.String("plan-title", "", "plan title")
	receiptID := fs.String("receipt-id", "", "receipt ID")
	mode := fs.String("mode", "", "work plan update mode: merge|replace")
	planFile := fs.String("plan-file", "", "JSON file containing work plan metadata")
	planJSON := fs.String("plan-json", "", "inline JSON object containing work plan metadata")
	tasksFile := fs.String("tasks-file", "", "JSON file containing an array of plan tasks")
	tasksJSON := fs.String("tasks-json", "", "inline JSON array containing plan tasks")
	var discoveredPaths repeatedStringFlag
	fs.Var(&discoveredPaths, "discovered-path", "repo-relative path discovered after context that should be added to plan.discovered_paths (repeatable)")
	if err := parseCommandFlags(fs, args); err != nil {
		return v1.CommandEnvelope{}, err
	}
	payload := v1.WorkPayload{
		ProjectID: strings.TrimSpace(*projectID),
		PlanKey:   strings.TrimSpace(*planKey),
		PlanTitle: strings.TrimSpace(*planTitle),
		ReceiptID: strings.TrimSpace(*receiptID),
		Mode:      v1.WorkPlanMode(strings.TrimSpace(*mode)),
	}
	trimmedPlanFile := strings.TrimSpace(*planFile)
	trimmedPlanJSON := strings.TrimSpace(*planJSON)
	if trimmedPlanFile != "" && trimmedPlanJSON != "" {
		return v1.CommandEnvelope{}, errors.New("use only one of --plan-file or --plan-json")
	}
	if trimmedPlanFile != "" {
		plan, err := readWorkPlanFromFile(trimmedPlanFile)
		if err != nil {
			return v1.CommandEnvelope{}, err
		}
		payload.Plan = &plan
	}
	if trimmedPlanJSON != "" {
		plan, err := readWorkPlanFromJSON(trimmedPlanJSON)
		if err != nil {
			return v1.CommandEnvelope{}, err
		}
		payload.Plan = &plan
	}
	if values := discoveredPaths.Values(); len(values) > 0 {
		if payload.Plan == nil {
			payload.Plan = &v1.WorkPlanPayload{}
		}
		payload.Plan.DiscoveredPaths = mergeUnique(payload.Plan.DiscoveredPaths, values)
	}

	trimmedTasksFile := strings.TrimSpace(*tasksFile)
	trimmedTasksJSON := strings.TrimSpace(*tasksJSON)
	if trimmedTasksFile != "" && trimmedTasksJSON != "" {
		return v1.CommandEnvelope{}, errors.New("use only one of --tasks-file or --tasks-json")
	}
	if trimmedTasksFile != "" {
		tasks, err := readWorkTasksFromFile(trimmedTasksFile)
		if err != nil {
			return v1.CommandEnvelope{}, err
		}
		payload.Tasks = tasks
	}
	if trimmedTasksJSON != "" {
		tasks, err := readWorkTasksFromJSON(trimmedTasksJSON)
		if err != nil {
			return v1.CommandEnvelope{}, err
		}
		payload.Tasks = tasks
	}

	return buildEnvelope(v1.CommandWork, *requestID, payload, now)
}

func buildDoneEnvelope(args []string, now func() time.Time) (v1.CommandEnvelope, error) {
	return buildDoneCommandEnvelope(
		"done",
		"awm done [--project <id>] [--receipt-id <id>|--plan-key <key>] [--outcome <text>|--outcome-file <path>] [--file-changed <path>]... [--files-changed-file <path>] [--files-changed-json <json>] [--no-file-changes[=true|false]] [--scope-mode <mode>] [--tags-file <path>]",
		"awm done --project myproject --plan-key plan:req-12345678 --file-changed cmd/awm/main.go --outcome \"Done\"",
		v1.CommandDone,
		args,
		now,
	)
}

func buildDoneCommandEnvelope(subcommand, usage, example string, command v1.Command, args []string, now func() time.Time) (v1.CommandEnvelope, error) {
	fs := newCommandFlagSet(
		subcommand,
		usage,
		example,
	)
	projectID, requestID := addProjectAndRequestFlags(fs)
	receiptID := fs.String("receipt-id", "", "receipt ID")
	planKey := fs.String("plan-key", "", "plan key")
	outcome := fs.String("outcome", "", "completion outcome summary")
	outcomeFile := fs.String("outcome-file", "", "file containing completion outcome ('-' for stdin)")
	scopeMode := fs.String("scope-mode", "", "scope mode: strict|warn")
	tagsFile := fs.String("tags-file", "", "explicit canonical tag dictionary file path (overrides default discovery)")
	filesChangedFile := fs.String("files-changed-file", "", "JSON file containing an array of changed file paths")
	filesChangedJSON := fs.String("files-changed-json", "", "inline JSON array of changed file paths")
	var noFileChanges optionalBoolFlag
	fs.Var(&noFileChanges, "no-file-changes", "explicitly declare that the task produced no file changes; use when auto-detection is unavailable or you want explicit no-file intent")
	var filesChanged repeatedStringFlag
	fs.Var(&filesChanged, "file-changed", "repository-relative changed file path (repeatable)")
	if err := parseCommandFlags(fs, args); err != nil {
		return v1.CommandEnvelope{}, err
	}
	if strings.TrimSpace(*receiptID) == "" && strings.TrimSpace(*planKey) == "" {
		return v1.CommandEnvelope{}, errors.New("done requires --receipt-id or --plan-key")
	}

	trimmedOutcome := strings.TrimSpace(*outcome)
	trimmedOutcomeFile := strings.TrimSpace(*outcomeFile)
	if trimmedOutcome != "" && trimmedOutcomeFile != "" {
		return v1.CommandEnvelope{}, errors.New("use only one of --outcome or --outcome-file")
	}
	if trimmedOutcome == "" {
		if trimmedOutcomeFile == "" {
			return v1.CommandEnvelope{}, errors.New("--outcome or --outcome-file is required")
		}
		blob, err := readTextFile(trimmedOutcomeFile)
		if err != nil {
			return v1.CommandEnvelope{}, fmt.Errorf("read --outcome-file %s: %w", trimmedOutcomeFile, err)
		}
		trimmedOutcome = strings.TrimSpace(blob)
	}
	if trimmedOutcome == "" {
		return v1.CommandEnvelope{}, errors.New("outcome must not be empty")
	}

	allFilesChanged := filesChanged.Values()
	if trimmedFilesChangedFile := strings.TrimSpace(*filesChangedFile); trimmedFilesChangedFile != "" {
		fileValues, err := readStringListFromFile(trimmedFilesChangedFile, "--files-changed-file")
		if err != nil {
			return v1.CommandEnvelope{}, err
		}
		allFilesChanged = mergeUnique(allFilesChanged, fileValues)
	}
	if trimmedFilesChangedJSON := strings.TrimSpace(*filesChangedJSON); trimmedFilesChangedJSON != "" {
		inlineValues, err := readStringListFromJSON(trimmedFilesChangedJSON, "--files-changed-json")
		if err != nil {
			return v1.CommandEnvelope{}, err
		}
		allFilesChanged = mergeUnique(allFilesChanged, inlineValues)
	}
	if noFileChanges.set && noFileChanges.value && len(allFilesChanged) > 0 {
		return v1.CommandEnvelope{}, errors.New("--no-file-changes cannot be combined with changed file inputs")
	}

	payload := v1.DonePayload{
		ProjectID:     strings.TrimSpace(*projectID),
		ReceiptID:     strings.TrimSpace(*receiptID),
		PlanKey:       strings.TrimSpace(*planKey),
		TagsFile:      strings.TrimSpace(*tagsFile),
		FilesChanged:  allFilesChanged,
		NoFileChanges: noFileChanges.set && noFileChanges.value,
		Outcome:       trimmedOutcome,
	}
	if trimmedScopeMode := strings.TrimSpace(*scopeMode); trimmedScopeMode != "" {
		payload.ScopeMode = v1.ScopeMode(trimmedScopeMode)
	}

	return buildEnvelope(command, *requestID, payload, now)
}

func buildReviewEnvelope(args []string, now func() time.Time) (v1.CommandEnvelope, error) {
	fs := newCommandFlagSet(
		"review",
		"awm review [--project <id>] [--receipt-id <id>|--plan-key <key>] [--run] [--key <task-key>] [--summary <text>] [--status <pending|in_progress|complete|blocked|superseded>] [--outcome <text>|--outcome-file <path>] [--blocked-reason <text>] [--evidence <text>]... [--evidence-file <path>|--evidence-json <json>] [--tags-file <path>]",
		"awm review --receipt-id req-12345678 --run",
	)
	projectID, requestID := addProjectAndRequestFlags(fs)
	receiptID := fs.String("receipt-id", "", "receipt ID")
	planKey := fs.String("plan-key", "", "plan key")
	run := fs.Bool("run", false, "execute the workflow-configured review command before recording the review task")
	key := fs.String("key", "", "review task key")
	summary := fs.String("summary", "", "review task summary")
	status := fs.String("status", "", "review task status: pending|in_progress|complete|blocked|superseded")
	outcome := fs.String("outcome", "", "review outcome summary")
	outcomeFile := fs.String("outcome-file", "", "file containing review outcome ('-' for stdin)")
	blockedReason := fs.String("blocked-reason", "", "why the review gate is blocked")
	evidenceFile := fs.String("evidence-file", "", "JSON file containing an array of evidence strings")
	evidenceJSON := fs.String("evidence-json", "", "inline JSON array of evidence strings")
	tagsFile := fs.String("tags-file", "", "explicit canonical tag dictionary file path (overrides default discovery)")
	var evidence repeatedStringFlag
	fs.Var(&evidence, "evidence", "evidence item recorded on the review task (repeatable)")
	if err := parseCommandFlags(fs, args); err != nil {
		return v1.CommandEnvelope{}, err
	}
	if strings.TrimSpace(*receiptID) == "" && strings.TrimSpace(*planKey) == "" {
		return v1.CommandEnvelope{}, errors.New("review requires --receipt-id or --plan-key")
	}

	trimmedOutcome := strings.TrimSpace(*outcome)
	trimmedOutcomeFile := strings.TrimSpace(*outcomeFile)
	if trimmedOutcome != "" && trimmedOutcomeFile != "" {
		return v1.CommandEnvelope{}, errors.New("use only one of --outcome or --outcome-file")
	}
	if !*run && trimmedOutcome == "" && trimmedOutcomeFile != "" {
		blob, err := readTextFile(trimmedOutcomeFile)
		if err != nil {
			return v1.CommandEnvelope{}, fmt.Errorf("read --outcome-file %s: %w", trimmedOutcomeFile, err)
		}
		trimmedOutcome = strings.TrimSpace(blob)
	}

	allEvidence := evidence.Values()
	if strings.TrimSpace(*evidenceFile) != "" && strings.TrimSpace(*evidenceJSON) != "" {
		return v1.CommandEnvelope{}, errors.New("use only one of --evidence-file or --evidence-json")
	}
	if trimmedEvidenceFile := strings.TrimSpace(*evidenceFile); trimmedEvidenceFile != "" {
		fileValues, err := readStringListFromFile(trimmedEvidenceFile, "--evidence-file")
		if err != nil {
			return v1.CommandEnvelope{}, err
		}
		allEvidence = mergeUnique(allEvidence, fileValues)
	}
	if trimmedEvidenceJSON := strings.TrimSpace(*evidenceJSON); trimmedEvidenceJSON != "" {
		inlineValues, err := readStringListFromJSON(trimmedEvidenceJSON, "--evidence-json")
		if err != nil {
			return v1.CommandEnvelope{}, err
		}
		allEvidence = mergeUnique(allEvidence, inlineValues)
	}
	if *run {
		if trimmedOutcome != "" || trimmedOutcomeFile != "" {
			return v1.CommandEnvelope{}, errors.New("--outcome and --outcome-file are only supported when --run is omitted")
		}
		if strings.TrimSpace(*blockedReason) != "" {
			return v1.CommandEnvelope{}, errors.New("--blocked-reason is only supported when --run is omitted")
		}
		if len(allEvidence) > 0 {
			return v1.CommandEnvelope{}, errors.New("--evidence, --evidence-file, and --evidence-json are only supported when --run is omitted")
		}
		if strings.TrimSpace(*status) != "" {
			return v1.CommandEnvelope{}, errors.New("--status is only supported when --run is omitted")
		}
	}

	payload := v1.ReviewPayload{
		ProjectID:     strings.TrimSpace(*projectID),
		ReceiptID:     strings.TrimSpace(*receiptID),
		PlanKey:       strings.TrimSpace(*planKey),
		Key:           strings.TrimSpace(*key),
		Summary:       strings.TrimSpace(*summary),
		Status:        v1.WorkItemStatus(strings.TrimSpace(*status)),
		Outcome:       trimmedOutcome,
		BlockedReason: strings.TrimSpace(*blockedReason),
		Evidence:      allEvidence,
		Run:           *run,
		TagsFile:      strings.TrimSpace(*tagsFile),
	}
	return buildEnvelope(v1.CommandReview, *requestID, payload, now)
}

func buildSyncEnvelope(args []string, now func() time.Time) (v1.CommandEnvelope, error) {
	fs := newCommandFlagSet(
		"sync",
		"awm sync [--project <id>] [--mode changed|full|working_tree] [--git-range <range>] [--project-root <path>] [--rules-file <path>] [--tags-file <path>] [--insert-new-candidates[=true|false]]",
		"awm sync --mode changed --git-range HEAD~1..HEAD",
	)
	projectID, requestID := addProjectAndRequestFlags(fs)
	mode := fs.String("mode", "", "sync mode: changed|full|working_tree")
	gitRange := fs.String("git-range", "", "git revision range")
	projectRoot := fs.String("project-root", "", "project root for sync")
	rulesFile := fs.String("rules-file", "", "explicit canonical rules file path (overrides default discovery)")
	tagsFile := fs.String("tags-file", "", "explicit canonical tag dictionary file path (overrides default discovery)")
	insertNewCandidates := optionalBoolFlag{}
	fs.Var(&insertNewCandidates, "insert-new-candidates", "auto-index unindexed files as pointer stubs (optional bool)")
	if err := parseCommandFlags(fs, args); err != nil {
		return v1.CommandEnvelope{}, err
	}
	payload := v1.SyncPayload{
		ProjectID:   strings.TrimSpace(*projectID),
		Mode:        strings.TrimSpace(*mode),
		GitRange:    strings.TrimSpace(*gitRange),
		ProjectRoot: strings.TrimSpace(*projectRoot),
		RulesFile:   strings.TrimSpace(*rulesFile),
		TagsFile:    strings.TrimSpace(*tagsFile),
	}
	if insertNewCandidates.IsSet() {
		payload.InsertNewCandidates = insertNewCandidates.Ptr()
	}

	return buildEnvelope(v1.CommandSync, *requestID, payload, now)
}

func buildHealthEnvelope(args []string, now func() time.Time) (v1.CommandEnvelope, error) {
	fs := newCommandFlagSet(
		"health",
		"awm health [--project <id>] [--include-details[=true|false]] [--max-findings-per-check <n>] | [--fix <name>]... [--dry-run[=true|false]] [--apply[=true|false]] [--project-root <path>] [--rules-file <path>] [--tags-file <path>]",
		"awm health --include-details --max-findings-per-check 50",
		"awm health --fix sync_ruleset",
		"awm health --fix all --dry-run",
		"awm health --fix sync_ruleset --dry-run",
	)
	projectID, requestID := addProjectAndRequestFlags(fs)
	includeDetails := optionalBoolFlag{}
	fs.Var(&includeDetails, "include-details", "include detailed findings in check mode (optional bool)")
	maxFindingsPerCheck := fs.Int("max-findings-per-check", -1, "maximum findings per check (1..500)")
	fixOptions := addHealthFixCLIFlags(fs)
	if err := parseCommandFlags(fs, args); err != nil {
		return v1.CommandEnvelope{}, err
	}
	payload, err := buildHealthPayload(strings.TrimSpace(*projectID), includeDetails, *maxFindingsPerCheck, fixOptions)
	if err != nil {
		return v1.CommandEnvelope{}, err
	}
	return buildEnvelope(v1.CommandHealth, *requestID, payload, now)
}

func buildVerifyEnvelope(args []string, now func() time.Time) (v1.CommandEnvelope, error) {
	fs := newCommandFlagSet(
		"verify",
		"awm verify [--project <id>] [--receipt-id <id>] [--plan-key <key>] [--phase <plan|execute|review>] [--test-id <id>]... [--file-changed <path>]... [--files-changed-file <path>|--files-changed-json <json>] [--tests-file <path>] [--tags-file <path>] [--dry-run]",
		"awm verify --phase review --file-changed internal/service/backend/service.go --dry-run",
	)
	projectID, requestID := addProjectAndRequestFlags(fs)
	receiptID := fs.String("receipt-id", "", "receipt ID")
	planKey := fs.String("plan-key", "", "plan key")
	phase := fs.String("phase", "", "phase: plan|execute|review")
	testsFile := fs.String("tests-file", "", "explicit verification definitions file path (overrides default discovery)")
	tagsFile := fs.String("tags-file", "", "explicit canonical tag dictionary file path (overrides default discovery)")
	filesChangedFile := fs.String("files-changed-file", "", "JSON file containing an array of changed files")
	filesChangedJSON := fs.String("files-changed-json", "", "inline JSON array of changed files")
	dryRun := fs.Bool("dry-run", false, "select tests without executing them")
	var testIDs repeatedStringFlag
	var filesChanged repeatedStringFlag
	fs.Var(&testIDs, "test-id", "verification test id (repeatable)")
	fs.Var(&filesChanged, "file-changed", "repository-relative changed file path (repeatable)")
	if err := parseCommandFlags(fs, args); err != nil {
		return v1.CommandEnvelope{}, err
	}
	allFilesChanged := filesChanged.Values()
	if trimmedFilesChangedFile := strings.TrimSpace(*filesChangedFile); trimmedFilesChangedFile != "" {
		fileValues, err := readStringListFromFile(trimmedFilesChangedFile, "--files-changed-file")
		if err != nil {
			return v1.CommandEnvelope{}, err
		}
		allFilesChanged = mergeUnique(allFilesChanged, fileValues)
	}
	if trimmedFilesChangedJSON := strings.TrimSpace(*filesChangedJSON); trimmedFilesChangedJSON != "" {
		inlineValues, err := readStringListFromJSON(trimmedFilesChangedJSON, "--files-changed-json")
		if err != nil {
			return v1.CommandEnvelope{}, err
		}
		allFilesChanged = mergeUnique(allFilesChanged, inlineValues)
	}

	payload := v1.VerifyPayload{
		ProjectID:    strings.TrimSpace(*projectID),
		ReceiptID:    strings.TrimSpace(*receiptID),
		PlanKey:      strings.TrimSpace(*planKey),
		TestIDs:      mergeUnique(nil, testIDs.Values()),
		FilesChanged: allFilesChanged,
		TestsFile:    strings.TrimSpace(*testsFile),
		TagsFile:     strings.TrimSpace(*tagsFile),
		DryRun:       *dryRun,
	}
	if trimmedPhase := strings.TrimSpace(*phase); trimmedPhase != "" {
		payload.Phase = v1.Phase(trimmedPhase)
	}
	if len(payload.TestIDs) == 0 && payload.ReceiptID == "" && payload.PlanKey == "" && payload.Phase == "" && len(payload.FilesChanged) == 0 {
		return v1.CommandEnvelope{}, errors.New("verify requires --test-id or selection context (--receipt-id, --plan-key, --phase, or --file-changed)")
	}

	return buildEnvelope(v1.CommandVerify, *requestID, payload, now)
}

func buildInitEnvelope(args []string, now func() time.Time) (v1.CommandEnvelope, error) {
	return buildInitCommandEnvelope(
		"init",
		"awm init [--project <id>] [--project-root <path>] [--apply-template <id>]... [--rules-file <path>] [--tags-file <path>] [--persist-candidates[=true|false]] [--respect-gitignore[=true|false]] [--output-candidates-path <path>]",
		"awm init --project-root . --apply-template starter-contract --apply-template verify-generic",
		v1.CommandInit,
		args,
		now,
	)
}

func buildInitCommandEnvelope(subcommand, usage, example string, command v1.Command, args []string, now func() time.Time) (v1.CommandEnvelope, error) {
	fs := newCommandFlagSet(
		subcommand,
		usage,
		example,
	)
	projectID, requestID := addProjectAndRequestFlags(fs)
	projectRoot := fs.String("project-root", "", "project root to analyze")
	rulesFile := fs.String("rules-file", "", "explicit canonical rules file path (overrides default discovery)")
	tagsFile := fs.String("tags-file", "", "explicit canonical tag dictionary file path (overrides default discovery)")
	var applyTemplates repeatedStringFlag
	fs.Var(&applyTemplates, "apply-template", "init template to apply (repeatable)")
	persistCandidates := optionalBoolFlag{}
	fs.Var(&persistCandidates, "persist-candidates", "persist init candidates to disk (optional bool)")
	respectGitIgnore := optionalBoolFlag{}
	fs.Var(&respectGitIgnore, "respect-gitignore", "respect .gitignore while scanning (optional bool)")
	outputCandidatesPath := fs.String("output-candidates-path", "", "output file for init candidates (implies persistence)")
	if err := parseCommandFlags(fs, args); err != nil {
		return v1.CommandEnvelope{}, err
	}
	payload := v1.InitPayload{
		ProjectID:      strings.TrimSpace(*projectID),
		ProjectRoot:    strings.TrimSpace(*projectRoot),
		RulesFile:      strings.TrimSpace(*rulesFile),
		TagsFile:       strings.TrimSpace(*tagsFile),
		ApplyTemplates: mergeUnique(nil, applyTemplates.Values()),
	}
	if respectGitIgnore.IsSet() {
		payload.RespectGitIgnore = respectGitIgnore.Ptr()
	}
	if persistCandidates.IsSet() {
		payload.PersistCandidates = persistCandidates.Ptr()
	}
	if trimmedOutputPath := strings.TrimSpace(*outputCandidatesPath); trimmedOutputPath != "" {
		payload.OutputCandidatesPath = &trimmedOutputPath
	}

	return buildEnvelope(command, *requestID, payload, now)
}

func buildEnvelope(command v1.Command, requestID string, payload any, now func() time.Time) (v1.CommandEnvelope, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return v1.CommandEnvelope{}, err
	}
	trimmedRequestID := strings.TrimSpace(requestID)
	if trimmedRequestID == "" {
		trimmedRequestID = newRequestID(command, now)
	}
	return v1.CommandEnvelope{
		Version:   v1.Version,
		Command:   command,
		RequestID: trimmedRequestID,
		Payload:   payloadJSON,
	}, nil
}

type readSurfaceExportFlags struct {
	formatRaw *string
	outFile   *string
	force     optionalBoolFlag
	Format    v1.ExportFormat
}

func addReadSurfaceExportFlags(fs *flag.FlagSet) *readSurfaceExportFlags {
	flags := &readSurfaceExportFlags{}
	flags.formatRaw = fs.String("format", "", "raw export format: json|markdown")
	flags.outFile = fs.String("out-file", "", "write raw export content to a file instead of stdout")
	fs.Var(&flags.force, "force", "allow overwriting an existing --out-file target (optional bool)")
	return flags
}

func (f *readSurfaceExportFlags) Validate() error {
	if f == nil {
		return nil
	}
	formatValue := strings.TrimSpace(derefString(f.formatRaw))
	outFileValue := strings.TrimSpace(derefString(f.outFile))
	if formatValue == "" {
		if outFileValue != "" || f.force.IsSet() {
			return errors.New("--out-file and --force require --format")
		}
		f.Format = ""
		return nil
	}

	switch v1.ExportFormat(formatValue) {
	case v1.ExportFormatJSON, v1.ExportFormatMarkdown:
		f.Format = v1.ExportFormat(formatValue)
	default:
		return fmt.Errorf("unsupported --format %q (want json or markdown)", formatValue)
	}

	if f.force.IsSet() && outFileValue == "" {
		return errors.New("--force requires --out-file")
	}
	return nil
}

func (f *readSurfaceExportFlags) Enabled() bool {
	return f != nil && f.Format != ""
}

func (f *readSurfaceExportFlags) RawOutput() *rawOutputOptions {
	if !f.Enabled() {
		return nil
	}
	return &rawOutputOptions{
		OutFile: strings.TrimSpace(derefString(f.outFile)),
		Force:   f.force.IsSet() && f.force.value,
	}
}

func extractExportContent(raw []byte) (string, error) {
	var env struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", err
	}
	if !env.OK {
		return "", errors.New("export envelope did not report success")
	}
	var result v1.ExportResult
	if err := json.Unmarshal(env.Result, &result); err != nil {
		return "", err
	}
	return result.Content, nil
}

func emitRawExportContent(out io.Writer, content string, options rawOutputOptions) error {
	if strings.TrimSpace(options.OutFile) == "" {
		_, err := io.WriteString(out, content)
		return err
	}
	return writeRawExportFile(options.OutFile, content, options.Force)
}

func writeRawExportFile(targetPath, content string, force bool) error {
	cleanPath := filepath.Clean(strings.TrimSpace(targetPath))
	if cleanPath == "" || cleanPath == "." {
		return errors.New("--out-file must not be empty")
	}
	if !force {
		if _, err := os.Stat(cleanPath); err == nil {
			return fmt.Errorf("%s already exists; rerun with --force to overwrite", cleanPath)
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	dir := filepath.Dir(cleanPath)
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}

	tempFile, err := os.CreateTemp(dir, filepath.Base(cleanPath)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer func() {
		if tempPath != "" {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := io.WriteString(tempFile, content); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Chmod(0o644); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, cleanPath); err != nil {
		return err
	}
	tempPath = ""
	return nil
}

func newRequestID(command v1.Command, now func() time.Time) string {
	if now == nil {
		now = time.Now
	}
	commandPart := strings.ReplaceAll(string(command), "_", "-")
	return fmt.Sprintf("req-%s-%d", commandPart, now().UTC().UnixNano())
}

func addProjectAndRequestFlags(fs *flag.FlagSet) (*string, *string) {
	projectID := new(string)
	requestID := new(string)
	fs.StringVar(projectID, "project", "", "project identifier (defaults to AWM_PROJECT_ID or inferred repo root name)")
	fs.StringVar(requestID, "request-id", "", "request identifier (defaults to generated value)")
	return projectID, requestID
}

func newCommandFlagSet(name, usageLine string, examples ...string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		out := fs.Output()
		fmt.Fprintln(out, "Usage:")
		fmt.Fprintf(out, "  %s\n", usageLine)
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Flags:")
		fs.PrintDefaults()
		if len(examples) > 0 {
			fmt.Fprintln(out)
			fmt.Fprintln(out, "Examples:")
			for _, example := range examples {
				fmt.Fprintf(out, "  %s\n", example)
			}
		}
	}
	return fs
}

func parseCommandFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	return nil
}

func parseExpectedVersion(raw string) (string, string, error) {
	parts := strings.SplitN(raw, "=", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid --expect value %q: expected key=version", raw)
	}
	key := strings.TrimSpace(parts[0])
	version := strings.TrimSpace(parts[1])
	if key == "" || version == "" {
		return "", "", fmt.Errorf("invalid --expect value %q: key and version are required", raw)
	}
	return key, version, nil
}

func mergeUnique(base, additional []string) []string {
	seen := make(map[string]struct{}, len(base)+len(additional))
	out := make([]string, 0, len(base)+len(additional))
	for _, raw := range append(append([]string(nil), base...), additional...) {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func readTextFile(path string) (string, error) {
	if strings.TrimSpace(path) == "-" {
		blob, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return string(blob), nil
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(blob), nil
}

func readWorkPlanFromFile(path string) (v1.WorkPlanPayload, error) {
	var plan v1.WorkPlanPayload
	if err := readJSONFileStrict(path, &plan); err != nil {
		return v1.WorkPlanPayload{}, fmt.Errorf("read --plan-file %s: %w", path, err)
	}
	return plan, nil
}

func readWorkPlanFromJSON(raw string) (v1.WorkPlanPayload, error) {
	var plan v1.WorkPlanPayload
	if err := readJSONInlineStrict(raw, &plan); err != nil {
		return v1.WorkPlanPayload{}, fmt.Errorf("read --plan-json: %w", err)
	}
	return plan, nil
}

func readWorkTasksFromFile(path string) ([]v1.WorkTaskPayload, error) {
	var tasks []v1.WorkTaskPayload
	if err := readJSONFileStrict(path, &tasks); err != nil {
		return nil, fmt.Errorf("read --tasks-file %s: %w", path, err)
	}
	return tasks, nil
}

func readWorkTasksFromJSON(raw string) ([]v1.WorkTaskPayload, error) {
	var tasks []v1.WorkTaskPayload
	if err := readJSONInlineStrict(raw, &tasks); err != nil {
		return nil, fmt.Errorf("read --tasks-json: %w", err)
	}
	return tasks, nil
}

func readStringListFromFile(path, flagName string) ([]string, error) {
	var values []string
	if err := readJSONFileStrict(path, &values); err != nil {
		return nil, fmt.Errorf("read %s %s: %w", flagName, path, err)
	}
	return mergeUnique(nil, values), nil
}

func readStringListFromJSON(raw, flagName string) ([]string, error) {
	var values []string
	if err := readJSONInlineStrict(raw, &values); err != nil {
		return nil, fmt.Errorf("read %s: %w", flagName, err)
	}
	return mergeUnique(nil, values), nil
}

func readStringMapFromFile(path, flagName string) (map[string]string, error) {
	values := map[string]string{}
	if err := readJSONFileStrict(path, &values); err != nil {
		return nil, fmt.Errorf("read %s %s: %w", flagName, path, err)
	}
	normalized := make(map[string]string, len(values))
	for k, v := range values {
		key := strings.TrimSpace(k)
		value := strings.TrimSpace(v)
		if key == "" || value == "" {
			continue
		}
		normalized[key] = value
	}
	return normalized, nil
}

func readStringMapFromJSON(raw, flagName string) (map[string]string, error) {
	values := map[string]string{}
	if err := readJSONInlineStrict(raw, &values); err != nil {
		return nil, fmt.Errorf("read %s: %w", flagName, err)
	}
	normalized := make(map[string]string, len(values))
	for k, v := range values {
		key := strings.TrimSpace(k)
		value := strings.TrimSpace(v)
		if key == "" || value == "" {
			continue
		}
		normalized[key] = value
	}
	return normalized, nil
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	maps.Copy(out, values)
	return out
}

func readJSONFileStrict(path string, out any) error {
	var (
		data []byte
		err  error
	)
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return err
	}

	return decodeJSONStrict(data, out)
}

func readJSONInlineStrict(raw string, out any) error {
	return decodeJSONStrict([]byte(raw), out)
}

type healthFixCLIFlags struct {
	apply       optionalBoolFlag
	dryRun      optionalBoolFlag
	projectRoot *string
	rulesFile   *string
	tagsFile    *string
	fixes       repeatedStringFlag
}

func addHealthFixCLIFlags(fs *flag.FlagSet) *healthFixCLIFlags {
	flags := &healthFixCLIFlags{}
	fs.Var(&flags.apply, "apply", "force apply mode for selected fixers (optional bool)")
	fs.Var(&flags.dryRun, "dry-run", "force preview mode without applying changes (optional bool)")
	flags.projectRoot = fs.String("project-root", "", "project root for fixers")
	flags.rulesFile = fs.String("rules-file", "", "explicit canonical rules file path for fix mode (overrides default discovery)")
	flags.tagsFile = fs.String("tags-file", "", "explicit canonical tag dictionary file path for fix mode (overrides default discovery)")
	fs.Var(&flags.fixes, "fix", healthFixerFlagDescription())
	return flags
}

func (f *healthFixCLIFlags) modeRequested() bool {
	if f == nil {
		return false
	}
	return f.apply.IsSet() || f.dryRun.IsSet() || len(f.fixes) > 0
}

func (f *healthFixCLIFlags) validateIdle() error {
	if f == nil {
		return nil
	}
	if strings.TrimSpace(derefString(f.projectRoot)) == "" && strings.TrimSpace(derefString(f.rulesFile)) == "" && strings.TrimSpace(derefString(f.tagsFile)) == "" {
		return nil
	}
	return errors.New("use --fix, --apply, or --dry-run when supplying fix-mode flags such as --project-root, --rules-file, or --tags-file")
}

func buildHealthPayload(projectID string, includeDetails optionalBoolFlag, maxFindingsPerCheck int, options *healthFixCLIFlags) (v1.HealthPayload, error) {
	payload := v1.HealthPayload{ProjectID: projectID}
	if includeDetails.IsSet() {
		payload.IncludeDetails = includeDetails.Ptr()
	}
	if maxFindingsPerCheck >= 0 {
		payload.MaxFindingsPerCheck = &maxFindingsPerCheck
	}
	if options == nil {
		return payload, nil
	}
	if options.modeRequested() {
		if includeDetails.IsSet() || maxFindingsPerCheck >= 0 {
			return v1.HealthPayload{}, errors.New("health check flags cannot be combined with --fix, --apply, or --dry-run")
		}
	} else if err := options.validateIdle(); err != nil {
		return v1.HealthPayload{}, err
	}
	apply, err := resolveHealthFixApply(options.apply, options.dryRun)
	if err != nil {
		return v1.HealthPayload{}, err
	}
	payload.ProjectRoot = strings.TrimSpace(derefString(options.projectRoot))
	payload.RulesFile = strings.TrimSpace(derefString(options.rulesFile))
	payload.TagsFile = strings.TrimSpace(derefString(options.tagsFile))
	if apply != nil {
		payload.Apply = apply
	}
	if options.modeRequested() && payload.Apply == nil {
		applyTrue := true
		payload.Apply = &applyTrue
	}
	for _, fixer := range options.fixes.Values() {
		payload.Fixers = append(payload.Fixers, v1.HealthFixer(fixer))
	}
	return payload, nil
}

func resolveHealthFixApply(applyFlag, dryRunFlag optionalBoolFlag) (*bool, error) {
	applyValue := (*bool)(nil)
	if applyFlag.IsSet() {
		applyValue = applyFlag.Ptr()
	}
	if !dryRunFlag.IsSet() {
		return applyValue, nil
	}
	dryRunValue := !dryRunFlag.value
	if applyValue != nil && *applyValue != dryRunValue {
		return nil, errors.New("--apply and --dry-run disagree; use one or provide matching values")
	}
	return &dryRunValue, nil
}

func healthFixerFlagDescription() string {
	parts := make([]string, 0, len(healthFixerDescriptions)+1)
	parts = append(parts, "fixer to run (repeatable)")
	for _, fixer := range healthFixerDescriptions {
		parts = append(parts, fmt.Sprintf("%s=%s", fixer.name, fixer.description))
	}
	return strings.Join(parts, "; ")
}

var healthFixerDescriptions = []struct {
	name        string
	description string
}{
	{name: string(v1.HealthFixerAll), description: "expand to the default fixer set"},
	{name: string(v1.HealthFixerSyncWorkingTree), description: "refresh indexed file hashes from the working tree"},
	{name: string(v1.HealthFixerIndexUnindexedFile), description: "add pointer stubs for unindexed files"},
	{name: string(v1.HealthFixerSyncRuleset), description: "re-sync pointers from the canonical ruleset"},
}

func decodeJSONStrict(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("unexpected trailing JSON tokens")
	}
	return nil
}

type repeatedStringFlag []string

func (r *repeatedStringFlag) String() string {
	return strings.Join(*r, ",")
}

func (r *repeatedStringFlag) Set(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return errors.New("value must not be empty")
	}
	*r = append(*r, trimmed)
	return nil
}

func (r *repeatedStringFlag) Values() []string {
	return append([]string(nil), *r...)
}

type optionalBoolFlag struct {
	set   bool
	value bool
}

func (f *optionalBoolFlag) String() string {
	if !f.set {
		return ""
	}
	return strconv.FormatBool(f.value)
}

func (f *optionalBoolFlag) Set(raw string) error {
	if strings.TrimSpace(raw) == "" {
		raw = "true"
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fmt.Errorf("invalid boolean value %q", raw)
	}
	f.set = true
	f.value = value
	return nil
}

func (f *optionalBoolFlag) IsBoolFlag() bool {
	return true
}

func (f *optionalBoolFlag) IsSet() bool {
	return f.set
}

func (f *optionalBoolFlag) Ptr() *bool {
	if f == nil || !f.set {
		return nil
	}
	value := f.value
	return &value
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
