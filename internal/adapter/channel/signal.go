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
	"sync"
	"time"

	"alfred-ai/internal/domain"
)

// SignalOption configures a SignalChannel.
type SignalOption func(*SignalChannel)

// WithSignalPollInterval sets the polling interval for receiving messages.
func WithSignalPollInterval(d time.Duration) SignalOption {
	return func(s *SignalChannel) { s.pollInterval = d }
}

// SignalChannel implements domain.Channel for Signal via signal-cli REST API.
type SignalChannel struct {
	apiURL       string
	phone        string
	pollInterval time.Duration

	handler domain.MessageHandler
	logger  *slog.Logger
	client  *http.Client
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewSignalChannel creates a Signal channel that communicates via signal-cli REST API.
func NewSignalChannel(apiURL, phone string, logger *slog.Logger, opts ...SignalOption) *SignalChannel {
	s := &SignalChannel{
		apiURL:       strings.TrimRight(apiURL, "/"),
		phone:        phone,
		pollInterval: 2 * time.Second,
		logger:       logger,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Name implements domain.Channel.
func (s *SignalChannel) Name() string { return "signal" }

// Start implements domain.Channel.
func (s *SignalChannel) Start(ctx context.Context, handler domain.MessageHandler) error {
	s.handler = handler

	pollCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	s.wg.Add(1)
	go s.pollLoop(pollCtx)

	s.logger.Info("signal channel started", "api_url", s.apiURL, "phone", s.phone)
	return nil
}

// Stop implements domain.Channel.
func (s *SignalChannel) Stop(_ context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	return nil
}

// Send implements domain.Channel.
func (s *SignalChannel) Send(ctx context.Context, msg domain.OutboundMessage) error {
	content := msg.Content
	if msg.IsError {
		content = "Error: " + content
	}

	recipient := msg.SessionID
	if recipient == "" {
		return fmt.Errorf("signal: session_id (recipient) is required")
	}

	return s.sendMessage(ctx, recipient, content)
}

// --- Polling ---

func (s *SignalChannel) pollLoop(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.receiveMessages(ctx)
		}
	}
}

func (s *SignalChannel) receiveMessages(ctx context.Context) {
	url := fmt.Sprintf("%s/v1/receive/%s", s.apiURL, s.phone)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}

	resp, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Debug("signal receive error", "error", err)
		}
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return
	}

	if resp.StatusCode != http.StatusOK {
		s.logger.Debug("signal receive non-200", "status", resp.StatusCode)
		return
	}

	var messages []signalEnvelope
	if err := json.Unmarshal(body, &messages); err != nil {
		s.logger.Debug("signal unmarshal error", "error", err)
		return
	}

	for i := range messages {
		s.processEnvelope(ctx, &messages[i])
	}
}

func (s *SignalChannel) processEnvelope(ctx context.Context, env *signalEnvelope) {
	dm := env.Envelope.DataMessage
	if dm == nil || dm.Message == "" {
		return
	}

	text := strings.TrimSpace(dm.Message)
	if text == "" {
		return
	}

	sender := env.Envelope.Source
	if sender == "" {
		return
	}

	// Handle commands.
	if s.handleCommand(ctx, sender, text) {
		return
	}

	// Determine group ID if present.
	groupID := ""
	if dm.GroupInfo != nil {
		groupID = dm.GroupInfo.GroupID
	}

	inbound := domain.InboundMessage{
		SessionID:   sender,
		Content:     text,
		ChannelName: "signal",
		SenderID:    sender,
		SenderName:  env.Envelope.SourceName,
		GroupID:     groupID,
	}

	if err := s.handler(ctx, inbound); err != nil {
		s.logger.Error("signal handler error", "error", err, "sender", sender)
	}
}

func (s *SignalChannel) handleCommand(ctx context.Context, recipient, content string) bool {
	fields := strings.Fields(content)
	if len(fields) == 0 {
		return false
	}

	switch fields[0] {
	case "/help", "/start":
		_ = s.sendMessage(ctx, recipient, GetHelpText("signal"))
		return true
	case "/privacy":
		_ = s.sendMessage(ctx, recipient, GetPrivacyText())
		return true
	default:
		return false
	}
}

// --- Outbound messaging ---

func (s *SignalChannel) sendMessage(ctx context.Context, recipient, text string) error {
	url := fmt.Sprintf("%s/v1/send", s.apiURL)

	payload := signalSendRequest{
		Message:    text,
		Number:     s.phone,
		Recipients: []string{recipient},
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

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("signal API error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// --- signal-cli REST API wire types ---

type signalEnvelope struct {
	Envelope signalEnvelopeData `json:"envelope"`
}

type signalEnvelopeData struct {
	Source      string             `json:"source"`
	SourceName  string             `json:"sourceName"`
	Timestamp   int64              `json:"timestamp"`
	DataMessage *signalDataMessage `json:"dataMessage"`
}

type signalDataMessage struct {
	Message   string           `json:"message"`
	Timestamp int64            `json:"timestamp"`
	GroupInfo *signalGroupInfo `json:"groupInfo,omitempty"`
}

type signalGroupInfo struct {
	GroupID string `json:"groupId"`
	Type    string `json:"type"`
}

type signalSendRequest struct {
	Message    string   `json:"message"`
	Number     string   `json:"number"`
	Recipients []string `json:"recipients"`
}
