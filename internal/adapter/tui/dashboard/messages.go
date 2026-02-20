// Package dashboard implements a Bubble Tea TUI monitoring dashboard for alfred-ai.
package dashboard

import "alfred-ai/internal/domain"

// EventBusMsg wraps a domain.Event from the EventBus subscription.
type EventBusMsg struct {
	Event domain.Event
}

// MemoryQueryResultMsg carries the result of a memory query.
type MemoryQueryResultMsg struct {
	Entries []domain.MemoryEntry
	Err     error
}
