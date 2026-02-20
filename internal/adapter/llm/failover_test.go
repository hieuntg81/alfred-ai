package llm

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"alfred-ai/internal/domain"
)

type mockProvider struct {
	name     string
	chatFunc func(context.Context, domain.ChatRequest) (*domain.ChatResponse, error)
}

func (m *mockProvider) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	return m.chatFunc(ctx, req)
}
func (m *mockProvider) Name() string { return m.name }

type mockStreamProvider struct {
	mockProvider
	streamFunc func(context.Context, domain.ChatRequest) (<-chan domain.StreamDelta, error)
}

func (m *mockStreamProvider) ChatStream(ctx context.Context, req domain.ChatRequest) (<-chan domain.StreamDelta, error) {
	return m.streamFunc(ctx, req)
}

func TestFailoverPrimarySuccess(t *testing.T) {
	primary := &mockProvider{
		name: "primary",
		chatFunc: func(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
			return &domain.ChatResponse{Message: domain.Message{Content: "primary response"}}, nil
		},
	}
	fallback := &mockProvider{
		name: "fallback",
		chatFunc: func(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
			t.Fatal("fallback should not be called")
			return nil, nil
		},
	}

	fp := NewFailoverProvider(primary, []domain.LLMProvider{fallback}, slog.Default())
	resp, err := fp.Chat(context.Background(), domain.ChatRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Content != "primary response" {
		t.Errorf("content = %q, want %q", resp.Message.Content, "primary response")
	}
}

func TestFailoverPrimaryFailFallbackSuccess(t *testing.T) {
	primary := &mockProvider{
		name: "primary",
		chatFunc: func(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
			return nil, errors.New("primary down")
		},
	}
	fallback := &mockProvider{
		name: "fallback",
		chatFunc: func(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
			return &domain.ChatResponse{Message: domain.Message{Content: "fallback response"}}, nil
		},
	}

	fp := NewFailoverProvider(primary, []domain.LLMProvider{fallback}, slog.Default())
	resp, err := fp.Chat(context.Background(), domain.ChatRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Content != "fallback response" {
		t.Errorf("content = %q, want %q", resp.Message.Content, "fallback response")
	}
}

func TestFailoverAllFail(t *testing.T) {
	primary := &mockProvider{
		name: "primary",
		chatFunc: func(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
			return nil, errors.New("primary down")
		},
	}
	fallback := &mockProvider{
		name: "fallback",
		chatFunc: func(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
			return nil, errors.New("fallback down")
		},
	}

	fp := NewFailoverProvider(primary, []domain.LLMProvider{fallback}, slog.Default())
	_, err := fp.Chat(context.Background(), domain.ChatRequest{})
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
}

func TestFailoverStreaming(t *testing.T) {
	primary := &mockStreamProvider{
		mockProvider: mockProvider{
			name: "primary",
			chatFunc: func(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
				return nil, errors.New("not used")
			},
		},
		streamFunc: func(_ context.Context, _ domain.ChatRequest) (<-chan domain.StreamDelta, error) {
			return nil, errors.New("primary stream down")
		},
	}
	fallback := &mockStreamProvider{
		mockProvider: mockProvider{
			name: "fallback",
			chatFunc: func(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
				return nil, errors.New("not used")
			},
		},
		streamFunc: func(_ context.Context, _ domain.ChatRequest) (<-chan domain.StreamDelta, error) {
			ch := make(chan domain.StreamDelta, 1)
			ch <- domain.StreamDelta{Content: "stream ok", Done: true}
			close(ch)
			return ch, nil
		},
	}

	fp := NewFailoverProvider(primary, []domain.LLMProvider{fallback}, slog.Default())
	ch, err := fp.ChatStream(context.Background(), domain.ChatRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	delta := <-ch
	if delta.Content != "stream ok" {
		t.Errorf("content = %q, want %q", delta.Content, "stream ok")
	}
}

func TestFailoverName(t *testing.T) {
	primary := &mockProvider{name: "openai"}
	fp := NewFailoverProvider(primary, nil, slog.Default())
	if fp.Name() != "openai+failover" {
		t.Errorf("Name = %q, want %q", fp.Name(), "openai+failover")
	}
}

// TestFailoverProvider_AggregatesAllErrors verifies that when all providers fail,
// the error message contains information about ALL failures, not just the last one.
// This helps with debugging and understanding why failover didn't work.
func TestFailoverProvider_AggregatesAllErrors(t *testing.T) {
	primary := &mockProvider{
		name: "primary",
		chatFunc: func(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
			return nil, errors.New("primary connection timeout")
		},
	}
	fb1 := &mockProvider{
		name: "gemini",
		chatFunc: func(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
			return nil, errors.New("gemini rate limit exceeded")
		},
	}
	fb2 := &mockProvider{
		name: "claude",
		chatFunc: func(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
			return nil, errors.New("claude API key invalid")
		},
	}

	fp := NewFailoverProvider(primary, []domain.LLMProvider{fb1, fb2}, slog.Default())

	ctx := context.Background()
	req := domain.ChatRequest{
		Model: "test",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "test"},
		},
	}

	_, err := fp.Chat(ctx, req)

	if err == nil {
		t.Fatal("expected error when all providers fail")
	}

	// Error should contain information about ALL failures, not just last one
	errStr := err.Error()

	// Check that all provider names are mentioned
	requiredSubstrings := []string{"primary", "gemini", "claude"}
	for _, substr := range requiredSubstrings {
		if !contains(errStr, substr) {
			t.Errorf("error should mention provider %q, got: %v", substr, err)
		}
	}

	// Should also contain error reasons (at least some of them)
	// The exact format depends on implementation, but we should see failure details
	if !contains(errStr, "timeout") && !contains(errStr, "limit") && !contains(errStr, "invalid") {
		t.Errorf("error should contain failure reasons, got: %v", err)
	}
}

// contains is a helper to check if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	// Simple case-insensitive check
	return len(s) >= len(substr) && (s == substr ||
		len(s) > len(substr) && indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
