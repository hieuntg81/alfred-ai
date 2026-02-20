package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

func TestGeminiChatStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "streamGenerateContent") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("alt") != "sse" {
			t.Errorf("expected alt=sse, got %s", r.URL.Query().Get("alt"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		chunks := []string{
			`data: {"candidates":[{"content":{"parts":[{"text":"Hello"}]}}]}`,
			`data: {"candidates":[{"content":{"parts":[{"text":" world"}]}}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"totalTokenCount":7}}`,
		}
		for _, c := range chunks {
			fmt.Fprintln(w, c)
			fmt.Fprintln(w)
			flusher.Flush()
		}
	}))
	defer server.Close()

	provider := NewGeminiProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "gemini-pro",
	}, newTestLogger())

	ch, err := provider.ChatStream(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var content string
	var usage *domain.Usage
	for delta := range ch {
		content += delta.Content
		if delta.Usage != nil {
			usage = delta.Usage
		}
	}

	if content != "Hello world" {
		t.Errorf("content = %q, want %q", content, "Hello world")
	}
	if usage == nil || usage.TotalTokens != 7 {
		t.Errorf("usage = %v, want TotalTokens=7", usage)
	}
}

func TestGeminiChatStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer server.Close()

	provider := NewGeminiProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "bad-key",
		Model:   "gemini-pro",
	}, newTestLogger())

	_, err := provider.ChatStream(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "test"}},
	})
	if err == nil {
		t.Fatal("expected error from HTTP error")
	}
}

func TestGeminiChatReadBodyError(t *testing.T) {
	provider := NewGeminiProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: "http://localhost",
		APIKey:  "test-key",
		Model:   "gemini-pro",
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

func TestGeminiRequestConversion(t *testing.T) {
	req := domain.ChatRequest{
		Model: "gemini-pro",
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: "You are helpful."},
			{Role: domain.RoleUser, Content: "Hello"},
			{Role: domain.RoleAssistant, Content: "Hi there!"},
		},
	}

	gemReq := toGeminiRequest(req)

	if gemReq.SystemInstruction == nil {
		t.Fatal("SystemInstruction is nil")
	}
	if gemReq.SystemInstruction.Parts[0].Text != "You are helpful." {
		t.Errorf("SystemInstruction = %q", gemReq.SystemInstruction.Parts[0].Text)
	}

	if len(gemReq.Contents) != 2 {
		t.Fatalf("Contents len = %d, want 2 (system extracted)", len(gemReq.Contents))
	}

	if gemReq.Contents[0].Role != "user" {
		t.Errorf("Content[0].Role = %q, want user", gemReq.Contents[0].Role)
	}
	if gemReq.Contents[1].Role != "model" {
		t.Errorf("Content[1].Role = %q, want model", gemReq.Contents[1].Role)
	}
}

func TestGeminiRequestWithTools(t *testing.T) {
	req := domain.ChatRequest{
		Model: "gemini-pro",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Read a file"},
		},
		Tools: []domain.ToolSchema{
			{Name: "filesystem", Description: "File ops", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
	}

	gemReq := toGeminiRequest(req)

	if len(gemReq.Tools) != 1 {
		t.Fatalf("Tools len = %d, want 1", len(gemReq.Tools))
	}
	if len(gemReq.Tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("FunctionDeclarations len = %d", len(gemReq.Tools[0].FunctionDeclarations))
	}
	if gemReq.Tools[0].FunctionDeclarations[0].Name != "filesystem" {
		t.Errorf("Tool name = %q", gemReq.Tools[0].FunctionDeclarations[0].Name)
	}
}

func TestGeminiResponseConversion(t *testing.T) {
	resp := geminiResponse{
		Candidates: []geminiCandidate{
			{
				Content: geminiContent{
					Role:  "model",
					Parts: []geminiPart{{Text: "Hello!"}},
				},
			},
		},
		UsageMetadata: &geminiUsage{
			PromptTokenCount:     10,
			CandidatesTokenCount: 5,
			TotalTokenCount:      15,
		},
	}

	result := fromGeminiResponse(resp)

	if result.Message.Content != "Hello!" {
		t.Errorf("Content = %q", result.Message.Content)
	}
	if result.Usage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d", result.Usage.PromptTokens)
	}
	if result.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d", result.Usage.TotalTokens)
	}
}

func TestGeminiResponseWithToolCall(t *testing.T) {
	resp := geminiResponse{
		Candidates: []geminiCandidate{
			{
				Content: geminiContent{
					Role: "model",
					Parts: []geminiPart{
						{
							FunctionCall: &geminiFunctionCall{
								Name: "filesystem",
								Args: json.RawMessage(`{"action":"read","path":"test.txt"}`),
							},
						},
					},
				},
			},
		},
		UsageMetadata: &geminiUsage{TotalTokenCount: 20},
	}

	result := fromGeminiResponse(resp)

	if len(result.Message.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(result.Message.ToolCalls))
	}
	if result.Message.ToolCalls[0].Name != "filesystem" {
		t.Errorf("ToolCall name = %q", result.Message.ToolCalls[0].Name)
	}
}

func TestGeminiProviderChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "generateContent") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "test-key" {
			t.Errorf("unexpected api key: %s", r.URL.Query().Get("key"))
		}

		resp := geminiResponse{
			Candidates: []geminiCandidate{
				{
					Content: geminiContent{
						Role:  "model",
						Parts: []geminiPart{{Text: "Gemini response"}},
					},
				},
			},
			UsageMetadata: &geminiUsage{
				PromptTokenCount:     8,
				CandidatesTokenCount: 4,
				TotalTokenCount:      12,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewGeminiProvider(config.ProviderConfig{
		Name:    "gemini-test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "gemini-pro",
	}, newTestLogger())

	resp, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Message.Content != "Gemini response" {
		t.Errorf("Content = %q", resp.Message.Content)
	}
	if provider.Name() != "gemini-test" {
		t.Errorf("Name = %q", provider.Name())
	}
}

func TestGeminiProviderHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"message":"forbidden"}}`))
	}))
	defer server.Close()

	provider := NewGeminiProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "bad-key",
		Model:   "gemini-pro",
	}, newTestLogger())

	_, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "test"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGeminiResponseNoUsage(t *testing.T) {
	resp := geminiResponse{
		Candidates: []geminiCandidate{
			{Content: geminiContent{Parts: []geminiPart{{Text: "Hi"}}}},
		},
	}

	result := fromGeminiResponse(resp)
	if result.Usage.TotalTokens != 0 {
		t.Errorf("TotalTokens = %d, want 0", result.Usage.TotalTokens)
	}
}

func TestGeminiChatToolUseResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := geminiResponse{
			Candidates: []geminiCandidate{
				{
					Content: geminiContent{
						Role: "model",
						Parts: []geminiPart{
							{
								FunctionCall: &geminiFunctionCall{
									Name: "web_fetch",
									Args: json.RawMessage(`{"url":"http://example.com"}`),
								},
							},
						},
					},
				},
			},
			UsageMetadata: &geminiUsage{
				PromptTokenCount:     12,
				CandidatesTokenCount: 8,
				TotalTokenCount:      20,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewGeminiProvider(config.ProviderConfig{
		Name:    "gemini-test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "gemini-pro",
	}, newTestLogger())

	resp, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Fetch example.com"},
		},
		Tools: []domain.ToolSchema{
			{Name: "web_fetch", Description: "Fetch a URL", Parameters: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}}}`)},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.Message.ToolCalls))
	}
	if resp.Message.ToolCalls[0].Name != "web_fetch" {
		t.Errorf("ToolCall name = %q, want %q", resp.Message.ToolCalls[0].Name, "web_fetch")
	}
	if !strings.HasPrefix(resp.Message.ToolCalls[0].ID, "call_web_fetch_") {
		t.Errorf("ToolCall ID = %q, expected prefix 'call_web_fetch_'", resp.Message.ToolCalls[0].ID)
	}
	if resp.Usage.PromptTokens != 12 {
		t.Errorf("PromptTokens = %d, want 12", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 8 {
		t.Errorf("CompletionTokens = %d, want 8", resp.Usage.CompletionTokens)
	}
}

func TestGeminiChatInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{totally broken json`))
	}))
	defer server.Close()

	provider := NewGeminiProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "gemini-pro",
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

func TestGeminiChatWithToolResultsInRequest(t *testing.T) {
	var receivedReq geminiRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedReq)

		resp := geminiResponse{
			Candidates: []geminiCandidate{
				{
					Content: geminiContent{
						Role:  "model",
						Parts: []geminiPart{{Text: "The file contains hello world"}},
					},
				},
			},
			UsageMetadata: &geminiUsage{
				PromptTokenCount:     25,
				CandidatesTokenCount: 10,
				TotalTokenCount:      35,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewGeminiProvider(config.ProviderConfig{
		Name:    "gemini-test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "gemini-pro",
	}, newTestLogger())

	resp, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Read test.txt"},
			{
				Role: domain.RoleAssistant,
				ToolCalls: []domain.ToolCall{
					{ID: "call_fs_1", Name: "filesystem", Arguments: json.RawMessage(`{"path":"test.txt"}`)},
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

	if resp.Message.Content != "The file contains hello world" {
		t.Errorf("Content = %q", resp.Message.Content)
	}

	// Verify request was properly converted
	if len(receivedReq.Contents) != 3 {
		t.Fatalf("Request contents len = %d, want 3", len(receivedReq.Contents))
	}

	// User message
	if receivedReq.Contents[0].Role != "user" {
		t.Errorf("Contents[0].Role = %q, want %q", receivedReq.Contents[0].Role, "user")
	}

	// Assistant message with function call
	if receivedReq.Contents[1].Role != "model" {
		t.Errorf("Contents[1].Role = %q, want %q", receivedReq.Contents[1].Role, "model")
	}
	if len(receivedReq.Contents[1].Parts) != 1 {
		t.Fatalf("Contents[1].Parts len = %d, want 1", len(receivedReq.Contents[1].Parts))
	}
	if receivedReq.Contents[1].Parts[0].FunctionCall == nil {
		t.Fatal("Contents[1].Parts[0].FunctionCall is nil")
	}
	if receivedReq.Contents[1].Parts[0].FunctionCall.Name != "filesystem" {
		t.Errorf("FunctionCall.Name = %q", receivedReq.Contents[1].Parts[0].FunctionCall.Name)
	}

	// Tool result - in Gemini it becomes "function" role
	if receivedReq.Contents[2].Role != "function" {
		t.Errorf("Contents[2].Role = %q, want %q", receivedReq.Contents[2].Role, "function")
	}
	if len(receivedReq.Contents[2].Parts) != 1 {
		t.Fatalf("Contents[2].Parts len = %d, want 1", len(receivedReq.Contents[2].Parts))
	}
	if receivedReq.Contents[2].Parts[0].FunctionResponse == nil {
		t.Fatal("Contents[2].Parts[0].FunctionResponse is nil")
	}
	if receivedReq.Contents[2].Parts[0].FunctionResponse.Name != "filesystem" {
		t.Errorf("FunctionResponse.Name = %q", receivedReq.Contents[2].Parts[0].FunctionResponse.Name)
	}

	// Verify tools were sent
	if len(receivedReq.Tools) != 1 {
		t.Fatalf("Request tools len = %d, want 1", len(receivedReq.Tools))
	}
}

func TestGeminiRequestWithToolCalls(t *testing.T) {
	req := domain.ChatRequest{
		Model: "gemini-pro",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Hello"},
			{
				Role: domain.RoleAssistant,
				ToolCalls: []domain.ToolCall{
					{ID: "call_1", Name: "search", Arguments: json.RawMessage(`{"query":"test"}`)},
					{ID: "call_2", Name: "fetch", Arguments: json.RawMessage(`{"url":"http://example.com"}`)},
				},
			},
		},
	}

	gemReq := toGeminiRequest(req)

	if len(gemReq.Contents) != 2 {
		t.Fatalf("Contents len = %d, want 2", len(gemReq.Contents))
	}

	// Assistant message with tool calls -> model role with FunctionCall parts
	modelMsg := gemReq.Contents[1]
	if modelMsg.Role != "model" {
		t.Errorf("Model msg role = %q, want %q", modelMsg.Role, "model")
	}
	if len(modelMsg.Parts) != 2 {
		t.Fatalf("Model msg parts = %d, want 2", len(modelMsg.Parts))
	}
	if modelMsg.Parts[0].FunctionCall == nil {
		t.Fatal("Parts[0].FunctionCall is nil")
	}
	if modelMsg.Parts[0].FunctionCall.Name != "search" {
		t.Errorf("Parts[0].FunctionCall.Name = %q", modelMsg.Parts[0].FunctionCall.Name)
	}
	if modelMsg.Parts[1].FunctionCall == nil {
		t.Fatal("Parts[1].FunctionCall is nil")
	}
	if modelMsg.Parts[1].FunctionCall.Name != "fetch" {
		t.Errorf("Parts[1].FunctionCall.Name = %q", modelMsg.Parts[1].FunctionCall.Name)
	}
}

func TestGeminiRequestWithToolResult(t *testing.T) {
	req := domain.ChatRequest{
		Model: "gemini-pro",
		Messages: []domain.Message{
			{
				Role:    domain.RoleTool,
				Name:    "filesystem",
				Content: "file content here",
			},
		},
	}

	gemReq := toGeminiRequest(req)

	if len(gemReq.Contents) != 1 {
		t.Fatalf("Contents len = %d, want 1", len(gemReq.Contents))
	}

	msg := gemReq.Contents[0]
	if msg.Role != "function" {
		t.Errorf("Role = %q, want %q", msg.Role, "function")
	}
	if len(msg.Parts) != 1 {
		t.Fatalf("Parts len = %d, want 1", len(msg.Parts))
	}
	if msg.Parts[0].FunctionResponse == nil {
		t.Fatal("FunctionResponse is nil")
	}
	if msg.Parts[0].FunctionResponse.Name != "filesystem" {
		t.Errorf("FunctionResponse.Name = %q, want %q", msg.Parts[0].FunctionResponse.Name, "filesystem")
	}
}

func TestGeminiDefaultBaseURL(t *testing.T) {
	provider := NewGeminiProvider(config.ProviderConfig{
		Name:   "gemini-test",
		APIKey: "test-key",
		Model:  "gemini-pro",
		// No BaseURL
	}, newTestLogger())

	if provider.baseURL != "https://generativelanguage.googleapis.com" {
		t.Errorf("baseURL = %q, want %q", provider.baseURL, "https://generativelanguage.googleapis.com")
	}
}

func TestGeminiProviderChatDefaultModel(t *testing.T) {
	var receivedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path

		resp := geminiResponse{
			Candidates: []geminiCandidate{
				{
					Content: geminiContent{
						Role:  "model",
						Parts: []geminiPart{{Text: "ok"}},
					},
				},
			},
			UsageMetadata: &geminiUsage{TotalTokenCount: 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewGeminiProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "gemini-1.5-flash",
	}, newTestLogger())

	// Request with no model - should use provider's default
	_, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if !strings.Contains(receivedPath, "gemini-1.5-flash") {
		t.Errorf("URL path = %q, expected it to contain provider default model 'gemini-1.5-flash'", receivedPath)
	}
}

func TestGeminiResponseNoCandidates(t *testing.T) {
	resp := geminiResponse{
		Candidates:    []geminiCandidate{},
		UsageMetadata: &geminiUsage{TotalTokenCount: 3},
	}

	result := fromGeminiResponse(resp)

	if result.Message.Content != "" {
		t.Errorf("Content = %q, want empty", result.Message.Content)
	}
	if len(result.Message.ToolCalls) != 0 {
		t.Errorf("ToolCalls len = %d, want 0", len(result.Message.ToolCalls))
	}
	if result.Message.Role != domain.RoleAssistant {
		t.Errorf("Role = %q, want %q", result.Message.Role, domain.RoleAssistant)
	}
}

func TestGeminiChatContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		}
	}))
	defer server.Close()

	provider := NewGeminiProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "gemini-pro",
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

func TestGeminiResponseWithTextAndToolCall(t *testing.T) {
	resp := geminiResponse{
		Candidates: []geminiCandidate{
			{
				Content: geminiContent{
					Role: "model",
					Parts: []geminiPart{
						{Text: "Let me search for that."},
						{
							FunctionCall: &geminiFunctionCall{
								Name: "search",
								Args: json.RawMessage(`{"query":"golang"}`),
							},
						},
					},
				},
			},
		},
		UsageMetadata: &geminiUsage{
			PromptTokenCount:     10,
			CandidatesTokenCount: 15,
			TotalTokenCount:      25,
		},
	}

	result := fromGeminiResponse(resp)

	if result.Message.Content != "Let me search for that." {
		t.Errorf("Content = %q, want %q", result.Message.Content, "Let me search for that.")
	}
	if len(result.Message.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(result.Message.ToolCalls))
	}
	if result.Message.ToolCalls[0].Name != "search" {
		t.Errorf("ToolCall name = %q", result.Message.ToolCalls[0].Name)
	}
}

func TestGeminiProviderWithCustomTimeouts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := geminiResponse{
			Candidates: []geminiCandidate{
				{Content: geminiContent{Parts: []geminiPart{{Text: "ok"}}}},
			},
			UsageMetadata: &geminiUsage{TotalTokenCount: 2},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewGeminiProvider(config.ProviderConfig{
		Name:        "test",
		BaseURL:     server.URL,
		APIKey:      "test-key",
		Model:       "gemini-pro",
		ConnTimeout: 5000000000,  // 5s in nanoseconds
		RespTimeout: 10000000000, // 10s in nanoseconds
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

func TestGeminiChatCreateRequestError(t *testing.T) {
	// A baseURL with a control character causes http.NewRequestWithContext to fail.
	provider := NewGeminiProvider(config.ProviderConfig{
		Name:    "test",
		BaseURL: "http://invalid\x7f.host",
		APIKey:  "test-key",
		Model:   "gemini-pro",
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

func TestGeminiRequestNoSystem(t *testing.T) {
	req := domain.ChatRequest{
		Model: "gemini-pro",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Hello"},
		},
	}

	gemReq := toGeminiRequest(req)

	if gemReq.SystemInstruction != nil {
		t.Errorf("SystemInstruction should be nil when no system message, got %+v", gemReq.SystemInstruction)
	}
	if len(gemReq.Contents) != 1 {
		t.Fatalf("Contents len = %d, want 1", len(gemReq.Contents))
	}
}
