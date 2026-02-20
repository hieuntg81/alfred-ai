package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"alfred-ai/internal/domain"
)

// mockTokenCounter allows controlling token counts in tests.
type mockTokenCounter struct {
	countMessages int
	countText     int
}

func (m *mockTokenCounter) CountMessages(_ []domain.Message) int { return m.countMessages }
func (m *mockTokenCounter) CountText(_ string) int               { return m.countText }

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestContextGuardCheckUnderLimit(t *testing.T) {
	counter := &mockTokenCounter{countMessages: 1000}
	guard := NewContextGuard(ContextGuardConfig{
		MaxTokens:     128000,
		ReserveTokens: 1000,
		SafetyMargin:  0.15,
	}, counter, nil, testLogger())

	session := NewSession("test")
	session.AddMessage(domain.Message{Role: "user", Content: "hello"})

	err := guard.Check(context.Background(), session)
	if err != nil {
		t.Fatalf("expected no error for under-limit session, got: %v", err)
	}
}

func TestContextGuardCheckOverLimitNoCompressor(t *testing.T) {
	// Token count exceeds limit, no compressor available → ErrContextOverflow.
	counter := &mockTokenCounter{countMessages: 200000}
	guard := NewContextGuard(ContextGuardConfig{
		MaxTokens:     128000,
		ReserveTokens: 1000,
		SafetyMargin:  0.15,
	}, counter, nil, testLogger())

	session := NewSession("test")
	session.AddMessage(domain.Message{Role: "user", Content: "hello"})

	err := guard.Check(context.Background(), session)
	if !errors.Is(err, domain.ErrContextOverflow) {
		t.Fatalf("expected ErrContextOverflow, got: %v", err)
	}
}

func TestContextGuardCheckOverLimitCompressionSucceeds(t *testing.T) {
	// Token count exceeds limit initially, but after compression goes below.
	callCount := 0
	counter := &mockTokenCounter{}

	// First call: over limit (200000). After compression: under limit (5000).
	origCountMessages := counter.countMessages
	_ = origCountMessages

	// We need a counter that returns different values on successive calls.
	// Use a wrapper.
	dynamicCounter := &dynamicMockTokenCounter{
		counts: []int{200000, 5000},
	}

	// We need a mock compressor. Create a real one with a mock LLM.
	mockLLM := &mockLLMForCompressor{}
	compressor := NewCompressor(mockLLM, CompressionConfig{
		Enabled:    true,
		Threshold:  1,
		KeepRecent: 1,
	}, testLogger())

	guard := NewContextGuard(ContextGuardConfig{
		MaxTokens:     128000,
		ReserveTokens: 1000,
		SafetyMargin:  0.15,
	}, dynamicCounter, compressor, testLogger())

	session := NewSession("test")
	// Add enough messages for compression to work.
	session.AddMessage(domain.Message{Role: "user", Content: "old message 1"})
	session.AddMessage(domain.Message{Role: "assistant", Content: "old response 1"})
	session.AddMessage(domain.Message{Role: "user", Content: "recent message"})

	err := guard.Check(context.Background(), session)
	if err != nil {
		t.Fatalf("expected nil error after successful compression, got: %v", err)
	}
	_ = callCount
}

func TestContextGuardCheckOverLimitCompressionFails(t *testing.T) {
	// Token count exceeds limit, compression runs but tokens still over → ErrContextOverflow.
	dynamicCounter := &dynamicMockTokenCounter{
		counts: []int{200000, 200000}, // still over after compression
	}

	mockLLM := &mockLLMForCompressor{}
	compressor := NewCompressor(mockLLM, CompressionConfig{
		Enabled:    true,
		Threshold:  1,
		KeepRecent: 1,
	}, testLogger())

	guard := NewContextGuard(ContextGuardConfig{
		MaxTokens:     128000,
		ReserveTokens: 1000,
		SafetyMargin:  0.15,
	}, dynamicCounter, compressor, testLogger())

	session := NewSession("test")
	session.AddMessage(domain.Message{Role: "user", Content: "old message"})
	session.AddMessage(domain.Message{Role: "assistant", Content: "old response"})
	session.AddMessage(domain.Message{Role: "user", Content: "recent"})

	err := guard.Check(context.Background(), session)
	if !errors.Is(err, domain.ErrContextOverflow) {
		t.Fatalf("expected ErrContextOverflow, got: %v", err)
	}
}

func TestContextGuardNilIsNoOp(t *testing.T) {
	// A nil ContextGuard pointer shouldn't be called, but verify the pattern
	// used in agent.go: `if guard != nil { guard.Check(...) }`.
	var guard *ContextGuard
	if guard != nil {
		t.Fatal("nil guard should not pass nil check")
	}
}

func TestContextGuardDefaults(t *testing.T) {
	counter := &mockTokenCounter{countMessages: 100}
	guard := NewContextGuard(ContextGuardConfig{}, counter, nil, testLogger())

	if guard.maxTokens != 128000 {
		t.Errorf("default maxTokens = %d, want 128000", guard.maxTokens)
	}
	if guard.reserveTokens != 1000 {
		t.Errorf("default reserveTokens = %d, want 1000", guard.reserveTokens)
	}
	if guard.safetyMargin != 0.15 {
		t.Errorf("default safetyMargin = %f, want 0.15", guard.safetyMargin)
	}
}

func TestContextGuardExactlyAtLimit(t *testing.T) {
	// Token count exactly at the limit should pass.
	// limit = 128000 * 0.85 - 1000 = 107800
	counter := &mockTokenCounter{countMessages: 107800}
	guard := NewContextGuard(ContextGuardConfig{
		MaxTokens:     128000,
		ReserveTokens: 1000,
		SafetyMargin:  0.15,
	}, counter, nil, testLogger())

	session := NewSession("test")
	session.AddMessage(domain.Message{Role: "user", Content: "hello"})

	err := guard.Check(context.Background(), session)
	if err != nil {
		t.Fatalf("expected no error at exact limit, got: %v", err)
	}
}

func TestContextGuardOneOverLimit(t *testing.T) {
	// limit = 128000 * 0.85 - 1000 = 107800
	counter := &mockTokenCounter{countMessages: 107801}
	guard := NewContextGuard(ContextGuardConfig{
		MaxTokens:     128000,
		ReserveTokens: 1000,
		SafetyMargin:  0.15,
	}, counter, nil, testLogger())

	session := NewSession("test")
	session.AddMessage(domain.Message{Role: "user", Content: "hello"})

	err := guard.Check(context.Background(), session)
	if !errors.Is(err, domain.ErrContextOverflow) {
		t.Fatalf("expected ErrContextOverflow for 1 over limit, got: %v", err)
	}
}

// dynamicMockTokenCounter returns different values on successive CountMessages calls.
type dynamicMockTokenCounter struct {
	counts []int
	idx    int
}

func (d *dynamicMockTokenCounter) CountMessages(_ []domain.Message) int {
	if d.idx >= len(d.counts) {
		return d.counts[len(d.counts)-1]
	}
	v := d.counts[d.idx]
	d.idx++
	return v
}

func (d *dynamicMockTokenCounter) CountText(text string) int {
	return len(text) / 3
}

// mockLLMForCompressor satisfies domain.LLMProvider for compressor tests.
type mockLLMForCompressor struct{}

func (m *mockLLMForCompressor) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	return &domain.ChatResponse{
		Message: domain.Message{
			Role:    "assistant",
			Content: "Compressed summary of the conversation.",
		},
		Usage: domain.Usage{TotalTokens: 10},
	}, nil
}

func (m *mockLLMForCompressor) Name() string { return "mock" }

func TestContextGuardSafetyMarginClamped(t *testing.T) {
	counter := &mockTokenCounter{countMessages: 100}

	// SafetyMargin 1.5 should be clamped to 0.5
	guard := NewContextGuard(ContextGuardConfig{
		MaxTokens:     128000,
		ReserveTokens: 1000,
		SafetyMargin:  1.5,
	}, counter, nil, testLogger())

	if guard.safetyMargin != 0.5 {
		t.Errorf("safetyMargin = %f, want 0.5 (clamped)", guard.safetyMargin)
	}
}

func TestContextGuardSafetyMarginNegative(t *testing.T) {
	counter := &mockTokenCounter{countMessages: 100}

	// Negative SafetyMargin should default to 0.15
	guard := NewContextGuard(ContextGuardConfig{
		MaxTokens:     128000,
		ReserveTokens: 1000,
		SafetyMargin:  -0.5,
	}, counter, nil, testLogger())

	if guard.safetyMargin != 0.15 {
		t.Errorf("safetyMargin = %f, want 0.15 (defaulted)", guard.safetyMargin)
	}
}

func TestContextGuardReserveTokensExceedMax(t *testing.T) {
	// If reserveTokens > maxTokens, the limit formula still works
	// (it just means everything is "over limit").
	counter := &mockTokenCounter{countMessages: 1}
	guard := NewContextGuard(ContextGuardConfig{
		MaxTokens:     1000,
		ReserveTokens: 2000,
		SafetyMargin:  0.15,
	}, counter, nil, testLogger())

	session := NewSession("test")
	session.AddMessage(domain.Message{Role: "user", Content: "hello"})

	// limit = 1000 * 0.85 - 2000 = -1150 → even 1 token triggers overflow
	err := guard.Check(context.Background(), session)
	if !errors.Is(err, domain.ErrContextOverflow) {
		t.Fatalf("expected ErrContextOverflow when reserve > max, got: %v", err)
	}
}

func TestContextGuardCompressionErrorWrapped(t *testing.T) {
	// When compression fails, the error should wrap both the compression
	// error and ErrContextOverflow.
	dynamicCounter := &dynamicMockTokenCounter{
		counts: []int{200000}, // only called once (no re-check after failed compression)
	}

	// Use a mock LLM that returns an error for compression.
	failingLLM := &failingCompressorLLM{}
	compressor := NewCompressor(failingLLM, CompressionConfig{
		Enabled:    true,
		Threshold:  1,
		KeepRecent: 1,
	}, testLogger())

	guard := NewContextGuard(ContextGuardConfig{
		MaxTokens:     128000,
		ReserveTokens: 1000,
		SafetyMargin:  0.15,
	}, dynamicCounter, compressor, testLogger())

	session := NewSession("test")
	session.AddMessage(domain.Message{Role: "user", Content: "old message"})
	session.AddMessage(domain.Message{Role: "assistant", Content: "old response"})
	session.AddMessage(domain.Message{Role: "user", Content: "recent"})

	err := guard.Check(context.Background(), session)
	if !errors.Is(err, domain.ErrContextOverflow) {
		t.Fatalf("expected ErrContextOverflow, got: %v", err)
	}
	if err == nil || err.Error() == domain.ErrContextOverflow.Error() {
		t.Error("expected wrapped error with compression failure details")
	}
}

type failingCompressorLLM struct{}

func (m *failingCompressorLLM) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	return nil, fmt.Errorf("llm api error")
}

func (m *failingCompressorLLM) Name() string { return "failing" }
