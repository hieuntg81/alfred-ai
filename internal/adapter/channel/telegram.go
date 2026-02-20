package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"alfred-ai/internal/domain"
)

// TelegramOption configures the Telegram channel.
type TelegramOption func(*TelegramChannel)

// WithTelegramMentionOnly enables mention-only filtering in groups.
func WithTelegramMentionOnly(v bool) TelegramOption {
	return func(t *TelegramChannel) { t.mentionOnly = v }
}

// TelegramChannel implements domain.Channel for Telegram Bot API via long-polling.
type TelegramChannel struct {
	token       string
	handler     domain.MessageHandler
	logger      *slog.Logger
	client      *http.Client
	baseURL     string
	offset      int64
	done        chan struct{}
	botUsername string
	mentionOnly bool
}

// NewTelegramChannel creates a Telegram bot channel.
func NewTelegramChannel(token string, logger *slog.Logger, opts ...TelegramOption) *TelegramChannel {
	t := &TelegramChannel{
		token:   token,
		logger:  logger,
		baseURL: "https://api.telegram.org",
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		done: make(chan struct{}),
	}
	for _, o := range opts {
		o(t)
	}
	return t
}

// Start begins long-polling for updates. Non-blocking (starts in goroutine).
func (t *TelegramChannel) Start(ctx context.Context, handler domain.MessageHandler) error {
	t.handler = handler

	// Fetch bot username for mention detection.
	if me, err := t.getMe(ctx); err == nil {
		t.botUsername = me
		t.logger.Info("telegram bot identified", "username", me)
	} else {
		t.logger.Warn("telegram getMe failed, mention detection disabled", "error", err)
	}

	go t.pollLoop(ctx)
	t.logger.Info("telegram channel started")
	return nil
}

// Stop signals the polling loop to stop.
func (t *TelegramChannel) Stop(_ context.Context) error {
	select {
	case <-t.done:
	default:
		close(t.done)
	}
	return nil
}

// Send sends a message to a Telegram chat.
func (t *TelegramChannel) Send(ctx context.Context, msg domain.OutboundMessage) error {
	content := msg.Content
	if msg.IsError {
		content = "Error: " + content
	}

	return t.sendMessage(ctx, msg.SessionID, content, msg.ThreadID, msg.ReplyToID)
}

// Name implements domain.Channel.
func (t *TelegramChannel) Name() string { return "telegram" }

func (t *TelegramChannel) pollLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.done:
			return
		default:
			updates, err := t.getUpdates(ctx)
			if err != nil {
				t.logger.Warn("telegram getUpdates failed", "error", err)
				time.Sleep(5 * time.Second)
				continue
			}

			for _, u := range updates {
				if u.UpdateID >= t.offset {
					t.offset = u.UpdateID + 1
				}
				if u.Message == nil {
					continue
				}

				// Determine content: text or caption fallback.
				content := u.Message.Text
				if content == "" {
					content = u.Message.Caption
				}
				if content == "" {
					continue
				}

				chatID := strconv.FormatInt(u.Message.Chat.ID, 10)

				// Handle commands first
				if strings.HasPrefix(content, "/") {
					if t.handleCommand(ctx, chatID, content) {
						continue // Command handled, don't send to agent
					}
				}

				// Detect mention.
				isMention := t.hasBotMention(u.Message)
				isGroup := u.Message.Chat.Type != "" && u.Message.Chat.Type != "private"

				// Mention gating: skip non-mentioned group messages when mentionOnly.
				if t.mentionOnly && isGroup && !isMention {
					continue
				}

				msg := domain.InboundMessage{
					SessionID:   chatID,
					Content:     content,
					ChannelName: "telegram",
					IsMention:   isMention,
				}

				// Enrich sender.
				if u.Message.From != nil {
					msg.SenderID = strconv.FormatInt(u.Message.From.ID, 10)
					name := u.Message.From.FirstName
					if u.Message.From.LastName != "" {
						name += " " + u.Message.From.LastName
					}
					msg.SenderName = name
				}

				// Enrich group/thread/reply.
				if isGroup {
					msg.GroupID = chatID
				}
				if u.Message.MessageThreadID != 0 {
					msg.ThreadID = strconv.FormatInt(u.Message.MessageThreadID, 10)
				}
				if u.Message.ReplyToMessage != nil {
					msg.ReplyToID = strconv.FormatInt(u.Message.ReplyToMessage.MessageID, 10)
				}

				// Enrich media.
				msg.Media = extractMedia(u.Message)

				if err := t.handler(ctx, msg); err != nil {
					t.logger.Error("telegram handler error", "error", err, "chat_id", chatID)
				}
			}
		}
	}
}

// hasBotMention checks if any entity in the message mentions the bot.
func (t *TelegramChannel) hasBotMention(msg *telegramMessage) bool {
	if t.botUsername == "" {
		return false
	}
	for _, e := range msg.Entities {
		if e.Type == "mention" {
			// Extract mention text from the message.
			end := e.Offset + e.Length
			if end <= int64(len(msg.Text)) {
				mention := msg.Text[e.Offset:end]
				if strings.EqualFold(mention, "@"+t.botUsername) {
					return true
				}
			}
		}
	}
	return false
}

// handleCommand processes bot commands. Returns true if command was handled.
func (t *TelegramChannel) handleCommand(ctx context.Context, chatID, content string) bool {
	fields := strings.Fields(content)
	if len(fields) == 0 {
		return false
	}

	cmd := fields[0]

	switch cmd {
	case "/help", "/start":
		_ = t.sendMessage(ctx, chatID, GetHelpText("telegram"), "", "")
		return true
	case "/privacy":
		_ = t.sendMessage(ctx, chatID, GetPrivacyText(), "", "")
		return true
	default:
		return false // Not a bot command, send to agent
	}
}

// extractMedia converts Telegram photo/document to domain.Media.
func extractMedia(msg *telegramMessage) []domain.Media {
	var media []domain.Media

	// Photos: take the largest (last) size.
	// Telegram API returns photo sizes sorted by ascending dimensions.
	if len(msg.Photo) > 0 {
		largest := msg.Photo[len(msg.Photo)-1]
		media = append(media, domain.Media{
			Type:    domain.MediaTypeImage,
			URL:     largest.FileID,
			Caption: msg.Caption,
		})
	}

	// Document.
	if msg.Document != nil {
		media = append(media, domain.Media{
			Type:     domain.MediaTypeFile,
			URL:      msg.Document.FileID,
			MIMEType: msg.Document.MIMEType,
			Caption:  msg.Caption,
		})
	}

	return media
}

// --- Telegram Bot API types ---

type telegramUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

type telegramEntity struct {
	Type   string `json:"type"`
	Offset int64  `json:"offset"`
	Length int64  `json:"length"`
}

type telegramPhotoSize struct {
	FileID string `json:"file_id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type telegramDocument struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MIMEType string `json:"mime_type"`
}

type telegramUpdate struct {
	UpdateID int64            `json:"update_id"`
	Message  *telegramMessage `json:"message"`
}

type telegramMessage struct {
	MessageID       int64               `json:"message_id"`
	From            *telegramUser       `json:"from,omitempty"`
	Chat            telegramChat        `json:"chat"`
	Text            string              `json:"text"`
	Caption         string              `json:"caption"`
	MessageThreadID int64               `json:"message_thread_id,omitempty"`
	ReplyToMessage  *telegramReplyInfo  `json:"reply_to_message,omitempty"`
	Entities        []telegramEntity    `json:"entities,omitempty"`
	Photo           []telegramPhotoSize `json:"photo,omitempty"`
	Document        *telegramDocument   `json:"document,omitempty"`
}

type telegramReplyInfo struct {
	MessageID int64 `json:"message_id"`
}

type telegramChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type telegramUpdateResponse struct {
	OK     bool             `json:"ok"`
	Result []telegramUpdate `json:"result"`
}

type telegramSendRequest struct {
	ChatID          string `json:"chat_id"`
	Text            string `json:"text"`
	MessageThreadID int64  `json:"message_thread_id,omitempty"`
	ReplyToMsgID    int64  `json:"reply_to_message_id,omitempty"`
}

type telegramSendResponse struct {
	OK bool `json:"ok"`
}

type telegramGetMeResponse struct {
	OK     bool `json:"ok"`
	Result struct {
		Username string `json:"username"`
	} `json:"result"`
}

func (t *TelegramChannel) getMe(ctx context.Context) (string, error) {
	url := fmt.Sprintf("%s/bot%s/getMe", t.baseURL, t.token)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var result telegramGetMeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("unmarshal: %w", err)
	}

	if !result.OK || result.Result.Username == "" {
		return "", fmt.Errorf("getMe returned ok=%v username=%q", result.OK, result.Result.Username)
	}

	return result.Result.Username, nil
}

func (t *TelegramChannel) getUpdates(ctx context.Context) ([]telegramUpdate, error) {
	url := fmt.Sprintf("%s/bot%s/getUpdates?offset=%d&timeout=30", t.baseURL, t.token, t.offset)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telegram API error %d: %s", resp.StatusCode, string(body))
	}

	var result telegramUpdateResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if !result.OK {
		return nil, fmt.Errorf("telegram API returned ok=false")
	}

	return result.Result, nil
}

func (t *TelegramChannel) sendMessage(ctx context.Context, chatID, text, threadID, replyToID string) error {
	url := fmt.Sprintf("%s/bot%s/sendMessage", t.baseURL, t.token)

	sendReq := telegramSendRequest{
		ChatID: chatID,
		Text:   text,
	}
	if threadID != "" {
		if tid, err := strconv.ParseInt(threadID, 10, 64); err == nil {
			sendReq.MessageThreadID = tid
		}
	}
	if replyToID != "" {
		if rid, err := strconv.ParseInt(replyToID, 10, 64); err == nil {
			sendReq.ReplyToMsgID = rid
		}
	}

	payload, err := json.Marshal(sendReq)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram sendMessage error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
