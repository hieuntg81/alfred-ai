package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"

	"go.opentelemetry.io/otel/trace"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/security"
	"alfred-ai/internal/usecase/process"
)

// ShellTool executes shell commands from an allowlist.
type ShellTool struct {
	backend         ShellBackend
	processManager  *process.Manager // nil = no background support
	allowedCommands map[string]bool
	sandbox         *security.Sandbox
	logger          *slog.Logger
}

// ShellToolOption configures optional ShellTool features.
type ShellToolOption func(*ShellTool)

// WithProcessManager enables background execution via the ProcessManager.
func WithProcessManager(pm *process.Manager) ShellToolOption {
	return func(t *ShellTool) {
		t.processManager = pm
	}
}

// NewShellTool creates a shell tool with an allowlist of commands, backed by the given ShellBackend.
func NewShellTool(backend ShellBackend, allowed []string, sandbox *security.Sandbox, logger *slog.Logger, opts ...ShellToolOption) *ShellTool {
	m := make(map[string]bool, len(allowed))
	for _, cmd := range allowed {
		m[cmd] = true
	}
	t := &ShellTool{
		backend:         backend,
		allowedCommands: m,
		sandbox:         sandbox,
		logger:          logger,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (t *ShellTool) Name() string { return "shell" }
func (t *ShellTool) Description() string {
	return "Execute allowed shell commands within the workspace"
}

func (t *ShellTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"command": {"type": "string", "description": "The command to execute"},
				"args": {"type": "array", "items": {"type": "string"}, "description": "Command arguments"},
				"workdir": {"type": "string", "description": "Working directory (optional, defaults to sandbox root)"},
				"background": {"type": "boolean", "description": "Run in background and return session_id for use with the process tool"}
			},
			"required": ["command"]
		}`),
	}
}

type shellParams struct {
	Command    string   `json:"command"`
	Args       []string `json:"args"`
	WorkDir    string   `json:"workdir,omitempty"`
	Background bool     `json:"background,omitempty"`
}

func (t *ShellTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	return Execute(ctx, "tool.shell", t.logger, params,
		func(ctx context.Context, span trace.Span, p shellParams) (any, error) {
			if err := t.validateCommand(p.Command); err != nil {
				return nil, err
			}

			workDir := t.sandbox.Root()
			if p.WorkDir != "" {
				resolved, err := t.sandbox.ValidatePath(p.WorkDir)
				if err != nil {
					return nil, err
				}
				workDir = resolved
			}

			// Background execution
			if p.Background {
				if t.processManager == nil {
					return nil, fmt.Errorf("background execution is not enabled (process tool disabled)")
				}

				session, err := t.processManager.Start(ctx, p.Command, p.Args, workDir, "")
				if err != nil {
					return nil, err
				}

				t.logger.Debug("shell command backgrounded", "command", p.Command, "session_id", session.ID)
				return map[string]string{
					"status":     "running",
					"session_id": session.ID,
				}, nil
			}

			// Synchronous execution
			stdout, stderr, err := t.backend.Execute(ctx, p.Command, p.Args, workDir)

			output := stdout
			if stderr != "" {
				output += "\nSTDERR:\n" + stderr
			}

			if err != nil {
				t.logger.Debug("shell command failed", "command", p.Command, "error", err)
				return nil, fmt.Errorf("command failed: %v\n%s", err, output)
			}

			t.logger.Debug("shell command completed", "command", p.Command)
			return output, nil
		},
	)
}

// validateCommand checks the base command name is in the allowlist.
func (t *ShellTool) validateCommand(command string) error {
	base := filepath.Base(command)
	if !t.allowedCommands[base] {
		return domain.NewDomainError("ShellTool.validateCommand", domain.ErrCommandNotAllowed,
			fmt.Sprintf("command %q (base: %q) not in allowlist", command, base))
	}
	return nil
}
