package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

func TestOpenRouterTransport(t *testing.T) {
	var capturedReq *http.Request
	inner := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		capturedReq = req
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       http.NoBody,
			Header:     make(http.Header),
		}, nil
	})

	transport := &openrouterTransport{base: inner}

	// Create original request with an existing header.
	origReq, _ := http.NewRequest("GET", "https://example.com", nil)
	origReq.Header.Set("Authorization", "Bearer test-key")

	_, err := transport.RoundTrip(origReq)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}

	// Verify headers were injected on the cloned request.
	if capturedReq.Header.Get("HTTP-Referer") != "https://github.com/byterover/alfred-ai" {
		t.Errorf("HTTP-Referer = %q", capturedReq.Header.Get("HTTP-Referer"))
	}
	if capturedReq.Header.Get("X-Title") != "alfred-ai" {
		t.Errorf("X-Title = %q", capturedReq.Header.Get("X-Title"))
	}
	// Existing headers should be preserved.
	if capturedReq.Header.Get("Authorization") != "Bearer test-key" {
		t.Errorf("Authorization = %q", capturedReq.Header.Get("Authorization"))
	}

	// Original request should NOT be mutated.
	if origReq.Header.Get("HTTP-Referer") != "" {
		t.Error("original request was mutated: HTTP-Referer set")
	}
	if origReq.Header.Get("X-Title") != "" {
		t.Error("original request was mutated: X-Title set")
	}
}

func TestOpenRouterProviderChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		// Verify OpenRouter-specific headers.
		if r.Header.Get("HTTP-Referer") != "https://github.com/byterover/alfred-ai" {
			t.Errorf("HTTP-Referer = %q", r.Header.Get("HTTP-Referer"))
		}
		if r.Header.Get("X-Title") != "alfred-ai" {
			t.Errorf("X-Title = %q", r.Header.Get("X-Title"))
		}
		// Verify standard headers.
		if r.Header.Get("Authorization") != "Bearer test-or-key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}

		resp := openaiResponse{
			ID:    "chatcmpl-or-123",
			Model: "openai/gpt-4o-mini",
			Choices: []openaiChoice{
				{
					Index: 0,
					Message: openaiMessage{
						Role:    "assistant",
						Content: "Hello from OpenRouter!",
					},
					FinishReason: "stop",
				},
			},
			Usage: openaiUsage{
				PromptTokens:     5,
				CompletionTokens: 4,
				TotalTokens:      9,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenRouterProvider(config.ProviderConfig{
		Name:    "openrouter-test",
		BaseURL: server.URL,
		APIKey:  "test-or-key",
		Model:   "openai/gpt-4o-mini",
	}, newTestLogger())

	resp, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Message.Content != "Hello from OpenRouter!" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "Hello from OpenRouter!")
	}
	if resp.Usage.TotalTokens != 9 {
		t.Errorf("TotalTokens = %d, want 9", resp.Usage.TotalTokens)
	}
}

func TestOpenRouterProviderChatStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify OpenRouter headers.
		if r.Header.Get("HTTP-Referer") != "https://github.com/byterover/alfred-ai" {
			t.Errorf("HTTP-Referer = %q", r.Header.Get("HTTP-Referer"))
		}
		if r.Header.Get("X-Title") != "alfred-ai" {
			t.Errorf("X-Title = %q", r.Header.Get("X-Title"))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		chunks := []string{
			`data: {"id":"c1","choices":[{"delta":{"content":"Hi"}}]}`,
			`data: {"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`,
			`data: [DONE]`,
		}
		for _, c := range chunks {
			fmt.Fprintln(w, c)
			fmt.Fprintln(w)
			flusher.Flush()
		}
	}))
	defer server.Close()

	provider := NewOpenRouterProvider(config.ProviderConfig{
		Name:    "openrouter-test",
		BaseURL: server.URL,
		APIKey:  "test-or-key",
		Model:   "openai/gpt-4o-mini",
	}, newTestLogger())

	ch, err := provider.ChatStream(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var content string
	var gotDone bool
	for delta := range ch {
		content += delta.Content
		if delta.Done {
			gotDone = true
		}
	}

	if content != "Hi" {
		t.Errorf("content = %q, want %q", content, "Hi")
	}
	if !gotDone {
		t.Error("expected Done=true")
	}
}

func TestOpenRouterProviderName(t *testing.T) {
	provider := NewOpenRouterProvider(config.ProviderConfig{
		Name:  "my-openrouter",
		Model: "openai/gpt-4o-mini",
	}, newTestLogger())

	if provider.Name() != "my-openrouter" {
		t.Errorf("Name() = %q, want %q", provider.Name(), "my-openrouter")
	}
}

func TestOpenRouterProviderDefaultBaseURL(t *testing.T) {
	provider := NewOpenRouterProvider(config.ProviderConfig{
		Name:  "test",
		Model: "openai/gpt-4o-mini",
		// No BaseURL - should default
	}, newTestLogger())

	if provider.inner.baseURL != "https://openrouter.ai/api/v1" {
		t.Errorf("baseURL = %q, want %q", provider.inner.baseURL, "https://openrouter.ai/api/v1")
	}
}
