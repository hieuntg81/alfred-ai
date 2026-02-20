package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"alfred-ai/internal/domain"
)

// OllamaOption configures the Ollama embedding provider.
type OllamaOption func(*OllamaProvider)

// WithOllamaModel sets the embedding model.
func WithOllamaModel(model string) OllamaOption {
	return func(p *OllamaProvider) { p.model = model }
}

// WithOllamaDimensions sets the embedding dimensions.
func WithOllamaDimensions(dims int) OllamaOption {
	return func(p *OllamaProvider) { p.dims = dims }
}

// WithOllamaBaseURL sets a custom base URL.
func WithOllamaBaseURL(url string) OllamaOption {
	return func(p *OllamaProvider) { p.baseURL = url }
}

// WithOllamaClient sets a custom HTTP client.
func WithOllamaClient(client *http.Client) OllamaOption {
	return func(p *OllamaProvider) { p.client = client }
}

// OllamaProvider implements domain.EmbeddingProvider using the Ollama embedding API.
type OllamaProvider struct {
	model   string
	dims    int
	baseURL string
	client  *http.Client
}

// NewOllamaProvider creates an Ollama embedding provider.
// The baseURL defaults to http://localhost:11434.
func NewOllamaProvider(opts ...OllamaOption) *OllamaProvider {
	p := &OllamaProvider{
		model:   "nomic-embed-text",
		dims:    768,
		baseURL: "http://localhost:11434",
		client:  &http.Client{Timeout: 60 * time.Second},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// --- Ollama embeddings wire types ---

type ollamaEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed implements domain.EmbeddingProvider.
func (p *OllamaProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody := ollamaEmbedRequest{
		Model: p.model,
		Input: texts,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal request: %v", domain.ErrEmbeddingFailed, err)
	}

	url := p.baseURL + "/api/embed"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: create request: %v", domain.ErrEmbeddingFailed, err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: http request: %v", domain.ErrEmbeddingFailed, err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("%w: read response: %v", domain.ErrEmbeddingFailed, err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: API error %d: %s", domain.ErrEmbeddingFailed, httpResp.StatusCode, string(respBody))
	}

	var ollamaResp ollamaEmbedResponse
	if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
		return nil, fmt.Errorf("%w: unmarshal response: %v", domain.ErrEmbeddingFailed, err)
	}

	return ollamaResp.Embeddings, nil
}

// Dimensions implements domain.EmbeddingProvider.
func (p *OllamaProvider) Dimensions() int { return p.dims }

// Name implements domain.EmbeddingProvider.
func (p *OllamaProvider) Name() string { return "ollama" }

// Compile-time interface check.
var _ domain.EmbeddingProvider = (*OllamaProvider)(nil)
