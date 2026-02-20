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

func TestGemini_RealAPI_BasicChat(t *testing.T) {
	integration.SkipIfShort(t)
	cfg := integration.LoadConfig()
	integration.SkipIfNoAPIKey(t, cfg.GeminiKey, "GEMINI")

	ctx := integration.NewTestContext(t, cfg.TestTimeout)
	provider := NewGeminiProvider(cfg.GeminiKey, "gemini-1.5-flash", nil)

	req := domain.LLMRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Say 'integration test success' and nothing else"},
		},
		MaxTokens:   20,
		Temperature: 0,
	}

	resp, err := provider.Complete(ctx, req)
	if err != nil {
		t.Fatalf("Gemini API call failed: %v", err)
	}

	if resp.Content == "" {
		t.Error("Gemini returned empty response")
	}

	t.Logf("Gemini response: %s", resp.Content)
}

func TestGemini_RealAPI_FunctionCalling(t *testing.T) {
	integration.SkipIfShort(t)
	cfg := integration.LoadConfig()
	integration.SkipIfNoAPIKey(t, cfg.GeminiKey, "GEMINI")

	ctx := integration.NewTestContext(t, cfg.TestTimeout)
	provider := NewGeminiProvider(cfg.GeminiKey, "gemini-1.5-flash", nil)

	// Define a test function (Gemini format)
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
			{Role: domain.RoleUser, Content: "What's the weather in Tokyo?"},
		},
		Tools:       []domain.ToolSchema{tool},
		MaxTokens:   100,
		Temperature: 0,
	}

	resp, err := provider.Complete(ctx, req)
	if err != nil {
		t.Fatalf("Gemini function call failed: %v", err)
	}

	if len(resp.ToolCalls) == 0 {
		t.Fatal("Expected function call, got none")
	}

	if resp.ToolCalls[0].Name != "get_weather" {
		t.Errorf("Expected function 'get_weather', got %q", resp.ToolCalls[0].Name)
	}

	t.Logf("Function call: %+v", resp.ToolCalls[0])
}

func TestGemini_RealAPI_MultiTurnConversation(t *testing.T) {
	integration.SkipIfShort(t)
	cfg := integration.LoadConfig()
	integration.SkipIfNoAPIKey(t, cfg.GeminiKey, "GEMINI")

	ctx := integration.NewTestContext(t, cfg.TestTimeout)
	provider := NewGeminiProvider(cfg.GeminiKey, "gemini-1.5-flash", nil)

	// Turn 1: Introduction
	req1 := domain.LLMRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Remember: my name is Bob"},
		},
		MaxTokens:   50,
		Temperature: 0,
	}

	resp1, err := provider.Complete(ctx, req1)
	if err != nil {
		t.Fatalf("Turn 1 failed: %v", err)
	}

	t.Logf("Turn 1 response: %s", resp1.Content)

	// Turn 2: Recall
	req2 := domain.LLMRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Remember: my name is Bob"},
			{Role: domain.RoleAssistant, Content: resp1.Content},
			{Role: domain.RoleUser, Content: "What's my name?"},
		},
		MaxTokens:   50,
		Temperature: 0,
	}

	resp2, err := provider.Complete(ctx, req2)
	if err != nil {
		t.Fatalf("Turn 2 failed: %v", err)
	}

	// Should mention "Bob"
	if !containsCaseInsensitive(resp2.Content, "Bob") {
		t.Errorf("Gemini didn't recall name. Response: %s", resp2.Content)
	}

	t.Logf("Turn 2 response: %s", resp2.Content)
}

func TestGemini_RealAPI_ErrorHandling(t *testing.T) {
	integration.SkipIfShort(t)
	cfg := integration.LoadConfig()
	integration.SkipIfNoAPIKey(t, cfg.GeminiKey, "GEMINI")

	ctx := integration.NewTestContext(t, cfg.TestTimeout)
	provider := NewGeminiProvider(cfg.GeminiKey, "gemini-1.5-flash", nil)

	// Test with empty message
	req := domain.LLMRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: ""},
		},
		MaxTokens: 100,
	}

	_, err := provider.Complete(ctx, req)
	if err == nil {
		t.Error("Expected error for empty message, got nil")
	}

	t.Logf("Expected error: %v", err)
}

// Helper function for case-insensitive contains check
func containsCaseInsensitive(s, substr string) bool {
	if len(s) == 0 || len(substr) == 0 {
		return false
	}
	// Simple case-insensitive check
	sLower := toLower(s)
	substrLower := toLower(substr)
	for i := 0; i <= len(sLower)-len(substrLower); i++ {
		if sLower[i:i+len(substrLower)] == substrLower {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c = c + ('a' - 'A')
		}
		result[i] = c
	}
	return string(result)
}
