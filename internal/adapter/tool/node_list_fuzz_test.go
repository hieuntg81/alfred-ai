package tool

import (
	"context"
	"encoding/json"
	"testing"
)

// FuzzNodeListTool fuzzes node list tool to ensure it never panics on any input.
func FuzzNodeListTool(f *testing.F) {
	mgr := &mockNodeManager{}
	tool := NewNodeListTool(mgr)

	// Seed corpus
	f.Add(`{}`)
	f.Add(`null`)
	f.Add(`{"unexpected":"field"}`)
	f.Add(`invalid json`)
	f.Add(``)
	f.Add(`[]`)

	f.Fuzz(func(t *testing.T, input string) {
		result, err := tool.Execute(context.Background(), json.RawMessage(input))

		// Invariant 1: Execute must never return a Go error
		if err != nil {
			t.Fatalf("Execute returned Go error: %v", err)
		}

		// Invariant 2: Must always return a result
		if result == nil {
			t.Fatal("Execute returned nil result")
		}
	})
}
