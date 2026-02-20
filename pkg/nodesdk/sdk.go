// Package nodesdk provides a client SDK for building alfred-ai node agents.
//
// A node agent registers capabilities that can be invoked remotely by
// alfred-ai's agent system. The SDK is transport-agnostic — it handles
// capability registration and invocation dispatch without depending on
// any specific transport (gRPC, HTTP, etc.).
//
// Example:
//
//	agent := nodesdk.New("my-node", "My IoT Device",
//	    nodesdk.WithPlatform("linux/arm64"),
//	    nodesdk.WithServer("bot.example.com:9090"),
//	)
//	agent.RegisterCapability("read_sensor", "Read temperature sensor", nil,
//	    func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
//	        return json.Marshal(map[string]float64{"temp": 23.5})
//	    },
//	)
//	result, err := agent.HandleInvocation(ctx, "read_sensor", nil)
package nodesdk

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
)

// CapabilityHandler processes an invocation of a capability.
type CapabilityHandler func(ctx context.Context, params json.RawMessage) (json.RawMessage, error)

// Capability describes a single capability provided by a node.
type Capability struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	handler     CapabilityHandler
}

// NodeAgent represents a alfred-ai node that provides capabilities.
type NodeAgent struct {
	mu           sync.RWMutex
	id           string
	name         string
	platform     string
	serverAddr   string
	deviceToken  string
	listenPort   int
	capabilities map[string]*Capability
	logger       *slog.Logger
}

// New creates a new NodeAgent with the given ID and name.
func New(id, name string, opts ...Option) *NodeAgent {
	n := &NodeAgent{
		id:           id,
		name:         name,
		platform:     "unknown",
		capabilities: make(map[string]*Capability),
		logger:       slog.Default(),
	}
	for _, opt := range opts {
		opt(n)
	}
	return n
}

// ID returns the node's identifier.
func (n *NodeAgent) ID() string { return n.id }

// Name returns the node's name.
func (n *NodeAgent) Name() string { return n.name }

// RegisterCapability adds a capability to the node.
func (n *NodeAgent) RegisterCapability(name, description string, parameters json.RawMessage, handler CapabilityHandler) {
	n.mu.Lock()
	n.capabilities[name] = &Capability{
		Name:        name,
		Description: description,
		Parameters:  parameters,
		handler:     handler,
	}
	n.mu.Unlock()
	n.logger.Debug("capability registered", "name", name)
}

// HandleInvocation dispatches a capability invocation to the registered handler.
func (n *NodeAgent) HandleInvocation(ctx context.Context, capability string, params json.RawMessage) (json.RawMessage, error) {
	n.mu.RLock()
	cap, ok := n.capabilities[capability]
	n.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("capability %q not registered", capability)
	}
	return cap.handler(ctx, params)
}

// Capabilities returns all registered capabilities.
func (n *NodeAgent) Capabilities() []Capability {
	n.mu.RLock()
	defer n.mu.RUnlock()

	caps := make([]Capability, 0, len(n.capabilities))
	for _, c := range n.capabilities {
		caps = append(caps, Capability{
			Name:        c.Name,
			Description: c.Description,
			Parameters:  c.Parameters,
		})
	}
	return caps
}

// Start connects to the alfred-ai server and registers the node.
// This is a placeholder — actual transport must be provided by the caller.
func (n *NodeAgent) Start(_ context.Context) error {
	return fmt.Errorf("no transport configured: use gRPC or HTTP adapter to connect to server at %s", n.serverAddr)
}

// Stop disconnects from the server.
func (n *NodeAgent) Stop() error {
	return nil
}
