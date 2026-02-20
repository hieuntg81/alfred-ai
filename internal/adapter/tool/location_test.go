package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

// --- mock LocationBackend ---

type mockLocationBackend struct {
	getResp  *LocationResponse
	getErr   error
	getCalls []LocationRequest
}

func (m *mockLocationBackend) GetLocation(_ context.Context, req LocationRequest) (*LocationResponse, error) {
	m.getCalls = append(m.getCalls, req)
	return m.getResp, m.getErr
}

func (m *mockLocationBackend) Name() string { return "mock" }

// --- helpers ---

func newTestLocationTool(backend *mockLocationBackend) *LocationTool {
	return NewLocationTool(backend, LocationConfig{}, newTestLogger())
}

func execLocation(t *testing.T, lt *LocationTool, params any) *domain.ToolResult {
	t.Helper()
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	result, err := lt.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return result
}

// --- interface compliance ---

var _ domain.Tool = (*LocationTool)(nil)

// --- metadata tests ---

func TestLocationToolName(t *testing.T) {
	lt := newTestLocationTool(&mockLocationBackend{})
	if lt.Name() != "location" {
		t.Errorf("Name() = %q, want %q", lt.Name(), "location")
	}
}

func TestLocationToolDescription(t *testing.T) {
	lt := newTestLocationTool(&mockLocationBackend{})
	if lt.Description() == "" {
		t.Error("Description() returned empty string")
	}
}

func TestLocationToolSchema(t *testing.T) {
	lt := newTestLocationTool(&mockLocationBackend{})
	schema := lt.Schema()
	if schema.Name != "location" {
		t.Errorf("Schema.Name = %q, want %q", schema.Name, "location")
	}
	if schema.Parameters == nil {
		t.Error("Schema.Parameters is nil")
	}
	var v any
	if err := json.Unmarshal(schema.Parameters, &v); err != nil {
		t.Errorf("Schema.Parameters is not valid JSON: %v", err)
	}
}

// --- invalid input tests ---

func TestLocationToolInvalidJSON(t *testing.T) {
	lt := newTestLocationTool(&mockLocationBackend{})
	result, err := lt.Execute(context.Background(), json.RawMessage(`invalid`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestLocationToolMissingNodeID(t *testing.T) {
	lt := newTestLocationTool(&mockLocationBackend{})
	result := execLocation(t, lt, map[string]string{"action": "get"})
	if !result.IsError {
		t.Error("expected error for missing node_id")
	}
}

func TestLocationToolUnknownAction(t *testing.T) {
	lt := newTestLocationTool(&mockLocationBackend{})
	result := execLocation(t, lt, map[string]string{"action": "track", "node_id": "n1"})
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

func TestLocationToolInvalidAccuracy(t *testing.T) {
	lt := newTestLocationTool(&mockLocationBackend{})
	result := execLocation(t, lt, map[string]any{
		"action": "get", "node_id": "n1", "desired_accuracy": "ultra",
	})
	if !result.IsError {
		t.Error("expected error for invalid accuracy")
	}
}

func TestLocationToolMaxAgeBounds(t *testing.T) {
	lt := newTestLocationTool(&mockLocationBackend{})
	result := execLocation(t, lt, map[string]any{
		"action": "get", "node_id": "n1", "max_age_ms": 999999,
	})
	if !result.IsError {
		t.Error("expected error for max_age_ms out of bounds")
	}
}

func TestLocationToolTimeoutMsBounds(t *testing.T) {
	lt := newTestLocationTool(&mockLocationBackend{})
	result := execLocation(t, lt, map[string]any{
		"action": "get", "node_id": "n1", "timeout_ms": 999999,
	})
	if !result.IsError {
		t.Error("expected error for timeout_ms out of bounds")
	}
}

func TestLocationToolNegativeMaxAge(t *testing.T) {
	lt := newTestLocationTool(&mockLocationBackend{})
	result := execLocation(t, lt, map[string]any{
		"action": "get", "node_id": "n1", "max_age_ms": -1,
	})
	if !result.IsError {
		t.Error("expected error for negative max_age_ms")
	}
}

func TestLocationToolNegativeTimeout(t *testing.T) {
	lt := newTestLocationTool(&mockLocationBackend{})
	result := execLocation(t, lt, map[string]any{
		"action": "get", "node_id": "n1", "timeout_ms": -1,
	})
	if !result.IsError {
		t.Error("expected error for negative timeout_ms")
	}
}

// --- success paths ---

func TestLocationGetSuccess(t *testing.T) {
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
	lt := newTestLocationTool(mock)
	result := execLocation(t, lt, map[string]any{
		"action": "get", "node_id": "n1", "desired_accuracy": "precise",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if len(mock.getCalls) != 1 {
		t.Fatalf("expected 1 get call, got %d", len(mock.getCalls))
	}
	if mock.getCalls[0].DesiredAccuracy != "precise" {
		t.Errorf("accuracy = %q, want %q", mock.getCalls[0].DesiredAccuracy, "precise")
	}
}

func TestLocationGetWithDefaultAccuracy(t *testing.T) {
	mock := &mockLocationBackend{
		getResp: &LocationResponse{
			Latitude: 48.20849, Longitude: 16.37208, Timestamp: "2026-01-03T12:34:56.000Z",
		},
	}
	lt := newTestLocationTool(mock)
	result := execLocation(t, lt, map[string]any{
		"action": "get", "node_id": "n1",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if mock.getCalls[0].DesiredAccuracy != "balanced" {
		t.Errorf("accuracy = %q, want default %q", mock.getCalls[0].DesiredAccuracy, "balanced")
	}
}

func TestLocationGetWithCustomConfig(t *testing.T) {
	mock := &mockLocationBackend{
		getResp: &LocationResponse{
			Latitude: 10.0, Longitude: 20.0, Timestamp: "2026-01-03T12:34:56.000Z",
		},
	}
	lt := NewLocationTool(mock, LocationConfig{
		Timeout:         20 * time.Second,
		DefaultAccuracy: "coarse",
	}, newTestLogger())
	result := execLocation(t, lt, map[string]any{
		"action": "get", "node_id": "n1",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if mock.getCalls[0].DesiredAccuracy != "coarse" {
		t.Errorf("accuracy = %q, want %q", mock.getCalls[0].DesiredAccuracy, "coarse")
	}
}

// --- backend error paths ---

func TestLocationGetBackendError(t *testing.T) {
	mock := &mockLocationBackend{getErr: fmt.Errorf("location disabled")}
	lt := newTestLocationTool(mock)
	result := execLocation(t, lt, map[string]any{
		"action": "get", "node_id": "n1",
	})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

// --- config defaults ---

func TestLocationToolDefaultConfig(t *testing.T) {
	lt := NewLocationTool(&mockLocationBackend{}, LocationConfig{}, newTestLogger())
	if lt.config.Timeout != defaultLocationTimeout {
		t.Errorf("Timeout = %v, want %v", lt.config.Timeout, defaultLocationTimeout)
	}
	if lt.config.DefaultAccuracy != defaultLocationDesiredAccuracy {
		t.Errorf("DefaultAccuracy = %q, want %q", lt.config.DefaultAccuracy, defaultLocationDesiredAccuracy)
	}
}

// --- coordinate validation tests (NodeLocationBackend) ---

func TestLocationNodeBackendCoordinateValidation(t *testing.T) {
	tests := []struct {
		name    string
		resp    LocationResponse
		wantErr bool
	}{
		{
			name:    "NaN latitude",
			resp:    LocationResponse{Latitude: math.NaN(), Longitude: 16.0, Timestamp: "t"},
			wantErr: true,
		},
		{
			name:    "NaN longitude",
			resp:    LocationResponse{Latitude: 48.0, Longitude: math.NaN(), Timestamp: "t"},
			wantErr: true,
		},
		{
			name:    "Inf latitude",
			resp:    LocationResponse{Latitude: math.Inf(1), Longitude: 16.0, Timestamp: "t"},
			wantErr: true,
		},
		{
			name:    "negative Inf longitude",
			resp:    LocationResponse{Latitude: 48.0, Longitude: math.Inf(-1), Timestamp: "t"},
			wantErr: true,
		},
		{
			name:    "latitude out of range high",
			resp:    LocationResponse{Latitude: 91.0, Longitude: 16.0, Timestamp: "t"},
			wantErr: true,
		},
		{
			name:    "latitude out of range low",
			resp:    LocationResponse{Latitude: -91.0, Longitude: 16.0, Timestamp: "t"},
			wantErr: true,
		},
		{
			name:    "longitude out of range high",
			resp:    LocationResponse{Latitude: 48.0, Longitude: 181.0, Timestamp: "t"},
			wantErr: true,
		},
		{
			name:    "longitude out of range low",
			resp:    LocationResponse{Latitude: 48.0, Longitude: -181.0, Timestamp: "t"},
			wantErr: true,
		},
		{
			name:    "valid coordinates",
			resp:    LocationResponse{Latitude: 48.20849, Longitude: 16.37208, Timestamp: "t"},
			wantErr: false,
		},
		{
			name:    "boundary -90 latitude",
			resp:    LocationResponse{Latitude: -90.0, Longitude: 0.0, Timestamp: "t"},
			wantErr: false,
		},
		{
			name:    "boundary 180 longitude",
			resp:    LocationResponse{Latitude: 0.0, Longitude: 180.0, Timestamp: "t"},
			wantErr: false,
		},
		{
			name:    "empty timestamp",
			resp:    LocationResponse{Latitude: 48.0, Longitude: 16.0, Timestamp: ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			respJSON, _ := json.Marshal(tt.resp)
			mgr := &mockNodeManager{invokeRes: respJSON}
			backend := NewNodeLocationBackend(mgr)

			_, err := backend.GetLocation(t.Context(), LocationRequest{
				NodeID:          "n1",
				DesiredAccuracy: "balanced",
			})
			if (err != nil) != tt.wantErr {
				t.Errorf("GetLocation() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNodeLocationBackendConditionalMarshal(t *testing.T) {
	respJSON, _ := json.Marshal(LocationResponse{
		Latitude: 48.0, Longitude: 16.0, Timestamp: "2026-01-03T12:00:00Z",
	})
	mgr := &mockNodeManager{invokeRes: respJSON}
	backend := NewNodeLocationBackend(mgr)

	// Call with zero-value optional fields — they should not be sent to the node
	_, err := backend.GetLocation(t.Context(), LocationRequest{
		NodeID:          "n1",
		DesiredAccuracy: "balanced",
		// MaxAgeMs=0, TimeoutMs=0 should be omitted
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
