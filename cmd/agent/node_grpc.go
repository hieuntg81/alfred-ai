//go:build grpc_node

package main

import (
	"log/slog"
	"time"

	"alfred-ai/internal/usecase/node"
)

func buildNodeInvoker(timeout time.Duration, logger *slog.Logger) node.NodeInvoker {
	return node.NewGRPCInvoker(timeout, logger)
}
