package chat

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"alfred-ai/internal/domain"
)

// sendMessageCmd runs the message handler in a background goroutine with a
// cancellable context. gen identifies the request so stale responses from
// cancelled requests can be discarded.
func sendMessageCmd(ctx context.Context, handler domain.MessageHandler, msg domain.InboundMessage, gen uint64) tea.Cmd {
	return func() tea.Msg {
		err := handler(ctx, msg)
		return HandlerDoneMsg{Err: err, Gen: gen}
	}
}

// streamTickCmd returns a Cmd that fires a StreamTickMsg after the given delay.
// Used for simulated streaming (progressive rendering of complete responses).
func streamTickCmd(rate time.Duration) tea.Cmd {
	if rate <= 0 {
		rate = 16 * time.Millisecond
	}
	return tea.Tick(rate, func(_ time.Time) tea.Msg {
		return StreamTickMsg{}
	})
}
