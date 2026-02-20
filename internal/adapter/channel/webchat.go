package channel

import (
	"context"
	"log/slog"

	"alfred-ai/internal/domain"
)

// WebChatChannel is a thin stub channel for the WebChat interface.
// It does not manage its own transport — the gateway RPC layer calls
// Router.Handle with ChannelName="webchat" directly and returns the
// response synchronously.
type WebChatChannel struct {
	handler domain.MessageHandler
	logger  *slog.Logger
}

// NewWebChatChannel creates a WebChat stub channel.
func NewWebChatChannel(logger *slog.Logger) *WebChatChannel {
	return &WebChatChannel{logger: logger}
}

func (w *WebChatChannel) Name() string { return "webchat" }

// Start stores the handler. WebChat has no transport to start.
func (w *WebChatChannel) Start(_ context.Context, handler domain.MessageHandler) error {
	w.handler = handler
	w.logger.Info("webchat channel registered")
	return nil
}

// Stop is a no-op for WebChat.
func (w *WebChatChannel) Stop(_ context.Context) error { return nil }

// Send is a no-op for WebChat (responses are returned synchronously via RPC).
func (w *WebChatChannel) Send(_ context.Context, _ domain.OutboundMessage) error { return nil }
