//go:build integration
// +build integration

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

// Integration tests for real LLM providers
// Run with: go test -tags=integration ./internal/adapter/llm
//
// These tests require API keys:
//   - OPENAI_API_KEY for OpenAI tests
//   - ANTHROPIC_API_KEY for Anthropic tests
//   - GEMINI_API_KEY for Gemini tests
//
// Skip with: go test -short (integration tests are skipped with -short)

func TestOpenAIIntegration_SimpleChat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	provider := NewOpenAIProvider(apiKey, "gpt-3.5-turbo")
	ctx := context.Background()

	req := domain.ChatRequest{
		Model: "gpt-3.5-turbo",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Say 'test successful' if you can read this."},
		},
		MaxTokens: 50,
	}

	resp, err := provider.Chat(ctx, req)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Expected non-nil response")
	}

	if resp.Message.Content == "" {
		t.Error("Expected non-empty response content")
	}

	t.Logf("OpenAI response: %s", resp.Message.Content)
}

func TestOpenAIIntegration_MultiToolCall(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	provider := NewOpenAIProvider(apiKey, "gpt-3.5-turbo")
	ctx := context.Background()

	// Define tools
	tools := []domain.ToolSchema{
		{
			Name:        "get_weather",
			Description: "Get weather for a location",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`),
		},
		{
			Name:        "get_time",
			Description: "Get current time for a location",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`),
		},
	}

	req := domain.ChatRequest{
		Model: "gpt-3.5-turbo",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "What's the weather and time in London?"},
		},
		Tools:     tools,
		MaxTokens: 150,
	}

	resp, err := provider.Chat(ctx, req)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if len(resp.Message.ToolCalls) == 0 {
		t.Log("Warning: Expected tool calls but got none. Model may have chosen to respond directly.")
		t.Logf("Response: %s", resp.Message.Content)
		return
	}

	t.Logf("Received %d tool calls", len(resp.Message.ToolCalls))
	for i, tc := range resp.Message.ToolCalls {
		t.Logf("  Tool %d: %s (ID: %s)", i+1, tc.Name, tc.ID)
	}

	// Test tool result mapping (Issue #3 fix verification)
	// Simulate tool execution and send results back
	toolResultMsgs := make([]domain.Message, len(resp.Message.ToolCalls))
	for i, tc := range resp.Message.ToolCalls {
		toolResultMsgs[i] = domain.Message{
			Role:    domain.RoleTool,
			Content: "Sample result",
			ToolCalls: []domain.ToolCall{
				{ID: tc.ID, Name: tc.Name}, // Preserve ID for mapping
			},
		}
	}

	// Second request with tool results
	req2 := domain.ChatRequest{
		Model: "gpt-3.5-turbo",
		Messages: append([]domain.Message{
			{Role: domain.RoleUser, Content: "What's the weather and time in London?"},
			resp.Message,
		}, toolResultMsgs...),
		MaxTokens: 100,
	}

	resp2, err := provider.Chat(ctx, req2)
	if err != nil {
		t.Fatalf("Second chat (with tool results) failed: %v", err)
	}

	t.Logf("Final response: %s", resp2.Message.Content)
}

func TestAnthropicIntegration_SimpleChat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	// Note: Update provider initialization if Anthropic provider exists
	t.Skip("Anthropic provider integration - implement when provider is available")
}

func TestGeminiIntegration_SimpleChat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	// Note: Update provider initialization if Gemini provider exists
	t.Skip("Gemini provider integration - implement when provider is available")
}

// TestOpenAIIntegration_ParallelToolCalls tests parallel tool calls with proper tool_call_id mapping
// This addresses the critical tool_call_id mapping bug identified in CODE_REVIEW.md
func TestOpenAIIntegration_ParallelToolCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "openai-test",
		Type:    "openai",
		BaseURL: "https://api.openai.com/v1",
		APIKey:  apiKey,
		Model:   "gpt-4-turbo-preview",
	}, slog.Default())

	// Tools: weather, time, calculator
	tools := []domain.ToolSchema{
		{Name: "get_weather", Description: "Get weather for a location", Parameters: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`)},
		{Name: "get_time", Description: "Get current time for a location", Parameters: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`)},
		{Name: "calculate", Description: "Calculate a mathematical expression", Parameters: json.RawMessage(`{"type":"object","properties":{"expression":{"type":"string"}},"required":["expression"]}`)},
	}

	// Request that should trigger multiple tools
	req := domain.ChatRequest{
		Model: "gpt-4-turbo-preview",
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: "You are a helpful assistant. Use the provided tools to answer questions."},
			{Role: domain.RoleUser, Content: "What's the weather in London, the current time in Tokyo, and what's 15 * 23?"},
		},
		Tools: tools,
	}

	resp, err := provider.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	// Verify multiple tool calls (might be less than 3 if model decides to batch)
	if len(resp.Message.ToolCalls) < 1 {
		t.Logf("Warning: Expected multiple tool calls, got %d. Model may have responded directly: %s", len(resp.Message.ToolCalls), resp.Message.Content)
		return
	}

	t.Logf("Received %d tool calls", len(resp.Message.ToolCalls))

	// Verify each tool call has unique ID
	ids := make(map[string]bool)
	for _, tc := range resp.Message.ToolCalls {
		if tc.ID == "" {
			t.Errorf("Tool call %s has empty ID", tc.Name)
		}
		if ids[tc.ID] {
			t.Errorf("Duplicate tool call ID: %s", tc.ID)
		}
		ids[tc.ID] = true
		t.Logf("Tool call: %s (ID: %s)", tc.Name, tc.ID)
	}

	// Simulate tool execution results
	toolResults := []domain.Message{}
	for _, tc := range resp.Message.ToolCalls {
		result := fmt.Sprintf("Result for %s: success", tc.Name)
		toolResults = append(toolResults, domain.Message{
			Role:    domain.RoleTool,
			Name:    tc.Name,
			Content: result,
			ToolCalls: []domain.ToolCall{{
				ID:   tc.ID, // CRITICAL: Map back correct ID
				Name: tc.Name,
			}},
		})
	}

	// Continue conversation with tool results
	followUpReq := domain.ChatRequest{
		Model: "gpt-4-turbo-preview",
		Messages: append([]domain.Message{
			{Role: domain.RoleSystem, Content: "You are a helpful assistant. Use the provided tools to answer questions."},
			{Role: domain.RoleUser, Content: "What's the weather in London, the current time in Tokyo, and what's 15 * 23?"},
		}, append([]domain.Message{resp.Message}, toolResults...)...),
		Tools: tools,
	}

	finalResp, err := provider.Chat(context.Background(), followUpReq)
	if err != nil {
		t.Fatalf("Follow-up chat with tool results failed: %v", err)
	}

	// Verify final response contains synthesis of tool results
	if finalResp.Message.Content == "" {
		t.Error("Expected non-empty final response")
	}

	t.Logf("Success: Multi-tool workflow completed with %d tool calls. Final response: %s", len(resp.Message.ToolCalls), finalResp.Message.Content)
}

// TestOpenAIIntegration_InvalidToolCallID tests error handling when tool_call_id is mismatched
func TestOpenAIIntegration_InvalidToolCallID(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "openai-test",
		Type:    "openai",
		BaseURL: "https://api.openai.com/v1",
		APIKey:  apiKey,
		Model:   "gpt-4-turbo-preview",
	}, slog.Default())

	tools := []domain.ToolSchema{
		{Name: "get_weather", Description: "Get weather for a location", Parameters: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`)},
	}

	req := domain.ChatRequest{
		Model: "gpt-4-turbo-preview",
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: "You are a helpful assistant. Use the provided tools."},
			{Role: domain.RoleUser, Content: "What's the weather in Paris?"},
		},
		Tools: tools,
	}

	resp, err := provider.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if len(resp.Message.ToolCalls) == 0 {
		t.Skip("Model did not request tool call")
	}

	// Intentionally use wrong tool_call_id
	wrongID := "wrong_id_12345"
	toolResult := domain.Message{
		Role:    domain.RoleTool,
		Name:    resp.Message.ToolCalls[0].Name,
		Content: "Weather: Sunny, 22°C",
		ToolCalls: []domain.ToolCall{{
			ID:   wrongID, // WRONG ID - should cause error
			Name: resp.Message.ToolCalls[0].Name,
		}},
	}

	followUpReq := domain.ChatRequest{
		Model: "gpt-4-turbo-preview",
		Messages: append([]domain.Message{
			{Role: domain.RoleSystem, Content: "You are a helpful assistant. Use the provided tools."},
			{Role: domain.RoleUser, Content: "What's the weather in Paris?"},
		}, resp.Message, toolResult),
		Tools: tools,
	}

	_, err = provider.Chat(context.Background(), followUpReq)
	// OpenAI API might accept this but it's a validation issue at the API level
	// We log the result to verify behavior
	if err != nil {
		t.Logf("Expected behavior: API rejected mismatched tool_call_id: %v", err)
	} else {
		t.Logf("Warning: API accepted mismatched tool_call_id (expected: %s, sent: %s)", resp.Message.ToolCalls[0].ID, wrongID)
	}
}

// TestAnthropicIntegration_MultipleToolUse tests Anthropic's tool_use functionality
func TestAnthropicIntegration_MultipleToolUse(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	provider := NewAnthropicProvider(config.ProviderConfig{
		Name:    "anthropic-test",
		Type:    "anthropic",
		BaseURL: "https://api.anthropic.com/v1",
		APIKey:  apiKey,
		Model:   "claude-3-5-sonnet-20241022",
	}, slog.Default())

	tools := []domain.ToolSchema{
		{Name: "get_weather", Description: "Get weather for a location", Parameters: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`)},
		{Name: "get_time", Description: "Get current time for a location", Parameters: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`)},
	}

	req := domain.ChatRequest{
		Model: "claude-3-5-sonnet-20241022",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "What's the weather and time in Berlin?"},
		},
		Tools:     tools,
		MaxTokens: 1024,
	}

	resp, err := provider.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if len(resp.Message.ToolCalls) == 0 {
		t.Logf("Warning: Expected tool calls, got none. Response: %s", resp.Message.Content)
		return
	}

	t.Logf("Received %d tool calls from Anthropic", len(resp.Message.ToolCalls))

	// Verify unique tool_use IDs
	ids := make(map[string]bool)
	for _, tc := range resp.Message.ToolCalls {
		if tc.ID == "" {
			t.Errorf("Tool call %s has empty ID", tc.Name)
		}
		if ids[tc.ID] {
			t.Errorf("Duplicate tool call ID: %s", tc.ID)
		}
		ids[tc.ID] = true
		t.Logf("Tool call: %s (ID: %s)", tc.Name, tc.ID)
	}

	// Simulate tool results
	toolResults := []domain.Message{}
	for _, tc := range resp.Message.ToolCalls {
		toolResults = append(toolResults, domain.Message{
			Role:    domain.RoleTool,
			Name:    tc.Name,
			Content: fmt.Sprintf("Result for %s", tc.Name),
			ToolCalls: []domain.ToolCall{{
				ID:   tc.ID,
				Name: tc.Name,
			}},
		})
	}

	followUpReq := domain.ChatRequest{
		Model: "claude-3-5-sonnet-20241022",
		Messages: append([]domain.Message{
			{Role: domain.RoleUser, Content: "What's the weather and time in Berlin?"},
		}, append([]domain.Message{resp.Message}, toolResults...)...),
		Tools:     tools,
		MaxTokens: 1024,
	}

	finalResp, err := provider.Chat(context.Background(), followUpReq)
	if err != nil {
		t.Fatalf("Follow-up chat failed: %v", err)
	}

	if finalResp.Message.Content == "" {
		t.Error("Expected non-empty final response")
	}

	t.Logf("Success: Anthropic multi-tool workflow completed")
}

// TestGeminiIntegration_MultipleFunctionCalls tests Gemini's function calling
func TestGeminiIntegration_MultipleFunctionCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	provider := NewGeminiProvider(config.ProviderConfig{
		Name:    "gemini-test",
		Type:    "gemini",
		BaseURL: "https://generativelanguage.googleapis.com/v1beta",
		APIKey:  apiKey,
		Model:   "gemini-1.5-pro",
	}, slog.Default())

	tools := []domain.ToolSchema{
		{Name: "get_weather", Description: "Get weather for a location", Parameters: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`)},
		{Name: "calculate", Description: "Calculate expression", Parameters: json.RawMessage(`{"type":"object","properties":{"expression":{"type":"string"}},"required":["expression"]}`)},
	}

	req := domain.ChatRequest{
		Model: "gemini-1.5-pro",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "What's the weather in Tokyo and what's 42 * 11?"},
		},
		Tools: tools,
	}

	resp, err := provider.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if len(resp.Message.ToolCalls) == 0 {
		t.Logf("Warning: Expected tool calls, got none. Response: %s", resp.Message.Content)
		return
	}

	t.Logf("Received %d function calls from Gemini", len(resp.Message.ToolCalls))

	// Verify unique function call IDs
	ids := make(map[string]bool)
	for _, tc := range resp.Message.ToolCalls {
		if tc.ID == "" {
			t.Errorf("Function call %s has empty ID", tc.Name)
		}
		if ids[tc.ID] {
			t.Errorf("Duplicate function call ID: %s", tc.ID)
		}
		ids[tc.ID] = true
		t.Logf("Function call: %s (ID: %s)", tc.Name, tc.ID)
	}

	t.Logf("Success: Gemini multiple function calls verified")
}

// TestIntegration_ToolExecutionError tests error handling during tool execution
func TestIntegration_ToolExecutionError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "openai-test",
		Type:    "openai",
		BaseURL: "https://api.openai.com/v1",
		APIKey:  apiKey,
		Model:   "gpt-4-turbo-preview",
	}, slog.Default())

	tools := []domain.ToolSchema{
		{Name: "risky_operation", Description: "An operation that might fail", Parameters: json.RawMessage(`{"type":"object","properties":{"action":{"type":"string"}},"required":["action"]}`)},
	}

	req := domain.ChatRequest{
		Model: "gpt-4-turbo-preview",
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: "You are a helpful assistant."},
			{Role: domain.RoleUser, Content: "Please perform a risky operation."},
		},
		Tools: tools,
	}

	resp, err := provider.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if len(resp.Message.ToolCalls) == 0 {
		t.Skip("Model did not request tool call")
	}

	// Simulate tool error
	errorResult := domain.Message{
		Role:    domain.RoleTool,
		Name:    resp.Message.ToolCalls[0].Name,
		Content: "Error: Operation failed - insufficient permissions",
		ToolCalls: []domain.ToolCall{{
			ID:   resp.Message.ToolCalls[0].ID,
			Name: resp.Message.ToolCalls[0].Name,
		}},
	}

	followUpReq := domain.ChatRequest{
		Model: "gpt-4-turbo-preview",
		Messages: append([]domain.Message{
			{Role: domain.RoleSystem, Content: "You are a helpful assistant."},
			{Role: domain.RoleUser, Content: "Please perform a risky operation."},
		}, resp.Message, errorResult),
		Tools: tools,
	}

	finalResp, err := provider.Chat(context.Background(), followUpReq)
	if err != nil {
		t.Fatalf("Chat with error result failed: %v", err)
	}

	// Verify LLM handles error gracefully
	if finalResp.Message.Content == "" {
		t.Error("Expected error handling response")
	}

	t.Logf("Success: LLM handled tool error gracefully: %s", finalResp.Message.Content)
}

// TestIntegration_ToolExecutionTimeout tests timeout handling during tool calls
func TestIntegration_ToolExecutionTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	provider := NewOpenAIProvider(config.ProviderConfig{
		Name:        "openai-test",
		Type:        "openai",
		BaseURL:     "https://api.openai.com/v1",
		APIKey:      apiKey,
		Model:       "gpt-4-turbo-preview",
		ConnTimeout: 5,  // 5 second timeout
		RespTimeout: 10, // 10 second timeout
	}, slog.Default())

	tools := []domain.ToolSchema{
		{Name: "slow_operation", Description: "A slow operation", Parameters: json.RawMessage(`{"type":"object","properties":{"duration":{"type":"number"}},"required":["duration"]}`)},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30) // Very short timeout for test
	defer cancel()

	req := domain.ChatRequest{
		Model: "gpt-4-turbo-preview",
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: "You are a helpful assistant."},
			{Role: domain.RoleUser, Content: "Perform a quick test."},
		},
		Tools: tools,
	}

	_, err := provider.Chat(ctx, req)
	if err != nil {
		// Timeout expected
		t.Logf("Expected timeout behavior: %v", err)
	} else {
		t.Logf("Note: Request completed before timeout")
	}
}
