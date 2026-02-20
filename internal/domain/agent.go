package domain

import "context"

// AgentIdentity describes a named agent instance in a multi-agent setup.
type AgentIdentity struct {
	ID          string            `json:"id"           yaml:"id"`
	Name        string            `json:"name"         yaml:"name"`
	Description string            `json:"description"  yaml:"description"`
	SystemPrompt string           `json:"system_prompt" yaml:"system_prompt"`
	Model       string            `json:"model"        yaml:"model"`
	Provider    string            `json:"provider"     yaml:"provider"`
	Tools       []string          `json:"tools,omitempty"  yaml:"tools,omitempty"`
	Skills      []string          `json:"skills,omitempty" yaml:"skills,omitempty"`
	MaxIter     int               `json:"max_iter,omitempty" yaml:"max_iter,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// AgentRouter decides which agent should handle an inbound message.
type AgentRouter interface {
	Route(ctx context.Context, msg InboundMessage) (agentID string, err error)
}

// AgentStatus is a read-only snapshot of a running agent instance.
type AgentStatus struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	ActiveSessions int    `json:"active_sessions"`
}
