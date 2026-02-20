package channel

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"alfred-ai/internal/domain"
)

// GoogleChatOption configures a GoogleChatChannel.
type GoogleChatOption func(*GoogleChatChannel)

// WithGoogleChatWebhookAddr sets the webhook listener address.
func WithGoogleChatWebhookAddr(addr string) GoogleChatOption {
	return func(g *GoogleChatChannel) { g.webhookAddr = addr }
}

// WithGoogleChatMentionOnly filters out non-mention messages in spaces.
func WithGoogleChatMentionOnly(v bool) GoogleChatOption {
	return func(g *GoogleChatChannel) { g.mentionOnly = v }
}

// WithGoogleChatSpaceID restricts the channel to a specific space.
func WithGoogleChatSpaceID(id string) GoogleChatOption {
	return func(g *GoogleChatChannel) { g.spaceID = id }
}

// GoogleChatChannel implements domain.Channel for Google Chat via webhooks.
type GoogleChatChannel struct {
	credentialsFile string
	webhookAddr     string
	spaceID         string
	mentionOnly     bool

	handler   domain.MessageHandler
	logger    *slog.Logger
	client    *http.Client
	server    *http.Server
	boundAddr string
	baseCtx   context.Context
	done      chan struct{}

	// Service account auth
	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
	saEmail     string
	saKey       *rsa.PrivateKey
}

// NewGoogleChatChannel creates a Google Chat channel.
func NewGoogleChatChannel(credentialsFile string, logger *slog.Logger, opts ...GoogleChatOption) *GoogleChatChannel {
	g := &GoogleChatChannel{
		credentialsFile: credentialsFile,
		webhookAddr:     ":8081",
		logger:          logger,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		done: make(chan struct{}),
	}
	for _, o := range opts {
		o(g)
	}
	return g
}

// Name implements domain.Channel.
func (g *GoogleChatChannel) Name() string { return "googlechat" }

// Start implements domain.Channel.
func (g *GoogleChatChannel) Start(ctx context.Context, handler domain.MessageHandler) error {
	g.handler = handler
	g.baseCtx = ctx

	// Load service account credentials for outbound API calls.
	if err := g.loadCredentials(); err != nil {
		return fmt.Errorf("googlechat credentials: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", g.handleWebhook)

	g.server = &http.Server{
		Addr:              g.webhookAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	ln, err := net.Listen("tcp", g.webhookAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", g.webhookAddr, err)
	}
	g.boundAddr = ln.Addr().String()

	go func() {
		g.logger.Info("googlechat webhook started", "addr", g.boundAddr)
		if err := g.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			g.logger.Error("googlechat webhook error", "error", err)
		}
	}()

	return nil
}

// Stop implements domain.Channel.
func (g *GoogleChatChannel) Stop(ctx context.Context) error {
	select {
	case <-g.done:
	default:
		close(g.done)
	}
	if g.server == nil {
		return nil
	}
	return g.server.Shutdown(ctx)
}

// Send implements domain.Channel.
func (g *GoogleChatChannel) Send(ctx context.Context, msg domain.OutboundMessage) error {
	content := msg.Content
	if msg.IsError {
		content = "Error: " + content
	}

	spaceName := msg.SessionID
	if spaceName == "" {
		return fmt.Errorf("googlechat: session_id (space name) is required")
	}

	return g.sendMessage(ctx, spaceName, content, msg.ThreadID)
}

// BoundAddr returns the actual address the webhook server is listening on.
// Useful for testing with dynamic port allocation.
func (g *GoogleChatChannel) BoundAddr() string {
	return g.boundAddr
}

// --- Webhook handling ---

func (g *GoogleChatChannel) handleWebhook(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1*1024*1024))
	if err != nil {
		g.logger.Warn("googlechat read body error", "error", err)
		rw.WriteHeader(http.StatusOK)
		return
	}

	var event gchatEvent
	if err := json.Unmarshal(body, &event); err != nil {
		g.logger.Warn("googlechat unmarshal error", "error", err)
		rw.WriteHeader(http.StatusOK)
		return
	}

	// Google Chat expects a quick 200 response.
	rw.WriteHeader(http.StatusOK)

	// Process async to avoid blocking the webhook response.
	// Use the base context from Start(), not r.Context(), because r.Context()
	// is cancelled once the handler returns.
	go g.processEvent(g.baseCtx, &event)
}

func (g *GoogleChatChannel) processEvent(ctx context.Context, event *gchatEvent) {
	switch event.Type {
	case "MESSAGE":
		g.processMessage(ctx, event)
	case "ADDED_TO_SPACE":
		g.logger.Info("googlechat added to space", "space", event.Space.Name)
	case "REMOVED_FROM_SPACE":
		g.logger.Info("googlechat removed from space", "space", event.Space.Name)
	default:
		g.logger.Debug("googlechat ignored event", "type", event.Type)
	}
}

func (g *GoogleChatChannel) processMessage(ctx context.Context, event *gchatEvent) {
	text := event.Message.ArgumentText
	if text == "" {
		text = event.Message.Text
	}
	text = strings.TrimSpace(text)

	if text == "" {
		return
	}

	// Filter by space if configured.
	if g.spaceID != "" && event.Space.Name != "spaces/"+g.spaceID {
		return
	}

	// In spaces (group chats), optionally only respond to mentions.
	isMention := event.Message.ArgumentText != ""
	if g.mentionOnly && event.Space.Type != "DM" && !isMention {
		return
	}

	// Handle commands.
	if g.handleCommand(ctx, event.Space.Name, text, event.Message.Thread.Name) {
		return
	}

	inbound := domain.InboundMessage{
		SessionID:   event.Space.Name,
		Content:     text,
		ChannelName: "googlechat",
		SenderID:    event.User.Name,
		SenderName:  event.User.DisplayName,
		GroupID:     event.Space.Name,
		ThreadID:    event.Message.Thread.Name,
		IsMention:   isMention,
	}

	if err := g.handler(ctx, inbound); err != nil {
		g.logger.Error("googlechat handler error", "error", err, "space", event.Space.Name)
	}
}

func (g *GoogleChatChannel) handleCommand(ctx context.Context, spaceName, content, threadName string) bool {
	fields := strings.Fields(content)
	if len(fields) == 0 {
		return false
	}

	switch fields[0] {
	case "/help", "/start":
		_ = g.sendMessage(ctx, spaceName, GetHelpText("googlechat"), threadName)
		return true
	case "/privacy":
		_ = g.sendMessage(ctx, spaceName, GetPrivacyText(), threadName)
		return true
	default:
		return false
	}
}

// --- Outbound messaging ---

func (g *GoogleChatChannel) sendMessage(ctx context.Context, spaceName, text, threadName string) error {
	token, err := g.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}

	url := fmt.Sprintf("https://chat.googleapis.com/v1/%s/messages", spaceName)

	payload := gchatSendMessage{Text: text}
	if threadName != "" {
		payload.Thread = &gchatThread{Name: threadName}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("googlechat API error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// --- Service account authentication ---

// serviceAccountJSON is the JSON structure of a Google Cloud service account key file.
type serviceAccountJSON struct {
	Type         string `json:"type"`
	ClientEmail  string `json:"client_email"`
	PrivateKey   string `json:"private_key"`
	TokenURI     string `json:"token_uri"`
}

func (g *GoogleChatChannel) loadCredentials() error {
	data, err := os.ReadFile(g.credentialsFile)
	if err != nil {
		return fmt.Errorf("read credentials file: %w", err)
	}

	var sa serviceAccountJSON
	if err := json.Unmarshal(data, &sa); err != nil {
		return fmt.Errorf("parse credentials: %w", err)
	}

	if sa.ClientEmail == "" || sa.PrivateKey == "" {
		return fmt.Errorf("credentials missing client_email or private_key")
	}

	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return fmt.Errorf("failed to decode PEM block from private key")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse private key: %w", err)
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return fmt.Errorf("private key is not RSA")
	}

	g.saEmail = sa.ClientEmail
	g.saKey = rsaKey

	return nil
}

func (g *GoogleChatChannel) getAccessToken(ctx context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Return cached token if still valid.
	if g.accessToken != "" && time.Now().Before(g.tokenExpiry.Add(-60*time.Second)) {
		return g.accessToken, nil
	}

	token, expiry, err := g.exchangeJWTForToken(ctx)
	if err != nil {
		return "", err
	}

	g.accessToken = token
	g.tokenExpiry = expiry
	return token, nil
}

func (g *GoogleChatChannel) exchangeJWTForToken(ctx context.Context) (string, time.Time, error) {
	now := time.Now()
	expiry := now.Add(55 * time.Minute)

	// Build JWT claims.
	claims := map[string]interface{}{
		"iss":   g.saEmail,
		"sub":   g.saEmail,
		"aud":   "https://oauth2.googleapis.com/token",
		"iat":   now.Unix(),
		"exp":   expiry.Unix(),
		"scope": "https://www.googleapis.com/auth/chat.bot",
	}

	signedJWT, err := signJWT(claims, g.saKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign jwt: %w", err)
	}

	// Exchange JWT for access token.
	form := "grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Ajwt-bearer&assertion=" + signedJWT

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://oauth2.googleapis.com/token",
		strings.NewReader(form))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := g.client.Do(req)
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

	tokenExpiry := now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	return tokenResp.AccessToken, tokenExpiry, nil
}

// --- JWT signing (minimal, no external dependency) ---

func signJWT(claims map[string]interface{}, key *rsa.PrivateKey) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))

	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)

	signingInput := header + "." + payload

	hash := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(nil, key, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// --- Google Chat API wire types ---

type gchatEvent struct {
	Type    string       `json:"type"`
	Message gchatMessage `json:"message"`
	User    gchatUser    `json:"user"`
	Space   gchatSpace   `json:"space"`
}

type gchatMessage struct {
	Name         string      `json:"name"`
	Text         string      `json:"text"`
	ArgumentText string      `json:"argumentText"`
	Thread       gchatThread `json:"thread"`
}

type gchatThread struct {
	Name string `json:"name"`
}

type gchatUser struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Type        string `json:"type"`
}

type gchatSpace struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type gchatSendMessage struct {
	Text   string       `json:"text"`
	Thread *gchatThread `json:"thread,omitempty"`
}
