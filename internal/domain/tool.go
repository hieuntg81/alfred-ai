package domain

import (
	"context"
	"encoding/json"
)

// ToolSchema describes a tool for the LLM function-calling protocol.
type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ToolCall represents an LLM's request to invoke a tool.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult is the outcome of executing a tool.
type ToolResult struct {
	ToolCallID  string `json:"tool_call_id"`
	Content     string `json:"content"`
	IsError     bool   `json:"is_error"`
	IsRetryable bool   `json:"is_retryable,omitempty"`
}

// Tool is the interface every tool must implement.
type Tool interface {
	Name() string
	Description() string
	Schema() ToolSchema
	Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error)
}

// ToolExecutor abstracts tool lookup and execution.
type ToolExecutor interface {
	Get(name string) (Tool, error)
	Schemas() []ToolSchema
}

// ToolApprover decides whether a tool call requires human approval.
type ToolApprover interface {
	// NeedsApproval returns true if the tool call should be gated on approval.
	NeedsApproval(call ToolCall) bool
	// RequestApproval blocks until the call is approved or denied.
	RequestApproval(ctx context.Context, call ToolCall) (bool, error)
}
