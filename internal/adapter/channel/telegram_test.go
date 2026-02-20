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

// roundTripFunc adapts a function to the http.RoundTripper interface.
type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// errorBody is an io.ReadCloser whose Read always returns an error.
type errorBody struct{}

func (e *errorBody) Read(p []byte) (int, error) { return 0, fmt.Errorf("read error") }
func (e *errorBody) Close() error               { return nil }

// Ensure errorBody satisfies io.ReadCloser.
var _ io.ReadCloser = (*errorBody)(nil)

func newTelegramTestLogger() *slog.Logger { return slog.Default() }

func TestTelegramUpdateParsing(t *testing.T) {
	var handlerCalled atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bottest-token/getUpdates" {
			resp := telegramUpdateResponse{
				OK: true,
				Result: []telegramUpdate{
					{
						UpdateID: 1,
						Message: &telegramMessage{
							MessageID: 100,
							Chat:      telegramChat{ID: 42},
							Text:      "Hello bot",
						},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		if r.URL.Path == "/bottest-token/sendMessage" {
			json.NewEncoder(w).Encode(telegramSendResponse{OK: true})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = server.URL

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	handler := func(ctx context.Context, msg domain.InboundMessage) error {
		handlerCalled.Add(1)
		if msg.SessionID != "42" {
			t.Errorf("SessionID = %q, want 42", msg.SessionID)
		}
		if msg.Content != "Hello bot" {
			t.Errorf("Content = %q", msg.Content)
		}
		if msg.ChannelName != "telegram" {
			t.Errorf("ChannelName = %q", msg.ChannelName)
		}
		return nil
	}

	ch.Start(ctx, handler)
	time.Sleep(300 * time.Millisecond)
	ch.Stop(ctx)

	if handlerCalled.Load() < 1 {
		t.Error("handler was never called")
	}
}

func TestTelegramSendMessage(t *testing.T) {
	var receivedText string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bottest-token/sendMessage" {
			var req telegramSendRequest
			json.NewDecoder(r.Body).Decode(&req)
			receivedText = req.Text
			json.NewEncoder(w).Encode(telegramSendResponse{OK: true})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = server.URL

	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "42",
		Content:   "Hello user",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if receivedText != "Hello user" {
		t.Errorf("sent text = %q", receivedText)
	}
}

func TestTelegramSendError(t *testing.T) {
	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = "http://localhost:1" // unreachable

	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "42",
		Content:   "test",
	})
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

func TestTelegramChannelName(t *testing.T) {
	ch := NewTelegramChannel("token", newTelegramTestLogger())
	if ch.Name() != "telegram" {
		t.Errorf("Name = %q", ch.Name())
	}
}

func TestTelegramLongPollError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/botbad-token/getUpdates" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"ok":false,"description":"Unauthorized"}`))
			return
		}
	}))
	defer server.Close()

	ch := NewTelegramChannel("bad-token", newTelegramTestLogger())
	ch.baseURL = server.URL

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Should not panic on error
	ch.Start(ctx, func(ctx context.Context, msg domain.InboundMessage) error { return nil })
	time.Sleep(150 * time.Millisecond)
	ch.Stop(ctx)
}

func TestTelegramSendErrorMessage(t *testing.T) {
	var receivedText string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req telegramSendRequest
		json.NewDecoder(r.Body).Decode(&req)
		receivedText = req.Text
		json.NewEncoder(w).Encode(telegramSendResponse{OK: true})
	}))
	defer server.Close()

	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = server.URL

	ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "42",
		Content:   "something went wrong",
		IsError:   true,
	})

	if receivedText != "Error: something went wrong" {
		t.Errorf("sent text = %q", receivedText)
	}
}

func TestTelegramChannelStopBeforeStart(t *testing.T) {
	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	if err := ch.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestTelegramChannelSendAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer server.Close()

	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = server.URL

	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "12345",
		Content:   "test",
	})
	if err == nil {
		t.Error("expected error for API error")
	}
}

func TestTelegramChannelGetUpdates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "getUpdates") {
			resp := telegramUpdateResponse{
				OK: true,
				Result: []telegramUpdate{
					{
						UpdateID: 1,
						Message: &telegramMessage{
							MessageID: 100,
							Chat:      telegramChat{ID: 42},
							Text:      "hello",
						},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = server.URL

	updates, err := ch.getUpdates(context.Background())
	if err != nil {
		t.Fatalf("getUpdates: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(updates))
	}
	if updates[0].Message.Text != "hello" {
		t.Errorf("Text = %q", updates[0].Message.Text)
	}
}

func TestTelegramChannelGetUpdatesNotOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(telegramUpdateResponse{OK: false})
	}))
	defer server.Close()

	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = server.URL

	_, err := ch.getUpdates(context.Background())
	if err == nil {
		t.Error("expected error for ok=false")
	}
}

func TestTelegramChannelGetUpdatesHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("bad gateway"))
	}))
	defer server.Close()

	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = server.URL

	_, err := ch.getUpdates(context.Background())
	if err == nil {
		t.Error("expected error for HTTP error")
	}
}

func TestTelegramChannelGetUpdatesInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = server.URL

	_, err := ch.getUpdates(context.Background())
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestTelegramChannelPollLoop(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "getUpdates") {
			callCount++
			if callCount == 1 {
				resp := telegramUpdateResponse{
					OK: true,
					Result: []telegramUpdate{
						{
							UpdateID: 1,
							Message: &telegramMessage{
								MessageID: 100,
								Chat:      telegramChat{ID: 42},
								Text:      "hello",
							},
						},
						{
							UpdateID: 2,
							Message:  nil, // nil message - should be skipped
						},
						{
							UpdateID: 3,
							Message: &telegramMessage{
								MessageID: 101,
								Chat:      telegramChat{ID: 42},
								Text:      "", // empty text - should be skipped
							},
						},
					},
				}
				json.NewEncoder(w).Encode(resp)
			} else {
				json.NewEncoder(w).Encode(telegramUpdateResponse{OK: true})
			}
		} else if strings.Contains(r.URL.Path, "sendMessage") {
			json.NewEncoder(w).Encode(telegramSendResponse{OK: true})
		}
	}))
	defer server.Close()

	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = server.URL

	var receivedMsg string
	handler := func(ctx context.Context, msg domain.InboundMessage) error {
		receivedMsg = msg.Content
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch.Start(ctx, handler)

	// Wait for poll loop to process
	time.Sleep(200 * time.Millisecond)
	cancel()
	ch.Stop(context.Background())

	if receivedMsg != "hello" {
		t.Errorf("received = %q, want %q", receivedMsg, "hello")
	}
}

func TestTelegramChannelPollLoopHandlerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "getUpdates") {
			resp := telegramUpdateResponse{
				OK: true,
				Result: []telegramUpdate{
					{UpdateID: 1, Message: &telegramMessage{MessageID: 1, Chat: telegramChat{ID: 1}, Text: "test"}},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = server.URL

	handler := func(ctx context.Context, msg domain.InboundMessage) error {
		return fmt.Errorf("handler error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch.Start(ctx, handler)

	time.Sleep(200 * time.Millisecond)
	cancel()
	ch.Stop(context.Background())
}

func TestTelegramPollLoopStopViaDone(t *testing.T) {
	// Test the <-t.done branch in pollLoop without cancelling context.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "getUpdates") {
			json.NewEncoder(w).Encode(telegramUpdateResponse{OK: true})
		}
	}))
	defer server.Close()

	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = server.URL

	handler := func(ctx context.Context, msg domain.InboundMessage) error {
		return nil
	}

	// Use non-cancellable context
	ch.Start(context.Background(), handler)

	time.Sleep(100 * time.Millisecond)

	// Stop via done channel (not context cancellation)
	ch.Stop(context.Background())
}

func TestTelegramGetUpdatesConnectionError(t *testing.T) {
	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = "http://localhost:1" // unreachable port

	_, err := ch.getUpdates(context.Background())
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

func TestTelegramSendMessageStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	}))
	defer server.Close()

	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = server.URL

	err := ch.sendMessage(context.Background(), "42", "test message", "", "")
	if err == nil {
		t.Error("expected error for non-200 status")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 in error, got %v", err)
	}
}

func TestTelegramGetUpdatesCreateRequestError(t *testing.T) {
	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = "http://\x7f" // control char makes URL parsing fail
	_, err := ch.getUpdates(context.Background())
	if err == nil {
		t.Error("expected error for invalid URL")
	}
	if !strings.Contains(err.Error(), "create request") {
		t.Errorf("expected 'create request' in error, got: %v", err)
	}
}

func TestTelegramSendMessageCreateRequestError(t *testing.T) {
	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = "http://\x7f" // control char makes URL parsing fail
	err := ch.sendMessage(context.Background(), "42", "test", "", "")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
	if !strings.Contains(err.Error(), "create request") {
		t.Errorf("expected 'create request' in error, got: %v", err)
	}
}

func TestTelegramGetUpdatesReadBodyError(t *testing.T) {
	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       &errorBody{},
			}, nil
		}),
	}
	_, err := ch.getUpdates(context.Background())
	if err == nil {
		t.Error("expected error for body read error")
	}
	if !strings.Contains(err.Error(), "read response") {
		t.Errorf("expected 'read response' in error, got: %v", err)
	}
}

func TestTelegramSendMessageReadBodyError(t *testing.T) {
	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       &errorBody{},
			}, nil
		}),
	}
	err := ch.sendMessage(context.Background(), "42", "test", "", "")
	if err == nil {
		t.Error("expected error for body read error")
	}
	if !strings.Contains(err.Error(), "read response") {
		t.Errorf("expected 'read response' in error, got: %v", err)
	}
}

// --- Phase 2: Enrichment tests ---

func TestTelegramEnrichedFields(t *testing.T) {
	var received domain.InboundMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "getMe") {
			json.NewEncoder(w).Encode(telegramGetMeResponse{
				OK: true,
				Result: struct {
					Username string `json:"username"`
				}{Username: "mybot"},
			})
			return
		}
		if strings.Contains(r.URL.Path, "getUpdates") {
			resp := telegramUpdateResponse{
				OK: true,
				Result: []telegramUpdate{{
					UpdateID: 1,
					Message: &telegramMessage{
						MessageID: 100,
						From:      &telegramUser{ID: 99, FirstName: "Alice", LastName: "Smith", Username: "alice"},
						Chat:      telegramChat{ID: 42, Type: "group"},
						Text:      "hello @mybot",
						Entities: []telegramEntity{
							{Type: "mention", Offset: 6, Length: 6},
						},
						MessageThreadID: 55,
						ReplyToMessage:  &telegramReplyInfo{MessageID: 90},
					},
				}},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
	}))
	defer server.Close()

	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = server.URL

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error {
		received = msg
		return nil
	})
	time.Sleep(300 * time.Millisecond)
	ch.Stop(ctx)

	if received.SenderID != "99" {
		t.Errorf("SenderID = %q, want 99", received.SenderID)
	}
	if received.SenderName != "Alice Smith" {
		t.Errorf("SenderName = %q", received.SenderName)
	}
	if received.GroupID != "42" {
		t.Errorf("GroupID = %q, want 42", received.GroupID)
	}
	if received.ThreadID != "55" {
		t.Errorf("ThreadID = %q, want 55", received.ThreadID)
	}
	if received.ReplyToID != "90" {
		t.Errorf("ReplyToID = %q, want 90", received.ReplyToID)
	}
	if !received.IsMention {
		t.Error("IsMention should be true")
	}
}

func TestTelegramMentionDetection(t *testing.T) {
	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.botUsername = "mybot"

	msg := &telegramMessage{
		Text: "hello @mybot how are you",
		Entities: []telegramEntity{
			{Type: "mention", Offset: 6, Length: 6},
		},
	}
	if !ch.hasBotMention(msg) {
		t.Error("expected mention detected")
	}

	// No mention entity.
	msg2 := &telegramMessage{
		Text: "hello world",
	}
	if ch.hasBotMention(msg2) {
		t.Error("expected no mention")
	}

	// Empty bot username.
	ch.botUsername = ""
	if ch.hasBotMention(msg) {
		t.Error("expected no mention with empty bot username")
	}
}

func TestTelegramMentionOnlyFiltering(t *testing.T) {
	var handlerCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "getMe") {
			json.NewEncoder(w).Encode(telegramGetMeResponse{
				OK: true,
				Result: struct {
					Username string `json:"username"`
				}{Username: "mybot"},
			})
			return
		}
		if strings.Contains(r.URL.Path, "getUpdates") {
			resp := telegramUpdateResponse{
				OK: true,
				Result: []telegramUpdate{{
					UpdateID: 1,
					Message: &telegramMessage{
						MessageID: 100,
						Chat:      telegramChat{ID: 42, Type: "group"},
						Text:      "hello everyone", // no mention
					},
				}},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
	}))
	defer server.Close()

	ch := NewTelegramChannel("test-token", newTelegramTestLogger(), WithTelegramMentionOnly(true))
	ch.baseURL = server.URL

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error {
		handlerCalled = true
		return nil
	})
	time.Sleep(300 * time.Millisecond)
	ch.Stop(ctx)

	if handlerCalled {
		t.Error("handler should NOT be called for non-mentioned group message in mentionOnly mode")
	}
}

func TestTelegramMediaExtraction(t *testing.T) {
	msg := &telegramMessage{
		Caption: "a photo",
		Photo: []telegramPhotoSize{
			{FileID: "small", Width: 100, Height: 100},
			{FileID: "large", Width: 800, Height: 600},
		},
		Document: &telegramDocument{
			FileID:   "doc1",
			FileName: "test.pdf",
			MIMEType: "application/pdf",
		},
	}

	media := extractMedia(msg)
	if len(media) != 2 {
		t.Fatalf("expected 2 media, got %d", len(media))
	}

	// Photo should be the largest.
	if media[0].Type != domain.MediaTypeImage {
		t.Errorf("media[0].Type = %q", media[0].Type)
	}
	if media[0].URL != "large" {
		t.Errorf("media[0].URL = %q, want large", media[0].URL)
	}

	// Document.
	if media[1].Type != domain.MediaTypeFile {
		t.Errorf("media[1].Type = %q", media[1].Type)
	}
	if media[1].MIMEType != "application/pdf" {
		t.Errorf("media[1].MIMEType = %q", media[1].MIMEType)
	}
}

func TestTelegramCaptionFallback(t *testing.T) {
	var received domain.InboundMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "getMe") {
			json.NewEncoder(w).Encode(telegramGetMeResponse{OK: true, Result: struct {
				Username string `json:"username"`
			}{}})
			return
		}
		if strings.Contains(r.URL.Path, "getUpdates") {
			resp := telegramUpdateResponse{
				OK: true,
				Result: []telegramUpdate{{
					UpdateID: 1,
					Message: &telegramMessage{
						MessageID: 100,
						Chat:      telegramChat{ID: 42, Type: "private"},
						Text:      "",              // no text
						Caption:   "photo caption", // caption fallback
						Photo: []telegramPhotoSize{
							{FileID: "photo1"},
						},
					},
				}},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
	}))
	defer server.Close()

	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = server.URL

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error {
		received = msg
		return nil
	})
	time.Sleep(300 * time.Millisecond)
	ch.Stop(ctx)

	if received.Content != "photo caption" {
		t.Errorf("Content = %q, want 'photo caption'", received.Content)
	}
}

func TestTelegramThreadReply(t *testing.T) {
	var sentPayload telegramSendRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "sendMessage") {
			json.NewDecoder(r.Body).Decode(&sentPayload)
			json.NewEncoder(w).Encode(telegramSendResponse{OK: true})
			return
		}
	}))
	defer server.Close()

	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = server.URL

	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "42",
		Content:   "reply",
		ThreadID:  "55",
		ReplyToID: "90",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if sentPayload.MessageThreadID != 55 {
		t.Errorf("MessageThreadID = %d, want 55", sentPayload.MessageThreadID)
	}
	if sentPayload.ReplyToMsgID != 90 {
		t.Errorf("ReplyToMsgID = %d, want 90", sentPayload.ReplyToMsgID)
	}
}

func TestTelegramGetMe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "getMe") {
			json.NewEncoder(w).Encode(telegramGetMeResponse{
				OK: true,
				Result: struct {
					Username string `json:"username"`
				}{Username: "testbot"},
			})
			return
		}
	}))
	defer server.Close()

	ch := NewTelegramChannel("test-token", newTelegramTestLogger())
	ch.baseURL = server.URL

	username, err := ch.getMe(context.Background())
	if err != nil {
		t.Fatalf("getMe: %v", err)
	}
	if username != "testbot" {
		t.Errorf("username = %q, want testbot", username)
	}
}
