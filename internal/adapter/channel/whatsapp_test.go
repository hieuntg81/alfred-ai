package channel

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

func newWhatsAppTestLogger() *slog.Logger { return slog.Default() }

func TestWhatsAppChannelName(t *testing.T) {
	ch := NewWhatsAppChannel("token", "phone-id", "verify", "secret", ":0", newWhatsAppTestLogger())
	if ch.Name() != "whatsapp" {
		t.Errorf("Name = %q, want whatsapp", ch.Name())
	}
}

func TestWhatsAppWebhookVerification(t *testing.T) {
	ch := NewWhatsAppChannel("token", "phone-id", "my-verify-token", "", ":0", newWhatsAppTestLogger())
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error { return nil }); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	url := fmt.Sprintf("http://%s/webhook?hub.mode=subscribe&hub.verify_token=my-verify-token&hub.challenge=test-challenge", ch.BoundAddr())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "test-challenge" {
		t.Errorf("body = %q, want test-challenge", string(body))
	}
}

func TestWhatsAppWebhookVerificationReject(t *testing.T) {
	ch := NewWhatsAppChannel("token", "phone-id", "my-verify-token", "", ":0", newWhatsAppTestLogger())
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error { return nil }); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	url := fmt.Sprintf("http://%s/webhook?hub.mode=subscribe&hub.verify_token=wrong-token&hub.challenge=test", ch.BoundAddr())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestWhatsAppReceiveTextMessage(t *testing.T) {
	var received domain.InboundMessage
	var handlerCalled atomic.Int32

	ch := NewWhatsAppChannel("token", "phone-id", "verify", "", ":0", newWhatsAppTestLogger())
	if err := ch.Start(context.Background(), func(_ context.Context, msg domain.InboundMessage) error {
		received = msg
		handlerCalled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	payload := whatsappWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []whatsappEntry{{
			ID: "entry-1",
			Changes: []whatsappChange{{
				Field: "messages",
				Value: whatsappChangeValue{
					Messages: []whatsappMessage{{
						From: "+1234567890",
						Type: "text",
						Text: &whatsappText{Body: "Hello Alfred"},
					}},
				},
			}},
		}},
	}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(50 * time.Millisecond)

	if handlerCalled.Load() != 1 {
		t.Fatalf("handler called %d times, want 1", handlerCalled.Load())
	}
	if received.SessionID != "+1234567890" {
		t.Errorf("SessionID = %q", received.SessionID)
	}
	if received.SenderID != "+1234567890" {
		t.Errorf("SenderID = %q", received.SenderID)
	}
	if received.Content != "Hello Alfred" {
		t.Errorf("Content = %q", received.Content)
	}
	if received.ChannelName != "whatsapp" {
		t.Errorf("ChannelName = %q", received.ChannelName)
	}
}

func TestWhatsAppReceiveWithSenderInfo(t *testing.T) {
	var received domain.InboundMessage

	ch := NewWhatsAppChannel("token", "phone-id", "verify", "", ":0", newWhatsAppTestLogger())
	if err := ch.Start(context.Background(), func(_ context.Context, msg domain.InboundMessage) error {
		received = msg
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	payload := whatsappWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []whatsappEntry{{
			Changes: []whatsappChange{{
				Field: "messages",
				Value: whatsappChangeValue{
					Contacts: []whatsappContact{{
						WaID:    "+1234567890",
						Profile: whatsappProfile{Name: "Alice"},
					}},
					Messages: []whatsappMessage{{
						From: "+1234567890",
						Type: "text",
						Text: &whatsappText{Body: "Hi"},
					}},
				},
			}},
		}},
	}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(50 * time.Millisecond)

	if received.SenderName != "Alice" {
		t.Errorf("SenderName = %q, want Alice", received.SenderName)
	}
}

func TestWhatsAppSendMessage(t *testing.T) {
	var sentPayload whatsappSendRequest
	var authHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&sentPayload)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"messages":[{"id":"msg-id"}]}`))
	}))
	defer server.Close()

	ch := NewWhatsAppChannel("test-token", "phone-123", "verify", "", ":0", newWhatsAppTestLogger())
	ch.baseURL = server.URL

	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "+1234567890",
		Content:   "Hello from Alfred",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if sentPayload.MessagingProduct != "whatsapp" {
		t.Errorf("MessagingProduct = %q", sentPayload.MessagingProduct)
	}
	if sentPayload.To != "+1234567890" {
		t.Errorf("To = %q", sentPayload.To)
	}
	if sentPayload.Text == nil || sentPayload.Text.Body != "Hello from Alfred" {
		t.Errorf("Text.Body = %v", sentPayload.Text)
	}
	if authHeader != "Bearer test-token" {
		t.Errorf("Authorization = %q", authHeader)
	}
}

func TestWhatsAppSendErrorMessage(t *testing.T) {
	var sentPayload whatsappSendRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&sentPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ch := NewWhatsAppChannel("token", "phone-id", "verify", "", ":0", newWhatsAppTestLogger())
	ch.baseURL = server.URL

	ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "+1234567890",
		Content:   "something went wrong",
		IsError:   true,
	})

	if sentPayload.Text == nil || sentPayload.Text.Body != "Error: something went wrong" {
		t.Errorf("Text.Body = %v", sentPayload.Text)
	}
}

func TestWhatsAppSendAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer server.Close()

	ch := NewWhatsAppChannel("token", "phone-id", "verify", "", ":0", newWhatsAppTestLogger())
	ch.baseURL = server.URL

	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "+1234567890",
		Content:   "test",
	})
	if err == nil {
		t.Error("expected error for API error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error, got %v", err)
	}
}

func TestWhatsAppWebhookAlways200(t *testing.T) {
	ch := NewWhatsAppChannel("token", "phone-id", "verify", "", ":0", newWhatsAppTestLogger())
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error { return nil }); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	// Send invalid JSON — should still get 200.
	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader("not json"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 for bad payload", resp.StatusCode)
	}
}

func TestWhatsAppWebhookSignature(t *testing.T) {
	appSecret := "test-app-secret"
	var handlerCalled atomic.Int32

	ch := NewWhatsAppChannel("token", "phone-id", "verify", appSecret, ":0", newWhatsAppTestLogger())
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	payload := whatsappWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []whatsappEntry{{
			Changes: []whatsappChange{{
				Field: "messages",
				Value: whatsappChangeValue{
					Messages: []whatsappMessage{{
						From: "+1234567890",
						Type: "text",
						Text: &whatsappText{Body: "signed msg"},
					}},
				},
			}},
		}},
	}
	body, _ := json.Marshal(payload)

	// Test with valid signature.
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(body)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", validSig)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(50 * time.Millisecond)
	if handlerCalled.Load() != 1 {
		t.Errorf("valid sig: handler called %d times, want 1", handlerCalled.Load())
	}

	// Test with invalid signature — handler should NOT be called again.
	req2, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(string(body)))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Hub-Signature-256", "sha256=invalidsignature00000000000000000000000000000000000000000000000")

	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp2.Body.Close()

	time.Sleep(50 * time.Millisecond)
	if handlerCalled.Load() != 1 {
		t.Errorf("invalid sig: handler called %d times, want still 1", handlerCalled.Load())
	}
}

func TestWhatsAppStopBeforeStart(t *testing.T) {
	ch := NewWhatsAppChannel("token", "phone-id", "verify", "", ":0", newWhatsAppTestLogger())
	if err := ch.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestWhatsAppStartStop(t *testing.T) {
	ch := NewWhatsAppChannel("token", "phone-id", "verify", "", ":0", newWhatsAppTestLogger())
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error { return nil }); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if ch.BoundAddr() == "" {
		t.Error("BoundAddr should not be empty after Start")
	}

	if err := ch.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestWhatsAppCommandHelp(t *testing.T) {
	var sentText string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req whatsappSendRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Text != nil {
			sentText = req.Text.Body
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var handlerCalled bool
	ch := NewWhatsAppChannel("token", "phone-id", "verify", "", ":0", newWhatsAppTestLogger())
	ch.baseURL = server.URL
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error {
		handlerCalled = true
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	payload := whatsappWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []whatsappEntry{{
			Changes: []whatsappChange{{
				Field: "messages",
				Value: whatsappChangeValue{
					Messages: []whatsappMessage{{
						From: "+1234567890",
						Type: "text",
						Text: &whatsappText{Body: "/help"},
					}},
				},
			}},
		}},
	}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(50 * time.Millisecond)

	if handlerCalled {
		t.Error("handler should not be called for /help command")
	}
	if sentText == "" {
		t.Error("help text should have been sent")
	}
}

func TestWhatsAppCommandPrivacy(t *testing.T) {
	var sentText string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req whatsappSendRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Text != nil {
			sentText = req.Text.Body
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ch := NewWhatsAppChannel("token", "phone-id", "verify", "", ":0", newWhatsAppTestLogger())
	ch.baseURL = server.URL
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error { return nil }); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	payload := whatsappWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []whatsappEntry{{
			Changes: []whatsappChange{{
				Field: "messages",
				Value: whatsappChangeValue{
					Messages: []whatsappMessage{{
						From: "+1234567890",
						Type: "text",
						Text: &whatsappText{Body: "/privacy"},
					}},
				},
			}},
		}},
	}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(50 * time.Millisecond)

	if !strings.Contains(sentText, "Privacy") {
		t.Errorf("expected privacy text, got %q", sentText)
	}
}

func TestWhatsAppMentionOnly(t *testing.T) {
	// WhatsApp mentionOnly with a group message (GroupID set) should be skipped.
	ch := NewWhatsAppChannel("token", "phone-id", "verify", "", ":0", newWhatsAppTestLogger(), WithWhatsAppMentionOnly(true))

	var handlerCalled bool
	// Directly test the dispatchMessage with a group context.
	ch.handler = func(_ context.Context, _ domain.InboundMessage) error {
		handlerCalled = true
		return nil
	}

	ch.dispatchMessage(context.Background(), "+1234567890", "group-123", "hello", nil, nil)

	if handlerCalled {
		t.Error("handler should not be called for group message in mentionOnly mode")
	}
}

func TestWhatsAppStatusIgnored(t *testing.T) {
	var handlerCalled atomic.Int32

	ch := NewWhatsAppChannel("token", "phone-id", "verify", "", ":0", newWhatsAppTestLogger())
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	// Payload with only statuses, no messages.
	payload := whatsappWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []whatsappEntry{{
			Changes: []whatsappChange{{
				Field: "messages",
				Value: whatsappChangeValue{
					Statuses: []whatsappStatus{{
						ID:     "status-1",
						Status: "delivered",
					}},
				},
			}},
		}},
	}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(50 * time.Millisecond)

	if handlerCalled.Load() != 0 {
		t.Errorf("handler called %d times, want 0 for status-only webhook", handlerCalled.Load())
	}
}

func TestWhatsAppNonTextIgnored(t *testing.T) {
	var handlerCalled atomic.Int32

	ch := NewWhatsAppChannel("token", "phone-id", "verify", "", ":0", newWhatsAppTestLogger())
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	// Location message (no text, no caption).
	payload := whatsappWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []whatsappEntry{{
			Changes: []whatsappChange{{
				Field: "messages",
				Value: whatsappChangeValue{
					Messages: []whatsappMessage{{
						From: "+1234567890",
						Type: "location",
					}},
				},
			}},
		}},
	}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(50 * time.Millisecond)

	if handlerCalled.Load() != 0 {
		t.Errorf("handler called %d times, want 0 for non-text message", handlerCalled.Load())
	}
}

func TestWhatsAppMediaExtraction(t *testing.T) {
	var received domain.InboundMessage

	ch := NewWhatsAppChannel("token", "phone-id", "verify", "", ":0", newWhatsAppTestLogger())
	if err := ch.Start(context.Background(), func(_ context.Context, msg domain.InboundMessage) error {
		received = msg
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	payload := whatsappWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []whatsappEntry{{
			Changes: []whatsappChange{{
				Field: "messages",
				Value: whatsappChangeValue{
					Messages: []whatsappMessage{{
						From: "+1234567890",
						Type: "image",
						Image: &whatsappMedia{
							ID:       "img-123",
							MIMEType: "image/jpeg",
							Caption:  "a photo",
						},
					}},
				},
			}},
		}},
	}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(50 * time.Millisecond)

	if received.Content != "a photo" {
		t.Errorf("Content = %q, want 'a photo'", received.Content)
	}
	if len(received.Media) != 1 {
		t.Fatalf("Media len = %d, want 1", len(received.Media))
	}
	if received.Media[0].Type != domain.MediaTypeImage {
		t.Errorf("Media[0].Type = %q", received.Media[0].Type)
	}
	if received.Media[0].URL != "img-123" {
		t.Errorf("Media[0].URL = %q", received.Media[0].URL)
	}
}

func TestWhatsAppSendUnreachable(t *testing.T) {
	ch := NewWhatsAppChannel("token", "phone-id", "verify", "", ":0", newWhatsAppTestLogger())
	ch.baseURL = "http://localhost:1" // unreachable

	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "+1234567890",
		Content:   "test",
	})
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

func TestWhatsAppNonMessageFieldIgnored(t *testing.T) {
	var handlerCalled atomic.Int32

	ch := NewWhatsAppChannel("token", "phone-id", "verify", "", ":0", newWhatsAppTestLogger())
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	// Change field is not "messages" — should be ignored.
	payload := whatsappWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []whatsappEntry{{
			Changes: []whatsappChange{{
				Field: "account_update",
				Value: whatsappChangeValue{},
			}},
		}},
	}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(50 * time.Millisecond)

	if handlerCalled.Load() != 0 {
		t.Errorf("handler called %d times, want 0 for non-messages field", handlerCalled.Load())
	}
}
