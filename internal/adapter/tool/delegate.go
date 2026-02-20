package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/usecase/multiagent"
)

// delegateSeq is a global counter for generating unique session IDs.
var delegateSeq atomic.Int64

// DelegateTool allows an agent to delegate work to another agent.
type DelegateTool struct {
	broker   *multiagent.Broker
	registry *multiagent.Registry
	agentID  string // the agent that owns this tool instance
}

// NewDelegateTool creates a delegate tool for the given agent.
func NewDelegateTool(broker *multiagent.Broker, registry *multiagent.Registry, agentID string) *DelegateTool {
	return &DelegateTool{
		broker:   broker,
		registry: registry,
		agentID:  agentID,
	}
}

func (t *DelegateTool) Name() string        { return "delegate" }
func (t *DelegateTool) Description() string { return "Delegate a task to another agent" }

func (t *DelegateTool) Schema() domain.ToolSchema {
	// Build available agents list for the description.
	agents := t.registry.List()
	var names []string
	for _, a := range agents {
		if a.ID != t.agentID {
			names = append(names, fmt.Sprintf("%s (%s)", a.ID, a.Name))
		}
	}
	agentList := "none"
	if len(names) > 0 {
		agentList = strings.Join(names, ", ")
	}

	return domain.ToolSchema{
		Name:        t.Name(),
		Description: fmt.Sprintf("Delegate a task to another agent. Available agents: %s", agentList),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"agent_id": {
					"type": "string",
					"description": "The ID of the agent to delegate to"
				},
				"message": {
					"type": "string",
					"description": "The message or task to delegate"
				},
				"session_id": {
					"type": "string",
					"description": "Optional session ID for context continuity"
				}
			},
			"required": ["agent_id", "message"]
		}`),
	}
}

type delegateParams struct {
	AgentID   string `json:"agent_id"`
	Message   string `json:"message"`
	SessionID string `json:"session_id"`
}

func (t *DelegateTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	p, errResult := ParseParams[delegateParams](params)
	if errResult != nil {
		return errResult, nil
	}

	if err := RequireFields("agent_id", p.AgentID, "message", p.Message); err != nil {
		return &domain.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	if p.SessionID == "" {
		p.SessionID = fmt.Sprintf("auto_%d_%d", time.Now().UnixMilli(), delegateSeq.Add(1))
	}

	resp, err := t.broker.Delegate(ctx, multiagent.DelegateRequest{
		FromAgent: t.agentID,
		ToAgent:   p.AgentID,
		SessionID: p.SessionID,
		Message:   p.Message,
	})
	if err != nil {
		return &domain.ToolResult{
			Content: fmt.Sprintf("delegation failed: %s", err.Error()),
			IsError: true,
		}, nil
	}

	return &domain.ToolResult{
		Content: resp.Content,
	}, nil
}
