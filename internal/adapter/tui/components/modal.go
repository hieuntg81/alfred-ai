package components

import (
	"fmt"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"alfred-ai/internal/adapter/tui/theme"
)

// ModalModel is a full-screen overlay viewport for viewing long content.
type ModalModel struct {
	Viewport viewport.Model
	Title    string
	Visible  bool
	width    int
	height   int
}

// NewModal creates a modal.
func NewModal() ModalModel {
	return ModalModel{}
}

// Open shows the modal with the given content.
func (m *ModalModel) Open(title, content string) {
	m.Title = title
	m.Visible = true
	if m.width > 0 {
		m.Viewport = viewport.New(m.width-4, m.height-4)
	} else {
		m.Viewport = viewport.New(80, 24)
	}
	m.Viewport.MouseWheelEnabled = true
	m.Viewport.SetContent(content)
}

// Close hides the modal.
func (m *ModalModel) Close() {
	m.Visible = false
}

// SetSize updates the modal dimensions.
func (m *ModalModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	if m.Visible {
		m.Viewport.Width = w - 4
		m.Viewport.Height = h - 4
	}
}

// Update handles modal keys: Esc closes, j/k scroll, q closes.
func (m ModalModel) Update(msg tea.Msg) (ModalModel, tea.Cmd) {
	if !m.Visible {
		return m, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc", "q":
			m.Close()
			return m, nil
		case "j", "down":
			m.Viewport.LineDown(3)
			return m, nil
		case "k", "up":
			m.Viewport.LineUp(3)
			return m, nil
		case "g":
			m.Viewport.GotoTop()
			return m, nil
		case "G":
			m.Viewport.GotoBottom()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.Viewport, cmd = m.Viewport.Update(msg)
	return m, cmd
}

// View renders the modal overlay.
func (m ModalModel) View() string {
	if !m.Visible {
		return ""
	}

	// Title bar.
	titleBar := theme.Bold.Render("  " + m.Title)

	// Scroll percentage.
	pct := m.Viewport.ScrollPercent() * 100
	scrollInfo := theme.TextMuted.Render(fmt.Sprintf(" %.0f%%", pct))

	// Footer with close hint.
	footer := theme.Dim.Render("  Esc/q: close  j/k: scroll  g/G: top/bottom") +
		"  " + scrollInfo

	// Content viewport.
	content := m.Viewport.View()

	inner := lipgloss.JoinVertical(lipgloss.Left, titleBar, content, footer)

	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(theme.ColorBorderActive).
		Padding(0, 1).
		Width(m.width - 2).
		Height(m.height - 2).
		Render(inner)
}
