package memory

import (
	"context"
	"time"
)

// ByteRoverResult represents a single result from a ByteRover query.
type ByteRoverResult struct {
	ID        string            `json:"id"`
	Content   string            `json:"content"`
	Tags      []string          `json:"tags,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Score     float64           `json:"score"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// SyncStatus represents the current sync state with ByteRover.
type SyncStatus struct {
	LastSyncAt  time.Time `json:"last_sync_at"`
	PendingPush int       `json:"pending_push"`
	PendingPull int       `json:"pending_pull"`
	InSync      bool      `json:"in_sync"`
}

// ByteRoverClient is the interface for communicating with the ByteRover API.
type ByteRoverClient interface {
	// Authenticate validates credentials and returns a session token.
	Authenticate(ctx context.Context) error

	// WriteContext stores a knowledge entry.
	WriteContext(ctx context.Context, id, content string, tags []string, metadata map[string]string) error

	// ReadContext retrieves a knowledge entry by ID.
	ReadContext(ctx context.Context, id string) (*ByteRoverResult, error)

	// DeleteContext removes a knowledge entry by ID.
	DeleteContext(ctx context.Context, id string) error

	// Query searches for knowledge entries matching the query string.
	Query(ctx context.Context, query string, limit int) ([]ByteRoverResult, error)

	// Push uploads local entries to ByteRover.
	Push(ctx context.Context, entries []ByteRoverResult) error

	// Pull downloads remote entries from ByteRover.
	Pull(ctx context.Context, since time.Time) ([]ByteRoverResult, error)

	// SyncStatus returns the current sync state.
	SyncStatus(ctx context.Context) (*SyncStatus, error)
}
