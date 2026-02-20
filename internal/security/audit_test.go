package security

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"alfred-ai/internal/domain"
)

func TestFileAuditLogger_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	logger, err := NewFileAuditLogger(path)
	if err != nil {
		t.Fatalf("NewFileAuditLogger: %v", err)
	}

	event := domain.AuditEvent{
		Type:   domain.AuditLLMCall,
		Detail: map[string]string{"model": "gpt-4o-mini", "total_tokens": "200"},
	}

	if err := logger.Log(context.Background(), event); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read back
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	var read domain.AuditEvent
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatal("expected at least one line")
	}
	if err := json.Unmarshal(scanner.Bytes(), &read); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if read.Type != domain.AuditLLMCall {
		t.Errorf("Type = %q, want %q", read.Type, domain.AuditLLMCall)
	}
	if read.Detail["model"] != "gpt-4o-mini" {
		t.Errorf("Detail[model] = %q", read.Detail["model"])
	}
}

func TestFileAuditLogger_MultipleEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	logger, err := NewFileAuditLogger(path)
	if err != nil {
		t.Fatalf("NewFileAuditLogger: %v", err)
	}

	events := []domain.AuditEvent{
		{Type: domain.AuditLLMCall, Detail: map[string]string{"model": "gpt-4"}},
		{Type: domain.AuditToolExec, Detail: map[string]string{"tool": "filesystem", "success": "true"}},
		{Type: domain.AuditMemoryStore, Detail: map[string]string{"id": "abc123"}},
	}

	for _, e := range events {
		if err := logger.Log(context.Background(), e); err != nil {
			t.Fatalf("Log: %v", err)
		}
	}
	logger.Close()

	// Count lines
	f, _ := os.Open(path)
	defer f.Close()
	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		count++
	}
	if count != 3 {
		t.Errorf("expected 3 lines, got %d", count)
	}
}

func TestFileAuditLogger_AutoTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	logger, err := NewFileAuditLogger(path)
	if err != nil {
		t.Fatalf("NewFileAuditLogger: %v", err)
	}

	before := time.Now().Add(-time.Second)
	logger.Log(context.Background(), domain.AuditEvent{
		Type:   domain.AuditToolExec,
		Detail: map[string]string{"tool": "shell"},
	})
	after := time.Now().Add(time.Second)
	logger.Close()

	f, _ := os.Open(path)
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Scan()

	var event domain.AuditEvent
	json.Unmarshal(scanner.Bytes(), &event)

	if event.Timestamp.Before(before) || event.Timestamp.After(after) {
		t.Errorf("timestamp %v not in expected range [%v, %v]", event.Timestamp, before, after)
	}
}

func TestFileAuditLogger_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	logger, err := NewFileAuditLogger(path)
	if err != nil {
		t.Fatalf("NewFileAuditLogger: %v", err)
	}

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Log(context.Background(), domain.AuditEvent{
				Type:   domain.AuditLLMCall,
				Detail: map[string]string{"model": "test"},
			})
		}()
	}
	wg.Wait()
	logger.Close()

	// Verify all lines are valid JSON
	f, _ := os.Open(path)
	defer f.Close()
	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		var event domain.AuditEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Errorf("line %d invalid JSON: %v", count, err)
		}
		count++
	}
	if count != n {
		t.Errorf("expected %d lines, got %d", n, count)
	}
}

func TestNewFileAuditLoggerInvalidPath(t *testing.T) {
	_, err := NewFileAuditLogger("/nonexistent/dir/audit.jsonl")
	if err == nil {
		t.Error("expected error for invalid path")
	}
}

func TestFileAuditLogger_WriteAfterClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	logger, err := NewFileAuditLogger(path)
	if err != nil {
		t.Fatalf("NewFileAuditLogger: %v", err)
	}

	logger.Close()

	err = logger.Log(context.Background(), domain.AuditEvent{
		Type:   domain.AuditLLMCall,
		Detail: map[string]string{"model": "test"},
	})
	if err == nil {
		t.Error("expected error writing to closed file")
	}
}

func TestFileAuditLoggerWriteError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	logger, err := NewFileAuditLogger(path)
	if err != nil {
		t.Fatalf("NewFileAuditLogger: %v", err)
	}

	// Close the file to force write error
	logger.file.Close()

	err = logger.Log(context.Background(), domain.AuditEvent{
		Type:   domain.AuditToolExec,
		Detail: map[string]string{"test": "value"},
	})
	if err == nil {
		t.Error("expected error from write to closed file")
	}
}

func TestFileAuditLogger_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	logger, _ := NewFileAuditLogger(path)
	logger.Log(context.Background(), domain.AuditEvent{Type: domain.AuditLLMCall})
	logger.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}
}

func TestFileAuditLogger_OTelSpanRecording(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	logger, err := NewFileAuditLogger(path)
	if err != nil {
		t.Fatalf("NewFileAuditLogger: %v", err)
	}
	defer logger.Close()

	// Set up a real TracerProvider that records spans so span.IsRecording() returns true
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer tp.Shutdown(context.Background())
	otel.SetTracerProvider(tp)

	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	if !span.IsRecording() {
		t.Fatal("span should be recording for this test to be meaningful")
	}

	event := domain.AuditEvent{
		Type: domain.AuditToolExec,
		Detail: map[string]string{
			"tool":    "shell",
			"success": "true",
		},
	}

	if err := logger.Log(ctx, event); err != nil {
		t.Fatalf("Log with active span: %v", err)
	}
}

// --- Compliance logging tests ---

func TestFileAuditLogger_LogAccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	logger, err := NewFileAuditLogger(path)
	if err != nil {
		t.Fatalf("NewFileAuditLogger: %v", err)
	}

	if err := logger.LogAccess(context.Background(), "user123", "session", "read", "success"); err != nil {
		t.Fatalf("LogAccess: %v", err)
	}
	logger.Close()

	f, _ := os.Open(path)
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Scan()

	var event domain.AuditEvent
	json.Unmarshal(scanner.Bytes(), &event)

	if event.Type != domain.AuditAccessLog {
		t.Errorf("Type = %q, want %q", event.Type, domain.AuditAccessLog)
	}
	if event.Actor != "user123" {
		t.Errorf("Actor = %q, want %q", event.Actor, "user123")
	}
	if event.Resource != "session" {
		t.Errorf("Resource = %q, want %q", event.Resource, "session")
	}
	if event.Action != "read" {
		t.Errorf("Action = %q, want %q", event.Action, "read")
	}
	if event.Outcome != "success" {
		t.Errorf("Outcome = %q, want %q", event.Outcome, "success")
	}
}

func TestFileAuditLogger_LogDataEvent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	logger, err := NewFileAuditLogger(path)
	if err != nil {
		t.Fatalf("NewFileAuditLogger: %v", err)
	}

	meta := map[string]string{"memory_id": "abc123", "tags": "important"}
	if err := logger.LogDataEvent(context.Background(), "agent-1", "memory", "create", meta); err != nil {
		t.Fatalf("LogDataEvent: %v", err)
	}
	logger.Close()

	f, _ := os.Open(path)
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Scan()

	var event domain.AuditEvent
	json.Unmarshal(scanner.Bytes(), &event)

	if event.Type != domain.AuditDataEvent {
		t.Errorf("Type = %q, want %q", event.Type, domain.AuditDataEvent)
	}
	if event.Actor != "agent-1" {
		t.Errorf("Actor = %q", event.Actor)
	}
	if event.Detail["memory_id"] != "abc123" {
		t.Errorf("Detail[memory_id] = %q", event.Detail["memory_id"])
	}
}

func TestFileAuditLogger_EnforceRetention_MaxAge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	logger, err := NewFileAuditLogger(path)
	if err != nil {
		t.Fatalf("NewFileAuditLogger: %v", err)
	}

	// Write an old event (2 hours ago) and a recent event.
	oldEvent := domain.AuditEvent{
		Timestamp: time.Now().Add(-2 * time.Hour),
		Type:      domain.AuditLLMCall,
		Detail:    map[string]string{"age": "old"},
	}
	newEvent := domain.AuditEvent{
		Timestamp: time.Now(),
		Type:      domain.AuditToolExec,
		Detail:    map[string]string{"age": "new"},
	}
	logger.Log(context.Background(), oldEvent)
	logger.Log(context.Background(), newEvent)

	logger.SetRetention(RetentionPolicy{MaxAge: 1 * time.Hour})

	removed, err := logger.EnforceRetention(context.Background())
	if err != nil {
		t.Fatalf("EnforceRetention: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}

	// Read back and verify only the new event remains.
	logger.Close()
	f, _ := os.Open(path)
	defer f.Close()
	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		count++
		var event domain.AuditEvent
		json.Unmarshal(scanner.Bytes(), &event)
		if event.Detail["age"] != "new" {
			t.Errorf("expected only new events, got Detail[age]=%q", event.Detail["age"])
		}
	}
	if count != 1 {
		t.Errorf("expected 1 remaining line, got %d", count)
	}
}

func TestFileAuditLogger_EnforceRetention_MaxSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	logger, err := NewFileAuditLogger(path)
	if err != nil {
		t.Fatalf("NewFileAuditLogger: %v", err)
	}

	// Write many events to exceed size limit.
	for i := 0; i < 100; i++ {
		logger.Log(context.Background(), domain.AuditEvent{
			Type:   domain.AuditLLMCall,
			Detail: map[string]string{"index": fmt.Sprintf("%d", i), "padding": "some data to make the line longer for testing"},
		})
	}

	// Set a small size limit.
	logger.SetRetention(RetentionPolicy{MaxSize: 500})

	removed, err := logger.EnforceRetention(context.Background())
	if err != nil {
		t.Fatalf("EnforceRetention: %v", err)
	}
	if removed == 0 {
		t.Error("expected some entries to be removed")
	}

	// Verify file is under the size limit.
	logger.Close()
	info, _ := os.Stat(path)
	if info.Size() > 500 {
		t.Errorf("file size = %d, want <= 500", info.Size())
	}
}

func TestFileAuditLogger_EnforceRetention_NoPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	logger, err := NewFileAuditLogger(path)
	if err != nil {
		t.Fatalf("NewFileAuditLogger: %v", err)
	}

	logger.Log(context.Background(), domain.AuditEvent{Type: domain.AuditLLMCall})

	// No retention policy set — should be a no-op.
	removed, err := logger.EnforceRetention(context.Background())
	if err != nil {
		t.Fatalf("EnforceRetention: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0", removed)
	}
	logger.Close()
}

func TestFileAuditLogger_EnforceRetention_ContinueWriting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	logger, err := NewFileAuditLogger(path)
	if err != nil {
		t.Fatalf("NewFileAuditLogger: %v", err)
	}

	logger.Log(context.Background(), domain.AuditEvent{
		Timestamp: time.Now().Add(-2 * time.Hour),
		Type:      domain.AuditLLMCall,
	})

	logger.SetRetention(RetentionPolicy{MaxAge: 1 * time.Hour})
	logger.EnforceRetention(context.Background())

	// Should be able to continue writing after retention enforcement.
	err = logger.Log(context.Background(), domain.AuditEvent{
		Type:   domain.AuditToolExec,
		Detail: map[string]string{"test": "after-retention"},
	})
	if err != nil {
		t.Fatalf("Log after retention: %v", err)
	}
	logger.Close()

	// Verify the new entry was written.
	f, _ := os.Open(path)
	defer f.Close()
	scanner := bufio.NewScanner(f)
	found := false
	for scanner.Scan() {
		var event domain.AuditEvent
		json.Unmarshal(scanner.Bytes(), &event)
		if event.Detail["test"] == "after-retention" {
			found = true
		}
	}
	if !found {
		t.Error("expected to find event written after retention enforcement")
	}
}

func TestParseRetentionMaxSize(t *testing.T) {
	tests := []struct {
		input string
		want  int64
		err   bool
	}{
		{"", 0, false},
		{"100MB", 100 * 1024 * 1024, false},
		{"1GB", 1024 * 1024 * 1024, false},
		{"512KB", 512 * 1024, false},
		{"1024B", 1024, false},
		{"100", 100, false},
		{"abc", 0, true},
	}
	for _, tc := range tests {
		got, err := ParseRetentionMaxSize(tc.input)
		if tc.err && err == nil {
			t.Errorf("ParseRetentionMaxSize(%q) expected error", tc.input)
		}
		if !tc.err && err != nil {
			t.Errorf("ParseRetentionMaxSize(%q) unexpected error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("ParseRetentionMaxSize(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestFileAuditLogger_ComplianceFieldsSerialization(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	logger, err := NewFileAuditLogger(path)
	if err != nil {
		t.Fatalf("NewFileAuditLogger: %v", err)
	}

	event := domain.AuditEvent{
		Type:     domain.AuditAccessLog,
		Actor:    "user-abc",
		Resource: "session-123",
		Action:   "delete",
		Outcome:  "denied",
		Detail:   map[string]string{"reason": "unauthorized"},
	}
	logger.Log(context.Background(), event)
	logger.Close()

	f, _ := os.Open(path)
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Scan()

	var read domain.AuditEvent
	json.Unmarshal(scanner.Bytes(), &read)

	if read.Actor != "user-abc" {
		t.Errorf("Actor = %q, want %q", read.Actor, "user-abc")
	}
	if read.Resource != "session-123" {
		t.Errorf("Resource = %q", read.Resource)
	}
	if read.Action != "delete" {
		t.Errorf("Action = %q", read.Action)
	}
	if read.Outcome != "denied" {
		t.Errorf("Outcome = %q", read.Outcome)
	}
}
