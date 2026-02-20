package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"alfred-ai/internal/usecase"
)

// FuzzSubAgentTool fuzzes sub-agent task queue to find array overflow and size DoS.
// Tests that the tool properly rejects unbounded task arrays and individual task size limits.
func FuzzSubAgentTool(f *testing.F) {
	// Create minimal test manager
	mockMgr := &usecase.SubAgentManager{} // Will fail execution but that's ok, we test validation

	tool := NewSubAgentTool(mockMgr)

	// Seed corpus
	f.Add(`{"tasks":["task1","task2"]}`)                             // Valid baseline
	f.Add(`{"tasks":[]}`)                                            // Empty array
	f.Add(`{"tasks":["` + strings.Repeat("A", 10*1024*1024) + `"]}`) // Single huge task
	f.Add(`{"tasks":[` + strings.Repeat(`"task",`, 10000) + `"x"]}`) // 10001 tasks (array overflow)

	f.Fuzz(func(t *testing.T, input string) {
		result, _ := tool.Execute(context.Background(), json.RawMessage(input))

		// Parse params to check what was in the input
		var params struct{ Tasks []string }
		if json.Unmarshal([]byte(input), &params) != nil {
			return // Invalid JSON, skip
		}

		// Invariant 1: Tool MUST reject excessive task counts (>1000)
		if len(params.Tasks) > 1000 {
			if result != nil && !result.IsError {
				t.Errorf("SECURITY: Tool accepted excessive task count: %d", len(params.Tasks))
			}
		}

		// Invariant 2: Tool MUST reject excessive individual task sizes (>10MB)
		for i, task := range params.Tasks {
			if len(task) > 10*1024*1024 {
				if result != nil && !result.IsError {
					t.Errorf("SECURITY: Tool accepted excessive task size at index %d: %d bytes", i, len(task))
				}
				break
			}
		}
	})
}
