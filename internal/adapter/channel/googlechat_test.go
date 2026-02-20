package channel

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

func newGChatTestLogger() *slog.Logger { return slog.Default() }

// writeTestCredentials creates a temporary service account JSON file with a real RSA key.
func writeTestCredentials(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal PKCS8: %v", err)
	}

	pemBlock := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})

	sa := serviceAccountJSON{
		Type:        "service_account",
		ClientEmail: "test@test.iam.gserviceaccount.com",
		PrivateKey:  string(pemBlock),
		TokenURI:    "https://oauth2.googleapis.com/token",
	}

	data, _ := json.Marshal(sa)
	f, err := os.CreateTemp(t.TempDir(), "sa-*.json")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	f.Write(data)
	f.Close()
	return f.Name(), key
}

func TestGoogleChatChannelName(t *testing.T) {
	ch := NewGoogleChatChannel("/nonexistent.json", newGChatTestLogger())
	if ch.Name() != "googlechat" {
		t.Errorf("Name = %q, want googlechat", ch.Name())
	}
}

func TestGoogleChatOptions(t *testing.T) {
	ch := NewGoogleChatChannel("/nonexistent.json", newGChatTestLogger(),
		WithGoogleChatWebhookAddr(":9999"),
		WithGoogleChatMentionOnly(true),
		WithGoogleChatSpaceID("abc123"),
	)
	if ch.webhookAddr != ":9999" {
		t.Errorf("webhookAddr = %q, want :9999", ch.webhookAddr)
	}
	if !ch.mentionOnly {
		t.Error("mentionOnly should be true")
	}
	if ch.spaceID != "abc123" {
		t.Errorf("spaceID = %q, want abc123", ch.spaceID)
	}
}

func TestGoogleChatDefaultWebhookAddr(t *testing.T) {
	ch := NewGoogleChatChannel("/nonexistent.json", newGChatTestLogger())
	if ch.webhookAddr != ":8081" {
		t.Errorf("default webhookAddr = %q, want :8081", ch.webhookAddr)
	}
}

func TestGoogleChatStartBadCredentials(t *testing.T) {
	ch := NewGoogleChatChannel("/nonexistent.json", newGChatTestLogger(), WithGoogleChatWebhookAddr(":0"))
	err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error { return nil })
	if err == nil {
		t.Error("expected error for missing credentials file")
	}
	if !strings.Contains(err.Error(), "credentials") {
		t.Errorf("error = %v, want credentials-related error", err)
	}
}

func TestGoogleChatStartStop(t *testing.T) {
	credFile, _ := writeTestCredentials(t)

	ch := NewGoogleChatChannel(credFile, newGChatTestLogger(), WithGoogleChatWebhookAddr(":0"))
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

func TestGoogleChatStopBeforeStart(t *testing.T) {
	ch := NewGoogleChatChannel("/nonexistent.json", newGChatTestLogger())
	if err := ch.Stop(context.Background()); err != nil {
		t.Errorf("Stop before Start: %v", err)
	}
}

func TestGoogleChatWebhookMethodNotAllowed(t *testing.T) {
	credFile, _ := writeTestCredentials(t)

	ch := NewGoogleChatChannel(credFile, newGChatTestLogger(), WithGoogleChatWebhookAddr(":0"))
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error { return nil }); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestGoogleChatWebhookBadJSON(t *testing.T) {
	credFile, _ := writeTestCredentials(t)

	ch := NewGoogleChatChannel(credFile, newGChatTestLogger(), WithGoogleChatWebhookAddr(":0"))
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error { return nil }); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader("not json"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	// Should still get 200 — Google Chat expects quick response.
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestGoogleChatReceiveMessage(t *testing.T) {
	credFile, _ := writeTestCredentials(t)

	var received domain.InboundMessage
	var handlerCalled atomic.Int32

	ch := NewGoogleChatChannel(credFile, newGChatTestLogger(), WithGoogleChatWebhookAddr(":0"))
	if err := ch.Start(context.Background(), func(_ context.Context, msg domain.InboundMessage) error {
		received = msg
		handlerCalled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	event := gchatEvent{
		Type: "MESSAGE",
		Message: gchatMessage{
			Name: "spaces/abc/messages/123",
			Text: "Hello Alfred",
			Thread: gchatThread{
				Name: "spaces/abc/threads/t1",
			},
		},
		User: gchatUser{
			Name:        "users/user1",
			DisplayName: "Alice",
			Type:        "HUMAN",
		},
		Space: gchatSpace{
			Name: "spaces/abc",
			Type: "DM",
		},
	}

	body, _ := json.Marshal(event)
	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	if handlerCalled.Load() != 1 {
		t.Fatalf("handler called %d times, want 1", handlerCalled.Load())
	}
	if received.SessionID != "spaces/abc" {
		t.Errorf("SessionID = %q, want spaces/abc", received.SessionID)
	}
	if received.Content != "Hello Alfred" {
		t.Errorf("Content = %q, want Hello Alfred", received.Content)
	}
	if received.ChannelName != "googlechat" {
		t.Errorf("ChannelName = %q", received.ChannelName)
	}
	if received.SenderID != "users/user1" {
		t.Errorf("SenderID = %q", received.SenderID)
	}
	if received.SenderName != "Alice" {
		t.Errorf("SenderName = %q", received.SenderName)
	}
	if received.ThreadID != "spaces/abc/threads/t1" {
		t.Errorf("ThreadID = %q", received.ThreadID)
	}
	if received.GroupID != "spaces/abc" {
		t.Errorf("GroupID = %q", received.GroupID)
	}
}

func TestGoogleChatReceiveMentionMessage(t *testing.T) {
	credFile, _ := writeTestCredentials(t)

	var received domain.InboundMessage

	ch := NewGoogleChatChannel(credFile, newGChatTestLogger(), WithGoogleChatWebhookAddr(":0"))
	if err := ch.Start(context.Background(), func(_ context.Context, msg domain.InboundMessage) error {
		received = msg
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	// When bot is mentioned, ArgumentText contains the message without the @mention.
	event := gchatEvent{
		Type: "MESSAGE",
		Message: gchatMessage{
			Name:         "spaces/abc/messages/456",
			Text:         "@Alfred what time is it",
			ArgumentText: "what time is it",
			Thread:       gchatThread{Name: "spaces/abc/threads/t2"},
		},
		User:  gchatUser{Name: "users/user2", DisplayName: "Bob"},
		Space: gchatSpace{Name: "spaces/abc", Type: "ROOM"},
	}

	body, _ := json.Marshal(event)
	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	// Should prefer ArgumentText over Text.
	if received.Content != "what time is it" {
		t.Errorf("Content = %q, want 'what time is it'", received.Content)
	}
	if !received.IsMention {
		t.Error("IsMention should be true when ArgumentText is set")
	}
}

func TestGoogleChatMentionOnlyFilter(t *testing.T) {
	credFile, _ := writeTestCredentials(t)

	var handlerCalled atomic.Int32

	ch := NewGoogleChatChannel(credFile, newGChatTestLogger(),
		WithGoogleChatWebhookAddr(":0"),
		WithGoogleChatMentionOnly(true),
	)
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	// Non-mention message in a ROOM — should be filtered.
	event := gchatEvent{
		Type: "MESSAGE",
		Message: gchatMessage{
			Name: "spaces/abc/messages/789",
			Text: "hello everyone",
		},
		User:  gchatUser{Name: "users/user3"},
		Space: gchatSpace{Name: "spaces/abc", Type: "ROOM"},
	}

	body, _ := json.Marshal(event)
	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	if handlerCalled.Load() != 0 {
		t.Errorf("handler called %d times, want 0 for non-mention in mentionOnly mode", handlerCalled.Load())
	}
}

func TestGoogleChatMentionOnlyAllowsDM(t *testing.T) {
	credFile, _ := writeTestCredentials(t)

	var handlerCalled atomic.Int32

	ch := NewGoogleChatChannel(credFile, newGChatTestLogger(),
		WithGoogleChatWebhookAddr(":0"),
		WithGoogleChatMentionOnly(true),
	)
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	// DM should always pass through even without mention.
	event := gchatEvent{
		Type: "MESSAGE",
		Message: gchatMessage{
			Name: "spaces/dm1/messages/1",
			Text: "hello",
		},
		User:  gchatUser{Name: "users/user4"},
		Space: gchatSpace{Name: "spaces/dm1", Type: "DM"},
	}

	body, _ := json.Marshal(event)
	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	if handlerCalled.Load() != 1 {
		t.Errorf("handler called %d times, want 1 for DM in mentionOnly mode", handlerCalled.Load())
	}
}

func TestGoogleChatSpaceFilter(t *testing.T) {
	credFile, _ := writeTestCredentials(t)

	var handlerCalled atomic.Int32

	ch := NewGoogleChatChannel(credFile, newGChatTestLogger(),
		WithGoogleChatWebhookAddr(":0"),
		WithGoogleChatSpaceID("allowed"),
	)
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	// Message from a different space — should be filtered.
	event := gchatEvent{
		Type: "MESSAGE",
		Message: gchatMessage{
			Name: "spaces/other/messages/1",
			Text: "hello",
		},
		User:  gchatUser{Name: "users/user5"},
		Space: gchatSpace{Name: "spaces/other", Type: "DM"},
	}

	body, _ := json.Marshal(event)
	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	if handlerCalled.Load() != 0 {
		t.Errorf("handler called %d times, want 0 for message from wrong space", handlerCalled.Load())
	}
}

func TestGoogleChatCommandHelp(t *testing.T) {
	credFile, _ := writeTestCredentials(t)

	// Mock token endpoint for outbound sendMessage.
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "mock-token",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	// Mock Chat API to capture outbound messages.
	var sentText string
	chatAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var msg gchatSendMessage
		json.Unmarshal(body, &msg)
		sentText = msg.Text
		w.WriteHeader(http.StatusOK)
	}))
	defer chatAPI.Close()

	var handlerCalled bool
	ch := NewGoogleChatChannel(credFile, newGChatTestLogger(), WithGoogleChatWebhookAddr(":0"))
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error {
		handlerCalled = true
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	// Inject a cached token so sendMessage doesn't need to call real Google API.
	ch.mu.Lock()
	ch.accessToken = "mock-token"
	ch.tokenExpiry = time.Now().Add(1 * time.Hour)
	ch.mu.Unlock()

	// Override the HTTP client to route to our mock Chat API.
	ch.client = chatAPI.Client()
	// We need to also override sendMessage's URL. The sendMessage method builds
	// the URL as https://chat.googleapis.com/v1/{spaceName}/messages.
	// Since we can't easily override that, let's test the command handling
	// by directly calling processMessage which calls handleCommand.
	// But handleCommand calls sendMessage which needs the real URL.
	// Instead, let's inject via an HTTP transport that rewrites URLs.
	originalTransport := http.DefaultTransport
	ch.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			// Redirect Chat API calls to our mock server.
			req.URL.Scheme = "http"
			req.URL.Host = strings.TrimPrefix(chatAPI.URL, "http://")
			return originalTransport.RoundTrip(req)
		}),
		Timeout: 5 * time.Second,
	}

	event := gchatEvent{
		Type: "MESSAGE",
		Message: gchatMessage{
			Name: "spaces/abc/messages/cmd1",
			Text: "/help",
			Thread: gchatThread{
				Name: "spaces/abc/threads/t-cmd",
			},
		},
		User:  gchatUser{Name: "users/user6"},
		Space: gchatSpace{Name: "spaces/abc", Type: "DM"},
	}

	body, _ := json.Marshal(event)
	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(200 * time.Millisecond)

	if handlerCalled {
		t.Error("handler should not be called for /help command")
	}
	if sentText == "" {
		t.Error("help text should have been sent")
	}
	if !strings.Contains(sentText, "alfred-ai") {
		t.Errorf("help text missing 'alfred-ai', got: %q", sentText[:min(80, len(sentText))])
	}
}

func TestGoogleChatCommandPrivacy(t *testing.T) {
	credFile, _ := writeTestCredentials(t)

	var sentText string
	ch := NewGoogleChatChannel(credFile, newGChatTestLogger(), WithGoogleChatWebhookAddr(":0"))
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error { return nil }); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	ch.mu.Lock()
	ch.accessToken = "mock-token"
	ch.tokenExpiry = time.Now().Add(1 * time.Hour)
	ch.mu.Unlock()

	chatAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var msg gchatSendMessage
		json.Unmarshal(body, &msg)
		sentText = msg.Text
		w.WriteHeader(http.StatusOK)
	}))
	defer chatAPI.Close()

	ch.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = strings.TrimPrefix(chatAPI.URL, "http://")
			return http.DefaultTransport.RoundTrip(req)
		}),
		Timeout: 5 * time.Second,
	}

	event := gchatEvent{
		Type: "MESSAGE",
		Message: gchatMessage{
			Name: "spaces/abc/messages/cmd2",
			Text: "/privacy",
		},
		User:  gchatUser{Name: "users/user7"},
		Space: gchatSpace{Name: "spaces/abc", Type: "DM"},
	}

	body, _ := json.Marshal(event)
	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(200 * time.Millisecond)

	if !strings.Contains(sentText, "Privacy") {
		t.Errorf("expected privacy text, got %q", sentText)
	}
}

func TestGoogleChatSendMessage(t *testing.T) {
	credFile, _ := writeTestCredentials(t)

	var sentPayload gchatSendMessage
	var authHeader string
	var requestPath string

	chatAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		requestPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &sentPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer chatAPI.Close()

	ch := NewGoogleChatChannel(credFile, newGChatTestLogger())
	ch.mu.Lock()
	ch.accessToken = "test-access-token"
	ch.tokenExpiry = time.Now().Add(1 * time.Hour)
	ch.mu.Unlock()
	ch.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = strings.TrimPrefix(chatAPI.URL, "http://")
			return http.DefaultTransport.RoundTrip(req)
		}),
		Timeout: 5 * time.Second,
	}

	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "spaces/abc",
		Content:   "Hello from Alfred",
		ThreadID:  "spaces/abc/threads/t1",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if sentPayload.Text != "Hello from Alfred" {
		t.Errorf("Text = %q, want 'Hello from Alfred'", sentPayload.Text)
	}
	if sentPayload.Thread == nil || sentPayload.Thread.Name != "spaces/abc/threads/t1" {
		t.Errorf("Thread = %v", sentPayload.Thread)
	}
	if authHeader != "Bearer test-access-token" {
		t.Errorf("Authorization = %q", authHeader)
	}
	if requestPath != "/v1/spaces/abc/messages" {
		t.Errorf("path = %q", requestPath)
	}
}

func TestGoogleChatSendErrorMessage(t *testing.T) {
	credFile, _ := writeTestCredentials(t)

	var sentPayload gchatSendMessage

	chatAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &sentPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer chatAPI.Close()

	ch := NewGoogleChatChannel(credFile, newGChatTestLogger())
	ch.mu.Lock()
	ch.accessToken = "token"
	ch.tokenExpiry = time.Now().Add(1 * time.Hour)
	ch.mu.Unlock()
	ch.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = strings.TrimPrefix(chatAPI.URL, "http://")
			return http.DefaultTransport.RoundTrip(req)
		}),
		Timeout: 5 * time.Second,
	}

	ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "spaces/abc",
		Content:   "something went wrong",
		IsError:   true,
	})

	if sentPayload.Text != "Error: something went wrong" {
		t.Errorf("Text = %q, want 'Error: something went wrong'", sentPayload.Text)
	}
}

func TestGoogleChatSendMissingSessionID(t *testing.T) {
	ch := NewGoogleChatChannel("/nonexistent.json", newGChatTestLogger())
	ch.mu.Lock()
	ch.accessToken = "token"
	ch.tokenExpiry = time.Now().Add(1 * time.Hour)
	ch.mu.Unlock()

	err := ch.Send(context.Background(), domain.OutboundMessage{
		Content: "test",
	})
	if err == nil {
		t.Error("expected error for missing session_id")
	}
	if !strings.Contains(err.Error(), "session_id") {
		t.Errorf("error = %v, want session_id-related error", err)
	}
}

func TestGoogleChatSendAPIError(t *testing.T) {
	credFile, _ := writeTestCredentials(t)

	chatAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("permission denied"))
	}))
	defer chatAPI.Close()

	ch := NewGoogleChatChannel(credFile, newGChatTestLogger())
	ch.mu.Lock()
	ch.accessToken = "token"
	ch.tokenExpiry = time.Now().Add(1 * time.Hour)
	ch.mu.Unlock()
	ch.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = strings.TrimPrefix(chatAPI.URL, "http://")
			return http.DefaultTransport.RoundTrip(req)
		}),
		Timeout: 5 * time.Second,
	}

	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "spaces/abc",
		Content:   "test",
	})
	if err == nil {
		t.Error("expected error for API error")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %v, want 403 in error", err)
	}
}

func TestGoogleChatLoadCredentials(t *testing.T) {
	credFile, _ := writeTestCredentials(t)

	ch := NewGoogleChatChannel(credFile, newGChatTestLogger())
	if err := ch.loadCredentials(); err != nil {
		t.Fatalf("loadCredentials: %v", err)
	}
	if ch.saEmail != "test@test.iam.gserviceaccount.com" {
		t.Errorf("saEmail = %q", ch.saEmail)
	}
	if ch.saKey == nil {
		t.Error("saKey should not be nil")
	}
}

func TestGoogleChatLoadCredentialsMissingFields(t *testing.T) {
	// Credentials file with empty client_email.
	sa := serviceAccountJSON{
		Type:       "service_account",
		PrivateKey: "-----BEGIN PRIVATE KEY-----\nfoo\n-----END PRIVATE KEY-----\n",
	}
	data, _ := json.Marshal(sa)
	f, err := os.CreateTemp(t.TempDir(), "sa-bad-*.json")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	f.Write(data)
	f.Close()

	ch := NewGoogleChatChannel(f.Name(), newGChatTestLogger())
	if err := ch.loadCredentials(); err == nil {
		t.Error("expected error for missing client_email")
	}
}

func TestGoogleChatSignJWT(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	claims := map[string]interface{}{
		"iss": "test@example.com",
		"aud": "https://oauth2.googleapis.com/token",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	}

	jwt, err := signJWT(claims, key)
	if err != nil {
		t.Fatalf("signJWT: %v", err)
	}

	// JWT should have 3 parts separated by dots.
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Errorf("JWT parts = %d, want 3", len(parts))
	}

	// Each part should be non-empty.
	for i, p := range parts {
		if p == "" {
			t.Errorf("JWT part %d is empty", i)
		}
	}
}

func TestGoogleChatTokenExchange(t *testing.T) {
	credFile, key := writeTestCredentials(t)

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("token request method = %s, want POST", r.Method)
		}
		ct := r.Header.Get("Content-Type")
		if ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", ct)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "exchanged-token",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	ch := NewGoogleChatChannel(credFile, newGChatTestLogger())
	ch.saEmail = "test@test.iam.gserviceaccount.com"
	ch.saKey = key
	ch.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = strings.TrimPrefix(tokenServer.URL, "http://")
			return http.DefaultTransport.RoundTrip(req)
		}),
		Timeout: 5 * time.Second,
	}

	token, err := ch.getAccessToken(context.Background())
	if err != nil {
		t.Fatalf("getAccessToken: %v", err)
	}
	if token != "exchanged-token" {
		t.Errorf("token = %q, want exchanged-token", token)
	}

	// Second call should return cached token.
	token2, err := ch.getAccessToken(context.Background())
	if err != nil {
		t.Fatalf("getAccessToken (cached): %v", err)
	}
	if token2 != "exchanged-token" {
		t.Errorf("cached token = %q, want exchanged-token", token2)
	}
}

func TestGoogleChatAddedToSpaceEvent(t *testing.T) {
	credFile, _ := writeTestCredentials(t)

	var handlerCalled atomic.Int32

	ch := NewGoogleChatChannel(credFile, newGChatTestLogger(), WithGoogleChatWebhookAddr(":0"))
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	event := gchatEvent{
		Type:  "ADDED_TO_SPACE",
		Space: gchatSpace{Name: "spaces/new-space"},
	}

	body, _ := json.Marshal(event)
	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	// Should NOT trigger the message handler.
	if handlerCalled.Load() != 0 {
		t.Errorf("handler called %d times for ADDED_TO_SPACE, want 0", handlerCalled.Load())
	}
}

func TestGoogleChatEmptyMessage(t *testing.T) {
	credFile, _ := writeTestCredentials(t)

	var handlerCalled atomic.Int32

	ch := NewGoogleChatChannel(credFile, newGChatTestLogger(), WithGoogleChatWebhookAddr(":0"))
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	event := gchatEvent{
		Type: "MESSAGE",
		Message: gchatMessage{
			Name: "spaces/abc/messages/empty",
			Text: "   ", // whitespace only
		},
		User:  gchatUser{Name: "users/u1"},
		Space: gchatSpace{Name: "spaces/abc", Type: "DM"},
	}

	body, _ := json.Marshal(event)
	url := fmt.Sprintf("http://%s/webhook", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	if handlerCalled.Load() != 0 {
		t.Errorf("handler called %d times for empty message, want 0", handlerCalled.Load())
	}
}

// roundTripFunc is declared in telegram_test.go (same package).
