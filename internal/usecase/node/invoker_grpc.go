//go:build grpc_node

package node

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	pb "alfred-ai/internal/usecase/node/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// GRPCInvoker invokes capabilities on remote nodes via gRPC.
// Connections are cached per address and reused across invocations.
type GRPCInvoker struct {
	timeout time.Duration
	logger  *slog.Logger
	mu      sync.Mutex
	conns   map[string]*grpc.ClientConn
}

// NewGRPCInvoker creates a new GRPCInvoker.
func NewGRPCInvoker(timeout time.Duration, logger *slog.Logger) *GRPCInvoker {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &GRPCInvoker{
		timeout: timeout,
		logger:  logger,
		conns:   make(map[string]*grpc.ClientConn),
	}
}

// getConn returns a cached connection or creates a new one.
func (g *GRPCInvoker) getConn(address string) (*grpc.ClientConn, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if conn, ok := g.conns[address]; ok {
		return conn, nil
	}

	conn, err := grpc.NewClient(address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.CallContentSubtype("json")),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc connect %s: %w", address, err)
	}
	g.conns[address] = conn
	return conn, nil
}

// Invoke calls Execute RPC on the node at address, reusing cached connections.
func (g *GRPCInvoker) Invoke(ctx context.Context, address, capability string, params json.RawMessage) (json.RawMessage, error) {
	conn, err := g.getConn(address)
	if err != nil {
		return nil, err
	}

	callCtx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	client := pb.NewNodeServiceClient(conn)
	resp, err := client.Execute(callCtx, &pb.ExecuteRequest{
		Capability: capability,
		Params:     []byte(params),
	})
	if err != nil {
		// On connection error, remove the cached connection so next call retries.
		g.mu.Lock()
		if g.conns[address] == conn {
			delete(g.conns, address)
			_ = conn.Close()
		}
		g.mu.Unlock()
		return nil, fmt.Errorf("grpc execute on %s: %w", address, err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("node error: %s", resp.Error)
	}

	g.logger.Debug("grpc invocation complete", "address", address, "capability", capability)
	return json.RawMessage(resp.Result), nil
}

// Close closes all cached connections.
func (g *GRPCInvoker) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for addr, conn := range g.conns {
		_ = conn.Close()
		delete(g.conns, addr)
	}
}
