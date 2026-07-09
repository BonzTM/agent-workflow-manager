package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/buildinfo"
	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/logging"
	"github.com/bonztm/agent-workflow-manager/internal/runtime"
)

type helpCommand struct {
	usage   string
	summary string
}

var automationEntryPoints = []helpCommand{
	{
		usage:   "awm run --in <request.json|->",
		summary: "Execute a full v1 command envelope from stdin or a file.",
	},
	{
		usage:   "awm validate --in <request.json|->",
		summary: "Validate a full v1 command envelope without executing it.",
	},
}

// RunCLI is the entry point for the awm binary. It dispatches args to the
// run/validate envelope commands, the convenience subcommands, or the
// version/help printers, wiring nil streams to the process defaults, and
// returns the process exit code (0 success, 1 execution failure, 2 usage
// error).
func RunCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	logger := runtime.NewLogger()
	ctx := context.Background()

	if len(args) < 1 {
		logger.Error(ctx, logging.EventAWMRun, "stage", "parse", "ok", false, "error_code", v1.ErrCodeMissingSubcommand)
		printMainUsage(stdout)
		return 2
	}

	switch args[0] {
	case "run":
		return runWithIO(ctx, logger, args[1:], stdin, stdout, stderr)
	case "validate":
		return validateWithIO(ctx, logger, args[1:], stdin, stdout, stderr)
	case "--version", "-v", "version":
		printVersion(stdout, "awm")
		return 0
	case "--help", "-h", "help":
		printMainUsage(stdout)
		return 0
	default:
		if route, consumed, ok := matchConvenienceRoute(args); ok {
			return runConvenienceWithDeps(
				ctx,
				logger,
				route.Name,
				args[consumed:],
				stdout,
				time.Now,
				runtime.NewServiceFromEnvWithLogger,
				RunWithLogger,
			)
		}
		logger.Error(ctx, logging.EventAWMRun, "stage", "parse", "subcommand", args[0], "ok", false, "error_code", v1.ErrCodeUnknownSubcommand)
		fmt.Fprintf(stderr, "unknown subcommand: %s\n", args[0])
		printMainUsage(stdout)
		return 2
	}
}

func runWithIO(ctx context.Context, logger logging.Logger, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	logger = logging.Normalize(logger)
	logger.Info(ctx, logging.EventAWMRun, "stage", "start", "subcommand", "run")

	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	configureRunUsage(fs)
	fs.SetOutput(stderr)
	inPath := fs.String("in", "-", "input request file path or '-' for stdin")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		logger.Error(ctx, logging.EventAWMRun, "stage", "parse_flags", "subcommand", "run", "ok", false, "error_code", v1.ErrCodeInvalidFlags)
		return 2
	}

	in, closeInput, err := openInputWithStdin(*inPath, stdin)
	if err != nil {
		logger.Error(ctx, logging.EventAWMIORead, "stage", "open_input", "subcommand", "run", "ok", false, "path", *inPath, "error_code", v1.ErrCodeReadFailed)
		fmt.Fprintf(stderr, "failed to open input: %v\n", err)
		return 2
	}
	defer closeInput()
	logger.Info(ctx, logging.EventAWMIORead, "stage", "open_input", "subcommand", "run", "ok", true, "path", *inPath)

	svc, closeService, err := runtime.NewServiceFromEnvWithLogger(ctx, logger)
	if err != nil {
		logger.Error(ctx, logging.EventAWMRun, "stage", "service_init", "subcommand", "run", "ok", false, "error_code", v1.ErrCodeServiceInitFailed)
		fmt.Fprintf(stderr, "failed to initialize service: %v\n", err)
		return 1
	}
	defer closeService()

	code := RunWithLogger(ctx, svc, in, stdout, time.Now, logger)
	logger.Info(ctx, logging.EventAWMRun, "stage", "finish", "subcommand", "run", "exit_code", code)
	return code
}

func validateWithIO(ctx context.Context, logger logging.Logger, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	logger = logging.Normalize(logger)
	logger.Info(ctx, logging.EventAWMRun, "stage", "start", "subcommand", "validate")

	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	configureValidateUsage(fs)
	fs.SetOutput(stderr)
	inPath := fs.String("in", "-", "input request file path or '-' for stdin")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		logger.Error(ctx, logging.EventAWMRun, "stage", "parse_flags", "subcommand", "validate", "ok", false, "error_code", v1.ErrCodeInvalidFlags)
		return 2
	}

	in, closeFn, err := openInputWithStdin(*inPath, stdin)
	if err != nil {
		logger.Error(ctx, logging.EventAWMIORead, "stage", "open_input", "subcommand", "validate", "ok", false, "path", *inPath, "error_code", v1.ErrCodeReadFailed)
		fmt.Fprintf(stderr, "failed to open input: %v\n", err)
		return 2
	}
	defer closeFn()
	logger.Info(ctx, logging.EventAWMIORead, "stage", "open_input", "subcommand", "validate", "ok", true, "path", *inPath)

	b, err := io.ReadAll(in)
	if err != nil {
		logger.Error(ctx, logging.EventAWMIORead, "stage", "read_input", "subcommand", "validate", "ok", false, "error_code", v1.ErrCodeReadFailed)
		fmt.Fprintf(stderr, "failed to read input: %v\n", err)
		return 2
	}
	logger.Info(ctx, logging.EventAWMIORead, "stage", "read_input", "subcommand", "validate", "ok", true, "bytes", len(b))
	_, _, valErr := v1.DecodeAndValidateCommandWithDefaults(b, v1.ValidationDefaults{ProjectID: runtime.ConfigFromEnv().EffectiveProjectID()})
	if valErr != nil {
		logger.Error(ctx, logging.EventAWMRun, "stage", "validate", "subcommand", "validate", "ok", false, "error_code", valErr.Code)
		fmt.Fprintf(stdout, "{\n  \"ok\": false,\n  \"error\": {\n    \"code\": %q,\n    \"message\": %q\n  }\n}\n", valErr.Code, valErr.Message)
		return 1
	}
	logger.Info(ctx, logging.EventAWMRun, "stage", "validate", "subcommand", "validate", "ok", true)
	fmt.Fprintln(stdout, "{\n  \"ok\": true\n}")
	logger.Info(ctx, logging.EventAWMRun, "stage", "finish", "subcommand", "validate", "exit_code", 0)
	return 0
}

func run(ctx context.Context, logger logging.Logger, args []string) int {
	logger = logging.Normalize(logger)
	logger.Info(ctx, logging.EventAWMRun, "stage", "start", "subcommand", "run")

	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	configureRunUsage(fs)
	inPath := fs.String("in", "-", "input request file path or '-' for stdin")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		logger.Error(ctx, logging.EventAWMRun, "stage", "parse_flags", "subcommand", "run", "ok", false, "error_code", v1.ErrCodeInvalidFlags)
		return 2
	}

	in, closeInput, err := openInput(*inPath)
	if err != nil {
		logger.Error(ctx, logging.EventAWMIORead, "stage", "open_input", "subcommand", "run", "ok", false, "path", *inPath, "error_code", v1.ErrCodeReadFailed)
		fmt.Fprintf(os.Stderr, "failed to open input: %v\n", err)
		return 2
	}
	defer closeInput()
	logger.Info(ctx, logging.EventAWMIORead, "stage", "open_input", "subcommand", "run", "ok", true, "path", *inPath)

	svc, closeService, err := runtime.NewServiceFromEnvWithLogger(ctx, logger)
	if err != nil {
		logger.Error(ctx, logging.EventAWMRun, "stage", "service_init", "subcommand", "run", "ok", false, "error_code", v1.ErrCodeServiceInitFailed)
		fmt.Fprintf(os.Stderr, "failed to initialize service: %v\n", err)
		return 1
	}
	defer closeService()

	code := RunWithLogger(ctx, svc, in, os.Stdout, time.Now, logger)
	logger.Info(ctx, logging.EventAWMRun, "stage", "finish", "subcommand", "run", "exit_code", code)
	return code
}

func validate(ctx context.Context, logger logging.Logger, args []string) int {
	logger = logging.Normalize(logger)
	logger.Info(ctx, logging.EventAWMRun, "stage", "start", "subcommand", "validate")

	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	configureValidateUsage(fs)
	inPath := fs.String("in", "-", "input request file path or '-' for stdin")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		logger.Error(ctx, logging.EventAWMRun, "stage", "parse_flags", "subcommand", "validate", "ok", false, "error_code", v1.ErrCodeInvalidFlags)
		return 2
	}

	in, closeFn, err := openInput(*inPath)
	if err != nil {
		logger.Error(ctx, logging.EventAWMIORead, "stage", "open_input", "subcommand", "validate", "ok", false, "path", *inPath, "error_code", v1.ErrCodeReadFailed)
		fmt.Fprintf(os.Stderr, "failed to open input: %v\n", err)
		return 2
	}
	defer closeFn()
	logger.Info(ctx, logging.EventAWMIORead, "stage", "open_input", "subcommand", "validate", "ok", true, "path", *inPath)

	b, err := io.ReadAll(in)
	if err != nil {
		logger.Error(ctx, logging.EventAWMIORead, "stage", "read_input", "subcommand", "validate", "ok", false, "error_code", v1.ErrCodeReadFailed)
		fmt.Fprintf(os.Stderr, "failed to read input: %v\n", err)
		return 2
	}
	logger.Info(ctx, logging.EventAWMIORead, "stage", "read_input", "subcommand", "validate", "ok", true, "bytes", len(b))
	_, _, valErr := v1.DecodeAndValidateCommandWithDefaults(b, v1.ValidationDefaults{
		ProjectID: runtime.ConfigFromEnv().EffectiveProjectID(),
	})
	if valErr != nil {
		logger.Error(ctx, logging.EventAWMRun, "stage", "validate", "subcommand", "validate", "ok", false, "error_code", valErr.Code)
		fmt.Printf("{\n  \"ok\": false,\n  \"error\": {\n    \"code\": %q,\n    \"message\": %q\n  }\n}\n", valErr.Code, valErr.Message)
		return 1
	}
	logger.Info(ctx, logging.EventAWMRun, "stage", "validate", "subcommand", "validate", "ok", true)
	fmt.Println("{\n  \"ok\": true\n}")
	logger.Info(ctx, logging.EventAWMRun, "stage", "finish", "subcommand", "validate", "exit_code", 0)
	return 0
}

func openInput(path string) (io.Reader, func(), error) {
	if path == "-" {
		return os.Stdin, func() {}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { _ = f.Close() }, nil
}

func openInputWithStdin(path string, stdin io.Reader) (io.Reader, func(), error) {
	if path == "-" {
		if stdin == nil {
			stdin = os.Stdin
		}
		return stdin, func() {}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { _ = f.Close() }, nil
}

func printVersion(w io.Writer, binaryName string) {
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprintln(w, buildinfo.Banner(binaryName))
}

func printMainUsage(w io.Writer) {
	if w == nil {
		w = os.Stdout
	}

	fmt.Fprintln(w, "awm - agent workflow manager CLI")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  awm <command> [flags]")
	fmt.Fprintln(w, "  awm --version | -v")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Agent Workflow Commands:")
	printHelpCommands(w, helpCommandsForGroup(routeGroupWorkflow))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Maintenance Commands:")
	printHelpCommands(w, helpCommandsForGroup(routeGroupMaintenance))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Structured JSON Automation:")
	printHelpCommands(w, automationEntryPoints)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Shared Conventions:")
	fmt.Fprintln(w, "  - Convenience commands accept optional `--project`; explicit values override env and repo-root defaults.")
	fmt.Fprintln(w, "  - Run `awm <subcommand> --help` for exhaustive flags and examples for one command.")
	fmt.Fprintln(w, "  - Run `awm health --help` to list available fixers and preview/apply examples.")
	fmt.Fprintln(w, "  - `--request-id` overrides the generated request id on convenience commands.")
	fmt.Fprintln(w, "  - Most text/list payloads support inline values, `--*-json`, and `--*-file` variants.")
	fmt.Fprintln(w, "  - `-` means stdin for `--in` and file-backed flags.")
	fmt.Fprintln(w, "  - Repeatable flags may be provided more than once.")
	fmt.Fprintln(w, "  - Optional bool flags accept `--flag`, `--flag=true`, or `--flag=false`.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "High-Signal Requirements:")
	fmt.Fprintln(w, "  - `context` requires one of `--task-text` or `--task-file`.")
	fmt.Fprintln(w, "  - `done` requires `--receipt-id` or `--plan-key` and one of `--outcome` or `--outcome-file`.")
	fmt.Fprintln(w, "  - `work` enforces exclusive payload groups such as `--plan-file|--plan-json` and `--tasks-file|--tasks-json`.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Config Resolution:")
	fmt.Fprintln(w, "  1. Process environment (`AWM_*`) wins.")
	fmt.Fprintln(w, "  2. `--project` / `project_id` wins when provided.")
	fmt.Fprintln(w, "  3. Otherwise `AWM_PROJECT_ID` sets the default project namespace.")
	fmt.Fprintln(w, "  4. Otherwise the repo-root name is inferred, using `AWM_PROJECT_ROOT` when the shell is elsewhere.")
	fmt.Fprintln(w, "  5. Repo-root `.env` is loaded when present.")
	fmt.Fprintln(w, "  6. If `AWM_PG_DSN` is set, Postgres is used.")
	fmt.Fprintln(w, "  7. Otherwise SQLite defaults to `<repo-root>/.awm/context.db`.")
	fmt.Fprintln(w, "  8. Outside a repo, SQLite defaults to `<cwd>/.awm/context.db`.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Environment Variables:")
	fmt.Fprintln(w, "  - `AWM_PG_DSN`: Postgres DSN. If set, Postgres is the active backend.")
	fmt.Fprintln(w, "  - `AWM_PROJECT_ID`: Optional default project identifier for convenience, run, validate, and MCP tool calls.")
	fmt.Fprintln(w, "  - `AWM_PROJECT_ROOT`: Optional explicit repo root when running awm from another directory.")
	fmt.Fprintln(w, "  - `AWM_SQLITE_PATH`: Optional explicit SQLite path. Relative paths resolve from the detected project root.")
	fmt.Fprintln(w, "  - `AWM_UNBOUNDED`: `true|false`. When true, history surfaces stop applying built-in result caps.")
	fmt.Fprintln(w, "  - `AWM_LOG_LEVEL`: `debug|info|warn|error`.")
	fmt.Fprintln(w, "  - `AWM_LOG_SINK`: `stderr|stdout|discard`.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Managed Repo Files:")
	fmt.Fprintln(w, "  - `.awm/context.db`, `.awm/context.db-shm`, `.awm/context.db-wal`: implicit repo-local SQLite database and sidecars.")
	fmt.Fprintln(w, "  - `.awm/awm-rules.yaml` or `awm-rules.yaml`: canonical rules.")
	fmt.Fprintln(w, "  - `.awm/awm-tags.yaml`: repo-local canonical tag overrides.")
	fmt.Fprintln(w, "  - `.awm/awm-tests.yaml` or `awm-tests.yaml`: repo-local executable verification definitions.")
	fmt.Fprintln(w, "  - `.awm/awm-workflows.yaml` or `awm-workflows.yaml`: repo-local completion gate definitions.")
	fmt.Fprintln(w, "  - `.env`: repo-local runtime/env overrides, loaded automatically.")
	fmt.Fprintln(w, "  - `.env.example`: seeded example for AWM runtime variables.")
	fmt.Fprintln(w, "  - `.awm/init_candidates.json`: optional persisted init candidate output.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "First-Run Recovery:")
	fmt.Fprintln(w, "  # zero-config local init")
	fmt.Fprintln(w, "  awm init")
	fmt.Fprintln(w, "  # later, opt into additive starter templates without overwriting edited files")
	fmt.Fprintln(w, "  awm init --apply-template starter-contract --apply-template verify-generic")
	fmt.Fprintln(w, "  awm health --include-details")
	fmt.Fprintln(w, "  # after later edits, refresh changed files")
	fmt.Fprintln(w, "  awm sync --mode working_tree --insert-new-candidates")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  # pin a stable namespace when the folder name is not what you want")
	fmt.Fprintln(w, "  export AWM_PROJECT_ID=myproject")
	fmt.Fprintln(w, "  # force explicit local SQLite")
	fmt.Fprintln(w, "  export AWM_SQLITE_PATH=.awm/context.db")
	fmt.Fprintln(w, "  awm health --include-details")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  # switch to Postgres")
	fmt.Fprintln(w, "  export AWM_PG_DSN='postgres://user:pass@localhost:5432/agents_context?sslmode=disable'")
	fmt.Fprintln(w, "  awm health --include-details")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "More Help:")
	fmt.Fprintln(w, "  - `awm-mcp --help` describes the JSON-RPC 2.0 MCP server.")
}

func printHelpCommands(w io.Writer, commands []helpCommand) {
	for _, command := range commands {
		fmt.Fprintf(w, "  %s\n", command.usage)
		fmt.Fprintf(w, "    %s\n", command.summary)
	}
}

func configureRunUsage(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		out := fs.Output()
		fmt.Fprintln(out, "Usage:")
		fmt.Fprintln(out, "  awm run --in <request.json|->")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Purpose:")
		fmt.Fprintln(out, "  Execute a full v1 command envelope from stdin or a file.")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Flags:")
		fs.PrintDefaults()
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Examples:")
		fmt.Fprintln(out, "  awm run --in request.json")
		fmt.Fprintln(out, "  cat request.json | awm run --in -")
	}
}

func configureValidateUsage(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		out := fs.Output()
		fmt.Fprintln(out, "Usage:")
		fmt.Fprintln(out, "  awm validate --in <request.json|->")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Purpose:")
		fmt.Fprintln(out, "  Validate a full v1 command envelope without executing it.")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Flags:")
		fs.PrintDefaults()
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Examples:")
		fmt.Fprintln(out, "  awm validate --in request.json")
		fmt.Fprintln(out, "  cat request.json | awm validate --in -")
	}
}
