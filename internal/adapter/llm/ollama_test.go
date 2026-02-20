package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

func TestOllamaProviderChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected content-type: %s", r.Header.Get("Content-Type"))
		}

		resp := openaiResponse{
			ID:    "chatcmpl-ollama-1",
			Model: "llama3",
			Choices: []openaiChoice{
				{
					Index: 0,
					Message: openaiMessage{
						Role:    "assistant",
						Content: "Hello from Ollama!",
					},
					FinishReason: "stop",
				},
			},
			Usage: openaiUsage{
				PromptTokens:     12,
				CompletionTokens: 5,
				TotalTokens:      17,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOllamaProvider(config.ProviderConfig{
		Name:    "ollama-test",
		BaseURL: server.URL,
		Model:   "llama3",
	}, newTestLogger())

	req := domain.ChatRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Hello"},
		},
	}

	resp, err := provider.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Message.Content != "Hello from Ollama!" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "Hello from Ollama!")
	}
	if resp.Usage.TotalTokens != 17 {
		t.Errorf("TotalTokens = %d, want 17", resp.Usage.TotalTokens)
	}
	if resp.Model != "llama3" {
		t.Errorf("Model = %q, want %q", resp.Model, "llama3")
	}
}

func TestOllamaProviderChat_NoAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify no Authorization header is sent (Ollama doesn't need API keys).
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("expected no Authorization header, got %q", auth)
		}

		resp := openaiResponse{
			ID:    "chatcmpl-ollama-nokey",
			Model: "llama3",
			Choices: []openaiChoice{
				{Message: openaiMessage{Role: "assistant", Content: "ok"}},
			},
			Usage: openaiUsage{TotalTokens: 5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOllamaProvider(config.ProviderConfig{
		Name:    "ollama-test",
		BaseURL: server.URL,
		Model:   "llama3",
		// No APIKey set - Ollama always sets apiKey to "" internally.
	}, newTestLogger())

	resp, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "ok")
	}
}

func TestOllamaProviderChat_DefaultBaseURL(t *testing.T) {
	// Create a provider with no BaseURL to verify the default.
	provider := NewOllamaProvider(config.ProviderConfig{
		Name:  "ollama-default",
		Model: "llama3",
		// No BaseURL
	}, newTestLogger())

	// The inner OpenAI provider should have baseURL set to the Ollama default + /v1.
	want := "http://localhost:11434/v1"
	if provider.inner.baseURL != want {
		t.Errorf("inner.baseURL = %q, want %q", provider.inner.baseURL, want)
	}

	// The native baseURL should be without /v1.
	wantBase := "http://localhost:11434"
	if provider.baseURL != wantBase {
		t.Errorf("baseURL = %q, want %q", provider.baseURL, wantBase)
	}
}

func TestOllamaProviderChat_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal server error"}`))
	}))
	defer server.Close()

	provider := NewOllamaProvider(config.ProviderConfig{
		Name:    "ollama-test",
		BaseURL: server.URL,
		Model:   "llama3",
	}, newTestLogger())

	_, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "test"}},
	})
	if err == nil {
		t.Fatal("expected error from 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want it to contain '500'", err.Error())
	}
}

func TestOllamaProviderChat_ContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until context is cancelled.
		<-r.Context().Done()
	}))
	defer server.Close()

	provider := NewOllamaProvider(config.ProviderConfig{
		Name:    "ollama-test",
		BaseURL: server.URL,
		Model:   "llama3",
	}, newTestLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := provider.Chat(ctx, domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "test"}},
	})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestOllamaProviderStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("unexpected Accept: %s", r.Header.Get("Accept"))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		chunks := []string{
			`data: {"id":"c1","choices":[{"delta":{"content":"Hello"}}]}`,
			`data: {"id":"c1","choices":[{"delta":{"content":" from"}}]}`,
			`data: {"id":"c1","choices":[{"delta":{"content":" Ollama"}}]}`,
			`data: {"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":3,"total_tokens":11}}`,
			`data: [DONE]`,
		}
		for _, c := range chunks {
			fmt.Fprintln(w, c)
			fmt.Fprintln(w)
			flusher.Flush()
		}
	}))
	defer server.Close()

	provider := NewOllamaProvider(config.ProviderConfig{
		Name:    "ollama-test",
		BaseURL: server.URL,
		Model:   "llama3",
	}, newTestLogger())

	ch, err := provider.ChatStream(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var content string
	var gotDone bool
	var usage *domain.Usage
	for delta := range ch {
		content += delta.Content
		if delta.Done {
			gotDone = true
		}
		if delta.Usage != nil {
			usage = delta.Usage
		}
	}

	if content != "Hello from Ollama" {
		t.Errorf("content = %q, want %q", content, "Hello from Ollama")
	}
	if !gotDone {
		t.Error("expected Done=true")
	}
	if usage == nil || usage.TotalTokens != 11 {
		t.Errorf("usage = %v, want TotalTokens=11", usage)
	}
}

func TestOllamaProviderName(t *testing.T) {
	provider := NewOllamaProvider(config.ProviderConfig{
		Name:  "my-ollama",
		Model: "llama3",
	}, newTestLogger())

	if got := provider.Name(); got != "my-ollama" {
		t.Errorf("Name() = %q, want %q", got, "my-ollama")
	}
}

func TestOllamaProviderListModels(t *testing.T) {
	now := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("unexpected path: %s, want /api/tags", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s, want GET", r.Method)
		}

		resp := struct {
			Models []OllamaModel `json:"models"`
		}{
			Models: []OllamaModel{
				{Name: "llama3:latest", ModifiedAt: now, Size: 4700000000},
				{Name: "mistral:7b", ModifiedAt: now.Add(-24 * time.Hour), Size: 3800000000},
				{Name: "codellama:13b", ModifiedAt: now.Add(-48 * time.Hour), Size: 7200000000},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOllamaProvider(config.ProviderConfig{
		Name:    "ollama-test",
		BaseURL: server.URL,
		Model:   "llama3",
	}, newTestLogger())

	models, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}

	if len(models) != 3 {
		t.Fatalf("len(models) = %d, want 3", len(models))
	}
	if models[0].Name != "llama3:latest" {
		t.Errorf("models[0].Name = %q, want %q", models[0].Name, "llama3:latest")
	}
	if models[0].Size != 4700000000 {
		t.Errorf("models[0].Size = %d, want 4700000000", models[0].Size)
	}
	if models[1].Name != "mistral:7b" {
		t.Errorf("models[1].Name = %q, want %q", models[1].Name, "mistral:7b")
	}
	if models[2].Name != "codellama:13b" {
		t.Errorf("models[2].Name = %q, want %q", models[2].Name, "codellama:13b")
	}
}

func TestOllamaProviderListModels_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`service unavailable`))
	}))
	defer server.Close()

	provider := NewOllamaProvider(config.ProviderConfig{
		Name:    "ollama-test",
		BaseURL: server.URL,
		Model:   "llama3",
	}, newTestLogger())

	_, err := provider.ListModels(context.Background())
	if err == nil {
		t.Fatal("expected error from 503 response")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error = %q, want it to contain '503'", err.Error())
	}
}

func TestOllamaProviderIsHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Errorf("unexpected path: %s, want /", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s, want GET", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Ollama is running"))
	}))
	defer server.Close()

	provider := NewOllamaProvider(config.ProviderConfig{
		Name:    "ollama-test",
		BaseURL: server.URL,
		Model:   "llama3",
	}, newTestLogger())

	if !provider.IsHealthy(context.Background()) {
		t.Error("IsHealthy() = false, want true")
	}
}

func TestOllamaProviderIsHealthy_Unreachable(t *testing.T) {
	// Use a server and close it immediately to simulate unreachable.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	serverURL := server.URL
	server.Close()

	provider := NewOllamaProvider(config.ProviderConfig{
		Name:    "ollama-test",
		BaseURL: serverURL,
		Model:   "llama3",
	}, newTestLogger())

	if provider.IsHealthy(context.Background()) {
		t.Error("IsHealthy() = true, want false for unreachable server")
	}
}

func TestOllamaProviderWarmup(t *testing.T) {
	var receivedPath string
	var receivedMethod string
	var receivedBody map[string]interface{}

	// Mux handles both the health check (GET /) and the warmup (POST /api/generate).
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/generate", func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedMethod = r.Method

		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		receivedBody = body

		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected content-type: %s", r.Header.Get("Content-Type"))
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	provider := NewOllamaProvider(config.ProviderConfig{
		Name:    "ollama-test",
		BaseURL: server.URL,
		Model:   "llama3",
	}, newTestLogger())

	err := provider.Warmup(context.Background())
	if err != nil {
		t.Fatalf("Warmup: %v", err)
	}

	if receivedPath != "/api/generate" {
		t.Errorf("path = %q, want /api/generate", receivedPath)
	}
	if receivedMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", receivedMethod)
	}
	if receivedBody == nil {
		t.Fatal("expected request body, got nil")
	}
	if model, ok := receivedBody["model"].(string); !ok || model != "llama3" {
		t.Errorf("body model = %v, want %q", receivedBody["model"], "llama3")
	}
	if keepAlive, ok := receivedBody["keep_alive"].(string); !ok || keepAlive != "5m" {
		t.Errorf("body keep_alive = %v, want %q", receivedBody["keep_alive"], "5m")
	}
}

func TestOllamaProviderWarmup_Unhealthy(t *testing.T) {
	// Use a closed server so IsHealthy returns false.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	serverURL := server.URL
	server.Close()

	provider := NewOllamaProvider(config.ProviderConfig{
		Name:    "ollama-test",
		BaseURL: serverURL,
		Model:   "llama3",
	}, newTestLogger())

	err := provider.Warmup(context.Background())
	if err == nil {
		t.Fatal("expected error when server is unhealthy")
	}
	if !strings.Contains(err.Error(), "not reachable") {
		t.Errorf("error = %q, want it to contain 'not reachable'", err.Error())
	}
}

func TestOllamaProviderDefaultTimeouts(t *testing.T) {
	// Create a provider with zero timeout config values so the Ollama
	// defaults are applied (5s connect, 300s response).
	provider := NewOllamaProvider(config.ProviderConfig{
		Name:  "ollama-timeout",
		Model: "llama3",
		// ConnTimeout and RespTimeout are zero.
	}, newTestLogger())

	// Verify the client's total timeout equals ConnTimeout + RespTimeout.
	// With Ollama defaults: 5s + 300s = 305s.
	wantTimeout := ollamaDefaultConnTimeout + ollamaDefaultRespTimeout
	if provider.client.Timeout != wantTimeout {
		t.Errorf("client.Timeout = %v, want %v", provider.client.Timeout, wantTimeout)
	}

	// Cross-check that the constants themselves hold the expected values.
	if ollamaDefaultConnTimeout != 5*time.Second {
		t.Errorf("ollamaDefaultConnTimeout = %v, want 5s", ollamaDefaultConnTimeout)
	}
	if ollamaDefaultRespTimeout != 300*time.Second {
		t.Errorf("ollamaDefaultRespTimeout = %v, want 300s", ollamaDefaultRespTimeout)
	}
}

func TestOllamaProviderDefaultTimeouts_CustomOverride(t *testing.T) {
	// When explicit timeouts are provided, they should be used instead of
	// the Ollama defaults.
	provider := NewOllamaProvider(config.ProviderConfig{
		Name:        "ollama-custom",
		Model:       "llama3",
		ConnTimeout: 10 * time.Second,
		RespTimeout: 60 * time.Second,
	}, newTestLogger())

	wantTimeout := 10*time.Second + 60*time.Second
	if provider.client.Timeout != wantTimeout {
		t.Errorf("client.Timeout = %v, want %v", provider.client.Timeout, wantTimeout)
	}
}

func TestOllamaProviderBaseURLTrailingSlash(t *testing.T) {
	// Ensure trailing slashes are handled correctly in the base URL.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := openaiResponse{
			ID:    "chatcmpl-slash",
			Model: "llama3",
			Choices: []openaiChoice{
				{Message: openaiMessage{Role: "assistant", Content: "ok"}},
			},
			Usage: openaiUsage{TotalTokens: 3},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOllamaProvider(config.ProviderConfig{
		Name:    "ollama-test",
		BaseURL: server.URL + "/", // trailing slash
		Model:   "llama3",
	}, newTestLogger())

	_, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "test"}},
	})
	if err != nil {
		t.Fatalf("Chat with trailing slash URL: %v", err)
	}
}

func TestOllamaProviderListModels_EmptyList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"models":[]}`))
	}))
	defer server.Close()

	provider := NewOllamaProvider(config.ProviderConfig{
		Name:    "ollama-test",
		BaseURL: server.URL,
		Model:   "llama3",
	}, newTestLogger())

	models, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("len(models) = %d, want 0", len(models))
	}
}

func TestOllamaProviderListModels_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{broken json!!!`))
	}))
	defer server.Close()

	provider := NewOllamaProvider(config.ProviderConfig{
		Name:    "ollama-test",
		BaseURL: server.URL,
		Model:   "llama3",
	}, newTestLogger())

	_, err := provider.ListModels(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
	if !strings.Contains(err.Error(), "unmarshal response") {
		t.Errorf("error = %q, want it to contain 'unmarshal response'", err.Error())
	}
}

func TestOllamaProviderIsHealthy_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	provider := NewOllamaProvider(config.ProviderConfig{
		Name:    "ollama-test",
		BaseURL: server.URL,
		Model:   "llama3",
	}, newTestLogger())

	if provider.IsHealthy(context.Background()) {
		t.Error("IsHealthy() = true, want false for 503 response")
	}
}

func TestOllamaProviderWarmup_GenerateEndpointError(t *testing.T) {
	// Server is healthy but the /api/generate endpoint returns an error.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/generate", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`model not found`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	provider := NewOllamaProvider(config.ProviderConfig{
		Name:    "ollama-test",
		BaseURL: server.URL,
		Model:   "nonexistent-model",
	}, newTestLogger())

	err := provider.Warmup(context.Background())
	if err == nil {
		t.Fatal("expected error when generate endpoint returns 500")
	}
	if !strings.Contains(err.Error(), "warmup failed") {
		t.Errorf("error = %q, want it to contain 'warmup failed'", err.Error())
	}
}

func TestOllamaProviderChatWithToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openaiResponse{
			ID:    "chatcmpl-ollama-tc",
			Model: "llama3",
			Choices: []openaiChoice{
				{
					Index: 0,
					Message: openaiMessage{
						Role: "assistant",
						ToolCalls: []openaiToolCall{
							{
								ID:   "call_ollama_1",
								Type: "function",
								Function: openaiToolCallFunction{
									Name:      "filesystem",
									Arguments: `{"action":"read","path":"test.txt"}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
			Usage: openaiUsage{TotalTokens: 20},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOllamaProvider(config.ProviderConfig{
		Name:    "ollama-test",
		BaseURL: server.URL,
		Model:   "llama3",
	}, newTestLogger())

	resp, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "read test.txt"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.Message.ToolCalls))
	}
	if resp.Message.ToolCalls[0].Name != "filesystem" {
		t.Errorf("tool name = %q, want %q", resp.Message.ToolCalls[0].Name, "filesystem")
	}
	if resp.Message.ToolCalls[0].ID != "call_ollama_1" {
		t.Errorf("tool call ID = %q, want %q", resp.Message.ToolCalls[0].ID, "call_ollama_1")
	}
}

func TestOllamaProviderInterfaceCompliance(t *testing.T) {
	// Verify at runtime that OllamaProvider satisfies both interfaces.
	provider := NewOllamaProvider(config.ProviderConfig{
		Name:  "ollama-interface",
		Model: "llama3",
	}, newTestLogger())

	var _ domain.LLMProvider = provider
	var _ domain.StreamingLLMProvider = provider
}

func TestOllamaProviderListModels_ContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	provider := NewOllamaProvider(config.ProviderConfig{
		Name:    "ollama-test",
		BaseURL: server.URL,
		Model:   "llama3",
	}, newTestLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := provider.ListModels(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestOllamaProviderStream_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer server.Close()

	provider := NewOllamaProvider(config.ProviderConfig{
		Name:    "ollama-test",
		BaseURL: server.URL,
		Model:   "llama3",
	}, newTestLogger())

	_, err := provider.ChatStream(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "test"}},
	})
	if err == nil {
		t.Fatal("expected error from HTTP error response")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error = %q, want it to contain '429'", err.Error())
	}
}

func TestOllamaProviderInnerAPIKeyAlwaysEmpty(t *testing.T) {
	// Even if the user provides an APIKey in the config, Ollama should
	// force it to empty since Ollama does not use API keys.
	provider := NewOllamaProvider(config.ProviderConfig{
		Name:   "ollama-test",
		Model:  "llama3",
		APIKey: "should-be-ignored",
	}, newTestLogger())

	if provider.inner.apiKey != "" {
		t.Errorf("inner.apiKey = %q, want empty string", provider.inner.apiKey)
	}
}
