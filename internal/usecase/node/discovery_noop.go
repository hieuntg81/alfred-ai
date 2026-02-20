package node

import (
	"context"

	"alfred-ai/internal/domain"
)

// NoopDiscoverer is a placeholder discoverer used when mDNS support is not compiled in.
type NoopDiscoverer struct{}

// NewNoopDiscoverer creates a NoopDiscoverer.
func NewNoopDiscoverer() *NoopDiscoverer { return &NoopDiscoverer{} }

// Scan returns nil — no discovery available without the mdns build tag.
func (n *NoopDiscoverer) Scan(_ context.Context) ([]domain.Node, error) {
	return nil, nil
}
