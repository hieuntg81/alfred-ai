package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

// --- Mock Redis client ---

type mockRedis struct {
	mu      sync.Mutex
	store   map[string]string
	expiry  map[string]time.Duration
	pubCh   map[string][]chan string
	closed  bool
}

func newMockRedis() *mockRedis {
	return &mockRedis{
		store:  make(map[string]string),
		expiry: make(map[string]time.Duration),
		pubCh:  make(map[string][]chan string),
	}
}

func (m *mockRedis) SetNX(_ context.Context, key, value string, exp time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.store[key]; exists {
		return false, nil
	}
	m.store[key] = value
	m.expiry[key] = exp
	return true, nil
}

func (m *mockRedis) Del(_ context.Context, keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.store, k)
		delete(m.expiry, k)
	}
	return nil
}

func (m *mockRedis) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.store[key]
	if !ok {
		return "", errors.New("key not found")
	}
	return v, nil
}

func (m *mockRedis) Publish(_ context.Context, channel, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ch := range m.pubCh[channel] {
		select {
		case ch <- message:
		default:
		}
	}
	return nil
}

func (m *mockRedis) Subscribe(_ context.Context, channel string) (<-chan string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan string, 32)
	m.pubCh[channel] = append(m.pubCh[channel], ch)
	return ch, nil
}

func (m *mockRedis) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	for _, chs := range m.pubCh {
		for _, ch := range chs {
			close(ch)
		}
	}
	m.pubCh = make(map[string][]chan string)
	return nil
}

// --- Tests ---

func TestAcquireSession(t *testing.T) {
	redis := newMockRedis()
	coord := NewClusterCoordinator(redis, CoordinatorConfig{NodeID: "node-1"}, slog.Default())

	ctx := context.Background()

	// First acquire should succeed.
	got, err := coord.AcquireSession(ctx, "session-abc")
	if err != nil {
		t.Fatalf("AcquireSession: %v", err)
	}
	if !got {
		t.Error("expected to acquire lock")
	}

	// Second acquire by same coordinator should fail (lock already held).
	got, err = coord.AcquireSession(ctx, "session-abc")
	if err != nil {
		t.Fatalf("AcquireSession second: %v", err)
	}
	if got {
		t.Error("expected lock to be already held")
	}
}

func TestAcquireSession_DifferentNodes(t *testing.T) {
	redis := newMockRedis()
	node1 := NewClusterCoordinator(redis, CoordinatorConfig{NodeID: "node-1"}, slog.Default())
	node2 := NewClusterCoordinator(redis, CoordinatorConfig{NodeID: "node-2"}, slog.Default())

	ctx := context.Background()

	// Node 1 acquires.
	ok, _ := node1.AcquireSession(ctx, "s1")
	if !ok {
		t.Fatal("node-1 should acquire lock")
	}

	// Node 2 fails to acquire same session.
	ok, _ = node2.AcquireSession(ctx, "s1")
	if ok {
		t.Fatal("node-2 should NOT acquire lock held by node-1")
	}

	// Node 2 acquires a different session.
	ok, _ = node2.AcquireSession(ctx, "s2")
	if !ok {
		t.Fatal("node-2 should acquire lock on different session")
	}
}

func TestReleaseSession(t *testing.T) {
	redis := newMockRedis()
	coord := NewClusterCoordinator(redis, CoordinatorConfig{NodeID: "node-1"}, slog.Default())

	ctx := context.Background()

	// Acquire then release.
	coord.AcquireSession(ctx, "s1")
	if err := coord.ReleaseSession(ctx, "s1"); err != nil {
		t.Fatalf("ReleaseSession: %v", err)
	}

	// After release, another acquire should succeed.
	ok, _ := coord.AcquireSession(ctx, "s1")
	if !ok {
		t.Error("expected to re-acquire after release")
	}
}

func TestReleaseSession_NotOwner(t *testing.T) {
	redis := newMockRedis()
	node1 := NewClusterCoordinator(redis, CoordinatorConfig{NodeID: "node-1"}, slog.Default())
	node2 := NewClusterCoordinator(redis, CoordinatorConfig{NodeID: "node-2"}, slog.Default())

	ctx := context.Background()

	// Node 1 acquires.
	node1.AcquireSession(ctx, "s1")

	// Node 2 tries to release — should not release (not owner).
	if err := node2.ReleaseSession(ctx, "s1"); err != nil {
		t.Fatalf("ReleaseSession: %v", err)
	}

	// Lock should still be held — node 2 can't acquire.
	ok, _ := node2.AcquireSession(ctx, "s1")
	if ok {
		t.Error("node-2 should not acquire lock that was not properly released")
	}
}

func TestReleaseSession_NonExistent(t *testing.T) {
	redis := newMockRedis()
	coord := NewClusterCoordinator(redis, CoordinatorConfig{NodeID: "node-1"}, slog.Default())

	// Release a session that was never acquired — should not error.
	if err := coord.ReleaseSession(context.Background(), "nonexistent"); err != nil {
		t.Fatalf("ReleaseSession nonexistent: %v", err)
	}
}

func TestPublishAndSubscribeEvents(t *testing.T) {
	redis := newMockRedis()
	coord := NewClusterCoordinator(redis, CoordinatorConfig{NodeID: "node-1"}, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan domain.Event, 1)
	err := coord.SubscribeEvents(ctx, func(_ context.Context, event domain.Event) {
		received <- event
	})
	if err != nil {
		t.Fatalf("SubscribeEvents: %v", err)
	}

	// Publish an event.
	evt := domain.Event{
		Type:      domain.EventMessageReceived,
		SessionID: "s1",
		Timestamp: time.Now(),
	}
	if err := coord.PublishEvent(ctx, evt); err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}

	// Wait for receipt.
	select {
	case got := <-received:
		if got.Type != domain.EventMessageReceived {
			t.Errorf("event type = %v, want %v", got.Type, domain.EventMessageReceived)
		}
		if got.SessionID != "s1" {
			t.Errorf("session = %q, want %q", got.SessionID, "s1")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestPublishEvent_MarshalRoundtrip(t *testing.T) {
	redis := newMockRedis()
	coord := NewClusterCoordinator(redis, CoordinatorConfig{NodeID: "node-1"}, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan domain.Event, 1)
	coord.SubscribeEvents(ctx, func(_ context.Context, event domain.Event) {
		received <- event
	})

	payload := map[string]string{"key": "value"}
	raw, _ := json.Marshal(payload)
	evt := domain.Event{
		Type:      domain.EventMessageSent,
		SessionID: "s2",
		Timestamp: time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
		Payload:   raw,
	}
	coord.PublishEvent(ctx, evt)

	select {
	case got := <-received:
		if got.SessionID != "s2" {
			t.Errorf("session = %q, want %q", got.SessionID, "s2")
		}
		var p map[string]string
		json.Unmarshal(got.Payload, &p)
		if p["key"] != "value" {
			t.Errorf("payload key = %q, want %q", p["key"], "value")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestCoordinatorNodeID(t *testing.T) {
	redis := newMockRedis()
	coord := NewClusterCoordinator(redis, CoordinatorConfig{NodeID: "my-node"}, slog.Default())
	if coord.NodeID() != "my-node" {
		t.Errorf("NodeID = %q, want %q", coord.NodeID(), "my-node")
	}
}

func TestCoordinatorDefaultLockTTL(t *testing.T) {
	redis := newMockRedis()
	coord := NewClusterCoordinator(redis, CoordinatorConfig{NodeID: "n1"}, slog.Default())
	if coord.lockTTL != 30*time.Second {
		t.Errorf("lockTTL = %v, want 30s", coord.lockTTL)
	}
}

func TestCoordinatorCustomLockTTL(t *testing.T) {
	redis := newMockRedis()
	coord := NewClusterCoordinator(redis, CoordinatorConfig{NodeID: "n1", LockTTL: 10 * time.Second}, slog.Default())
	if coord.lockTTL != 10*time.Second {
		t.Errorf("lockTTL = %v, want 10s", coord.lockTTL)
	}
}

func TestCoordinatorStop(t *testing.T) {
	redis := newMockRedis()
	coord := NewClusterCoordinator(redis, CoordinatorConfig{NodeID: "n1"}, slog.Default())

	if err := coord.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !redis.closed {
		t.Error("expected redis client to be closed")
	}
}
