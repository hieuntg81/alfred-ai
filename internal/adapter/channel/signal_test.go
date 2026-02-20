package channel

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

func newSignalTestLogger() *slog.Logger { return slog.Default() }

func TestSignalChannelName(t *testing.T) {
	ch := NewSignalChannel("http://localhost:8080", "+1234567890", newSignalTestLogger())
	if ch.Name() != "signal" {
		t.Errorf("Name = %q, want signal", ch.Name())
	}
}

func TestSignalConstructorDefaults(t *testing.T) {
	ch := NewSignalChannel("http://localhost:8080/", "+1234567890", newSignalTestLogger())
	if ch.apiURL != "http://localhost:8080" {
		t.Errorf("apiURL = %q, want trailing slash trimmed", ch.apiURL)
	}
	if ch.phone != "+1234567890" {
		t.Errorf("phone = %q", ch.phone)
	}
	if ch.pollInterval != 2*time.Second {
		t.Errorf("pollInterval = %v, want 2s", ch.pollInterval)
	}
}

func TestSignalOptionPollInterval(t *testing.T) {
	ch := NewSignalChannel("http://localhost:8080", "+1", newSignalTestLogger(),
		WithSignalPollInterval(5*time.Second),
	)
	if ch.pollInterval != 5*time.Second {
		t.Errorf("pollInterval = %v, want 5s", ch.pollInterval)
	}
}

func TestSignalStopBeforeStart(t *testing.T) {
	ch := NewSignalChannel("http://localhost:8080", "+1", newSignalTestLogger())
	if err := ch.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestSignalStartStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	ch := NewSignalChannel(server.URL, "+1", newSignalTestLogger(),
		WithSignalPollInterval(50*time.Millisecond),
	)

	ctx := context.Background()
	if err := ch.Start(ctx, func(_ context.Context, _ domain.InboundMessage) error { return nil }); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := ch.Stop(ctx); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestSignalReceiveMessage(t *testing.T) {
	var received domain.InboundMessage
	var handlerCalled atomic.Int32

	requestCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v1/receive/") {
			mu.Lock()
			requestCount++
			count := requestCount
			mu.Unlock()

			if count == 1 {
				envelopes := []signalEnvelope{{
					Envelope: signalEnvelopeData{
						Source:     "+9876543210",
						SourceName: "Alice",
						Timestamp:  time.Now().UnixMilli(),
						DataMessage: &signalDataMessage{
							Message: "Hello from Signal",
						},
					},
				}}
				json.NewEncoder(w).Encode(envelopes)
				return
			}
			w.Write([]byte("[]"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	ch := NewSignalChannel(server.URL, "+1234567890", newSignalTestLogger(),
		WithSignalPollInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error {
		received = msg
		handlerCalled.Add(1)
		return nil
	})
	time.Sleep(300 * time.Millisecond)
	ch.Stop(ctx)

	if handlerCalled.Load() < 1 {
		t.Fatal("handler was never called")
	}
	if received.SessionID != "+9876543210" {
		t.Errorf("SessionID = %q", received.SessionID)
	}
	if received.SenderID != "+9876543210" {
		t.Errorf("SenderID = %q", received.SenderID)
	}
	if received.SenderName != "Alice" {
		t.Errorf("SenderName = %q", received.SenderName)
	}
	if received.Content != "Hello from Signal" {
		t.Errorf("Content = %q", received.Content)
	}
	if received.ChannelName != "signal" {
		t.Errorf("ChannelName = %q", received.ChannelName)
	}
}

func TestSignalReceiveGroupMessage(t *testing.T) {
	var received domain.InboundMessage
	var handlerCalled atomic.Int32

	requestCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v1/receive/") {
			mu.Lock()
			requestCount++
			count := requestCount
			mu.Unlock()

			if count == 1 {
				envelopes := []signalEnvelope{{
					Envelope: signalEnvelopeData{
						Source:     "+9876543210",
						SourceName: "Bob",
						DataMessage: &signalDataMessage{
							Message: "Group message",
							GroupInfo: &signalGroupInfo{
								GroupID: "group123",
								Type:    "DELIVER",
							},
						},
					},
				}}
				json.NewEncoder(w).Encode(envelopes)
				return
			}
			w.Write([]byte("[]"))
			return
		}
	}))
	defer server.Close()

	ch := NewSignalChannel(server.URL, "+1", newSignalTestLogger(),
		WithSignalPollInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error {
		received = msg
		handlerCalled.Add(1)
		return nil
	})
	time.Sleep(300 * time.Millisecond)
	ch.Stop(ctx)

	if handlerCalled.Load() < 1 {
		t.Fatal("handler was never called")
	}
	if received.GroupID != "group123" {
		t.Errorf("GroupID = %q, want group123", received.GroupID)
	}
}

func TestSignalIgnoreEmptyMessages(t *testing.T) {
	var handlerCalled atomic.Int32

	requestCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v1/receive/") {
			mu.Lock()
			requestCount++
			count := requestCount
			mu.Unlock()

			if count == 1 {
				envelopes := []signalEnvelope{
					{Envelope: signalEnvelopeData{Source: "+1", DataMessage: &signalDataMessage{Message: ""}}},
					{Envelope: signalEnvelopeData{Source: "+1", DataMessage: nil}},
					{Envelope: signalEnvelopeData{Source: "", DataMessage: &signalDataMessage{Message: "no sender"}}},
				}
				json.NewEncoder(w).Encode(envelopes)
				return
			}
			w.Write([]byte("[]"))
			return
		}
	}))
	defer server.Close()

	ch := NewSignalChannel(server.URL, "+1", newSignalTestLogger(),
		WithSignalPollInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	})
	time.Sleep(300 * time.Millisecond)
	ch.Stop(ctx)

	if handlerCalled.Load() != 0 {
		t.Errorf("handler called %d times, want 0 for empty/invalid messages", handlerCalled.Load())
	}
}

func TestSignalReceiveNon200(t *testing.T) {
	var handlerCalled atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ch := NewSignalChannel(server.URL, "+1", newSignalTestLogger(),
		WithSignalPollInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	})
	time.Sleep(200 * time.Millisecond)
	ch.Stop(ctx)

	if handlerCalled.Load() != 0 {
		t.Errorf("handler called %d times, want 0 for non-200 response", handlerCalled.Load())
	}
}

func TestSignalReceiveInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	ch := NewSignalChannel(server.URL, "+1", newSignalTestLogger(),
		WithSignalPollInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error { return nil })
	time.Sleep(200 * time.Millisecond)
	ch.Stop(ctx)
	// Should not panic.
}

func TestSignalSendMessage(t *testing.T) {
	var sentPayload signalSendRequest
	var reqMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqMethod = r.Method
		json.NewDecoder(r.Body).Decode(&sentPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ch := NewSignalChannel(server.URL, "+1234567890", newSignalTestLogger())

	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "+9876543210",
		Content:   "Hello from bot",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if reqMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", reqMethod)
	}
	if sentPayload.Message != "Hello from bot" {
		t.Errorf("message = %q", sentPayload.Message)
	}
	if sentPayload.Number != "+1234567890" {
		t.Errorf("number = %q", sentPayload.Number)
	}
	if len(sentPayload.Recipients) != 1 || sentPayload.Recipients[0] != "+9876543210" {
		t.Errorf("recipients = %v", sentPayload.Recipients)
	}
}

func TestSignalSendErrorMessage(t *testing.T) {
	var sentPayload signalSendRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&sentPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ch := NewSignalChannel(server.URL, "+1", newSignalTestLogger())
	ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "+2",
		Content:   "something failed",
		IsError:   true,
	})
	if sentPayload.Message != "Error: something failed" {
		t.Errorf("message = %q", sentPayload.Message)
	}
}

func TestSignalSendMissingRecipient(t *testing.T) {
	ch := NewSignalChannel("http://localhost:8080", "+1", newSignalTestLogger())
	err := ch.Send(context.Background(), domain.OutboundMessage{Content: "test"})
	if err == nil {
		t.Error("expected error for missing session_id")
	}
}

func TestSignalSendAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	}))
	defer server.Close()

	ch := NewSignalChannel(server.URL, "+1", newSignalTestLogger())
	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "+2",
		Content:   "test",
	})
	if err == nil {
		t.Error("expected error for API error")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 in error, got %v", err)
	}
}

func TestSignalCommandHelp(t *testing.T) {
	var sentPayload signalSendRequest
	var handlerCalled atomic.Int32

	requestCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v1/receive/") {
			mu.Lock()
			requestCount++
			count := requestCount
			mu.Unlock()

			if count == 1 {
				envelopes := []signalEnvelope{{
					Envelope: signalEnvelopeData{
						Source:      "+9876543210",
						SourceName:  "Alice",
						DataMessage: &signalDataMessage{Message: "/help"},
					},
				}}
				json.NewEncoder(w).Encode(envelopes)
				return
			}
			w.Write([]byte("[]"))
			return
		}
		if strings.Contains(r.URL.Path, "/v1/send") {
			json.NewDecoder(r.Body).Decode(&sentPayload)
			w.WriteHeader(http.StatusOK)
			return
		}
	}))
	defer server.Close()

	ch := NewSignalChannel(server.URL, "+1", newSignalTestLogger(),
		WithSignalPollInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	})
	time.Sleep(300 * time.Millisecond)
	ch.Stop(ctx)

	if handlerCalled.Load() != 0 {
		t.Error("handler should not be called for /help command")
	}
	if sentPayload.Message == "" {
		t.Error("help text should have been sent")
	}
}

func TestSignalCommandStart(t *testing.T) {
	var sentPayload signalSendRequest

	requestCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v1/receive/") {
			mu.Lock()
			requestCount++
			count := requestCount
			mu.Unlock()

			if count == 1 {
				envelopes := []signalEnvelope{{
					Envelope: signalEnvelopeData{
						Source:      "+9876543210",
						DataMessage: &signalDataMessage{Message: "/start"},
					},
				}}
				json.NewEncoder(w).Encode(envelopes)
				return
			}
			w.Write([]byte("[]"))
			return
		}
		if strings.Contains(r.URL.Path, "/v1/send") {
			json.NewDecoder(r.Body).Decode(&sentPayload)
			w.WriteHeader(http.StatusOK)
			return
		}
	}))
	defer server.Close()

	ch := NewSignalChannel(server.URL, "+1", newSignalTestLogger(),
		WithSignalPollInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error { return nil })
	time.Sleep(300 * time.Millisecond)
	ch.Stop(ctx)

	if sentPayload.Message == "" {
		t.Error("help text should have been sent for /start")
	}
}

func TestSignalCommandPrivacy(t *testing.T) {
	var sentPayload signalSendRequest

	requestCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v1/receive/") {
			mu.Lock()
			requestCount++
			count := requestCount
			mu.Unlock()

			if count == 1 {
				envelopes := []signalEnvelope{{
					Envelope: signalEnvelopeData{
						Source:      "+9876543210",
						DataMessage: &signalDataMessage{Message: "/privacy"},
					},
				}}
				json.NewEncoder(w).Encode(envelopes)
				return
			}
			w.Write([]byte("[]"))
			return
		}
		if strings.Contains(r.URL.Path, "/v1/send") {
			json.NewDecoder(r.Body).Decode(&sentPayload)
			w.WriteHeader(http.StatusOK)
			return
		}
	}))
	defer server.Close()

	ch := NewSignalChannel(server.URL, "+1", newSignalTestLogger(),
		WithSignalPollInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error { return nil })
	time.Sleep(300 * time.Millisecond)
	ch.Stop(ctx)

	if sentPayload.Message == "" || !strings.Contains(sentPayload.Message, "Privacy") {
		t.Errorf("expected privacy text, got %q", sentPayload.Message)
	}
}

func TestSignalSendUnreachable(t *testing.T) {
	ch := NewSignalChannel("http://localhost:1", "+1", newSignalTestLogger())
	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "+2",
		Content:   "test",
	})
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

func TestSignalReceivePhoneInURL(t *testing.T) {
	var receivedPath string
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedPath = r.URL.Path
		mu.Unlock()
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	ch := NewSignalChannel(server.URL, "+1234567890", newSignalTestLogger(),
		WithSignalPollInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error { return nil })
	time.Sleep(100 * time.Millisecond)
	ch.Stop(ctx)

	mu.Lock()
	path := receivedPath
	mu.Unlock()

	if !strings.Contains(path, "+1234567890") {
		t.Errorf("path = %q, expected phone number", path)
	}
	if !strings.HasPrefix(path, "/v1/receive/") {
		t.Errorf("path = %q, expected /v1/receive/ prefix", path)
	}
}
