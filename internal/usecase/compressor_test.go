package usecase

import (
	"context"
	"fmt"
	"testing"

	"alfred-ai/internal/domain"
)

func TestCompressorShouldCompress(t *testing.T) {
	llm := &mockLLM{responses: []domain.ChatResponse{}}
	c := NewCompressor(llm, CompressionConfig{Threshold: 5, KeepRecent: 2}, newTestLogger())

	tests := []struct {
		name     string
		msgCount int
		want     bool
	}{
		{"below threshold", 3, false},
		{"at threshold", 5, false},
		{"above threshold", 6, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSession("test")
			for i := 0; i < tt.msgCount; i++ {
				s.AddMessage(domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("msg %d", i)})
			}
			if got := c.ShouldCompress(s); got != tt.want {
				t.Errorf("ShouldCompress() = %v, want %v (count=%d, threshold=%d)", got, tt.want, tt.msgCount, 5)
			}
		})
	}
}

func TestCompressorCompress(t *testing.T) {
	llm := &mockLLM{
		responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "Summary of earlier conversation."}},
		},
	}
	c := NewCompressor(llm, CompressionConfig{Threshold: 5, KeepRecent: 3}, newTestLogger())

	s := NewSession("test")
	for i := 0; i < 10; i++ {
		s.AddMessage(domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("msg %d", i)})
	}

	err := c.Compress(context.Background(), s)
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}

	msgs := s.Messages()
	// Should be: 1 summary + 3 recent = 4
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages after compression, got %d", len(msgs))
	}

	// First message should be the summary
	if msgs[0].Name != compressSummaryName {
		t.Errorf("first message name = %q, want %q", msgs[0].Name, compressSummaryName)
	}
	if msgs[0].Content != "Summary of earlier conversation." {
		t.Errorf("summary content = %q", msgs[0].Content)
	}

	// Last 3 should be the recent messages
	if msgs[1].Content != "msg 7" {
		t.Errorf("first recent = %q, want %q", msgs[1].Content, "msg 7")
	}
	if msgs[3].Content != "msg 9" {
		t.Errorf("last recent = %q, want %q", msgs[3].Content, "msg 9")
	}
}

func TestCompressorShortConversation(t *testing.T) {
	llm := &mockLLM{responses: []domain.ChatResponse{}}
	c := NewCompressor(llm, CompressionConfig{Threshold: 30, KeepRecent: 10}, newTestLogger())

	s := NewSession("test")
	for i := 0; i < 5; i++ {
		s.AddMessage(domain.Message{Role: domain.RoleUser, Content: "msg"})
	}

	err := c.Compress(context.Background(), s)
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}

	// No compression should happen
	if s.MessageCount() != 5 {
		t.Errorf("expected 5 messages (no compression), got %d", s.MessageCount())
	}
}

func TestCompressorLLMError(t *testing.T) {
	llm := &mockLLM{responses: []domain.ChatResponse{}} // empty = will use fallback
	c := NewCompressor(llm, CompressionConfig{Threshold: 3, KeepRecent: 2}, newTestLogger())

	s := NewSession("test")
	for i := 0; i < 5; i++ {
		s.AddMessage(domain.Message{Role: domain.RoleUser, Content: "msg"})
	}

	// The mock returns "fallback" which is not an error, so compression proceeds
	err := c.Compress(context.Background(), s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSessionMessageCount(t *testing.T) {
	s := NewSession("test")
	if s.MessageCount() != 0 {
		t.Errorf("empty session count = %d", s.MessageCount())
	}

	s.AddMessage(domain.Message{Role: domain.RoleUser, Content: "a"})
	s.AddMessage(domain.Message{Role: domain.RoleUser, Content: "b"})

	if s.MessageCount() != 2 {
		t.Errorf("count = %d, want 2", s.MessageCount())
	}
}

func TestNewCompressorDefaults(t *testing.T) {
	llm := &mockLLM{}
	c := NewCompressor(llm, CompressionConfig{Threshold: 0, KeepRecent: 0}, newTestLogger())
	if c.config.Threshold != 30 {
		t.Errorf("default Threshold = %d, want 30", c.config.Threshold)
	}
	if c.config.KeepRecent != 10 {
		t.Errorf("default KeepRecent = %d, want 10", c.config.KeepRecent)
	}
}

func TestNewCompressorCustom(t *testing.T) {
	llm := &mockLLM{}
	c := NewCompressor(llm, CompressionConfig{Threshold: 50, KeepRecent: 20}, newTestLogger())
	if c.config.Threshold != 50 {
		t.Errorf("Threshold = %d, want 50", c.config.Threshold)
	}
	if c.config.KeepRecent != 20 {
		t.Errorf("KeepRecent = %d, want 20", c.config.KeepRecent)
	}
}

func TestSessionCompressMessages(t *testing.T) {
	s := NewSession("test")
	for i := 0; i < 10; i++ {
		s.AddMessage(domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("msg %d", i)})
	}

	s.CompressMessages("summary text", 3)

	msgs := s.Messages()
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	if msgs[0].Name != compressSummaryName {
		t.Errorf("summary name = %q", msgs[0].Name)
	}
	if msgs[0].Content != "summary text" {
		t.Errorf("summary content = %q", msgs[0].Content)
	}
}

func TestCompressorCompressAllKeepRecent(t *testing.T) {
	llm := &mockLLM{}
	c := NewCompressor(llm, CompressionConfig{Threshold: 3, KeepRecent: 10}, newTestLogger())

	s := NewSession("test")
	for i := 0; i < 5; i++ {
		s.AddMessage(domain.Message{Role: domain.RoleUser, Content: "msg"})
	}

	// 5 messages > threshold 3, but 5 <= keepRecent 10, so no compression
	err := c.Compress(context.Background(), s)
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if s.MessageCount() != 5 {
		t.Errorf("expected 5 messages, got %d", s.MessageCount())
	}
}

func TestCompressorCompressSystemOnlyMessages(t *testing.T) {
	llm := &mockLLM{responses: []domain.ChatResponse{
		{Message: domain.Message{Role: domain.RoleAssistant, Content: "summary"}},
	}}
	c := NewCompressor(llm, CompressionConfig{Threshold: 3, KeepRecent: 2}, newTestLogger())

	s := NewSession("test")
	// Add only system messages (which are skipped in conv text)
	for i := 0; i < 10; i++ {
		s.AddMessage(domain.Message{Role: domain.RoleSystem, Content: "system msg"})
	}

	err := c.Compress(context.Background(), s)
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
}

func TestCompressorLLMReturnsError(t *testing.T) {
	llm := &errorLLM{}
	c := NewCompressor(llm, CompressionConfig{Threshold: 3, KeepRecent: 2}, newTestLogger())

	s := NewSession("test")
	for i := 0; i < 10; i++ {
		s.AddMessage(domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("msg %d", i)})
	}

	err := c.Compress(context.Background(), s)
	if err == nil {
		t.Error("expected error from LLM failure")
	}
}

func TestCompressorLLMReturnsEmptySummary(t *testing.T) {
	llm := &mockLLM{responses: []domain.ChatResponse{
		{Message: domain.Message{Role: domain.RoleAssistant, Content: "   "}},
	}}
	c := NewCompressor(llm, CompressionConfig{Threshold: 3, KeepRecent: 2}, newTestLogger())

	s := NewSession("test")
	for i := 0; i < 10; i++ {
		s.AddMessage(domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("msg %d", i)})
	}

	err := c.Compress(context.Background(), s)
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	// Should not compress because summary is empty
	if s.MessageCount() != 10 {
		t.Errorf("expected 10 messages (no compression due to empty summary), got %d", s.MessageCount())
	}
}
