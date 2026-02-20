package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testManager(t *testing.T, opts ...func(*Manager)) *Manager {
	t.Helper()
	m := NewManager(
		NewNoopInvoker(),
		NewNoopDiscoverer(),
		NewAuth(),
		nil, nil,
		ManagerConfig{HeartbeatInterval: time.Second, InvokeTimeout: 5 * time.Second},
		testLogger(),
	)
	for _, o := range opts {
		o(m)
	}
	return m
}

// registerTestNode is a helper that generates a token and registers a node.
func registerTestNode(t *testing.T, m *Manager, id, name string, caps []domain.NodeCapability) {
	t.Helper()
	token, err := m.auth.GenerateToken(id)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	n := domain.Node{
		ID:           id,
		Name:         name,
		Platform:     "linux",
		Address:      "127.0.0.1:9090",
		Capabilities: caps,
		DeviceToken:  token,
	}
	if err := m.Register(context.Background(), n); err != nil {
		t.Fatalf("Register: %v", err)
	}
}

// --- mock invoker for testing ---

type mockInvoker struct {
	result json.RawMessage
	err    error
}

func (mi *mockInvoker) Invoke(_ context.Context, _, _ string, _ json.RawMessage) (json.RawMessage, error) {
	return mi.result, mi.err
}

// --- tests ---

func TestRegisterSuccess(t *testing.T) {
	m := testManager(t)
	registerTestNode(t, m, "n1", "Node 1", nil)

	nodes, _ := m.List(context.Background())
	if len(nodes) != 1 {
		t.Fatalf("node count = %d, want 1", len(nodes))
	}
	if nodes[0].Status != domain.NodeStatusOnline {
		t.Errorf("status = %q, want online", nodes[0].Status)
	}
}

func TestRegisterInvalidToken(t *testing.T) {
	m := testManager(t)
	m.auth.GenerateToken("n1")

	n := domain.Node{ID: "n1", DeviceToken: "wrong-token"}
	err := m.Register(context.Background(), n)
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
	if !errors.Is(err, domain.ErrNodeAuth) {
		t.Errorf("expected ErrNodeAuth, got: %v", err)
	}
}

func TestRegisterNotAllowed(t *testing.T) {
	m := testManager(t, func(mgr *Manager) {
		mgr.allowedSet = map[string]struct{}{"allowed-1": {}}
	})
	m.auth.GenerateToken("n1")

	n := domain.Node{ID: "n1", DeviceToken: "x"}
	err := m.Register(context.Background(), n)
	if !errors.Is(err, domain.ErrNodeNotAllowed) {
		t.Errorf("expected ErrNodeNotAllowed, got: %v", err)
	}
}

func TestRegisterAllowedWhenEmpty(t *testing.T) {
	m := testManager(t) // empty allowedSet = allow all
	registerTestNode(t, m, "n1", "Node 1", nil)

	nodes, _ := m.List(context.Background())
	if len(nodes) != 1 {
		t.Errorf("node count = %d, want 1", len(nodes))
	}
}

func TestRegisterDuplicate(t *testing.T) {
	m := testManager(t)
	registerTestNode(t, m, "n1", "Node 1", nil)

	token, _ := m.auth.GenerateToken("n1")
	err := m.Register(context.Background(), domain.Node{ID: "n1", DeviceToken: token})
	if !errors.Is(err, domain.ErrNodeDuplicate) {
		t.Errorf("expected ErrNodeDuplicate, got: %v", err)
	}
}

func TestUnregisterSuccess(t *testing.T) {
	m := testManager(t)
	registerTestNode(t, m, "n1", "Node 1", nil)

	if err := m.Unregister(context.Background(), "n1"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	nodes, _ := m.List(context.Background())
	if len(nodes) != 0 {
		t.Errorf("node count = %d, want 0", len(nodes))
	}
}

func TestUnregisterNotFound(t *testing.T) {
	m := testManager(t)
	err := m.Unregister(context.Background(), "missing")
	if !errors.Is(err, domain.ErrNodeNotFound) {
		t.Errorf("expected ErrNodeNotFound, got: %v", err)
	}
}

func TestList(t *testing.T) {
	m := testManager(t)
	registerTestNode(t, m, "b", "B", nil)
	registerTestNode(t, m, "a", "A", nil)

	nodes, _ := m.List(context.Background())
	if len(nodes) != 2 {
		t.Fatalf("node count = %d, want 2", len(nodes))
	}
	// Should be sorted by ID.
	if nodes[0].ID != "a" || nodes[1].ID != "b" {
		t.Errorf("order: %q, %q; want a, b", nodes[0].ID, nodes[1].ID)
	}
}

func TestGetSuccess(t *testing.T) {
	m := testManager(t)
	registerTestNode(t, m, "n1", "Node 1", nil)

	n, err := m.Get(context.Background(), "n1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if n.ID != "n1" {
		t.Errorf("ID = %q", n.ID)
	}
}

func TestGetNotFound(t *testing.T) {
	m := testManager(t)
	_, err := m.Get(context.Background(), "missing")
	if !errors.Is(err, domain.ErrNodeNotFound) {
		t.Errorf("expected ErrNodeNotFound, got: %v", err)
	}
}

func TestInvokeSuccess(t *testing.T) {
	inv := &mockInvoker{result: json.RawMessage(`{"status":"ok"}`)}
	m := testManager(t, func(mgr *Manager) { mgr.invoker = inv })

	caps := []domain.NodeCapability{{Name: "run_cmd", Description: "Run a command"}}
	registerTestNode(t, m, "n1", "Node 1", caps)

	result, err := m.Invoke(context.Background(), "n1", "run_cmd", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if string(result) != `{"status":"ok"}` {
		t.Errorf("result = %s", result)
	}
}

func TestInvokeNodeNotFound(t *testing.T) {
	m := testManager(t)
	_, err := m.Invoke(context.Background(), "missing", "cap", nil)
	if !errors.Is(err, domain.ErrNodeNotFound) {
		t.Errorf("expected ErrNodeNotFound, got: %v", err)
	}
}

func TestInvokeCapabilityNotFound(t *testing.T) {
	m := testManager(t)
	registerTestNode(t, m, "n1", "Node 1", nil)

	_, err := m.Invoke(context.Background(), "n1", "missing_cap", nil)
	if !errors.Is(err, domain.ErrNodeCapability) {
		t.Errorf("expected ErrNodeCapability, got: %v", err)
	}
}

func TestInvokeNodeOffline(t *testing.T) {
	m := testManager(t)
	registerTestNode(t, m, "n1", "Node 1", []domain.NodeCapability{{Name: "cap1"}})

	// Mark node as unreachable.
	m.mu.Lock()
	m.nodes["n1"].Status = domain.NodeStatusUnreachable
	m.mu.Unlock()

	_, err := m.Invoke(context.Background(), "n1", "cap1", nil)
	if !errors.Is(err, domain.ErrNodeUnreachable) {
		t.Errorf("expected ErrNodeUnreachable, got: %v", err)
	}
}

func TestInvokeError(t *testing.T) {
	inv := &mockInvoker{err: fmt.Errorf("connection refused")}
	m := testManager(t, func(mgr *Manager) { mgr.invoker = inv })

	caps := []domain.NodeCapability{{Name: "cap1"}}
	registerTestNode(t, m, "n1", "Node 1", caps)

	_, err := m.Invoke(context.Background(), "n1", "cap1", nil)
	if !errors.Is(err, domain.ErrNodeInvoke) {
		t.Errorf("expected ErrNodeInvoke, got: %v", err)
	}
}

func TestHeartbeatSuccess(t *testing.T) {
	m := testManager(t)
	registerTestNode(t, m, "n1", "Node 1", nil)

	time.Sleep(10 * time.Millisecond)

	if err := m.Heartbeat(context.Background(), "n1"); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	n, _ := m.Get(context.Background(), "n1")
	if time.Since(n.LastSeen) > time.Second {
		t.Error("LastSeen not updated")
	}
}

func TestHeartbeatNotFound(t *testing.T) {
	m := testManager(t)
	err := m.Heartbeat(context.Background(), "missing")
	if !errors.Is(err, domain.ErrNodeNotFound) {
		t.Errorf("expected ErrNodeNotFound, got: %v", err)
	}
}

func TestCheckHealthMarksUnreachable(t *testing.T) {
	m := testManager(t)
	registerTestNode(t, m, "n1", "Node 1", nil)

	// Backdate LastSeen.
	m.mu.Lock()
	m.nodes["n1"].LastSeen = time.Now().Add(-5 * time.Minute)
	m.mu.Unlock()

	m.checkHealth(context.Background(), time.Second)

	n, _ := m.Get(context.Background(), "n1")
	if n.Status != domain.NodeStatusUnreachable {
		t.Errorf("status = %q, want unreachable", n.Status)
	}
}

func TestDiscover(t *testing.T) {
	m := testManager(t) // NoopDiscoverer returns nil, nil
	nodes, err := m.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if nodes != nil {
		t.Errorf("expected nil from noop discoverer, got %v", nodes)
	}
}

func TestManagerConcurrentAccess(t *testing.T) {
	m := testManager(t)
	ctx := context.Background()
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			nodeID := fmt.Sprintf("node-%d", id)
			token, _ := m.auth.GenerateToken(nodeID)
			_ = m.Register(ctx, domain.Node{
				ID:          nodeID,
				Name:        nodeID,
				DeviceToken: token,
			})
			_, _ = m.List(ctx)
			_, _ = m.Get(ctx, nodeID)
			_ = m.Heartbeat(ctx, nodeID)
		}(i)
	}
	wg.Wait()
}
