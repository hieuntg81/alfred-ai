package channel

import (
	"context"
	"encoding/json"
	"fmt"
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

func newMatrixTestLogger() *slog.Logger { return slog.Default() }

func TestMatrixChannelName(t *testing.T) {
	ch := NewMatrixChannel("https://matrix.org", "token", "@bot:matrix.org", newMatrixTestLogger())
	if ch.Name() != "matrix" {
		t.Errorf("Name = %q, want matrix", ch.Name())
	}
}

func TestMatrixReceiveMessage(t *testing.T) {
	var received domain.InboundMessage
	var handlerCalled atomic.Int32

	syncCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/_matrix/client/v3/sync") {
			syncCount++
			if syncCount == 1 {
				resp := matrixSyncResponse{
					NextBatch: "batch-2",
					Rooms: matrixSyncRooms{
						Join: map[string]matrixJoinedRoom{
							"!room1:matrix.org": {
								Timeline: matrixTimeline{
									Events: []matrixEvent{{
										Type:   "m.room.message",
										Sender: "@alice:matrix.org",
										Content: map[string]interface{}{
											"msgtype": "m.text",
											"body":    "Hello from Matrix",
										},
									}},
								},
							},
						},
					},
				}
				json.NewEncoder(w).Encode(resp)
				return
			}
			// Subsequent syncs return empty.
			json.NewEncoder(w).Encode(matrixSyncResponse{NextBatch: "batch-3"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	ch := NewMatrixChannel(server.URL, "test-token", "@bot:matrix.org", newMatrixTestLogger())

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
	if received.SessionID != "!room1:matrix.org" {
		t.Errorf("SessionID = %q", received.SessionID)
	}
	if received.SenderID != "@alice:matrix.org" {
		t.Errorf("SenderID = %q", received.SenderID)
	}
	if received.Content != "Hello from Matrix" {
		t.Errorf("Content = %q", received.Content)
	}
	if received.ChannelName != "matrix" {
		t.Errorf("ChannelName = %q", received.ChannelName)
	}
	if received.GroupID != "!room1:matrix.org" {
		t.Errorf("GroupID = %q", received.GroupID)
	}
}

func TestMatrixFilterOwnMessages(t *testing.T) {
	var handlerCalled atomic.Int32
	syncCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "sync") {
			syncCount++
			if syncCount == 1 {
				resp := matrixSyncResponse{
					NextBatch: "batch-2",
					Rooms: matrixSyncRooms{
						Join: map[string]matrixJoinedRoom{
							"!room1:matrix.org": {
								Timeline: matrixTimeline{
									Events: []matrixEvent{{
										Type:   "m.room.message",
										Sender: "@bot:matrix.org", // own message
										Content: map[string]interface{}{
											"msgtype": "m.text",
											"body":    "my own message",
										},
									}},
								},
							},
						},
					},
				}
				json.NewEncoder(w).Encode(resp)
				return
			}
			json.NewEncoder(w).Encode(matrixSyncResponse{NextBatch: "batch-3"})
			return
		}
	}))
	defer server.Close()

	ch := NewMatrixChannel(server.URL, "test-token", "@bot:matrix.org", newMatrixTestLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	})
	time.Sleep(250 * time.Millisecond)
	ch.Stop(ctx)

	if handlerCalled.Load() != 0 {
		t.Errorf("handler called %d times, want 0 for own messages", handlerCalled.Load())
	}
}

func TestMatrixSendMessage(t *testing.T) {
	var sentBody map[string]string
	var reqMethod string
	var reqPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqMethod = r.Method
		reqPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&sentBody)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"event_id":"$evt1"}`))
	}))
	defer server.Close()

	ch := NewMatrixChannel(server.URL, "test-token", "@bot:matrix.org", newMatrixTestLogger())

	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "!room1:matrix.org",
		Content:   "Hello from bot",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if reqMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", reqMethod)
	}
	if !strings.Contains(reqPath, "!room1:matrix.org") {
		t.Errorf("path = %q, expected room ID", reqPath)
	}
	if !strings.Contains(reqPath, "m.room.message") {
		t.Errorf("path = %q, expected m.room.message", reqPath)
	}
	if sentBody["msgtype"] != "m.text" {
		t.Errorf("msgtype = %q", sentBody["msgtype"])
	}
	if sentBody["body"] != "Hello from bot" {
		t.Errorf("body = %q", sentBody["body"])
	}
}

func TestMatrixSendErrorMessage(t *testing.T) {
	var sentBody map[string]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&sentBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ch := NewMatrixChannel(server.URL, "test-token", "@bot:matrix.org", newMatrixTestLogger())

	ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "!room1:matrix.org",
		Content:   "something failed",
		IsError:   true,
	})

	if sentBody["body"] != "Error: something failed" {
		t.Errorf("body = %q", sentBody["body"])
	}
}

func TestMatrixSendAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	}))
	defer server.Close()

	ch := NewMatrixChannel(server.URL, "test-token", "@bot:matrix.org", newMatrixTestLogger())

	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "!room1:matrix.org",
		Content:   "test",
	})
	if err == nil {
		t.Error("expected error for API error")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 in error, got %v", err)
	}
}

func TestMatrixSyncErrorRetry(t *testing.T) {
	syncCount := 0
	var handlerCalled atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "sync") {
			syncCount++
			if syncCount == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("server error"))
				return
			}
			// Second sync succeeds with a message.
			resp := matrixSyncResponse{
				NextBatch: "batch-2",
				Rooms: matrixSyncRooms{
					Join: map[string]matrixJoinedRoom{
						"!room1:matrix.org": {
							Timeline: matrixTimeline{
								Events: []matrixEvent{{
									Type:   "m.room.message",
									Sender: "@alice:matrix.org",
									Content: map[string]interface{}{
										"msgtype": "m.text",
										"body":    "after retry",
									},
								}},
							},
						},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
	}))
	defer server.Close()

	ch := NewMatrixChannel(server.URL, "test-token", "@bot:matrix.org", newMatrixTestLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	})

	// Wait for error + retry delay + second sync.
	time.Sleep(7 * time.Second)
	ch.Stop(ctx)

	if handlerCalled.Load() < 1 {
		t.Error("handler was never called after retry")
	}
}

func TestMatrixSyncInvalidJSON(t *testing.T) {
	syncCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		syncCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	ch := NewMatrixChannel(server.URL, "test-token", "@bot:matrix.org", newMatrixTestLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error { return nil })
	time.Sleep(200 * time.Millisecond)
	ch.Stop(ctx)

	// Should not panic — just logs warning and retries.
}

func TestMatrixAutoJoinOnInvite(t *testing.T) {
	var joinedRoom string
	var mu sync.Mutex
	syncCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/join") {
			mu.Lock()
			// Extract room ID from path: /_matrix/client/v3/rooms/{roomID}/join
			parts := strings.Split(r.URL.Path, "/")
			for i, p := range parts {
				if p == "rooms" && i+1 < len(parts) {
					joinedRoom = parts[i+1]
					break
				}
			}
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"room_id":"!invited:matrix.org"}`))
			return
		}
		if strings.Contains(r.URL.Path, "sync") {
			syncCount++
			if syncCount == 1 {
				resp := matrixSyncResponse{
					NextBatch: "batch-2",
					Rooms: matrixSyncRooms{
						Invite: map[string]matrixInviteRoom{
							"!invited:matrix.org": {},
						},
					},
				}
				json.NewEncoder(w).Encode(resp)
				return
			}
			json.NewEncoder(w).Encode(matrixSyncResponse{NextBatch: "batch-3"})
			return
		}
	}))
	defer server.Close()

	ch := NewMatrixChannel(server.URL, "test-token", "@bot:matrix.org", newMatrixTestLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error { return nil })
	time.Sleep(300 * time.Millisecond)
	ch.Stop(ctx)

	mu.Lock()
	room := joinedRoom
	mu.Unlock()

	if room != "!invited:matrix.org" {
		t.Errorf("joined room = %q, want !invited:matrix.org", room)
	}
}

func TestMatrixStopBeforeStart(t *testing.T) {
	ch := NewMatrixChannel("https://matrix.org", "token", "@bot:matrix.org", newMatrixTestLogger())
	if err := ch.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestMatrixStartStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(matrixSyncResponse{NextBatch: "batch-1"})
	}))
	defer server.Close()

	ch := NewMatrixChannel(server.URL, "test-token", "@bot:matrix.org", newMatrixTestLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	if err := ch.Start(ctx, func(_ context.Context, _ domain.InboundMessage) error { return nil }); err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if err := ch.Stop(ctx); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestMatrixCommandHelp(t *testing.T) {
	var sentBody map[string]string

	syncCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "sync") {
			syncCount++
			if syncCount == 1 {
				resp := matrixSyncResponse{
					NextBatch: "batch-2",
					Rooms: matrixSyncRooms{
						Join: map[string]matrixJoinedRoom{
							"!room1:matrix.org": {
								Timeline: matrixTimeline{
									Events: []matrixEvent{{
										Type:   "m.room.message",
										Sender: "@alice:matrix.org",
										Content: map[string]interface{}{
											"msgtype": "m.text",
											"body":    "/help",
										},
									}},
								},
							},
						},
					},
				}
				json.NewEncoder(w).Encode(resp)
				return
			}
			json.NewEncoder(w).Encode(matrixSyncResponse{NextBatch: "batch-3"})
			return
		}
		if strings.Contains(r.URL.Path, "m.room.message") {
			json.NewDecoder(r.Body).Decode(&sentBody)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"event_id":"$evt1"}`))
			return
		}
	}))
	defer server.Close()

	var handlerCalled bool
	ch := NewMatrixChannel(server.URL, "test-token", "@bot:matrix.org", newMatrixTestLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error {
		handlerCalled = true
		return nil
	})
	time.Sleep(300 * time.Millisecond)
	ch.Stop(ctx)

	if handlerCalled {
		t.Error("handler should not be called for /help command")
	}
	if sentBody == nil || sentBody["body"] == "" {
		t.Error("help text should have been sent")
	}
}

func TestMatrixCommandPrivacy(t *testing.T) {
	var sentBody map[string]string

	syncCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "sync") {
			syncCount++
			if syncCount == 1 {
				resp := matrixSyncResponse{
					NextBatch: "batch-2",
					Rooms: matrixSyncRooms{
						Join: map[string]matrixJoinedRoom{
							"!room1:matrix.org": {
								Timeline: matrixTimeline{
									Events: []matrixEvent{{
										Type:   "m.room.message",
										Sender: "@alice:matrix.org",
										Content: map[string]interface{}{
											"msgtype": "m.text",
											"body":    "/privacy",
										},
									}},
								},
							},
						},
					},
				}
				json.NewEncoder(w).Encode(resp)
				return
			}
			json.NewEncoder(w).Encode(matrixSyncResponse{NextBatch: "batch-3"})
			return
		}
		if strings.Contains(r.URL.Path, "m.room.message") {
			json.NewDecoder(r.Body).Decode(&sentBody)
			w.WriteHeader(http.StatusOK)
			return
		}
	}))
	defer server.Close()

	ch := NewMatrixChannel(server.URL, "test-token", "@bot:matrix.org", newMatrixTestLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error { return nil })
	time.Sleep(300 * time.Millisecond)
	ch.Stop(ctx)

	if sentBody == nil || !strings.Contains(sentBody["body"], "Privacy") {
		t.Errorf("expected privacy text, got %v", sentBody)
	}
}

func TestMatrixMentionOnly(t *testing.T) {
	var handlerCalled atomic.Int32
	syncCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "sync") {
			syncCount++
			if syncCount == 1 {
				resp := matrixSyncResponse{
					NextBatch: "batch-2",
					Rooms: matrixSyncRooms{
						Join: map[string]matrixJoinedRoom{
							"!room1:matrix.org": {
								Timeline: matrixTimeline{
									Events: []matrixEvent{{
										Type:   "m.room.message",
										Sender: "@alice:matrix.org",
										Content: map[string]interface{}{
											"msgtype": "m.text",
											"body":    "hello everyone", // no mention of bot
										},
									}},
								},
							},
						},
					},
				}
				json.NewEncoder(w).Encode(resp)
				return
			}
			json.NewEncoder(w).Encode(matrixSyncResponse{NextBatch: "batch-3"})
			return
		}
	}))
	defer server.Close()

	ch := NewMatrixChannel(server.URL, "test-token", "@bot:matrix.org", newMatrixTestLogger(), WithMatrixMentionOnly(true))

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	})
	time.Sleep(300 * time.Millisecond)
	ch.Stop(ctx)

	if handlerCalled.Load() != 0 {
		t.Errorf("handler called %d times, want 0 in mentionOnly mode without mention", handlerCalled.Load())
	}
}

func TestMatrixNextBatchToken(t *testing.T) {
	syncCount := 0
	var receivedSince string
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "sync") {
			syncCount++
			if syncCount == 2 {
				mu.Lock()
				receivedSince = r.URL.Query().Get("since")
				mu.Unlock()
			}
			json.NewEncoder(w).Encode(matrixSyncResponse{
				NextBatch: fmt.Sprintf("batch-%d", syncCount),
			})
			return
		}
	}))
	defer server.Close()

	ch := NewMatrixChannel(server.URL, "test-token", "@bot:matrix.org", newMatrixTestLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error { return nil })
	time.Sleep(300 * time.Millisecond)
	ch.Stop(ctx)

	mu.Lock()
	since := receivedSince
	mu.Unlock()

	if since != "batch-1" {
		t.Errorf("since = %q, want batch-1", since)
	}
}

func TestMatrixSendUnreachable(t *testing.T) {
	ch := NewMatrixChannel("http://localhost:1", "test-token", "@bot:matrix.org", newMatrixTestLogger())

	err := ch.Send(context.Background(), domain.OutboundMessage{
		SessionID: "!room1:matrix.org",
		Content:   "test",
	})
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

func TestMatrixNonTextIgnored(t *testing.T) {
	var handlerCalled atomic.Int32
	syncCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "sync") {
			syncCount++
			if syncCount == 1 {
				resp := matrixSyncResponse{
					NextBatch: "batch-2",
					Rooms: matrixSyncRooms{
						Join: map[string]matrixJoinedRoom{
							"!room1:matrix.org": {
								Timeline: matrixTimeline{
									Events: []matrixEvent{{
										Type:   "m.room.message",
										Sender: "@alice:matrix.org",
										Content: map[string]interface{}{
											"msgtype": "m.image",
											"body":    "image.png",
										},
									}},
								},
							},
						},
					},
				}
				json.NewEncoder(w).Encode(resp)
				return
			}
			json.NewEncoder(w).Encode(matrixSyncResponse{NextBatch: "batch-3"})
			return
		}
	}))
	defer server.Close()

	ch := NewMatrixChannel(server.URL, "test-token", "@bot:matrix.org", newMatrixTestLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch.Start(ctx, func(_ context.Context, msg domain.InboundMessage) error {
		handlerCalled.Add(1)
		return nil
	})
	time.Sleep(300 * time.Millisecond)
	ch.Stop(ctx)

	if handlerCalled.Load() != 0 {
		t.Errorf("handler called %d times, want 0 for non-text message", handlerCalled.Load())
	}
}

func TestMatrixSendTxnIDIncrement(t *testing.T) {
	var paths []string
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"event_id":"$evt"}`))
	}))
	defer server.Close()

	ch := NewMatrixChannel(server.URL, "test-token", "@bot:matrix.org", newMatrixTestLogger())

	ch.Send(context.Background(), domain.OutboundMessage{SessionID: "!room:m.org", Content: "msg1"})
	ch.Send(context.Background(), domain.OutboundMessage{SessionID: "!room:m.org", Content: "msg2"})

	mu.Lock()
	defer mu.Unlock()

	if len(paths) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(paths))
	}
	// Transaction IDs should differ.
	if paths[0] == paths[1] {
		t.Errorf("expected different txnIDs, got same paths: %q", paths[0])
	}
}
