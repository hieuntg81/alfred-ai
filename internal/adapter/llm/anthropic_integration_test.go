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

func TestAnthropic_RealAPI_BasicChat(t *testing.T) {
	integration.SkipIfShort(t)
	cfg := integration.LoadConfig()
	integration.SkipIfNoAPIKey(t, cfg.AnthropicKey, "ANTHROPIC")

	ctx := integration.NewTestContext(t, cfg.TestTimeout)
	provider := NewAnthropicProvider(cfg.AnthropicKey, "claude-3-5-haiku-20241022", nil)

	req := domain.LLMRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Say 'integration test success' and nothing else"},
		},
		MaxTokens:   20,
		Temperature: 0,
	}

	resp, err := provider.Complete(ctx, req)
	if err != nil {
		t.Fatalf("Anthropic API call failed: %v", err)
	}

	if resp.Content == "" {
		t.Error("Anthropic returned empty response")
	}

	t.Logf("Anthropic response: %s", resp.Content)
}

func TestAnthropic_RealAPI_ToolCalling(t *testing.T) {
	integration.SkipIfShort(t)
	cfg := integration.LoadConfig()
	integration.SkipIfNoAPIKey(t, cfg.AnthropicKey, "ANTHROPIC")

	ctx := integration.NewTestContext(t, cfg.TestTimeout)
	provider := NewAnthropicProvider(cfg.AnthropicKey, "claude-3-5-haiku-20241022", nil)

	// Define a test tool (Claude format)
	tool := domain.ToolSchema{
		Name:        "get_weather",
		Description: "Get weather for a location",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"location": {
					"type": "string",
					"description": "The city name"
				}
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
		t.Fatalf("Anthropic tool call failed: %v", err)
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

func TestAnthropic_RealAPI_MultiTurnConversation(t *testing.T) {
	integration.SkipIfShort(t)
	cfg := integration.LoadConfig()
	integration.SkipIfNoAPIKey(t, cfg.AnthropicKey, "ANTHROPIC")

	ctx := integration.NewTestContext(t, cfg.TestTimeout)
	provider := NewAnthropicProvider(cfg.AnthropicKey, "claude-3-5-haiku-20241022", nil)

	// Turn 1: Ask question
	req1 := domain.LLMRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "My favorite color is blue"},
		},
		MaxTokens:   50,
		Temperature: 0,
	}

	resp1, err := provider.Complete(ctx, req1)
	if err != nil {
		t.Fatalf("Turn 1 failed: %v", err)
	}

	t.Logf("Turn 1 response: %s", resp1.Content)

	// Turn 2: Check memory
	req2 := domain.LLMRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "My favorite color is blue"},
			{Role: domain.RoleAssistant, Content: resp1.Content},
			{Role: domain.RoleUser, Content: "What's my favorite color?"},
		},
		MaxTokens:   50,
		Temperature: 0,
	}

	resp2, err := provider.Complete(ctx, req2)
	if err != nil {
		t.Fatalf("Turn 2 failed: %v", err)
	}

	// Should mention "blue"
	if !contains(resp2.Content, "blue") {
		t.Errorf("Claude didn't recall favorite color. Response: %s", resp2.Content)
	}

	t.Logf("Turn 2 response: %s", resp2.Content)
}

func TestAnthropic_RealAPI_ErrorHandling(t *testing.T) {
	integration.SkipIfShort(t)
	cfg := integration.LoadConfig()
	integration.SkipIfNoAPIKey(t, cfg.AnthropicKey, "ANTHROPIC")

	ctx := integration.NewTestContext(t, cfg.TestTimeout)
	provider := NewAnthropicProvider(cfg.AnthropicKey, "claude-3-5-haiku-20241022", nil)

	// Test with invalid max_tokens
	req := domain.LLMRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Hello"},
		},
		MaxTokens: 10000000, // Way too large
	}

	_, err := provider.Complete(ctx, req)
	if err == nil {
		t.Error("Expected error for invalid max_tokens, got nil")
	}

	t.Logf("Expected error: %v", err)
}

// Helper function for string contains check
func contains(s, substr string) bool {
	if len(s) == 0 || len(substr) == 0 {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
