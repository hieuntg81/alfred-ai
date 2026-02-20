package dashboard

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"alfred-ai/internal/domain"
)

// queryMemoryCmd runs a memory query asynchronously.
func queryMemoryCmd(mem domain.MemoryProvider, query string) tea.Cmd {
	return func() tea.Msg {
		entries, err := mem.Query(context.Background(), query, 50)
		return MemoryQueryResultMsg{Entries: entries, Err: err}
	}
}
