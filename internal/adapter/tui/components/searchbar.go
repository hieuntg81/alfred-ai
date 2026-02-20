package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"alfred-ai/internal/adapter/tui/theme"
)

// SearchMode tracks the current state of inline search.
type SearchMode int

const (
	SearchInactive SearchMode = iota
	SearchInput               // user is typing the search query
	SearchActive              // navigating between matches
)

// SearchBarModel provides inline search with match navigation.
type SearchBarModel struct {
	Mode       SearchMode
	Query      string
	Input      textinput.Model
	Matches    []int // line indices containing the match
	CurrentIdx int   // index into Matches
	width      int
}

// NewSearchBar creates a search bar.
func NewSearchBar() SearchBarModel {
	ti := textinput.New()
	ti.Placeholder = "Search..."
	ti.Prompt = "/ "
	ti.Width = 30
	ti.PromptStyle = theme.TextInfo
	ti.PlaceholderStyle = theme.Dim
	return SearchBarModel{
		Input: ti,
	}
}

// SetWidth updates the search bar width.
func (m *SearchBarModel) SetWidth(w int) {
	m.width = w
	m.Input.Width = w - 20
}

// Activate opens the search input.
func (m *SearchBarModel) Activate() {
	m.Mode = SearchInput
	m.Input.SetValue("")
	m.Input.Focus()
	m.Query = ""
	m.Matches = nil
	m.CurrentIdx = 0
}

// Deactivate closes search and clears matches.
func (m *SearchBarModel) Deactivate() {
	m.Mode = SearchInactive
	m.Input.Blur()
	m.Query = ""
	m.Matches = nil
	m.CurrentIdx = 0
}

// Search runs the query against the given lines and stores match indices.
func (m *SearchBarModel) Search(lines []string) {
	m.Matches = nil
	m.CurrentIdx = 0
	if m.Query == "" {
		return
	}
	q := strings.ToLower(m.Query)
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), q) {
			m.Matches = append(m.Matches, i)
		}
	}
}

// NextMatch returns the line index of the current match, then advances.
func (m *SearchBarModel) NextMatch() int {
	if len(m.Matches) == 0 {
		return -1
	}
	idx := m.Matches[m.CurrentIdx]
	m.CurrentIdx = (m.CurrentIdx + 1) % len(m.Matches)
	return idx
}

// PrevMatch returns the line index of the current match, then moves back.
func (m *SearchBarModel) PrevMatch() int {
	if len(m.Matches) == 0 {
		return -1
	}
	idx := m.Matches[m.CurrentIdx]
	m.CurrentIdx--
	if m.CurrentIdx < 0 {
		m.CurrentIdx = len(m.Matches) - 1
	}
	return idx
}

// Update handles input for the search bar.
func (m SearchBarModel) Update(msg tea.Msg) (SearchBarModel, tea.Cmd) {
	if m.Mode == SearchInactive {
		return m, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.Type {
		case tea.KeyEsc:
			m.Deactivate()
			return m, nil
		case tea.KeyEnter:
			if m.Mode == SearchInput {
				m.Query = m.Input.Value()
				m.Mode = SearchActive
				m.Input.Blur()
			}
			return m, nil
		}
	}

	if m.Mode == SearchInput {
		var cmd tea.Cmd
		m.Input, cmd = m.Input.Update(msg)
		return m, cmd
	}

	return m, nil
}

// View renders the search bar.
func (m SearchBarModel) View() string {
	if m.Mode == SearchInactive {
		return ""
	}

	if m.Mode == SearchInput {
		return "  " + m.Input.View()
	}

	// SearchActive: show query + match count + navigation hints.
	matchInfo := "no matches"
	if len(m.Matches) > 0 {
		matchInfo = fmt.Sprintf("%d/%d", m.CurrentIdx+1, len(m.Matches))
	}
	return fmt.Sprintf("  /%s  %s  %s",
		theme.Bold.Render(m.Query),
		theme.TextMuted.Render("["+matchInfo+"]"),
		theme.Dim.Render("n:next N:prev Esc:close"),
	)
}
