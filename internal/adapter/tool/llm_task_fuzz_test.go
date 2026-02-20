package tool

import (
	"context"
	"testing"
	"time"

	"alfred-ai/internal/adapter/llm"
	"alfred-ai/internal/domain"
)

func FuzzLLMTaskTool(f *testing.F) {
	f.Add(`{"prompt": "return 42"}`)
	f.Add(`{"prompt": "classify", "input": {"text": "hello"}}`)
	f.Add(`{"prompt": "x", "schema": {"type": "object"}}`)
	f.Add(`{"prompt": "x", "provider": "nonexistent"}`)
	f.Add(`{}`)
	f.Add(`{"prompt": ""}`)
	f.Add(`invalid json`)
	f.Add(`{"prompt": "x", "temperature": 0.5, "max_tokens": 100}`)
	f.Add(`{"prompt": "x", "timeout_ms": 1000}`)

	f.Fuzz(func(t *testing.T, input string) {
		mock := &mockLLMProvider{
			response: &domain.ChatResponse{
				Message: domain.Message{
					Role:    domain.RoleAssistant,
					Content: `{"ok": true}`,
				},
			},
		}
		reg := llm.NewRegistry()
		reg.Register(mock)

		tool := NewLLMTaskTool(mock, reg, LLMTaskConfig{
			DefaultModel:  "mock",
			MaxTokens:     100,
			Timeout:       1 * time.Second,
			MaxPromptSize: 1024,
			MaxInputSize:  1024,
		}, newTestLogger())

		// Must not panic on any input
		tool.Execute(context.Background(), []byte(input))
	})
}
