package tool

import "context"

// LocationBackend abstracts location retrieval from remote nodes.
type LocationBackend interface {
	// GetLocation retrieves the current location from the target node.
	GetLocation(ctx context.Context, req LocationRequest) (*LocationResponse, error)
	// Name returns the backend identifier (e.g. "node").
	Name() string
}

// LocationRequest holds parameters for a location get action.
type LocationRequest struct {
	NodeID          string `json:"node_id"`
	MaxAgeMs        int    `json:"max_age_ms,omitempty"`
	TimeoutMs       int    `json:"timeout_ms,omitempty"`
	DesiredAccuracy string `json:"desired_accuracy,omitempty"`
}

// LocationResponse holds the result of a location query.
type LocationResponse struct {
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	AccuracyMeters float64 `json:"accuracy_meters,omitempty"`
	AltitudeMeters float64 `json:"altitude_meters,omitempty"`
	SpeedMps       float64 `json:"speed_mps,omitempty"`
	HeadingDeg     float64 `json:"heading_deg,omitempty"`
	Timestamp      string  `json:"timestamp"`
	IsPrecise      bool    `json:"is_precise"`
	Source         string  `json:"source,omitempty"`
}
