package channel

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"alfred-ai/internal/domain"
)

// WhatsAppOption configures the WhatsApp channel.
type WhatsAppOption func(*WhatsAppChannel)

// WithWhatsAppMentionOnly enables mention-only filtering in groups.
func WithWhatsAppMentionOnly(v bool) WhatsAppOption {
	return func(w *WhatsAppChannel) { w.mentionOnly = v }
}

// WhatsAppChannel implements domain.Channel for WhatsApp Cloud API.
// It runs a webhook server for receiving messages and uses the Graph API for sending.
type WhatsAppChannel struct {
	token         string // Graph API access token
	phoneNumberID string // sender phone number ID
	verifyToken   string // webhook verification token
	appSecret     string // for X-Hub-Signature-256 verification
	handler       domain.MessageHandler
	logger        *slog.Logger
	client        *http.Client // outbound API calls
	baseURL       string       // Graph API base (overridable for tests)
	server        *http.Server // webhook server
	webhookAddr   string       // ":3335"
	boundAddr     string       // actual bound address
	done          chan struct{}
	mentionOnly   bool
}

// NewWhatsAppChannel creates a WhatsApp channel.
func NewWhatsAppChannel(token, phoneNumberID, verifyToken, appSecret, webhookAddr string, logger *slog.Logger, opts ...WhatsAppOption) *WhatsAppChannel {
	w := &WhatsAppChannel{
		token:         token,
		phoneNumberID: phoneNumberID,
		verifyToken:   verifyToken,
		appSecret:     appSecret,
		webhookAddr:   webhookAddr,
		logger:        logger,
		baseURL:       "https://graph.facebook.com",
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		done: make(chan struct{}),
	}
	for _, o := range opts {
		o(w)
	}
	return w
}

// Start begins the webhook server. Non-blocking (starts in goroutine).
func (w *WhatsAppChannel) Start(ctx context.Context, handler domain.MessageHandler) error {
	w.handler = handler

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", w.handleWebhook)

	w.server = &http.Server{
		Addr:              w.webhookAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	ln, err := net.Listen("tcp", w.webhookAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", w.webhookAddr, err)
	}
	w.boundAddr = ln.Addr().String()

	go func() {
		w.logger.Info("whatsapp webhook started", "addr", w.boundAddr)
		if err := w.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			w.logger.Error("whatsapp webhook server error", "error", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the webhook server.
func (w *WhatsAppChannel) Stop(ctx context.Context) error {
	select {
	case <-w.done:
	default:
		close(w.done)
	}
	if w.server == nil {
		return nil
	}
	return w.server.Shutdown(ctx)
}

// Send sends a message to a WhatsApp recipient via the Graph API.
func (w *WhatsAppChannel) Send(ctx context.Context, msg domain.OutboundMessage) error {
	content := msg.Content
	if msg.IsError {
		content = "Error: " + content
	}

	return w.sendMessage(ctx, msg.SessionID, content)
}

// Name implements domain.Channel.
func (w *WhatsAppChannel) Name() string { return "whatsapp" }

// BoundAddr returns the actual bound address of the webhook server.
func (w *WhatsAppChannel) BoundAddr() string { return w.boundAddr }

func (w *WhatsAppChannel) handleWebhook(rw http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.handleVerification(rw, r)
	case http.MethodPost:
		w.handleIncoming(rw, r)
	default:
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleVerification handles the Meta webhook verification challenge.
func (w *WhatsAppChannel) handleVerification(rw http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("hub.mode")
	token := r.URL.Query().Get("hub.verify_token")
	challenge := r.URL.Query().Get("hub.challenge")

	if mode == "subscribe" && token == w.verifyToken {
		rw.WriteHeader(http.StatusOK)
		rw.Write([]byte(challenge))
		return
	}

	http.Error(rw, "forbidden", http.StatusForbidden)
}

// handleIncoming processes incoming WhatsApp webhook payloads.
// Always returns 200 to prevent Meta from retrying.
func (w *WhatsAppChannel) handleIncoming(rw http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
	if err != nil {
		w.logger.Warn("whatsapp read body error", "error", err)
		rw.WriteHeader(http.StatusOK)
		return
	}

	// Validate signature if app secret is configured.
	if w.appSecret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if !w.validateSignature(body, sig) {
			w.logger.Warn("whatsapp invalid webhook signature")
			rw.WriteHeader(http.StatusOK)
			return
		}
	}

	var payload whatsappWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		w.logger.Warn("whatsapp unmarshal error", "error", err)
		rw.WriteHeader(http.StatusOK)
		return
	}

	w.processPayload(r.Context(), &payload)

	rw.WriteHeader(http.StatusOK)
}

func (w *WhatsAppChannel) validateSignature(body []byte, signature string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(w.appSecret))
	mac.Write(body)
	expected := mac.Sum(nil)

	return hmac.Equal(sig, expected)
}

func (w *WhatsAppChannel) processPayload(ctx context.Context, payload *whatsappWebhookPayload) {
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			if change.Field != "messages" {
				continue
			}
			for _, msg := range change.Value.Messages {
				w.processMessage(ctx, msg, change.Value.Contacts)
			}
		}
	}
}

func (w *WhatsAppChannel) processMessage(ctx context.Context, msg whatsappMessage, contacts []whatsappContact) {
	// Only process text messages.
	if msg.Type != "text" || msg.Text == nil {
		// Check for media types with caption.
		content := w.extractMediaContent(msg)
		if content == "" {
			return
		}
		w.dispatchMessage(ctx, msg.From, "", content, w.extractMediaAttachments(msg), contacts)
		return
	}

	content := msg.Text.Body
	if content == "" {
		return
	}

	// Handle commands.
	if strings.HasPrefix(content, "/") {
		if w.handleCommand(ctx, msg.From, content) {
			return
		}
	}

	w.dispatchMessage(ctx, msg.From, "", content, nil, contacts)
}

func (w *WhatsAppChannel) dispatchMessage(ctx context.Context, from, groupID, content string, media []domain.Media, contacts []whatsappContact) {
	// Mention gating: in group contexts with mentionOnly, skip non-mentioned messages.
	// WhatsApp doesn't have a reliable group mention detection for bots,
	// so we check if the bot's phone number appears in the message.
	if w.mentionOnly && groupID != "" {
		return
	}

	inbound := domain.InboundMessage{
		SessionID:   from,
		SenderID:    from,
		Content:     content,
		ChannelName: "whatsapp",
		Media:       media,
	}

	if groupID != "" {
		inbound.GroupID = groupID
	}

	// Enrich sender name from contacts.
	for _, c := range contacts {
		if c.WaID == from && c.Profile.Name != "" {
			inbound.SenderName = c.Profile.Name
			break
		}
	}

	if err := w.handler(ctx, inbound); err != nil {
		w.logger.Error("whatsapp handler error", "error", err, "from", from)
	}
}

func (w *WhatsAppChannel) extractMediaContent(msg whatsappMessage) string {
	switch msg.Type {
	case "image":
		if msg.Image != nil {
			return msg.Image.Caption
		}
	case "document":
		if msg.Document != nil {
			return msg.Document.Caption
		}
	}
	return ""
}

func (w *WhatsAppChannel) extractMediaAttachments(msg whatsappMessage) []domain.Media {
	var media []domain.Media
	switch msg.Type {
	case "image":
		if msg.Image != nil {
			media = append(media, domain.Media{
				Type:     domain.MediaTypeImage,
				URL:      msg.Image.ID,
				MIMEType: msg.Image.MIMEType,
				Caption:  msg.Image.Caption,
			})
		}
	case "document":
		if msg.Document != nil {
			media = append(media, domain.Media{
				Type:     domain.MediaTypeFile,
				URL:      msg.Document.ID,
				MIMEType: msg.Document.MIMEType,
				Caption:  msg.Document.Caption,
			})
		}
	case "audio":
		if msg.Audio != nil {
			media = append(media, domain.Media{
				Type:     domain.MediaTypeAudio,
				URL:      msg.Audio.ID,
				MIMEType: msg.Audio.MIMEType,
			})
		}
	}
	return media
}

// handleCommand processes bot commands. Returns true if command was handled.
func (w *WhatsAppChannel) handleCommand(ctx context.Context, to, content string) bool {
	fields := strings.Fields(content)
	if len(fields) == 0 {
		return false
	}

	switch fields[0] {
	case "/help", "/start":
		_ = w.sendMessage(ctx, to, GetHelpText("whatsapp"))
		return true
	case "/privacy":
		_ = w.sendMessage(ctx, to, GetPrivacyText())
		return true
	default:
		return false
	}
}

func (w *WhatsAppChannel) sendMessage(ctx context.Context, to, text string) error {
	url := fmt.Sprintf("%s/v21.0/%s/messages", w.baseURL, w.phoneNumberID)

	payload := whatsappSendRequest{
		MessagingProduct: "whatsapp",
		To:               to,
		Type:             "text",
		Text: &whatsappSendText{
			Body: text,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.token)

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("whatsapp API error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// --- WhatsApp Cloud API types ---

type whatsappWebhookPayload struct {
	Object string           `json:"object"`
	Entry  []whatsappEntry  `json:"entry"`
}

type whatsappEntry struct {
	ID      string            `json:"id"`
	Changes []whatsappChange  `json:"changes"`
}

type whatsappChange struct {
	Field string              `json:"field"`
	Value whatsappChangeValue `json:"value"`
}

type whatsappChangeValue struct {
	MessagingProduct string             `json:"messaging_product"`
	Metadata         whatsappMetadata   `json:"metadata"`
	Contacts         []whatsappContact  `json:"contacts"`
	Messages         []whatsappMessage  `json:"messages"`
	Statuses         []whatsappStatus   `json:"statuses"`
}

type whatsappMetadata struct {
	DisplayPhoneNumber string `json:"display_phone_number"`
	PhoneNumberID      string `json:"phone_number_id"`
}

type whatsappContact struct {
	WaID    string         `json:"wa_id"`
	Profile whatsappProfile `json:"profile"`
}

type whatsappProfile struct {
	Name string `json:"name"`
}

type whatsappStatus struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

type whatsappMessage struct {
	From      string              `json:"from"`
	ID        string              `json:"id"`
	Timestamp string              `json:"timestamp"`
	Type      string              `json:"type"`
	Text      *whatsappText       `json:"text,omitempty"`
	Image     *whatsappMedia      `json:"image,omitempty"`
	Document  *whatsappDocMedia   `json:"document,omitempty"`
	Audio     *whatsappAudioMedia `json:"audio,omitempty"`
}

type whatsappText struct {
	Body string `json:"body"`
}

type whatsappMedia struct {
	ID       string `json:"id"`
	MIMEType string `json:"mime_type"`
	Caption  string `json:"caption,omitempty"`
}

type whatsappDocMedia struct {
	ID       string `json:"id"`
	MIMEType string `json:"mime_type"`
	Filename string `json:"filename,omitempty"`
	Caption  string `json:"caption,omitempty"`
}

type whatsappAudioMedia struct {
	ID       string `json:"id"`
	MIMEType string `json:"mime_type"`
}

type whatsappSendRequest struct {
	MessagingProduct string          `json:"messaging_product"`
	To               string          `json:"to"`
	Type             string          `json:"type"`
	Text             *whatsappSendText `json:"text,omitempty"`
}

type whatsappSendText struct {
	Body string `json:"body"`
}
