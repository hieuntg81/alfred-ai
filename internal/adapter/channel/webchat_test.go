package channel

import (
	"context"
	"testing"

	"alfred-ai/internal/domain"
)

func TestWebChatChannelName(t *testing.T) {
	ch := NewWebChatChannel(newTelegramTestLogger())
	if ch.Name() != "webchat" {
		t.Errorf("Name = %q", ch.Name())
	}
}

func TestWebChatStartStop(t *testing.T) {
	ch := NewWebChatChannel(newTelegramTestLogger())
	handler := func(_ context.Context, _ domain.InboundMessage) error { return nil }

	if err := ch.Start(context.Background(), handler); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := ch.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestWebChatSendNoop(t *testing.T) {
	ch := NewWebChatChannel(newTelegramTestLogger())
	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "test",
		Content:   "hello",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
}
