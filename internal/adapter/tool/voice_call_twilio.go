package tool

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"alfred-ai/internal/domain"
)

// TwilioBackendConfig holds Twilio-specific configuration.
type TwilioBackendConfig struct {
	AccountSID string
	AuthToken  string
	FromNumber string
}

// TwilioBackend implements VoiceCallBackend using the Twilio REST API.
type TwilioBackend struct {
	config TwilioBackendConfig
	client *http.Client
	logger *slog.Logger
}

// NewTwilioBackend creates a new Twilio backend.
func NewTwilioBackend(cfg TwilioBackendConfig, logger *slog.Logger) *TwilioBackend {
	return &TwilioBackend{
		config: cfg,
		client: &http.Client{Timeout: 30 * time.Second},
		logger: logger,
	}
}

func (b *TwilioBackend) Name() string { return "twilio" }

// InitiateCall places an outbound call via the Twilio REST API.
func (b *TwilioBackend) InitiateCall(ctx context.Context, req InitiateCallRequest) (*InitiateCallResponse, error) {
	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Calls.json", b.config.AccountSID)

	// Generate TwiML based on mode.
	twiml := b.generateTwiML(req)

	form := url.Values{
		"To":          {req.To},
		"From":        {req.From},
		"Twiml":       {twiml},
		"StatusCallback": {req.WebhookURL},
		"StatusCallbackEvent": {"initiated ringing answered completed"},
		"StatusCallbackMethod": {"POST"},
	}

	if req.MaxDuration > 0 {
		form.Set("Timeout", fmt.Sprintf("%d", req.MaxDuration))
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.SetBasicAuth(b.config.AccountSID, b.config.AuthToken)

	resp, err := b.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("twilio api call: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("twilio api error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		SID    string `json:"sid"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse twilio response: %w", err)
	}

	return &InitiateCallResponse{
		ProviderCallID: result.SID,
		Status:         result.Status,
	}, nil
}

// HangupCall terminates a call via the Twilio REST API.
func (b *TwilioBackend) HangupCall(ctx context.Context, req HangupCallRequest) error {
	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Calls/%s.json",
		b.config.AccountSID, req.ProviderCallID)

	form := url.Values{
		"Status": {"completed"},
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.SetBasicAuth(b.config.AccountSID, b.config.AuthToken)

	resp, err := b.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("twilio hangup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("twilio hangup error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// PlayTTS plays text-to-speech into an active call.
// This updates the call with new TwiML containing a <Say> verb.
func (b *TwilioBackend) PlayTTS(ctx context.Context, req PlayTTSRequest) error {
	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Calls/%s.json",
		b.config.AccountSID, req.ProviderCallID)

	twiml := fmt.Sprintf(`<Response><Say>%s</Say></Response>`, xmlEscape(req.Message))

	form := url.Values{
		"Twiml": {twiml},
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.SetBasicAuth(b.config.AccountSID, b.config.AuthToken)

	resp, err := b.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("twilio play tts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("twilio play tts error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// VerifyWebhook validates a Twilio webhook signature using HMAC-SHA1.
func (b *TwilioBackend) VerifyWebhook(_ context.Context, req WebhookVerifyRequest) error {
	if req.Signature == "" {
		return fmt.Errorf("%w: missing signature", domain.ErrPermissionDenied)
	}

	// Compute expected signature.
	expectedSig := computeTwilioSignature(b.config.AuthToken, req.URL, req.Body)

	sigBytes, err := base64.StdEncoding.DecodeString(req.Signature)
	if err != nil {
		return fmt.Errorf("%w: invalid signature encoding", domain.ErrPermissionDenied)
	}

	if !hmac.Equal(sigBytes, expectedSig) {
		return fmt.Errorf("%w: signature mismatch", domain.ErrPermissionDenied)
	}

	return nil
}

// ParseWebhookEvent parses a Twilio status callback into normalized CallEvents.
func (b *TwilioBackend) ParseWebhookEvent(_ context.Context, req WebhookParseRequest) ([]CallEvent, *WebhookResponse, error) {
	// Twilio sends status callbacks as form-encoded POST.
	values, err := url.ParseQuery(string(req.Body))
	if err != nil {
		return nil, nil, fmt.Errorf("parse form body: %w", err)
	}

	callSID := values.Get("CallSid")
	callStatus := values.Get("CallStatus")

	if callSID == "" {
		return nil, nil, fmt.Errorf("missing CallSid in webhook")
	}

	// Map Twilio status to internal CallState.
	state := mapTwilioStatus(callStatus)

	event := CallEvent{
		ProviderCallID: callSID,
		Type:           callStatus,
		Status:         state,
		Timestamp:      time.Now().UTC(),
	}

	return []CallEvent{event}, nil, nil
}

// generateTwiML creates TwiML based on the call mode.
func (b *TwilioBackend) generateTwiML(req InitiateCallRequest) string {
	switch req.Mode {
	case "conversation":
		// Conversation mode: greet then connect to media stream.
		streamURL := req.StreamURL
		// Convert http(s) to ws(s) for WebSocket.
		streamURL = strings.Replace(streamURL, "https://", "wss://", 1)
		streamURL = strings.Replace(streamURL, "http://", "ws://", 1)
		return fmt.Sprintf(
			`<Response>`+
				`<Say>%s</Say>`+
				`<Connect><Stream url="%s" /></Connect>`+
				`</Response>`,
			xmlEscape(req.Message), xmlEscape(streamURL),
		)
	default:
		// Notify mode: speak message and hang up.
		return fmt.Sprintf(
			`<Response><Say>%s</Say><Hangup/></Response>`,
			xmlEscape(req.Message),
		)
	}
}

// computeTwilioSignature computes the HMAC-SHA1 signature for a Twilio webhook.
func computeTwilioSignature(authToken, webhookURL string, body []byte) []byte {
	// Parse form values from body and sort by key.
	values, _ := url.ParseQuery(string(body))
	// Build the string to sign: URL + sorted key=value pairs.
	data := webhookURL
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range values[k] {
			data += k + v
		}
	}

	mac := hmac.New(sha1.New, []byte(authToken))
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

// mapTwilioStatus maps Twilio call statuses to internal CallState.
func mapTwilioStatus(status string) CallState {
	switch status {
	case "queued", "initiated":
		return CallStateInitiated
	case "ringing":
		return CallStateRinging
	case "in-progress":
		return CallStateAnswered
	case "completed":
		return CallStateCompleted
	case "busy":
		return CallStateBusy
	case "no-answer":
		return CallStateNoAnswer
	case "canceled":
		return CallStateHangupBot
	case "failed":
		return CallStateFailed
	default:
		return CallStateError
	}
}

// xmlEscape escapes special characters for XML/TwiML content.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// MockVoiceCallBackend is a test double for VoiceCallBackend.
type MockVoiceCallBackend struct {
	InitiateResp *InitiateCallResponse
	InitiateErr  error
	HangupErr    error
	PlayTTSErr   error
	VerifyErr    error
	ParseEvents  []CallEvent
	ParseResp    *WebhookResponse
	ParseErr     error

	InitiateCalls []InitiateCallRequest
	HangupCalls   []HangupCallRequest
	PlayTTSCalls  []PlayTTSRequest
}

func NewMockVoiceCallBackend() *MockVoiceCallBackend {
	return &MockVoiceCallBackend{
		InitiateResp: &InitiateCallResponse{
			ProviderCallID: "mock-provider-id",
			Status:         "initiated",
		},
	}
}

func (m *MockVoiceCallBackend) InitiateCall(_ context.Context, req InitiateCallRequest) (*InitiateCallResponse, error) {
	m.InitiateCalls = append(m.InitiateCalls, req)
	return m.InitiateResp, m.InitiateErr
}

func (m *MockVoiceCallBackend) HangupCall(_ context.Context, req HangupCallRequest) error {
	m.HangupCalls = append(m.HangupCalls, req)
	return m.HangupErr
}

func (m *MockVoiceCallBackend) PlayTTS(_ context.Context, req PlayTTSRequest) error {
	m.PlayTTSCalls = append(m.PlayTTSCalls, req)
	return m.PlayTTSErr
}

func (m *MockVoiceCallBackend) VerifyWebhook(_ context.Context, _ WebhookVerifyRequest) error {
	return m.VerifyErr
}

func (m *MockVoiceCallBackend) ParseWebhookEvent(_ context.Context, _ WebhookParseRequest) ([]CallEvent, *WebhookResponse, error) {
	return m.ParseEvents, m.ParseResp, m.ParseErr
}

func (m *MockVoiceCallBackend) Name() string { return "mock" }
