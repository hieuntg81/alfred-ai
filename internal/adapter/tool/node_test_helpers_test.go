package tool

import (
	"context"
	"encoding/json"

	"alfred-ai/internal/domain"
)

// mockNodeManager implements domain.NodeManager for testing.
type mockNodeManager struct {
	nodes     []domain.Node
	listErr   error
	getNode   *domain.Node
	getErr    error
	invokeRes json.RawMessage
	invokeErr error
	discNodes []domain.Node
	discErr   error
}

func (m *mockNodeManager) Register(_ context.Context, _ domain.Node) error { return nil }
func (m *mockNodeManager) Unregister(_ context.Context, _ string) error    { return nil }
func (m *mockNodeManager) Heartbeat(_ context.Context, _ string) error     { return nil }

func (m *mockNodeManager) List(_ context.Context) ([]domain.Node, error) {
	return m.nodes, m.listErr
}

func (m *mockNodeManager) Get(_ context.Context, _ string) (*domain.Node, error) {
	return m.getNode, m.getErr
}

func (m *mockNodeManager) Invoke(_ context.Context, _, _ string, _ json.RawMessage) (json.RawMessage, error) {
	return m.invokeRes, m.invokeErr
}

func (m *mockNodeManager) Discover(_ context.Context) ([]domain.Node, error) {
	return m.discNodes, m.discErr
}
