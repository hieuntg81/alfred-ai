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

var defaultGeminiHTTPClient = &http.Client{Timeout: 30 * time.Second}

// GeminiOption configures the Gemini embedding provider.
type GeminiOption func(*GeminiProvider)

// WithGeminiModel sets the embedding model.
func WithGeminiModel(model string) GeminiOption {
	return func(p *GeminiProvider) { p.model = model }
}

// WithGeminiDimensions sets the embedding dimensions.
func WithGeminiDimensions(dims int) GeminiOption {
	return func(p *GeminiProvider) { p.dims = dims }
}

// WithGeminiBaseURL sets a custom base URL.
func WithGeminiBaseURL(url string) GeminiOption {
	return func(p *GeminiProvider) { p.baseURL = url }
}

// WithGeminiClient sets a custom HTTP client.
func WithGeminiClient(client *http.Client) GeminiOption {
	return func(p *GeminiProvider) { p.client = client }
}

// GeminiProvider implements domain.EmbeddingProvider using the Google Gemini API.
type GeminiProvider struct {
	apiKey  string
	model   string
	dims    int
	baseURL string
	client  *http.Client
}

// NewGeminiProvider creates a Gemini embedding provider.
func NewGeminiProvider(apiKey string, opts ...GeminiOption) *GeminiProvider {
	p := &GeminiProvider{
		apiKey:  apiKey,
		model:   "text-embedding-004",
		dims:    768,
		baseURL: "https://generativelanguage.googleapis.com",
		client:  defaultGeminiHTTPClient,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// --- Gemini embeddings wire types ---

type geminiBatchEmbedRequest struct {
	Requests []geminiEmbedContentRequest `json:"requests"`
}

type geminiEmbedContentRequest struct {
	Model   string       `json:"model"`
	Content geminiECPart `json:"content"`
}

type geminiECPart struct {
	Parts []geminiTextPart `json:"parts"`
}

type geminiTextPart struct {
	Text string `json:"text"`
}

type geminiBatchEmbedResponse struct {
	Embeddings []geminiEmbedValues `json:"embeddings"`
}

type geminiEmbedValues struct {
	Values []float32 `json:"values"`
}

// Embed implements domain.EmbeddingProvider.
func (p *GeminiProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	requests := make([]geminiEmbedContentRequest, len(texts))
	for i, text := range texts {
		requests[i] = geminiEmbedContentRequest{
			Model: "models/" + p.model,
			Content: geminiECPart{
				Parts: []geminiTextPart{{Text: text}},
			},
		}
	}

	reqBody := geminiBatchEmbedRequest{Requests: requests}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal request: %v", domain.ErrEmbeddingFailed, err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:batchEmbedContents", p.baseURL, p.model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: create request: %v", domain.ErrEmbeddingFailed, err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Goog-Api-Key", p.apiKey)

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

	var gemResp geminiBatchEmbedResponse
	if err := json.Unmarshal(respBody, &gemResp); err != nil {
		return nil, fmt.Errorf("%w: unmarshal response: %v", domain.ErrEmbeddingFailed, err)
	}

	result := make([][]float32, len(gemResp.Embeddings))
	for i, e := range gemResp.Embeddings {
		result[i] = e.Values
	}

	return result, nil
}

// Dimensions implements domain.EmbeddingProvider.
func (p *GeminiProvider) Dimensions() int { return p.dims }

// Name implements domain.EmbeddingProvider.
func (p *GeminiProvider) Name() string { return "gemini" }
