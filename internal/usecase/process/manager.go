package process

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"alfred-ai/internal/domain"
)

// DefaultLogLimit is the default number of lines returned by Log when no limit is specified.
const DefaultLogLimit = 100

// ManagerConfig holds configuration for the Manager.
type ManagerConfig struct {
	MaxSessions     int           // max concurrent running sessions (default: 10)
	SessionTTL      time.Duration // auto-cleanup completed sessions after this (default: 30m)
	OutputBufferMax int           // max bytes of output to buffer per session (default: 1MB)
	CleanupInterval time.Duration // how often to run TTL cleanup (default: 1m)
}

// processEntry holds the runtime state for a single background process session.
type processEntry struct {
	session         domain.ProcessSession
	cmd             *exec.Cmd
	cancel          context.CancelFunc
	stdin           io.WriteCloser
	stdout          *ringBuffer
	stderr          *ringBuffer
	stdoutPollIndex int64 // tracks how far poll has read stdout (total bytes written offset)
	stderrPollIndex int64 // tracks how far poll has read stderr (total bytes written offset)
	done            chan struct{}
}

// Manager orchestrates background process sessions.
// Sessions are in-memory only and scoped per agent.
type Manager struct {
	sessions map[string]*processEntry
	mu       sync.Mutex
	config   ManagerConfig
	bus      domain.EventBus
	logger   *slog.Logger
	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewManager creates a Manager and starts the TTL cleanup goroutine.
func NewManager(cfg ManagerConfig, bus domain.EventBus, logger *slog.Logger) *Manager {
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 10
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 30 * time.Minute
	}
	if cfg.OutputBufferMax <= 0 {
		cfg.OutputBufferMax = 1024 * 1024
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = 1 * time.Minute
	}

	pm := &Manager{
		sessions: make(map[string]*processEntry),
		config:   cfg,
		bus:      bus,
		logger:   logger,
		stopCh:   make(chan struct{}),
	}
	go pm.cleanupLoop()
	return pm
}

// Start launches a background process and returns the session immediately.
func (pm *Manager) Start(ctx context.Context, command string, args []string, workDir string, agentID string) (*domain.ProcessSession, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Count active running sessions for this agent.
	activeCount := 0
	for _, entry := range pm.sessions {
		if entry.session.AgentID == agentID && entry.session.Status == domain.ProcessStatusRunning {
			activeCount++
		}
	}
	if activeCount >= pm.config.MaxSessions {
		return nil, domain.NewSubSystemError("process", "ProcessManager.Start", domain.ErrLimitReached,
			fmt.Sprintf("agent %q has %d/%d active sessions", agentID, activeCount, pm.config.MaxSessions))
	}

	sessionID := pm.newID()

	// Use a detached context so the process outlives the request.
	cmdCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(cmdCtx, command, args...)
	cmd.Dir = workDir

	stdoutBuf := newRingBuffer(pm.config.OutputBufferMax)
	stderrBuf := newRingBuffer(pm.config.OutputBufferMax)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("processmanager: stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("processmanager: start: %w", err)
	}

	session := domain.ProcessSession{
		ID:        sessionID,
		Command:   command,
		Args:      args,
		WorkDir:   workDir,
		Status:    domain.ProcessStatusRunning,
		StartedAt: time.Now(),
		AgentID:   agentID,
	}

	entry := &processEntry{
		session: session,
		cmd:     cmd,
		cancel:  cancel,
		stdin:   stdinPipe,
		stdout:  stdoutBuf,
		stderr:  stderrBuf,
		done:    make(chan struct{}),
	}
	pm.sessions[sessionID] = entry

	// Monitor process completion in a goroutine.
	go pm.waitForCompletion(entry)

	pm.emitEvent(ctx, domain.EventProcessStarted, session)
	pm.logger.Info("process started", "session_id", sessionID, "command", command)

	return &session, nil
}

// List returns summary entries for all sessions (optionally filtered by agentID).
func (pm *Manager) List(agentID string) []domain.ProcessListEntry {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	entries := make([]domain.ProcessListEntry, 0, len(pm.sessions))
	for _, e := range pm.sessions {
		if agentID != "" && e.session.AgentID != agentID {
			continue
		}
		entries = append(entries, domain.ProcessListEntry{
			ID:        e.session.ID,
			Command:   e.session.Command,
			Status:    e.session.Status,
			StartedAt: e.session.StartedAt,
			EndedAt:   e.session.EndedAt,
			ExitCode:  e.session.ExitCode,
		})
	}
	return entries
}

// Poll returns new output since the last poll and the current status.
func (pm *Manager) Poll(sessionID string) (*domain.ProcessPollResult, error) {
	pm.mu.Lock()
	entry, ok := pm.sessions[sessionID]
	if !ok {
		pm.mu.Unlock()
		return nil, domain.NewSubSystemError("process", "ProcessManager.Poll", domain.ErrNotFound, sessionID)
	}
	prevStdout := entry.stdoutPollIndex
	prevStderr := entry.stderrPollIndex
	pm.mu.Unlock()

	newOut := entry.stdout.ReadFrom(prevStdout)
	newErr := entry.stderr.ReadFrom(prevStderr)

	newStdoutIndex := entry.stdout.TotalWritten()
	newStderrIndex := entry.stderr.TotalWritten()

	pm.mu.Lock()
	entry.stdoutPollIndex = newStdoutIndex
	entry.stderrPollIndex = newStderrIndex
	status := entry.session.Status
	exitCode := entry.session.ExitCode
	pm.mu.Unlock()

	combined := newOut
	if newErr != "" {
		if combined != "" {
			combined += "\n"
		}
		combined += "STDERR:\n" + newErr
	}

	return &domain.ProcessPollResult{
		SessionID: sessionID,
		Status:    status,
		NewOutput: combined,
		ExitCode:  exitCode,
	}, nil
}

// Log returns buffered output with line-based offset/limit pagination.
func (pm *Manager) Log(sessionID string, offset, limit int) (*domain.ProcessOutput, error) {
	pm.mu.Lock()
	entry, ok := pm.sessions[sessionID]
	if !ok {
		pm.mu.Unlock()
		return nil, domain.NewSubSystemError("process", "ProcessManager.Log", domain.ErrNotFound, sessionID)
	}
	pm.mu.Unlock()

	stdout := entry.stdout.String()
	stderr := entry.stderr.String()

	stdout = strings.TrimRight(stdout, "\n")
	if stdout == "" {
		return &domain.ProcessOutput{
			SessionID:  sessionID,
			Stdout:     "",
			Stderr:     stderr,
			TotalLines: 0,
			Offset:     0,
			HasMore:    false,
		}, nil
	}

	lines := strings.Split(stdout, "\n")
	totalLines := len(lines)

	if offset < 0 {
		offset = 0
	}
	if offset >= totalLines {
		offset = totalLines
	}
	if limit <= 0 {
		limit = DefaultLogLimit
	}
	end := offset + limit
	if end > totalLines {
		end = totalLines
	}

	return &domain.ProcessOutput{
		SessionID:  sessionID,
		Stdout:     strings.Join(lines[offset:end], "\n"),
		Stderr:     stderr,
		TotalLines: totalLines,
		Offset:     offset,
		HasMore:    end < totalLines,
	}, nil
}

// Write sends data to the process's stdin.
func (pm *Manager) Write(sessionID, input string) error {
	pm.mu.Lock()
	entry, ok := pm.sessions[sessionID]
	if !ok {
		pm.mu.Unlock()
		return domain.NewSubSystemError("process", "ProcessManager.Write", domain.ErrNotFound, sessionID)
	}
	if entry.session.Status != domain.ProcessStatusRunning {
		pm.mu.Unlock()
		return domain.NewSubSystemError("process", "ProcessManager.Write", domain.ErrInvalidInput, "process is not running")
	}
	stdin := entry.stdin
	pm.mu.Unlock()

	if stdin == nil {
		return domain.NewSubSystemError("process", "ProcessManager.Write", domain.ErrInvalidInput, "stdin is closed")
	}
	_, err := io.WriteString(stdin, input)
	return err
}

// Kill terminates a running process.
func (pm *Manager) Kill(ctx context.Context, sessionID string) error {
	pm.mu.Lock()
	entry, ok := pm.sessions[sessionID]
	if !ok {
		pm.mu.Unlock()
		return domain.NewSubSystemError("process", "ProcessManager.Kill", domain.ErrNotFound, sessionID)
	}
	if entry.session.Status != domain.ProcessStatusRunning {
		pm.mu.Unlock()
		return domain.NewSubSystemError("process", "ProcessManager.Kill", domain.ErrInvalidInput, "process is not running")
	}
	// Set status BEFORE cancel so waitForCompletion sees it and skips status update.
	entry.session.Status = domain.ProcessStatusKilled
	now := time.Now()
	entry.session.EndedAt = &now
	pm.mu.Unlock()

	entry.cancel()
	<-entry.done

	pm.emitEvent(ctx, domain.EventProcessKilled, entry.session)
	pm.logger.Info("process killed", "session_id", sessionID)
	return nil
}

// Clear removes all finished (completed/failed/killed) sessions.
func (pm *Manager) Clear() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	removed := 0
	for id, entry := range pm.sessions {
		if entry.session.Status != domain.ProcessStatusRunning {
			delete(pm.sessions, id)
			removed++
		}
	}
	return removed
}

// Remove removes a specific session. Kills it if running, clears if finished.
func (pm *Manager) Remove(ctx context.Context, sessionID string) error {
	pm.mu.Lock()
	entry, ok := pm.sessions[sessionID]
	if !ok {
		pm.mu.Unlock()
		return domain.NewSubSystemError("process", "ProcessManager.Remove", domain.ErrNotFound, sessionID)
	}

	if entry.session.Status == domain.ProcessStatusRunning {
		pm.mu.Unlock()
		if err := pm.Kill(ctx, sessionID); err != nil {
			return err
		}
		pm.mu.Lock()
	}

	delete(pm.sessions, sessionID)
	pm.mu.Unlock()
	return nil
}

// Stop shuts down the cleanup goroutine and kills all running processes.
func (pm *Manager) Stop(ctx context.Context) {
	pm.stopOnce.Do(func() {
		close(pm.stopCh)
	})

	pm.mu.Lock()
	var running []*processEntry
	now := time.Now()
	for _, e := range pm.sessions {
		if e.session.Status == domain.ProcessStatusRunning {
			e.session.Status = domain.ProcessStatusKilled
			e.session.EndedAt = &now
			running = append(running, e)
		}
	}
	pm.mu.Unlock()

	for _, e := range running {
		e.cancel()
		<-e.done
	}
}

// --- internal ---

func (pm *Manager) waitForCompletion(entry *processEntry) {
	err := entry.cmd.Wait()
	close(entry.done)

	pm.mu.Lock()
	// Only update status and emit completion event if Kill()/Stop() hasn't already set it.
	emitCompletion := entry.session.Status == domain.ProcessStatusRunning
	if emitCompletion {
		now := time.Now()
		entry.session.EndedAt = &now
		if err != nil {
			entry.session.Status = domain.ProcessStatusFailed
			if exitErr, ok := err.(*exec.ExitError); ok {
				code := exitErr.ExitCode()
				entry.session.ExitCode = &code
			}
		} else {
			entry.session.Status = domain.ProcessStatusCompleted
			code := 0
			entry.session.ExitCode = &code
		}
	}
	if entry.stdin != nil {
		entry.stdin.Close()
	}
	pm.mu.Unlock()

	if emitCompletion {
		pm.emitEvent(context.Background(), domain.EventProcessCompleted, entry.session)
	}
	pm.logger.Info("process finished", "session_id", entry.session.ID, "status", entry.session.Status)
}

func (pm *Manager) cleanupLoop() {
	ticker := time.NewTicker(pm.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-pm.stopCh:
			return
		case <-ticker.C:
			pm.cleanupExpired()
		}
	}
}

func (pm *Manager) cleanupExpired() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	cutoff := time.Now().Add(-pm.config.SessionTTL)
	for id, entry := range pm.sessions {
		if entry.session.Status != domain.ProcessStatusRunning && entry.session.EndedAt != nil {
			if entry.session.EndedAt.Before(cutoff) {
				delete(pm.sessions, id)
				pm.logger.Debug("process session expired", "session_id", id)
			}
		}
	}
}

func (pm *Manager) emitEvent(ctx context.Context, eventType domain.EventType, payload any) {
	if pm.bus == nil {
		return
	}
	data, _ := json.Marshal(payload)
	pm.bus.Publish(ctx, domain.Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Payload:   data,
	})
}

func (pm *Manager) newID() string {
	t := time.Now()
	entropy := ulid.Monotonic(rand.New(rand.NewSource(t.UnixNano())), 0)
	return ulid.MustNew(ulid.Timestamp(t), entropy).String()
}
