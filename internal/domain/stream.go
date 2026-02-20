package domain

// StreamDeltaPayload is the payload for EventStreamDelta events.
// Published for each incremental chunk during a streaming LLM response.
type StreamDeltaPayload struct {
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Done      bool       `json:"done,omitempty"`
	Iteration int        `json:"iteration"`
}

// StreamCompletedPayload is the payload for EventStreamCompleted events.
// Published once when the full streaming response is available.
type StreamCompletedPayload struct {
	Content string `json:"content"`
	Usage   *Usage `json:"usage,omitempty"`
}

// StreamErrorPayload is the payload for EventStreamError events.
// Published when a streaming response fails mid-stream.
type StreamErrorPayload struct {
	Error string `json:"error"`
}
