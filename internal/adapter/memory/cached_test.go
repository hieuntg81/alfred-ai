package memory

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

// trackingMemory wraps a basic memory provider and counts Query calls.
type trackingMemory struct {
	mu         sync.Mutex
	entries    []domain.MemoryEntry
	queryCalls atomic.Int32
}

func (m *trackingMemory) Store(_ context.Context, entry domain.MemoryEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, entry)
	return nil
}

func (m *trackingMemory) Query(_ context.Context, _ string, limit int) ([]domain.MemoryEntry, error) {
	m.queryCalls.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit > len(m.entries) {
		limit = len(m.entries)
	}
	result := make([]domain.MemoryEntry, limit)
	copy(result, m.entries[:limit])
	return result, nil
}

func (m *trackingMemory) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, e := range m.entries {
		if e.ID == id {
			m.entries = append(m.entries[:i], m.entries[i+1:]...)
			return nil
		}
	}
	return domain.ErrMemoryDelete
}

func (m *trackingMemory) Curate(_ context.Context, _ []domain.Message) (*domain.CurateResult, error) {
	return &domain.CurateResult{}, nil
}
func (m *trackingMemory) Sync(_ context.Context) error { return nil }
func (m *trackingMemory) Name() string                 { return "tracking" }
func (m *trackingMemory) IsAvailable() bool            { return true }

func TestCachedQueryHit(t *testing.T) {
	inner := &trackingMemory{
		entries: []domain.MemoryEntry{{ID: "1", Content: "hello"}},
	}
	cached := NewCachedMemory(inner, 5*time.Second)
	ctx := context.Background()

	// First call: cache miss → calls inner.
	r1, err := cached.Query(ctx, "hello", 10)
	if err != nil {
		t.Fatalf("Query 1: %v", err)
	}
	if len(r1) != 1 {
		t.Errorf("Query 1 len = %d, want 1", len(r1))
	}

	// Second call: cache hit → should NOT call inner again.
	r2, err := cached.Query(ctx, "hello", 10)
	if err != nil {
		t.Fatalf("Query 2: %v", err)
	}
	if len(r2) != 1 {
		t.Errorf("Query 2 len = %d, want 1", len(r2))
	}

	if inner.queryCalls.Load() != 1 {
		t.Errorf("inner Query calls = %d, want 1 (cache hit should skip inner)", inner.queryCalls.Load())
	}
}

func TestCachedQueryDifferentKeys(t *testing.T) {
	inner := &trackingMemory{
		entries: []domain.MemoryEntry{{ID: "1", Content: "hello"}},
	}
	cached := NewCachedMemory(inner, 5*time.Second)
	ctx := context.Background()

	// Different query strings = different cache keys.
	cached.Query(ctx, "hello", 10)
	cached.Query(ctx, "world", 10)
	cached.Query(ctx, "hello", 5) // different limit = different key

	if inner.queryCalls.Load() != 3 {
		t.Errorf("inner Query calls = %d, want 3 (each unique key is a miss)", inner.queryCalls.Load())
	}
}

func TestCachedQueryExpiration(t *testing.T) {
	inner := &trackingMemory{
		entries: []domain.MemoryEntry{{ID: "1", Content: "hello"}},
	}
	cached := NewCachedMemory(inner, 50*time.Millisecond) // very short TTL
	ctx := context.Background()

	cached.Query(ctx, "hello", 10)
	if inner.queryCalls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", inner.queryCalls.Load())
	}

	// Wait for TTL to expire.
	time.Sleep(60 * time.Millisecond)

	cached.Query(ctx, "hello", 10)
	if inner.queryCalls.Load() != 2 {
		t.Errorf("inner Query calls = %d, want 2 (expired cache → new call)", inner.queryCalls.Load())
	}
}

func TestCachedInvalidateOnStore(t *testing.T) {
	inner := &trackingMemory{
		entries: []domain.MemoryEntry{{ID: "1", Content: "hello"}},
	}
	cached := NewCachedMemory(inner, 5*time.Second)
	ctx := context.Background()

	// Fill cache.
	cached.Query(ctx, "hello", 10)
	if cached.CacheSize() != 1 {
		t.Fatalf("cache size = %d, want 1", cached.CacheSize())
	}

	// Store invalidates cache.
	cached.Store(ctx, domain.MemoryEntry{ID: "2", Content: "world"})
	if cached.CacheSize() != 0 {
		t.Errorf("cache size after Store = %d, want 0 (invalidated)", cached.CacheSize())
	}

	// Next query should hit inner.
	cached.Query(ctx, "hello", 10)
	if inner.queryCalls.Load() != 2 {
		t.Errorf("inner Query calls = %d, want 2", inner.queryCalls.Load())
	}
}

func TestCachedInvalidateOnDelete(t *testing.T) {
	inner := &trackingMemory{
		entries: []domain.MemoryEntry{{ID: "1", Content: "hello"}},
	}
	cached := NewCachedMemory(inner, 5*time.Second)
	ctx := context.Background()

	cached.Query(ctx, "hello", 10)
	cached.Delete(ctx, "1")
	if cached.CacheSize() != 0 {
		t.Errorf("cache size after Delete = %d, want 0", cached.CacheSize())
	}
}

func TestCachedInvalidateOnCurate(t *testing.T) {
	inner := &trackingMemory{
		entries: []domain.MemoryEntry{{ID: "1", Content: "hello"}},
	}
	cached := NewCachedMemory(inner, 5*time.Second)
	ctx := context.Background()

	cached.Query(ctx, "hello", 10)
	cached.Curate(ctx, nil)
	if cached.CacheSize() != 0 {
		t.Errorf("cache size after Curate = %d, want 0", cached.CacheSize())
	}
}

func TestCachedStoreBatchDelegation(t *testing.T) {
	inner := &trackingMemory{}
	cached := NewCachedMemory(inner, 5*time.Second)
	ctx := context.Background()

	entries := []domain.MemoryEntry{
		{ID: "b1", Content: "one"},
		{ID: "b2", Content: "two"},
	}

	// trackingMemory doesn't implement BatchStorer, so it falls back to Store loop.
	err := cached.StoreBatch(ctx, entries)
	if err != nil {
		t.Fatalf("StoreBatch: %v", err)
	}

	inner.mu.Lock()
	count := len(inner.entries)
	inner.mu.Unlock()
	if count != 2 {
		t.Errorf("inner entries = %d, want 2", count)
	}
}

func TestCachedConcurrentAccess(t *testing.T) {
	inner := &trackingMemory{
		entries: []domain.MemoryEntry{{ID: "1", Content: "data"}},
	}
	cached := NewCachedMemory(inner, 5*time.Second)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			q := fmt.Sprintf("query_%d", idx%5) // 5 unique keys
			_, err := cached.Query(ctx, q, 10)
			if err != nil {
				t.Errorf("Query(%q): %v", q, err)
			}
		}(i)
	}
	wg.Wait()

	// With 5 unique keys and caching, inner should have been called at most 5 times
	// (one per unique key), though race conditions might cause a few extra.
	calls := inner.queryCalls.Load()
	if calls > 10 {
		t.Errorf("inner Query calls = %d, expected <= 10 (5 unique keys, minor races ok)", calls)
	}
}

func TestCachedDelegatesMethods(t *testing.T) {
	inner := &trackingMemory{}
	cached := NewCachedMemory(inner, time.Second)

	if cached.Name() != "tracking" {
		t.Errorf("Name() = %q, want tracking", cached.Name())
	}
	if !cached.IsAvailable() {
		t.Error("IsAvailable() = false, want true")
	}
	if err := cached.Sync(context.Background()); err != nil {
		t.Errorf("Sync: %v", err)
	}
}

// Compile-time interface check.
var _ domain.MemoryProvider = (*CachedMemory)(nil)
var _ domain.BatchStorer = (*CachedMemory)(nil)
