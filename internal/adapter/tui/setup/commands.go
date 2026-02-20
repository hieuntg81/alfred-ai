package setup

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"alfred-ai/cmd/agent/setup"
)

// validateAPIKeyCmd runs API key validation asynchronously.
func validateAPIKeyCmd(providerType, apiKey string) tea.Cmd {
	return func() tea.Msg {
		err := setup.ValidateAPIKey(providerType, apiKey)
		return ValidationResultMsg{Err: err}
	}
}

// fetchModelsCmd fetches available models asynchronously.
func fetchModelsCmd(providerType, apiKey string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		models := setup.GetModelsWithFallback(ctx, providerType, apiKey)
		return ModelsResultMsg{Models: models}
	}
}
