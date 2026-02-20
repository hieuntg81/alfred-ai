package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"alfred-ai/internal/adapter/tui/theme"
)

// ToolCallSummary represents a tool invocation that happened during a response.
type ToolCallSummary struct {
	Name     string
	Duration time.Duration
	IsError  bool
}

// MessageRole identifies the sender of a chat message.
type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleSystem    MessageRole = "system"
	RoleTool      MessageRole = "tool"
	RoleError     MessageRole = "error"
)

// ChatMessage represents a single message in the chat history.
type ChatMessage struct {
	Role      MessageRole
	Content   string
	Rendered  string // cached glamour output; empty means not yet rendered
	Timestamp time.Time
	ToolName  string            // only for RoleTool
	ToolCalls []ToolCallSummary // tools used before this response (assistant only)
}

// MessageListModel manages an ordered list of chat messages with optional ring buffer.
type MessageListModel struct {
	Messages    []ChatMessage
	MaxMessages int // 0 = unlimited; positive = ring buffer cap
	trimCount   int // number of messages trimmed so far
	width       int
	mdRenderer  *glamour.TermRenderer
}

// NewMessageList creates an empty message list.
func NewMessageList() MessageListModel {
	return MessageListModel{}
}

// SetWidth updates the rendering width and clears cached renders.
func (m *MessageListModel) SetWidth(w int) {
	if w == m.width {
		return
	}
	m.width = w
	m.mdRenderer = nil // force re-creation with new width
	// Clear cached renders so they get re-rendered at new width.
	for i := range m.Messages {
		m.Messages[i].Rendered = ""
	}
}

// SetMaxMessages sets the ring buffer capacity. 0 means unlimited.
func (m *MessageListModel) SetMaxMessages(max int) {
	m.MaxMessages = max
}

// TrimmedIndicator returns a message if older messages were trimmed, empty otherwise.
func (m *MessageListModel) TrimmedIndicator() string {
	if m.trimCount == 0 {
		return ""
	}
	return fmt.Sprintf("(%d older messages trimmed)", m.trimCount)
}

// Add appends a message. If MaxMessages is set, trims oldest messages.
func (m *MessageListModel) Add(msg ChatMessage) {
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	m.Messages = append(m.Messages, msg)
	if m.MaxMessages > 0 && len(m.Messages) > m.MaxMessages {
		excess := len(m.Messages) - m.MaxMessages
		m.Messages = m.Messages[excess:]
		m.trimCount += excess
	}
}

// Clear removes all messages.
func (m *MessageListModel) Clear() {
	m.Messages = nil
}

// UpdateLast replaces the content of the last message (for streaming).
func (m *MessageListModel) UpdateLast(content string) {
	if len(m.Messages) == 0 {
		return
	}
	m.Messages[len(m.Messages)-1].Content = content
	m.Messages[len(m.Messages)-1].Rendered = "" // invalidate cache
}

// View renders all messages as a single string.
func (m *MessageListModel) View() string {
	if len(m.Messages) == 0 {
		return theme.TextMuted.Render("  No messages yet. Start a conversation!")
	}

	contentWidth := m.width - 4 // padding
	if contentWidth > theme.MaxContentWidth {
		contentWidth = theme.MaxContentWidth
	}
	if contentWidth < 40 {
		contentWidth = 40
	}

	var sb strings.Builder
	if indicator := m.TrimmedIndicator(); indicator != "" {
		sb.WriteString(theme.TextMuted.Render("  "+indicator) + "\n\n")
	}
	for i := range m.Messages {
		msg := &m.Messages[i]
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(m.renderMessage(msg, contentWidth))
	}
	return sb.String()
}

func (m *MessageListModel) renderMessage(msg *ChatMessage, width int) string {
	// Header: role label + timestamp.
	label := m.roleLabel(msg.Role, msg.ToolName)
	ts := RelativeTime(msg.Timestamp)
	header := label + " " + theme.Timestamp.Render(ts)
	headerWidth := lipgloss.Width(header)

	// Tool summary (for assistant messages that used tools).
	toolSummary := renderToolSummary(msg.ToolCalls, width)

	// Body: render markdown for assistant messages, plain wrap for others.
	var body string
	switch msg.Role {
	case RoleAssistant:
		if msg.Rendered == "" {
			msg.Rendered = m.renderMarkdown(msg.Content, width)
		}
		body = strings.TrimSpace(msg.Rendered)
	case RoleError:
		body = theme.TextError.Render(wrapText(msg.Content, width-2))
	default:
		inlineW := width - headerWidth - 2
		if inlineW < 20 {
			inlineW = width - 2
		}
		body = wrapText(msg.Content, inlineW)
	}

	if body == "" {
		if toolSummary != "" {
			return header + "\n" + toolSummary
		}
		return header
	}

	// If tool calls exist, put them between header and body on separate lines.
	if toolSummary != "" {
		return header + "\n" + toolSummary + body
	}

	// Inline: put header and first line of body on the same line.
	if width-headerWidth-2 < 20 {
		return header + "\n  " + body
	}

	lines := strings.SplitN(body, "\n", 2)
	firstLine := strings.TrimSpace(lines[0])
	result := header + "  " + firstLine
	if len(lines) > 1 {
		// wrapText and glamour already handle continuation indentation;
		// just append the remaining lines as-is.
		result += "\n" + lines[1]
	}
	return result
}

// renderToolSummary renders a compact list of tool calls used during a response.
// Width is used to truncate long tool names on narrow terminals.
func renderToolSummary(tools []ToolCallSummary, width int) string {
	if len(tools) == 0 {
		return ""
	}
	// Reserve space for indent(2) + icon(1) + space(1) + duration(~12).
	maxNameLen := width - 16
	if maxNameLen < 10 {
		maxNameLen = 10
	}

	var sb strings.Builder
	for _, tc := range tools {
		icon := theme.TextSuccess.Render(theme.SymbolSuccess)
		if tc.IsError {
			icon = theme.TextError.Render(theme.SymbolError)
		}
		name := tc.Name
		if len(name) > maxNameLen {
			name = name[:maxNameLen-1] + theme.SymbolEllipsis
		}
		dur := ""
		if tc.Duration > 0 {
			dur = " " + theme.TextMuted.Render(tc.Duration.Round(time.Millisecond).String())
		}
		sb.WriteString("  " + icon + " " + theme.Dim.Render(name) + dur + "\n")
	}
	return sb.String()
}

func (m *MessageListModel) roleLabel(role MessageRole, toolName string) string {
	switch role {
	case RoleUser:
		return theme.UserLabel.Render(theme.SymbolUser)
	case RoleAssistant:
		return theme.BotLabel.Render(theme.SymbolBot)
	case RoleSystem:
		return theme.SystemLabel.Render("System")
	case RoleTool:
		name := "Tool"
		if toolName != "" {
			name = toolName
		}
		return theme.ToolLabel.Render(theme.SymbolArrowR + " " + name)
	case RoleError:
		return theme.ErrorLabel.Render(theme.SymbolError + " Error")
	default:
		return theme.TextMuted.Render(string(role))
	}
}

func (m *MessageListModel) renderMarkdown(content string, width int) string {
	if m.mdRenderer == nil {
		r, err := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return "  " + content
		}
		m.mdRenderer = r
	}
	rendered, err := m.mdRenderer.Render(content)
	if err != nil {
		return "  " + content
	}
	return rendered
}

// RelativeTime returns a human-readable relative time string.
func RelativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		return fmt.Sprintf("%dh ago", h)
	default:
		return t.Format("Jan 2 15:04")
	}
}

// wrapText wraps text to the given width with a 2-space indent on continuation lines.
// Uses rune-based indexing to safely handle multibyte UTF-8.
func wrapText(s string, width int) string {
	runes := []rune(s)
	if width <= 0 || len(runes) <= width {
		return s
	}
	var lines []string
	for len(runes) > width {
		// Find a good break point (space) within width.
		idx := -1
		for i := width - 1; i > 0; i-- {
			if runes[i] == ' ' {
				idx = i
				break
			}
		}
		if idx <= 0 {
			idx = width
		}
		lines = append(lines, string(runes[:idx]))
		runes = runes[idx:]
		// Trim leading spaces.
		for len(runes) > 0 && runes[0] == ' ' {
			runes = runes[1:]
		}
	}
	if len(runes) > 0 {
		lines = append(lines, string(runes))
	}
	return strings.Join(lines, "\n  ")
}

// TruncatePath smartly truncates a file path with ellipsis in the middle.
// e.g. "/home/user/very/deep/nested/path/file.go" -> "/home/.../path/file.go"
func TruncatePath(path string, maxLen int) string {
	if len(path) <= maxLen || maxLen < 10 {
		return path
	}

	sep := "/"
	parts := strings.Split(path, sep)
	if len(parts) <= 3 {
		return path[:maxLen-1] + theme.SymbolEllipsis
	}

	// Keep first and last 2 parts.
	head := parts[0]
	tail := strings.Join(parts[len(parts)-2:], sep)
	result := head + sep + theme.SymbolEllipsis + sep + tail

	if len(result) > maxLen {
		return path[:maxLen-1] + theme.SymbolEllipsis
	}
	return result
}

// ContentWidth calculates the content width respecting MaxContentWidth.
func ContentWidth(termWidth int) int {
	w := termWidth - 4
	if w > theme.MaxContentWidth {
		w = theme.MaxContentWidth
	}
	if w < 40 {
		w = 40
	}
	return w
}

// Divider renders a horizontal line at the given width.
func Divider(width int) string {
	return lipgloss.NewStyle().
		Foreground(theme.ColorBorder).
		Render(strings.Repeat("─", width))
}
