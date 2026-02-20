package setup

import (
	"bufio"
	"io"
	"time"

	"alfred-ai/internal/infra/config"
)

// Phase represents a single phase of the onboarding process.
type Phase interface {
	Execute(wizard *OnboardingWizard) error
}

// OnboardingWizard orchestrates the user-friendly onboarding flow.
type OnboardingWizard struct {
	reader   *bufio.Reader
	writer   io.Writer
	config   *config.Config
	template *Template
	context  *OnboardingContext
}

// OnboardingContext tracks state throughout the onboarding process.
type OnboardingContext struct {
	StartTime      time.Time
	SkipValidation bool
	TestResults    []TestResult
}

// NewOnboardingWizard creates a new onboarding wizard.
func NewOnboardingWizard(r io.Reader, w io.Writer) *OnboardingWizard {
	return &OnboardingWizard{
		reader: bufio.NewReader(r),
		writer: w,
		config: config.Defaults(),
		context: &OnboardingContext{
			StartTime: time.Now(),
		},
	}
}

// Run executes the complete onboarding flow through all phases.
func (w *OnboardingWizard) Run() (*config.Config, error) {
	phases := []Phase{
		&WelcomePhase{},
		&TemplateSelectionPhase{},
		&LLMSetupPhase{},
		&ChannelConfigPhase{},
		&SecurityOptionsPhase{},
		&ValidationPhase{},
		&CompletionPhase{},
	}

	for _, phase := range phases {
		if err := phase.Execute(w); err != nil {
			return nil, err
		}
	}

	return w.config, nil
}
