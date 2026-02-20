package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"alfred-ai/internal/domain"
)

// mockChannel implements domain.Channel for MessageTool testing.
type mockChannel struct {
	name    string
	mu      sync.Mutex
	sent    []domain.OutboundMessage
	sendErr error
}

func (m *mockChannel) Start(context.Context, domain.MessageHandler) error { return nil }
func (m *mockChannel) Stop(context.Context) error                         { return nil }
func (m *mockChannel) Name() string                                       { return m.name }
func (m *mockChannel) Send(_ context.Context, msg domain.OutboundMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sent = append(m.sent, msg)
	return nil
}

func (m *mockChannel) sentMessages() []domain.OutboundMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]domain.OutboundMessage, len(m.sent))
	copy(cp, m.sent)
	return cp
}

func newTestMessageTool(channels ...domain.Channel) *MessageTool {
	reg := NewChannelRegistry(channels, newTestLogger())
	return NewMessageTool(reg, newTestLogger())
}

func execMessageTool(t *testing.T, mt *MessageTool, params any) *domain.ToolResult {
	t.Helper()
	data, _ := json.Marshal(params)
	result, err := mt.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return result
}

// --- Name / Description / Schema ---

func TestMessageToolName(t *testing.T) {
	mt := newTestMessageTool()
	if mt.Name() != "message" {
		t.Errorf("got %q, want %q", mt.Name(), "message")
	}
}

func TestMessageToolDescription(t *testing.T) {
	mt := newTestMessageTool()
	if mt.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestMessageToolSchema(t *testing.T) {
	mt := newTestMessageTool()
	schema := mt.Schema()
	if schema.Name != "message" {
		t.Errorf("schema name %q, want %q", schema.Name, "message")
	}

	var raw map[string]any
	if err := json.Unmarshal(schema.Parameters, &raw); err != nil {
		t.Fatalf("invalid schema JSON: %v", err)
	}
	props, ok := raw["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema missing properties")
	}
	for _, key := range []string{"action", "channel", "channels", "target", "targets", "content", "thread_id", "reply_to_id", "metadata"} {
		if _, ok := props[key]; !ok {
			t.Errorf("schema missing property %q", key)
		}
	}
}

// --- Send action ---

func TestMessageToolSend(t *testing.T) {
	tg := &mockChannel{name: "telegram"}
	mt := newTestMessageTool(tg)

	result := execMessageTool(t, mt, map[string]any{
		"action":  "send",
		"channel": "telegram",
		"target":  "12345",
		"content": "hello world",
	})

	if result.IsError {
		t.Fatalf("send failed: %s", result.Content)
	}

	msgs := tg.sentMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(msgs))
	}
	if msgs[0].SessionID != "12345" {
		t.Errorf("target %q, want %q", msgs[0].SessionID, "12345")
	}
	if msgs[0].Content != "hello world" {
		t.Errorf("content %q, want %q", msgs[0].Content, "hello world")
	}
}

func TestMessageToolSendWithThreadID(t *testing.T) {
	tg := &mockChannel{name: "telegram"}
	mt := newTestMessageTool(tg)

	result := execMessageTool(t, mt, map[string]any{
		"action":      "send",
		"channel":     "telegram",
		"target":      "123",
		"content":     "threaded msg",
		"thread_id":   "t-100",
		"reply_to_id": "m-200",
	})

	if result.IsError {
		t.Fatalf("send failed: %s", result.Content)
	}

	msgs := tg.sentMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].ThreadID != "t-100" {
		t.Errorf("thread_id %q, want %q", msgs[0].ThreadID, "t-100")
	}
	if msgs[0].ReplyToID != "m-200" {
		t.Errorf("reply_to_id %q, want %q", msgs[0].ReplyToID, "m-200")
	}
}

func TestMessageToolSendMissingChannel(t *testing.T) {
	mt := newTestMessageTool(&mockChannel{name: "telegram"})
	result := execMessageTool(t, mt, map[string]any{
		"action":  "send",
		"content": "hello",
	})
	if !result.IsError {
		t.Error("expected error for missing channel")
	}
}

func TestMessageToolSendMissingContent(t *testing.T) {
	mt := newTestMessageTool(&mockChannel{name: "telegram"})
	result := execMessageTool(t, mt, map[string]any{
		"action":  "send",
		"channel": "telegram",
	})
	if !result.IsError {
		t.Error("expected error for missing content")
	}
}

func TestMessageToolSendChannelNotFound(t *testing.T) {
	mt := newTestMessageTool(&mockChannel{name: "telegram"})
	result := execMessageTool(t, mt, map[string]any{
		"action":  "send",
		"channel": "slack",
		"content": "hello",
	})
	if !result.IsError {
		t.Error("expected error for channel not found")
	}
}

func TestMessageToolSendFailure(t *testing.T) {
	tg := &mockChannel{name: "telegram", sendErr: fmt.Errorf("network error")}
	mt := newTestMessageTool(tg)
	result := execMessageTool(t, mt, map[string]any{
		"action":  "send",
		"channel": "telegram",
		"target":  "123",
		"content": "hello",
	})
	if !result.IsError {
		t.Error("expected error for send failure")
	}
	if !strings.Contains(result.Content, "network error") {
		t.Errorf("error should mention underlying cause, got: %s", result.Content)
	}
}

func TestMessageToolSendWithMetadata(t *testing.T) {
	tg := &mockChannel{name: "telegram"}
	mt := newTestMessageTool(tg)

	result := execMessageTool(t, mt, map[string]any{
		"action":  "send",
		"channel": "telegram",
		"target":  "123",
		"content": "hello",
		"metadata": map[string]string{
			"priority": "high",
		},
	})

	if result.IsError {
		t.Fatalf("send failed: %s", result.Content)
	}

	msgs := tg.sentMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Metadata["priority"] != "high" {
		t.Errorf("metadata not passed through: %v", msgs[0].Metadata)
	}
}

// --- ListChannels action ---

func TestMessageToolListChannels(t *testing.T) {
	mt := newTestMessageTool(
		&mockChannel{name: "telegram"},
		&mockChannel{name: "discord"},
		&mockChannel{name: "slack"},
	)

	result := execMessageTool(t, mt, map[string]any{
		"action": "list_channels",
	})

	if result.IsError {
		t.Fatalf("list_channels failed: %s", result.Content)
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(result.Content), &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	count, ok := data["count"].(float64)
	if !ok || int(count) != 3 {
		t.Errorf("expected count=3, got %v", data["count"])
	}
}

func TestMessageToolListChannelsEmpty(t *testing.T) {
	mt := newTestMessageTool()

	result := execMessageTool(t, mt, map[string]any{
		"action": "list_channels",
	})

	if result.IsError {
		t.Fatalf("list_channels failed: %s", result.Content)
	}

	var data map[string]any
	json.Unmarshal([]byte(result.Content), &data)
	count, ok := data["count"].(float64)
	if !ok || int(count) != 0 {
		t.Errorf("expected count=0, got %v", data["count"])
	}
}

// --- Broadcast action ---

func TestMessageToolBroadcast(t *testing.T) {
	tg := &mockChannel{name: "telegram"}
	dc := &mockChannel{name: "discord"}
	mt := newTestMessageTool(tg, dc)

	result := execMessageTool(t, mt, map[string]any{
		"action":   "broadcast",
		"channels": []string{"telegram", "discord"},
		"target":   "123",
		"content":  "broadcast msg",
	})

	if result.IsError {
		t.Fatalf("broadcast failed: %s", result.Content)
	}

	var data map[string]any
	json.Unmarshal([]byte(result.Content), &data)
	if int(data["success"].(float64)) != 2 {
		t.Errorf("expected 2 successes, got %v", data["success"])
	}

	if len(tg.sentMessages()) != 1 {
		t.Error("telegram should have received 1 message")
	}
	if len(dc.sentMessages()) != 1 {
		t.Error("discord should have received 1 message")
	}
}

func TestMessageToolBroadcastPerChannelTargets(t *testing.T) {
	tg := &mockChannel{name: "telegram"}
	dc := &mockChannel{name: "discord"}
	mt := newTestMessageTool(tg, dc)

	result := execMessageTool(t, mt, map[string]any{
		"action":   "broadcast",
		"channels": []string{"telegram", "discord"},
		"content":  "hello",
		"targets": map[string]string{
			"telegram": "tg-chat-111",
			"discord":  "dc-chan-222",
		},
	})

	if result.IsError {
		t.Fatalf("broadcast failed: %s", result.Content)
	}

	tgMsgs := tg.sentMessages()
	dcMsgs := dc.sentMessages()
	if len(tgMsgs) != 1 || len(dcMsgs) != 1 {
		t.Fatalf("expected 1 message each, got tg=%d dc=%d", len(tgMsgs), len(dcMsgs))
	}
	if tgMsgs[0].SessionID != "tg-chat-111" {
		t.Errorf("telegram target %q, want %q", tgMsgs[0].SessionID, "tg-chat-111")
	}
	if dcMsgs[0].SessionID != "dc-chan-222" {
		t.Errorf("discord target %q, want %q", dcMsgs[0].SessionID, "dc-chan-222")
	}
}

func TestMessageToolBroadcastTargetsFallback(t *testing.T) {
	tg := &mockChannel{name: "telegram"}
	dc := &mockChannel{name: "discord"}
	mt := newTestMessageTool(tg, dc)

	// targets only has telegram; discord should fallback to target
	result := execMessageTool(t, mt, map[string]any{
		"action":   "broadcast",
		"channels": []string{"telegram", "discord"},
		"target":   "fallback-id",
		"content":  "hello",
		"targets": map[string]string{
			"telegram": "tg-specific",
		},
	})

	if result.IsError {
		t.Fatalf("broadcast failed: %s", result.Content)
	}

	tgMsgs := tg.sentMessages()
	dcMsgs := dc.sentMessages()
	if tgMsgs[0].SessionID != "tg-specific" {
		t.Errorf("telegram target %q, want %q", tgMsgs[0].SessionID, "tg-specific")
	}
	if dcMsgs[0].SessionID != "fallback-id" {
		t.Errorf("discord target %q, want fallback %q", dcMsgs[0].SessionID, "fallback-id")
	}
}

func TestMessageToolBroadcastAll(t *testing.T) {
	tg := &mockChannel{name: "telegram"}
	dc := &mockChannel{name: "discord"}
	mt := newTestMessageTool(tg, dc)

	result := execMessageTool(t, mt, map[string]any{
		"action":  "broadcast",
		"content": "to everyone",
	})

	if result.IsError {
		t.Fatalf("broadcast failed: %s", result.Content)
	}

	if len(tg.sentMessages()) != 1 {
		t.Error("telegram should have received 1 message")
	}
	if len(dc.sentMessages()) != 1 {
		t.Error("discord should have received 1 message")
	}
}

func TestMessageToolBroadcastPartialFailure(t *testing.T) {
	tg := &mockChannel{name: "telegram"}
	dc := &mockChannel{name: "discord", sendErr: fmt.Errorf("discord down")}
	mt := newTestMessageTool(tg, dc)

	result := execMessageTool(t, mt, map[string]any{
		"action":   "broadcast",
		"channels": []string{"telegram", "discord"},
		"content":  "partial",
	})

	if result.IsError {
		t.Fatalf("broadcast should not return IsError for partial failure: %s", result.Content)
	}

	var data map[string]any
	json.Unmarshal([]byte(result.Content), &data)
	if int(data["success"].(float64)) != 1 {
		t.Errorf("expected 1 success, got %v", data["success"])
	}
	if int(data["total"].(float64)) != 2 {
		t.Errorf("expected 2 total, got %v", data["total"])
	}
}

func TestMessageToolBroadcastMissingContent(t *testing.T) {
	mt := newTestMessageTool(&mockChannel{name: "telegram"})
	result := execMessageTool(t, mt, map[string]any{
		"action": "broadcast",
	})
	if !result.IsError {
		t.Error("expected error for missing content")
	}
}

func TestMessageToolBroadcastNoChannels(t *testing.T) {
	mt := newTestMessageTool()
	result := execMessageTool(t, mt, map[string]any{
		"action":  "broadcast",
		"content": "hello",
	})
	if !result.IsError {
		t.Error("expected error for no channels")
	}
}

// --- Reply action ---

func TestMessageToolReply(t *testing.T) {
	tg := &mockChannel{name: "telegram"}
	mt := newTestMessageTool(tg)

	result := execMessageTool(t, mt, map[string]any{
		"action":    "reply",
		"channel":   "telegram",
		"target":    "123",
		"content":   "reply msg",
		"thread_id": "456",
	})

	if result.IsError {
		t.Fatalf("reply failed: %s", result.Content)
	}

	msgs := tg.sentMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].ThreadID != "456" {
		t.Errorf("thread_id %q, want %q", msgs[0].ThreadID, "456")
	}
}

func TestMessageToolReplyWithReplyToID(t *testing.T) {
	tg := &mockChannel{name: "telegram"}
	mt := newTestMessageTool(tg)

	result := execMessageTool(t, mt, map[string]any{
		"action":      "reply",
		"channel":     "telegram",
		"target":      "123",
		"content":     "reply msg",
		"reply_to_id": "789",
	})

	if result.IsError {
		t.Fatalf("reply failed: %s", result.Content)
	}

	msgs := tg.sentMessages()
	if msgs[0].ReplyToID != "789" {
		t.Errorf("reply_to_id %q, want %q", msgs[0].ReplyToID, "789")
	}
}

func TestMessageToolReplyMissingChannel(t *testing.T) {
	mt := newTestMessageTool(&mockChannel{name: "telegram"})
	result := execMessageTool(t, mt, map[string]any{
		"action":    "reply",
		"content":   "hello",
		"thread_id": "456",
	})
	if !result.IsError {
		t.Error("expected error for missing channel")
	}
}

func TestMessageToolReplyMissingContent(t *testing.T) {
	mt := newTestMessageTool(&mockChannel{name: "telegram"})
	result := execMessageTool(t, mt, map[string]any{
		"action":    "reply",
		"channel":   "telegram",
		"thread_id": "456",
	})
	if !result.IsError {
		t.Error("expected error for missing content")
	}
}

func TestMessageToolReplyMissingThreadAndReplyTo(t *testing.T) {
	mt := newTestMessageTool(&mockChannel{name: "telegram"})
	result := execMessageTool(t, mt, map[string]any{
		"action":  "reply",
		"channel": "telegram",
		"content": "hello",
	})
	if !result.IsError {
		t.Error("expected error for missing thread_id and reply_to_id")
	}
}

// --- Edge cases ---

func TestMessageToolUnknownAction(t *testing.T) {
	mt := newTestMessageTool()
	result := execMessageTool(t, mt, map[string]any{
		"action": "delete",
	})
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
	if !strings.Contains(result.Content, "delete") {
		t.Errorf("error should mention unknown action, got: %s", result.Content)
	}
}

func TestMessageToolInvalidParams(t *testing.T) {
	mt := newTestMessageTool()
	result, err := mt.Execute(context.Background(), json.RawMessage(`{invalid`))
	if err != nil {
		t.Fatalf("Execute should not return Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestMessageToolBroadcastWithMetadata(t *testing.T) {
	tg := &mockChannel{name: "telegram"}
	dc := &mockChannel{name: "discord"}
	mt := newTestMessageTool(tg, dc)

	result := execMessageTool(t, mt, map[string]any{
		"action":   "broadcast",
		"channels": []string{"telegram", "discord"},
		"content":  "hello",
		"metadata": map[string]string{
			"priority": "high",
			"source":   "cron",
		},
	})

	if result.IsError {
		t.Fatalf("broadcast failed: %s", result.Content)
	}

	for _, ch := range []*mockChannel{tg, dc} {
		msgs := ch.sentMessages()
		if len(msgs) != 1 {
			t.Fatalf("%s: expected 1 message, got %d", ch.name, len(msgs))
		}
		if msgs[0].Metadata["priority"] != "high" {
			t.Errorf("%s: metadata[priority] = %q, want %q", ch.name, msgs[0].Metadata["priority"], "high")
		}
		if msgs[0].Metadata["source"] != "cron" {
			t.Errorf("%s: metadata[source] = %q, want %q", ch.name, msgs[0].Metadata["source"], "cron")
		}
	}
}

func TestMessageToolBroadcastWithThreading(t *testing.T) {
	tg := &mockChannel{name: "telegram"}
	dc := &mockChannel{name: "discord"}
	mt := newTestMessageTool(tg, dc)

	result := execMessageTool(t, mt, map[string]any{
		"action":      "broadcast",
		"channels":    []string{"telegram", "discord"},
		"content":     "threaded broadcast",
		"thread_id":   "t-100",
		"reply_to_id": "m-200",
	})

	if result.IsError {
		t.Fatalf("broadcast failed: %s", result.Content)
	}

	for _, ch := range []*mockChannel{tg, dc} {
		msgs := ch.sentMessages()
		if len(msgs) != 1 {
			t.Fatalf("%s: expected 1 message, got %d", ch.name, len(msgs))
		}
		if msgs[0].ThreadID != "t-100" {
			t.Errorf("%s: thread_id = %q, want %q", ch.name, msgs[0].ThreadID, "t-100")
		}
		if msgs[0].ReplyToID != "m-200" {
			t.Errorf("%s: reply_to_id = %q, want %q", ch.name, msgs[0].ReplyToID, "m-200")
		}
	}
}
