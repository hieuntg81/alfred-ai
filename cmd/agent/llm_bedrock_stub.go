//go:build !bedrock

package main

import (
	"fmt"
	"log/slog"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

func createBedrockProvider(_ config.ProviderConfig, _ *slog.Logger) (domain.LLMProvider, error) {
	return nil, fmt.Errorf("bedrock provider requires build with -tags bedrock")
}
