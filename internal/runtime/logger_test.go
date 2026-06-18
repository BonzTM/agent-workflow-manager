package runtime

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoggerConfigFromEnv_Defaults(t *testing.T) {
	cfg := loggerConfigFromEnv(func(string) string { return "" })

	if got, want := cfg.level, slog.LevelInfo; got != want {
		t.Fatalf("unexpected default level: got=%v want=%v", got, want)
	}
	if got, want := cfg.sink, loggerSinkStderr; got != want {
		t.Fatalf("unexpected default sink: got=%q want=%q", got, want)
	}
}

func TestLoggerConfigFromEnv_BoundedValues(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		level     string
		sink      string
		wantLevel slog.Level
		wantSink  loggerSink
	}{
		{name: "debug stdout", level: " debug ", sink: " stdout ", wantLevel: slog.LevelDebug, wantSink: loggerSinkStdout},
		{name: "info stderr", level: "INFO", sink: "STDERR", wantLevel: slog.LevelInfo, wantSink: loggerSinkStderr},
		{name: "warn discard", level: "Warn", sink: "Discard", wantLevel: slog.LevelWarn, wantSink: loggerSinkDiscard},
		{name: "error stderr", level: " error ", sink: " stderr ", wantLevel: slog.LevelError, wantSink: loggerSinkStderr},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := loggerConfigFromEnv(envGetter(map[string]string{
				LogLevelEnvVar: tc.level,
				LogSinkEnvVar:  tc.sink,
			}))

			if got := cfg.level; got != tc.wantLevel {
				t.Fatalf("unexpected level: got=%v want=%v", got, tc.wantLevel)
			}
			if got := cfg.sink; got != tc.wantSink {
				t.Fatalf("unexpected sink: got=%q want=%q", got, tc.wantSink)
			}
		})
	}
}

func TestLoggerConfigFromEnv_InvalidValuesFallbackToDefaults(t *testing.T) {
	cfg := loggerConfigFromEnv(envGetter(map[string]string{
		LogLevelEnvVar: "trace",
		LogSinkEnvVar:  "file",
	}))

	if got, want := cfg.level, slog.LevelInfo; got != want {
		t.Fatalf("unexpected fallback level: got=%v want=%v", got, want)
	}
	if got, want := cfg.sink, loggerSinkStderr; got != want {
		t.Fatalf("unexpected fallback sink: got=%q want=%q", got, want)
	}
}

func TestNewLoggerFromEnvWithOutputs_WritesToConfiguredSink(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sink         string
		expectedSink loggerSink
	}{
		{name: "default stderr", sink: "", expectedSink: loggerSinkStderr},
		{name: "stdout", sink: "stdout", expectedSink: loggerSinkStdout},
		{name: "discard", sink: "discard", expectedSink: loggerSinkDiscard},
		{name: "invalid defaults to stderr", sink: "not-a-sink", expectedSink: loggerSinkStderr},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var stdoutBuf bytes.Buffer
			var stderrBuf bytes.Buffer
			var discardBuf bytes.Buffer

			logger := newLoggerFromEnvWithOutputs(
				envGetter(map[string]string{
					LogSinkEnvVar: tc.sink,
				}),
				loggerOutputs{
					stdout:  &stdoutBuf,
					stderr:  &stderrBuf,
					discard: &discardBuf,
				},
			)

			event := "logger.sink." + strings.ReplaceAll(tc.name, " ", "_")
			logger.Error(context.Background(), event, "error_code", "INTERNAL_ERROR")

			stdoutHasEvent := strings.Contains(stdoutBuf.String(), event)
			stderrHasEvent := strings.Contains(stderrBuf.String(), event)
			discardHasEvent := strings.Contains(discardBuf.String(), event)

			switch tc.expectedSink {
			case loggerSinkStdout:
				if !stdoutHasEvent || stderrHasEvent || discardHasEvent {
					t.Fatalf("unexpected sink routing: stdout=%t stderr=%t discard=%t", stdoutHasEvent, stderrHasEvent, discardHasEvent)
				}
			case loggerSinkDiscard:
				if stdoutHasEvent || stderrHasEvent || !discardHasEvent {
					t.Fatalf("unexpected sink routing: stdout=%t stderr=%t discard=%t", stdoutHasEvent, stderrHasEvent, discardHasEvent)
				}
			default:
				if stdoutHasEvent || !stderrHasEvent || discardHasEvent {
					t.Fatalf("unexpected sink routing: stdout=%t stderr=%t discard=%t", stdoutHasEvent, stderrHasEvent, discardHasEvent)
				}
			}
		})
	}
}

func TestNewLoggerFromEnvWithOutputs_RespectsConfiguredLevel(t *testing.T) {
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	var discardBuf bytes.Buffer

	logger := newLoggerFromEnvWithOutputs(
		envGetter(map[string]string{
			LogSinkEnvVar:  "stderr",
			LogLevelEnvVar: "error",
		}),
		loggerOutputs{
			stdout:  &stdoutBuf,
			stderr:  &stderrBuf,
			discard: &discardBuf,
		},
	)

	logger.Info(context.Background(), "logger.level.info")
	logger.Error(context.Background(), "logger.level.error", "error_code", "INTERNAL_ERROR")

	stderrOutput := stderrBuf.String()
	if strings.Contains(stderrOutput, "logger.level.info") {
		t.Fatalf("info event should be filtered at error level: %s", stderrOutput)
	}
	if !strings.Contains(stderrOutput, "logger.level.error") {
		t.Fatalf("expected error event in stderr output: %s", stderrOutput)
	}
	if got := stdoutBuf.Len(); got != 0 {
		t.Fatalf("expected no stdout output, got %d bytes", got)
	}
	if got := discardBuf.Len(); got != 0 {
		t.Fatalf("expected no discard output, got %d bytes", got)
	}
}

func TestRuntimeEnvGetenv_LoadsDotEnvValues(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("AWM_LOG_LEVEL=debug\nAWM_LOG_SINK=stdout\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	getenv := runtimeEnvGetenv(root, func(string) (string, bool) { return "", false })
	cfg := loggerConfigFromEnv(getenv)
	if got, want := cfg.level, slog.LevelDebug; got != want {
		t.Fatalf("unexpected level: got=%v want=%v", got, want)
	}
	if got, want := cfg.sink, loggerSinkStdout; got != want {
		t.Fatalf("unexpected sink: got=%q want=%q", got, want)
	}
}

func envGetter(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}
