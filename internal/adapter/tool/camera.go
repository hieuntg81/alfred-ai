package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/trace"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/tracer"
)

// Camera tool guard constants.
const (
	defaultCameraMaxPayloadSize  = 5 * 1024 * 1024 // 5 MiB base64
	defaultCameraMaxClipDuration = 60 * time.Second
	defaultCameraTimeout         = 30 * time.Second
	maxCameraDelayMs             = 10000
	maxCameraQuality             = 100
	maxCameraMaxWidth            = 4096
	maxScreenRecordFPS           = 30
)

// CameraConfig holds configuration for the CameraTool.
type CameraConfig struct {
	MaxPayloadSize  int
	MaxClipDuration time.Duration
	Timeout         time.Duration
}

// CameraTool provides camera and screen capture capabilities via remote nodes.
type CameraTool struct {
	backend CameraBackend
	config  CameraConfig
	logger  *slog.Logger
}

// NewCameraTool creates a camera tool backed by the given CameraBackend.
func NewCameraTool(backend CameraBackend, cfg CameraConfig, logger *slog.Logger) *CameraTool {
	if cfg.MaxPayloadSize <= 0 {
		cfg.MaxPayloadSize = defaultCameraMaxPayloadSize
	}
	if cfg.MaxClipDuration <= 0 {
		cfg.MaxClipDuration = defaultCameraMaxClipDuration
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultCameraTimeout
	}
	return &CameraTool{backend: backend, config: cfg, logger: logger}
}

func (t *CameraTool) Name() string { return "camera" }
func (t *CameraTool) Description() string {
	return "Capture photos, record short video clips, record screen, and list cameras on remote nodes. " +
		"Use node_list first to find available nodes, then pass node_id to camera actions."
}

func (t *CameraTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["snap", "clip", "list_devices", "screen_record"],
					"description": "The camera action to perform"
				},
				"node_id": {
					"type": "string",
					"description": "Target node ID (use node_list to find available nodes)"
				},
				"facing": {
					"type": "string",
					"enum": ["front", "back", "both"],
					"description": "Camera facing direction (for snap/clip, default: front)"
				},
				"max_width": {
					"type": "integer",
					"description": "Maximum image width in pixels (for snap, max 4096)"
				},
				"quality": {
					"type": "integer",
					"description": "Image quality 1-100 (for snap, default 80)"
				},
				"delay_ms": {
					"type": "integer",
					"description": "Delay before capture in milliseconds (for snap, max 10000)"
				},
				"device_id": {
					"type": "string",
					"description": "Specific camera device ID from list_devices"
				},
				"duration_ms": {
					"type": "integer",
					"description": "Recording duration in milliseconds (required for clip/screen_record)"
				},
				"include_audio": {
					"type": "boolean",
					"description": "Include audio in recording (for clip/screen_record, default true)"
				},
				"fps": {
					"type": "integer",
					"description": "Frames per second for screen_record (default 15, max 30)"
				},
				"screen_index": {
					"type": "integer",
					"description": "Screen/display index for screen_record (default 0)"
				}
			},
			"required": ["action", "node_id"]
		}`),
	}
}

var validCameraFacings = map[string]bool{
	"front": true, "back": true, "both": true,
}

func validateFacing(facing string, allowBoth bool) error {
	if facing == "" {
		return nil
	}
	if !validCameraFacings[facing] {
		return fmt.Errorf("invalid facing %q", facing)
	}
	if facing == "both" && !allowBoth {
		return fmt.Errorf("facing %q not supported for this action (want: front, back)", facing)
	}
	return nil
}

type cameraParams struct {
	Action       string `json:"action"`
	NodeID       string `json:"node_id"`
	Facing       string `json:"facing,omitempty"`
	MaxWidth     int    `json:"max_width,omitempty"`
	Quality      int    `json:"quality,omitempty"`
	DelayMs      int    `json:"delay_ms,omitempty"`
	DeviceID     string `json:"device_id,omitempty"`
	DurationMs   int    `json:"duration_ms,omitempty"`
	IncludeAudio *bool  `json:"include_audio,omitempty"`
	FPS          int    `json:"fps,omitempty"`
	ScreenIndex  int    `json:"screen_index,omitempty"`
}

func (t *CameraTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	return Execute(ctx, "tool.camera", t.logger, params,
		func(ctx context.Context, span trace.Span, p cameraParams) (any, error) {
			span.SetAttributes(tracer.StringAttr("tool.action", p.Action))

			if err := RequireField("node_id", p.NodeID); err != nil {
				return nil, err
			}

			ctx, cancel := context.WithTimeout(ctx, t.config.Timeout)
			defer cancel()

			switch p.Action {
			case "snap":
				return t.handleSnap(ctx, p)
			case "clip":
				return t.handleClip(ctx, p)
			case "list_devices":
				return t.handleListDevices(ctx, p)
			case "screen_record":
				return t.handleScreenRecord(ctx, p)
			default:
				return nil, BadAction(p.Action, "snap", "clip", "list_devices", "screen_record")
			}
		},
	)
}

func (t *CameraTool) handleSnap(ctx context.Context, p cameraParams) (any, error) {
	if err := validateFacing(p.Facing, true); err != nil {
		return nil, err
	}
	if err := ValidateRange("quality", p.Quality, 0, maxCameraQuality); err != nil {
		return nil, err
	}
	if err := ValidateRange("max_width", p.MaxWidth, 0, maxCameraMaxWidth); err != nil {
		return nil, err
	}
	if err := ValidateRange("delay_ms", p.DelayMs, 0, maxCameraDelayMs); err != nil {
		return nil, err
	}

	resp, err := t.backend.Snap(ctx, CameraSnapRequest{
		NodeID:   p.NodeID,
		Facing:   p.Facing,
		MaxWidth: p.MaxWidth,
		Quality:  p.Quality,
		DelayMs:  p.DelayMs,
		DeviceID: p.DeviceID,
	})
	if err != nil {
		return nil, err
	}

	t.logger.Info("camera snap captured",
		"node_id", p.NodeID,
		"format", resp.Format,
		"size", resp.SizeBytes,
	)
	return resp, nil
}

func (t *CameraTool) handleClip(ctx context.Context, p cameraParams) (any, error) {
	if p.DurationMs <= 0 {
		return nil, fmt.Errorf("duration_ms is required and must be > 0 for clip action")
	}
	maxMs := int(t.config.MaxClipDuration.Milliseconds())
	if p.DurationMs > maxMs {
		return nil, fmt.Errorf("%w: %d ms (max %d)", domain.ErrLimitReached, p.DurationMs, maxMs)
	}
	if err := validateFacing(p.Facing, false); err != nil {
		return nil, err
	}

	includeAudio := true
	if p.IncludeAudio != nil {
		includeAudio = *p.IncludeAudio
	}

	resp, err := t.backend.Clip(ctx, CameraClipRequest{
		NodeID:       p.NodeID,
		Facing:       p.Facing,
		DurationMs:   p.DurationMs,
		IncludeAudio: includeAudio,
		DeviceID:     p.DeviceID,
	})
	if err != nil {
		return nil, err
	}

	t.logger.Info("camera clip recorded",
		"node_id", p.NodeID,
		"duration_ms", resp.DurationMs,
		"size", resp.SizeBytes,
	)
	return resp, nil
}

func (t *CameraTool) handleListDevices(ctx context.Context, p cameraParams) (any, error) {
	devices, err := t.backend.ListDevices(ctx, p.NodeID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"node_id": p.NodeID,
		"devices": devices,
		"count":   len(devices),
	}, nil
}

func (t *CameraTool) handleScreenRecord(ctx context.Context, p cameraParams) (any, error) {
	if err := ValidatePositive("duration_ms", p.DurationMs); err != nil {
		return nil, err
	}
	maxMs := int(t.config.MaxClipDuration.Milliseconds())
	if p.DurationMs > maxMs {
		return nil, fmt.Errorf("%w: %d ms (max %d)", domain.ErrLimitReached, p.DurationMs, maxMs)
	}
	if err := ValidateRange("fps", p.FPS, 0, maxScreenRecordFPS); err != nil {
		return nil, err
	}
	if p.ScreenIndex < 0 {
		return nil, fmt.Errorf("screen_index must be >= 0")
	}

	includeAudio := true
	if p.IncludeAudio != nil {
		includeAudio = *p.IncludeAudio
	}

	resp, err := t.backend.ScreenRecord(ctx, ScreenRecordRequest{
		NodeID:       p.NodeID,
		DurationMs:   p.DurationMs,
		FPS:          p.FPS,
		ScreenIndex:  p.ScreenIndex,
		IncludeAudio: includeAudio,
	})
	if err != nil {
		return nil, err
	}

	t.logger.Info("screen recording captured",
		"node_id", p.NodeID,
		"duration_ms", resp.DurationMs,
		"size", resp.SizeBytes,
	)
	return resp, nil
}
