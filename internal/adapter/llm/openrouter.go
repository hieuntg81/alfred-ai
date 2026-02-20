package llm

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

// Compile-time interface assertions.
var (
	_ domain.LLMProvider          = (*OpenRouterProvider)(nil)
	_ domain.StreamingLLMProvider = (*OpenRouterProvider)(nil)
)

// openrouterTransport is a custom http.RoundTripper that injects
// OpenRouter-specific headers (HTTP-Referer and X-Title) into every request.
type openrouterTransport struct {
	base http.RoundTripper
}

func (t *openrouterTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid mutating the original.
	clone := req.Clone(req.Context())
	clone.Header.Set("HTTP-Referer", "https://github.com/byterover/alfred-ai")
	clone.Header.Set("X-Title", "alfred-ai")
	return t.base.RoundTrip(clone)
}

// OpenRouterProvider wraps OpenAIProvider to work with the OpenRouter API.
type OpenRouterProvider struct {
	inner *OpenAIProvider
}

// NewOpenRouterProvider creates an OpenRouter provider that delegates to OpenAIProvider
// with a custom transport for OpenRouter-specific headers.
func NewOpenRouterProvider(cfg config.ProviderConfig, logger *slog.Logger) *OpenRouterProvider {
	client := NewHTTPClient(cfg)
	// Wrap transport with OpenRouter-specific headers.
	client.Transport = &openrouterTransport{base: client.Transport}

	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}

	return &OpenRouterProvider{
		inner: &OpenAIProvider{
			name:    cfg.Name,
			model:   cfg.Model,
			apiKey:  cfg.APIKey,
			baseURL: baseURL,
			client:  client,
			logger:  logger,
		},
	}
}

// Chat implements domain.LLMProvider.
func (p *OpenRouterProvider) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	return p.inner.Chat(ctx, req)
}

// ChatStream implements domain.StreamingLLMProvider.
func (p *OpenRouterProvider) ChatStream(ctx context.Context, req domain.ChatRequest) (<-chan domain.StreamDelta, error) {
	return p.inner.ChatStream(ctx, req)
}

// Name implements domain.LLMProvider.
func (p *OpenRouterProvider) Name() string { return p.inner.Name() }
