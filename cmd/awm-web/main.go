package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/buildinfo"
	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/logging"
	"github.com/bonztm/agent-workflow-manager/internal/runtime"
	"github.com/bonztm/agent-workflow-manager/web"

	awmhttp "github.com/bonztm/agent-workflow-manager/internal/adapters/http"
)

func main() {
	logger := runtime.NewLogger()
	ctx := context.Background()

	if len(os.Args) < 2 {
		os.Exit(serve(ctx, logger, nil))
	}

	switch os.Args[1] {
	case "serve":
		os.Exit(serve(ctx, logger, os.Args[2:]))
	case "--version", "-v", "version":
		fmt.Fprintln(os.Stdout, buildinfo.Banner("awm-web"))
		os.Exit(0)
	case "--help", "-h", "help":
		usage()
		os.Exit(0)
	default:
		// Treat bare flags (e.g. `awm-web --addr :9090`) as implicit serve.
		if strings.HasPrefix(os.Args[1], "-") {
			os.Exit(serve(ctx, logger, os.Args[1:]))
		}
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func serve(ctx context.Context, logger logging.Logger, args []string) int {
	logger = logging.Normalize(logger)

	fset := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fset.String("addr", ":8080", "listen address (host:port)")
	if args == nil {
		args = []string{}
	}
	if err := fset.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		fmt.Fprintf(os.Stderr, "invalid flags: %v\n", err)
		return 2
	}

	svc, cleanup, err := runtime.NewServiceFromEnvWithLogger(ctx, logger)
	if err != nil {
		logger.Error(ctx, logging.EventAWMRun, "stage", "service_init", "ok", false, "error_code", v1.ErrCodeServiceInitFailed)
		fmt.Fprintf(os.Stderr, "failed to initialize service: %v\n", err)
		return 1
	}
	defer cleanup()

	projectID := runtime.ConfigFromEnv().EffectiveProjectID()

	static, err := staticFS()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load static assets: %v\n", err)
		return 1
	}

	handler := awmhttp.New(svc, projectID, static)

	srv := &http.Server{
		Addr:         *addr,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info(ctx, "awm-web.listen", "addr", *addr)
		printBanner(*addr, projectID)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "awm-web.listen", "ok", false, "error", err.Error())
			fmt.Fprintf(os.Stderr, "listen error: %v\n", err)
			os.Exit(1)
		}
	}()

	<-done
	fmt.Fprintln(os.Stderr, "\nshutting down...")

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "awm-web.shutdown", "ok", false, "error", err.Error())
	}
	return 0
}

func printBanner(addr, projectID string) {
	host, port, _ := net.SplitHostPort(addr)
	if host == "" {
		host = "localhost"
	}
	url := fmt.Sprintf("http://%s:%s", host, port)

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  awm-web ready")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "  Board:    %s/\n", url)
	fmt.Fprintf(os.Stderr, "  Status:   %s/status.html\n", url)
	fmt.Fprintf(os.Stderr, "  Health:   %s/healthz\n", url)
	if projectID != "" {
		fmt.Fprintf(os.Stderr, "  Project:  %s\n", projectID)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  Press Ctrl+C to stop")
	fmt.Fprintln(os.Stderr)
}

func staticFS() (http.FileSystem, error) {
	sub, err := fs.Sub(web.Static, ".")
	if err != nil {
		return nil, err
	}
	return http.FS(sub), nil
}

func usage() {
	fmt.Fprintln(os.Stdout, "awm-web - AWM web dashboard")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Usage:")
	fmt.Fprintln(os.Stdout, "  awm-web serve [--addr :8080]")
	fmt.Fprintln(os.Stdout, "  awm-web --version | -v")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Commands:")
	fmt.Fprintln(os.Stdout, "  serve   Start the web server (default if no subcommand given)")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Flags:")
	fmt.Fprintln(os.Stdout, "  --addr string   listen address (default \":8080\")")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "The web server shares the same database and config as awm and awm-mcp.")
	fmt.Fprintln(os.Stdout, "Go's net/http server is production-grade — the same binary runs in dev")
	fmt.Fprintln(os.Stdout, "and production. For k8s, use /healthz for liveness/readiness probes.")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Config Resolution:")
	fmt.Fprintln(os.Stdout, "  See `awm --help` for full environment variable reference.")
}
