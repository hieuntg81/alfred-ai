package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

// Compile-time interface assertions.
var (
	_ domain.LLMProvider          = (*OllamaProvider)(nil)
	_ domain.StreamingLLMProvider = (*OllamaProvider)(nil)
)

// Default Ollama timeouts: short connect (local), long response (model loading).
const (
	ollamaDefaultConnTimeout = 5 * time.Second
	ollamaDefaultRespTimeout = 300 * time.Second
)

// OllamaProvider wraps OpenAIProvider to work with the Ollama API.
// Ollama exposes an OpenAI-compatible endpoint at /v1, so chat and stream
// are delegated to the inner OpenAI provider. Ollama-specific features
// (model listing, health check) use the native API.
type OllamaProvider struct {
	inner   *OpenAIProvider
	baseURL string // native Ollama API base (without /v1)
	client  *http.Client
	logger  *slog.Logger
}

// OllamaModel describes a locally available Ollama model.
type OllamaModel struct {
	Name       string    `json:"name"`
	ModifiedAt time.Time `json:"modified_at"`
	Size       int64     `json:"size"`
}

// NewOllamaProvider creates an Ollama provider that delegates chat/stream
// to OpenAIProvider via Ollama's OpenAI-compatible /v1 endpoint.
func NewOllamaProvider(cfg config.ProviderConfig, logger *slog.Logger) *OllamaProvider {
	// Apply Ollama-specific timeout defaults.
	ollamaCfg := cfg
	if ollamaCfg.ConnTimeout == 0 {
		ollamaCfg.ConnTimeout = ollamaDefaultConnTimeout
	}
	if ollamaCfg.RespTimeout == 0 {
		ollamaCfg.RespTimeout = ollamaDefaultRespTimeout
	}

	client := NewHTTPClient(ollamaCfg)

	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	return &OllamaProvider{
		inner: &OpenAIProvider{
			name:    cfg.Name,
			model:   cfg.Model,
			apiKey:  "", // Ollama doesn't need an API key
			baseURL: baseURL + "/v1",
			client:  client,
			logger:  logger,
		},
		baseURL: baseURL,
		client:  client,
		logger:  logger,
	}
}

// Chat implements domain.LLMProvider.
func (p *OllamaProvider) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	return p.inner.Chat(ctx, req)
}

// ChatStream implements domain.StreamingLLMProvider.
func (p *OllamaProvider) ChatStream(ctx context.Context, req domain.ChatRequest) (<-chan domain.StreamDelta, error) {
	return p.inner.ChatStream(ctx, req)
}

// Name implements domain.LLMProvider.
func (p *OllamaProvider) Name() string { return p.inner.Name() }

// ListModels returns the locally available Ollama models.
func (p *OllamaProvider) ListModels(ctx context.Context) ([]OllamaModel, error) {
	url := p.baseURL + "/api/tags"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(httpResp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error %d: %s", httpResp.StatusCode, string(body))
	}

	var resp struct {
		Models []OllamaModel `json:"models"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return resp.Models, nil
}

// IsHealthy checks if the Ollama server is reachable.
func (p *OllamaProvider) IsHealthy(ctx context.Context) bool {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/", nil)
	if err != nil {
		return false
	}

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return false
	}
	httpResp.Body.Close()

	return httpResp.StatusCode == http.StatusOK
}

// Warmup sends a lightweight request to pre-load the configured model.
// This prevents the first real request from incurring model load latency.
func (p *OllamaProvider) Warmup(ctx context.Context) error {
	if !p.IsHealthy(ctx) {
		return fmt.Errorf("ollama server not reachable at %s", p.baseURL)
	}

	p.logger.Info("warming up Ollama model", "model", p.inner.model, "base_url", p.baseURL)

	// Use the generate endpoint with keep_alive to load the model without generating.
	payload := fmt.Sprintf(`{"model":%q,"keep_alive":"5m"}`, p.inner.model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/generate",
		strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create warmup request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("warmup request: %w", err)
	}
	defer httpResp.Body.Close()
	io.Copy(io.Discard, httpResp.Body)

	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("warmup failed: status %d", httpResp.StatusCode)
	}

	p.logger.Info("Ollama model warmed up", "model", p.inner.model)
	return nil
}
