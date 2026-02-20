package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"alfred-ai/internal/adapter/tui/theme"
	"alfred-ai/internal/domain"
)

// MemoryQueryMsg triggers a memory query.
type MemoryQueryMsg struct {
	Query string
}

// MemoryViewerModel provides a query input + results table for memory browsing.
type MemoryViewerModel struct {
	QueryInput textinput.Model
	Table      table.Model
	entries    []domain.MemoryEntry
	selected   *domain.MemoryEntry
	err        string
	ready      bool
	width      int
	height     int
}

// NewMemoryViewer creates a memory viewer.
func NewMemoryViewer() MemoryViewerModel {
	qi := textinput.New()
	qi.Placeholder = "Search memory..."
	qi.Focus()
	qi.Width = 40
	qi.PromptStyle = theme.InputPrompt
	qi.PlaceholderStyle = theme.InputPlaceholder

	return MemoryViewerModel{
		QueryInput: qi,
	}
}

// SetSize sets the available dimensions.
func (m *MemoryViewerModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.QueryInput.Width = w - 20
	m.ready = true
	m.rebuildTable()
}

// SetResults updates the displayed entries.
func (m *MemoryViewerModel) SetResults(entries []domain.MemoryEntry) {
	m.entries = entries
	m.err = ""
	m.selected = nil
	m.rebuildTable()
}

// SetError shows an error message.
func (m *MemoryViewerModel) SetError(errMsg string) {
	m.err = errMsg
}

// Update handles input and table navigation.
func (m MemoryViewerModel) Update(msg tea.Msg) (MemoryViewerModel, tea.Cmd) {
	if !m.ready {
		return m, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.Type {
		case tea.KeyEnter:
			// If query input has focus, fire a query.
			if m.QueryInput.Focused() {
				query := strings.TrimSpace(m.QueryInput.Value())
				if query != "" {
					return m, func() tea.Msg {
						return MemoryQueryMsg{Query: query}
					}
				}
			}
		case tea.KeyTab:
			// Switch focus between query input and table.
			if m.QueryInput.Focused() {
				m.QueryInput.Blur()
				m.Table.Focus()
			} else {
				m.Table.Blur()
				m.QueryInput.Focus()
			}
			return m, nil
		}
	}

	var cmds []tea.Cmd

	if m.QueryInput.Focused() {
		var cmd tea.Cmd
		m.QueryInput, cmd = m.QueryInput.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		var cmd tea.Cmd
		m.Table, cmd = m.Table.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// View renders the memory viewer.
func (m MemoryViewerModel) View() string {
	if !m.ready {
		return ""
	}

	// Query input line.
	queryLine := "  " + m.QueryInput.View()

	// Results section.
	var resultsView string
	if m.err != "" {
		resultsView = theme.TextError.Render("  " + theme.SymbolError + " " + m.err)
	} else if len(m.entries) == 0 {
		resultsView = theme.TextMuted.Render("  No results. Enter a query to search memory.")
	} else {
		header := theme.TextMuted.Render(fmt.Sprintf("  Results: %d entries", len(m.entries)))
		tableView := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(theme.ColorBorder).
			Render(m.Table.View())
		resultsView = header + "\n" + tableView
	}

	// Detail view.
	var detailView string
	if m.selected != nil {
		detailView = "\n" + theme.Bold.Render("  Detail:") + "\n" +
			"  " + m.selected.Content
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		queryLine,
		"",
		resultsView,
		detailView,
	)
}

func (m *MemoryViewerModel) rebuildTable() {
	if !m.ready {
		return
	}

	idW := 10
	contentW := m.width - idW - 20 - 10
	if contentW < 20 {
		contentW = 20
	}

	columns := []table.Column{
		{Title: "ID", Width: idW},
		{Title: "Content", Width: contentW},
		{Title: "Tags", Width: 15},
		{Title: "Age", Width: 8},
	}

	var rows []table.Row
	for _, e := range m.entries {
		content := e.Content
		if len(content) > contentW-3 {
			content = content[:contentW-3] + theme.SymbolEllipsis
		}
		tags := strings.Join(e.Tags, ", ")
		age := RelativeTime(e.CreatedAt)
		rows = append(rows, table.Row{e.ID, content, tags, age})
	}

	tableH := m.height - 8
	if tableH < 5 {
		tableH = 5
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(false),
		table.WithHeight(tableH),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(theme.ColorBorder).
		BorderBottom(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57"))
	t.SetStyles(s)

	m.Table = t
}
