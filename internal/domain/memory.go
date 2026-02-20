package domain

import (
	"context"
	"time"
)

// MemoryEntry represents a piece of stored knowledge.
type MemoryEntry struct {
	ID        string            `json:"id"`
	Content   string            `json:"content"`
	Tags      []string          `json:"tags,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// CurateResult holds the outcome of a curation operation.
type CurateResult struct {
	Stored   int      `json:"stored"`
	Skipped  int      `json:"skipped"`
	Keywords []string `json:"keywords,omitempty"`
	Summary  string   `json:"summary,omitempty"`
}

// MemoryProvider is the interface for long-term memory backends.
type MemoryProvider interface {
	Store(ctx context.Context, entry MemoryEntry) error
	Query(ctx context.Context, query string, limit int) ([]MemoryEntry, error)
	Delete(ctx context.Context, id string) error
	Curate(ctx context.Context, messages []Message) (*CurateResult, error)
	Sync(ctx context.Context) error
	Name() string
	IsAvailable() bool
}

// BatchStorer is an optional interface that MemoryProvider implementations
// can support for efficient bulk writes with a single embedding call.
type BatchStorer interface {
	StoreBatch(ctx context.Context, entries []MemoryEntry) error
}
