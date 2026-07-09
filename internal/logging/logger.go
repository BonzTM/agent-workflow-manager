package logging

import (
	"context"
	"io"
	"log/slog"
)

// Logger is the shared project logging interface used across runtime, adapters, and tests.
type Logger interface {
	Info(ctx context.Context, event string, fields ...any)
	Error(ctx context.Context, event string, fields ...any)
}

type slogLogger struct {
	base *slog.Logger
}

// NewJSONLogger returns a Logger that writes JSON log lines to out at Info level.
func NewJSONLogger(out io.Writer) Logger {
	return NewJSONLoggerWithLevel(out, slog.LevelInfo)
}

// NewJSONLoggerWithLevel returns a Logger that writes JSON log lines to out,
// emitting only records at or above level. A nil out discards all output.
func NewJSONLoggerWithLevel(out io.Writer, level slog.Level) Logger {
	if out == nil {
		out = io.Discard
	}
	handler := slog.NewJSONHandler(out, &slog.HandlerOptions{Level: level})
	return &slogLogger{base: slog.New(handler)}
}

// NewDiscardLogger returns a Logger that silently drops every record.
func NewDiscardLogger() Logger {
	return NewJSONLogger(io.Discard)
}

// Normalize returns logger unchanged if it is non-nil, otherwise a discard
// Logger, so callers can log without nil checks.
func Normalize(logger Logger) Logger {
	if logger == nil {
		return NewDiscardLogger()
	}
	return logger
}

func (l *slogLogger) Info(ctx context.Context, event string, fields ...any) {
	if l == nil || l.base == nil {
		return
	}
	l.base.InfoContext(ctx, event, fields...)
}

func (l *slogLogger) Error(ctx context.Context, event string, fields ...any) {
	if l == nil || l.base == nil {
		return
	}
	l.base.ErrorContext(ctx, event, fields...)
}
