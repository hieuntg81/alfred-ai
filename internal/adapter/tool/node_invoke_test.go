package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"alfred-ai/internal/domain"
)

func TestNodeInvokeToolName(t *testing.T) {
	tool := NewNodeInvokeTool(&mockNodeManager{})
	if tool.Name() != "node_invoke" {
		t.Errorf("Name() = %q", tool.Name())
	}
}

func TestNodeInvokeToolSchema(t *testing.T) {
	tool := NewNodeInvokeTool(&mockNodeManager{})
	s := tool.Schema()
	if s.Name != "node_invoke" {
		t.Errorf("Schema.Name = %q", s.Name)
	}
}

func TestNodeInvokeSuccess(t *testing.T) {
	mgr := &mockNodeManager{invokeRes: json.RawMessage(`{"output":"hello"}`)}
	tool := NewNodeInvokeTool(mgr)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"node_id":"n1","capability":"run_cmd"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error result: %s", result.Content)
	}
	if result.Content != `{"output":"hello"}` {
		t.Errorf("Content = %s", result.Content)
	}
}

func TestNodeInvokeManagerError(t *testing.T) {
	mgr := &mockNodeManager{invokeErr: domain.NewDomainError("test", domain.ErrNodeNotFound, "n1")}
	tool := NewNodeInvokeTool(mgr)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"node_id":"n1","capability":"cap"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result")
	}
}

func TestNodeInvokeInvalidParams(t *testing.T) {
	tool := NewNodeInvokeTool(&mockNodeManager{})
	result, err := tool.Execute(context.Background(), json.RawMessage(`{invalid`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for invalid params")
	}
}

func TestNodeInvokeMissingFields(t *testing.T) {
	tool := NewNodeInvokeTool(&mockNodeManager{})

	tests := []struct {
		name    string
		payload string
	}{
		{"empty node_id", `{"node_id":"","capability":"cap"}`},
		{"empty capability", `{"node_id":"n1","capability":""}`},
		{"both empty", `{"node_id":"","capability":""}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), json.RawMessage(tc.payload))
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !result.IsError {
				t.Error("expected error result for missing fields")
			}
		})
	}
}

func TestNodeInvokeInterface(t *testing.T) {
	var _ domain.Tool = (*NodeInvokeTool)(nil)
}

func TestNodeInvokeWithParams(t *testing.T) {
	mgr := &mockNodeManager{invokeRes: json.RawMessage(`{"ok":true}`)}
	tool := NewNodeInvokeTool(mgr)

	params := fmt.Sprintf(`{"node_id":"n1","capability":"exec","params":{"cmd":"ls"}}`)
	result, err := tool.Execute(context.Background(), json.RawMessage(params))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}
}
