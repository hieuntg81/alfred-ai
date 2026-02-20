package memory

import (
	"context"
	"testing"

	"alfred-ai/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockMemory is a simple in-memory provider for testing.
type mockMemory struct {
	entries []domain.MemoryEntry
}

func (m *mockMemory) Store(_ context.Context, entry domain.MemoryEntry) error {
	m.entries = append(m.entries, entry)
	return nil
}

func (m *mockMemory) Query(_ context.Context, _ string, limit int) ([]domain.MemoryEntry, error) {
	if limit <= 0 || limit > len(m.entries) {
		limit = len(m.entries)
	}
	return m.entries[:limit], nil
}

func (m *mockMemory) Delete(_ context.Context, id string) error {
	for i, e := range m.entries {
		if e.ID == id {
			m.entries = append(m.entries[:i], m.entries[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *mockMemory) Curate(_ context.Context, _ []domain.Message) (*domain.CurateResult, error) {
	return &domain.CurateResult{}, nil
}

func (m *mockMemory) Sync(_ context.Context) error { return nil }
func (m *mockMemory) Name() string                 { return "mock" }
func (m *mockMemory) IsAvailable() bool             { return true }

func TestTenantScopedMemory_StoreAddsTenantID(t *testing.T) {
	inner := &mockMemory{}
	scoped := NewTenantScopedMemory(inner, "tenant-a")

	err := scoped.Store(context.Background(), domain.MemoryEntry{
		ID:      "e1",
		Content: "hello",
	})
	require.NoError(t, err)
	require.Len(t, inner.entries, 1)
	assert.Equal(t, "tenant-a", inner.entries[0].Metadata["tenant_id"])
}

func TestTenantScopedMemory_QueryFiltersByTenant(t *testing.T) {
	inner := &mockMemory{
		entries: []domain.MemoryEntry{
			{ID: "e1", Content: "a", Metadata: map[string]string{"tenant_id": "tenant-a"}},
			{ID: "e2", Content: "b", Metadata: map[string]string{"tenant_id": "tenant-b"}},
			{ID: "e3", Content: "c", Metadata: map[string]string{"tenant_id": "tenant-a"}},
			{ID: "e4", Content: "d", Metadata: map[string]string{}}, // no tenant (legacy)
		},
	}

	scoped := NewTenantScopedMemory(inner, "tenant-a")
	entries, err := scoped.Query(context.Background(), "", 100)
	require.NoError(t, err)

	// Should include e1 (tenant-a), e3 (tenant-a), and e4 (no tenant / legacy).
	assert.Len(t, entries, 3)
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	assert.Contains(t, ids, "e1")
	assert.Contains(t, ids, "e3")
	assert.Contains(t, ids, "e4")
	assert.NotContains(t, ids, "e2")
}
