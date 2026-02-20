package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"alfred-ai/internal/domain"
)

// MatrixOption configures the Matrix channel.
type MatrixOption func(*MatrixChannel)

// WithMatrixMentionOnly enables mention-only filtering in rooms.
func WithMatrixMentionOnly(v bool) MatrixOption {
	return func(m *MatrixChannel) { m.mentionOnly = v }
}

// MatrixChannel implements domain.Channel for Matrix protocol via long-poll sync.
type MatrixChannel struct {
	homeserverURL string // e.g. https://matrix.org
	accessToken   string
	userID        string // e.g. @alfred:matrix.org
	handler       domain.MessageHandler
	logger        *slog.Logger
	client        *http.Client
	nextBatch     string // sync token
	txnID         int64  // atomic, for send idempotency
	done          chan struct{}
	mentionOnly   bool
}

// NewMatrixChannel creates a Matrix channel.
func NewMatrixChannel(homeserverURL, accessToken, userID string, logger *slog.Logger, opts ...MatrixOption) *MatrixChannel {
	m := &MatrixChannel{
		homeserverURL: strings.TrimRight(homeserverURL, "/"),
		accessToken:   accessToken,
		userID:        userID,
		logger:        logger,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		done: make(chan struct{}),
	}
	for _, o := range opts {
		o(m)
	}
	return m
}

// Start begins long-polling for Matrix sync events. Non-blocking (starts in goroutine).
func (m *MatrixChannel) Start(ctx context.Context, handler domain.MessageHandler) error {
	m.handler = handler
	go m.pollLoop(ctx)
	m.logger.Info("matrix channel started", "user_id", m.userID)
	return nil
}

// Stop signals the polling loop to stop.
func (m *MatrixChannel) Stop(_ context.Context) error {
	select {
	case <-m.done:
	default:
		close(m.done)
	}
	return nil
}

// Send sends a message to a Matrix room.
func (m *MatrixChannel) Send(ctx context.Context, msg domain.OutboundMessage) error {
	content := msg.Content
	if msg.IsError {
		content = "Error: " + content
	}

	return m.sendMessage(ctx, msg.SessionID, content)
}

// Name implements domain.Channel.
func (m *MatrixChannel) Name() string { return "matrix" }

func (m *MatrixChannel) pollLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.done:
			return
		default:
			syncResp, err := m.sync(ctx)
			if err != nil {
				m.logger.Warn("matrix sync failed", "error", err)
				select {
				case <-time.After(5 * time.Second):
				case <-ctx.Done():
					return
				case <-m.done:
					return
				}
				continue
			}

			m.nextBatch = syncResp.NextBatch

			// Process invites: auto-join rooms.
			for roomID := range syncResp.Rooms.Invite {
				if err := m.joinRoom(ctx, roomID); err != nil {
					m.logger.Warn("matrix auto-join failed", "room_id", roomID, "error", err)
				} else {
					m.logger.Info("matrix auto-joined room", "room_id", roomID)
				}
			}

			// Process joined room events.
			for roomID, room := range syncResp.Rooms.Join {
				for _, event := range room.Timeline.Events {
					m.processEvent(ctx, roomID, event)
				}
			}
		}
	}
}

func (m *MatrixChannel) processEvent(ctx context.Context, roomID string, event matrixEvent) {
	// Only handle m.room.message events.
	if event.Type != "m.room.message" {
		return
	}

	// Filter out own messages.
	if event.Sender == m.userID {
		return
	}

	// Only handle text messages.
	msgType, _ := event.Content["msgtype"].(string)
	if msgType != "m.text" {
		return
	}

	body, _ := event.Content["body"].(string)
	if body == "" {
		return
	}

	// Handle commands.
	if strings.HasPrefix(body, "/") {
		if m.handleCommand(ctx, roomID, body) {
			return
		}
	}

	// Mention gating.
	isMention := strings.Contains(body, m.userID)
	if m.mentionOnly && !isMention {
		return
	}

	inbound := domain.InboundMessage{
		SessionID:   roomID,
		SenderID:    event.Sender,
		Content:     body,
		ChannelName: "matrix",
		GroupID:     roomID,
		IsMention:   isMention,
	}

	// Extract display name if available.
	if displayName, ok := event.Content["displayname"].(string); ok {
		inbound.SenderName = displayName
	}

	if err := m.handler(ctx, inbound); err != nil {
		m.logger.Error("matrix handler error", "error", err, "room_id", roomID)
	}
}

// handleCommand processes bot commands. Returns true if command was handled.
func (m *MatrixChannel) handleCommand(ctx context.Context, roomID, content string) bool {
	fields := strings.Fields(content)
	if len(fields) == 0 {
		return false
	}

	switch fields[0] {
	case "/help", "/start":
		_ = m.sendMessage(ctx, roomID, GetHelpText("matrix"))
		return true
	case "/privacy":
		_ = m.sendMessage(ctx, roomID, GetPrivacyText())
		return true
	default:
		return false
	}
}

func (m *MatrixChannel) sync(ctx context.Context) (*matrixSyncResponse, error) {
	url := fmt.Sprintf("%s/_matrix/client/v3/sync?timeout=30000", m.homeserverURL)
	if m.nextBatch != "" {
		url += "&since=" + m.nextBatch
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.accessToken)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("matrix sync error %d: %s", resp.StatusCode, string(body))
	}

	var syncResp matrixSyncResponse
	if err := json.Unmarshal(body, &syncResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &syncResp, nil
}

func (m *MatrixChannel) joinRoom(ctx context.Context, roomID string) error {
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/join", m.homeserverURL, roomID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.accessToken)

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
		return fmt.Errorf("join error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (m *MatrixChannel) sendMessage(ctx context.Context, roomID, text string) error {
	txnID := atomic.AddInt64(&m.txnID, 1)
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/send/m.room.message/%d",
		m.homeserverURL, roomID, txnID)

	payload := map[string]string{
		"msgtype": "m.text",
		"body":    text,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.accessToken)

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("matrix send error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// --- Matrix Client-Server API types ---

type matrixSyncResponse struct {
	NextBatch string           `json:"next_batch"`
	Rooms     matrixSyncRooms  `json:"rooms"`
}

type matrixSyncRooms struct {
	Join   map[string]matrixJoinedRoom `json:"join"`
	Invite map[string]matrixInviteRoom `json:"invite"`
}

type matrixJoinedRoom struct {
	Timeline matrixTimeline `json:"timeline"`
}

type matrixTimeline struct {
	Events []matrixEvent `json:"events"`
}

type matrixInviteRoom struct {
	InviteState matrixInviteState `json:"invite_state"`
}

type matrixInviteState struct {
	Events []matrixEvent `json:"events"`
}

type matrixEvent struct {
	Type    string                 `json:"type"`
	Sender  string                 `json:"sender"`
	Content map[string]interface{} `json:"content"`
}
