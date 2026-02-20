package domain

import "time"

// Role constants for message roles.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message represents a single message in a conversation.
type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	Name      string     `json:"name,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Thinking  string     `json:"thinking,omitempty"`
	Timestamp time.Time  `json:"timestamp"`
}

// ChatRequest is sent to an LLM provider.
type ChatRequest struct {
	Model          string       `json:"model"`
	Messages       []Message    `json:"messages"`
	Tools          []ToolSchema `json:"tools,omitempty"`
	MaxTokens      int          `json:"max_tokens,omitempty"`
	Temperature    float64      `json:"temperature,omitempty"`
	Stream         bool         `json:"stream,omitempty"`
	ThinkingBudget int          `json:"thinking_budget,omitempty"`
}

// ChatResponse is returned from an LLM provider.
type ChatResponse struct {
	ID        string    `json:"id"`
	Model     string    `json:"model"`
	Message   Message   `json:"message"`
	Usage     Usage     `json:"usage"`
	CreatedAt time.Time `json:"created_at"`
}

// Usage tracks token consumption.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Conversation holds an ordered sequence of messages.
type Conversation struct {
	ID        string    `json:"id"`
	Messages  []Message `json:"messages"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
