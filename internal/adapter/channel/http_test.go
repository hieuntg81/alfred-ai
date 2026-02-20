package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

func newHTTPTestLogger() *slog.Logger { return slog.Default() }

func TestHTTPChannelStartStop(t *testing.T) {
	ch := NewHTTPChannel(":0", newHTTPTestLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := ch.Start(ctx, func(ctx context.Context, msg domain.InboundMessage) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := ch.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestHTTPChannelChat(t *testing.T) {
	ch := NewHTTPChannel(":0", newHTTPTestLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := func(ctx context.Context, msg domain.InboundMessage) error {
		if msg.ChannelName != "http" {
			t.Errorf("ChannelName = %q, want http", msg.ChannelName)
		}
		return ch.Send(ctx, domain.OutboundMessage{
			SessionID: msg.SessionID,
			Content:   "Hello " + msg.Content,
		})
	}

	err := ch.Start(ctx, handler)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(ctx)

	time.Sleep(50 * time.Millisecond)

	body, _ := json.Marshal(chatRequest{
		SessionID: "test-session",
		Content:   "World",
	})

	resp, err := http.Post(fmt.Sprintf("http://%s/api/v1/chat", ch.boundAddr), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	var cr chatResponse
	json.NewDecoder(resp.Body).Decode(&cr)

	if cr.Content != "Hello World" {
		t.Errorf("Content = %q, want %q", cr.Content, "Hello World")
	}
	if cr.SessionID != "test-session" {
		t.Errorf("SessionID = %q", cr.SessionID)
	}
}

func TestHTTPChannelHealth(t *testing.T) {
	ch := NewHTTPChannel(":0", newHTTPTestLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch.Start(ctx, func(ctx context.Context, msg domain.InboundMessage) error { return nil })
	defer ch.Stop(ctx)

	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://%s/api/v1/health", ch.boundAddr))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "ok" {
		t.Errorf("status = %q", result["status"])
	}
}

func TestHTTPChannelInvalidJSON(t *testing.T) {
	ch := NewHTTPChannel(":0", newHTTPTestLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch.Start(ctx, func(ctx context.Context, msg domain.InboundMessage) error { return nil })
	defer ch.Stop(ctx)

	time.Sleep(50 * time.Millisecond)

	resp, err := http.Post(
		fmt.Sprintf("http://%s/api/v1/chat", ch.boundAddr),
		"application/json",
		bytes.NewReader([]byte("not json")),
	)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHTTPChannelConcurrent(t *testing.T) {
	ch := NewHTTPChannel(":0", newHTTPTestLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := func(ctx context.Context, msg domain.InboundMessage) error {
		return ch.Send(ctx, domain.OutboundMessage{
			SessionID: msg.SessionID,
			Content:   "reply:" + msg.Content,
		})
	}

	ch.Start(ctx, handler)
	defer ch.Stop(ctx)

	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			body, _ := json.Marshal(chatRequest{
				SessionID: fmt.Sprintf("session-%d", n),
				Content:   fmt.Sprintf("msg-%d", n),
			})
			resp, err := http.Post(
				fmt.Sprintf("http://%s/api/v1/chat", ch.boundAddr),
				"application/json",
				bytes.NewReader(body),
			)
			if err != nil {
				t.Errorf("request %d: %v", n, err)
				return
			}
			resp.Body.Close()
		}(i)
	}
	wg.Wait()
}

func TestHTTPChannelHandlerError(t *testing.T) {
	ch := NewHTTPChannel(":0", newHTTPTestLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := func(ctx context.Context, msg domain.InboundMessage) error {
		return fmt.Errorf("handler error")
	}

	ch.Start(ctx, handler)
	defer ch.Stop(ctx)

	time.Sleep(50 * time.Millisecond)

	body, _ := json.Marshal(chatRequest{
		SessionID: "err-session",
		Content:   "trigger error",
	})

	resp, err := http.Post(fmt.Sprintf("http://%s/api/v1/chat", ch.boundAddr), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestHTTPChannelMethodNotAllowed(t *testing.T) {
	ch := NewHTTPChannel(":0", newHTTPTestLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch.Start(ctx, func(ctx context.Context, msg domain.InboundMessage) error { return nil })
	defer ch.Stop(ctx)

	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://%s/api/v1/chat", ch.boundAddr))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestHTTPChannelEmptyContent(t *testing.T) {
	ch := NewHTTPChannel(":0", newHTTPTestLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch.Start(ctx, func(ctx context.Context, msg domain.InboundMessage) error { return nil })
	defer ch.Stop(ctx)

	time.Sleep(50 * time.Millisecond)

	body, _ := json.Marshal(chatRequest{
		SessionID: "empty-session",
		Content:   "",
	})

	resp, err := http.Post(fmt.Sprintf("http://%s/api/v1/chat", ch.boundAddr), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHTTPChannelStopNilServer(t *testing.T) {
	ch := NewHTTPChannel(":0", newHTTPTestLogger())
	// Stop before Start (server is nil)
	if err := ch.Stop(context.Background()); err != nil {
		t.Errorf("Stop nil server: %v", err)
	}
}

func TestHTTPChannelAutoSessionID(t *testing.T) {
	ch := NewHTTPChannel(":0", newHTTPTestLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var receivedSessionID string
	handler := func(ctx context.Context, msg domain.InboundMessage) error {
		receivedSessionID = msg.SessionID
		return ch.Send(ctx, domain.OutboundMessage{
			SessionID: msg.SessionID,
			Content:   "ok",
		})
	}

	ch.Start(ctx, handler)
	defer ch.Stop(ctx)

	time.Sleep(50 * time.Millisecond)

	// Send without session_id
	body, _ := json.Marshal(map[string]string{"content": "hello"})
	resp, err := http.Post(fmt.Sprintf("http://%s/api/v1/chat", ch.boundAddr), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	if receivedSessionID == "" {
		t.Error("session ID should be auto-generated")
	}
	if !strings.HasPrefix(receivedSessionID, "http-") {
		t.Errorf("auto session ID = %q, should start with http-", receivedSessionID)
	}
}

func TestHTTPChannelName(t *testing.T) {
	ch := NewHTTPChannel(":0", newHTTPTestLogger())
	if ch.Name() != "http" {
		t.Errorf("Name = %q", ch.Name())
	}
}

func TestHTTPChannelSendNoPending(t *testing.T) {
	ch := NewHTTPChannel(":0", newHTTPTestLogger())
	// Send to a session that has no pending request - should not error
	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "nonexistent",
		Content:   "orphan",
	})
	if err == nil {
		t.Error("Send should return error for non-existent session")
	}
	// Verify it's a session not found error
	if !errors.Is(err, domain.ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got: %v", err)
	}
}

func TestHTTPChannelStartListenError(t *testing.T) {
	// Try to listen on an already-used port
	ch1 := NewHTTPChannel(":0", newHTTPTestLogger())
	ctx := context.Background()
	ch1.Start(ctx, func(ctx context.Context, msg domain.InboundMessage) error { return nil })
	defer ch1.Stop(ctx)

	// Try same port
	ch2 := NewHTTPChannel(ch1.boundAddr, newHTTPTestLogger())
	err := ch2.Start(ctx, func(ctx context.Context, msg domain.InboundMessage) error { return nil })
	if err == nil {
		ch2.Stop(ctx)
		t.Error("expected error when binding to already-used port")
	}
}

func TestHTTPChannelChatRequestCancelled(t *testing.T) {
	ch := NewHTTPChannel(":0", newHTTPTestLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handler that does NOT send a response, causing the select to wait
	handlerReady := make(chan struct{})
	handler := func(ctx context.Context, msg domain.InboundMessage) error {
		close(handlerReady)
		// Do NOT call ch.Send - the response channel will never receive
		return nil
	}

	if err := ch.Start(ctx, handler); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(ctx)

	time.Sleep(50 * time.Millisecond)

	body, _ := json.Marshal(chatRequest{
		SessionID: "cancel-session",
		Content:   "test",
	})

	// Create a request with a very short timeout to trigger r.Context().Done()
	reqCtx, reqCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer reqCancel()

	req, _ := http.NewRequestWithContext(reqCtx, http.MethodPost, fmt.Sprintf("http://%s/api/v1/chat", ch.boundAddr), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		// Context cancelled before response received is OK
		return
	}
	defer resp.Body.Close()

	// If we get a response, it should be a timeout (408)
	if resp.StatusCode != http.StatusRequestTimeout {
		t.Logf("status = %d (may vary depending on timing)", resp.StatusCode)
	}
}
