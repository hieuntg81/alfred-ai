package tabs

import (
	tea "github.com/charmbracelet/bubbletea"

	"alfred-ai/internal/adapter/tui/components"
	"alfred-ai/internal/domain"
)

// EventsModel wraps the event stream component with a filter bar.
type EventsModel struct {
	Stream    components.EventStreamModel
	FilterBar components.FilterBarModel
	width     int
	height    int
}

// NewEvents creates an events tab.
func NewEvents() EventsModel {
	return EventsModel{
		Stream: components.NewEventStream(),
		FilterBar: components.NewFilterBar([]components.FilterOption{
			{ID: "tool.", Label: "Tool", Shortcut: "t"},
			{ID: "llm.", Label: "LLM", Shortcut: "l"},
			{ID: "memory.", Label: "Memory", Shortcut: "m"},
			{ID: "agent.error", Label: "Error", Shortcut: "e"},
		}),
	}
}

// SetSize sets dimensions. Reserve 1 line for the filter bar.
func (m *EventsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.FilterBar.SetWidth(w)
	m.Stream.SetSize(w, h-1)
}

// AddEvent appends an event and updates filter counts.
func (m *EventsModel) AddEvent(event domain.Event) {
	m.Stream.AddEvent(event)
	m.FilterBar.SetCounts(m.Stream.EventCount(), m.Stream.FilteredCount())
}

// Update handles viewport scrolling and filter shortcuts.
func (m EventsModel) Update(msg tea.Msg) (EventsModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.Type == tea.KeyRunes {
			key := string(keyMsg.Runes)
			if m.FilterBar.HandleShortcut(key) {
				m.Stream.SetFilter(domain.EventType(m.FilterBar.Active))
				m.FilterBar.SetCounts(m.Stream.EventCount(), m.Stream.FilteredCount())
				return m, nil
			}
		}
	}

	var cmd tea.Cmd
	m.Stream, cmd = m.Stream.Update(msg)
	return m, cmd
}

// View renders the filter bar + event stream.
func (m EventsModel) View() string {
	return m.FilterBar.View() + "\n" + m.Stream.View()
}
