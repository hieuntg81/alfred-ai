package node

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"alfred-ai/internal/domain"
)

// ManagerConfig holds configuration for the node manager.
type ManagerConfig struct {
	HeartbeatInterval time.Duration
	InvokeTimeout     time.Duration
	AllowedNodes      []string
}

// Manager manages remote node registration, invocation, and discovery.
type Manager struct {
	mu          sync.RWMutex
	nodes       map[string]*domain.Node
	invoker     NodeInvoker
	discoverer  NodeDiscoverer
	auth        *Auth
	bus         domain.EventBus
	auditLogger domain.AuditLogger
	config      ManagerConfig
	logger      *slog.Logger
	allowedSet  map[string]struct{}
}

// NewManager creates a new node manager. bus and auditLogger may be nil.
func NewManager(
	invoker NodeInvoker,
	discoverer NodeDiscoverer,
	auth *Auth,
	bus domain.EventBus,
	auditLogger domain.AuditLogger,
	cfg ManagerConfig,
	logger *slog.Logger,
) *Manager {
	allowed := make(map[string]struct{}, len(cfg.AllowedNodes))
	for _, id := range cfg.AllowedNodes {
		allowed[id] = struct{}{}
	}
	return &Manager{
		nodes:       make(map[string]*domain.Node),
		invoker:     invoker,
		discoverer:  discoverer,
		auth:        auth,
		bus:         bus,
		auditLogger: auditLogger,
		config:      cfg,
		logger:      logger,
		allowedSet:  allowed,
	}
}

// Register adds a new node. The node's DeviceToken is validated and then cleared.
func (m *Manager) Register(ctx context.Context, n domain.Node) error {
	if n.ID == "" {
		return domain.NewDomainError("Manager.Register", domain.ErrNodeAuth, "empty node ID")
	}

	// Allowlist check (empty allowlist = allow all). Allowlist is immutable after
	// construction so no lock is needed.
	if len(m.allowedSet) > 0 {
		if _, ok := m.allowedSet[n.ID]; !ok {
			return domain.NewDomainError("Manager.Register", domain.ErrNodeNotAllowed, n.ID)
		}
	}

	// Validate device token before acquiring the lock. Auth has its own lock,
	// and token validation is idempotent, so no TOCTOU risk here — the token
	// either matches or it doesn't.
	if err := m.auth.ValidateToken(n.ID, n.DeviceToken); err != nil {
		return err
	}

	m.mu.Lock()
	// Re-validate token under the manager lock to close the TOCTOU window
	// (token could have been revoked between validation and lock acquisition).
	if err := m.auth.ValidateToken(n.ID, n.DeviceToken); err != nil {
		m.mu.Unlock()
		return err
	}
	if _, exists := m.nodes[n.ID]; exists {
		m.mu.Unlock()
		return domain.NewDomainError("Manager.Register", domain.ErrNodeDuplicate, n.ID)
	}
	n.DeviceToken = "" // never store raw token
	n.Status = domain.NodeStatusOnline
	n.LastSeen = time.Now()
	m.nodes[n.ID] = &n
	m.mu.Unlock()

	m.publishEvent(ctx, domain.EventNodeRegistered, map[string]string{"node_id": n.ID, "name": n.Name})
	m.audit(ctx, domain.AuditNodeRegister, map[string]string{"node_id": n.ID})
	m.logger.Info("node registered", "node_id", n.ID, "name", n.Name)
	return nil
}

// Unregister removes a node.
func (m *Manager) Unregister(ctx context.Context, nodeID string) error {
	m.mu.Lock()
	_, exists := m.nodes[nodeID]
	if !exists {
		m.mu.Unlock()
		return domain.NewDomainError("Manager.Unregister", domain.ErrNodeNotFound, nodeID)
	}
	delete(m.nodes, nodeID)
	m.mu.Unlock()

	m.publishEvent(ctx, domain.EventNodeUnregistered, map[string]string{"node_id": nodeID})
	m.audit(ctx, domain.AuditNodeUnregister, map[string]string{"node_id": nodeID})
	m.logger.Info("node unregistered", "node_id", nodeID)
	return nil
}

// List returns all registered nodes sorted by ID.
func (m *Manager) List(_ context.Context) ([]domain.Node, error) {
	m.mu.RLock()
	nodes := make([]domain.Node, 0, len(m.nodes))
	for _, n := range m.nodes {
		nodes = append(nodes, *n)
	}
	m.mu.RUnlock()

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
	return nodes, nil
}

// Get returns a single node by ID.
func (m *Manager) Get(_ context.Context, nodeID string) (*domain.Node, error) {
	m.mu.RLock()
	n, ok := m.nodes[nodeID]
	m.mu.RUnlock()

	if !ok {
		return nil, domain.NewDomainError("Manager.Get", domain.ErrNodeNotFound, nodeID)
	}
	return new(*n), nil
}

// Invoke calls a capability on a remote node.
func (m *Manager) Invoke(ctx context.Context, nodeID, capability string, params json.RawMessage) (json.RawMessage, error) {
	// Copy node data while holding the lock to avoid data races.
	m.mu.RLock()
	n, ok := m.nodes[nodeID]
	var nodeCopy domain.Node
	if ok {
		nodeCopy = *n
	}
	m.mu.RUnlock()

	if !ok {
		return nil, domain.NewDomainError("Manager.Invoke", domain.ErrNodeNotFound, nodeID)
	}
	if nodeCopy.Status != domain.NodeStatusOnline {
		return nil, domain.NewDomainError("Manager.Invoke", domain.ErrNodeUnreachable, nodeID)
	}

	// Verify the node has the requested capability.
	found := false
	for _, cap := range nodeCopy.Capabilities {
		if cap.Name == capability {
			found = true
			break
		}
	}
	if !found {
		return nil, domain.NewDomainError("Manager.Invoke", domain.ErrNodeCapability, fmt.Sprintf("%s on %s", capability, nodeID))
	}

	invokeCtx := ctx
	if m.config.InvokeTimeout > 0 {
		var cancel context.CancelFunc
		invokeCtx, cancel = context.WithTimeout(ctx, m.config.InvokeTimeout)
		defer cancel()
	}

	result, err := m.invoker.Invoke(invokeCtx, nodeCopy.Address, capability, params)
	if err != nil {
		m.audit(ctx, domain.AuditNodeInvoke, map[string]string{
			"node_id": nodeID, "capability": capability, "error": err.Error(),
		})
		return nil, domain.NewDomainError("Manager.Invoke", domain.ErrNodeInvoke, err.Error())
	}

	m.publishEvent(ctx, domain.EventNodeInvoked, map[string]string{"node_id": nodeID, "capability": capability})
	m.audit(ctx, domain.AuditNodeInvoke, map[string]string{"node_id": nodeID, "capability": capability})
	return result, nil
}

// Discover scans the network for nodes using the configured discoverer.
func (m *Manager) Discover(ctx context.Context) ([]domain.Node, error) {
	nodes, err := m.discoverer.Scan(ctx)
	if err != nil {
		return nil, err
	}
	m.publishEvent(ctx, domain.EventNodeDiscovered, map[string]string{"count": fmt.Sprintf("%d", len(nodes))})
	return nodes, nil
}

// Heartbeat updates the last-seen timestamp for a node.
func (m *Manager) Heartbeat(ctx context.Context, nodeID string) error {
	m.mu.Lock()
	n, ok := m.nodes[nodeID]
	if !ok {
		m.mu.Unlock()
		return domain.NewDomainError("Manager.Heartbeat", domain.ErrNodeNotFound, nodeID)
	}
	n.LastSeen = time.Now()
	n.Status = domain.NodeStatusOnline
	m.mu.Unlock()

	m.publishEvent(ctx, domain.EventNodeHeartbeat, map[string]string{"node_id": nodeID})
	return nil
}

// StartHeartbeatChecker launches a goroutine that marks nodes as unreachable
// if they haven't sent a heartbeat within 2x the heartbeat interval.
func (m *Manager) StartHeartbeatChecker(ctx context.Context) {
	interval := m.config.HeartbeatInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	timeout := interval * 2

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.checkHealth(ctx, timeout)
			}
		}
	}()
}

func (m *Manager) checkHealth(ctx context.Context, timeout time.Duration) {
	cutoff := time.Now().Add(-timeout)

	// Collect unreachable nodes while holding the lock, publish events after releasing.
	var unreachable []string

	m.mu.Lock()
	for id, n := range m.nodes {
		if n.Status == domain.NodeStatusOnline && n.LastSeen.Before(cutoff) {
			n.Status = domain.NodeStatusUnreachable
			unreachable = append(unreachable, id)
		}
	}
	m.mu.Unlock()

	for _, id := range unreachable {
		m.logger.Warn("node unreachable", "node_id", id)
		m.publishEvent(ctx, domain.EventNodeUnreachable, map[string]string{"node_id": id})
	}
}

func (m *Manager) publishEvent(ctx context.Context, eventType domain.EventType, detail map[string]string) {
	if m.bus == nil {
		return
	}
	payload, err := json.Marshal(detail)
	if err != nil {
		m.logger.Error("failed to marshal event payload", "event", string(eventType), "error", err)
		return
	}
	m.bus.Publish(ctx, domain.Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Payload:   payload,
	})
}

func (m *Manager) audit(ctx context.Context, eventType domain.AuditEventType, detail map[string]string) {
	if m.auditLogger == nil {
		return
	}
	_ = m.auditLogger.Log(ctx, domain.AuditEvent{
		Timestamp: time.Now(),
		Type:      eventType,
		Detail:    detail,
	})
}
