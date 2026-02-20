package tabs

import (
	tea "github.com/charmbracelet/bubbletea"

	"alfred-ai/internal/adapter/tui/components"
	"alfred-ai/internal/domain"
)

// MemoryModel wraps the memory viewer component for the dashboard tab.
type MemoryModel struct {
	Viewer components.MemoryViewerModel
}

// NewMemory creates a memory tab.
func NewMemory() MemoryModel {
	return MemoryModel{
		Viewer: components.NewMemoryViewer(),
	}
}

// SetSize sets dimensions.
func (m *MemoryModel) SetSize(w, h int) {
	m.Viewer.SetSize(w, h)
}

// SetResults updates displayed entries.
func (m *MemoryModel) SetResults(entries []domain.MemoryEntry) {
	m.Viewer.SetResults(entries)
}

// SetError shows an error message.
func (m *MemoryModel) SetError(errMsg string) {
	m.Viewer.SetError(errMsg)
}

// Update handles input.
func (m MemoryModel) Update(msg tea.Msg) (MemoryModel, tea.Cmd) {
	var cmd tea.Cmd
	m.Viewer, cmd = m.Viewer.Update(msg)
	return m, cmd
}

// View renders the memory tab.
func (m MemoryModel) View() string {
	return m.Viewer.View()
}
