package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

// roundTripFunc is a function type that implements http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// errorReadCloser is an io.ReadCloser whose Read always returns an error.
type errorReadCloser struct{}

func (e *errorReadCloser) Read([]byte) (int, error) {
	return 0, fmt.Errorf("simulated body read error")
}

func (e *errorReadCloser) Close() error {
	return nil
}

func newTestLogger() *slog.Logger {
	return slog.Default()
}

func TestOpenAIProviderChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected content-type: %s", r.Header.Get("Content-Type"))
		}

		resp := openaiResponse{
			ID:    "chatcmpl-123",
			Model: "gpt-4o-mini",
			Choices: []openaiChoice{
				{
					Index: 0,
					Message: openaiMessage{
						Role:    "assistant",
						Content: "Hello! How can I help?",
					},
					FinishReason: "stop",
				},
			},
			Usage: openaiUsage{
				PromptTokens:     10,
				CompletionTokens: 8,
				TotalTokens:      18,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "gpt-4o-mini",
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

	if resp.Message.Content != "Hello! How can I help?" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "Hello! How can I help?")
	}
	if resp.Usage.TotalTokens != 18 {
		t.Errorf("TotalTokens = %d, want 18", resp.Usage.TotalTokens)
	}
}

func TestOpenAIProviderChatWithToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openaiResponse{
			ID:    "chatcmpl-456",
			Model: "gpt-4o-mini",
			Choices: []openaiChoice{
				{
					Index: 0,
					Message: openaiMessage{
						Role: "assistant",
						ToolCalls: []openaiToolCall{
							{
								ID:   "call_1",
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
			Usage: openaiUsage{TotalTokens: 25},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		Model:   "gpt-4o-mini",
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
}

func TestOpenAIProviderHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limit exceeded"}}`))
	}))
	defer server.Close()

	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		Model:   "gpt-4o-mini",
	}, newTestLogger())

	_, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "test"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, domain.ErrRateLimit) {
		t.Errorf("expected ErrRateLimit, got %v", err)
	}
}

func TestOpenAIProviderErrorResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    error
	}{
		{
			name:       "429 rate limit",
			statusCode: http.StatusTooManyRequests,
			body:       `{"error":{"message":"rate limit exceeded"}}`,
			wantErr:    domain.ErrRateLimit,
		},
		{
			name:       "401 unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       `{"error":{"message":"invalid api key"}}`,
			wantErr:    domain.ErrAuthInvalid,
		},
		{
			name:       "403 forbidden",
			statusCode: http.StatusForbidden,
			body:       `{"error":{"message":"access denied"}}`,
			wantErr:    domain.ErrAuthInvalid,
		},
		{
			name:       "413 context overflow",
			statusCode: http.StatusRequestEntityTooLarge,
			body:       `{"error":{"message":"maximum context length exceeded"}}`,
			wantErr:    domain.ErrContextOverflow,
		},
		{
			name:       "500 server error",
			statusCode: http.StatusInternalServerError,
			body:       `{"error":{"message":"internal server error"}}`,
			wantErr:    domain.ErrToolFailure,
		},
		{
			name:       "502 bad gateway",
			statusCode: http.StatusBadGateway,
			body:       `bad gateway`,
			wantErr:    domain.ErrToolFailure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()

			provider := NewOpenAIProvider(config.ProviderConfig{
				Name:    "test",
				BaseURL: server.URL,
				Model:   "gpt-4o-mini",
				APIKey:  "test-key",
			}, newTestLogger())

			_, err := provider.Chat(context.Background(), domain.ChatRequest{
				Messages: []domain.Message{{Role: domain.RoleUser, Content: "test"}},
			})
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("expected %v, got %v", tt.wantErr, err)
			}
			// Error message should include the response body for debugging.
			if !strings.Contains(err.Error(), fmt.Sprintf("API error %d", tt.statusCode)) {
				t.Errorf("error should contain status code, got: %s", err.Error())
			}
		})
	}
}

func TestOpenAIProviderStreamErrorResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    error
	}{
		{
			name:       "429 rate limit on stream",
			statusCode: http.StatusTooManyRequests,
			body:       `{"error":"rate limited"}`,
			wantErr:    domain.ErrRateLimit,
		},
		{
			name:       "401 unauthorized on stream",
			statusCode: http.StatusUnauthorized,
			body:       `{"error":"bad key"}`,
			wantErr:    domain.ErrAuthInvalid,
		},
		{
			name:       "500 server error on stream",
			statusCode: http.StatusInternalServerError,
			body:       `internal error`,
			wantErr:    domain.ErrToolFailure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()

			provider := NewOpenAIProvider(config.ProviderConfig{
				Name:    "test",
				BaseURL: server.URL,
				Model:   "gpt-4o-mini",
				APIKey:  "test-key",
			}, newTestLogger())

			_, err := provider.ChatStream(context.Background(), domain.ChatRequest{
				Messages: []domain.Message{{Role: domain.RoleUser, Content: "test"}},
			})
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestOpenAIProviderContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow response
		select {
		case <-r.Context().Done():
			return
		}
	}))
	defer server.Close()

	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		Model:   "gpt-4o-mini",
	}, newTestLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := provider.Chat(ctx, domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "test"}},
	})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestOpenAIChatInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{broken json!!!`))
	}))
	defer server.Close()

	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "gpt-4o-mini",
	}, newTestLogger())

	_, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "Hello"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
	if !strings.Contains(err.Error(), "unmarshal response") {
		t.Errorf("error = %q, want it to contain 'unmarshal response'", err.Error())
	}
}

func TestOpenAIRequestConversion(t *testing.T) {
	req := domain.ChatRequest{
		Model: "gpt-4o",
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: "You are a helper."},
			{Role: domain.RoleUser, Content: "Hello"},
		},
		MaxTokens:   2048,
		Temperature: 0.7,
	}

	oaiReq := toOpenAIRequest(req)

	if oaiReq.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", oaiReq.Model, "gpt-4o")
	}
	if len(oaiReq.Messages) != 2 {
		t.Fatalf("Messages len = %d, want 2", len(oaiReq.Messages))
	}
	if oaiReq.Messages[0].Role != "system" {
		t.Errorf("Messages[0].Role = %q, want %q", oaiReq.Messages[0].Role, "system")
	}
	if oaiReq.Messages[0].Content != "You are a helper." {
		t.Errorf("Messages[0].Content = %q", oaiReq.Messages[0].Content)
	}
	if oaiReq.MaxTokens != 2048 {
		t.Errorf("MaxTokens = %d, want 2048", oaiReq.MaxTokens)
	}
	if oaiReq.Temperature == nil || *oaiReq.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", oaiReq.Temperature)
	}
}

func TestOpenAIRequestWithToolCalls(t *testing.T) {
	req := domain.ChatRequest{
		Model: "gpt-4o",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Read a file"},
			{
				Role: domain.RoleAssistant,
				ToolCalls: []domain.ToolCall{
					{ID: "call_1", Name: "filesystem", Arguments: json.RawMessage(`{"path":"test.txt"}`)},
				},
			},
			{
				Role:    domain.RoleTool,
				Name:    "filesystem",
				Content: "file content here",
			},
		},
	}

	oaiReq := toOpenAIRequest(req)

	if len(oaiReq.Messages) != 3 {
		t.Fatalf("Messages len = %d, want 3", len(oaiReq.Messages))
	}

	// Check assistant message with tool calls
	assistantMsg := oaiReq.Messages[1]
	if assistantMsg.Role != "assistant" {
		t.Errorf("Assistant role = %q", assistantMsg.Role)
	}
	if len(assistantMsg.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(assistantMsg.ToolCalls))
	}
	if assistantMsg.ToolCalls[0].ID != "call_1" {
		t.Errorf("ToolCall ID = %q, want %q", assistantMsg.ToolCalls[0].ID, "call_1")
	}
	if assistantMsg.ToolCalls[0].Type != "function" {
		t.Errorf("ToolCall Type = %q, want %q", assistantMsg.ToolCalls[0].Type, "function")
	}
	if assistantMsg.ToolCalls[0].Function.Name != "filesystem" {
		t.Errorf("ToolCall Function.Name = %q, want %q", assistantMsg.ToolCalls[0].Function.Name, "filesystem")
	}
	if assistantMsg.ToolCalls[0].Function.Arguments != `{"path":"test.txt"}` {
		t.Errorf("ToolCall Function.Arguments = %q", assistantMsg.ToolCalls[0].Function.Arguments)
	}

	// Check tool result message
	toolMsg := oaiReq.Messages[2]
	if toolMsg.Role != "tool" {
		t.Errorf("Tool msg role = %q, want %q", toolMsg.Role, "tool")
	}
	if toolMsg.Content != "file content here" {
		t.Errorf("Tool msg content = %q", toolMsg.Content)
	}
}

func TestOpenAIRequestWithTools(t *testing.T) {
	req := domain.ChatRequest{
		Model: "gpt-4o",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Hello"},
		},
		Tools: []domain.ToolSchema{
			{
				Name:        "web_fetch",
				Description: "Fetch a URL",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}}}`),
			},
			{
				Name:        "filesystem",
				Description: "File operations",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			},
		},
	}

	oaiReq := toOpenAIRequest(req)

	if len(oaiReq.Tools) != 2 {
		t.Fatalf("Tools len = %d, want 2", len(oaiReq.Tools))
	}
	if oaiReq.Tools[0].Type != "function" {
		t.Errorf("Tools[0].Type = %q, want %q", oaiReq.Tools[0].Type, "function")
	}
	if oaiReq.Tools[0].Function.Name != "web_fetch" {
		t.Errorf("Tools[0].Function.Name = %q", oaiReq.Tools[0].Function.Name)
	}
	if oaiReq.Tools[0].Function.Description != "Fetch a URL" {
		t.Errorf("Tools[0].Function.Description = %q", oaiReq.Tools[0].Function.Description)
	}
	if oaiReq.Tools[1].Function.Name != "filesystem" {
		t.Errorf("Tools[1].Function.Name = %q", oaiReq.Tools[1].Function.Name)
	}
}

func TestOpenAIRequestNoMaxTokensNoTemp(t *testing.T) {
	req := domain.ChatRequest{
		Model: "gpt-4o",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Hello"},
		},
		// MaxTokens = 0, Temperature = 0
	}

	oaiReq := toOpenAIRequest(req)

	if oaiReq.MaxTokens != 0 {
		t.Errorf("MaxTokens = %d, want 0", oaiReq.MaxTokens)
	}
	if oaiReq.Temperature != nil {
		t.Errorf("Temperature = %v, want nil", oaiReq.Temperature)
	}
}

func TestOpenAIResponseConversion(t *testing.T) {
	resp := openaiResponse{
		ID:    "chatcmpl-test",
		Model: "gpt-4o",
		Choices: []openaiChoice{
			{
				Index: 0,
				Message: openaiMessage{
					Role:    "assistant",
					Content: "Hello there!",
					Name:    "bot",
				},
				FinishReason: "stop",
			},
		},
		Usage: openaiUsage{
			PromptTokens:     20,
			CompletionTokens: 10,
			TotalTokens:      30,
		},
		Created: 1700000000,
	}

	result := fromOpenAIResponse(resp)

	if result.ID != "chatcmpl-test" {
		t.Errorf("ID = %q", result.ID)
	}
	if result.Model != "gpt-4o" {
		t.Errorf("Model = %q", result.Model)
	}
	if result.Message.Role != "assistant" {
		t.Errorf("Role = %q", result.Message.Role)
	}
	if result.Message.Content != "Hello there!" {
		t.Errorf("Content = %q", result.Message.Content)
	}
	if result.Message.Name != "bot" {
		t.Errorf("Name = %q", result.Message.Name)
	}
	if result.Usage.PromptTokens != 20 {
		t.Errorf("PromptTokens = %d", result.Usage.PromptTokens)
	}
	if result.Usage.CompletionTokens != 10 {
		t.Errorf("CompletionTokens = %d", result.Usage.CompletionTokens)
	}
	if result.Usage.TotalTokens != 30 {
		t.Errorf("TotalTokens = %d", result.Usage.TotalTokens)
	}
}

func TestOpenAIResponseWithToolCalls(t *testing.T) {
	resp := openaiResponse{
		ID:    "chatcmpl-tc",
		Model: "gpt-4o",
		Choices: []openaiChoice{
			{
				Index: 0,
				Message: openaiMessage{
					Role: "assistant",
					ToolCalls: []openaiToolCall{
						{
							ID:   "call_abc",
							Type: "function",
							Function: openaiToolCallFunction{
								Name:      "web_fetch",
								Arguments: `{"url":"http://example.com"}`,
							},
						},
						{
							ID:   "call_def",
							Type: "function",
							Function: openaiToolCallFunction{
								Name:      "filesystem",
								Arguments: `{"path":"/tmp/test"}`,
							},
						},
					},
				},
				FinishReason: "tool_calls",
			},
		},
		Usage: openaiUsage{TotalTokens: 50},
	}

	result := fromOpenAIResponse(resp)

	if len(result.Message.ToolCalls) != 2 {
		t.Fatalf("ToolCalls len = %d, want 2", len(result.Message.ToolCalls))
	}
	if result.Message.ToolCalls[0].ID != "call_abc" {
		t.Errorf("ToolCalls[0].ID = %q", result.Message.ToolCalls[0].ID)
	}
	if result.Message.ToolCalls[0].Name != "web_fetch" {
		t.Errorf("ToolCalls[0].Name = %q", result.Message.ToolCalls[0].Name)
	}
	if string(result.Message.ToolCalls[0].Arguments) != `{"url":"http://example.com"}` {
		t.Errorf("ToolCalls[0].Arguments = %s", result.Message.ToolCalls[0].Arguments)
	}
	if result.Message.ToolCalls[1].ID != "call_def" {
		t.Errorf("ToolCalls[1].ID = %q", result.Message.ToolCalls[1].ID)
	}
	if result.Message.ToolCalls[1].Name != "filesystem" {
		t.Errorf("ToolCalls[1].Name = %q", result.Message.ToolCalls[1].Name)
	}
}

func TestOpenAIResponseEmptyChoices(t *testing.T) {
	resp := openaiResponse{
		ID:      "chatcmpl-empty",
		Model:   "gpt-4o",
		Choices: []openaiChoice{},
		Usage:   openaiUsage{TotalTokens: 5},
	}

	result := fromOpenAIResponse(resp)

	// With empty choices, message should be zero-value
	if result.Message.Content != "" {
		t.Errorf("Content = %q, want empty", result.Message.Content)
	}
	if len(result.Message.ToolCalls) != 0 {
		t.Errorf("ToolCalls len = %d, want 0", len(result.Message.ToolCalls))
	}
}

func TestOpenAIChatWithToolsInRequest(t *testing.T) {
	var receivedReq openaiRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedReq)

		resp := openaiResponse{
			ID:    "chatcmpl-tools",
			Model: "gpt-4o",
			Choices: []openaiChoice{
				{
					Message: openaiMessage{
						Role: "assistant",
						ToolCalls: []openaiToolCall{
							{
								ID:   "call_999",
								Type: "function",
								Function: openaiToolCallFunction{
									Name:      "web_fetch",
									Arguments: `{"url":"http://example.com"}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
			Usage: openaiUsage{PromptTokens: 20, CompletionTokens: 15, TotalTokens: 35},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "gpt-4o",
	}, newTestLogger())

	resp, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Fetch example.com"},
		},
		Tools: []domain.ToolSchema{
			{
				Name:        "web_fetch",
				Description: "Fetch a URL",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}}}`),
			},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.Message.ToolCalls))
	}
	if resp.Message.ToolCalls[0].Name != "web_fetch" {
		t.Errorf("ToolCall name = %q", resp.Message.ToolCalls[0].Name)
	}

	// Verify tools were sent in the request
	if len(receivedReq.Tools) != 1 {
		t.Fatalf("Request tools len = %d, want 1", len(receivedReq.Tools))
	}
	if receivedReq.Tools[0].Type != "function" {
		t.Errorf("Tool type = %q, want %q", receivedReq.Tools[0].Type, "function")
	}
	if receivedReq.Tools[0].Function.Name != "web_fetch" {
		t.Errorf("Tool name = %q", receivedReq.Tools[0].Function.Name)
	}
}

func TestOpenAIChatWithToolResultsInRequest(t *testing.T) {
	var receivedReq openaiRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedReq)

		resp := openaiResponse{
			ID:    "chatcmpl-result",
			Model: "gpt-4o",
			Choices: []openaiChoice{
				{
					Message: openaiMessage{
						Role:    "assistant",
						Content: "The file says hello world",
					},
				},
			},
			Usage: openaiUsage{TotalTokens: 30},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "gpt-4o",
	}, newTestLogger())

	resp, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Read test.txt"},
			{
				Role: domain.RoleAssistant,
				ToolCalls: []domain.ToolCall{
					{ID: "call_read1", Name: "filesystem", Arguments: json.RawMessage(`{"path":"test.txt"}`)},
				},
			},
			{
				Role:    domain.RoleTool,
				Name:    "filesystem",
				Content: "hello world",
			},
		},
		Tools: []domain.ToolSchema{
			{Name: "filesystem", Description: "File ops", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Message.Content != "The file says hello world" {
		t.Errorf("Content = %q", resp.Message.Content)
	}

	// Verify request messages were converted properly
	if len(receivedReq.Messages) != 3 {
		t.Fatalf("Request messages len = %d, want 3", len(receivedReq.Messages))
	}

	// Assistant message should have tool_calls
	if len(receivedReq.Messages[1].ToolCalls) != 1 {
		t.Fatalf("Assistant tool_calls len = %d, want 1", len(receivedReq.Messages[1].ToolCalls))
	}

	// Tool result message
	if receivedReq.Messages[2].Role != "tool" {
		t.Errorf("Tool msg role = %q, want %q", receivedReq.Messages[2].Role, "tool")
	}
	if receivedReq.Messages[2].Content != "hello world" {
		t.Errorf("Tool msg content = %q", receivedReq.Messages[2].Content)
	}
}

func TestOpenAIChatDefaultModel(t *testing.T) {
	var receivedReq openaiRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedReq)

		resp := openaiResponse{
			ID:    "chatcmpl-dm",
			Model: "gpt-4o",
			Choices: []openaiChoice{
				{Message: openaiMessage{Role: "assistant", Content: "ok"}},
			},
			Usage: openaiUsage{TotalTokens: 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "gpt-4o",
	}, newTestLogger())

	// Request with no model - should use provider's default
	_, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if receivedReq.Model != "gpt-4o" {
		t.Errorf("Request model = %q, want %q", receivedReq.Model, "gpt-4o")
	}
}

func TestOpenAIProviderNoAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify no Authorization header when apiKey is empty
		if r.Header.Get("Authorization") != "" {
			t.Errorf("expected no Authorization header, got %q", r.Header.Get("Authorization"))
		}
		resp := openaiResponse{
			ID:    "chatcmpl-nokey",
			Model: "local-model",
			Choices: []openaiChoice{
				{Message: openaiMessage{Role: "assistant", Content: "ok"}},
			},
			Usage: openaiUsage{TotalTokens: 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "local",
		BaseURL: server.URL,
		Model:   "local-model",
		// No APIKey
	}, newTestLogger())

	resp, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Errorf("Content = %q", resp.Message.Content)
	}
}

func TestOpenAIRequestStreamFlag(t *testing.T) {
	req := domain.ChatRequest{
		Model: "gpt-4o",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Hello"},
		},
		Stream: true,
	}

	oaiReq := toOpenAIRequest(req)

	if !oaiReq.Stream {
		t.Error("Stream = false, want true")
	}
}

func TestOpenAIChatCreateRequestError(t *testing.T) {
	// A baseURL with a control character causes http.NewRequestWithContext to fail.
	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: "http://invalid\x7f.host",
		APIKey:  "test-key",
		Model:   "gpt-4o",
	}, newTestLogger())

	_, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "test"}},
	})
	if err == nil {
		t.Fatal("expected error from invalid URL")
	}
	if !strings.Contains(err.Error(), "create request") {
		t.Errorf("error = %q, want it to contain 'create request'", err.Error())
	}
}

func TestOpenAIChatStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("unexpected Accept: %s", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		chunks := []string{
			`data: {"id":"c1","choices":[{"delta":{"content":"Hello"}}]}`,
			`data: {"id":"c1","choices":[{"delta":{"content":" world"}}]}`,
			`data: {"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`,
			`data: [DONE]`,
		}
		for _, c := range chunks {
			fmt.Fprintln(w, c)
			fmt.Fprintln(w)
			flusher.Flush()
		}
	}))
	defer server.Close()

	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "gpt-4o-mini",
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

	if content != "Hello world" {
		t.Errorf("content = %q, want %q", content, "Hello world")
	}
	if !gotDone {
		t.Error("expected Done=true")
	}
	if usage == nil || usage.TotalTokens != 7 {
		t.Errorf("usage = %v, want TotalTokens=7", usage)
	}
}

func TestOpenAIChatStreamContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for i := 0; i < 1000; i++ {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			fmt.Fprintf(w, "data: {\"id\":\"c1\",\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
			flusher.Flush()
		}
	}))
	defer server.Close()

	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "gpt-4o-mini",
	}, newTestLogger())

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := provider.ChatStream(ctx, domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	// Read a few then cancel
	<-ch
	cancel()

	// Drain the rest — channel should close
	count := 0
	for range ch {
		count++
	}
	if count > 100 {
		t.Errorf("got %d deltas after cancel, expected much fewer", count)
	}
}

func TestOpenAIChatStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer server.Close()

	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "gpt-4o-mini",
	}, newTestLogger())

	_, err := provider.ChatStream(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "test"}},
	})
	if err == nil {
		t.Fatal("expected error from HTTP error")
	}
}

func TestOpenAIChatReadBodyError(t *testing.T) {
	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: "http://localhost",
		APIKey:  "test-key",
		Model:   "gpt-4o",
	}, newTestLogger())

	// Replace the client's transport to return a response with a broken body.
	provider.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       &errorReadCloser{},
				Header:     make(http.Header),
			}, nil
		}),
	}

	_, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "test"}},
	})
	if err == nil {
		t.Fatal("expected error from body read failure")
	}
	if !strings.Contains(err.Error(), "read response") {
		t.Errorf("error = %q, want it to contain 'read response'", err.Error())
	}
}

func TestRegistryBasic(t *testing.T) {
	reg := NewRegistry()

	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:  "test-provider",
		Model: "test-model",
	}, newTestLogger())

	if err := reg.Register(provider); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := reg.Get("test-provider")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name() != "test-provider" {
		t.Errorf("Name = %q, want %q", got.Name(), "test-provider")
	}

	names := reg.List()
	if len(names) != 1 || names[0] != "test-provider" {
		t.Errorf("List = %v, want [test-provider]", names)
	}
}

func TestRegistryDuplicate(t *testing.T) {
	reg := NewRegistry()
	p := NewOpenAIProvider(config.ProviderConfig{Name: "dup"}, newTestLogger())

	if err := reg.Register(p); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(p); err == nil {
		t.Error("expected error on duplicate register")
	}
}

func TestRegistryNotFound(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.Get("nonexistent")
	if !errors.Is(err, domain.ErrProviderNotFound) {
		t.Errorf("expected ErrProviderNotFound, got %v", err)
	}
}
