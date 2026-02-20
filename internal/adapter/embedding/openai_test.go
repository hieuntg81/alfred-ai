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

func TestOpenAIEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth: %s", r.Header.Get("Authorization"))
		}

		var req openaiEmbedRequest
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Input) != 2 {
			t.Errorf("input len = %d, want 2", len(req.Input))
		}

		resp := openaiEmbedResponse{
			Data: []openaiEmbedData{
				{Index: 1, Embedding: []float32{0.4, 0.5, 0.6}},
				{Index: 0, Embedding: []float32{0.1, 0.2, 0.3}},
			},
			Usage: openaiEmbedUsage{PromptTokens: 10, TotalTokens: 10},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := NewOpenAIProvider("test-key", WithOpenAIBaseURL(server.URL))
	vecs, err := p.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("vecs len = %d, want 2", len(vecs))
	}
	// Should be reordered by index: [0] = {0.1,0.2,0.3}, [1] = {0.4,0.5,0.6}
	if vecs[0][0] != 0.1 {
		t.Errorf("vecs[0][0] = %f, want 0.1", vecs[0][0])
	}
	if vecs[1][0] != 0.4 {
		t.Errorf("vecs[1][0] = %f, want 0.4", vecs[1][0])
	}
}

func TestOpenAIEmbedEmptyInput(t *testing.T) {
	p := NewOpenAIProvider("key")
	vecs, err := p.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if vecs != nil {
		t.Errorf("expected nil for empty input, got %v", vecs)
	}
}

func TestOpenAIEmbedHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer server.Close()

	p := NewOpenAIProvider("key", WithOpenAIBaseURL(server.URL))
	_, err := p.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, domain.ErrEmbeddingFailed) {
		t.Errorf("expected ErrEmbeddingFailed, got: %v", err)
	}
}

func TestOpenAIEmbedInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{broken json!!!`))
	}))
	defer server.Close()

	p := NewOpenAIProvider("key", WithOpenAIBaseURL(server.URL))
	_, err := p.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, domain.ErrEmbeddingFailed) {
		t.Errorf("expected ErrEmbeddingFailed, got: %v", err)
	}
}

func TestOpenAIEmbedContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	p := NewOpenAIProvider("key", WithOpenAIBaseURL(server.URL))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Embed(ctx, []string{"hello"})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestOpenAIEmbedReorder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openaiEmbedResponse{
			Data: []openaiEmbedData{
				{Index: 2, Embedding: []float32{3.0}},
				{Index: 0, Embedding: []float32{1.0}},
				{Index: 1, Embedding: []float32{2.0}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := NewOpenAIProvider("key", WithOpenAIBaseURL(server.URL))
	vecs, err := p.Embed(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if vecs[0][0] != 1.0 || vecs[1][0] != 2.0 || vecs[2][0] != 3.0 {
		t.Errorf("unexpected order: %v", vecs)
	}
}

func TestOpenAIOptions(t *testing.T) {
	p := NewOpenAIProvider("key",
		WithOpenAIModel("text-embedding-3-large"),
		WithOpenAIDimensions(3072),
		WithOpenAIBaseURL("http://custom.api"),
		WithOpenAIClient(&http.Client{}),
	)
	if p.model != "text-embedding-3-large" {
		t.Errorf("model = %q, want text-embedding-3-large", p.model)
	}
	if p.dims != 3072 {
		t.Errorf("dims = %d, want 3072", p.dims)
	}
	if p.baseURL != "http://custom.api" {
		t.Errorf("baseURL = %q", p.baseURL)
	}
	if p.Dimensions() != 3072 {
		t.Errorf("Dimensions() = %d, want 3072", p.Dimensions())
	}
	if p.Name() != "openai" {
		t.Errorf("Name() = %q, want openai", p.Name())
	}
}

// Compile-time interface check.
var _ domain.EmbeddingProvider = (*OpenAIProvider)(nil)
