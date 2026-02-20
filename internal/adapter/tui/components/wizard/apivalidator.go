package wizard

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"alfred-ai/internal/adapter/tui/theme"
)

// ValidationResultMsg carries the result of an async API key validation.
type ValidationResultMsg struct {
	Err error
}

// APIValidatorModel provides an async validation UI with spinner.
type APIValidatorModel struct {
	Spinner    spinner.Model
	Validating bool
	Success    bool
	ErrMsg     string
}

// NewAPIValidator creates a new validator model.
func NewAPIValidator() APIValidatorModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(theme.ColorInfo)

	return APIValidatorModel{
		Spinner: s,
	}
}

// Start begins the validation animation.
func (m *APIValidatorModel) Start() {
	m.Validating = true
	m.Success = false
	m.ErrMsg = ""
}

// HandleResult processes a validation result.
func (m *APIValidatorModel) HandleResult(err error) {
	m.Validating = false
	if err != nil {
		m.Success = false
		m.ErrMsg = err.Error()
	} else {
		m.Success = true
		m.ErrMsg = ""
	}
}

// Reset clears the validator state.
func (m *APIValidatorModel) Reset() {
	m.Validating = false
	m.Success = false
	m.ErrMsg = ""
}

// Update handles spinner ticks.
func (m APIValidatorModel) Update(msg tea.Msg) (APIValidatorModel, tea.Cmd) {
	if m.Validating {
		var cmd tea.Cmd
		m.Spinner, cmd = m.Spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

// View renders the validation state.
func (m APIValidatorModel) View() string {
	if m.Validating {
		return m.Spinner.View() + " Validating API key..."
	}
	if m.Success {
		return theme.TextSuccess.Render(theme.SymbolSuccess + " API key validated successfully!")
	}
	if m.ErrMsg != "" {
		return theme.TextError.Render(theme.SymbolError+" Validation failed: "+m.ErrMsg) +
			"\n" + theme.TextMuted.Render("  Press Enter to try again, or Esc to skip")
	}
	return ""
}
