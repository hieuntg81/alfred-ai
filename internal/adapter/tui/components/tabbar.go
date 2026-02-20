// Package components provides reusable Bubble Tea sub-models for the TUI.
package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"alfred-ai/internal/adapter/tui/theme"
)

// Tab represents a single tab entry.
type Tab struct {
	ID    string
	Label string
	Badge int // notification badge count; 0 = hidden
}

// TabBarModel is a horizontal tab bar with keyboard navigation.
type TabBarModel struct {
	Tabs      []Tab
	Active    int
	width     int
	collapsed bool // true when width < MinTabWidth
}

// NewTabBar creates a tab bar with the given tabs. The first tab is active.
func NewTabBar(tabs []Tab) TabBarModel {
	return TabBarModel{Tabs: tabs}
}

// SetWidth updates the available width and determines if tabs should collapse.
func (m *TabBarModel) SetWidth(w int) {
	m.width = w
	m.collapsed = w < theme.MinTabWidth
}

// Next advances to the next tab, wrapping around.
func (m *TabBarModel) Next() {
	if len(m.Tabs) == 0 {
		return
	}
	m.Active = (m.Active + 1) % len(m.Tabs)
}

// Prev moves to the previous tab, wrapping around.
func (m *TabBarModel) Prev() {
	if len(m.Tabs) == 0 {
		return
	}
	m.Active = (m.Active - 1 + len(m.Tabs)) % len(m.Tabs)
}

// SetActive sets the active tab by index.
func (m *TabBarModel) SetActive(i int) {
	if i >= 0 && i < len(m.Tabs) {
		m.Active = i
	}
}

// Update handles key messages for tab switching.
func (m TabBarModel) Update(msg tea.Msg) (TabBarModel, tea.Cmd) {
	// Tab bar does not consume keys on its own; the parent model routes
	// Ctrl+N/Ctrl+P to Next()/Prev(). This Update is provided for
	// symmetry with the Bubble Tea pattern.
	return m, nil
}

// View renders the tab bar.
func (m TabBarModel) View() string {
	if len(m.Tabs) == 0 {
		return ""
	}

	if m.collapsed {
		// Collapsed mode: show only the active tab with index.
		t := m.Tabs[m.Active]
		label := theme.TabActive.Render(t.Label)
		counter := theme.Dim.Render(
			strings.Join([]string{"[", intStr(m.Active + 1), "/", intStr(len(m.Tabs)), "]"}, ""),
		)
		return lipgloss.JoinHorizontal(lipgloss.Center, label, " ", counter)
	}

	var parts []string
	for i, t := range m.Tabs {
		label := t.Label
		if t.Badge > 0 {
			label += " " + theme.TextWarning.Render(intStr(t.Badge))
		}
		if i == m.Active {
			parts = append(parts, theme.TabActive.Render(label))
		} else {
			parts = append(parts, theme.TabNormal.Render(label))
		}
	}

	bar := lipgloss.JoinHorizontal(lipgloss.Center, parts...)

	// Pad to full width.
	if m.width > 0 {
		bg := theme.TabNormal.Copy().UnsetPadding()
		remaining := m.width - lipgloss.Width(bar)
		if remaining > 0 {
			bar += bg.Render(strings.Repeat(" ", remaining))
		}
	}

	return bar
}

// intStr converts an int to its string representation without importing strconv.
func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
