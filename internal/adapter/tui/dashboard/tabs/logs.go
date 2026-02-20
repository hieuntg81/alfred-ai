package tabs

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"alfred-ai/internal/adapter/tui/components"
	"alfred-ai/internal/adapter/tui/theme"
)

const maxLogEntries = 500

// LogEntry represents a single log line.
type LogEntry struct {
	Time    time.Time
	Level   string // "debug", "info", "warn", "error"
	Message string
}

// LogsModel displays logs with level filtering via a filter bar.
type LogsModel struct {
	Viewport  viewport.Model
	FilterBar components.FilterBarModel
	entries   []LogEntry
	ready     bool
	atBottom  bool
	width     int
	height    int
}

// NewLogs creates a logs tab.
func NewLogs() LogsModel {
	return LogsModel{
		atBottom: true,
		FilterBar: components.NewFilterBar([]components.FilterOption{
			{ID: "debug", Label: "Debug", Shortcut: "d"},
			{ID: "info", Label: "Info", Shortcut: "i"},
			{ID: "warn", Label: "Warn", Shortcut: "w"},
			{ID: "error", Label: "Error", Shortcut: "e"},
		}),
	}
}

// SetSize sets dimensions. Reserve 1 line for the filter bar.
func (m *LogsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.FilterBar.SetWidth(w)
	viewH := h - 1
	if viewH < 3 {
		viewH = 3
	}
	if !m.ready {
		m.Viewport = viewport.New(w, viewH)
		m.Viewport.MouseWheelEnabled = true
		m.ready = true
	} else {
		m.Viewport.Width = w
		m.Viewport.Height = viewH
	}
	m.refreshContent()
}

// AddEntry adds a log entry.
func (m *LogsModel) AddEntry(entry LogEntry) {
	m.entries = append(m.entries, entry)
	if len(m.entries) > maxLogEntries {
		m.entries = m.entries[len(m.entries)-maxLogEntries:]
	}
	m.refreshContent()
	m.updateCounts()
	if m.atBottom {
		m.Viewport.GotoBottom()
	}
}

// Update handles viewport scrolling and filter shortcuts.
func (m LogsModel) Update(msg tea.Msg) (LogsModel, tea.Cmd) {
	if !m.ready {
		return m, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.Type == tea.KeyRunes {
			key := string(keyMsg.Runes)
			if m.FilterBar.HandleShortcut(key) {
				m.refreshContent()
				m.updateCounts()
				return m, nil
			}
		}
	}

	var cmd tea.Cmd
	m.Viewport, cmd = m.Viewport.Update(msg)
	m.atBottom = m.Viewport.AtBottom()
	return m, cmd
}

// View renders the filter bar + log viewport.
func (m LogsModel) View() string {
	if !m.ready {
		return ""
	}
	return m.FilterBar.View() + "\n" + m.Viewport.View()
}

func (m *LogsModel) updateCounts() {
	filtered := 0
	for _, e := range m.entries {
		if m.FilterBar.Active == "" || e.Level == m.FilterBar.Active {
			filtered++
		}
	}
	m.FilterBar.SetCounts(len(m.entries), filtered)
}

func (m *LogsModel) refreshContent() {
	if !m.ready {
		return
	}

	if len(m.entries) == 0 {
		m.Viewport.SetContent(theme.TextMuted.Render("  No log entries yet"))
		return
	}

	filter := m.FilterBar.Active

	var sb strings.Builder
	for _, e := range m.entries {
		if filter != "" && e.Level != filter {
			continue
		}

		ts := e.Time.Format("15:04:05")

		var levelStr string
		switch e.Level {
		case "error":
			levelStr = theme.TextError.Render("ERROR")
		case "warn":
			levelStr = theme.TextWarning.Render("WARN ")
		case "info":
			levelStr = theme.TextInfo.Render("INFO ")
		default:
			levelStr = theme.TextMuted.Render("DEBUG")
		}

		sb.WriteString(fmt.Sprintf("  %s  %s  %s\n",
			theme.Dim.Render(ts), levelStr, e.Message))
	}

	m.Viewport.SetContent(sb.String())
}
