package domain

import "time"

// ProcessStatus represents the lifecycle state of a background process.
type ProcessStatus string

const (
	ProcessStatusRunning   ProcessStatus = "running"
	ProcessStatusCompleted ProcessStatus = "completed"
	ProcessStatusFailed    ProcessStatus = "failed"
	ProcessStatusKilled    ProcessStatus = "killed"
)

// ProcessSession represents a background process tracked by the ProcessManager.
type ProcessSession struct {
	ID        string        `json:"id"`
	Command   string        `json:"command"`
	Args      []string      `json:"args"`
	WorkDir   string        `json:"workdir"`
	Status    ProcessStatus `json:"status"`
	ExitCode  *int          `json:"exit_code,omitempty"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   *time.Time    `json:"ended_at,omitempty"`
	AgentID   string        `json:"agent_id,omitempty"`
}

// ProcessOutput represents buffered output with line-based pagination.
type ProcessOutput struct {
	SessionID  string `json:"session_id"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	TotalLines int    `json:"total_lines"`
	Offset     int    `json:"offset"`
	HasMore    bool   `json:"has_more"`
}

// ProcessPollResult is returned by the poll action with incremental output.
type ProcessPollResult struct {
	SessionID string        `json:"session_id"`
	Status    ProcessStatus `json:"status"`
	NewOutput string        `json:"new_output"`
	ExitCode  *int          `json:"exit_code,omitempty"`
}

// ProcessListEntry is a summary view of a session for the list action.
type ProcessListEntry struct {
	ID        string        `json:"id"`
	Command   string        `json:"command"`
	Status    ProcessStatus `json:"status"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   *time.Time    `json:"ended_at,omitempty"`
	ExitCode  *int          `json:"exit_code,omitempty"`
}
