package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/usecase"
)

// SubAgentTool wraps a SubAgentManager as a domain.Tool.
type SubAgentTool struct {
	manager *usecase.SubAgentManager
}

// NewSubAgentTool creates a tool that spawns sub-agents.
func NewSubAgentTool(manager *usecase.SubAgentManager) *SubAgentTool {
	return &SubAgentTool{manager: manager}
}

// Name implements domain.Tool.
func (t *SubAgentTool) Name() string { return "sub_agent" }

// Description implements domain.Tool.
func (t *SubAgentTool) Description() string {
	return "Spawn sub-agents to execute tasks in parallel. Each task runs in an independent conversation."
}

// Schema implements domain.Tool.
func (t *SubAgentTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"tasks": {
					"type": "array",
					"items": {"type": "string"},
					"description": "List of tasks to execute in parallel"
				}
			},
			"required": ["tasks"]
		}`),
	}
}

// Execute implements domain.Tool.
func (t *SubAgentTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	var p struct {
		Tasks []string `json:"tasks"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return &domain.ToolResult{
			Content: "invalid parameters: " + err.Error(),
			IsError: true,
		}, nil
	}

	if len(p.Tasks) == 0 {
		return &domain.ToolResult{
			Content: "no tasks provided",
			IsError: true,
		}, nil
	}

	// Security: Limit task count to prevent DoS via array overflow
	if len(p.Tasks) > 1000 {
		return &domain.ToolResult{
			Content: fmt.Sprintf("too many tasks: %d (max 1000)", len(p.Tasks)),
			IsError: true,
		}, nil
	}

	// Security: Limit individual task size to prevent memory exhaustion
	const maxTaskSize = 10 * 1024 * 1024 // 10MB
	for i, task := range p.Tasks {
		if len(task) > maxTaskSize {
			return &domain.ToolResult{
				Content: fmt.Sprintf("task %d too large: %d bytes (max %d)", i+1, len(task), maxTaskSize),
				IsError: true,
			}, nil
		}
	}

	results, err := t.manager.SpawnParallel(ctx, p.Tasks)

	var sb strings.Builder
	for i, result := range results {
		fmt.Fprintf(&sb, "## Task %d\n%s\n\n", i+1, result)
	}

	content := sb.String()
	if err != nil {
		content += "\nWarning: some tasks failed: " + err.Error()
	}

	return &domain.ToolResult{
		Content: content,
	}, nil
}
