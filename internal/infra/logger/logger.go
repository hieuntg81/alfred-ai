package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"alfred-ai/internal/infra/config"
)

// New creates a configured *slog.Logger.
// The returned closer function should be deferred to flush/close file handles.
func New(cfg config.LoggerConfig) (*slog.Logger, func() error, error) {
	writer, closer, err := openOutput(cfg.Output)
	if err != nil {
		return nil, nil, fmt.Errorf("open log output: %w", err)
	}

	level := parseLevel(cfg.Level)
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "json":
		handler = slog.NewJSONHandler(writer, opts)
	default:
		handler = slog.NewTextHandler(writer, opts)
	}

	return slog.New(handler), closer, nil
}

// parseLevel converts a string level to slog.Level.
func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// openOutput returns an io.Writer for the specified output target.
func openOutput(output string) (io.Writer, func() error, error) {
	noop := func() error { return nil }

	switch strings.ToLower(output) {
	case "stdout":
		return os.Stdout, noop, nil
	case "stderr", "":
		return os.Stderr, noop, nil
	default:
		f, err := os.OpenFile(output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return nil, nil, err
		}
		return f, f.Close, nil
	}
}
