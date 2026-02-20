//go:build integration
// +build integration

package llm

import (
	"context"
	"encoding/json"
	"testing"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/integration"
)

func TestOpenAI_RealAPI_BasicChat(t *testing.T) {
	integration.SkipIfShort(t)
	cfg := integration.LoadConfig()
	integration.SkipIfNoAPIKey(t, cfg.OpenAIKey, "OPENAI")

	ctx := integration.NewTestContext(t, cfg.TestTimeout)
	provider := NewOpenAIProvider(cfg.OpenAIKey, "gpt-4o-mini", nil)

	req := domain.LLMRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Say 'integration test success' and nothing else"},
		},
		MaxTokens:   20,
		Temperature: 0,
	}

	resp, err := provider.Complete(ctx, req)
	if err != nil {
		t.Fatalf("OpenAI API call failed: %v", err)
	}

	if resp.Content == "" {
		t.Error("OpenAI returned empty response")
	}

	t.Logf("OpenAI response: %s", resp.Content)
}

func TestOpenAI_RealAPI_ToolCalling_Single(t *testing.T) {
	integration.SkipIfShort(t)
	cfg := integration.LoadConfig()
	integration.SkipIfNoAPIKey(t, cfg.OpenAIKey, "OPENAI")

	ctx := integration.NewTestContext(t, cfg.TestTimeout)
	provider := NewOpenAIProvider(cfg.OpenAIKey, "gpt-4o-mini", nil)

	// Define a test tool
	tool := domain.ToolSchema{
		Name:        "get_weather",
		Description: "Get weather for a location",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"location": {"type": "string"}
			},
			"required": ["location"]
		}`),
	}

	req := domain.LLMRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "What's the weather in Paris?"},
		},
		Tools:       []domain.ToolSchema{tool},
		MaxTokens:   100,
		Temperature: 0,
	}

	resp, err := provider.Complete(ctx, req)
	if err != nil {
		t.Fatalf("OpenAI tool call failed: %v", err)
	}

	if len(resp.ToolCalls) == 0 {
		t.Fatal("Expected tool call, got none")
	}

	if resp.ToolCalls[0].Name != "get_weather" {
		t.Errorf("Expected tool 'get_weather', got %q", resp.ToolCalls[0].Name)
	}

	if resp.ToolCalls[0].ID == "" {
		t.Error("Tool call ID is empty")
	}

	t.Logf("Tool call: %+v", resp.ToolCalls[0])
}

func TestOpenAI_RealAPI_ToolCalling_MultipleParallel(t *testing.T) {
	integration.SkipIfShort(t)
	cfg := integration.LoadConfig()
	integration.SkipIfNoAPIKey(t, cfg.OpenAIKey, "OPENAI")

	ctx := integration.NewTestContext(t, cfg.TestTimeout)
	provider := NewOpenAIProvider(cfg.OpenAIKey, "gpt-4o", nil) // gpt-4o supports parallel

	// Define multiple tools
	tools := []domain.ToolSchema{
		{
			Name:        "get_weather",
			Description: "Get weather for a location",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {"location": {"type": "string"}},
				"required": ["location"]
			}`),
		},
		{
			Name:        "get_time",
			Description: "Get current time for a location",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {"location": {"type": "string"}},
				"required": ["location"]
			}`),
		},
	}

	req := domain.LLMRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "What's the weather and current time in Tokyo?"},
		},
		Tools:       tools,
		MaxTokens:   200,
		Temperature: 0,
	}

	resp, err := provider.Complete(ctx, req)
	if err != nil {
		t.Fatalf("OpenAI parallel tool call failed: %v", err)
	}

	if len(resp.ToolCalls) < 2 {
		t.Fatalf("Expected 2 parallel tool calls, got %d", len(resp.ToolCalls))
	}

	// Verify each tool call has unique ID
	seen := make(map[string]bool)
	for _, tc := range resp.ToolCalls {
		if tc.ID == "" {
			t.Errorf("Tool call %q has empty ID", tc.Name)
		}
		if seen[tc.ID] {
			t.Errorf("Duplicate tool_call_id: %s", tc.ID)
		}
		seen[tc.ID] = true
	}

	t.Logf("Parallel tool calls: %+v", resp.ToolCalls)
}

func TestOpenAI_RealAPI_ToolResult_Mapping(t *testing.T) {
	// This test validates the fix for CODE_REVIEW.md bug #5 (OpenAI tool_call_id)
	integration.SkipIfShort(t)
	cfg := integration.LoadConfig()
	integration.SkipIfNoAPIKey(t, cfg.OpenAIKey, "OPENAI")

	ctx := integration.NewTestContext(t, cfg.TestTimeout)
	provider := NewOpenAIProvider(cfg.OpenAIKey, "gpt-4o-mini", nil)

	tool := domain.ToolSchema{
		Name:        "calculate",
		Description: "Perform calculation",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {"expression": {"type": "string"}},
			"required": ["expression"]
		}`),
	}

	// Step 1: Get tool call
	req1 := domain.LLMRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Calculate 42 + 8"},
		},
		Tools:       []domain.ToolSchema{tool},
		MaxTokens:   100,
		Temperature: 0,
	}

	resp1, err := provider.Complete(ctx, req1)
	if err != nil {
		t.Fatalf("Step 1 failed: %v", err)
	}

	if len(resp1.ToolCalls) == 0 {
		t.Fatal("Expected tool call")
	}

	toolCallID := resp1.ToolCalls[0].ID
	t.Logf("Tool call ID: %s", toolCallID)

	// Step 2: Send tool result with tool_call_id
	req2 := domain.LLMRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Calculate 42 + 8"},
			{
				Role: domain.RoleAssistant,
				ToolCalls: []domain.ToolCall{
					{
						ID:        toolCallID,
						Name:      "calculate",
						Arguments: json.RawMessage(`{"expression":"42+8"}`),
					},
				},
			},
			{
				Role:    domain.RoleTool,
				Name:    "calculate",
				Content: "50",
				ToolCalls: []domain.ToolCall{{
					ID:   toolCallID, // This is what gets mapped to tool_call_id
					Name: "calculate",
				}},
			},
		},
		MaxTokens:   50,
		Temperature: 0,
	}

	resp2, err := provider.Complete(ctx, req2)
	if err != nil {
		// If this fails, the tool_call_id mapping is broken
		t.Fatalf("Tool result mapping failed (bug still exists): %v", err)
	}

	if resp2.Content == "" {
		t.Error("Expected final response after tool result")
	}

	t.Logf("Final response: %s", resp2.Content)
}

func TestOpenAI_RealAPI_ErrorHandling(t *testing.T) {
	integration.SkipIfShort(t)
	cfg := integration.LoadConfig()
	integration.SkipIfNoAPIKey(t, cfg.OpenAIKey, "OPENAI")

	ctx := integration.NewTestContext(t, cfg.TestTimeout)
	provider := NewOpenAIProvider(cfg.OpenAIKey, "gpt-4o-mini", nil)

	// Test with invalid max_tokens (too large)
	req := domain.LLMRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Hello"},
		},
		MaxTokens: 1000000, // Way too large
	}

	_, err := provider.Complete(ctx, req)
	if err == nil {
		t.Error("Expected error for invalid max_tokens, got nil")
	}

	t.Logf("Expected error: %v", err)
}
