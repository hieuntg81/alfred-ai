package process

import (
	"context"
	"io"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// recordingBus captures published events for assertions.
type recordingBus struct {
	mu     sync.Mutex
	events []domain.Event
}

func (b *recordingBus) Publish(_ context.Context, evt domain.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, evt)
}

func (b *recordingBus) Subscribe(domain.EventType, domain.EventHandler) func() { return func() {} }
func (b *recordingBus) SubscribeAll(domain.EventHandler) func()               { return func() {} }
func (b *recordingBus) Close()                                                {}

func (b *recordingBus) Events() []domain.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := make([]domain.Event, len(b.events))
	copy(cp, b.events)
	return cp
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	pm := NewManager(ManagerConfig{
		MaxSessions:     5,
		SessionTTL:      10 * time.Minute,
		OutputBufferMax: 1024 * 1024,
		CleanupInterval: 1 * time.Hour, // don't auto-cleanup during tests
	}, nil, newTestLogger())
	t.Cleanup(func() { pm.Stop(context.Background()) })
	return pm
}

func TestManagerStart(t *testing.T) {
	pm := newTestManager(t)
	ctx := context.Background()

	session, err := pm.Start(ctx, echoCommand(), echoArgs("hello"), "", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if session.ID == "" {
		t.Error("expected non-empty session ID")
	}
	if session.Status != domain.ProcessStatusRunning {
		t.Errorf("status = %q, want %q", session.Status, domain.ProcessStatusRunning)
	}

	// Wait for process to complete
	waitForSession(t, pm, session.ID, 2*time.Second)
}

func TestManagerStartMaxSessions(t *testing.T) {
	pm := newTestManager(t)
	ctx := context.Background()

	// Fill up max sessions with long-running processes
	for i := 0; i < 5; i++ {
		_, err := pm.Start(ctx, sleepCommand(), sleepArgs("10"), "", "agent1")
		if err != nil {
			t.Fatalf("Start[%d]: %v", i, err)
		}
	}

	// Next should fail
	_, err := pm.Start(ctx, echoCommand(), echoArgs("overflow"), "", "agent1")
	if err == nil {
		t.Error("expected ErrProcessMaxSessions")
	}

	// Different agent should still succeed (sessions are per-agent)
	_, err = pm.Start(ctx, echoCommand(), echoArgs("ok"), "", "agent2")
	if err != nil {
		t.Fatalf("Start for agent2: %v", err)
	}
}

func TestManagerList(t *testing.T) {
	pm := newTestManager(t)
	ctx := context.Background()

	s1, _ := pm.Start(ctx, echoCommand(), echoArgs("one"), "", "")
	s2, _ := pm.Start(ctx, echoCommand(), echoArgs("two"), "", "")
	waitForSession(t, pm, s1.ID, 2*time.Second)
	waitForSession(t, pm, s2.ID, 2*time.Second)

	entries := pm.List("")
	if len(entries) != 2 {
		t.Errorf("List() returned %d entries, want 2", len(entries))
	}
}

func TestManagerListAgentFilter(t *testing.T) {
	pm := newTestManager(t)
	ctx := context.Background()

	pm.Start(ctx, echoCommand(), echoArgs("a1"), "", "agent1")
	pm.Start(ctx, echoCommand(), echoArgs("a2"), "", "agent2")

	time.Sleep(200 * time.Millisecond)

	entries := pm.List("agent1")
	if len(entries) != 1 {
		t.Errorf("List('agent1') returned %d entries, want 1", len(entries))
	}
}

func TestManagerPoll(t *testing.T) {
	pm := newTestManager(t)
	ctx := context.Background()

	session, _ := pm.Start(ctx, echoCommand(), echoArgs("hello world"), "", "")
	waitForSession(t, pm, session.ID, 2*time.Second)

	result, err := pm.Poll(session.ID)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if !strings.Contains(result.NewOutput, "hello world") {
		t.Errorf("Poll output = %q, want it to contain 'hello world'", result.NewOutput)
	}

	// Second poll should return empty (no new output)
	result2, err := pm.Poll(session.ID)
	if err != nil {
		t.Fatalf("Poll[2]: %v", err)
	}
	if result2.NewOutput != "" {
		t.Errorf("second poll output = %q, want empty", result2.NewOutput)
	}
}

func TestManagerPollNotFound(t *testing.T) {
	pm := newTestManager(t)
	_, err := pm.Poll("nonexistent")
	if err == nil {
		t.Error("expected ErrProcessNotFound")
	}
}

func TestManagerLog(t *testing.T) {
	pm := newTestManager(t)
	ctx := context.Background()

	// Create multi-line output
	var cmd string
	var args []string
	if runtime.GOOS == "windows" {
		cmd = "cmd"
		args = []string{"/c", "echo line1& echo line2& echo line3"}
	} else {
		cmd = "sh"
		args = []string{"-c", "echo line1; echo line2; echo line3"}
	}

	session, _ := pm.Start(ctx, cmd, args, "", "")
	waitForSession(t, pm, session.ID, 2*time.Second)

	output, err := pm.Log(session.ID, 0, 2)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if output.Offset != 0 {
		t.Errorf("Offset = %d, want 0", output.Offset)
	}
	if !output.HasMore {
		t.Error("expected HasMore to be true")
	}

	// Full log
	fullOut, _ := pm.Log(session.ID, 0, 100)
	if fullOut.TotalLines < 3 {
		t.Errorf("TotalLines = %d, want >= 3", fullOut.TotalLines)
	}
}

func TestManagerKill(t *testing.T) {
	pm := newTestManager(t)
	ctx := context.Background()

	session, _ := pm.Start(ctx, sleepCommand(), sleepArgs("60"), "", "")

	err := pm.Kill(ctx, session.ID)
	if err != nil {
		t.Fatalf("Kill: %v", err)
	}

	entries := pm.List("")
	for _, e := range entries {
		if e.ID == session.ID && e.Status != domain.ProcessStatusKilled {
			t.Errorf("status after kill = %q, want %q", e.Status, domain.ProcessStatusKilled)
		}
	}
}

func TestManagerKillNotRunning(t *testing.T) {
	pm := newTestManager(t)
	ctx := context.Background()

	session, _ := pm.Start(ctx, echoCommand(), echoArgs("done"), "", "")
	waitForSession(t, pm, session.ID, 2*time.Second)

	err := pm.Kill(ctx, session.ID)
	if err == nil {
		t.Error("expected ErrProcessNotRunning")
	}
}

func TestManagerClear(t *testing.T) {
	pm := newTestManager(t)
	ctx := context.Background()

	s1, _ := pm.Start(ctx, echoCommand(), echoArgs("done"), "", "")
	s2, _ := pm.Start(ctx, sleepCommand(), sleepArgs("60"), "", "")
	waitForSession(t, pm, s1.ID, 2*time.Second)

	removed := pm.Clear()
	if removed != 1 {
		t.Errorf("Clear() removed %d, want 1", removed)
	}

	entries := pm.List("")
	if len(entries) != 1 {
		t.Errorf("List() after clear = %d entries, want 1", len(entries))
	}
	if entries[0].ID != s2.ID {
		t.Errorf("remaining session = %s, want %s", entries[0].ID, s2.ID)
	}
}

func TestManagerRemoveRunning(t *testing.T) {
	pm := newTestManager(t)
	ctx := context.Background()

	session, _ := pm.Start(ctx, sleepCommand(), sleepArgs("60"), "", "")

	err := pm.Remove(ctx, session.ID)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}

	entries := pm.List("")
	if len(entries) != 0 {
		t.Errorf("List() after remove = %d entries, want 0", len(entries))
	}
}

func TestManagerRemoveFinished(t *testing.T) {
	pm := newTestManager(t)
	ctx := context.Background()

	session, _ := pm.Start(ctx, echoCommand(), echoArgs("done"), "", "")
	waitForSession(t, pm, session.ID, 2*time.Second)

	err := pm.Remove(ctx, session.ID)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}

	entries := pm.List("")
	if len(entries) != 0 {
		t.Errorf("List() after remove = %d entries, want 0", len(entries))
	}
}

func TestManagerRemoveNotFound(t *testing.T) {
	pm := newTestManager(t)
	err := pm.Remove(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected ErrProcessNotFound")
	}
}

func TestManagerCompletedExitCode(t *testing.T) {
	pm := newTestManager(t)
	ctx := context.Background()

	session, _ := pm.Start(ctx, echoCommand(), echoArgs("success"), "", "")
	waitForSession(t, pm, session.ID, 2*time.Second)

	entries := pm.List("")
	for _, e := range entries {
		if e.ID == session.ID {
			if e.Status != domain.ProcessStatusCompleted {
				t.Errorf("status = %q, want %q", e.Status, domain.ProcessStatusCompleted)
			}
			if e.ExitCode == nil || *e.ExitCode != 0 {
				t.Errorf("exit code = %v, want 0", e.ExitCode)
			}
		}
	}
}

func TestManagerFailedExitCode(t *testing.T) {
	pm := newTestManager(t)
	ctx := context.Background()

	var cmd string
	var args []string
	if runtime.GOOS == "windows" {
		cmd = "cmd"
		args = []string{"/c", "exit 1"}
	} else {
		cmd = "sh"
		args = []string{"-c", "exit 1"}
	}

	session, _ := pm.Start(ctx, cmd, args, "", "")
	waitForSession(t, pm, session.ID, 2*time.Second)

	entries := pm.List("")
	for _, e := range entries {
		if e.ID == session.ID {
			if e.Status != domain.ProcessStatusFailed {
				t.Errorf("status = %q, want %q", e.Status, domain.ProcessStatusFailed)
			}
			if e.ExitCode == nil || *e.ExitCode != 1 {
				t.Errorf("exit code = %v, want 1", e.ExitCode)
			}
		}
	}
}

func TestManagerStop(t *testing.T) {
	pm := NewManager(ManagerConfig{
		MaxSessions:     5,
		SessionTTL:      10 * time.Minute,
		OutputBufferMax: 1024 * 1024,
		CleanupInterval: 1 * time.Hour,
	}, nil, newTestLogger())

	ctx := context.Background()
	pm.Start(ctx, sleepCommand(), sleepArgs("60"), "", "")
	pm.Start(ctx, sleepCommand(), sleepArgs("60"), "", "")

	pm.Stop(ctx)

	// All processes should be stopped (no hanging goroutines)
	entries := pm.List("")
	for _, e := range entries {
		if e.Status == domain.ProcessStatusRunning {
			t.Errorf("session %s still running after Stop", e.ID)
		}
	}
}

func TestManagerWithEvents(t *testing.T) {
	bus := &recordingBus{}
	pm := NewManager(ManagerConfig{
		MaxSessions:     5,
		SessionTTL:      10 * time.Minute,
		OutputBufferMax: 1024 * 1024,
		CleanupInterval: 1 * time.Hour,
	}, bus, newTestLogger())
	t.Cleanup(func() { pm.Stop(context.Background()) })

	ctx := context.Background()
	session, _ := pm.Start(ctx, echoCommand(), echoArgs("hi"), "", "")
	waitForSession(t, pm, session.ID, 2*time.Second)

	// Allow event goroutine to fire
	time.Sleep(100 * time.Millisecond)

	events := bus.Events()
	hasStarted := false
	hasCompleted := false
	for _, e := range events {
		if e.Type == domain.EventProcessStarted {
			hasStarted = true
		}
		if e.Type == domain.EventProcessCompleted {
			hasCompleted = true
		}
	}
	if !hasStarted {
		t.Error("expected EventProcessStarted")
	}
	if !hasCompleted {
		t.Error("expected EventProcessCompleted")
	}
}

func TestManagerKillConcurrentWithCompletion(t *testing.T) {
	pm := newTestManager(t)
	ctx := context.Background()

	cmd, args := shCommand("sleep 0.5")
	session, err := pm.Start(ctx, cmd, args, "", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	err = pm.Kill(ctx, session.ID)
	if err != nil {
		t.Logf("Kill returned error (process may have completed first): %v", err)
		return
	}

	pm.mu.Lock()
	entry := pm.sessions[session.ID]
	status := entry.session.Status
	pm.mu.Unlock()

	if status != domain.ProcessStatusKilled {
		t.Errorf("status after Kill = %q, want %q", status, domain.ProcessStatusKilled)
	}
}

func TestManagerKillNoCompletionEvent(t *testing.T) {
	bus := &recordingBus{}
	pm := NewManager(ManagerConfig{
		MaxSessions:     5,
		SessionTTL:      10 * time.Minute,
		OutputBufferMax: 1024 * 1024,
		CleanupInterval: 1 * time.Hour,
	}, bus, newTestLogger())
	t.Cleanup(func() { pm.Stop(context.Background()) })

	ctx := context.Background()
	session, _ := pm.Start(ctx, sleepCommand(), sleepArgs("60"), "", "")

	err := pm.Kill(ctx, session.ID)
	if err != nil {
		t.Fatalf("Kill: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	events := bus.Events()
	completedCount := 0
	killedCount := 0
	for _, e := range events {
		if e.Type == domain.EventProcessCompleted {
			completedCount++
		}
		if e.Type == domain.EventProcessKilled {
			killedCount++
		}
	}

	if killedCount != 1 {
		t.Errorf("EventProcessKilled count = %d, want 1", killedCount)
	}
	if completedCount != 0 {
		t.Errorf("EventProcessCompleted count = %d, want 0 (should not emit when killed)", completedCount)
	}
}

func TestManagerStopSetsKilledStatus(t *testing.T) {
	pm := NewManager(ManagerConfig{
		MaxSessions:     5,
		SessionTTL:      10 * time.Minute,
		OutputBufferMax: 1024 * 1024,
		CleanupInterval: 1 * time.Hour,
	}, nil, newTestLogger())

	ctx := context.Background()
	pm.Start(ctx, sleepCommand(), sleepArgs("60"), "", "")
	pm.Start(ctx, sleepCommand(), sleepArgs("60"), "", "")

	pm.Stop(ctx)

	entries := pm.List("")
	for _, e := range entries {
		if e.Status != domain.ProcessStatusKilled {
			t.Errorf("session %s status = %q after Stop, want %q", e.ID, e.Status, domain.ProcessStatusKilled)
		}
	}
}

func TestManagerPollStderr(t *testing.T) {
	pm := newTestManager(t)
	ctx := context.Background()

	cmd, args := shCommand("echo stdout_data; echo stderr_data >&2")
	session, err := pm.Start(ctx, cmd, args, "", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForSession(t, pm, session.ID, 2*time.Second)

	result, err := pm.Poll(session.ID)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if !strings.Contains(result.NewOutput, "stdout_data") {
		t.Errorf("Poll output missing stdout_data: %q", result.NewOutput)
	}
	if !strings.Contains(result.NewOutput, "stderr_data") {
		t.Errorf("Poll output missing stderr_data: %q", result.NewOutput)
	}

	result2, err := pm.Poll(session.ID)
	if err != nil {
		t.Fatalf("Poll[2]: %v", err)
	}
	if result2.NewOutput != "" {
		t.Errorf("second poll output = %q, want empty", result2.NewOutput)
	}
}

func TestManagerLogLineCountingEmpty(t *testing.T) {
	pm := newTestManager(t)
	ctx := context.Background()

	cmd, args := shCommand("echo -n ''")
	session, err := pm.Start(ctx, cmd, args, "", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForSession(t, pm, session.ID, 2*time.Second)

	output, err := pm.Log(session.ID, 0, 100)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if output.TotalLines != 0 {
		t.Errorf("TotalLines for empty output = %d, want 0", output.TotalLines)
	}
}

func TestManagerLogLineCountingTrailingNewline(t *testing.T) {
	pm := newTestManager(t)
	ctx := context.Background()

	cmd, args := shCommand("echo line1; echo line2; echo line3")
	session, err := pm.Start(ctx, cmd, args, "", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForSession(t, pm, session.ID, 2*time.Second)

	output, err := pm.Log(session.ID, 0, 100)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	if output.TotalLines != 3 {
		t.Errorf("TotalLines = %d, want 3 (stdout=%q)", output.TotalLines, output.Stdout)
	}
}

// --- helpers ---

func echoCommand() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "echo"
}

func echoArgs(msg string) []string {
	if runtime.GOOS == "windows" {
		return []string{"/c", "echo " + msg}
	}
	return []string{msg}
}

func sleepCommand() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "sleep"
}

func sleepArgs(seconds string) []string {
	if runtime.GOOS == "windows" {
		return []string{"/c", "timeout /t " + seconds}
	}
	return []string{seconds}
}

func shCommand(script string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", script}
	}
	return "sh", []string{"-c", script}
}

func waitForSession(t *testing.T, pm *Manager, sessionID string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for session %s to complete", sessionID)
		default:
			pm.mu.Lock()
			entry, ok := pm.sessions[sessionID]
			if ok && entry.session.Status != domain.ProcessStatusRunning {
				pm.mu.Unlock()
				return
			}
			pm.mu.Unlock()
			time.Sleep(50 * time.Millisecond)
		}
	}
}
