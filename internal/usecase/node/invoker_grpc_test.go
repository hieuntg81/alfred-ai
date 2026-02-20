//go:build grpc_node

package node

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	pb "alfred-ai/internal/usecase/node/proto"
	"google.golang.org/grpc"
)

// mockNodeService implements pb.NodeServiceServer for testing.
type mockNodeService struct {
	pb.UnimplementedNodeServiceServer
	result []byte
	err    string
}

func (m *mockNodeService) Execute(_ context.Context, req *pb.ExecuteRequest) (*pb.ExecuteResponse, error) {
	return &pb.ExecuteResponse{Result: m.result, Error: m.err}, nil
}

func startTestGRPCServer(t *testing.T, svc pb.NodeServiceServer) (string, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterNodeServiceServer(s, svc)

	go s.Serve(lis)

	return lis.Addr().String(), func() {
		s.GracefulStop()
		lis.Close()
	}
}

func grpcTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestGRPCInvokerSuccess(t *testing.T) {
	svc := &mockNodeService{result: []byte(`{"status":"ok"}`)}
	addr, cleanup := startTestGRPCServer(t, svc)
	defer cleanup()

	inv := NewGRPCInvoker(5*time.Second, grpcTestLogger())
	result, err := inv.Invoke(context.Background(), addr, "test_cap", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if string(result) != `{"status":"ok"}` {
		t.Errorf("result = %s", result)
	}
}

func TestGRPCInvokerServerError(t *testing.T) {
	svc := &mockNodeService{err: "something went wrong"}
	addr, cleanup := startTestGRPCServer(t, svc)
	defer cleanup()

	inv := NewGRPCInvoker(5*time.Second, grpcTestLogger())
	_, err := inv.Invoke(context.Background(), addr, "cap", nil)
	if err == nil {
		t.Fatal("expected error from server")
	}
}

func TestGRPCInvokerConnectionError(t *testing.T) {
	inv := NewGRPCInvoker(500*time.Millisecond, grpcTestLogger())
	_, err := inv.Invoke(context.Background(), "127.0.0.1:1", "cap", nil)
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestGRPCInvokerTimeout(t *testing.T) {
	inv := NewGRPCInvoker(100*time.Millisecond, grpcTestLogger())
	defer inv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := inv.Invoke(ctx, "127.0.0.1:1", "cap", nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestGRPCInvokerConnectionCaching(t *testing.T) {
	svc := &mockNodeService{result: []byte(`{"ok":true}`)}
	addr, cleanup := startTestGRPCServer(t, svc)
	defer cleanup()

	inv := NewGRPCInvoker(5*time.Second, grpcTestLogger())
	defer inv.Close()

	// First call creates connection.
	_, err := inv.Invoke(context.Background(), addr, "cap1", nil)
	if err != nil {
		t.Fatalf("first Invoke: %v", err)
	}

	// Second call reuses cached connection.
	_, err = inv.Invoke(context.Background(), addr, "cap2", nil)
	if err != nil {
		t.Fatalf("second Invoke: %v", err)
	}

	inv.mu.Lock()
	numConns := len(inv.conns)
	inv.mu.Unlock()
	if numConns != 1 {
		t.Errorf("expected 1 cached connection, got %d", numConns)
	}
}

func TestGRPCInvokerClose(t *testing.T) {
	svc := &mockNodeService{result: []byte(`{}`)}
	addr, cleanup := startTestGRPCServer(t, svc)
	defer cleanup()

	inv := NewGRPCInvoker(5*time.Second, grpcTestLogger())

	_, _ = inv.Invoke(context.Background(), addr, "cap", nil)
	inv.Close()

	inv.mu.Lock()
	numConns := len(inv.conns)
	inv.mu.Unlock()
	if numConns != 0 {
		t.Errorf("expected 0 connections after Close, got %d", numConns)
	}
}
