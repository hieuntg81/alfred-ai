package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"alfred-ai/internal/adapter/tui/theme"
	"alfred-ai/internal/domain"
)

const maxEventEntries = 500

// EventStreamModel displays a scrollable stream of domain events with smart auto-scroll.
type EventStreamModel struct {
	Viewport viewport.Model
	events   []domain.Event
	filter   domain.EventType // empty = show all
	ready    bool
	atBottom bool
	width    int
	height   int
}

// NewEventStream creates an event stream viewer.
func NewEventStream() EventStreamModel {
	return EventStreamModel{atBottom: true}
}

// SetSize sets the viewport dimensions.
func (m *EventStreamModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	if !m.ready {
		m.Viewport = viewport.New(w, h)
		m.Viewport.MouseWheelEnabled = true
		m.Viewport.MouseWheelDelta = 3
		m.ready = true
	} else {
		m.Viewport.Width = w
		m.Viewport.Height = h
	}
	m.refreshContent()
}

// SetFilter sets the event type filter. Empty string shows all events.
func (m *EventStreamModel) SetFilter(t domain.EventType) {
	m.filter = t
	m.refreshContent()
}

// AddEvent appends an event and auto-scrolls if at bottom.
func (m *EventStreamModel) AddEvent(event domain.Event) {
	m.events = append(m.events, event)
	// Ring buffer: drop oldest when exceeding max.
	if len(m.events) > maxEventEntries {
		m.events = m.events[len(m.events)-maxEventEntries:]
	}
	m.refreshContent()
	if m.atBottom {
		m.Viewport.GotoBottom()
	}
}

// Update handles viewport scrolling.
func (m EventStreamModel) Update(msg tea.Msg) (EventStreamModel, tea.Cmd) {
	if !m.ready {
		return m, nil
	}
	var cmd tea.Cmd
	m.Viewport, cmd = m.Viewport.Update(msg)
	m.atBottom = m.Viewport.AtBottom()
	return m, cmd
}

// EventCount returns the total number of events.
func (m EventStreamModel) EventCount() int {
	return len(m.events)
}

// FilteredCount returns the number of events matching the current filter.
func (m EventStreamModel) FilteredCount() int {
	if m.filter == "" {
		return len(m.events)
	}
	count := 0
	for _, evt := range m.events {
		if evt.Type == m.filter || strings.HasPrefix(string(evt.Type), string(m.filter)) {
			count++
		}
	}
	return count
}

// View renders the event stream.
func (m EventStreamModel) View() string {
	if !m.ready {
		return ""
	}
	return m.Viewport.View()
}

func (m *EventStreamModel) refreshContent() {
	if !m.ready {
		return
	}

	if len(m.events) == 0 {
		m.Viewport.SetContent(theme.TextMuted.Render("  Waiting for events..."))
		return
	}

	var sb strings.Builder
	for _, evt := range m.events {
		if m.filter != "" && !strings.HasPrefix(string(evt.Type), string(m.filter)) {
			continue
		}

		ts := evt.Timestamp.Format("15:04:05")
		eventType := string(evt.Type)
		session := ""
		if evt.SessionID != "" {
			session = fmt.Sprintf(" session=%s", evt.SessionID)
		}

		// Color code by event category.
		var typeStyled string
		paddedType := fmt.Sprintf("%-25s", eventType)
		switch {
		case strings.HasPrefix(eventType, "tool."):
			typeStyled = theme.TextWarning.Render(paddedType)
		case strings.HasPrefix(eventType, "llm."):
			typeStyled = theme.TextInfo.Render(paddedType)
		case strings.HasPrefix(eventType, "memory."):
			typeStyled = theme.TextAccent.Render(paddedType)
		case strings.HasPrefix(eventType, "agent.error"):
			typeStyled = theme.TextError.Render(paddedType)
		default:
			typeStyled = theme.TextMuted.Render(paddedType)
		}

		line := fmt.Sprintf("  %s  %s%s",
			theme.Dim.Render(ts),
			typeStyled,
			theme.TextMuted.Render(session),
		)

		sb.WriteString(line + "\n")
	}

	m.Viewport.SetContent(sb.String())
}
