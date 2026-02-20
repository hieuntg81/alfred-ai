package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func FuzzLocationTool(f *testing.F) {
	mock := &mockLocationBackend{
		getResp: &LocationResponse{
			Latitude:       48.20849,
			Longitude:      16.37208,
			AccuracyMeters: 12.5,
			AltitudeMeters: 182.0,
			Timestamp:      "2026-01-03T12:34:56.000Z",
			IsPrecise:      true,
			Source:         "gps",
		},
	}
	lt := NewLocationTool(mock, LocationConfig{}, newTestLogger())

	f.Add(`{"action":"get","node_id":"n1"}`)
	f.Add(`{"action":"get","node_id":"n1","desired_accuracy":"precise"}`)
	f.Add(`{"action":"get","node_id":"n1","max_age_ms":60000,"timeout_ms":5000}`)
	f.Add(`{}`)
	f.Add(`{"action":"","node_id":""}`)
	f.Add(`{"action":"get","node_id":"n1","max_age_ms":-1}`)
	f.Add(`{"action":"get","node_id":"n1","timeout_ms":999999}`)
	f.Add(`{"action":"get","node_id":"n1","desired_accuracy":"ultra"}`)

	f.Fuzz(func(t *testing.T, input string) {
		result, err := lt.Execute(context.Background(), json.RawMessage(input))
		if err != nil {
			t.Fatalf("Execute returned error: %v", err)
		}
		if result == nil {
			t.Fatal("Execute returned nil result")
		}

		// Security: empty node_id must never succeed.
		if !result.IsError {
			var p locationParams
			if json.Unmarshal([]byte(input), &p) == nil && p.NodeID == "" {
				t.Error("SECURITY: empty node_id allowed through")
			}
		}
	})
}
