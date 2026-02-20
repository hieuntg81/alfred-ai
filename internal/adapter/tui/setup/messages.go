// Package setup implements a Bubble Tea TUI setup wizard for alfred-ai.
package setup

import "alfred-ai/cmd/agent/setup"

// ValidationResultMsg carries the result of async API key validation.
type ValidationResultMsg struct {
	Err error
}

// ModelsResultMsg carries the result of async model fetching.
type ModelsResultMsg struct {
	Models []setup.ModelInfo
}

// PhaseAdvanceMsg signals to move to the next wizard phase.
type PhaseAdvanceMsg struct{}

// PhaseBackMsg signals to go back to the previous phase.
type PhaseBackMsg struct{}

// WizardDoneMsg signals that the wizard is complete.
type WizardDoneMsg struct {
	Cancelled bool
}
