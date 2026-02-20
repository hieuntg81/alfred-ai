//go:build !mdns

package main

import (
	"log/slog"

	"alfred-ai/internal/usecase/node"
)

func buildNodeDiscoverer(_ *slog.Logger) node.NodeDiscoverer {
	return node.NewNoopDiscoverer()
}
