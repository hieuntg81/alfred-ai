package memory

import (
	"context"
	"testing"

	"alfred-ai/internal/domain"
)

func TestNoopMemory(t *testing.T) {
	mem := NewNoopMemory()
	if mem == nil {
		t.Fatal("NewNoopMemory returned nil")
	}

	ctx := context.Background()

	if err := mem.Store(ctx, domain.MemoryEntry{Content: "test"}); err != nil {
		t.Errorf("Store: %v", err)
	}

	entries, err := mem.Query(ctx, "test", 10)
	if err != nil {
		t.Errorf("Query: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Query returned %d entries, want 0", len(entries))
	}

	if err := mem.Delete(ctx, "id"); err != nil {
		t.Errorf("Delete: %v", err)
	}

	result, err := mem.Curate(ctx, nil)
	if err != nil {
		t.Errorf("Curate: %v", err)
	}
	if result == nil {
		t.Error("Curate returned nil result")
	}

	if err := mem.Sync(ctx); err != nil {
		t.Errorf("Sync: %v", err)
	}

	if mem.Name() != "noop" {
		t.Errorf("Name() = %q, want %q", mem.Name(), "noop")
	}

	if !mem.IsAvailable() {
		t.Error("IsAvailable() = false, want true")
	}
}
