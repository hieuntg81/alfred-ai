package tool

import (
	"context"
	"encoding/json"
	"log/slog"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/usecase/process"
)

// ProcessTool exposes background process management to the LLM via function calling.
type ProcessTool struct {
	manager *process.Manager
	logger  *slog.Logger
}

// NewProcessTool creates a process tool backed by the given ProcessManager.
func NewProcessTool(manager *process.Manager, logger *slog.Logger) *ProcessTool {
	return &ProcessTool{manager: manager, logger: logger}
}

func (t *ProcessTool) Name() string { return "process" }
func (t *ProcessTool) Description() string {
	return "Manage background process sessions: list running/completed processes, poll for new output, read logs with pagination, write to stdin, kill processes, and clean up sessions."
}

func (t *ProcessTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["list", "poll", "log", "write", "kill", "clear", "remove"],
					"description": "The operation to perform"
				},
				"session_id": {
					"type": "string",
					"description": "Session ID (required for poll, log, write, kill, remove)"
				},
				"input": {
					"type": "string",
					"description": "Input to write to process stdin (for write action)"
				},
				"offset": {
					"type": "integer",
					"description": "Line offset for log pagination (default 0)"
				},
				"limit": {
					"type": "integer",
					"description": "Max lines to return for log action (default 100)"
				}
			},
			"required": ["action"]
		}`),
	}
}

type processParams struct {
	Action    string `json:"action"`
	SessionID string `json:"session_id"`
	Input     string `json:"input"`
	Offset    int    `json:"offset"`
	Limit     int    `json:"limit"`
}

func (t *ProcessTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	return Execute(ctx, "tool.process", t.logger, params,
		Dispatch(func(p processParams) string { return p.Action }, ActionMap[processParams]{
			"list": func(_ context.Context, _ processParams) (any, error) {
				return t.handleList(), nil
			},
			"poll": func(_ context.Context, p processParams) (any, error) {
				return t.handlePoll(p)
			},
			"log": func(_ context.Context, p processParams) (any, error) {
				return t.handleLog(p)
			},
			"write": func(_ context.Context, p processParams) (any, error) {
				if err := t.handleWrite(p); err != nil {
					return nil, err
				}
				return map[string]bool{"ok": true}, nil
			},
			"kill": func(ctx context.Context, p processParams) (any, error) {
				if err := t.handleKill(ctx, p); err != nil {
					return nil, err
				}
				return map[string]bool{"killed": true}, nil
			},
			"clear": func(_ context.Context, _ processParams) (any, error) {
				return t.handleClear(), nil
			},
			"remove": func(ctx context.Context, p processParams) (any, error) {
				if err := t.handleRemove(ctx, p); err != nil {
					return nil, err
				}
				return map[string]bool{"removed": true}, nil
			},
		}),
	)
}

func (t *ProcessTool) handleList() any {
	entries := t.manager.List("")
	if entries == nil {
		entries = []domain.ProcessListEntry{}
	}
	return entries
}

func (t *ProcessTool) handlePoll(p processParams) (any, error) {
	if err := RequireField("session_id", p.SessionID); err != nil {
		return nil, err
	}
	return t.manager.Poll(p.SessionID)
}

func (t *ProcessTool) handleLog(p processParams) (any, error) {
	if err := RequireField("session_id", p.SessionID); err != nil {
		return nil, err
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	if p.Limit <= 0 {
		p.Limit = process.DefaultLogLimit
	}
	return t.manager.Log(p.SessionID, p.Offset, p.Limit)
}

func (t *ProcessTool) handleWrite(p processParams) error {
	if err := RequireFields("session_id", p.SessionID, "input", p.Input); err != nil {
		return err
	}
	return t.manager.Write(p.SessionID, p.Input)
}

func (t *ProcessTool) handleKill(ctx context.Context, p processParams) error {
	if err := RequireField("session_id", p.SessionID); err != nil {
		return err
	}
	return t.manager.Kill(ctx, p.SessionID)
}

func (t *ProcessTool) handleClear() any {
	removed := t.manager.Clear()
	return map[string]int{"cleared": removed}
}

func (t *ProcessTool) handleRemove(ctx context.Context, p processParams) error {
	if err := RequireField("session_id", p.SessionID); err != nil {
		return err
	}
	return t.manager.Remove(ctx, p.SessionID)
}
