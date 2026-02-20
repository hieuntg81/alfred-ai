package wizard

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"alfred-ai/internal/adapter/tui/theme"
)

// FieldSubmitMsg is sent when a form field value is submitted.
type FieldSubmitMsg struct {
	Value string
}

// FormFieldModel wraps a textinput for wizard forms (text, secret, confirm).
type FormFieldModel struct {
	Input       textinput.Model
	Label       string
	Description string
	IsSecret    bool
	IsConfirm   bool // y/n field
	ErrMsg      string
}

// NewTextField creates a text input field.
func NewTextField(label, placeholder string) FormFieldModel {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Focus()
	ti.Width = 50
	ti.PromptStyle = theme.InputPrompt
	ti.PlaceholderStyle = theme.InputPlaceholder

	return FormFieldModel{
		Input: ti,
		Label: label,
	}
}

// NewSecretField creates a secret (password) input field.
func NewSecretField(label, placeholder string) FormFieldModel {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Focus()
	ti.Width = 50
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.PromptStyle = theme.InputPrompt
	ti.PlaceholderStyle = theme.InputPlaceholder

	return FormFieldModel{
		Input:    ti,
		Label:    label,
		IsSecret: true,
	}
}

// NewConfirmField creates a yes/no confirmation field.
func NewConfirmField(label string, defaultYes bool) FormFieldModel {
	ti := textinput.New()
	if defaultYes {
		ti.Placeholder = "Y/n"
	} else {
		ti.Placeholder = "y/N"
	}
	ti.Focus()
	ti.Width = 10
	ti.CharLimit = 3
	ti.PromptStyle = theme.InputPrompt
	ti.PlaceholderStyle = theme.InputPlaceholder

	return FormFieldModel{
		Input:     ti,
		Label:     label,
		IsConfirm: true,
	}
}

// SetError displays a validation error message.
func (m *FormFieldModel) SetError(msg string) {
	m.ErrMsg = msg
}

// ClearError clears the validation error.
func (m *FormFieldModel) ClearError() {
	m.ErrMsg = ""
}

// Value returns the current input value.
func (m FormFieldModel) Value() string {
	return strings.TrimSpace(m.Input.Value())
}

// ConfirmValue interprets the input as a boolean.
func (m FormFieldModel) ConfirmValue(defaultYes bool) bool {
	v := strings.ToLower(m.Value())
	if v == "" {
		return defaultYes
	}
	return v == "y" || v == "yes"
}

// Update handles input events.
func (m FormFieldModel) Update(msg tea.Msg) (FormFieldModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.Type == tea.KeyEnter {
			value := m.Value()
			return m, func() tea.Msg {
				return FieldSubmitMsg{Value: value}
			}
		}
	}

	var cmd tea.Cmd
	m.Input, cmd = m.Input.Update(msg)
	return m, cmd
}

// View renders the form field.
func (m FormFieldModel) View() string {
	label := theme.Bold.Render(m.Label)

	var parts []string
	parts = append(parts, label)

	if m.Description != "" {
		parts = append(parts, theme.TextMuted.Render(m.Description))
	}

	parts = append(parts, "")
	parts = append(parts, m.Input.View())

	if m.ErrMsg != "" {
		errLine := theme.TextError.Render(theme.SymbolError + " " + m.ErrMsg)
		parts = append(parts, errLine)
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
