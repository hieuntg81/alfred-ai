package tool

import (
	"context"
	"time"
)

// CanvasBackend abstracts canvas storage and lifecycle operations.
type CanvasBackend interface {
	// Create creates a new canvas with the given name and content in the session.
	Create(ctx context.Context, sessionID, name, content string) (*CanvasInfo, error)
	// Update replaces the content of an existing canvas.
	Update(ctx context.Context, sessionID, name, content string) (*CanvasInfo, error)
	// Read retrieves the content of a canvas by name.
	Read(ctx context.Context, sessionID, name string) (*CanvasContent, error)
	// Delete removes a canvas by name.
	Delete(ctx context.Context, sessionID, name string) error
	// List returns all canvases in the given session.
	List(ctx context.Context, sessionID string) ([]CanvasInfo, error)
	// Close releases backend resources.
	Close() error
	// Name returns the backend identifier (e.g. "local").
	Name() string
}

// CanvasInfo holds metadata about a canvas.
type CanvasInfo struct {
	Name      string    `json:"name"`
	SessionID string    `json:"session_id"`
	Size      int       `json:"size"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Path      string    `json:"-"` // internal only, not exposed to LLM
}

// CanvasContent holds a canvas's full content plus metadata.
type CanvasContent struct {
	CanvasInfo
	Content string `json:"content"`
}

// CanvasEvalRequest is published as an event payload for eval_js.
type CanvasEvalRequest struct {
	CanvasName string `json:"canvas_name"`
	Expression string `json:"expression"`
}
