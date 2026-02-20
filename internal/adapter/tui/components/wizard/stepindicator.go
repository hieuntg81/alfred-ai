// Package wizard provides TUI components for the setup wizard.
package wizard

import (
	"fmt"
	"strings"

	"alfred-ai/internal/adapter/tui/theme"
)

// Step represents a single wizard step.
type Step struct {
	Name string
}

// StepIndicatorModel displays wizard progress as "Step 3/7: LLM Provider" with a progress bar.
type StepIndicatorModel struct {
	Steps   []Step
	Current int
	width   int
}

// NewStepIndicator creates a step indicator.
func NewStepIndicator(steps []Step) StepIndicatorModel {
	return StepIndicatorModel{Steps: steps}
}

// SetWidth sets the rendering width.
func (m *StepIndicatorModel) SetWidth(w int) {
	m.width = w
}

// SetCurrent sets the active step index.
func (m *StepIndicatorModel) SetCurrent(i int) {
	if i >= 0 && i < len(m.Steps) {
		m.Current = i
	}
}

// View renders the step indicator.
func (m StepIndicatorModel) View() string {
	if len(m.Steps) == 0 || m.width < 20 {
		return ""
	}

	// Header: "Step 3/7: LLM Provider"
	stepName := ""
	if m.Current < len(m.Steps) {
		stepName = m.Steps[m.Current].Name
	}
	header := theme.WizardStepActive.Render(
		fmt.Sprintf("Step %d/%d: %s", m.Current+1, len(m.Steps), stepName),
	)

	// Progress bar.
	barWidth := m.width - 10 // leave room for percentage
	if barWidth < 10 {
		barWidth = 10
	}
	pct := float64(m.Current) / float64(len(m.Steps))
	filled := int(pct * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	bar := theme.ProgressFull.Render(strings.Repeat("█", filled)) +
		theme.ProgressEmpty.Render(strings.Repeat("░", barWidth-filled))
	pctStr := theme.TextMuted.Render(fmt.Sprintf(" %d%%", int(pct*100)))

	return header + "\n" + bar + pctStr
}
