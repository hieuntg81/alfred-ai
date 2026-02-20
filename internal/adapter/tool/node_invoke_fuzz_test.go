package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// FuzzNodeInvokeTool fuzzes node RPC invocations to find validation gaps and type confusion.
// Tests for empty IDs, excessive payload sizes, and parameter structure abuse.
func FuzzNodeInvokeTool(f *testing.F) {
	// Mock node manager for fuzzing
	mockMgr := &mockNodeManager{
		invokeRes: json.RawMessage(`{"result":"ok"}`),
	}
	tool := NewNodeInvokeTool(mockMgr)

	// Seed corpus
	f.Add(`{"node_id":"node1","capability":"exec"}`)                      // Valid baseline
	f.Add(`{"node_id":"","capability":"test"}`)                           // Empty node_id
	f.Add(`{"node_id":"node1","capability":"","params":{}}`)              // Empty capability
	f.Add(`{"node_id":"node1","capability":"exec","params":`+strings.Repeat(`{"a":"b",`, 5000)+`"x":"y"`+strings.Repeat(`}`, 5000)+`}`) // Deeply nested
	f.Add(`{"params":{"__proto__":{"isAdmin":true}}}`)                   // Prototype pollution attempt

	f.Fuzz(func(t *testing.T, input string) {
		result, _ := tool.Execute(context.Background(), json.RawMessage(input))

		if result != nil && !result.IsError {
			var params nodeInvokeParams
			if json.Unmarshal([]byte(input), &params) == nil {
				// Invariant 1: node_id and capability are required
				if params.NodeID == "" || params.Capability == "" {
					t.Errorf("SECURITY: Empty node_id or capability allowed")
				}

				// Invariant 2: Params size should be limited (DoS protection)
				if len(params.Params) > 1024*1024 {
					t.Errorf("SECURITY: Excessive params size: %d bytes", len(params.Params))
				}
			}
		}
	})
}
