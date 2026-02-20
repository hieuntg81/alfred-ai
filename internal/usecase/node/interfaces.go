package node

import (
	"context"
	"encoding/json"

	"alfred-ai/internal/domain"
)

// NodeInvoker sends an invocation request to a remote node.
type NodeInvoker interface {
	Invoke(ctx context.Context, address, capability string, params json.RawMessage) (json.RawMessage, error)
}

// NodeDiscoverer scans the network for available nodes.
type NodeDiscoverer interface {
	Scan(ctx context.Context) ([]domain.Node, error)
}
