package tool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"alfred-ai/internal/domain"
)

// NodeCameraBackend implements CameraBackend by delegating to NodeManager.Invoke().
type NodeCameraBackend struct {
	manager        domain.NodeManager
	maxPayloadSize int
}

// NewNodeCameraBackend creates a camera backend backed by the node system.
func NewNodeCameraBackend(manager domain.NodeManager, maxPayloadSize int) *NodeCameraBackend {
	if maxPayloadSize <= 0 {
		maxPayloadSize = 5 * 1024 * 1024 // 5 MiB
	}
	return &NodeCameraBackend{
		manager:        manager,
		maxPayloadSize: maxPayloadSize,
	}
}

func (b *NodeCameraBackend) Name() string { return "node" }

// nodeMediaPayload is the expected JSON envelope from a node media response.
type nodeMediaPayload struct {
	Data       string `json:"data"`
	Format     string `json:"format"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	DurationMs int    `json:"duration_ms,omitempty"`
	HasAudio   bool   `json:"has_audio,omitempty"`
}

func (b *NodeCameraBackend) Snap(ctx context.Context, req CameraSnapRequest) (*CameraSnapResponse, error) {
	p := make(map[string]any)
	if req.Facing != "" {
		p["facing"] = req.Facing
	}
	if req.MaxWidth > 0 {
		p["max_width"] = req.MaxWidth
	}
	if req.Quality > 0 {
		p["quality"] = req.Quality
	}
	if req.DelayMs > 0 {
		p["delay_ms"] = req.DelayMs
	}
	if req.DeviceID != "" {
		p["device_id"] = req.DeviceID
	}
	params, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal camera_snap params: %w", err)
	}

	raw, err := b.manager.Invoke(ctx, req.NodeID, "camera_snap", params)
	if err != nil {
		return nil, fmt.Errorf("camera_snap invoke: %w", err)
	}

	var payload nodeMediaPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse camera_snap response: %w", err)
	}

	filePath, size, err := b.decodeAndWriteMedia(payload.Data, payload.Format, "alfredai-camera-snap-*")
	if err != nil {
		return nil, err
	}

	return &CameraSnapResponse{
		FilePath:  filePath,
		Format:    payload.Format,
		Width:     payload.Width,
		Height:    payload.Height,
		SizeBytes: size,
	}, nil
}

func (b *NodeCameraBackend) Clip(ctx context.Context, req CameraClipRequest) (*CameraClipResponse, error) {
	p := map[string]any{
		"duration_ms":   req.DurationMs,
		"include_audio": req.IncludeAudio,
	}
	if req.Facing != "" {
		p["facing"] = req.Facing
	}
	if req.DeviceID != "" {
		p["device_id"] = req.DeviceID
	}
	params, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal camera_clip params: %w", err)
	}

	raw, err := b.manager.Invoke(ctx, req.NodeID, "camera_clip", params)
	if err != nil {
		return nil, fmt.Errorf("camera_clip invoke: %w", err)
	}

	var payload nodeMediaPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse camera_clip response: %w", err)
	}

	filePath, size, err := b.decodeAndWriteMedia(payload.Data, payload.Format, "alfredai-camera-clip-*")
	if err != nil {
		return nil, err
	}

	return &CameraClipResponse{
		FilePath:   filePath,
		Format:     payload.Format,
		DurationMs: payload.DurationMs,
		SizeBytes:  size,
	}, nil
}

func (b *NodeCameraBackend) ListDevices(ctx context.Context, nodeID string) ([]CameraDevice, error) {
	raw, err := b.manager.Invoke(ctx, nodeID, "camera_list", nil)
	if err != nil {
		return nil, fmt.Errorf("camera_list invoke: %w", err)
	}

	var devices []CameraDevice
	if err := json.Unmarshal(raw, &devices); err != nil {
		return nil, fmt.Errorf("parse camera_list response: %w", err)
	}

	return devices, nil
}

func (b *NodeCameraBackend) ScreenRecord(ctx context.Context, req ScreenRecordRequest) (*ScreenRecordResponse, error) {
	p := map[string]any{
		"duration_ms":   req.DurationMs,
		"include_audio": req.IncludeAudio,
	}
	if req.FPS > 0 {
		p["fps"] = req.FPS
	}
	if req.ScreenIndex > 0 {
		p["screen_index"] = req.ScreenIndex
	}
	params, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal screen_record params: %w", err)
	}

	raw, err := b.manager.Invoke(ctx, req.NodeID, "screen_record", params)
	if err != nil {
		return nil, fmt.Errorf("screen_record invoke: %w", err)
	}

	var payload nodeMediaPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse screen_record response: %w", err)
	}

	filePath, size, err := b.decodeAndWriteMedia(payload.Data, payload.Format, "alfredai-screen-record-*")
	if err != nil {
		return nil, err
	}

	return &ScreenRecordResponse{
		FilePath:   filePath,
		Format:     payload.Format,
		DurationMs: payload.DurationMs,
		SizeBytes:  size,
	}, nil
}

// decodeAndWriteMedia validates the base64 payload size, decodes it,
// and writes the result to a temporary file. Returns the file path and byte count.
//
// Callers are responsible for cleaning up the returned file when no longer needed.
// Files use the "alfredai-camera-*" or "alfredai-screen-*" prefix for easy discovery.
//
// NOTE: The full base64 payload and decoded bytes coexist in memory briefly.
// For a 5 MiB base64 payload, peak memory usage is ~8.75 MiB per call.
func (b *NodeCameraBackend) decodeAndWriteMedia(data, format, pattern string) (string, int, error) {
	if len(data) == 0 {
		return "", 0, fmt.Errorf("empty media payload from node")
	}
	if len(data) > b.maxPayloadSize {
		return "", 0, fmt.Errorf("payload too large: %w: %d bytes (max %d)", domain.ErrLimitReached, len(data), b.maxPayloadSize)
	}

	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", 0, fmt.Errorf("decode base64 media: %w", err)
	}

	ext := extForFormat(format)
	f, err := os.CreateTemp("", pattern+ext)
	if err != nil {
		return "", 0, fmt.Errorf("create temp file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(decoded); err != nil {
		os.Remove(f.Name())
		return "", 0, fmt.Errorf("write temp file: %w", err)
	}

	return f.Name(), len(decoded), nil
}

// extForFormat maps a media format string to a file extension.
func extForFormat(format string) string {
	switch format {
	case "jpg", "jpeg":
		return ".jpg"
	case "png":
		return ".png"
	case "mp4":
		return ".mp4"
	case "webm":
		return ".webm"
	default:
		return ".bin"
	}
}
