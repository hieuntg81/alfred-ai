package tool

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/usecase/process"
)

func newTestProcessTool(t *testing.T) (*ProcessTool, *process.Manager) {
	t.Helper()
	pm := process.NewManager(process.ManagerConfig{
		MaxSessions:     5,
		SessionTTL:      10 * time.Minute,
		OutputBufferMax: 1024 * 1024,
		CleanupInterval: 1 * time.Hour,
	}, nil, newTestLogger())
	t.Cleanup(func() { pm.Stop(context.Background()) })
	return NewProcessTool(pm, newTestLogger()), pm
}

func execProcessTool(t *testing.T, tool *ProcessTool, params any) *domain.ToolResult {
	t.Helper()
	data, _ := json.Marshal(params)
	result, err := tool.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return result
}

func TestProcessToolListEmpty(t *testing.T) {
	pt, _ := newTestProcessTool(t)
	result := execProcessTool(t, pt, map[string]any{"action": "list"})
	if result.IsError {
		t.Fatalf("list failed: %s", result.Content)
	}
	// Should return empty array
	if !strings.Contains(result.Content, "[]") {
		t.Errorf("expected empty array, got %s", result.Content)
	}
}

func TestProcessToolPollNotFound(t *testing.T) {
	pt, _ := newTestProcessTool(t)
	result := execProcessTool(t, pt, map[string]any{
		"action":     "poll",
		"session_id": "nonexistent",
	})
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
}

func TestProcessToolPollMissingSessionID(t *testing.T) {
	pt, _ := newTestProcessTool(t)
	result := execProcessTool(t, pt, map[string]any{"action": "poll"})
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestProcessToolLogMissingSessionID(t *testing.T) {
	pt, _ := newTestProcessTool(t)
	result := execProcessTool(t, pt, map[string]any{"action": "log"})
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestProcessToolWriteMissingInput(t *testing.T) {
	pt, _ := newTestProcessTool(t)
	result := execProcessTool(t, pt, map[string]any{
		"action":     "write",
		"session_id": "some-id",
	})
	if !result.IsError {
		t.Error("expected error for missing input")
	}
}

func TestProcessToolWriteMissingSessionID(t *testing.T) {
	pt, _ := newTestProcessTool(t)
	result := execProcessTool(t, pt, map[string]any{
		"action": "write",
		"input":  "hello",
	})
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestProcessToolKillMissingSessionID(t *testing.T) {
	pt, _ := newTestProcessTool(t)
	result := execProcessTool(t, pt, map[string]any{"action": "kill"})
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestProcessToolRemoveNotFound(t *testing.T) {
	pt, _ := newTestProcessTool(t)
	result := execProcessTool(t, pt, map[string]any{
		"action":     "remove",
		"session_id": "nonexistent",
	})
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
}

func TestProcessToolClearEmpty(t *testing.T) {
	pt, _ := newTestProcessTool(t)
	result := execProcessTool(t, pt, map[string]any{"action": "clear"})
	if result.IsError {
		t.Fatalf("clear failed: %s", result.Content)
	}
	if !strings.Contains(result.Content, `"cleared": 0`) {
		t.Errorf("expected cleared=0, got %s", result.Content)
	}
}

func TestProcessToolUnknownAction(t *testing.T) {
	pt, _ := newTestProcessTool(t)
	result := execProcessTool(t, pt, map[string]any{"action": "bogus"})
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

func TestProcessToolInvalidParams(t *testing.T) {
	pt, _ := newTestProcessTool(t)
	result, err := pt.Execute(context.Background(), json.RawMessage(`{invalid`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestProcessToolListWithSessions(t *testing.T) {
	pt, pm := newTestProcessTool(t)
	ctx := context.Background()

	cmd := "echo"
	args := []string{"hello"}
	if runtime.GOOS == "windows" {
		cmd = "cmd"
		args = []string{"/c", "echo hello"}
	}

	session, err := pm.Start(ctx, cmd, args, "", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Wait for process to complete
	time.Sleep(200 * time.Millisecond)

	result := execProcessTool(t, pt, map[string]any{"action": "list"})
	if result.IsError {
		t.Fatalf("list failed: %s", result.Content)
	}
	if !strings.Contains(result.Content, session.ID) {
		t.Errorf("list result should contain session ID %q", session.ID)
	}
}

func TestProcessToolPollWithOutput(t *testing.T) {
	pt, pm := newTestProcessTool(t)
	ctx := context.Background()

	cmd := "echo"
	args := []string{"poll-test"}
	if runtime.GOOS == "windows" {
		cmd = "cmd"
		args = []string{"/c", "echo poll-test"}
	}

	session, err := pm.Start(ctx, cmd, args, "", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	result := execProcessTool(t, pt, map[string]any{
		"action":     "poll",
		"session_id": session.ID,
	})
	if result.IsError {
		t.Fatalf("poll failed: %s", result.Content)
	}
	if !strings.Contains(result.Content, "poll-test") {
		t.Errorf("poll result should contain 'poll-test', got %s", result.Content)
	}
}
