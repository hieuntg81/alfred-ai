package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func FuzzCameraTool(f *testing.F) {
	f.Add(`{"action":"snap","node_id":"n1"}`)
	f.Add(`{"action":"clip","node_id":"n1","duration_ms":5000}`)
	f.Add(`{"action":"list_devices","node_id":"n1"}`)
	f.Add(`{"action":"screen_record","node_id":"n1","duration_ms":10000}`)
	f.Add(`{"action":"","node_id":""}`)
	f.Add(`{}`)
	f.Add(`{"action":"snap","node_id":"n1","quality":50,"max_width":1280,"facing":"front"}`)
	f.Add(`{"action":"snap","node_id":"n1","quality":-1}`)
	f.Add(`{"action":"clip","node_id":"n1","duration_ms":999999}`)
	f.Add(`{"action":"screen_record","node_id":"n1","duration_ms":5000,"screen_index":-1}`)

	f.Fuzz(func(t *testing.T, input string) {
		// Create fresh mock per iteration to avoid unbounded slice growth
		// in call-tracking fields (snapCalls, clipCalls, etc.).
		mock := &mockCameraBackend{
			snapResp: &CameraSnapResponse{
				FilePath: "/tmp/test.jpg", Format: "jpeg", Width: 100, Height: 100, SizeBytes: 1000,
			},
			clipResp: &CameraClipResponse{
				FilePath: "/tmp/test.mp4", Format: "mp4", DurationMs: 5000, SizeBytes: 50000,
			},
			listResp: []CameraDevice{
				{DeviceID: "cam0", Label: "Front", Facing: "front"},
			},
			screenResp: &ScreenRecordResponse{
				FilePath: "/tmp/screen.mp4", Format: "mp4", DurationMs: 5000, SizeBytes: 50000,
			},
		}
		ct := NewCameraTool(mock, CameraConfig{}, newTestLogger())

		result, err := ct.Execute(context.Background(), json.RawMessage(input))
		if err != nil {
			t.Fatalf("Execute returned error: %v", err)
		}
		if result == nil {
			t.Fatal("Execute returned nil result")
		}

		// Security: empty node_id must never succeed.
		if !result.IsError {
			var p cameraParams
			if json.Unmarshal([]byte(input), &p) == nil && p.NodeID == "" {
				t.Error("SECURITY: empty node_id allowed through")
			}
		}
	})
}
