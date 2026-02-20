package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"alfred-ai/internal/domain"
)

// MessageTool allows the LLM to send messages to connected channels.
type MessageTool struct {
	channels *ChannelRegistry
	logger   *slog.Logger
}

// NewMessageTool creates a new message tool backed by the given channel registry.
func NewMessageTool(channels *ChannelRegistry, logger *slog.Logger) *MessageTool {
	return &MessageTool{channels: channels, logger: logger}
}

func (t *MessageTool) Name() string { return "message" }
func (t *MessageTool) Description() string {
	return "Send messages to connected channels, list available channels, broadcast to multiple channels, or reply to specific threads."
}

func (t *MessageTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["send", "list_channels", "broadcast", "reply"],
					"description": "The operation to perform"
				},
				"channel": {
					"type": "string",
					"description": "Target channel name (required for send, reply)"
				},
				"channels": {
					"type": "array",
					"items": { "type": "string" },
					"description": "Target channel names (required for broadcast; omit to broadcast to all)"
				},
				"target": {
					"type": "string",
					"description": "Destination identifier on the channel (chat ID, user ID, channel ID). For broadcast, used as fallback when targets is not set."
				},
				"targets": {
					"type": "object",
					"additionalProperties": { "type": "string" },
					"description": "Per-channel destination map for broadcast (e.g. {\"telegram\": \"12345\", \"discord\": \"67890\"}). Overrides target per channel."
				},
				"content": {
					"type": "string",
					"description": "Message content to send"
				},
				"thread_id": {
					"type": "string",
					"description": "Thread ID to send within (for send and reply actions)"
				},
				"reply_to_id": {
					"type": "string",
					"description": "Message ID to reply to (for send and reply actions)"
				},
				"metadata": {
					"type": "object",
					"additionalProperties": { "type": "string" },
					"description": "Optional key-value metadata to attach to the message"
				}
			},
			"required": ["action"]
		}`),
	}
}

type messageParams struct {
	Action    string            `json:"action"`
	Channel   string            `json:"channel"`
	Channels  []string          `json:"channels"`
	Target    string            `json:"target"`
	Targets   map[string]string `json:"targets,omitempty"`
	Content   string            `json:"content"`
	ThreadID  string            `json:"thread_id"`
	ReplyToID string            `json:"reply_to_id"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

func (t *MessageTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	return Execute(ctx, "tool.message", t.logger, params,
		Dispatch(func(p messageParams) string { return p.Action }, ActionMap[messageParams]{
			"send":          t.handleSend,
			"list_channels": t.handleListChannels,
			"broadcast":     t.handleBroadcast,
			"reply":         t.handleReply,
		}),
	)
}

// sendToChannel sends a message to the named channel using the given params.
func (t *MessageTool) sendToChannel(ctx context.Context, chName string, p messageParams) error {
	ch, err := t.channels.Get(chName)
	if err != nil {
		return err
	}

	msg := domain.OutboundMessage{
		SessionID: p.Target,
		Content:   p.Content,
		ThreadID:  p.ThreadID,
		ReplyToID: p.ReplyToID,
		Metadata:  p.Metadata,
	}

	if err := ch.Send(ctx, msg); err != nil {
		return fmt.Errorf("send to %s failed: %w", chName, err)
	}
	return nil
}

func (t *MessageTool) handleSend(ctx context.Context, p messageParams) (any, error) {
	if err := RequireFields("channel", p.Channel, "content", p.Content); err != nil {
		return nil, err
	}

	if err := t.sendToChannel(ctx, p.Channel, p); err != nil {
		return nil, err
	}

	t.logger.Info("message sent via tool",
		"channel", p.Channel,
		"target", p.Target,
	)

	return map[string]any{
		"sent":    true,
		"channel": p.Channel,
		"target":  p.Target,
	}, nil
}

func (t *MessageTool) handleListChannels(_ context.Context, _ messageParams) (any, error) {
	names := t.channels.List()
	type channelInfo struct {
		Name string `json:"name"`
	}
	result := make([]channelInfo, len(names))
	for i, name := range names {
		result[i] = channelInfo{Name: name}
	}
	return map[string]any{
		"channels": result,
		"count":    len(result),
	}, nil
}

func (t *MessageTool) handleBroadcast(ctx context.Context, p messageParams) (any, error) {
	if err := RequireField("content", p.Content); err != nil {
		return nil, err
	}

	targetNames := p.Channels
	if len(targetNames) == 0 {
		targetNames = t.channels.List()
	}

	if len(targetNames) == 0 {
		return nil, fmt.Errorf("no channels available for broadcast")
	}

	type sendResult struct {
		Channel string `json:"channel"`
		Sent    bool   `json:"sent"`
		Error   string `json:"error,omitempty"`
	}

	results := make([]sendResult, len(targetNames))
	var wg sync.WaitGroup

	for i, name := range targetNames {
		wg.Add(1)
		go func(idx int, chName string) {
			defer wg.Done()

			// Resolve per-channel target: targets map > fallback target.
			sessionID := p.Target
			if v, ok := p.Targets[chName]; ok {
				sessionID = v
			}

			msg := domain.OutboundMessage{
				SessionID: sessionID,
				Content:   p.Content,
				ThreadID:  p.ThreadID,
				ReplyToID: p.ReplyToID,
				Metadata:  p.Metadata,
			}

			ch, err := t.channels.Get(chName)
			sr := sendResult{Channel: chName}
			if err != nil {
				sr.Error = err.Error()
			} else if err := ch.Send(ctx, msg); err != nil {
				sr.Error = fmt.Sprintf("send failed: %v", err)
			} else {
				sr.Sent = true
			}
			results[idx] = sr
		}(i, name)
	}
	wg.Wait()

	successCount := 0
	for _, r := range results {
		if r.Sent {
			successCount++
		}
	}

	t.logger.Info("broadcast sent via tool",
		"total", len(targetNames),
		"success", successCount,
	)

	return map[string]any{
		"results": results,
		"total":   len(targetNames),
		"success": successCount,
	}, nil
}

func (t *MessageTool) handleReply(ctx context.Context, p messageParams) (any, error) {
	if err := RequireFields("channel", p.Channel, "content", p.Content); err != nil {
		return nil, err
	}
	if p.ThreadID == "" && p.ReplyToID == "" {
		return nil, fmt.Errorf("'thread_id' or 'reply_to_id' is required for reply action")
	}

	if err := t.sendToChannel(ctx, p.Channel, p); err != nil {
		return nil, err
	}

	t.logger.Info("reply sent via tool",
		"channel", p.Channel,
		"thread_id", p.ThreadID,
		"reply_to_id", p.ReplyToID,
	)

	return map[string]any{
		"sent":        true,
		"channel":     p.Channel,
		"thread_id":   p.ThreadID,
		"reply_to_id": p.ReplyToID,
	}, nil
}
