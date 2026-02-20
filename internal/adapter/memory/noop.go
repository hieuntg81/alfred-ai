package memory

import (
	"context"

	"alfred-ai/internal/domain"
)

// NoopMemory is a placeholder that stores nothing and returns empty results.
type NoopMemory struct{}

// NewNoopMemory creates a noop memory provider.
func NewNoopMemory() *NoopMemory { return &NoopMemory{} }

func (n *NoopMemory) Store(_ context.Context, _ domain.MemoryEntry) error { return nil }

func (n *NoopMemory) Query(_ context.Context, _ string, _ int) ([]domain.MemoryEntry, error) {
	return nil, nil
}

func (n *NoopMemory) Delete(_ context.Context, _ string) error { return nil }

func (n *NoopMemory) Curate(_ context.Context, _ []domain.Message) (*domain.CurateResult, error) {
	return &domain.CurateResult{}, nil
}

func (n *NoopMemory) Sync(_ context.Context) error { return nil }
func (n *NoopMemory) Name() string                 { return "noop" }
func (n *NoopMemory) IsAvailable() bool            { return true }
