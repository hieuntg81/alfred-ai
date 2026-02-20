package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"alfred-ai/internal/domain"
)

func TestNodeListToolName(t *testing.T) {
	tool := NewNodeListTool(&mockNodeManager{})
	if tool.Name() != "node_list" {
		t.Errorf("Name() = %q", tool.Name())
	}
}

func TestNodeListToolDescription(t *testing.T) {
	tool := NewNodeListTool(&mockNodeManager{})
	if tool.Description() == "" {
		t.Error("Description() returned empty string")
	}
}

func TestNodeListToolSchema(t *testing.T) {
	tool := NewNodeListTool(&mockNodeManager{})
	schema := tool.Schema()
	if schema.Name != "node_list" {
		t.Errorf("Schema.Name = %q, want %q", schema.Name, "node_list")
	}
	if schema.Parameters == nil {
		t.Error("Schema.Parameters is nil")
	}
	// Verify parameters are valid JSON
	var params map[string]interface{}
	if err := json.Unmarshal(schema.Parameters, &params); err != nil {
		t.Errorf("Schema.Parameters is invalid JSON: %v", err)
	}
}

func TestNodeListToolListError(t *testing.T) {
	mgr := &mockNodeManager{
		listErr: fmt.Errorf("connection lost"),
	}
	tool := NewNodeListTool(mgr)

	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true when List() fails")
	}
	if result.Content == "" {
		t.Error("expected non-empty error content")
	}
}

func TestNodeListSuccess(t *testing.T) {
	mgr := &mockNodeManager{
		nodes: []domain.Node{
			{ID: "n1", Name: "Node 1", Status: domain.NodeStatusOnline},
			{ID: "n2", Name: "Node 2", Status: domain.NodeStatusOffline},
		},
	}
	tool := NewNodeListTool(mgr)

	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}

	var nodes []domain.Node
	if err := json.Unmarshal([]byte(result.Content), &nodes); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("nodes len = %d, want 2", len(nodes))
	}
}

func TestNodeListEmpty(t *testing.T) {
	tool := NewNodeListTool(&mockNodeManager{})
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}
}

func TestNodeListInterface(t *testing.T) {
	var _ domain.Tool = (*NodeListTool)(nil)
}
