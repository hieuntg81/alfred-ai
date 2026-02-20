package domain

import "context"

// LLMProvider is the interface for any LLM backend.
type LLMProvider interface {
	// Chat sends a request and returns a complete response.
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	// Name returns the provider's identifier (e.g., "openai", "groq").
	Name() string
}

// StreamDelta is a single incremental chunk from a streaming LLM response.
type StreamDelta struct {
	Content   string     `json:"content,omitempty"`
	Thinking  string     `json:"thinking,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Done      bool       `json:"done,omitempty"`
	Usage     *Usage     `json:"usage,omitempty"`
}

// StreamingLLMProvider extends LLMProvider with streaming support.
type StreamingLLMProvider interface {
	LLMProvider
	// ChatStream sends a request and returns a channel of incremental deltas.
	ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamDelta, error)
}
