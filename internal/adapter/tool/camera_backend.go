package tool

import "context"

// CameraBackend abstracts camera and screen capture operations.
// The default implementation delegates to NodeManager.Invoke().
type CameraBackend interface {
	// Snap captures a photo from a camera on the target node.
	Snap(ctx context.Context, req CameraSnapRequest) (*CameraSnapResponse, error)
	// Clip records a short video clip from a camera on the target node.
	Clip(ctx context.Context, req CameraClipRequest) (*CameraClipResponse, error)
	// ListDevices enumerates available cameras on the target node.
	ListDevices(ctx context.Context, nodeID string) ([]CameraDevice, error)
	// ScreenRecord captures screen video from the target node.
	ScreenRecord(ctx context.Context, req ScreenRecordRequest) (*ScreenRecordResponse, error)
	// Name returns the backend identifier (e.g. "node").
	Name() string
}

// CameraSnapRequest holds parameters for a camera snap action.
type CameraSnapRequest struct {
	NodeID   string `json:"node_id"`
	Facing   string `json:"facing,omitempty"`
	MaxWidth int    `json:"max_width,omitempty"`
	Quality  int    `json:"quality,omitempty"`
	DelayMs  int    `json:"delay_ms,omitempty"`
	DeviceID string `json:"device_id,omitempty"`
}

// CameraSnapResponse holds the result of a camera snap.
type CameraSnapResponse struct {
	FilePath  string `json:"file_path"`
	Format    string `json:"format"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	SizeBytes int    `json:"size_bytes"`
}

// CameraClipRequest holds parameters for a camera clip action.
type CameraClipRequest struct {
	NodeID       string `json:"node_id"`
	Facing       string `json:"facing,omitempty"`
	DurationMs   int    `json:"duration_ms"`
	IncludeAudio bool   `json:"include_audio,omitempty"`
	DeviceID     string `json:"device_id,omitempty"`
}

// CameraClipResponse holds the result of a camera clip recording.
type CameraClipResponse struct {
	FilePath   string `json:"file_path"`
	Format     string `json:"format"`
	DurationMs int    `json:"duration_ms"`
	SizeBytes  int    `json:"size_bytes"`
}

// CameraDevice describes a single camera available on a node.
type CameraDevice struct {
	DeviceID string `json:"device_id"`
	Label    string `json:"label"`
	Facing   string `json:"facing,omitempty"`
}

// ScreenRecordRequest holds parameters for a screen record action.
type ScreenRecordRequest struct {
	NodeID       string `json:"node_id"`
	DurationMs   int    `json:"duration_ms"`
	FPS          int    `json:"fps,omitempty"`
	ScreenIndex  int    `json:"screen_index,omitempty"`
	IncludeAudio bool   `json:"include_audio,omitempty"`
}

// ScreenRecordResponse holds the result of a screen recording.
type ScreenRecordResponse struct {
	FilePath   string `json:"file_path"`
	Format     string `json:"format"`
	DurationMs int    `json:"duration_ms"`
	SizeBytes  int    `json:"size_bytes"`
}
