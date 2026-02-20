package components

import (
	"github.com/charmbracelet/lipgloss"

	"alfred-ai/internal/adapter/tui/theme"
)

// Pane identifies which pane is focused.
type Pane int

const (
	PaneLeft Pane = iota
	PaneRight
)

// SplitPaneModel manages a horizontal split layout with focus tracking.
type SplitPaneModel struct {
	Focused Pane
	Visible bool // whether the right pane is shown
	Ratio   float64
	width   int
	height  int
}

// NewSplitPane creates a split pane. ratio is the fraction of width for the left pane (0.0–1.0).
func NewSplitPane(ratio float64) SplitPaneModel {
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.65
	}
	return SplitPaneModel{
		Focused: PaneLeft,
		Visible: false,
		Ratio:   ratio,
	}
}

// SetSize updates the available dimensions.
func (m *SplitPaneModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	// Auto-hide right pane on narrow terminals.
	if w < theme.MinSplitWidth {
		m.Visible = false
	}
}

// Toggle shows/hides the right pane.
func (m *SplitPaneModel) Toggle() {
	if m.width < theme.MinSplitWidth {
		return // too narrow
	}
	m.Visible = !m.Visible
	if !m.Visible {
		m.Focused = PaneLeft
	}
}

// SwitchFocus moves focus to the other pane.
func (m *SplitPaneModel) SwitchFocus() {
	if !m.Visible {
		return
	}
	if m.Focused == PaneLeft {
		m.Focused = PaneRight
	} else {
		m.Focused = PaneLeft
	}
}

// LeftWidth returns the width allocated to the left pane.
func (m SplitPaneModel) LeftWidth() int {
	if !m.Visible {
		return m.width
	}
	divider := 1 // 1-char vertical divider
	return int(float64(m.width-divider) * m.Ratio)
}

// RightWidth returns the width allocated to the right pane.
func (m SplitPaneModel) RightWidth() int {
	if !m.Visible {
		return 0
	}
	divider := 1
	return m.width - divider - m.LeftWidth()
}

// Height returns the content height.
func (m SplitPaneModel) Height() int {
	return m.height
}

// Render joins left and right content side-by-side with a focus-aware divider.
// The divider is highlighted when the right pane is focused.
func (m SplitPaneModel) Render(left, right string) string {
	if !m.Visible {
		return left
	}

	// Use an active-colored divider when the right pane is focused.
	divColor := theme.ColorBorder
	if m.Focused == PaneRight {
		divColor = theme.ColorBorderActive
	}
	divider := lipgloss.NewStyle().
		Foreground(divColor).
		Render("│")

	// Build divider column at the full height.
	var divCol string
	for i := 0; i < m.height; i++ {
		if i > 0 {
			divCol += "\n"
		}
		divCol += divider
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, left, divCol, right)
}
