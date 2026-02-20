package node

import (
	"context"
	"encoding/json"
	"fmt"
)

// NoopInvoker is a placeholder invoker used when gRPC support is not compiled in.
type NoopInvoker struct{}

// NewNoopInvoker creates a NoopInvoker.
func NewNoopInvoker() *NoopInvoker { return &NoopInvoker{} }

// Invoke always returns an error indicating that gRPC support is not available.
func (n *NoopInvoker) Invoke(_ context.Context, _, _ string, _ json.RawMessage) (json.RawMessage, error) {
	return nil, fmt.Errorf("node invocation unavailable: build with -tags grpc_node to enable gRPC transport")
}
