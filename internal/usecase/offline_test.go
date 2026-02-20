package usecase

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

func TestMessageQueue_EnqueueAndDrain(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	q := NewMessageQueue(dir)

	msg1 := QueuedMessage{
		SessionID: "s1",
		Content:   "Hello offline",
		Sender:    "user1",
		Channel:   "cli",
		QueuedAt:  time.Now(),
	}
	msg2 := QueuedMessage{
		SessionID: "s2",
		Content:   "Another message",
		Sender:    "user2",
		Channel:   "cli",
		QueuedAt:  time.Now(),
	}

	if err := q.Enqueue(msg1); err != nil {
		t.Fatalf("Enqueue msg1: %v", err)
	}
	if err := q.Enqueue(msg2); err != nil {
		t.Fatalf("Enqueue msg2: %v", err)
	}

	if q.Len() != 2 {
		t.Errorf("Len = %d, want 2", q.Len())
	}

	msgs, err := q.Drain()
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("Drain count = %d, want 2", len(msgs))
	}

	// Queue should be empty after drain.
	if q.Len() != 0 {
		t.Errorf("Len after drain = %d, want 0", q.Len())
	}
}

func TestMessageQueue_DrainEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "empty-queue")
	q := NewMessageQueue(dir)

	msgs, err := q.Drain()
	if err != nil {
		t.Fatalf("Drain empty: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("Drain count = %d, want 0", len(msgs))
	}
}

func TestMessageQueue_DrainNonExistentDir(t *testing.T) {
	q := NewMessageQueue("/nonexistent/path/that/does/not/exist")

	msgs, err := q.Drain()
	if err != nil {
		t.Fatalf("Drain nonexistent: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("Drain count = %d, want 0", len(msgs))
	}
}

func TestMessageQueue_Len(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue-len")
	q := NewMessageQueue(dir)

	if q.Len() != 0 {
		t.Errorf("initial Len = %d, want 0", q.Len())
	}

	q.Enqueue(QueuedMessage{SessionID: "s1", Content: "test", QueuedAt: time.Now()})
	if q.Len() != 1 {
		t.Errorf("Len = %d, want 1", q.Len())
	}

	// Add a subdirectory (should not be counted).
	os.MkdirAll(filepath.Join(dir, "subdir"), 0700)
	if q.Len() != 1 {
		t.Errorf("Len with subdir = %d, want 1", q.Len())
	}
}

func TestMessageQueue_MessageRoundtrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue-rt")
	q := NewMessageQueue(dir)

	original := QueuedMessage{
		SessionID: "session-123",
		Content:   "Test content with unicode: café ñ",
		Sender:    "test-user",
		Channel:   "gateway",
		QueuedAt:  time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
	}
	if err := q.Enqueue(original); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	msgs, err := q.Drain()
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("Drain count = %d, want 1", len(msgs))
	}

	got := msgs[0]
	if got.SessionID != original.SessionID {
		t.Errorf("SessionID = %q, want %q", got.SessionID, original.SessionID)
	}
	if got.Content != original.Content {
		t.Errorf("Content = %q, want %q", got.Content, original.Content)
	}
	if got.Sender != original.Sender {
		t.Errorf("Sender = %q, want %q", got.Sender, original.Sender)
	}
	if got.Channel != original.Channel {
		t.Errorf("Channel = %q, want %q", got.Channel, original.Channel)
	}
}

// --- OfflineManager tests ---

// mockLLMProvider is a fake LLM provider for testing.
type mockLLMProvider struct {
	response string
	err      error
}

func (m *mockLLMProvider) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &domain.ChatResponse{
		Message: domain.Message{Role: domain.RoleAssistant, Content: m.response},
	}, nil
}

func (m *mockLLMProvider) Name() string { return "mock-local" }

func TestOfflineManager_IsOnlineDefault(t *testing.T) {
	om := NewOfflineManager(&mockLLMProvider{}, t.TempDir(), "https://example.com", time.Minute, slog.Default())
	if !om.IsOnline() {
		t.Error("expected IsOnline=true by default")
	}
}

func TestOfflineManager_HandleOffline(t *testing.T) {
	qDir := filepath.Join(t.TempDir(), "q")
	llm := &mockLLMProvider{response: "I'm running locally"}
	om := NewOfflineManager(llm, qDir, "https://example.com", time.Minute, slog.Default())

	msg := domain.InboundMessage{
		Content:     "Hello",
		SenderName:  "alice",
		ChannelName: "cli",
	}

	resp, err := om.HandleOffline(context.Background(), "sess-1", msg)
	if err != nil {
		t.Fatalf("HandleOffline: %v", err)
	}
	if resp != "I'm running locally" {
		t.Errorf("response = %q, want %q", resp, "I'm running locally")
	}

	// Message should be queued.
	if om.queue.Len() != 1 {
		t.Errorf("queue Len = %d, want 1", om.queue.Len())
	}
}

func TestOfflineManager_HandleOfflineLLMError(t *testing.T) {
	qDir := filepath.Join(t.TempDir(), "q")
	llm := &mockLLMProvider{err: errors.New("local model crashed")}
	om := NewOfflineManager(llm, qDir, "https://example.com", time.Minute, slog.Default())

	_, err := om.HandleOffline(context.Background(), "sess-1", domain.InboundMessage{Content: "test"})
	if err == nil {
		t.Fatal("expected error from HandleOffline when LLM fails")
	}
	if !errors.Is(err, errors.Unwrap(err)) {
		// Just verify it wraps the error.
		if err.Error() == "" {
			t.Error("error should have a message")
		}
	}
}

func TestOfflineManager_Sync(t *testing.T) {
	qDir := filepath.Join(t.TempDir(), "q")
	llm := &mockLLMProvider{response: "ok"}
	om := NewOfflineManager(llm, qDir, "https://example.com", time.Minute, slog.Default())

	// Enqueue some messages first.
	om.queue.Enqueue(QueuedMessage{SessionID: "s1", Content: "msg1", QueuedAt: time.Now()})
	om.queue.Enqueue(QueuedMessage{SessionID: "s2", Content: "msg2", QueuedAt: time.Now()})

	if err := om.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Queue should be empty after sync.
	if om.queue.Len() != 0 {
		t.Errorf("queue Len after Sync = %d, want 0", om.queue.Len())
	}
}

func TestOfflineManager_SyncEmpty(t *testing.T) {
	qDir := filepath.Join(t.TempDir(), "q")
	om := NewOfflineManager(&mockLLMProvider{}, qDir, "https://example.com", time.Minute, slog.Default())

	// Sync on empty queue should succeed.
	if err := om.Sync(context.Background()); err != nil {
		t.Fatalf("Sync empty: %v", err)
	}
}
