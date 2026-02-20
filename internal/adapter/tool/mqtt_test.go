package tool

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"
)

func testLogger(_ *testing.T) *slog.Logger { return slog.Default() }

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustJSON: %v", err)
	}
	return data
}

func TestMQTTTool_Publish(t *testing.T) {
	backend := NewMockMQTTBackend()
	tool := NewMQTTTool(backend, testLogger(t))

	result, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action":  "publish",
		"topic":   "sensors/temperature",
		"payload": `{"value": 22.5}`,
		"qos":     1,
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	pubs := backend.Published()
	if len(pubs) != 1 {
		t.Fatalf("published count = %d, want 1", len(pubs))
	}
	if pubs[0].Topic != "sensors/temperature" {
		t.Errorf("topic = %q, want %q", pubs[0].Topic, "sensors/temperature")
	}
	if pubs[0].QoS != 1 {
		t.Errorf("qos = %d, want 1", pubs[0].QoS)
	}
}

func TestMQTTTool_PublishMissingTopic(t *testing.T) {
	backend := NewMockMQTTBackend()
	tool := NewMQTTTool(backend, testLogger(t))

	result, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action":  "publish",
		"payload": "data",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing topic")
	}
}

func TestMQTTTool_SubscribeAndRead(t *testing.T) {
	backend := NewMockMQTTBackend()
	tool := NewMQTTTool(backend, testLogger(t))

	// Subscribe.
	result, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "subscribe",
		"topic":  "test/topic",
	}))
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if result.IsError {
		t.Fatalf("subscribe error: %s", result.Content)
	}

	// Publish a message (will be delivered to subscriber).
	backend.Publish(context.Background(), "test/topic", []byte("hello"), 0, false)

	// Wait a bit for the goroutine to buffer.
	time.Sleep(50 * time.Millisecond)

	// Read messages.
	result, err = tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "read",
		"topic":  "test/topic",
	}))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if result.IsError {
		t.Fatalf("read error: %s", result.Content)
	}

	var msgs []MQTTMessage
	if err := json.Unmarshal([]byte(result.Content), &msgs); err != nil {
		t.Fatalf("unmarshal read result: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected at least 1 message from read")
	}
	if msgs[0].Payload != "hello" {
		t.Errorf("payload = %q, want %q", msgs[0].Payload, "hello")
	}
}

func TestMQTTTool_ReadEmpty(t *testing.T) {
	backend := NewMockMQTTBackend()
	tool := NewMQTTTool(backend, testLogger(t))

	result, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "read",
		"topic":  "empty/topic",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != `No messages on "empty/topic"` {
		t.Errorf("content = %q", result.Content)
	}
}

func TestMQTTTool_Unsubscribe(t *testing.T) {
	backend := NewMockMQTTBackend()
	tool := NewMQTTTool(backend, testLogger(t))

	// Subscribe first.
	tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "subscribe",
		"topic":  "unsub/topic",
	}))

	subs := backend.ListSubscriptions()
	if len(subs) != 1 {
		t.Fatalf("subscriptions before = %d, want 1", len(subs))
	}

	// Unsubscribe.
	result, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "unsubscribe",
		"topic":  "unsub/topic",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unsubscribe error: %s", result.Content)
	}

	subs = backend.ListSubscriptions()
	if len(subs) != 0 {
		t.Errorf("subscriptions after = %d, want 0", len(subs))
	}
}

func TestMQTTTool_ListSubscriptions(t *testing.T) {
	backend := NewMockMQTTBackend()
	tool := NewMQTTTool(backend, testLogger(t))

	// No subscriptions.
	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "list_subscriptions",
	}))
	if result.Content != "No active subscriptions" {
		t.Errorf("expected no subscriptions, got %q", result.Content)
	}

	// Subscribe to two topics.
	tool.Execute(context.Background(), mustJSON(t, map[string]any{"action": "subscribe", "topic": "a"}))
	tool.Execute(context.Background(), mustJSON(t, map[string]any{"action": "subscribe", "topic": "b"}))

	result, _ = tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "list_subscriptions",
	}))
	var topics []string
	if err := json.Unmarshal([]byte(result.Content), &topics); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(topics) != 2 {
		t.Errorf("topic count = %d, want 2", len(topics))
	}
}

func TestMQTTTool_UnknownAction(t *testing.T) {
	backend := NewMockMQTTBackend()
	tool := NewMQTTTool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "bogus",
	}))
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

func TestMQTTTool_InvalidParams(t *testing.T) {
	backend := NewMockMQTTBackend()
	tool := NewMQTTTool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), json.RawMessage(`{invalid json`))
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}
