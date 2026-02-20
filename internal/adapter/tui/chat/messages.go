// Package chat implements a Bubble Tea TUI chat channel for alfred-ai.
package chat

import "alfred-ai/internal/domain"

// OutboundMsg wraps a domain.OutboundMessage injected from Channel.Send().
// Gen identifies the request generation so stale responses can be discarded.
type OutboundMsg struct {
	Message domain.OutboundMessage
	Gen     uint64
}

// HandlerDoneMsg signals that the message handler goroutine finished.
// Gen identifies the request generation so stale completions can be discarded.
type HandlerDoneMsg struct {
	Err error
	Gen uint64
}

// QuitMsg signals the program to exit.
type QuitMsg struct{}

// StreamTickMsg drives simulated streaming (progressive rendering).
type StreamTickMsg struct{}

// ToolStartedMsg signals a tool execution has started.
type ToolStartedMsg struct {
	Name string
}

// ToolCompletedMsg signals a tool execution has completed.
type ToolCompletedMsg struct {
	Name    string
	Result  string
	IsError bool
}

// ToolExpandMsg requests opening the full tool output in a modal.
type ToolExpandMsg struct {
	Name   string
	Result string
}
