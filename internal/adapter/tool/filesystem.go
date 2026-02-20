package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/security"
)

// FilesystemTool provides sandboxed file read/write/list operations.
type FilesystemTool struct {
	backend FilesystemBackend
	sandbox *security.Sandbox
	logger  *slog.Logger
}

// NewFilesystemTool creates a sandboxed filesystem tool backed by the given FilesystemBackend.
func NewFilesystemTool(backend FilesystemBackend, sandbox *security.Sandbox, logger *slog.Logger) *FilesystemTool {
	return &FilesystemTool{backend: backend, sandbox: sandbox, logger: logger}
}

func (t *FilesystemTool) Name() string { return "filesystem" }
func (t *FilesystemTool) Description() string {
	return "Read, write, and list files within the workspace"
}

func (t *FilesystemTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["read", "write", "list"], "description": "The file operation to perform"},
				"path": {"type": "string", "description": "File or directory path"},
				"content": {"type": "string", "description": "Content to write (only for write action)"}
			},
			"required": ["action"]
		}`),
	}
}

type filesystemParams struct {
	Action  string `json:"action"`
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
}

func (t *FilesystemTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	return Execute(ctx, "tool.filesystem", t.logger, params,
		Dispatch(func(p filesystemParams) string { return p.Action }, ActionMap[filesystemParams]{
			"read":  t.readFile,
			"write": t.writeFile,
			"list":  t.listDir,
		}),
	)
}

func (t *FilesystemTool) resolvePath(path string) (string, error) {
	if path == "" || path == "." {
		return t.sandbox.Root(), nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(t.sandbox.Root(), path)
	}
	return t.sandbox.ValidatePath(path)
}

func (t *FilesystemTool) readFile(_ context.Context, p filesystemParams) (any, error) {
	resolved, err := t.resolvePath(p.Path)
	if err != nil {
		return nil, err
	}

	data, err := t.backend.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	t.logger.Debug("filesystem read", "path", resolved, "size", len(data))
	return TextResult(string(data)), nil
}

func (t *FilesystemTool) writeFile(_ context.Context, p filesystemParams) (any, error) {
	resolved, err := t.resolvePath(p.Path)
	if err != nil {
		return nil, err
	}

	if err := t.backend.WriteFile(resolved, []byte(p.Content), 0644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	t.logger.Debug("filesystem write", "path", resolved, "size", len(p.Content))
	return TextResult(fmt.Sprintf("wrote %d bytes to %s", len(p.Content), p.Path)), nil
}

func (t *FilesystemTool) listDir(_ context.Context, p filesystemParams) (any, error) {
	resolved, err := t.resolvePath(p.Path)
	if err != nil {
		return nil, err
	}

	entries, err := t.backend.ReadDir(resolved)
	if err != nil {
		return nil, fmt.Errorf("list dir: %w", err)
	}

	var sb strings.Builder
	for _, entry := range entries {
		if entry.IsDir() {
			fmt.Fprintf(&sb, "%s/\n", entry.Name())
		} else {
			fmt.Fprintf(&sb, "%s\n", entry.Name())
		}
	}

	return TextResult(sb.String()), nil
}
