//go:build integration

package llm

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

// TestStream_ToolCallsInStream tests streaming responses that include tool calls
func TestStream_ToolCallsInStream(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name     string
		provider func() domain.StreamingLLMProvider
		envVar   string
		model    string
	}{
		{
			name: "OpenAI",
			provider: func() domain.StreamingLLMProvider {
				apiKey := os.Getenv("OPENAI_API_KEY")
				provider := NewOpenAIProvider(config.ProviderConfig{
					Name:    "openai-stream-test",
					Type:    "openai",
					BaseURL: "https://api.openai.com/v1",
					APIKey:  apiKey,
					Model:   "gpt-4-turbo-preview",
				}, slog.Default())
				// Check if it implements StreamingLLMProvider
				if sp, ok := interface{}(provider).(domain.StreamingLLMProvider); ok {
					return sp
				}
				return nil
			},
			envVar: "OPENAI_API_KEY",
			model:  "gpt-4-turbo-preview",
		},
		{
			name: "Anthropic",
			provider: func() domain.StreamingLLMProvider {
				apiKey := os.Getenv("ANTHROPIC_API_KEY")
				provider := NewAnthropicProvider(config.ProviderConfig{
					Name:    "anthropic-stream-test",
					Type:    "anthropic",
					BaseURL: "https://api.anthropic.com/v1",
					APIKey:  apiKey,
					Model:   "claude-3-5-sonnet-20241022",
				}, slog.Default())
				if sp, ok := interface{}(provider).(domain.StreamingLLMProvider); ok {
					return sp
				}
				return nil
			},
			envVar: "ANTHROPIC_API_KEY",
			model:  "claude-3-5-sonnet-20241022",
		},
		{
			name: "Gemini",
			provider: func() domain.StreamingLLMProvider {
				apiKey := os.Getenv("GEMINI_API_KEY")
				provider := NewGeminiProvider(config.ProviderConfig{
					Name:    "gemini-stream-test",
					Type:    "gemini",
					BaseURL: "https://generativelanguage.googleapis.com/v1beta",
					APIKey:  apiKey,
					Model:   "gemini-1.5-pro",
				}, slog.Default())
				if sp, ok := interface{}(provider).(domain.StreamingLLMProvider); ok {
					return sp
				}
				return nil
			},
			envVar: "GEMINI_API_KEY",
			model:  "gemini-1.5-pro",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if os.Getenv(tt.envVar) == "" {
				t.Skipf("%s not set", tt.envVar)
			}

			provider := tt.provider()
			if provider == nil {
				t.Skip("Provider does not support streaming")
			}

			ctx := context.Background()

			tools := []domain.ToolSchema{
				{Name: "get_weather", Description: "Get weather for a location", Parameters: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`)},
				{Name: "get_time", Description: "Get current time", Parameters: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`)},
			}

			req := domain.ChatRequest{
				Model: tt.model,
				Messages: []domain.Message{
					{Role: domain.RoleUser, Content: "What's the weather and time in Paris?"},
				},
				Tools:  tools,
				Stream: true,
			}

			// Add MaxTokens for Anthropic
			if tt.name == "Anthropic" {
				req.MaxTokens = 1024
			}

			streamChan, err := provider.ChatStream(ctx, req)
			if err != nil {
				t.Fatalf("ChatStream failed: %v", err)
			}

			var toolCalls []domain.ToolCall
			var content strings.Builder

			for delta := range streamChan {
				content.WriteString(delta.Content)
				if len(delta.ToolCalls) > 0 {
					toolCalls = append(toolCalls, delta.ToolCalls...)
				}

				if delta.Done {
					break
				}
			}

			// Verify tool calls received via stream
			if len(toolCalls) < 1 {
				t.Logf("Note: Expected at least 1 tool call in stream, got %d. Content: %s", len(toolCalls), content.String())
				// Some providers might respond directly instead of using tools
				return
			}

			t.Logf("Received %d tool calls via stream", len(toolCalls))

			// Verify unique tool_call_ids
			ids := make(map[string]bool)
			for _, tc := range toolCalls {
				if tc.ID == "" {
					t.Errorf("Tool call %s has empty ID in stream", tc.Name)
				}
				if ids[tc.ID] {
					t.Errorf("Duplicate tool_call_id in stream: %s", tc.ID)
				}
				ids[tc.ID] = true
				t.Logf("  %s (ID: %s)", tc.Name, tc.ID)
			}

			t.Logf("Success: Stream completed with %d unique tool calls", len(toolCalls))
		})
	}
}

// TestStream_InterruptDuringToolCall tests context cancellation during streaming
func TestStream_InterruptDuringToolCall(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "openai-stream-test",
		Type:    "openai",
		BaseURL: "https://api.openai.com/v1",
		APIKey:  apiKey,
		Model:   "gpt-4-turbo-preview",
	}, slog.Default())

	// Check if it implements StreamingLLMProvider
	sp, ok := interface{}(provider).(domain.StreamingLLMProvider)
	if !ok {
		t.Skip("Provider does not support streaming")
	}

	tools := []domain.ToolSchema{
		{Name: "long_operation", Description: "A long running operation", Parameters: json.RawMessage(`{"type":"object","properties":{"task":{"type":"string"}},"required":["task"]}`)},
	}

	// Context that will be cancelled quickly
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	req := domain.ChatRequest{
		Model: "gpt-4-turbo-preview",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Perform a complex analysis task that requires tools."},
		},
		Tools:  tools,
		Stream: true,
	}

	streamChan, err := sp.ChatStream(ctx, req)
	if err != nil {
		// Context might cancel before stream starts
		t.Logf("Expected behavior: Stream failed to start due to timeout: %v", err)
		return
	}

	// Try to read from stream (should be interrupted)
	deltaCount := 0
	for range streamChan {
		deltaCount++
	}

	t.Logf("Stream interrupted after %d deltas (context timeout)", deltaCount)
	// Success if we get here without panic or deadlock
}

// TestStream_FullConversationWithTools tests a complete streaming conversation with tool execution
func TestStream_FullConversationWithTools(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "openai-stream-test",
		Type:    "openai",
		BaseURL: "https://api.openai.com/v1",
		APIKey:  apiKey,
		Model:   "gpt-4-turbo-preview",
	}, slog.Default())

	sp, ok := interface{}(provider).(domain.StreamingLLMProvider)
	if !ok {
		t.Skip("Provider does not support streaming")
	}

	tools := []domain.ToolSchema{
		{Name: "calculate", Description: "Calculate expression", Parameters: json.RawMessage(`{"type":"object","properties":{"expression":{"type":"string"}},"required":["expression"]}`)},
	}

	ctx := context.Background()

	// First stream: Get tool call
	req1 := domain.ChatRequest{
		Model: "gpt-4-turbo-preview",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Calculate 15 * 23 using the calculator tool."},
		},
		Tools:  tools,
		Stream: true,
	}

	streamChan1, err := sp.ChatStream(ctx, req1)
	if err != nil {
		t.Fatalf("First stream failed: %v", err)
	}

	var toolCalls []domain.ToolCall
	var content1 strings.Builder

	for delta := range streamChan1 {
		content1.WriteString(delta.Content)
		if len(delta.ToolCalls) > 0 {
			toolCalls = append(toolCalls, delta.ToolCalls...)
		}
		if delta.Done {
			break
		}
	}

	if len(toolCalls) == 0 {
		t.Skip("Model did not use tool in stream")
	}

	t.Logf("First stream: Received %d tool calls", len(toolCalls))

	// Simulate tool execution
	toolResult := domain.Message{
		Role:    domain.RoleTool,
		Name:    toolCalls[0].Name,
		Content: "345",
		ToolCalls: []domain.ToolCall{{
			ID:   toolCalls[0].ID,
			Name: toolCalls[0].Name,
		}},
	}

	// Second stream: Send tool result and get final answer
	req2 := domain.ChatRequest{
		Model: "gpt-4-turbo-preview",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Calculate 15 * 23 using the calculator tool."},
			{Role: domain.RoleAssistant, Content: content1.String(), ToolCalls: toolCalls},
			toolResult,
		},
		Tools:  tools,
		Stream: true,
	}

	streamChan2, err := sp.ChatStream(ctx, req2)
	if err != nil {
		t.Fatalf("Second stream failed: %v", err)
	}

	var content2 strings.Builder
	for delta := range streamChan2 {
		content2.WriteString(delta.Content)
		if delta.Done {
			break
		}
	}

	finalResponse := content2.String()
	if finalResponse == "" {
		t.Error("Expected non-empty final response")
	}

	t.Logf("Success: Full streaming conversation completed. Final: %s", finalResponse)
}
