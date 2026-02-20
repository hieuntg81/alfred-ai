package usecase

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"alfred-ai/internal/domain"
)

// silentLogger returns a logger that discards all output (avoids noise in benchmarks).
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// instantLLM returns a fixed summary instantly (no network latency).
type instantLLM struct {
	summary string
}

func (m *instantLLM) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	return &domain.ChatResponse{
		Message: domain.Message{Role: domain.RoleAssistant, Content: m.summary},
	}, nil
}
func (m *instantLLM) Name() string { return "instant" }

// seedSession fills a session with n user/assistant message pairs.
func seedSession(n int) *Session {
	s := NewSession("bench")
	for i := 0; i < n; i++ {
		role := domain.RoleUser
		if i%2 == 1 {
			role = domain.RoleAssistant
		}
		s.AddMessage(domain.Message{
			Role:    role,
			Content: fmt.Sprintf("Message %d with some realistic content that would appear in a conversation about programming, debugging, or general assistance tasks.", i),
		})
	}
	return s
}

// --- ShouldCompress ---

func BenchmarkShouldCompress(b *testing.B) {
	sizes := []int{10, 30, 50, 100, 500}

	for _, n := range sizes {
		b.Run(fmt.Sprintf("msgs_%d", n), func(b *testing.B) {
			session := seedSession(n)
			comp := NewCompressor(&instantLLM{}, CompressionConfig{Threshold: 30, KeepRecent: 10}, silentLogger())

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				comp.ShouldCompress(session)
			}
		})
	}
}

// --- CompressMessages (session manipulation only, no LLM) ---

func BenchmarkCompressMessages(b *testing.B) {
	sizes := []int{30, 50, 100, 200, 500}
	keepRecent := 10

	for _, n := range sizes {
		b.Run(fmt.Sprintf("msgs_%d_keep_%d", n, keepRecent), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				session := seedSession(n)
				b.StartTimer()
				session.CompressMessages("Summary of the conversation so far.", keepRecent)
			}
		})
	}
}

// --- Full Compress (with instant LLM mock) ---
// Measures all overhead EXCEPT actual LLM latency:
// - Message copy
// - String building for conversation text
// - Session manipulation

func BenchmarkCompress(b *testing.B) {
	sizes := []int{30, 50, 100, 200, 500}

	for _, n := range sizes {
		b.Run(fmt.Sprintf("msgs_%d", n), func(b *testing.B) {
			llm := &instantLLM{summary: "Summary of earlier discussion about programming topics."}
			comp := NewCompressor(llm, CompressionConfig{Threshold: 1, KeepRecent: 10}, silentLogger())
			ctx := context.Background()

			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				session := seedSession(n)
				b.StartTimer()
				comp.Compress(ctx, session)
			}
		})
	}
}

// --- ForceCompress (used by error recovery) ---

func BenchmarkForceCompress(b *testing.B) {
	sizes := []int{30, 100, 500}

	for _, n := range sizes {
		b.Run(fmt.Sprintf("msgs_%d", n), func(b *testing.B) {
			llm := &instantLLM{summary: "Compressed conversation summary."}
			comp := NewCompressor(llm, CompressionConfig{Threshold: 9999, KeepRecent: 10}, silentLogger())
			ctx := context.Background()

			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				session := seedSession(n)
				b.StartTimer()
				comp.ForceCompress(ctx, session)
			}
		})
	}
}

// --- Conversation Text Building (isolated) ---
// Measures just the string-building overhead that grows with message count.

func BenchmarkConversationTextBuilding(b *testing.B) {
	sizes := []int{50, 100, 200, 500, 1000}

	for _, n := range sizes {
		b.Run(fmt.Sprintf("msgs_%d", n), func(b *testing.B) {
			session := seedSession(n)
			msgs := session.Messages()

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				buildConversationText(msgs)
			}
		})
	}
}

// buildConversationText mirrors the compressor's internal string building.
func buildConversationText(msgs []domain.Message) string {
	// Estimate: ~150 bytes per message
	buf := make([]byte, 0, len(msgs)*150)
	for _, msg := range msgs {
		if msg.Role == domain.RoleSystem {
			continue
		}
		buf = append(buf, msg.Role...)
		buf = append(buf, ':', ' ')
		buf = append(buf, msg.Content...)
		buf = append(buf, '\n')
	}
	return string(buf)
}
