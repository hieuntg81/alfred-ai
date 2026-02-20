package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

func TestAnthropicChatStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("unexpected Accept: %s", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		events := []string{
			`data: {"type":"message_start"}`,
			`data: {"type":"content_block_start","content_block":{"type":"text"}}`,
			`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}`,
			`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":" world"}}`,
			`data: {"type":"message_delta","usage":{"input_tokens":5,"output_tokens":2}}`,
			`data: {"type":"message_stop"}`,
		}
		for _, e := range events {
			fmt.Fprintln(w, e)
			fmt.Fprintln(w)
			flusher.Flush()
		}
	}))
	defer server.Close()

	provider := NewAnthropicProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-20250514",
	}, newTestLogger())

	ch, err := provider.ChatStream(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "Hello"}},
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

	if content != "Hello world" {
		t.Errorf("content = %q, want %q", content, "Hello world")
	}
	if !gotDone {
		t.Error("expected Done=true")
	}
}

func TestAnthropicChatStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid key"}`))
	}))
	defer server.Close()

	provider := NewAnthropicProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "bad-key",
		Model:   "claude-sonnet-4-20250514",
	}, newTestLogger())

	_, err := provider.ChatStream(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "test"}},
	})
	if err == nil {
		t.Fatal("expected error from HTTP error")
	}
}

func TestAnthropicChatReadBodyError(t *testing.T) {
	provider := NewAnthropicProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: "http://localhost",
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-20250514",
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

func TestAnthropicRequestConversion(t *testing.T) {
	req := domain.ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: "You are helpful."},
			{Role: domain.RoleUser, Content: "Hello"},
		},
		MaxTokens: 1024,
	}

	antReq := toAnthropicRequest(req)

	if antReq.System != "You are helpful." {
		t.Errorf("System = %q, want %q", antReq.System, "You are helpful.")
	}
	if len(antReq.Messages) != 1 {
		t.Fatalf("Messages len = %d, want 1 (system extracted)", len(antReq.Messages))
	}
	if antReq.Messages[0].Role != "user" {
		t.Errorf("Message role = %q, want %q", antReq.Messages[0].Role, "user")
	}
	if antReq.MaxTokens != 1024 {
		t.Errorf("MaxTokens = %d, want 1024", antReq.MaxTokens)
	}
}

func TestAnthropicRequestWithToolCalls(t *testing.T) {
	req := domain.ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Read file"},
			{
				Role: domain.RoleAssistant,
				ToolCalls: []domain.ToolCall{
					{ID: "tc_1", Name: "filesystem", Arguments: json.RawMessage(`{"action":"read"}`)},
				},
			},
			{
				Role:    domain.RoleTool,
				Name:    "filesystem",
				Content: "file content",
				ToolCalls: []domain.ToolCall{
					{ID: "tc_1", Name: "filesystem"},
				},
			},
		},
		Tools: []domain.ToolSchema{
			{Name: "filesystem", Description: "File ops", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
	}

	antReq := toAnthropicRequest(req)

	if len(antReq.Tools) != 1 {
		t.Fatalf("Tools len = %d, want 1", len(antReq.Tools))
	}
	if antReq.Tools[0].Name != "filesystem" {
		t.Errorf("Tool name = %q", antReq.Tools[0].Name)
	}

	// Assistant message with tool calls should have tool_use content
	if len(antReq.Messages) < 2 {
		t.Fatalf("Messages len = %d, want at least 2", len(antReq.Messages))
	}
}

func TestAnthropicResponseConversion(t *testing.T) {
	resp := anthropicResponse{
		ID:    "msg_123",
		Model: "claude-sonnet-4-20250514",
		Content: []anthropicContent{
			{Type: "text", Text: "Hello there!"},
		},
		Usage: anthropicUsage{
			InputTokens:  10,
			OutputTokens: 5,
		},
	}

	result := fromAnthropicResponse(resp)

	if result.ID != "msg_123" {
		t.Errorf("ID = %q", result.ID)
	}
	if result.Message.Content != "Hello there!" {
		t.Errorf("Content = %q", result.Message.Content)
	}
	if result.Usage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d", result.Usage.PromptTokens)
	}
	if result.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d", result.Usage.TotalTokens)
	}
}

func TestAnthropicResponseWithToolUse(t *testing.T) {
	resp := anthropicResponse{
		ID:    "msg_456",
		Model: "claude-sonnet-4-20250514",
		Content: []anthropicContent{
			{Type: "text", Text: "Let me read that file."},
			{Type: "tool_use", ID: "toolu_1", Name: "filesystem", Input: json.RawMessage(`{"action":"read"}`)},
		},
		Usage: anthropicUsage{InputTokens: 20, OutputTokens: 15},
	}

	result := fromAnthropicResponse(resp)

	if result.Message.Content != "Let me read that file." {
		t.Errorf("Content = %q", result.Message.Content)
	}
	if len(result.Message.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(result.Message.ToolCalls))
	}
	if result.Message.ToolCalls[0].Name != "filesystem" {
		t.Errorf("ToolCall name = %q", result.Message.ToolCalls[0].Name)
	}
	if result.Message.ToolCalls[0].ID != "toolu_1" {
		t.Errorf("ToolCall ID = %q", result.Message.ToolCalls[0].ID)
	}
}

func TestAnthropicProviderChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("unexpected api key: %s", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != defaultAnthropicVersion {
			t.Errorf("unexpected version: %s", r.Header.Get("anthropic-version"))
		}

		resp := anthropicResponse{
			ID:    "msg_test",
			Model: "claude-sonnet-4-20250514",
			Content: []anthropicContent{
				{Type: "text", Text: "Test response"},
			},
			Usage: anthropicUsage{InputTokens: 5, OutputTokens: 3},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider(config.ProviderConfig{
		Name:    "anthropic-test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-20250514",
	}, newTestLogger())

	resp, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Message.Content != "Test response" {
		t.Errorf("Content = %q", resp.Message.Content)
	}
	if provider.Name() != "anthropic-test" {
		t.Errorf("Name = %q", provider.Name())
	}
}

func TestAnthropicProviderHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer server.Close()

	provider := NewAnthropicProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "bad-key",
		Model:   "claude-sonnet-4-20250514",
	}, newTestLogger())

	_, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "test"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, domain.ErrAuthInvalid) {
		t.Errorf("expected ErrAuthInvalid, got %v", err)
	}
}

func TestAnthropicProviderErrorResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    error
	}{
		{
			name:       "429 rate limit",
			statusCode: http.StatusTooManyRequests,
			body:       `{"error":{"type":"rate_limit_error","message":"rate limit exceeded"}}`,
			wantErr:    domain.ErrRateLimit,
		},
		{
			name:       "401 unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       `{"error":{"type":"authentication_error","message":"invalid x-api-key"}}`,
			wantErr:    domain.ErrAuthInvalid,
		},
		{
			name:       "403 forbidden",
			statusCode: http.StatusForbidden,
			body:       `{"error":{"type":"permission_error","message":"access denied"}}`,
			wantErr:    domain.ErrAuthInvalid,
		},
		{
			name:       "413 context overflow",
			statusCode: http.StatusRequestEntityTooLarge,
			body:       `{"error":{"type":"invalid_request_error","message":"prompt is too long"}}`,
			wantErr:    domain.ErrContextOverflow,
		},
		{
			name:       "500 server error",
			statusCode: http.StatusInternalServerError,
			body:       `{"error":{"type":"api_error","message":"internal server error"}}`,
			wantErr:    domain.ErrToolFailure,
		},
		{
			name:       "529 overloaded",
			statusCode: 529,
			body:       `{"error":{"type":"overloaded_error","message":"overloaded"}}`,
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

			provider := NewAnthropicProvider(config.ProviderConfig{
				Name:    "test",
				BaseURL: server.URL,
				APIKey:  "test-key",
				Model:   "claude-sonnet-4-20250514",
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
			// Error message should include status code for debugging.
			if !strings.Contains(err.Error(), fmt.Sprintf("API error %d", tt.statusCode)) {
				t.Errorf("error should contain status code, got: %s", err.Error())
			}
		})
	}
}

func TestAnthropicProviderStreamErrorResponses(t *testing.T) {
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
			body:       `{"error":"invalid key"}`,
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

			provider := NewAnthropicProvider(config.ProviderConfig{
				Name:    "test",
				BaseURL: server.URL,
				APIKey:  "test-key",
				Model:   "claude-sonnet-4-20250514",
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

func TestAnthropicDefaultMaxTokens(t *testing.T) {
	req := domain.ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Hello"},
		},
	}

	antReq := toAnthropicRequest(req)
	if antReq.MaxTokens != 4096 {
		t.Errorf("default MaxTokens = %d, want 4096", antReq.MaxTokens)
	}
}

func TestAnthropicChatToolUseResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := anthropicResponse{
			ID:    "msg_tool",
			Model: "claude-sonnet-4-20250514",
			Content: []anthropicContent{
				{Type: "text", Text: "Let me fetch that URL."},
				{Type: "tool_use", ID: "call_123", Name: "web_fetch", Input: json.RawMessage(`{"url":"http://example.com"}`)},
			},
			Usage: anthropicUsage{InputTokens: 15, OutputTokens: 10},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider(config.ProviderConfig{
		Name:    "anthropic-test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-20250514",
	}, newTestLogger())

	resp, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Fetch http://example.com"},
		},
		Tools: []domain.ToolSchema{
			{Name: "web_fetch", Description: "Fetch a URL", Parameters: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}}}`)},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Message.Content != "Let me fetch that URL." {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "Let me fetch that URL.")
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.Message.ToolCalls))
	}
	if resp.Message.ToolCalls[0].ID != "call_123" {
		t.Errorf("ToolCall ID = %q, want %q", resp.Message.ToolCalls[0].ID, "call_123")
	}
	if resp.Message.ToolCalls[0].Name != "web_fetch" {
		t.Errorf("ToolCall Name = %q, want %q", resp.Message.ToolCalls[0].Name, "web_fetch")
	}
	if resp.Usage.PromptTokens != 15 {
		t.Errorf("PromptTokens = %d, want 15", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 10 {
		t.Errorf("CompletionTokens = %d, want 10", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 25 {
		t.Errorf("TotalTokens = %d, want 25", resp.Usage.TotalTokens)
	}
}

func TestAnthropicChatInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{not valid json!!!`))
	}))
	defer server.Close()

	provider := NewAnthropicProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-20250514",
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

func TestAnthropicChatWithToolResultsInRequest(t *testing.T) {
	var receivedReq anthropicRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedReq)

		resp := anthropicResponse{
			ID:    "msg_result",
			Model: "claude-sonnet-4-20250514",
			Content: []anthropicContent{
				{Type: "text", Text: "The file contains: hello world"},
			},
			Usage: anthropicUsage{InputTokens: 30, OutputTokens: 12},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider(config.ProviderConfig{
		Name:    "anthropic-test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-20250514",
	}, newTestLogger())

	resp, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Read test.txt"},
			{
				Role: domain.RoleAssistant,
				ToolCalls: []domain.ToolCall{
					{ID: "toolu_abc", Name: "filesystem", Arguments: json.RawMessage(`{"path":"test.txt"}`)},
				},
			},
			{
				Role:    domain.RoleTool,
				Name:    "filesystem",
				Content: "hello world",
				ToolCalls: []domain.ToolCall{
					{ID: "toolu_abc"},
				},
			},
		},
		Tools: []domain.ToolSchema{
			{Name: "filesystem", Description: "File ops", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.Content != "The file contains: hello world" {
		t.Errorf("Content = %q", resp.Message.Content)
	}

	// Verify the request was properly converted
	if len(receivedReq.Messages) != 3 {
		t.Fatalf("Request messages len = %d, want 3", len(receivedReq.Messages))
	}
	// Tool result should be sent as "user" role with "tool_result" content type
	toolResultMsg := receivedReq.Messages[2]
	if toolResultMsg.Role != "user" {
		t.Errorf("Tool result message role = %q, want %q", toolResultMsg.Role, "user")
	}
	if len(toolResultMsg.Content) != 1 {
		t.Fatalf("Tool result content len = %d, want 1", len(toolResultMsg.Content))
	}
	if toolResultMsg.Content[0].Type != "tool_result" {
		t.Errorf("Tool result content type = %q, want %q", toolResultMsg.Content[0].Type, "tool_result")
	}
	if toolResultMsg.Content[0].ToolUseID != "toolu_abc" {
		t.Errorf("Tool result ToolUseID = %q, want %q", toolResultMsg.Content[0].ToolUseID, "toolu_abc")
	}
	if toolResultMsg.Content[0].Content != "hello world" {
		t.Errorf("Tool result Content = %q, want %q", toolResultMsg.Content[0].Content, "hello world")
	}

	// Verify tools were converted
	if len(receivedReq.Tools) != 1 {
		t.Fatalf("Request tools len = %d, want 1", len(receivedReq.Tools))
	}
	if receivedReq.Tools[0].Name != "filesystem" {
		t.Errorf("Tool name = %q, want %q", receivedReq.Tools[0].Name, "filesystem")
	}
}

func TestExtractToolCallID_WithToolCalls(t *testing.T) {
	m := domain.Message{
		Role:    domain.RoleTool,
		Content: "result",
		ToolCalls: []domain.ToolCall{
			{ID: "tc_abc123", Name: "my_tool"},
		},
	}
	got := extractToolCallID(m)
	if got != "tc_abc123" {
		t.Errorf("extractToolCallID = %q, want %q", got, "tc_abc123")
	}
}

func TestExtractToolCallID_Empty(t *testing.T) {
	m := domain.Message{
		Role:    domain.RoleTool,
		Content: "result",
	}
	got := extractToolCallID(m)
	if got != "" {
		t.Errorf("extractToolCallID = %q, want empty string", got)
	}
}

func TestAnthropicRequestAssistantWithContentAndToolCalls(t *testing.T) {
	req := domain.ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Hello"},
			{
				Role:    domain.RoleAssistant,
				Content: "I will read that file for you.",
				ToolCalls: []domain.ToolCall{
					{ID: "tc_1", Name: "filesystem", Arguments: json.RawMessage(`{"action":"read"}`)},
				},
			},
		},
	}

	antReq := toAnthropicRequest(req)

	if len(antReq.Messages) != 2 {
		t.Fatalf("Messages len = %d, want 2", len(antReq.Messages))
	}

	assistantMsg := antReq.Messages[1]
	if assistantMsg.Role != "assistant" {
		t.Errorf("Assistant message role = %q, want %q", assistantMsg.Role, "assistant")
	}
	// When content + tool calls, text should be prepended
	if len(assistantMsg.Content) != 2 {
		t.Fatalf("Assistant content blocks = %d, want 2 (text + tool_use)", len(assistantMsg.Content))
	}
	if assistantMsg.Content[0].Type != "text" {
		t.Errorf("First content type = %q, want %q", assistantMsg.Content[0].Type, "text")
	}
	if assistantMsg.Content[0].Text != "I will read that file for you." {
		t.Errorf("First content text = %q", assistantMsg.Content[0].Text)
	}
	if assistantMsg.Content[1].Type != "tool_use" {
		t.Errorf("Second content type = %q, want %q", assistantMsg.Content[1].Type, "tool_use")
	}
	if assistantMsg.Content[1].ID != "tc_1" {
		t.Errorf("Second content ID = %q, want %q", assistantMsg.Content[1].ID, "tc_1")
	}
}

func TestAnthropicRequestToolResultWithoutToolCallID(t *testing.T) {
	req := domain.ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Hello"},
			{
				Role:    domain.RoleTool,
				Name:    "filesystem",
				Content: "some result",
				// No ToolCalls - extractToolCallID should return ""
			},
		},
	}

	antReq := toAnthropicRequest(req)

	if len(antReq.Messages) != 2 {
		t.Fatalf("Messages len = %d, want 2", len(antReq.Messages))
	}

	toolResultMsg := antReq.Messages[1]
	if toolResultMsg.Role != "user" {
		t.Errorf("Tool result role = %q, want %q", toolResultMsg.Role, "user")
	}
	if len(toolResultMsg.Content) != 1 {
		t.Fatalf("Tool result content len = %d, want 1", len(toolResultMsg.Content))
	}
	if toolResultMsg.Content[0].ToolUseID != "" {
		t.Errorf("ToolUseID = %q, want empty string", toolResultMsg.Content[0].ToolUseID)
	}
	if toolResultMsg.Content[0].Content != "some result" {
		t.Errorf("Content = %q, want %q", toolResultMsg.Content[0].Content, "some result")
	}
}

func TestAnthropicChatDefaultModel(t *testing.T) {
	var receivedReq anthropicRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedReq)

		resp := anthropicResponse{
			ID:      "msg_dm",
			Model:   "claude-sonnet-4-20250514",
			Content: []anthropicContent{{Type: "text", Text: "ok"}},
			Usage:   anthropicUsage{InputTokens: 1, OutputTokens: 1},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-20250514",
	}, newTestLogger())

	// Send request with no model - should use provider's default
	_, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if receivedReq.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Request model = %q, want %q", receivedReq.Model, "claude-sonnet-4-20250514")
	}
}

func TestAnthropicChatContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		}
	}))
	defer server.Close()

	provider := NewAnthropicProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-20250514",
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

func TestAnthropicChatCreateRequestError(t *testing.T) {
	// A baseURL with a control character causes http.NewRequestWithContext to fail.
	provider := NewAnthropicProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: "http://invalid\x7f.host",
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-20250514",
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

// --- Extended Thinking Tests ---

func TestAnthropicRequestWithThinkingBudget(t *testing.T) {
	req := domain.ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Think carefully about this"},
		},
		ThinkingBudget: 10000,
		MaxTokens:      16000,
	}

	antReq := toAnthropicRequest(req)

	if antReq.Thinking == nil {
		t.Fatal("Thinking should be set when ThinkingBudget > 0")
	}
	if antReq.Thinking.Type != "enabled" {
		t.Errorf("Thinking.Type = %q, want %q", antReq.Thinking.Type, "enabled")
	}
	if antReq.Thinking.BudgetTokens != 10000 {
		t.Errorf("Thinking.BudgetTokens = %d, want 10000", antReq.Thinking.BudgetTokens)
	}
}

func TestAnthropicRequestWithoutThinkingBudget(t *testing.T) {
	req := domain.ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Hello"},
		},
	}

	antReq := toAnthropicRequest(req)

	if antReq.Thinking != nil {
		t.Error("Thinking should be nil when ThinkingBudget is 0")
	}
}

func TestAnthropicRequestThinkingNotInJSON(t *testing.T) {
	req := domain.ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Hello"},
		},
	}

	antReq := toAnthropicRequest(req)
	data, err := json.Marshal(antReq)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(data), "thinking") {
		t.Error("JSON should not contain 'thinking' field when budget is 0")
	}
}

func TestAnthropicRequestWithThinkingInHistory(t *testing.T) {
	req := domain.ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Think about this"},
			{
				Role:     domain.RoleAssistant,
				Content:  "Here is the answer.",
				Thinking: "Let me think step by step...",
			},
			{Role: domain.RoleUser, Content: "Now do something else"},
		},
		ThinkingBudget: 10000,
	}

	antReq := toAnthropicRequest(req)

	if len(antReq.Messages) != 3 {
		t.Fatalf("Messages len = %d, want 3", len(antReq.Messages))
	}

	assistantMsg := antReq.Messages[1]
	if len(assistantMsg.Content) != 2 {
		t.Fatalf("Assistant content blocks = %d, want 2 (thinking + text)", len(assistantMsg.Content))
	}
	if assistantMsg.Content[0].Type != "thinking" {
		t.Errorf("First content type = %q, want %q", assistantMsg.Content[0].Type, "thinking")
	}
	if assistantMsg.Content[0].Thinking != "Let me think step by step..." {
		t.Errorf("Thinking = %q", assistantMsg.Content[0].Thinking)
	}
	if assistantMsg.Content[1].Type != "text" {
		t.Errorf("Second content type = %q, want %q", assistantMsg.Content[1].Type, "text")
	}
	if assistantMsg.Content[1].Text != "Here is the answer." {
		t.Errorf("Text = %q", assistantMsg.Content[1].Text)
	}
}

func TestAnthropicResponseWithThinking(t *testing.T) {
	resp := anthropicResponse{
		ID:    "msg_thinking",
		Model: "claude-sonnet-4-20250514",
		Content: []anthropicContent{
			{Type: "thinking", Thinking: "I need to analyze this carefully..."},
			{Type: "text", Text: "Here is my answer."},
		},
		Usage: anthropicUsage{InputTokens: 20, OutputTokens: 50},
	}

	result := fromAnthropicResponse(resp)

	if result.Message.Thinking != "I need to analyze this carefully..." {
		t.Errorf("Thinking = %q", result.Message.Thinking)
	}
	if result.Message.Content != "Here is my answer." {
		t.Errorf("Content = %q", result.Message.Content)
	}
}

func TestAnthropicResponseThinkingWithToolUse(t *testing.T) {
	resp := anthropicResponse{
		ID:    "msg_think_tool",
		Model: "claude-sonnet-4-20250514",
		Content: []anthropicContent{
			{Type: "thinking", Thinking: "I should read the file first."},
			{Type: "text", Text: "Let me read that file."},
			{Type: "tool_use", ID: "toolu_1", Name: "filesystem", Input: json.RawMessage(`{"path":"test.txt"}`)},
		},
		Usage: anthropicUsage{InputTokens: 30, OutputTokens: 60},
	}

	result := fromAnthropicResponse(resp)

	if result.Message.Thinking != "I should read the file first." {
		t.Errorf("Thinking = %q", result.Message.Thinking)
	}
	if result.Message.Content != "Let me read that file." {
		t.Errorf("Content = %q", result.Message.Content)
	}
	if len(result.Message.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(result.Message.ToolCalls))
	}
	if result.Message.ToolCalls[0].Name != "filesystem" {
		t.Errorf("ToolCall name = %q", result.Message.ToolCalls[0].Name)
	}
}

func TestAnthropicChatWithThinking(t *testing.T) {
	var receivedReq anthropicRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedReq)

		resp := anthropicResponse{
			ID:    "msg_think_e2e",
			Model: "claude-sonnet-4-20250514",
			Content: []anthropicContent{
				{Type: "thinking", Thinking: "Deep analysis here..."},
				{Type: "text", Text: "The answer is 42."},
			},
			Usage: anthropicUsage{InputTokens: 15, OutputTokens: 100},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-20250514",
	}, newTestLogger())

	resp, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages:       []domain.Message{{Role: domain.RoleUser, Content: "What is the answer?"}},
		ThinkingBudget: 5000,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	// Verify request had thinking enabled
	if receivedReq.Thinking == nil {
		t.Fatal("request should have thinking enabled")
	}
	if receivedReq.Thinking.BudgetTokens != 5000 {
		t.Errorf("BudgetTokens = %d, want 5000", receivedReq.Thinking.BudgetTokens)
	}

	// Verify response parsed thinking
	if resp.Message.Thinking != "Deep analysis here..." {
		t.Errorf("Thinking = %q", resp.Message.Thinking)
	}
	if resp.Message.Content != "The answer is 42." {
		t.Errorf("Content = %q", resp.Message.Content)
	}
}

func TestAnthropicChatStreamWithThinking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		events := []string{
			`data: {"type":"message_start"}`,
			`data: {"type":"content_block_start","content_block":{"type":"thinking"}}`,
			`data: {"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"Let me "}}`,
			`data: {"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"think..."}}`,
			`data: {"type":"content_block_stop"}`,
			`data: {"type":"content_block_start","content_block":{"type":"text"}}`,
			`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"The answer"}}`,
			`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":" is 42."}}`,
			`data: {"type":"message_delta","usage":{"input_tokens":10,"output_tokens":50}}`,
			`data: {"type":"message_stop"}`,
		}
		for _, e := range events {
			fmt.Fprintln(w, e)
			fmt.Fprintln(w)
			flusher.Flush()
		}
	}))
	defer server.Close()

	provider := NewAnthropicProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-20250514",
	}, newTestLogger())

	ch, err := provider.ChatStream(context.Background(), domain.ChatRequest{
		Messages:       []domain.Message{{Role: domain.RoleUser, Content: "Think about this"}},
		ThinkingBudget: 10000,
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var thinking, content string
	var gotDone bool
	for delta := range ch {
		thinking += delta.Thinking
		content += delta.Content
		if delta.Done {
			gotDone = true
		}
	}

	if thinking != "Let me think..." {
		t.Errorf("thinking = %q, want %q", thinking, "Let me think...")
	}
	if content != "The answer is 42." {
		t.Errorf("content = %q, want %q", content, "The answer is 42.")
	}
	if !gotDone {
		t.Error("expected Done=true")
	}
}
