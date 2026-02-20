package multiagent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Workspace manages per-agent directories under a common base path.
type Workspace struct {
	baseDir string
}

// NewWorkspace creates a Workspace rooted at baseDir.
func NewWorkspace(baseDir string) *Workspace {
	return &Workspace{baseDir: baseDir}
}

// AgentDir returns (and creates) the data directory for the given agent.
// Path: <baseDir>/agents/<agentID>
func (w *Workspace) AgentDir(agentID string) (string, error) {
	if agentID == "" {
		return "", fmt.Errorf("workspace: agent ID must not be empty")
	}
	if strings.ContainsAny(agentID, `/\`) || strings.Contains(agentID, "..") {
		return "", fmt.Errorf("workspace: agent ID %q contains invalid path characters", agentID)
	}
	dir := filepath.Join(w.baseDir, "agents", agentID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("workspace: create agent dir: %w", err)
	}
	return dir, nil
}

// SessionDir returns (and creates) the sessions directory for the given agent.
// Path: <baseDir>/agents/<agentID>/sessions
func (w *Workspace) SessionDir(agentID string) (string, error) {
	agentDir, err := w.AgentDir(agentID)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(agentDir, "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("workspace: create session dir: %w", err)
	}
	return dir, nil
}
