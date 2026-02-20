package domain

import (
	"context"
	"encoding/json"
	"time"
)

// NodeStatus represents the current state of a remote node.
type NodeStatus string

const (
	NodeStatusOnline      NodeStatus = "online"
	NodeStatusOffline     NodeStatus = "offline"
	NodeStatusUnreachable NodeStatus = "unreachable"
)

// Node represents a remote device registered with alfred-ai.
type Node struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Platform     string            `json:"platform"`
	Address      string            `json:"address"`
	Capabilities []NodeCapability  `json:"capabilities"`
	Status       NodeStatus        `json:"status"`
	LastSeen     time.Time         `json:"last_seen"`
	DeviceToken  string            `json:"-"` // never serialized
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// NodeCapability describes a single capability that a node can perform.
type NodeCapability struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// NodeManager provides operations for managing remote nodes.
type NodeManager interface {
	Register(ctx context.Context, node Node) error
	Unregister(ctx context.Context, nodeID string) error
	List(ctx context.Context) ([]Node, error)
	Get(ctx context.Context, nodeID string) (*Node, error)
	Invoke(ctx context.Context, nodeID, capability string, params json.RawMessage) (json.RawMessage, error)
	Discover(ctx context.Context) ([]Node, error)
	Heartbeat(ctx context.Context, nodeID string) error
}

// NodeTokenManager handles authentication token lifecycle for nodes.
type NodeTokenManager interface {
	GenerateToken(nodeID string) (string, error)
	RevokeToken(nodeID string)
}
