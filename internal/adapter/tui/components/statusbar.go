package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"alfred-ai/internal/adapter/tui/theme"
)

// KeyHint represents a single keybinding hint shown in the status bar.
type KeyHint struct {
	Key  string // e.g. "Enter"
	Desc string // e.g. "Send"
}

// StatusBarModel renders a bottom status bar with keybinding hints and agent info.
type StatusBarModel struct {
	Hints     []KeyHint // show 4-5 most important hints
	AgentName string
	ModelName string
	Extra     string // additional status text (e.g. "Thinking...")
	width     int
}

// NewStatusBar creates a status bar with default hints.
func NewStatusBar() StatusBarModel {
	return StatusBarModel{}
}

// SetWidth updates the available width.
func (m *StatusBarModel) SetWidth(w int) {
	m.width = w
}

// View renders the status bar as a single line.
func (m StatusBarModel) View() string {
	// Left side: keybinding hints.
	var hints []string
	for _, h := range m.Hints {
		key := theme.StatusKey.Render(h.Key)
		hints = append(hints, key+": "+h.Desc)
	}
	left := strings.Join(hints, "  "+theme.Dim.Render("|")+"  ")

	// Right side: agent/model info.
	var right string
	if m.AgentName != "" || m.ModelName != "" {
		var parts []string
		if m.AgentName != "" {
			parts = append(parts, m.AgentName)
		}
		if m.ModelName != "" {
			parts = append(parts, m.ModelName)
		}
		right = theme.TextMuted.Render(strings.Join(parts, " "+theme.SymbolBullet+" "))
	}

	if m.Extra != "" {
		if right != "" {
			right += "  "
		}
		right += theme.TextInfo.Render(m.Extra)
	}

	// Join left and right, padding the gap.
	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := m.width - leftW - rightW
	if gap < 1 {
		gap = 1
	}

	bar := left + strings.Repeat(" ", gap) + right
	return theme.StatusBar.Width(m.width).Render(bar)
}
