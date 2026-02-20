package channel

import (
	"context"
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

func newTeamsTestLogger() *slog.Logger { return slog.Default() }

func TestTeamsChannelName(t *testing.T) {
	ch := NewTeamsChannel("app-id", "app-secret", newTeamsTestLogger())
	if ch.Name() != "teams" {
		t.Errorf("Name = %q, want teams", ch.Name())
	}
}

func TestTeamsOptions(t *testing.T) {
	ch := NewTeamsChannel("app-id", "secret", newTeamsTestLogger(),
		WithTeamsWebhookAddr(":5555"),
		WithTeamsMentionOnly(true),
		WithTeamsTenantID("tenant-123"),
	)
	if ch.webhookAddr != ":5555" {
		t.Errorf("webhookAddr = %q, want :5555", ch.webhookAddr)
	}
	if !ch.mentionOnly {
		t.Error("mentionOnly should be true")
	}
	if ch.tenantID != "tenant-123" {
		t.Errorf("tenantID = %q, want tenant-123", ch.tenantID)
	}
}

func TestTeamsDefaultWebhookAddr(t *testing.T) {
	ch := NewTeamsChannel("app-id", "secret", newTeamsTestLogger())
	if ch.webhookAddr != ":3978" {
		t.Errorf("default webhookAddr = %q, want :3978", ch.webhookAddr)
	}
}

func TestTeamsStartStop(t *testing.T) {
	ch := NewTeamsChannel("app-id", "secret", newTeamsTestLogger(), WithTeamsWebhookAddr(":0"))
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

func TestTeamsStopBeforeStart(t *testing.T) {
	ch := NewTeamsChannel("app-id", "secret", newTeamsTestLogger())
	if err := ch.Stop(context.Background()); err != nil {
		t.Errorf("Stop before Start: %v", err)
	}
}

func TestTeamsWebhookMethodNotAllowed(t *testing.T) {
	ch := NewTeamsChannel("app-id", "secret", newTeamsTestLogger(), WithTeamsWebhookAddr(":0"))
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error { return nil }); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	url := fmt.Sprintf("http://%s/api/messages", ch.BoundAddr())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestTeamsWebhookBadJSON(t *testing.T) {
	ch := NewTeamsChannel("app-id", "secret", newTeamsTestLogger(), WithTeamsWebhookAddr(":0"))
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error { return nil }); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	url := fmt.Sprintf("http://%s/api/messages", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader("not json"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestTeamsReceiveMessage(t *testing.T) {
	var received domain.InboundMessage
	var handlerCalled atomic.Int32

	ch := NewTeamsChannel("app-id", "secret", newTeamsTestLogger(), WithTeamsWebhookAddr(":0"))
	if err := ch.Start(context.Background(), func(_ context.Context, msg domain.InboundMessage) error {
		received = msg
		handlerCalled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	activity := teamsActivity{
		Type:       "message",
		ID:         "activity-1",
		ServiceURL: "https://smba.trafficmanager.net/teams/",
		Text:       "Hello Alfred",
		From: teamsAccount{
			ID:   "user-1",
			Name: "Alice",
		},
		Conversation: teamsConversation{
			ID:       "conv-123",
			TenantID: "tenant-abc",
		},
	}

	body, _ := json.Marshal(activity)
	url := fmt.Sprintf("http://%s/api/messages", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	if handlerCalled.Load() != 1 {
		t.Fatalf("handler called %d times, want 1", handlerCalled.Load())
	}
	if received.SessionID != "conv-123" {
		t.Errorf("SessionID = %q, want conv-123", received.SessionID)
	}
	if received.Content != "Hello Alfred" {
		t.Errorf("Content = %q, want Hello Alfred", received.Content)
	}
	if received.ChannelName != "teams" {
		t.Errorf("ChannelName = %q", received.ChannelName)
	}
	if received.SenderID != "user-1" {
		t.Errorf("SenderID = %q", received.SenderID)
	}
	if received.SenderName != "Alice" {
		t.Errorf("SenderName = %q", received.SenderName)
	}
	if received.Metadata["service_url"] != "https://smba.trafficmanager.net/teams/" {
		t.Errorf("service_url = %q", received.Metadata["service_url"])
	}
}

func TestTeamsReceiveMessageWithHTML(t *testing.T) {
	var received domain.InboundMessage

	ch := NewTeamsChannel("bot-id", "secret", newTeamsTestLogger(), WithTeamsWebhookAddr(":0"))
	if err := ch.Start(context.Background(), func(_ context.Context, msg domain.InboundMessage) error {
		received = msg
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	// Teams sends HTML with <at> tags for mentions.
	activity := teamsActivity{
		Type:       "message",
		ServiceURL: "https://smba.trafficmanager.net/teams/",
		Text:       "<at>Alfred</at> what time is it",
		From:       teamsAccount{ID: "user-2", Name: "Bob"},
		Conversation: teamsConversation{
			ID: "conv-456",
		},
		Entities: []teamsEntity{{
			Type:      "mention",
			Mentioned: teamsAccount{ID: "bot-id", Name: "Alfred"},
			Text:      "<at>Alfred</at>",
		}},
	}

	body, _ := json.Marshal(activity)
	url := fmt.Sprintf("http://%s/api/messages", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	// HTML tags should be stripped.
	if received.Content != "Alfred what time is it" {
		t.Errorf("Content = %q, want 'Alfred what time is it'", received.Content)
	}
	if !received.IsMention {
		t.Error("IsMention should be true")
	}
}

func TestTeamsMentionOnlyFilter(t *testing.T) {
	var handlerCalled atomic.Int32

	ch := NewTeamsChannel("bot-id", "secret", newTeamsTestLogger(),
		WithTeamsWebhookAddr(":0"),
		WithTeamsMentionOnly(true),
	)
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	// Non-mention in group chat — should be filtered.
	activity := teamsActivity{
		Type:       "message",
		ServiceURL: "https://smba.trafficmanager.net/teams/",
		Text:       "hello everyone",
		From:       teamsAccount{ID: "user-3"},
		Conversation: teamsConversation{
			ID:      "conv-group",
			IsGroup: true,
		},
	}

	body, _ := json.Marshal(activity)
	url := fmt.Sprintf("http://%s/api/messages", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	if handlerCalled.Load() != 0 {
		t.Errorf("handler called %d times, want 0 for non-mention in mentionOnly", handlerCalled.Load())
	}
}

func TestTeamsMentionOnlyAllowsPersonalChat(t *testing.T) {
	var handlerCalled atomic.Int32

	ch := NewTeamsChannel("bot-id", "secret", newTeamsTestLogger(),
		WithTeamsWebhookAddr(":0"),
		WithTeamsMentionOnly(true),
	)
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	// Personal chat (IsGroup=false) should pass through.
	activity := teamsActivity{
		Type:       "message",
		ServiceURL: "https://smba.trafficmanager.net/teams/",
		Text:       "hello",
		From:       teamsAccount{ID: "user-4"},
		Conversation: teamsConversation{
			ID:      "conv-personal",
			IsGroup: false,
		},
	}

	body, _ := json.Marshal(activity)
	url := fmt.Sprintf("http://%s/api/messages", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	if handlerCalled.Load() != 1 {
		t.Errorf("handler called %d times, want 1 for personal chat in mentionOnly", handlerCalled.Load())
	}
}

func TestTeamsTenantFilter(t *testing.T) {
	var handlerCalled atomic.Int32

	ch := NewTeamsChannel("app-id", "secret", newTeamsTestLogger(),
		WithTeamsWebhookAddr(":0"),
		WithTeamsTenantID("allowed-tenant"),
	)
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	// Message from a different tenant — should be filtered.
	activity := teamsActivity{
		Type:       "message",
		ServiceURL: "https://smba.trafficmanager.net/teams/",
		Text:       "hello",
		From:       teamsAccount{ID: "user-5"},
		Conversation: teamsConversation{
			ID:       "conv-other-tenant",
			TenantID: "wrong-tenant",
		},
	}

	body, _ := json.Marshal(activity)
	url := fmt.Sprintf("http://%s/api/messages", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	if handlerCalled.Load() != 0 {
		t.Errorf("handler called %d times, want 0 for wrong tenant", handlerCalled.Load())
	}
}

func TestTeamsCommandHelp(t *testing.T) {
	var sentText string

	chatAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var act teamsSendActivity
		json.Unmarshal(body, &act)
		sentText = act.Text
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"resp-1"}`))
	}))
	defer chatAPI.Close()

	var handlerCalled bool
	ch := NewTeamsChannel("app-id", "secret", newTeamsTestLogger(), WithTeamsWebhookAddr(":0"))
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error {
		handlerCalled = true
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	// Inject cached token.
	ch.mu.Lock()
	ch.accessToken = "mock-token"
	ch.tokenExpiry = time.Now().Add(1 * time.Hour)
	ch.mu.Unlock()

	activity := teamsActivity{
		Type:       "message",
		ServiceURL: chatAPI.URL,
		Text:       "/help",
		From:       teamsAccount{ID: "user-6"},
		Conversation: teamsConversation{
			ID: "conv-cmd",
		},
	}

	body, _ := json.Marshal(activity)
	url := fmt.Sprintf("http://%s/api/messages", ch.BoundAddr())
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

func TestTeamsCommandPrivacy(t *testing.T) {
	var sentText string

	chatAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var act teamsSendActivity
		json.Unmarshal(body, &act)
		sentText = act.Text
		w.WriteHeader(http.StatusOK)
	}))
	defer chatAPI.Close()

	ch := NewTeamsChannel("app-id", "secret", newTeamsTestLogger(), WithTeamsWebhookAddr(":0"))
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error { return nil }); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	ch.mu.Lock()
	ch.accessToken = "mock-token"
	ch.tokenExpiry = time.Now().Add(1 * time.Hour)
	ch.mu.Unlock()

	activity := teamsActivity{
		Type:       "message",
		ServiceURL: chatAPI.URL,
		Text:       "/privacy",
		From:       teamsAccount{ID: "user-7"},
		Conversation: teamsConversation{
			ID: "conv-priv",
		},
	}

	body, _ := json.Marshal(activity)
	url := fmt.Sprintf("http://%s/api/messages", ch.BoundAddr())
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

func TestTeamsSendMessage(t *testing.T) {
	var sentPayload teamsSendActivity
	var authHeader string
	var requestPath string

	chatAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		requestPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &sentPayload)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"resp-2"}`))
	}))
	defer chatAPI.Close()

	ch := NewTeamsChannel("app-id", "secret", newTeamsTestLogger())
	ch.mu.Lock()
	ch.accessToken = "test-access-token"
	ch.tokenExpiry = time.Now().Add(1 * time.Hour)
	ch.mu.Unlock()

	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "conv-123",
		Content:   "Hello from Alfred",
		ThreadID:  "reply-to-1",
		Metadata: map[string]string{
			"service_url": chatAPI.URL,
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if sentPayload.Text != "Hello from Alfred" {
		t.Errorf("Text = %q, want 'Hello from Alfred'", sentPayload.Text)
	}
	if sentPayload.Type != "message" {
		t.Errorf("Type = %q, want message", sentPayload.Type)
	}
	if sentPayload.ReplyToID != "reply-to-1" {
		t.Errorf("ReplyToID = %q, want reply-to-1", sentPayload.ReplyToID)
	}
	if authHeader != "Bearer test-access-token" {
		t.Errorf("Authorization = %q", authHeader)
	}
	if requestPath != "/v3/conversations/conv-123/activities" {
		t.Errorf("path = %q", requestPath)
	}
}

func TestTeamsSendErrorMessage(t *testing.T) {
	var sentPayload teamsSendActivity

	chatAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &sentPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer chatAPI.Close()

	ch := NewTeamsChannel("app-id", "secret", newTeamsTestLogger())
	ch.mu.Lock()
	ch.accessToken = "token"
	ch.tokenExpiry = time.Now().Add(1 * time.Hour)
	ch.mu.Unlock()

	ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "conv-123",
		Content:   "something went wrong",
		IsError:   true,
		Metadata: map[string]string{
			"service_url": chatAPI.URL,
		},
	})

	if sentPayload.Text != "Error: something went wrong" {
		t.Errorf("Text = %q, want 'Error: something went wrong'", sentPayload.Text)
	}
}

func TestTeamsSendMissingServiceURL(t *testing.T) {
	ch := NewTeamsChannel("app-id", "secret", newTeamsTestLogger())
	ch.mu.Lock()
	ch.accessToken = "token"
	ch.tokenExpiry = time.Now().Add(1 * time.Hour)
	ch.mu.Unlock()

	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "conv-123",
		Content:   "test",
	})
	if err == nil {
		t.Error("expected error for missing service_url")
	}
	if !strings.Contains(err.Error(), "service_url") {
		t.Errorf("error = %v, want service_url-related error", err)
	}
}

func TestTeamsSendMissingSessionID(t *testing.T) {
	ch := NewTeamsChannel("app-id", "secret", newTeamsTestLogger())
	ch.mu.Lock()
	ch.accessToken = "token"
	ch.tokenExpiry = time.Now().Add(1 * time.Hour)
	ch.mu.Unlock()

	err := ch.Send(context.Background(), domain.OutboundMessage{
		Content: "test",
		Metadata: map[string]string{
			"service_url": "https://example.com",
		},
	})
	if err == nil {
		t.Error("expected error for missing session_id")
	}
	if !strings.Contains(err.Error(), "session_id") {
		t.Errorf("error = %v, want session_id-related error", err)
	}
}

func TestTeamsSendAPIError(t *testing.T) {
	chatAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("permission denied"))
	}))
	defer chatAPI.Close()

	ch := NewTeamsChannel("app-id", "secret", newTeamsTestLogger())
	ch.mu.Lock()
	ch.accessToken = "token"
	ch.tokenExpiry = time.Now().Add(1 * time.Hour)
	ch.mu.Unlock()

	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "conv-123",
		Content:   "test",
		Metadata: map[string]string{
			"service_url": chatAPI.URL,
		},
	})
	if err == nil {
		t.Error("expected error for API error")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %v, want 403 in error", err)
	}
}

func TestTeamsTokenExchange(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("token request method = %s, want POST", r.Method)
		}
		ct := r.Header.Get("Content-Type")
		if ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", ct)
		}
		body, _ := io.ReadAll(r.Body)
		form := string(body)
		if !strings.Contains(form, "client_id=test-app-id") {
			t.Errorf("missing client_id in form: %s", form)
		}
		if !strings.Contains(form, "client_secret=test-secret") {
			t.Errorf("missing client_secret in form: %s", form)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "exchanged-token",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	ch := NewTeamsChannel("test-app-id", "test-secret", newTeamsTestLogger())
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

func TestTeamsTokenExchangeFailure(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("invalid credentials"))
	}))
	defer tokenServer.Close()

	ch := NewTeamsChannel("bad-id", "bad-secret", newTeamsTestLogger())
	ch.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = strings.TrimPrefix(tokenServer.URL, "http://")
			return http.DefaultTransport.RoundTrip(req)
		}),
		Timeout: 5 * time.Second,
	}

	_, err := ch.getAccessToken(context.Background())
	if err == nil {
		t.Error("expected error for token exchange failure")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %v, want 401", err)
	}
}

func TestTeamsConversationUpdateIgnored(t *testing.T) {
	var handlerCalled atomic.Int32

	ch := NewTeamsChannel("app-id", "secret", newTeamsTestLogger(), WithTeamsWebhookAddr(":0"))
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	activity := teamsActivity{
		Type:       "conversationUpdate",
		ServiceURL: "https://smba.trafficmanager.net/teams/",
		Conversation: teamsConversation{
			ID: "conv-update",
		},
	}

	body, _ := json.Marshal(activity)
	url := fmt.Sprintf("http://%s/api/messages", ch.BoundAddr())
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	if handlerCalled.Load() != 0 {
		t.Errorf("handler called %d times for conversationUpdate, want 0", handlerCalled.Load())
	}
}

func TestTeamsEmptyMessage(t *testing.T) {
	var handlerCalled atomic.Int32

	ch := NewTeamsChannel("app-id", "secret", newTeamsTestLogger(), WithTeamsWebhookAddr(":0"))
	if err := ch.Start(context.Background(), func(_ context.Context, _ domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	activity := teamsActivity{
		Type:       "message",
		ServiceURL: "https://smba.trafficmanager.net/teams/",
		Text:       "   ",
		From:       teamsAccount{ID: "user-8"},
		Conversation: teamsConversation{
			ID: "conv-empty",
		},
	}

	body, _ := json.Marshal(activity)
	url := fmt.Sprintf("http://%s/api/messages", ch.BoundAddr())
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

func TestStripTeamsHTML(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"<at>Bot</at> hello", "Bot hello"},
		{"<p>paragraph</p>", "paragraph"},
		{"<b>bold</b> and <i>italic</i>", "bold and italic"},
		{"no tags here", "no tags here"},
		{"", ""},
		{"<at>Alfred</at> what's the <b>weather</b>", "Alfred what's the weather"},
	}

	for _, tt := range tests {
		got := stripTeamsHTML(tt.input)
		if got != tt.want {
			t.Errorf("stripTeamsHTML(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
