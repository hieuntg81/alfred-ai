package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"alfred-ai/internal/adapter/tui/theme"
)

// CanvasPaneModel displays canvas HTML content in a scrollable pane.
type CanvasPaneModel struct {
	Viewport viewport.Model
	Name     string
	content  string
	ready    bool
	width    int
	height   int
}

// NewCanvasPane creates a canvas pane.
func NewCanvasPane() CanvasPaneModel {
	return CanvasPaneModel{}
}

// SetSize sets the pane dimensions.
func (m *CanvasPaneModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	if !m.ready {
		m.Viewport = viewport.New(w, h)
		m.Viewport.MouseWheelEnabled = true
		m.ready = true
	} else {
		m.Viewport.Width = w
		m.Viewport.Height = h
	}
	m.refreshContent()
}

// SetCanvas updates the displayed canvas.
func (m *CanvasPaneModel) SetCanvas(name, content string) {
	m.Name = name
	m.content = content
	m.refreshContent()
}

// Clear removes the displayed canvas.
func (m *CanvasPaneModel) Clear() {
	m.Name = ""
	m.content = ""
	m.refreshContent()
}

// Update handles viewport scrolling.
func (m CanvasPaneModel) Update(msg tea.Msg) (CanvasPaneModel, tea.Cmd) {
	if !m.ready {
		return m, nil
	}
	var cmd tea.Cmd
	m.Viewport, cmd = m.Viewport.Update(msg)
	return m, cmd
}

// View renders the canvas pane.
func (m CanvasPaneModel) View() string {
	if !m.ready {
		return ""
	}
	header := theme.Bold.Render(fmt.Sprintf(" Canvas: %s", m.Name))
	return header + "\n" + m.Viewport.View()
}

func (m *CanvasPaneModel) refreshContent() {
	if !m.ready {
		return
	}
	if m.content == "" {
		m.Viewport.SetContent(theme.TextMuted.Render("  No canvas displayed"))
		return
	}

	contentWidth := m.width - 8
	if contentWidth < 20 {
		contentWidth = 20
	}

	var sb strings.Builder
	lines := strings.Split(m.content, "\n")
	for i, line := range lines {
		lineNum := fmt.Sprintf("%4d", i+1)
		if len(line) > contentWidth {
			line = line[:contentWidth-1] + theme.SymbolEllipsis
		}
		sb.WriteString(theme.Dim.Render(lineNum) + "  " + line + "\n")
	}
	m.Viewport.SetContent(sb.String())
}
