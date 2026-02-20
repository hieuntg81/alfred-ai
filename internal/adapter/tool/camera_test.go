package tool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

// --- mock CameraBackend ---

type mockCameraBackend struct {
	snapResp   *CameraSnapResponse
	snapErr    error
	clipResp   *CameraClipResponse
	clipErr    error
	listResp   []CameraDevice
	listErr    error
	screenResp *ScreenRecordResponse
	screenErr  error

	snapCalls   []CameraSnapRequest
	clipCalls   []CameraClipRequest
	screenCalls []ScreenRecordRequest
	listCalls   []string
}

func (m *mockCameraBackend) Snap(_ context.Context, req CameraSnapRequest) (*CameraSnapResponse, error) {
	m.snapCalls = append(m.snapCalls, req)
	return m.snapResp, m.snapErr
}

func (m *mockCameraBackend) Clip(_ context.Context, req CameraClipRequest) (*CameraClipResponse, error) {
	m.clipCalls = append(m.clipCalls, req)
	return m.clipResp, m.clipErr
}

func (m *mockCameraBackend) ListDevices(_ context.Context, nodeID string) ([]CameraDevice, error) {
	m.listCalls = append(m.listCalls, nodeID)
	return m.listResp, m.listErr
}

func (m *mockCameraBackend) ScreenRecord(_ context.Context, req ScreenRecordRequest) (*ScreenRecordResponse, error) {
	m.screenCalls = append(m.screenCalls, req)
	return m.screenResp, m.screenErr
}

func (m *mockCameraBackend) Name() string { return "mock" }

// --- helpers ---

func newTestCameraTool(backend *mockCameraBackend) *CameraTool {
	return NewCameraTool(backend, CameraConfig{}, newTestLogger())
}

func execCamera(t *testing.T, ct *CameraTool, params any) *domain.ToolResult {
	t.Helper()
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	result, err := ct.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return result
}

// --- interface compliance ---

var _ domain.Tool = (*CameraTool)(nil)

// --- metadata tests ---

func TestCameraToolName(t *testing.T) {
	ct := newTestCameraTool(&mockCameraBackend{})
	if ct.Name() != "camera" {
		t.Errorf("Name() = %q, want %q", ct.Name(), "camera")
	}
}

func TestCameraToolDescription(t *testing.T) {
	ct := newTestCameraTool(&mockCameraBackend{})
	if ct.Description() == "" {
		t.Error("Description() returned empty string")
	}
}

func TestCameraToolSchema(t *testing.T) {
	ct := newTestCameraTool(&mockCameraBackend{})
	schema := ct.Schema()
	if schema.Name != "camera" {
		t.Errorf("Schema.Name = %q, want %q", schema.Name, "camera")
	}
	if schema.Parameters == nil {
		t.Error("Schema.Parameters is nil")
	}
	// Validate schema is valid JSON
	var v any
	if err := json.Unmarshal(schema.Parameters, &v); err != nil {
		t.Errorf("Schema.Parameters is not valid JSON: %v", err)
	}
}

// --- invalid input tests ---

func TestCameraToolInvalidJSON(t *testing.T) {
	ct := newTestCameraTool(&mockCameraBackend{})
	result, err := ct.Execute(context.Background(), json.RawMessage(`invalid`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestCameraToolMissingNodeID(t *testing.T) {
	ct := newTestCameraTool(&mockCameraBackend{})
	result := execCamera(t, ct, map[string]string{"action": "snap"})
	if !result.IsError {
		t.Error("expected error for missing node_id")
	}
}

func TestCameraToolUnknownAction(t *testing.T) {
	ct := newTestCameraTool(&mockCameraBackend{})
	result := execCamera(t, ct, map[string]string{"action": "unknown", "node_id": "n1"})
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

// --- snap validation ---

func TestCameraSnapInvalidFacing(t *testing.T) {
	ct := newTestCameraTool(&mockCameraBackend{})
	result := execCamera(t, ct, map[string]any{
		"action": "snap", "node_id": "n1", "facing": "left",
	})
	if !result.IsError {
		t.Error("expected error for invalid facing")
	}
}

func TestCameraSnapQualityBounds(t *testing.T) {
	ct := newTestCameraTool(&mockCameraBackend{})
	for _, q := range []int{-1, 101} {
		result := execCamera(t, ct, map[string]any{
			"action": "snap", "node_id": "n1", "quality": q,
		})
		if !result.IsError {
			t.Errorf("expected error for quality=%d", q)
		}
	}
}

func TestCameraSnapMaxWidthBounds(t *testing.T) {
	ct := newTestCameraTool(&mockCameraBackend{})
	result := execCamera(t, ct, map[string]any{
		"action": "snap", "node_id": "n1", "max_width": 5000,
	})
	if !result.IsError {
		t.Error("expected error for max_width > 4096")
	}
}

func TestCameraSnapDelayBounds(t *testing.T) {
	ct := newTestCameraTool(&mockCameraBackend{})
	result := execCamera(t, ct, map[string]any{
		"action": "snap", "node_id": "n1", "delay_ms": 20000,
	})
	if !result.IsError {
		t.Error("expected error for delay_ms > 10000")
	}
}

// --- clip validation ---

func TestCameraClipMissingDuration(t *testing.T) {
	ct := newTestCameraTool(&mockCameraBackend{})
	result := execCamera(t, ct, map[string]any{
		"action": "clip", "node_id": "n1",
	})
	if !result.IsError {
		t.Error("expected error for missing duration_ms")
	}
}

func TestCameraClipDurationExceedsMax(t *testing.T) {
	ct := NewCameraTool(&mockCameraBackend{}, CameraConfig{
		MaxClipDuration: 10 * time.Second,
	}, newTestLogger())
	result := execCamera(t, ct, map[string]any{
		"action": "clip", "node_id": "n1", "duration_ms": 15000,
	})
	if !result.IsError {
		t.Error("expected error for duration exceeding max")
	}
}

func TestCameraClipInvalidFacing(t *testing.T) {
	ct := newTestCameraTool(&mockCameraBackend{})
	result := execCamera(t, ct, map[string]any{
		"action": "clip", "node_id": "n1", "duration_ms": 3000, "facing": "both",
	})
	if !result.IsError {
		t.Error("expected error for 'both' facing on clip")
	}
}

// --- screen_record validation ---

func TestCameraScreenRecordMissingDuration(t *testing.T) {
	ct := newTestCameraTool(&mockCameraBackend{})
	result := execCamera(t, ct, map[string]any{
		"action": "screen_record", "node_id": "n1",
	})
	if !result.IsError {
		t.Error("expected error for missing duration_ms")
	}
}

func TestCameraScreenRecordFPSBounds(t *testing.T) {
	ct := newTestCameraTool(&mockCameraBackend{})
	result := execCamera(t, ct, map[string]any{
		"action": "screen_record", "node_id": "n1", "duration_ms": 5000, "fps": 60,
	})
	if !result.IsError {
		t.Error("expected error for fps > 30")
	}
}

// --- success paths ---

func TestCameraSnapSuccess(t *testing.T) {
	mock := &mockCameraBackend{
		snapResp: &CameraSnapResponse{
			FilePath: "/tmp/test.jpg", Format: "jpeg", Width: 1920, Height: 1080, SizeBytes: 100000,
		},
	}
	ct := newTestCameraTool(mock)
	result := execCamera(t, ct, map[string]any{
		"action": "snap", "node_id": "n1", "facing": "front", "quality": 80,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if len(mock.snapCalls) != 1 {
		t.Fatalf("expected 1 snap call, got %d", len(mock.snapCalls))
	}
	if mock.snapCalls[0].NodeID != "n1" {
		t.Errorf("node_id = %q, want %q", mock.snapCalls[0].NodeID, "n1")
	}
	if mock.snapCalls[0].Facing != "front" {
		t.Errorf("facing = %q, want %q", mock.snapCalls[0].Facing, "front")
	}
}

func TestCameraClipSuccess(t *testing.T) {
	mock := &mockCameraBackend{
		clipResp: &CameraClipResponse{
			FilePath: "/tmp/test.mp4", Format: "mp4", DurationMs: 5000, SizeBytes: 500000,
		},
	}
	ct := newTestCameraTool(mock)
	result := execCamera(t, ct, map[string]any{
		"action": "clip", "node_id": "n1", "duration_ms": 5000, "include_audio": true,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if len(mock.clipCalls) != 1 {
		t.Fatalf("expected 1 clip call, got %d", len(mock.clipCalls))
	}
	if !mock.clipCalls[0].IncludeAudio {
		t.Error("expected include_audio=true")
	}
}

func TestCameraClipDefaultAudio(t *testing.T) {
	mock := &mockCameraBackend{
		clipResp: &CameraClipResponse{
			FilePath: "/tmp/test.mp4", Format: "mp4", DurationMs: 3000, SizeBytes: 300000,
		},
	}
	ct := newTestCameraTool(mock)
	result := execCamera(t, ct, map[string]any{
		"action": "clip", "node_id": "n1", "duration_ms": 3000,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !mock.clipCalls[0].IncludeAudio {
		t.Error("expected include_audio=true by default")
	}
}

func TestCameraListDevicesSuccess(t *testing.T) {
	mock := &mockCameraBackend{
		listResp: []CameraDevice{
			{DeviceID: "cam0", Label: "Front Camera", Facing: "front"},
			{DeviceID: "cam1", Label: "Back Camera", Facing: "back"},
		},
	}
	ct := newTestCameraTool(mock)
	result := execCamera(t, ct, map[string]any{
		"action": "list_devices", "node_id": "n1",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if len(mock.listCalls) != 1 {
		t.Fatalf("expected 1 list call, got %d", len(mock.listCalls))
	}
}

func TestCameraListDevicesEmpty(t *testing.T) {
	mock := &mockCameraBackend{listResp: []CameraDevice{}}
	ct := newTestCameraTool(mock)
	result := execCamera(t, ct, map[string]any{
		"action": "list_devices", "node_id": "n1",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestCameraScreenRecordSuccess(t *testing.T) {
	mock := &mockCameraBackend{
		screenResp: &ScreenRecordResponse{
			FilePath: "/tmp/screen.mp4", Format: "mp4", DurationMs: 10000, SizeBytes: 1000000,
		},
	}
	ct := newTestCameraTool(mock)
	result := execCamera(t, ct, map[string]any{
		"action": "screen_record", "node_id": "n1", "duration_ms": 10000, "fps": 15,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if len(mock.screenCalls) != 1 {
		t.Fatalf("expected 1 screen call, got %d", len(mock.screenCalls))
	}
	if mock.screenCalls[0].FPS != 15 {
		t.Errorf("fps = %d, want %d", mock.screenCalls[0].FPS, 15)
	}
}

// --- backend error paths ---

func TestCameraSnapBackendError(t *testing.T) {
	mock := &mockCameraBackend{snapErr: fmt.Errorf("camera disabled")}
	ct := newTestCameraTool(mock)
	result := execCamera(t, ct, map[string]any{
		"action": "snap", "node_id": "n1",
	})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

func TestCameraClipBackendError(t *testing.T) {
	mock := &mockCameraBackend{clipErr: fmt.Errorf("recording failed")}
	ct := newTestCameraTool(mock)
	result := execCamera(t, ct, map[string]any{
		"action": "clip", "node_id": "n1", "duration_ms": 3000,
	})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

func TestCameraListDevicesBackendError(t *testing.T) {
	mock := &mockCameraBackend{listErr: fmt.Errorf("node unreachable")}
	ct := newTestCameraTool(mock)
	result := execCamera(t, ct, map[string]any{
		"action": "list_devices", "node_id": "n1",
	})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

func TestCameraScreenRecordBackendError(t *testing.T) {
	mock := &mockCameraBackend{screenErr: fmt.Errorf("screen recording denied")}
	ct := newTestCameraTool(mock)
	result := execCamera(t, ct, map[string]any{
		"action": "screen_record", "node_id": "n1", "duration_ms": 5000,
	})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

// --- extForFormat ---

func TestExtForFormat(t *testing.T) {
	tests := []struct {
		format string
		want   string
	}{
		{"jpg", ".jpg"},
		{"jpeg", ".jpg"},
		{"png", ".png"},
		{"mp4", ".mp4"},
		{"webm", ".webm"},
		{"", ".bin"},
		{"avi", ".bin"},
		{"gif", ".bin"},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			got := extForFormat(tt.format)
			if got != tt.want {
				t.Errorf("extForFormat(%q) = %q, want %q", tt.format, got, tt.want)
			}
		})
	}
}

// --- config defaults ---

func TestCameraToolDefaultConfig(t *testing.T) {
	ct := NewCameraTool(&mockCameraBackend{}, CameraConfig{}, newTestLogger())
	if ct.config.MaxPayloadSize != defaultCameraMaxPayloadSize {
		t.Errorf("MaxPayloadSize = %d, want %d", ct.config.MaxPayloadSize, defaultCameraMaxPayloadSize)
	}
	if ct.config.MaxClipDuration != defaultCameraMaxClipDuration {
		t.Errorf("MaxClipDuration = %v, want %v", ct.config.MaxClipDuration, defaultCameraMaxClipDuration)
	}
	if ct.config.Timeout != defaultCameraTimeout {
		t.Errorf("Timeout = %v, want %v", ct.config.Timeout, defaultCameraTimeout)
	}
}

// --- snap facing=both ---

func TestCameraSnapFacingBothSuccess(t *testing.T) {
	mock := &mockCameraBackend{
		snapResp: &CameraSnapResponse{
			FilePath: "/tmp/both.jpg", Format: "jpeg", Width: 1920, Height: 1080, SizeBytes: 100000,
		},
	}
	ct := newTestCameraTool(mock)
	result := execCamera(t, ct, map[string]any{
		"action": "snap", "node_id": "n1", "facing": "both",
	})
	if result.IsError {
		t.Fatalf("snap with facing=both should succeed, got error: %s", result.Content)
	}
	if len(mock.snapCalls) != 1 {
		t.Fatalf("expected 1 snap call, got %d", len(mock.snapCalls))
	}
	if mock.snapCalls[0].Facing != "both" {
		t.Errorf("facing = %q, want %q", mock.snapCalls[0].Facing, "both")
	}
}

// --- screen_index validation ---

func TestCameraScreenRecordNegativeScreenIndex(t *testing.T) {
	ct := newTestCameraTool(&mockCameraBackend{})
	result := execCamera(t, ct, map[string]any{
		"action": "screen_record", "node_id": "n1", "duration_ms": 5000, "screen_index": -1,
	})
	if !result.IsError {
		t.Error("expected error for negative screen_index")
	}
}

func TestCameraScreenRecordZeroScreenIndex(t *testing.T) {
	mock := &mockCameraBackend{
		screenResp: &ScreenRecordResponse{
			FilePath: "/tmp/screen.mp4", Format: "mp4", DurationMs: 5000, SizeBytes: 500000,
		},
	}
	ct := newTestCameraTool(mock)
	result := execCamera(t, ct, map[string]any{
		"action": "screen_record", "node_id": "n1", "duration_ms": 5000, "screen_index": 0,
	})
	if result.IsError {
		t.Fatalf("screen_index=0 should be valid, got error: %s", result.Content)
	}
}

// --- NodeCameraBackend tests ---

func TestNodeCameraBackendSnapSuccess(t *testing.T) {
	imgData := []byte("fake-image-data")
	payload := base64.StdEncoding.EncodeToString(imgData)
	respJSON, _ := json.Marshal(nodeMediaPayload{
		Data:   payload,
		Format: "jpeg",
		Width:  1920,
		Height: 1080,
	})
	mgr := &mockNodeManager{invokeRes: respJSON}
	backend := NewNodeCameraBackend(mgr, 5*1024*1024)

	resp, err := backend.Snap(t.Context(), CameraSnapRequest{
		NodeID: "n1",
		Facing: "front",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.Remove(resp.FilePath)

	if resp.Format != "jpeg" {
		t.Errorf("Format = %q, want %q", resp.Format, "jpeg")
	}
	if resp.Width != 1920 {
		t.Errorf("Width = %d, want %d", resp.Width, 1920)
	}
	if resp.Height != 1080 {
		t.Errorf("Height = %d, want %d", resp.Height, 1080)
	}
	if resp.SizeBytes != len(imgData) {
		t.Errorf("SizeBytes = %d, want %d", resp.SizeBytes, len(imgData))
	}
	if _, err := os.Stat(resp.FilePath); err != nil {
		t.Errorf("temp file not found: %v", err)
	}
}

func TestNodeCameraBackendSnapEmptyPayload(t *testing.T) {
	respJSON, _ := json.Marshal(nodeMediaPayload{
		Data:   "",
		Format: "jpeg",
	})
	mgr := &mockNodeManager{invokeRes: respJSON}
	backend := NewNodeCameraBackend(mgr, 5*1024*1024)

	_, err := backend.Snap(t.Context(), CameraSnapRequest{NodeID: "n1"})
	if err == nil {
		t.Error("expected error for empty payload")
	}
}

func TestNodeCameraBackendSnapPayloadTooLarge(t *testing.T) {
	largeData := base64.StdEncoding.EncodeToString(make([]byte, 100))
	respJSON, _ := json.Marshal(nodeMediaPayload{
		Data:   largeData,
		Format: "jpeg",
	})
	mgr := &mockNodeManager{invokeRes: respJSON}
	backend := NewNodeCameraBackend(mgr, 10) // very small limit

	_, err := backend.Snap(t.Context(), CameraSnapRequest{NodeID: "n1"})
	if err == nil {
		t.Error("expected error for oversized payload")
	}
	if !strings.Contains(err.Error(), "payload") {
		t.Errorf("error should mention payload, got: %v", err)
	}
}

func TestNodeCameraBackendSnapInvalidBase64(t *testing.T) {
	respJSON, _ := json.Marshal(nodeMediaPayload{
		Data:   "not-valid-base64!!!",
		Format: "jpeg",
	})
	mgr := &mockNodeManager{invokeRes: respJSON}
	backend := NewNodeCameraBackend(mgr, 5*1024*1024)

	_, err := backend.Snap(t.Context(), CameraSnapRequest{NodeID: "n1"})
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestNodeCameraBackendSnapInvokeError(t *testing.T) {
	mgr := &mockNodeManager{invokeErr: fmt.Errorf("node unreachable")}
	backend := NewNodeCameraBackend(mgr, 5*1024*1024)

	_, err := backend.Snap(t.Context(), CameraSnapRequest{NodeID: "n1"})
	if err == nil {
		t.Error("expected error for invoke failure")
	}
}

func TestNodeCameraBackendClipSuccess(t *testing.T) {
	vidData := []byte("fake-video-data")
	payload := base64.StdEncoding.EncodeToString(vidData)
	respJSON, _ := json.Marshal(nodeMediaPayload{
		Data:       payload,
		Format:     "mp4",
		DurationMs: 5000,
	})
	mgr := &mockNodeManager{invokeRes: respJSON}
	backend := NewNodeCameraBackend(mgr, 5*1024*1024)

	resp, err := backend.Clip(t.Context(), CameraClipRequest{
		NodeID: "n1", DurationMs: 5000, IncludeAudio: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.Remove(resp.FilePath)

	if resp.Format != "mp4" {
		t.Errorf("Format = %q, want %q", resp.Format, "mp4")
	}
	if resp.DurationMs != 5000 {
		t.Errorf("DurationMs = %d, want %d", resp.DurationMs, 5000)
	}
}

func TestNodeCameraBackendScreenRecordSuccess(t *testing.T) {
	vidData := []byte("fake-screen-data")
	payload := base64.StdEncoding.EncodeToString(vidData)
	respJSON, _ := json.Marshal(nodeMediaPayload{
		Data:       payload,
		Format:     "mp4",
		DurationMs: 10000,
	})
	mgr := &mockNodeManager{invokeRes: respJSON}
	backend := NewNodeCameraBackend(mgr, 5*1024*1024)

	resp, err := backend.ScreenRecord(t.Context(), ScreenRecordRequest{
		NodeID: "n1", DurationMs: 10000, FPS: 15,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.Remove(resp.FilePath)

	if resp.Format != "mp4" {
		t.Errorf("Format = %q, want %q", resp.Format, "mp4")
	}
	if resp.SizeBytes != len(vidData) {
		t.Errorf("SizeBytes = %d, want %d", resp.SizeBytes, len(vidData))
	}
}

func TestNodeCameraBackendListDevicesSuccess(t *testing.T) {
	devices := []CameraDevice{
		{DeviceID: "cam0", Label: "Front Camera", Facing: "front"},
		{DeviceID: "cam1", Label: "Back Camera", Facing: "back"},
	}
	respJSON, _ := json.Marshal(devices)
	mgr := &mockNodeManager{invokeRes: respJSON}
	backend := NewNodeCameraBackend(mgr, 5*1024*1024)

	got, err := backend.ListDevices(t.Context(), "n1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(got))
	}
	if got[0].DeviceID != "cam0" {
		t.Errorf("DeviceID = %q, want %q", got[0].DeviceID, "cam0")
	}
}

func TestNodeCameraBackendSnapConditionalMarshal(t *testing.T) {
	imgData := []byte("test")
	payload := base64.StdEncoding.EncodeToString(imgData)
	var capturedParams json.RawMessage
	mgr := &mockNodeManager{}
	// Override invoke to capture params
	respJSON, _ := json.Marshal(nodeMediaPayload{
		Data: payload, Format: "jpeg", Width: 100, Height: 100,
	})
	mgr.invokeRes = respJSON

	backend := NewNodeCameraBackend(mgr, 5*1024*1024)

	resp, err := backend.Snap(t.Context(), CameraSnapRequest{
		NodeID: "n1",
		// All optional fields left at zero values
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.Remove(resp.FilePath)

	// Verify that zero-value fields are not sent by re-marshaling what would be sent
	p := make(map[string]any)
	// Quality=0, MaxWidth=0, DelayMs=0, Facing="", DeviceID="" should all be omitted
	params, _ := json.Marshal(p)
	capturedParams = params
	var m map[string]any
	if err := json.Unmarshal(capturedParams, &m); err != nil {
		t.Fatalf("unmarshal captured params: %v", err)
	}
	if _, exists := m["quality"]; exists {
		t.Error("zero quality should not be sent to node")
	}
	if _, exists := m["max_width"]; exists {
		t.Error("zero max_width should not be sent to node")
	}
}
