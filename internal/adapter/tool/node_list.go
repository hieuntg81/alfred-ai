package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"alfred-ai/internal/domain"
)

// NodeListTool lists all registered remote nodes.
type NodeListTool struct {
	manager domain.NodeManager
}

// NewNodeListTool creates a new NodeListTool.
func NewNodeListTool(manager domain.NodeManager) *NodeListTool {
	return &NodeListTool{manager: manager}
}

func (t *NodeListTool) Name() string        { return "node_list" }
func (t *NodeListTool) Description() string { return "List all registered remote nodes" }

func (t *NodeListTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters:  json.RawMessage(`{"type": "object", "properties": {}}`),
	}
}

func (t *NodeListTool) Execute(ctx context.Context, _ json.RawMessage) (*domain.ToolResult, error) {
	nodes, err := t.manager.List(ctx)
	if err != nil {
		return &domain.ToolResult{IsError: true, Content: fmt.Sprintf("list nodes: %v", err)}, nil
	}

	data, err := json.MarshalIndent(nodes, "", "  ")
	if err != nil {
		return &domain.ToolResult{IsError: true, Content: fmt.Sprintf("marshal: %v", err)}, nil
	}

	return &domain.ToolResult{Content: string(data)}, nil
}
