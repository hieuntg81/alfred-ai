//go:build bedrock

package main

import (
	"log/slog"

	"alfred-ai/internal/adapter/llm"
	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

func createBedrockProvider(pc config.ProviderConfig, log *slog.Logger) (domain.LLMProvider, error) {
	return llm.NewBedrockProvider(pc, log)
}
