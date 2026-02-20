package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"alfred-ai/internal/domain"
)

// --- test doubles ---

type testBus struct {
	mu       sync.Mutex
	handlers []domain.EventHandler
}

func (b *testBus) Publish(ctx context.Context, event domain.Event) {
	b.mu.Lock()
	hs := make([]domain.EventHandler, len(b.handlers))
	copy(hs, b.handlers)
	b.mu.Unlock()
	for _, h := range hs {
		h(ctx, event)
	}
}

func (b *testBus) Subscribe(_ domain.EventType, _ domain.EventHandler) func() { return func() {} }

func (b *testBus) SubscribeAll(handler domain.EventHandler) func() {
	b.mu.Lock()
	b.handlers = append(b.handlers, handler)
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for i, h := range b.handlers {
			// Simple identity check by function address is not reliable,
			// so just clear all for tests.
			_ = h
			_ = i
		}
		b.handlers = nil
	}
}

func (b *testBus) Close() {}

func newTestAuth() Authenticator {
	return NewStaticTokenAuth([]struct {
		Token string
		Name  string
		Roles []string
	}{
		{Token: "test-token", Name: "tester", Roles: []string{"admin"}},
	})
}

func startTestServer(t *testing.T, bus domain.EventBus) *Server {
	t.Helper()
	srv := NewServer(bus, newTestAuth(), "127.0.0.1:0", slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	started := make(chan struct{})
	go func() {
		// Wait for server to bind.
		go func() {
			for srv.BoundAddr() == "" {
				time.Sleep(5 * time.Millisecond)
			}
			close(started)
		}()
		if err := srv.Start(ctx); err != nil {
			// Only log; the test may have cancelled context already.
			_ = err
		}
	}()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not start in time")
	}

	t.Cleanup(func() {
		srv.Stop(context.Background())
	})

	return srv
}

func dialWS(t *testing.T, addr, token string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ws, _, err := websocket.Dial(ctx, "ws://"+addr+"/ws?token="+token, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { ws.Close(websocket.StatusNormalClosure, "") })
	return ws
}

// --- tests ---

func TestServerLifecycle(t *testing.T) {
	bus := &testBus{}
	srv := startTestServer(t, bus)

	if srv.BoundAddr() == "" {
		t.Fatal("BoundAddr is empty")
	}
}

func TestServerAuthReject(t *testing.T) {
	bus := &testBus{}
	srv := startTestServer(t, bus)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, err := websocket.Dial(ctx, "ws://"+srv.BoundAddr()+"/ws?token=bad-token", nil)
	if err == nil {
		t.Fatal("expected auth rejection")
	}
}

func TestServerRPCRoundtrip(t *testing.T) {
	bus := &testBus{}
	srv := startTestServer(t, bus)

	// Register a simple echo handler.
	srv.RegisterHandler("echo", func(_ context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		return payload, nil
	})

	ws := dialWS(t, srv.BoundAddr(), "test-token")
	ctx := context.Background()

	// Send request.
	req := Frame{
		Type:    FrameTypeRequest,
		ID:      1,
		Method:  "echo",
		Payload: json.RawMessage(`{"msg":"hello"}`),
	}
	if err := wsjson.Write(ctx, ws, req); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read response.
	var resp Frame
	if err := wsjson.Read(ctx, ws, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}

	if resp.Type != FrameTypeResponse {
		t.Errorf("type = %q", resp.Type)
	}
	if resp.ID != 1 {
		t.Errorf("ID = %d", resp.ID)
	}
	if resp.Error != "" {
		t.Errorf("error = %q", resp.Error)
	}
	if string(resp.Payload) != `{"msg":"hello"}` {
		t.Errorf("payload = %s", resp.Payload)
	}
}

func TestServerUnknownMethod(t *testing.T) {
	bus := &testBus{}
	srv := startTestServer(t, bus)

	ws := dialWS(t, srv.BoundAddr(), "test-token")
	ctx := context.Background()

	req := Frame{
		Type:   FrameTypeRequest,
		ID:     2,
		Method: "nonexistent",
	}
	if err := wsjson.Write(ctx, ws, req); err != nil {
		t.Fatalf("write: %v", err)
	}

	var resp Frame
	if err := wsjson.Read(ctx, ws, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}

	if resp.Error == "" {
		t.Error("expected error for unknown method")
	}
}

func TestServerEventForwarding(t *testing.T) {
	bus := &testBus{}
	srv := startTestServer(t, bus)

	ws := dialWS(t, srv.BoundAddr(), "test-token")

	// Give the connection time to be registered.
	time.Sleep(100 * time.Millisecond)

	// Publish an event.
	bus.Publish(context.Background(), domain.Event{
		Type:      domain.EventMessageReceived,
		Timestamp: time.Now(),
		SessionID: "test-sess",
	})

	// Read the forwarded event.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var frame Frame
	if err := wsjson.Read(ctx, ws, &frame); err != nil {
		t.Fatalf("read event: %v", err)
	}

	if frame.Type != FrameTypeEvent {
		t.Errorf("type = %q, want event", frame.Type)
	}
}

func TestServerSlowClient(t *testing.T) {
	bus := &testBus{}
	srv := startTestServer(t, bus)

	ws := dialWS(t, srv.BoundAddr(), "test-token")
	_ = ws // connected but not reading

	// Give time for connection registration.
	time.Sleep(100 * time.Millisecond)

	// Flood events — should not block or panic.
	for i := 0; i < 200; i++ {
		bus.Publish(context.Background(), domain.Event{
			Type:      domain.EventMessageSent,
			Timestamp: time.Now(),
		})
	}
	// If we get here without hanging, the test passes.
}

func TestServerConcurrentClients(t *testing.T) {
	bus := &testBus{}
	srv := startTestServer(t, bus)

	srv.RegisterHandler("ping", func(_ context.Context, _ *ClientInfo, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"pong"`), nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ws := dialWS(t, srv.BoundAddr(), "test-token")

			ctx := context.Background()
			req := Frame{Type: FrameTypeRequest, ID: uint64(id), Method: "ping"}
			if err := wsjson.Write(ctx, ws, req); err != nil {
				return
			}
			var resp Frame
			wsjson.Read(ctx, ws, &resp)
		}(i)
	}
	wg.Wait()
}

func TestServerDisconnect(t *testing.T) {
	bus := &testBus{}
	srv := startTestServer(t, bus)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ws, _, err := websocket.Dial(ctx, "ws://"+srv.BoundAddr()+"/ws?token=test-token", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Immediately close.
	ws.Close(websocket.StatusNormalClosure, "bye")

	// Give server time to clean up.
	time.Sleep(100 * time.Millisecond)

	// Publish an event — should not panic even though client is gone.
	bus.Publish(context.Background(), domain.Event{
		Type:      domain.EventMessageSent,
		Timestamp: time.Now(),
	})
}

func TestServerHandlerError(t *testing.T) {
	bus := &testBus{}
	srv := startTestServer(t, bus)

	srv.RegisterHandler("fail", func(_ context.Context, _ *ClientInfo, _ json.RawMessage) (json.RawMessage, error) {
		return nil, domain.ErrRPCInvalidPayload
	})

	ws := dialWS(t, srv.BoundAddr(), "test-token")
	ctx := context.Background()

	req := Frame{Type: FrameTypeRequest, ID: 1, Method: "fail"}
	if err := wsjson.Write(ctx, ws, req); err != nil {
		t.Fatalf("write: %v", err)
	}

	var resp Frame
	if err := wsjson.Read(ctx, ws, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}

	if resp.Error == "" {
		t.Error("expected error in response")
	}
}
