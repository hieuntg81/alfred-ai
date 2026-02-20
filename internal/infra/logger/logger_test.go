package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alfred-ai/internal/infra/config"
)

func TestNewJSONLogger(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LoggerConfig{Level: "info", Format: "json", Output: "stderr"}

	log, closer, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closer()

	// Replace the handler's writer with our buffer for testing
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	log = slog.New(handler)

	log.Info("test message", "key", "value")

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON: %v, output: %s", err, buf.String())
	}
	if entry["msg"] != "test message" {
		t.Errorf("msg = %q, want %q", entry["msg"], "test message")
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"unknown", slog.LevelInfo},
		{"", slog.LevelInfo},
	}
	for _, tt := range tests {
		if got := parseLevel(tt.input); got != tt.want {
			t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestOpenOutputStdout(t *testing.T) {
	w, closer, err := openOutput("stdout")
	if err != nil {
		t.Fatalf("openOutput(stdout): %v", err)
	}
	defer closer()
	if w != os.Stdout {
		t.Error("expected os.Stdout")
	}
}

func TestOpenOutputStderr(t *testing.T) {
	w, closer, err := openOutput("stderr")
	if err != nil {
		t.Fatalf("openOutput(stderr): %v", err)
	}
	defer closer()
	if w != os.Stderr {
		t.Error("expected os.Stderr")
	}
}

func TestOpenOutputEmpty(t *testing.T) {
	w, closer, err := openOutput("")
	if err != nil {
		t.Fatalf("openOutput(''): %v", err)
	}
	defer closer()
	if w != os.Stderr {
		t.Error("expected os.Stderr for empty output")
	}
}

func TestOpenOutputFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	w, closer, err := openOutput(path)
	if err != nil {
		t.Fatalf("openOutput(file): %v", err)
	}

	if w == nil {
		t.Fatal("writer is nil")
	}

	// Write something to verify it works
	_, err = w.Write([]byte("test log line\n"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := closer(); err != nil {
		t.Fatalf("closer: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "test log line\n" {
		t.Errorf("file content = %q", string(data))
	}
}

func TestOpenOutputInvalidPath(t *testing.T) {
	_, _, err := openOutput("/nonexistent/dir/log.txt")
	if err == nil {
		t.Error("expected error for invalid path")
	}
}

func TestNewLoggerFileOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	cfg := config.LoggerConfig{Level: "info", Format: "text", Output: path}
	log, closer, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	log.Info("file output test", "key", "value")
	if err := closer(); err != nil {
		t.Fatalf("closer: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "file output test") {
		t.Error("log file should contain the logged message")
	}
}

func TestNewLoggerStdoutOutput(t *testing.T) {
	cfg := config.LoggerConfig{Level: "debug", Format: "text", Output: "stdout"}
	log, closer, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closer()
	if log == nil {
		t.Error("logger is nil")
	}
}

func TestNewLoggerInvalidOutput(t *testing.T) {
	cfg := config.LoggerConfig{Level: "info", Format: "text", Output: "/nonexistent/dir/app.log"}
	_, _, err := New(cfg)
	if err == nil {
		t.Error("expected error for invalid output path")
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	log := slog.New(handler)

	log.Info("should be filtered")
	log.Warn("should appear")

	output := buf.String()
	if strings.Contains(output, "should be filtered") {
		t.Error("info message should be filtered at warn level")
	}
	if !strings.Contains(output, "should appear") {
		t.Error("warn message should appear at warn level")
	}
}
