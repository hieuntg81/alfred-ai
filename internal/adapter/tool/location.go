package tool

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/trace"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/tracer"
)

// Location tool constants.
const (
	defaultLocationTimeout         = 10 * time.Second
	defaultLocationDesiredAccuracy = "balanced"
	maxLocationMaxAgeMs            = 300000 // 5 minutes
	maxLocationTimeoutMs           = 60000  // 60 seconds
)

// validLocationAccuracies defines the allowed accuracy levels.
var validLocationAccuracies = map[string]bool{
	"coarse":   true,
	"balanced": true,
	"precise":  true,
}

// LocationConfig holds configuration for the LocationTool.
type LocationConfig struct {
	Timeout         time.Duration
	DefaultAccuracy string
}

// LocationTool retrieves geographic location from remote nodes.
type LocationTool struct {
	backend LocationBackend
	config  LocationConfig
	logger  *slog.Logger
}

// NewLocationTool creates a location tool backed by the given LocationBackend.
func NewLocationTool(backend LocationBackend, cfg LocationConfig, logger *slog.Logger) *LocationTool {
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultLocationTimeout
	}
	if cfg.DefaultAccuracy == "" {
		cfg.DefaultAccuracy = defaultLocationDesiredAccuracy
	}
	return &LocationTool{backend: backend, config: cfg, logger: logger}
}

func (t *LocationTool) Name() string { return "location" }
func (t *LocationTool) Description() string {
	return "Get the geographic location of a remote node (phone, tablet, laptop). " +
		"Use node_list first to find available nodes, then pass node_id."
}

func (t *LocationTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["get"],
					"description": "The location action to perform"
				},
				"node_id": {
					"type": "string",
					"description": "Target node ID (use node_list to find available nodes)"
				},
				"desired_accuracy": {
					"type": "string",
					"enum": ["coarse", "balanced", "precise"],
					"description": "Desired location accuracy (default: balanced)"
				},
				"max_age_ms": {
					"type": "integer",
					"description": "Accept cached location up to this age in milliseconds (max 300000)"
				},
				"timeout_ms": {
					"type": "integer",
					"description": "Maximum time to wait for location fix in milliseconds (max 60000)"
				}
			},
			"required": ["action", "node_id"]
		}`),
	}
}

type locationParams struct {
	Action          string `json:"action"`
	NodeID          string `json:"node_id"`
	DesiredAccuracy string `json:"desired_accuracy,omitempty"`
	MaxAgeMs        int    `json:"max_age_ms,omitempty"`
	TimeoutMs       int    `json:"timeout_ms,omitempty"`
}

func (t *LocationTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	return Execute(ctx, "tool.location", t.logger, params,
		func(ctx context.Context, span trace.Span, p locationParams) (any, error) {
			span.SetAttributes(tracer.StringAttr("tool.action", p.Action))

			if err := RequireField("node_id", p.NodeID); err != nil {
				return nil, err
			}

			switch p.Action {
			case "get":
				return t.handleGetLocation(ctx, p)
			default:
				return nil, BadAction(p.Action, "get")
			}
		},
	)
}

func (t *LocationTool) handleGetLocation(ctx context.Context, p locationParams) (any, error) {
	accuracy := p.DesiredAccuracy
	if accuracy == "" {
		accuracy = t.config.DefaultAccuracy
	}
	if err := ValidateEnum("desired_accuracy", accuracy, "coarse", "balanced", "precise"); err != nil {
		return nil, err
	}
	if err := ValidateRange("max_age_ms", p.MaxAgeMs, 0, maxLocationMaxAgeMs); err != nil {
		return nil, err
	}
	if err := ValidateRange("timeout_ms", p.TimeoutMs, 0, maxLocationTimeoutMs); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, t.config.Timeout)
	defer cancel()

	resp, err := t.backend.GetLocation(ctx, LocationRequest{
		NodeID:          p.NodeID,
		MaxAgeMs:        p.MaxAgeMs,
		TimeoutMs:       p.TimeoutMs,
		DesiredAccuracy: accuracy,
	})
	if err != nil {
		return nil, err
	}

	t.logger.Debug("location retrieved",
		"node_id", p.NodeID,
		"source", resp.Source,
		"is_precise", resp.IsPrecise,
	)
	return resp, nil
}
