package components

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// ChatViewModel wraps a viewport with smart auto-scroll behavior.
// Auto-scroll is active when the user is at the bottom.
// If the user scrolls up, auto-scroll pauses.
// It resumes when the user scrolls back to the bottom.
type ChatViewModel struct {
	Viewport viewport.Model
	Messages MessageListModel
	ready    bool
	atBottom bool
}

// NewChatView creates a chat view. The viewport is initialized lazily on the first WindowSizeMsg.
func NewChatView() ChatViewModel {
	return ChatViewModel{
		Messages: NewMessageList(),
		atBottom: true,
	}
}

// SetMaxMessages sets the ring buffer capacity for the message list.
func (m *ChatViewModel) SetMaxMessages(max int) {
	m.Messages.SetMaxMessages(max)
}

// SetSize sets the viewport dimensions and triggers content re-render.
func (m *ChatViewModel) SetSize(w, h int) {
	m.Messages.SetWidth(w)
	if !m.ready {
		m.Viewport = viewport.New(w, h)
		m.Viewport.MouseWheelEnabled = true
		m.Viewport.MouseWheelDelta = 3
		m.ready = true
	} else {
		m.Viewport.Width = w
		m.Viewport.Height = h
	}
	m.refreshContent()
}

// AddMessage appends a message and scrolls to bottom if auto-scroll is active.
func (m *ChatViewModel) AddMessage(msg ChatMessage) {
	m.Messages.Add(msg)
	m.refreshContent()
	if m.atBottom {
		m.Viewport.GotoBottom()
	}
}

// UpdateLastMessage updates the last message content (for streaming).
func (m *ChatViewModel) UpdateLastMessage(content string) {
	m.Messages.UpdateLast(content)
	m.refreshContent()
	if m.atBottom {
		m.Viewport.GotoBottom()
	}
}

// Clear removes all messages and resets the viewport.
func (m *ChatViewModel) Clear() {
	m.Messages.Clear()
	m.refreshContent()
	m.atBottom = true
	m.Viewport.GotoTop()
}

// Update handles viewport scrolling and tracks auto-scroll state.
func (m ChatViewModel) Update(msg tea.Msg) (ChatViewModel, tea.Cmd) {
	if !m.ready {
		return m, nil
	}

	var cmd tea.Cmd
	m.Viewport, cmd = m.Viewport.Update(msg)

	// Track whether user is at the bottom for smart auto-scroll.
	m.atBottom = m.Viewport.AtBottom()

	return m, cmd
}

// RawLines returns the raw (un-rendered) message content split into lines.
// Use this for search instead of View() which contains ANSI escape codes.
func (m ChatViewModel) RawLines() []string {
	var lines []string
	for _, msg := range m.Messages.Messages {
		lines = append(lines, strings.Split(msg.Content, "\n")...)
	}
	return lines
}

// View renders the chat viewport.
func (m ChatViewModel) View() string {
	if !m.ready {
		return "  Initializing..."
	}
	return m.Viewport.View()
}

func (m *ChatViewModel) refreshContent() {
	if !m.ready {
		return
	}
	content := m.Messages.View()
	m.Viewport.SetContent(content)
}
