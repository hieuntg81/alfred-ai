package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// FuzzWebSearchTool fuzzes web search to find query injection and parameter validation bypass.
func FuzzWebSearchTool(f *testing.F) {
	backend := newMockBackend(nil)
	ws := NewWebSearchTool(backend, 0, newTestLogger())

	// Seed corpus
	f.Add(`{"query":"golang tutorial"}`)
	f.Add(`{"query":"","count":5}`)
	f.Add(`{"query":"test","count":0}`)
	f.Add(`{"query":"test","count":-1}`)
	f.Add(`{"query":"test","count":100}`)
	f.Add(`{"query":"test","time_range":"invalid"}`)
	f.Add(`{"query":"test\r\nX-Injected: true"}`)
	f.Add(`{"query":"` + strings.Repeat("A", 10*1024) + `"}`)
	f.Add(`malformed json`)
	f.Add(`{"query":"test","time_range":"day","count":5}`)
	f.Add(`{"query":"   "}`)
	f.Add(`{"query":"\x00test"}`)
	f.Add(`{"query":"test","time_range":""}`)
	f.Add(`{"query":"test","count":20}`)
	f.Add(`{"query":"test","count":21}`)
	f.Add(`{"query":"test","time_range":"week"}`)
	f.Add(`{"query":"test","time_range":"month"}`)
	f.Add(`{"query":"test","time_range":"year"}`)

	f.Fuzz(func(t *testing.T, input string) {
		result, err := ws.Execute(context.Background(), json.RawMessage(input))

		// Invariant 1: Execute must never return a Go error (operational errors go in ToolResult.IsError)
		if err != nil {
			t.Fatalf("Execute returned Go error: %v", err)
		}

		// Invariant 2: Must always return a result
		if result == nil {
			t.Fatal("Execute returned nil result")
		}

		// Invariant 3: If success, query must not be empty
		if !result.IsError {
			var params webSearchParams
			if json.Unmarshal([]byte(input), &params) == nil {
				if strings.TrimSpace(params.Query) == "" {
					t.Errorf("SECURITY: Empty query succeeded")
				}
			}
		}

		// Invariant 4: If success and time_range was set, it must be valid
		if !result.IsError {
			var params webSearchParams
			if json.Unmarshal([]byte(input), &params) == nil {
				if params.TimeRange != "" {
					valid := map[string]bool{"day": true, "week": true, "month": true, "year": true}
					if !valid[params.TimeRange] {
						t.Errorf("SECURITY: Invalid time_range %q accepted", params.TimeRange)
					}
				}
			}
		}
	})
}
