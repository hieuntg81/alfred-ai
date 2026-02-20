package llm

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

func TestParseSSEStreamBasic(t *testing.T) {
	raw := "data: {\"text\":\"hello\"}\n\ndata: {\"text\":\"world\"}\n\ndata: [DONE]\n\n"
	body := io.NopCloser(strings.NewReader(raw))

	ch := parseSSEStream(context.Background(), body, func(data []byte) (*domain.StreamDelta, error) {
		// Simple parser: grab the "text" field.
		s := string(data)
		if strings.Contains(s, "hello") {
			return &domain.StreamDelta{Content: "hello"}, nil
		}
		if strings.Contains(s, "world") {
			return &domain.StreamDelta{Content: "world"}, nil
		}
		return nil, nil
	})

	var deltas []domain.StreamDelta
	for d := range ch {
		deltas = append(deltas, d)
	}

	// Expect: "hello", "world", and the [DONE] sentinel.
	if len(deltas) != 3 {
		t.Fatalf("expected 3 deltas, got %d", len(deltas))
	}
	if deltas[0].Content != "hello" {
		t.Errorf("delta[0] content = %q, want hello", deltas[0].Content)
	}
	if deltas[1].Content != "world" {
		t.Errorf("delta[1] content = %q, want world", deltas[1].Content)
	}
	if !deltas[2].Done {
		t.Error("expected final delta to be Done")
	}
}

func TestParseSSEStreamSkipsComments(t *testing.T) {
	raw := ": this is a comment\ndata: {\"text\":\"ok\"}\n\n"
	body := io.NopCloser(strings.NewReader(raw))

	ch := parseSSEStream(context.Background(), body, func(data []byte) (*domain.StreamDelta, error) {
		return &domain.StreamDelta{Content: "ok"}, nil
	})

	var deltas []domain.StreamDelta
	for d := range ch {
		deltas = append(deltas, d)
	}

	if len(deltas) != 1 || deltas[0].Content != "ok" {
		t.Fatalf("expected 1 delta with 'ok', got %v", deltas)
	}
}

func TestParseSSEStreamContextCancel(t *testing.T) {
	// Slow reader — should be cancelled
	pr, pw := io.Pipe()
	go func() {
		for i := 0; i < 100; i++ {
			pw.Write([]byte("data: {}\n\n"))
			time.Sleep(50 * time.Millisecond)
		}
		pw.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ch := parseSSEStream(ctx, pr, func(data []byte) (*domain.StreamDelta, error) {
		return &domain.StreamDelta{Content: "x"}, nil
	})

	var count int
	for range ch {
		count++
	}

	// Should have received some but not all 100
	if count >= 100 {
		t.Fatalf("expected context cancel to stop early, got %d", count)
	}
}

func TestParseSSEStreamParseError(t *testing.T) {
	// Invalid JSON should be skipped, valid lines should pass through
	raw := "data: INVALID\ndata: {\"text\":\"good\"}\n\n"
	body := io.NopCloser(strings.NewReader(raw))

	ch := parseSSEStream(context.Background(), body, func(data []byte) (*domain.StreamDelta, error) {
		if string(data) == "INVALID" {
			return nil, io.ErrUnexpectedEOF
		}
		return &domain.StreamDelta{Content: "good"}, nil
	})

	var deltas []domain.StreamDelta
	for d := range ch {
		deltas = append(deltas, d)
	}

	if len(deltas) != 1 || deltas[0].Content != "good" {
		t.Fatalf("expected 1 good delta, got %v", deltas)
	}
}
