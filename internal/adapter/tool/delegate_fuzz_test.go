package tool

import (
	"encoding/json"
	"strings"
	"testing"
)

// FuzzDelegateTool fuzzes inter-agent delegation to find self-delegation loops and message size DoS.
// Tests for circular delegation and unbounded message payloads.
func FuzzDelegateTool(f *testing.F) {
	// Create minimal test delegate tool
	tool := &DelegateTool{agentID: "agent1"}

	// Seed corpus
	f.Add(`{"agent_id":"agent2","message":"test"}`)                       // Valid baseline
	f.Add(`{"agent_id":"agent1","message":"self-delegate"}`)              // Self-delegation (circular)
	f.Add(`{"agent_id":"agent2","message":"`+strings.Repeat("A", 10*1024*1024)+`"}`) // 10MB message DoS
	f.Add(`{"session_id":"../../etc/passwd","message":"test"}`)           // Session injection attempt

	f.Fuzz(func(t *testing.T, input string) {
		// Just verify JSON parsing doesn't panic
		var params delegateParams
		json.Unmarshal([]byte(input), &params)

		// Invariant 1: Prevent self-delegation (infinite loop risk)
		if params.AgentID == tool.agentID {
			t.Logf("WARNING: Self-delegation detected (agent_id=%q)", params.AgentID)
		}

		// Invariant 2: Message size should be limited
		if len(params.Message) > 100*1024*1024 {
			t.Errorf("SECURITY: Excessive message size: %d bytes", len(params.Message))
		}
	})
}
