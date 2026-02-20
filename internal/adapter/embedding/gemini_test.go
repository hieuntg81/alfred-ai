package embedding

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"alfred-ai/internal/domain"
)

func TestGeminiEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "batchEmbedContents") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("X-Goog-Api-Key") != "test-key" {
			t.Errorf("unexpected API key header: %s", r.Header.Get("X-Goog-Api-Key"))
		}
		if r.URL.Query().Get("key") != "" {
			t.Error("API key should not appear in URL query parameters")
		}

		var req geminiBatchEmbedRequest
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Requests) != 2 {
			t.Errorf("requests len = %d, want 2", len(req.Requests))
		}

		resp := geminiBatchEmbedResponse{
			Embeddings: []geminiEmbedValues{
				{Values: []float32{0.1, 0.2}},
				{Values: []float32{0.3, 0.4}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := NewGeminiProvider("test-key", WithGeminiBaseURL(server.URL))
	vecs, err := p.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("vecs len = %d, want 2", len(vecs))
	}
	if vecs[0][0] != 0.1 {
		t.Errorf("vecs[0][0] = %f, want 0.1", vecs[0][0])
	}
	if vecs[1][0] != 0.3 {
		t.Errorf("vecs[1][0] = %f, want 0.3", vecs[1][0])
	}
}

func TestGeminiEmbedHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer server.Close()

	p := NewGeminiProvider("key", WithGeminiBaseURL(server.URL))
	_, err := p.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, domain.ErrEmbeddingFailed) {
		t.Errorf("expected ErrEmbeddingFailed, got: %v", err)
	}
}

func TestGeminiEmbedEmptyInput(t *testing.T) {
	p := NewGeminiProvider("key")
	vecs, err := p.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if vecs != nil {
		t.Errorf("expected nil for empty input, got %v", vecs)
	}
}

func TestGeminiEmbedContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	p := NewGeminiProvider("key", WithGeminiBaseURL(server.URL))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Embed(ctx, []string{"hello"})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestGeminiOptions(t *testing.T) {
	p := NewGeminiProvider("key",
		WithGeminiModel("custom-model"),
		WithGeminiDimensions(256),
		WithGeminiBaseURL("http://custom.api"),
		WithGeminiClient(&http.Client{}),
	)
	if p.model != "custom-model" {
		t.Errorf("model = %q", p.model)
	}
	if p.dims != 256 {
		t.Errorf("dims = %d", p.dims)
	}
	if p.Dimensions() != 256 {
		t.Errorf("Dimensions() = %d", p.Dimensions())
	}
	if p.Name() != "gemini" {
		t.Errorf("Name() = %q", p.Name())
	}
}

// Compile-time interface check.
var _ domain.EmbeddingProvider = (*GeminiProvider)(nil)
