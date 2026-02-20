package memory

import (
	"context"

	"alfred-ai/internal/domain"
)

// TenantScopedMemory wraps a MemoryProvider and scopes all operations to a
// specific tenant by injecting the tenant ID into the context.
// This ensures memory queries and stores are isolated per tenant.
type TenantScopedMemory struct {
	inner    domain.MemoryProvider
	tenantID string
}

// NewTenantScopedMemory creates a memory provider scoped to the given tenant.
func NewTenantScopedMemory(inner domain.MemoryProvider, tenantID string) *TenantScopedMemory {
	return &TenantScopedMemory{inner: inner, tenantID: tenantID}
}

func (t *TenantScopedMemory) scopedCtx(ctx context.Context) context.Context {
	return domain.ContextWithTenantID(ctx, t.tenantID)
}

func (t *TenantScopedMemory) Store(ctx context.Context, entry domain.MemoryEntry) error {
	// Tag entries with tenant ID for isolation.
	if entry.Metadata == nil {
		entry.Metadata = make(map[string]string)
	}
	entry.Metadata["tenant_id"] = t.tenantID
	return t.inner.Store(t.scopedCtx(ctx), entry)
}

func (t *TenantScopedMemory) Query(ctx context.Context, query string, limit int) ([]domain.MemoryEntry, error) {
	entries, err := t.inner.Query(t.scopedCtx(ctx), query, limit)
	if err != nil {
		return nil, err
	}

	// Filter results to only entries belonging to this tenant.
	filtered := make([]domain.MemoryEntry, 0, len(entries))
	for _, e := range entries {
		if e.Metadata["tenant_id"] == t.tenantID || e.Metadata["tenant_id"] == "" {
			filtered = append(filtered, e)
		}
	}
	return filtered, nil
}

func (t *TenantScopedMemory) Delete(ctx context.Context, id string) error {
	return t.inner.Delete(t.scopedCtx(ctx), id)
}

func (t *TenantScopedMemory) Curate(ctx context.Context, messages []domain.Message) (*domain.CurateResult, error) {
	return t.inner.Curate(t.scopedCtx(ctx), messages)
}

func (t *TenantScopedMemory) Sync(ctx context.Context) error {
	return t.inner.Sync(t.scopedCtx(ctx))
}

func (t *TenantScopedMemory) Name() string      { return t.inner.Name() }
func (t *TenantScopedMemory) IsAvailable() bool  { return t.inner.IsAvailable() }
