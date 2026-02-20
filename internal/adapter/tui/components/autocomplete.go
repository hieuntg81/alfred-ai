package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"alfred-ai/internal/adapter/tui/theme"
)

// CommandDef defines a slash command for autocomplete.
type CommandDef struct {
	Name        string // e.g. "/help"
	Description string // e.g. "Show available commands"
}

// AutocompleteModel manages a filtered popup of slash commands.
type AutocompleteModel struct {
	Commands []CommandDef
	Filtered []CommandDef
	Selected int
	Visible  bool
	prefix   string
	maxShow  int
	width    int
}

// NewAutocomplete creates an autocomplete model with the given commands.
func NewAutocomplete(commands []CommandDef) AutocompleteModel {
	return AutocompleteModel{
		Commands: commands,
		maxShow:  7,
	}
}

// SetWidth updates the popup width.
func (m *AutocompleteModel) SetWidth(w int) {
	m.width = w
}

// SetPrefix updates the prefix filter and refreshes the filtered list.
func (m *AutocompleteModel) SetPrefix(prefix string) {
	m.prefix = strings.ToLower(prefix)
	m.Filtered = nil
	for _, cmd := range m.Commands {
		if strings.HasPrefix(strings.ToLower(cmd.Name), m.prefix) {
			m.Filtered = append(m.Filtered, cmd)
		}
	}
	m.Visible = len(m.Filtered) > 0 && m.prefix != ""
	if m.Selected >= len(m.Filtered) {
		m.Selected = 0
	}
}

// Hide hides the popup.
func (m *AutocompleteModel) Hide() {
	m.Visible = false
	m.Filtered = nil
	m.prefix = ""
	m.Selected = 0
}

// SelectNext moves selection down.
func (m *AutocompleteModel) SelectNext() {
	if len(m.Filtered) == 0 {
		return
	}
	m.Selected = (m.Selected + 1) % len(m.Filtered)
}

// SelectPrev moves selection up.
func (m *AutocompleteModel) SelectPrev() {
	if len(m.Filtered) == 0 {
		return
	}
	m.Selected--
	if m.Selected < 0 {
		m.Selected = len(m.Filtered) - 1
	}
}

// Accept returns the selected command name and hides the popup.
func (m *AutocompleteModel) Accept() string {
	if len(m.Filtered) == 0 {
		return ""
	}
	name := m.Filtered[m.Selected].Name
	m.Hide()
	return name
}

// Height returns how many lines the popup will occupy.
func (m AutocompleteModel) Height() int {
	if !m.Visible {
		return 0
	}
	n := len(m.Filtered)
	if n > m.maxShow {
		n = m.maxShow
	}
	return n + 2 // +2 for border top/bottom
}

// View renders the autocomplete popup.
func (m AutocompleteModel) View() string {
	if !m.Visible || len(m.Filtered) == 0 {
		return ""
	}

	popupWidth := m.width - 4
	if popupWidth < 30 {
		popupWidth = 30
	}

	show := m.Filtered
	if len(show) > m.maxShow {
		show = show[:m.maxShow]
	}

	var lines []string
	for i, cmd := range show {
		nameW := 12
		name := cmd.Name
		if len(name) < nameW {
			name += strings.Repeat(" ", nameW-len(name))
		}

		desc := cmd.Description
		maxDesc := popupWidth - nameW - 4
		if maxDesc > 0 && len(desc) > maxDesc {
			desc = desc[:maxDesc-1] + theme.SymbolEllipsis
		}

		line := name + " " + theme.TextMuted.Render(desc)
		if i == m.Selected {
			line = theme.TextInfo.Render(theme.SymbolArrowR+" ") + line
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorBorderActive).
		Padding(0, 1).
		Render(content)
}
