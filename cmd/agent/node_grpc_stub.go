//go:build !grpc_node

package main

import (
	"log/slog"
	"time"

	"alfred-ai/internal/usecase/node"
)

func buildNodeInvoker(_ time.Duration, _ *slog.Logger) node.NodeInvoker {
	return node.NewNoopInvoker()
}
