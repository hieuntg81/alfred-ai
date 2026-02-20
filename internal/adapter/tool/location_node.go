package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"alfred-ai/internal/domain"
)

// NodeLocationBackend implements LocationBackend by delegating to NodeManager.Invoke().
type NodeLocationBackend struct {
	manager domain.NodeManager
}

// NewNodeLocationBackend creates a location backend backed by the node system.
func NewNodeLocationBackend(manager domain.NodeManager) *NodeLocationBackend {
	return &NodeLocationBackend{manager: manager}
}

func (b *NodeLocationBackend) Name() string { return "node" }

func (b *NodeLocationBackend) GetLocation(ctx context.Context, req LocationRequest) (*LocationResponse, error) {
	p := map[string]any{
		"desired_accuracy": req.DesiredAccuracy,
	}
	if req.MaxAgeMs > 0 {
		p["max_age_ms"] = req.MaxAgeMs
	}
	if req.TimeoutMs > 0 {
		p["timeout_ms"] = req.TimeoutMs
	}
	params, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal location_get params: %w", err)
	}

	raw, err := b.manager.Invoke(ctx, req.NodeID, "location_get", params)
	if err != nil {
		return nil, fmt.Errorf("location_get invoke: %w", err)
	}

	var resp LocationResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse location_get response: %w", err)
	}

	if math.IsNaN(resp.Latitude) || math.IsInf(resp.Latitude, 0) ||
		math.IsNaN(resp.Longitude) || math.IsInf(resp.Longitude, 0) {
		return nil, fmt.Errorf("invalid coordinates from node: lat=%v lon=%v", resp.Latitude, resp.Longitude)
	}
	if resp.Latitude < -90 || resp.Latitude > 90 {
		return nil, fmt.Errorf("invalid latitude from node: %f", resp.Latitude)
	}
	if resp.Longitude < -180 || resp.Longitude > 180 {
		return nil, fmt.Errorf("invalid longitude from node: %f", resp.Longitude)
	}
	if resp.Timestamp == "" {
		return nil, fmt.Errorf("missing timestamp from node response")
	}

	return &resp, nil
}
