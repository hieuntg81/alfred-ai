package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"alfred-ai/internal/domain"
)

// TeamsOption configures a TeamsChannel.
type TeamsOption func(*TeamsChannel)

// WithTeamsWebhookAddr sets the webhook listener address.
func WithTeamsWebhookAddr(addr string) TeamsOption {
	return func(t *TeamsChannel) { t.webhookAddr = addr }
}

// WithTeamsMentionOnly filters out non-mention messages in group chats.
func WithTeamsMentionOnly(v bool) TeamsOption {
	return func(t *TeamsChannel) { t.mentionOnly = v }
}

// WithTeamsTenantID restricts the channel to a specific tenant.
func WithTeamsTenantID(id string) TeamsOption {
	return func(t *TeamsChannel) { t.tenantID = id }
}

// TeamsChannel implements domain.Channel for Microsoft Teams via Bot Framework.
type TeamsChannel struct {
	appID       string
	appSecret   string
	webhookAddr string
	tenantID    string
	mentionOnly bool

	handler   domain.MessageHandler
	logger    *slog.Logger
	client    *http.Client
	server    *http.Server
	boundAddr string
	baseCtx   context.Context
	done      chan struct{}

	// Token cache for Bot Framework auth.
	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

// NewTeamsChannel creates a Microsoft Teams channel.
func NewTeamsChannel(appID, appSecret string, logger *slog.Logger, opts ...TeamsOption) *TeamsChannel {
	t := &TeamsChannel{
		appID:       appID,
		appSecret:   appSecret,
		webhookAddr: ":3978",
		logger:      logger,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		done: make(chan struct{}),
	}
	for _, o := range opts {
		o(t)
	}
	return t
}

// Name implements domain.Channel.
func (t *TeamsChannel) Name() string { return "teams" }

// Start implements domain.Channel.
func (t *TeamsChannel) Start(ctx context.Context, handler domain.MessageHandler) error {
	t.handler = handler
	t.baseCtx = ctx

	mux := http.NewServeMux()
	mux.HandleFunc("/api/messages", t.handleActivity)

	t.server = &http.Server{
		Addr:              t.webhookAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	ln, err := net.Listen("tcp", t.webhookAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", t.webhookAddr, err)
	}
	t.boundAddr = ln.Addr().String()

	go func() {
		t.logger.Info("teams webhook started", "addr", t.boundAddr)
		if err := t.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			t.logger.Error("teams webhook error", "error", err)
		}
	}()

	return nil
}

// Stop implements domain.Channel.
func (t *TeamsChannel) Stop(ctx context.Context) error {
	select {
	case <-t.done:
	default:
		close(t.done)
	}
	if t.server == nil {
		return nil
	}
	return t.server.Shutdown(ctx)
}

// Send implements domain.Channel.
func (t *TeamsChannel) Send(ctx context.Context, msg domain.OutboundMessage) error {
	content := msg.Content
	if msg.IsError {
		content = "Error: " + content
	}

	serviceURL := msg.Metadata["service_url"]
	if serviceURL == "" {
		return fmt.Errorf("teams: service_url is required in metadata")
	}
	conversationID := msg.SessionID
	if conversationID == "" {
		return fmt.Errorf("teams: session_id (conversation ID) is required")
	}

	return t.sendActivity(ctx, serviceURL, conversationID, content, msg.ThreadID)
}

// BoundAddr returns the actual address the webhook server is listening on.
func (t *TeamsChannel) BoundAddr() string {
	return t.boundAddr
}

// --- Webhook handling ---

func (t *TeamsChannel) handleActivity(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1*1024*1024))
	if err != nil {
		t.logger.Warn("teams read body error", "error", err)
		rw.WriteHeader(http.StatusOK)
		return
	}

	var activity teamsActivity
	if err := json.Unmarshal(body, &activity); err != nil {
		t.logger.Warn("teams unmarshal error", "error", err)
		rw.WriteHeader(http.StatusOK)
		return
	}

	// Teams expects a quick 200/202 response.
	rw.WriteHeader(http.StatusOK)

	// Process async to avoid blocking the webhook response.
	go t.processActivity(t.baseCtx, &activity)
}

func (t *TeamsChannel) processActivity(ctx context.Context, activity *teamsActivity) {
	switch activity.Type {
	case "message":
		t.processMessage(ctx, activity)
	case "conversationUpdate":
		t.logger.Info("teams conversation update",
			"conversation", activity.Conversation.ID)
	case "installationUpdate":
		t.logger.Info("teams installation update",
			"action", activity.Action)
	default:
		t.logger.Debug("teams ignored activity", "type", activity.Type)
	}
}

func (t *TeamsChannel) processMessage(ctx context.Context, activity *teamsActivity) {
	text := activity.Text
	// Strip HTML tags and bot mention from Teams messages.
	text = stripTeamsHTML(text)
	text = strings.TrimSpace(text)

	if text == "" {
		return
	}

	// Filter by tenant if configured.
	if t.tenantID != "" && activity.Conversation.TenantID != t.tenantID {
		return
	}

	// Determine if this is a mention.
	isMention := false
	for _, mention := range activity.Entities {
		if mention.Type == "mention" && mention.Mentioned.ID == t.appID {
			isMention = true
			break
		}
	}

	// In group chats, optionally only respond to mentions.
	isGroup := activity.Conversation.IsGroup
	if t.mentionOnly && isGroup && !isMention {
		return
	}

	// Handle commands.
	if t.handleCommand(ctx, activity, text) {
		return
	}

	inbound := domain.InboundMessage{
		SessionID:   activity.Conversation.ID,
		Content:     text,
		ChannelName: "teams",
		SenderID:    activity.From.ID,
		SenderName:  activity.From.Name,
		GroupID:     activity.Conversation.ID,
		ThreadID:    activity.ReplyToID,
		IsMention:   isMention,
		Metadata: map[string]string{
			"service_url": activity.ServiceURL,
		},
	}

	if err := t.handler(ctx, inbound); err != nil {
		t.logger.Error("teams handler error", "error", err, "conversation", activity.Conversation.ID)
	}
}

func (t *TeamsChannel) handleCommand(ctx context.Context, activity *teamsActivity, content string) bool {
	fields := strings.Fields(content)
	if len(fields) == 0 {
		return false
	}

	switch fields[0] {
	case "/help", "/start":
		_ = t.sendActivity(ctx, activity.ServiceURL, activity.Conversation.ID,
			GetHelpText("teams"), activity.ReplyToID)
		return true
	case "/privacy":
		_ = t.sendActivity(ctx, activity.ServiceURL, activity.Conversation.ID,
			GetPrivacyText(), activity.ReplyToID)
		return true
	default:
		return false
	}
}

// --- Outbound messaging ---

func (t *TeamsChannel) sendActivity(ctx context.Context, serviceURL, conversationID, text, replyToID string) error {
	token, err := t.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}

	url := fmt.Sprintf("%s/v3/conversations/%s/activities", strings.TrimRight(serviceURL, "/"), conversationID)

	outActivity := teamsSendActivity{
		Type: "message",
		Text: text,
	}
	if replyToID != "" {
		outActivity.ReplyToID = replyToID
	}

	body, err := json.Marshal(outActivity)
	if err != nil {
		return fmt.Errorf("marshal activity: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("teams API error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// --- Bot Framework authentication ---

func (t *TeamsChannel) getAccessToken(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Return cached token if still valid (with 60s buffer).
	if t.accessToken != "" && time.Now().Before(t.tokenExpiry.Add(-60*time.Second)) {
		return t.accessToken, nil
	}

	token, expiry, err := t.exchangeToken(ctx)
	if err != nil {
		return "", err
	}

	t.accessToken = token
	t.tokenExpiry = expiry
	return token, nil
}

func (t *TeamsChannel) exchangeToken(ctx context.Context) (string, time.Time, error) {
	tokenURL := "https://login.microsoftonline.com/botframework.com/oauth2/v2.0/token"

	form := fmt.Sprintf("grant_type=client_credentials&client_id=%s&client_secret=%s&scope=%s",
		t.appID, t.appSecret,
		"https://api.botframework.com/.default")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("token exchange: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("token exchange failed %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", time.Time{}, fmt.Errorf("parse token response: %w", err)
	}

	tokenExpiry := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	return tokenResp.AccessToken, tokenExpiry, nil
}

// --- HTML stripping for Teams messages ---

// stripTeamsHTML removes HTML tags from Teams messages.
// Teams wraps bot mentions in <at>...</at> tags and may include other HTML.
func stripTeamsHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// --- Bot Framework wire types ---

type teamsActivity struct {
	Type         string              `json:"type"`
	ID           string              `json:"id"`
	Timestamp    string              `json:"timestamp"`
	ServiceURL   string              `json:"serviceUrl"`
	ChannelID    string              `json:"channelId"`
	Text         string              `json:"text"`
	From         teamsAccount        `json:"from"`
	Recipient    teamsAccount        `json:"recipient"`
	Conversation teamsConversation   `json:"conversation"`
	ReplyToID    string              `json:"replyToId,omitempty"`
	Entities     []teamsEntity       `json:"entities,omitempty"`
	Action       string              `json:"action,omitempty"`
}

type teamsAccount struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type teamsConversation struct {
	ID       string `json:"id"`
	TenantID string `json:"tenantId,omitempty"`
	IsGroup  bool   `json:"isGroup,omitempty"`
}

type teamsEntity struct {
	Type      string       `json:"type"`
	Mentioned teamsAccount `json:"mentioned,omitempty"`
	Text      string       `json:"text,omitempty"`
}

type teamsSendActivity struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	ReplyToID string `json:"replyToId,omitempty"`
}
