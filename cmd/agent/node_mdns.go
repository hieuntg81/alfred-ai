//go:build mdns

package main

import (
	"log/slog"

	"alfred-ai/internal/usecase/node"
)

func buildNodeDiscoverer(logger *slog.Logger) node.NodeDiscoverer {
	return node.NewMDNSDiscoverer(logger)
}
