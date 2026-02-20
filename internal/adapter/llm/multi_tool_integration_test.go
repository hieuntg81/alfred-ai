//go:build integration

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

// TestMultiTool_AgenticWorkflow tests complex agentic workflow: Research → Analysis → Summary
// This simulates a real multi-step agentic task across all providers
func TestMultiTool_AgenticWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name     string
		provider func() domain.LLMProvider
		envVar   string
		model    string
	}{
		{
			name: "OpenAI",
			provider: func() domain.LLMProvider {
				return NewOpenAIProvider(config.ProviderConfig{
					Name:    "openai-test",
					Type:    "openai",
					BaseURL: "https://api.openai.com/v1",
					APIKey:  os.Getenv("OPENAI_API_KEY"),
					Model:   "gpt-4-turbo-preview",
				}, slog.Default())
			},
			envVar: "OPENAI_API_KEY",
			model:  "gpt-4-turbo-preview",
		},
		{
			name: "Anthropic",
			provider: func() domain.LLMProvider {
				return NewAnthropicProvider(config.ProviderConfig{
					Name:    "anthropic-test",
					Type:    "anthropic",
					BaseURL: "https://api.anthropic.com/v1",
					APIKey:  os.Getenv("ANTHROPIC_API_KEY"),
					Model:   "claude-3-5-sonnet-20241022",
				}, slog.Default())
			},
			envVar: "ANTHROPIC_API_KEY",
			model:  "claude-3-5-sonnet-20241022",
		},
		{
			name: "Gemini",
			provider: func() domain.LLMProvider {
				return NewGeminiProvider(config.ProviderConfig{
					Name:    "gemini-test",
					Type:    "gemini",
					BaseURL: "https://generativelanguage.googleapis.com/v1beta",
					APIKey:  os.Getenv("GEMINI_API_KEY"),
					Model:   "gemini-1.5-pro",
				}, slog.Default())
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

			// Define tools for agentic workflow
			tools := []domain.ToolSchema{
				{
					Name:        "search_web",
					Description: "Search the web for information",
					Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
				},
				{
					Name:        "analyze_data",
					Description: "Analyze data and extract insights",
					Parameters:  json.RawMessage(`{"type":"object","properties":{"data":{"type":"string"}},"required":["data"]}`),
				},
				{
					Name:        "create_summary",
					Description: "Create a summary of findings",
					Parameters:  json.RawMessage(`{"type":"object","properties":{"findings":{"type":"string"}},"required":["findings"]}`),
				},
			}

			// Simulate multi-step agentic task
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			messages := []domain.Message{
				{Role: domain.RoleSystem, Content: "You are a research assistant. Use tools to complete tasks step by step."},
				{Role: domain.RoleUser, Content: "Research Go 1.23 new features, analyze their impact, and create a summary."},
			}

			maxIterations := 10
			for i := 0; i < maxIterations; i++ {
				req := domain.ChatRequest{
					Model:    tt.model,
					Messages: messages,
					Tools:    tools,
				}

				// Add MaxTokens for Anthropic
				if tt.name == "Anthropic" {
					req.MaxTokens = 1024
				}

				resp, err := provider.Chat(ctx, req)
				if err != nil {
					t.Fatalf("Iteration %d failed: %v", i, err)
				}

				messages = append(messages, resp.Message)

				// No tool calls = task complete
				if len(resp.Message.ToolCalls) == 0 {
					t.Logf("Task completed in %d iterations", i+1)
					if resp.Message.Content == "" {
						t.Error("Expected final summary content")
					}
					t.Logf("Final summary: %s", resp.Message.Content)
					return
				}

				t.Logf("Iteration %d: %d tool calls", i+1, len(resp.Message.ToolCalls))

				// Verify unique tool_call_ids in each iteration
				ids := make(map[string]bool)
				for _, tc := range resp.Message.ToolCalls {
					if tc.ID == "" {
						t.Errorf("Tool call %s has empty ID in iteration %d", tc.Name, i+1)
					}
					if ids[tc.ID] {
						t.Errorf("Duplicate tool call ID %s in iteration %d", tc.ID, i+1)
					}
					ids[tc.ID] = true
					t.Logf("  Tool call: %s (ID: %s)", tc.Name, tc.ID)
				}

				// Execute tools and add results
				for _, tc := range resp.Message.ToolCalls {
					result := simulateToolExecution(tc)
					messages = append(messages, domain.Message{
						Role:    domain.RoleTool,
						Name:    tc.Name,
						Content: result,
						ToolCalls: []domain.ToolCall{{
							ID:   tc.ID,
							Name: tc.Name,
						}},
					})
				}
			}

			t.Errorf("Task did not complete within %d iterations", maxIterations)
		})
	}
}

// TestMultiTool_ConcurrentExecution tests handling of multiple simultaneous tool calls
func TestMultiTool_ConcurrentExecution(t *testing.T) {
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

	// Define multiple independent tools
	tools := []domain.ToolSchema{
		{Name: "fetch_weather", Description: "Fetch weather data", Parameters: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`)},
		{Name: "fetch_news", Description: "Fetch latest news", Parameters: json.RawMessage(`{"type":"object","properties":{"topic":{"type":"string"}},"required":["topic"]}`)},
		{Name: "fetch_stock", Description: "Fetch stock price", Parameters: json.RawMessage(`{"type":"object","properties":{"symbol":{"type":"string"}},"required":["symbol"]}`)},
		{Name: "fetch_time", Description: "Fetch current time", Parameters: json.RawMessage(`{"type":"object","properties":{"timezone":{"type":"string"}},"required":["timezone"]}`)},
	}

	req := domain.ChatRequest{
		Model: "gpt-4-turbo-preview",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Get me weather in Paris, latest tech news, AAPL stock price, and time in UTC."},
		},
		Tools: tools,
	}

	resp, err := provider.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if len(resp.Message.ToolCalls) < 2 {
		t.Logf("Warning: Expected multiple concurrent tool calls, got %d", len(resp.Message.ToolCalls))
		return
	}

	t.Logf("Received %d concurrent tool calls", len(resp.Message.ToolCalls))

	// Verify all IDs are unique
	ids := make(map[string]bool)
	for _, tc := range resp.Message.ToolCalls {
		if tc.ID == "" {
			t.Errorf("Tool call %s has empty ID", tc.Name)
		}
		if ids[tc.ID] {
			t.Errorf("Duplicate tool call ID: %s", tc.ID)
		}
		ids[tc.ID] = true
		t.Logf("  %s (ID: %s)", tc.Name, tc.ID)
	}

	t.Logf("Success: All %d concurrent tool calls have unique IDs", len(resp.Message.ToolCalls))
}

// TestMultiTool_ToolChaining tests tool output feeding into next tool
func TestMultiTool_ToolChaining(t *testing.T) {
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

	// Tools that naturally chain: search → extract → summarize
	tools := []domain.ToolSchema{
		{Name: "search_documents", Description: "Search for documents", Parameters: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`)},
		{Name: "extract_data", Description: "Extract data from documents", Parameters: json.RawMessage(`{"type":"object","properties":{"doc_id":{"type":"string"}},"required":["doc_id"]}`)},
		{Name: "summarize_data", Description: "Summarize extracted data", Parameters: json.RawMessage(`{"type":"object","properties":{"data":{"type":"string"}},"required":["data"]}`)},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: "You are a helpful assistant. Use tools in sequence as needed."},
		{Role: domain.RoleUser, Content: "Search for Go documentation, extract key features, and summarize them."},
	}

	// Allow up to 5 chained tool calls
	for i := 0; i < 5; i++ {
		req := domain.ChatRequest{
			Model:    "gpt-4-turbo-preview",
			Messages: messages,
			Tools:    tools,
		}

		resp, err := provider.Chat(ctx, req)
		if err != nil {
			t.Fatalf("Iteration %d failed: %v", i+1, err)
		}

		messages = append(messages, resp.Message)

		if len(resp.Message.ToolCalls) == 0 {
			t.Logf("Tool chain completed in %d steps", i+1)
			if resp.Message.Content == "" {
				t.Error("Expected final content")
			}
			t.Logf("Final result: %s", resp.Message.Content)
			return
		}

		// Process tool calls
		for _, tc := range resp.Message.ToolCalls {
			t.Logf("Step %d: %s (ID: %s)", i+1, tc.Name, tc.ID)
			result := simulateToolExecution(tc)
			messages = append(messages, domain.Message{
				Role:    domain.RoleTool,
				Name:    tc.Name,
				Content: result,
				ToolCalls: []domain.ToolCall{{
					ID:   tc.ID,
					Name: tc.Name,
				}},
			})
		}
	}

	t.Log("Tool chain completed")
}

// simulateToolExecution simulates tool execution with realistic responses
func simulateToolExecution(call domain.ToolCall) string {
	switch call.Name {
	case "search_web":
		return `{"results": ["Go 1.23 adds range over int", "Improved type inference", "New standard library packages"]}`
	case "analyze_data":
		return `{"insights": ["Performance improvements", "Developer experience enhanced", "Breaking changes minimal"]}`
	case "create_summary":
		return "Summary: Go 1.23 brings significant improvements including range over int, better type inference, and new stdlib packages. Impact is positive with minimal breaking changes."
	case "fetch_weather":
		return `{"city": "Paris", "temp": "18C", "condition": "Sunny"}`
	case "fetch_news":
		return `{"topic": "tech", "headlines": ["AI advancements", "New framework released"]}`
	case "fetch_stock":
		return `{"symbol": "AAPL", "price": 175.43, "change": "+2.1%"}`
	case "fetch_time":
		return `{"timezone": "UTC", "time": "2024-01-15T14:30:00Z"}`
	case "search_documents":
		return `{"doc_id": "go-docs-v1.23", "title": "Go 1.23 Release Notes"}`
	case "extract_data":
		return `{"features": ["range over int", "generic type inference", "slices package"]}`
	case "summarize_data":
		return "Key features: range over int simplifies iteration, improved generics, new slices package for common operations."
	default:
		return fmt.Sprintf("Executed %s successfully", call.Name)
	}
}
