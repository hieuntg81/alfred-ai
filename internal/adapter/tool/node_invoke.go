package tool

import (
	"context"
	"encoding/json"

	"alfred-ai/internal/domain"
)

// NodeInvokeTool invokes a capability on a remote node.
type NodeInvokeTool struct {
	manager domain.NodeManager
}

// NewNodeInvokeTool creates a new NodeInvokeTool.
func NewNodeInvokeTool(manager domain.NodeManager) *NodeInvokeTool {
	return &NodeInvokeTool{manager: manager}
}

func (t *NodeInvokeTool) Name() string        { return "node_invoke" }
func (t *NodeInvokeTool) Description() string { return "Invoke a capability on a remote node" }

func (t *NodeInvokeTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"node_id": {"type": "string", "description": "The node to invoke the capability on"},
				"capability": {"type": "string", "description": "The capability to invoke"},
				"params": {"type": "object", "description": "Parameters to pass to the capability"}
			},
			"required": ["node_id", "capability"]
		}`),
	}
}

type nodeInvokeParams struct {
	NodeID     string          `json:"node_id"`
	Capability string          `json:"capability"`
	Params     json.RawMessage `json:"params,omitempty"`
}

func (t *NodeInvokeTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	p, errResult := ParseParams[nodeInvokeParams](params)
	if errResult != nil {
		return errResult, nil
	}
	if p.NodeID == "" || p.Capability == "" {
		return &domain.ToolResult{IsError: true, Content: "node_id and capability are required"}, nil
	}

	result, err := t.manager.Invoke(ctx, p.NodeID, p.Capability, p.Params)
	if err != nil {
		return &domain.ToolResult{IsError: true, Content: err.Error()}, nil
	}

	return &domain.ToolResult{Content: string(result)}, nil
}
