package embedding

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"alfred-ai/internal/domain"
)

func TestOllamaEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected content-type: %s", r.Header.Get("Content-Type"))
		}

		var req ollamaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.Model != "nomic-embed-text" {
			t.Errorf("model = %q, want nomic-embed-text", req.Model)
		}
		if len(req.Input) != 1 {
			t.Errorf("input len = %d, want 1", len(req.Input))
		}
		if req.Input[0] != "hello world" {
			t.Errorf("input[0] = %q, want hello world", req.Input[0])
		}

		resp := ollamaEmbedResponse{
			Embeddings: [][]float32{{0.1, 0.2, 0.3}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := NewOllamaProvider(WithOllamaBaseURL(server.URL))
	vecs, err := p.Embed(context.Background(), []string{"hello world"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 1 {
		t.Fatalf("vecs len = %d, want 1", len(vecs))
	}
	if len(vecs[0]) != 3 {
		t.Fatalf("vecs[0] len = %d, want 3", len(vecs[0]))
	}
	if vecs[0][0] != 0.1 {
		t.Errorf("vecs[0][0] = %f, want 0.1", vecs[0][0])
	}
	if vecs[0][1] != 0.2 {
		t.Errorf("vecs[0][1] = %f, want 0.2", vecs[0][1])
	}
	if vecs[0][2] != 0.3 {
		t.Errorf("vecs[0][2] = %f, want 0.3", vecs[0][2])
	}
}

func TestOllamaEmbed_BatchMultiple(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(req.Input) != 3 {
			t.Errorf("input len = %d, want 3", len(req.Input))
		}

		resp := ollamaEmbedResponse{
			Embeddings: [][]float32{
				{1.0, 2.0},
				{3.0, 4.0},
				{5.0, 6.0},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := NewOllamaProvider(WithOllamaBaseURL(server.URL))
	vecs, err := p.Embed(context.Background(), []string{"alpha", "beta", "gamma"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("vecs len = %d, want 3", len(vecs))
	}
	if vecs[0][0] != 1.0 || vecs[0][1] != 2.0 {
		t.Errorf("vecs[0] = %v, want [1.0 2.0]", vecs[0])
	}
	if vecs[1][0] != 3.0 || vecs[1][1] != 4.0 {
		t.Errorf("vecs[1] = %v, want [3.0 4.0]", vecs[1])
	}
	if vecs[2][0] != 5.0 || vecs[2][1] != 6.0 {
		t.Errorf("vecs[2] = %v, want [5.0 6.0]", vecs[2])
	}
}

func TestOllamaEmbed_EmptyInput(t *testing.T) {
	p := NewOllamaProvider()
	vecs, err := p.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if vecs != nil {
		t.Errorf("expected nil for empty input, got %v", vecs)
	}

	// Also test with an empty slice (not just nil).
	vecs, err = p.Embed(context.Background(), []string{})
	if err != nil {
		t.Fatalf("Embed with empty slice: %v", err)
	}
	if vecs != nil {
		t.Errorf("expected nil for empty slice, got %v", vecs)
	}
}

func TestOllamaEmbed_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal server error"}`))
	}))
	defer server.Close()

	p := NewOllamaProvider(WithOllamaBaseURL(server.URL))
	_, err := p.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, domain.ErrEmbeddingFailed) {
		t.Errorf("expected ErrEmbeddingFailed, got: %v", err)
	}
}

func TestOllamaEmbed_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{broken json!!!`))
	}))
	defer server.Close()

	p := NewOllamaProvider(WithOllamaBaseURL(server.URL))
	_, err := p.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, domain.ErrEmbeddingFailed) {
		t.Errorf("expected ErrEmbeddingFailed, got: %v", err)
	}
}

func TestOllamaEmbed_ContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	p := NewOllamaProvider(WithOllamaBaseURL(server.URL))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Embed(ctx, []string{"hello"})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestOllamaOptions(t *testing.T) {
	customClient := &http.Client{}
	p := NewOllamaProvider(
		WithOllamaModel("mxbai-embed-large"),
		WithOllamaDimensions(1024),
		WithOllamaBaseURL("http://custom.ollama:11434"),
		WithOllamaClient(customClient),
	)

	if p.model != "mxbai-embed-large" {
		t.Errorf("model = %q, want mxbai-embed-large", p.model)
	}
	if p.dims != 1024 {
		t.Errorf("dims = %d, want 1024", p.dims)
	}
	if p.baseURL != "http://custom.ollama:11434" {
		t.Errorf("baseURL = %q, want http://custom.ollama:11434", p.baseURL)
	}
	if p.client != customClient {
		t.Error("client was not set to custom client")
	}
}

func TestOllamaDimensions(t *testing.T) {
	// Default dimensions.
	p := NewOllamaProvider()
	if p.Dimensions() != 768 {
		t.Errorf("default Dimensions() = %d, want 768", p.Dimensions())
	}

	// Custom dimensions.
	p = NewOllamaProvider(WithOllamaDimensions(384))
	if p.Dimensions() != 384 {
		t.Errorf("custom Dimensions() = %d, want 384", p.Dimensions())
	}
}

func TestOllamaName(t *testing.T) {
	p := NewOllamaProvider()
	if p.Name() != "ollama" {
		t.Errorf("Name() = %q, want ollama", p.Name())
	}
}

// Compile-time interface check.
var _ domain.EmbeddingProvider = (*OllamaProvider)(nil)
