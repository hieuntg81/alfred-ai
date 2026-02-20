package setup

// Phase represents a wizard phase.
type Phase int

const (
	PhaseWelcome Phase = iota
	PhaseTemplate
	PhaseLLM
	PhaseLLMKey
	PhaseLLMModel
	PhaseChannels
	PhaseSecurity
	PhaseCompletion
	PhaseCount // sentinel
)

// PhaseInfo describes a phase for the step indicator.
type PhaseInfo struct {
	Name string
}

// AllPhases returns display info for each phase.
func AllPhases() []PhaseInfo {
	return []PhaseInfo{
		{Name: "Welcome"},
		{Name: "Template"},
		{Name: "AI Provider"},
		{Name: "API Key"},
		{Name: "Model"},
		{Name: "Channels"},
		{Name: "Security"},
		{Name: "Complete"},
	}
}

// ProviderChoice represents a selectable LLM provider.
type ProviderChoice struct {
	ID          string
	Name        string
	Description string
}

// Providers returns the available LLM providers.
func Providers() []ProviderChoice {
	return []ProviderChoice{
		{ID: "openai", Name: "OpenAI", Description: "GPT-4o, GPT-4, o1 - Recommended"},
		{ID: "anthropic", Name: "Anthropic", Description: "Claude 4.5, Claude 4"},
		{ID: "gemini", Name: "Google", Description: "Gemini 2.5 Pro, Flash"},
		{ID: "openrouter", Name: "OpenRouter", Description: "100+ models, one API key"},
	}
}
