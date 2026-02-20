package components

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"alfred-ai/internal/adapter/tui/theme"
)

// InputSubmitMsg is sent when the user presses Enter to submit input.
type InputSubmitMsg struct {
	Value string
}

// InputAreaModel wraps a textarea with slash-command detection, autocomplete, and submit handling.
type InputAreaModel struct {
	Textarea     textarea.Model
	Autocomplete AutocompleteModel
	Enabled      bool
	width        int
}

// NewInputArea creates an input area with sensible defaults.
func NewInputArea() InputAreaModel {
	ta := textarea.New()
	ta.Placeholder = "Type your message..."
	ta.Prompt = "> "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0 // no limit
	ta.SetHeight(3)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Prompt = theme.InputPrompt
	ta.FocusedStyle.Placeholder = theme.InputPlaceholder
	ta.Focus()

	return InputAreaModel{
		Textarea: ta,
		Enabled:  true,
	}
}

// SetWidth updates the textarea width.
func (m *InputAreaModel) SetWidth(w int) {
	m.width = w
	m.Textarea.SetWidth(w - 2) // account for border/padding
	m.Autocomplete.SetWidth(w)
}

// SetEnabled enables or disables input (e.g. while waiting for response).
func (m *InputAreaModel) SetEnabled(enabled bool) {
	m.Enabled = enabled
	if enabled {
		m.Textarea.Focus()
	} else {
		m.Textarea.Blur()
	}
}

// Reset clears the input.
func (m *InputAreaModel) Reset() {
	m.Textarea.Reset()
}

// Value returns the current input text.
func (m InputAreaModel) Value() string {
	return m.Textarea.Value()
}

// IsSlashCommand checks if the current input starts with a slash.
func (m InputAreaModel) IsSlashCommand() bool {
	return strings.HasPrefix(strings.TrimSpace(m.Textarea.Value()), "/")
}

// ParseSlashCommand extracts command and args from slash command input.
func ParseSlashCommand(input string) (cmd string, args []string, ok bool) {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") {
		return "", nil, false
	}
	parts := strings.Fields(input)
	return strings.ToLower(parts[0]), parts[1:], true
}

// Update handles key events. Enter submits (unless Alt/Shift is held for newline).
// When the autocomplete popup is visible, Tab/arrow keys navigate it.
func (m InputAreaModel) Update(msg tea.Msg) (InputAreaModel, tea.Cmd) {
	if !m.Enabled {
		return m, nil
	}

	// Filter out mouse events — the textarea should never receive them.
	if _, ok := msg.(tea.MouseMsg); ok {
		return m, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		// When autocomplete popup is showing, intercept navigation keys.
		if m.Autocomplete.Visible {
			switch keyMsg.Type {
			case tea.KeyTab, tea.KeyDown:
				m.Autocomplete.SelectNext()
				return m, nil
			case tea.KeyShiftTab, tea.KeyUp:
				m.Autocomplete.SelectPrev()
				return m, nil
			case tea.KeyEnter:
				// Accept the selected command into the textarea (don't submit yet).
				accepted := m.Autocomplete.Accept()
				if accepted != "" {
					m.Textarea.SetValue(accepted + " ")
					m.Textarea.CursorEnd()
				}
				return m, nil
			case tea.KeyEsc:
				m.Autocomplete.Hide()
				return m, nil
			}
		}

		switch keyMsg.Type {
		case tea.KeyEnter:
			// Plain Enter = submit. Alt+Enter handled by textarea as newline.
			value := strings.TrimSpace(m.Textarea.Value())
			if value != "" {
				m.Textarea.Reset()
				m.Autocomplete.Hide()
				return m, func() tea.Msg {
					return InputSubmitMsg{Value: value}
				}
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.Textarea, cmd = m.Textarea.Update(msg)

	// Update autocomplete filter based on current input.
	value := m.Textarea.Value()
	if strings.HasPrefix(value, "/") && !strings.Contains(value, " ") {
		m.Autocomplete.SetPrefix(value)
	} else {
		m.Autocomplete.Hide()
	}

	return m, cmd
}

// View renders the input area with optional autocomplete popup above it.
func (m InputAreaModel) View() string {
	popup := m.Autocomplete.View()
	if popup != "" {
		return popup + "\n" + m.Textarea.View()
	}
	return m.Textarea.View()
}
