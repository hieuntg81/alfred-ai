package setup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"alfred-ai/internal/infra/config"
)

// LLMSetupPhase implements the AI provider setup phase.
type LLMSetupPhase struct{}

// Execute guides the user through AI provider setup and API key configuration.
func (p *LLMSetupPhase) Execute(w *OnboardingWizard) error {
	ui := NewUIHelper(w.reader, w.writer)

	ui.PrintStepHeader(3, 6, "AI Provider Setup")

	ui.PrintSection("Choose Your AI Brain",
		"Think of this as choosing which AI will power your assistant. "+
			"Each provider has different strengths, but they all work great with alfred-ai.")

	ui.PrintEmptyLine()

	// Check if template already selected a provider
	providerType := w.config.LLM.DefaultProvider
	if providerType != "" {
		ui.PrintInfo("ℹ️", fmt.Sprintf("Your template uses %s by default", providerType))
		change, err := ui.AskConfirmation("Would you like to use a different provider?", false)
		if err != nil {
			return err
		}

		if !change {
			return p.setupAPIKey(w, providerType, ui)
		}
	}

	// Show provider options
	providers := []struct {
		name         string
		providerType string
		description  string
	}{
		{
			name:         "OpenAI (GPT-4)",
			providerType: "openai",
			description:  "Most popular • Great for general tasks • $$$",
		},
		{
			name:         "Anthropic (Claude)",
			providerType: "anthropic",
			description:  "Best for long conversations • Thoughtful responses • $$",
		},
		{
			name:         "Google (Gemini)",
			providerType: "gemini",
			description:  "Fast & affordable • Good for coding • $",
		},
		{
			name:         "OpenRouter",
			providerType: "openrouter",
			description:  "Access 100+ models • Single API key • Pay-per-use",
		},
	}

	for i, prov := range providers {
		fmt.Fprintf(w.writer, "  %d) %s\n", i+1, prov.name)
		fmt.Fprintf(w.writer, "     %s\n\n", prov.description)
	}

	choice, err := ui.AskChoice(len(providers))
	if err != nil {
		return err
	}

	selected := providers[choice-1]
	return p.setupAPIKey(w, selected.providerType, ui)
}

// setupAPIKey guides the user through API key setup and validation.
func (p *LLMSetupPhase) setupAPIKey(w *OnboardingWizard, providerType string, ui *UIHelper) error {
	ui.PrintEmptyLine()
	ui.PrintSection("API Key Setup",
		"An API key is like a password that lets alfred-ai communicate with the AI service. "+
			"You'll need to create one (most providers offer free credits to start).")

	// Show instructions
	_, steps := GetAPIKeyInstructions(providerType)
	ui.PrintInfo("📝", "How to get your API key:")
	ui.PrintEmptyLine()

	for _, step := range steps {
		ui.PrintBullet(step)
	}

	ui.PrintEmptyLine()

	// Check if user has key ready
	hasKey, err := ui.AskConfirmation("Do you have an API key ready?", false)
	if err != nil {
		return err
	}

	if !hasKey {
		skip, err := ui.AskConfirmation(
			"Skip for now? (You can add it later via environment variable)", false)
		if err != nil {
			return err
		}

		if skip {
			envVar := fmt.Sprintf("ALFREDAI_LLM_PROVIDER_%s_API_KEY", strings.ToUpper(providerType))
			ui.PrintWarning(fmt.Sprintf("Remember to set %s before running alfred-ai", envVar))
			return p.configureProvider(w, providerType, "", ui)
		}

		ui.PrintInfo("ℹ️", "Press Enter when you have your API key...")
		w.reader.ReadString('\n')
	}

	// Get and validate API key (with retries)
	maxAttempts := 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			ui.PrintInfo("🔄", fmt.Sprintf("Attempt %d of %d", attempt, maxAttempts))
		}

		apiKey, err := ui.AskSecret("Paste your API key")
		if err != nil {
			return err
		}

		apiKey = strings.TrimSpace(apiKey)
		if apiKey == "" {
			ui.PrintError("API key cannot be empty")
			continue
		}

		// Validate key
		ui.PrintInfo("⏳", "Validating API key (this may take a few seconds)...")

		if err := ValidateAPIKey(providerType, apiKey); err != nil {
			ui.PrintError(fmt.Sprintf("Validation failed: %v", err))

			if attempt < maxAttempts {
				retry, err := ui.AskConfirmation("Try again?", true)
				if err != nil || !retry {
					return fmt.Errorf("API key validation failed: %w", err)
				}
				continue
			}

			return fmt.Errorf("failed to validate API key after %d attempts", maxAttempts)
		}

		ui.PrintSuccess("API key validated successfully!")
		return p.configureProvider(w, providerType, apiKey, ui)
	}

	return fmt.Errorf("failed to set up API key")
}

// SelectModel fetches available models and lets the user choose one.
// Returns the selected model ID.
func SelectModel(w *OnboardingWizard, ui *UIHelper) (string, error) {
	providerType := w.config.LLM.DefaultProvider
	apiKey := ""
	if len(w.config.LLM.Providers) > 0 {
		apiKey = w.config.LLM.Providers[0].APIKey
	}

	ui.PrintInfo("⏳", "Fetching available models...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	models := GetModelsWithFallback(ctx, providerType, apiKey)
	recommended := RecommendedModel(providerType)

	if len(models) == 0 {
		if recommended != "" {
			return recommended, nil
		}
		return "default", nil
	}

	fmt.Fprintf(w.writer, "\nAvailable models for %s:\n\n", providerType)

	for i, m := range models {
		marker := ""
		if m.ID == recommended {
			marker = " (Recommended)"
		}
		fmt.Fprintf(w.writer, "  %d) %s%s\n", i+1, m.ID, marker)
		if m.Description != "" {
			fmt.Fprintf(w.writer, "     %s\n", m.Description)
		}
		fmt.Fprintln(w.writer)
	}

	choice, err := ui.AskChoice(len(models))
	if err != nil {
		return "", err
	}
	return models[choice-1].ID, nil
}

// configureProvider configures the LLM provider in the config.
func (p *LLMSetupPhase) configureProvider(w *OnboardingWizard, providerType, apiKey string, ui *UIHelper) error {
	ui.PrintEmptyLine()

	// Temporarily set provider info so SelectModel can read it
	w.config.LLM.DefaultProvider = providerType
	if len(w.config.LLM.Providers) == 0 {
		w.config.LLM.Providers = []config.ProviderConfig{{APIKey: apiKey}}
	} else {
		w.config.LLM.Providers[0].APIKey = apiKey
	}

	model, err := SelectModel(w, ui)
	if err != nil {
		return err
	}

	return p.saveProvider(w, providerType, apiKey, model, ui)
}

// saveProvider writes the provider config and prints confirmation.
func (p *LLMSetupPhase) saveProvider(w *OnboardingWizard, providerType, apiKey, model string, ui *UIHelper) error {
	w.config.LLM.DefaultProvider = providerType
	w.config.LLM.Providers = []config.ProviderConfig{
		{
			Name:   providerType,
			Type:   providerType,
			Model:  model,
			APIKey: apiKey,
		},
	}

	ui.PrintEmptyLine()
	ui.PrintSuccess(fmt.Sprintf("Configured %s with model %s", providerType, model))
	ui.PrintEmptyLine()
	ui.PrintSeparator()

	return nil
}
