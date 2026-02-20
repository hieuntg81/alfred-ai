package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"alfred-ai/internal/usecase/process"
)

func TestShellToolBackgroundNoProcessManager(t *testing.T) {
	sb := newSandbox(t)
	sh := NewShellTool(NewLocalShellBackend(30*time.Second), []string{"echo"}, sb, newTestLogger())
	// No WithProcessManager option

	data, _ := json.Marshal(map[string]any{
		"command":    "echo",
		"args":       []string{"hello"},
		"background": true,
	})

	result, err := sh.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when background=true without ProcessManager")
	}
	if !strings.Contains(result.Content, "not enabled") {
		t.Errorf("error should mention 'not enabled', got %q", result.Content)
	}
}

func TestShellToolBackgroundSuccess(t *testing.T) {
	sb := newSandbox(t)
	pm := process.NewManager(process.ManagerConfig{
		MaxSessions:     5,
		SessionTTL:      10 * time.Minute,
		OutputBufferMax: 1024 * 1024,
		CleanupInterval: 1 * time.Hour,
	}, nil, newTestLogger())
	t.Cleanup(func() { pm.Stop(context.Background()) })

	sh := NewShellTool(NewLocalShellBackend(30*time.Second), []string{"echo"}, sb, newTestLogger(),
		WithProcessManager(pm))

	data, _ := json.Marshal(map[string]any{
		"command":    "echo",
		"args":       []string{"background-test"},
		"background": true,
	})

	result, err := sh.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content)
	}

	// Should return JSON with status and session_id
	var resp map[string]string
	if err := json.Unmarshal([]byte(result.Content), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["status"] != "running" {
		t.Errorf("status = %q, want 'running'", resp["status"])
	}
	if resp["session_id"] == "" {
		t.Error("expected non-empty session_id")
	}
}

func TestShellToolBackgroundSecurityValidation(t *testing.T) {
	sb := newSandbox(t)
	pm := process.NewManager(process.ManagerConfig{
		MaxSessions:     5,
		SessionTTL:      10 * time.Minute,
		OutputBufferMax: 1024 * 1024,
		CleanupInterval: 1 * time.Hour,
	}, nil, newTestLogger())
	t.Cleanup(func() { pm.Stop(context.Background()) })

	// Only allow "echo", not "rm"
	sh := NewShellTool(NewLocalShellBackend(30*time.Second), []string{"echo"}, sb, newTestLogger(),
		WithProcessManager(pm))

	data, _ := json.Marshal(map[string]any{
		"command":    "rm",
		"args":       []string{"-rf", "/"},
		"background": true,
	})

	result, err := sh.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for command not in allowlist even with background=true")
	}
	if !strings.Contains(result.Content, "not in allowlist") {
		t.Errorf("error should mention allowlist, got %q", result.Content)
	}
}

func TestShellToolForegroundUnchanged(t *testing.T) {
	sb := newSandbox(t)
	pm := process.NewManager(process.ManagerConfig{
		MaxSessions:     5,
		SessionTTL:      10 * time.Minute,
		OutputBufferMax: 1024 * 1024,
		CleanupInterval: 1 * time.Hour,
	}, nil, newTestLogger())
	t.Cleanup(func() { pm.Stop(context.Background()) })

	sh := NewShellTool(NewLocalShellBackend(30*time.Second), []string{"echo"}, sb, newTestLogger(),
		WithProcessManager(pm))

	// Without background flag → synchronous execution
	data, _ := json.Marshal(map[string]any{
		"command": "echo",
		"args":    []string{"sync-test"},
	})

	result, err := sh.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "sync-test") {
		t.Errorf("output should contain 'sync-test', got %q", result.Content)
	}
	// Should NOT contain session_id (foreground mode)
	if strings.Contains(result.Content, "session_id") {
		t.Error("foreground execution should not return session_id")
	}
}
