package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"alfred-ai/internal/domain"
)

var defaultHTTPClient = &http.Client{Timeout: 30 * time.Second}

// OpenAIOption configures the OpenAI embedding provider.
type OpenAIOption func(*OpenAIProvider)

// WithOpenAIModel sets the embedding model.
func WithOpenAIModel(model string) OpenAIOption {
	return func(p *OpenAIProvider) { p.model = model }
}

// WithOpenAIDimensions sets the embedding dimensions.
func WithOpenAIDimensions(dims int) OpenAIOption {
	return func(p *OpenAIProvider) { p.dims = dims }
}

// WithOpenAIBaseURL sets a custom base URL.
func WithOpenAIBaseURL(url string) OpenAIOption {
	return func(p *OpenAIProvider) { p.baseURL = url }
}

// WithOpenAIClient sets a custom HTTP client.
func WithOpenAIClient(client *http.Client) OpenAIOption {
	return func(p *OpenAIProvider) { p.client = client }
}

// OpenAIProvider implements domain.EmbeddingProvider using the OpenAI embeddings API.
type OpenAIProvider struct {
	apiKey  string
	model   string
	dims    int
	baseURL string
	client  *http.Client
}

// NewOpenAIProvider creates an OpenAI embedding provider.
func NewOpenAIProvider(apiKey string, opts ...OpenAIOption) *OpenAIProvider {
	p := &OpenAIProvider{
		apiKey:  apiKey,
		model:   "text-embedding-3-small",
		dims:    1536,
		baseURL: "https://api.openai.com/v1",
		client:  defaultHTTPClient,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// --- OpenAI embeddings wire types ---

type openaiEmbedRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

type openaiEmbedResponse struct {
	Data  []openaiEmbedData `json:"data"`
	Usage openaiEmbedUsage  `json:"usage"`
}

type openaiEmbedData struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

type openaiEmbedUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// Embed implements domain.EmbeddingProvider.
func (p *OpenAIProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody := openaiEmbedRequest{
		Input: texts,
		Model: p.model,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal request: %v", domain.ErrEmbeddingFailed, err)
	}

	url := p.baseURL + "/embeddings"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: create request: %v", domain.ErrEmbeddingFailed, err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

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

	var oaiResp openaiEmbedResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return nil, fmt.Errorf("%w: unmarshal response: %v", domain.ErrEmbeddingFailed, err)
	}

	// Sort by index to ensure correct ordering.
	sort.Slice(oaiResp.Data, func(i, j int) bool {
		return oaiResp.Data[i].Index < oaiResp.Data[j].Index
	})

	result := make([][]float32, len(oaiResp.Data))
	for i, d := range oaiResp.Data {
		result[i] = d.Embedding
	}

	return result, nil
}

// Dimensions implements domain.EmbeddingProvider.
func (p *OpenAIProvider) Dimensions() int { return p.dims }

// Name implements domain.EmbeddingProvider.
func (p *OpenAIProvider) Name() string { return "openai" }
