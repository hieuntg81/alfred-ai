package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"alfred-ai/internal/domain"
)

// CachedMemory wraps a MemoryProvider with a TTL-based query cache.
// Cache is invalidated on Store, StoreBatch, Delete, and Curate.
type CachedMemory struct {
	inner domain.MemoryProvider
	ttl   time.Duration
	mu    sync.RWMutex
	cache map[string]cachedResult
}

type cachedResult struct {
	entries   []domain.MemoryEntry
	expiresAt time.Time
}

// NewCachedMemory wraps inner with a query cache using the given TTL.
func NewCachedMemory(inner domain.MemoryProvider, ttl time.Duration) *CachedMemory {
	return &CachedMemory{
		inner: inner,
		ttl:   ttl,
		cache: make(map[string]cachedResult),
	}
}

func (c *CachedMemory) Store(ctx context.Context, entry domain.MemoryEntry) error {
	err := c.inner.Store(ctx, entry)
	if err == nil {
		c.invalidate()
	}
	return err
}

// StoreBatch delegates to the inner provider's StoreBatch if supported,
// otherwise falls back to sequential Store calls.
func (c *CachedMemory) StoreBatch(ctx context.Context, entries []domain.MemoryEntry) error {
	if bs, ok := c.inner.(domain.BatchStorer); ok {
		err := bs.StoreBatch(ctx, entries)
		if err == nil {
			c.invalidate()
		}
		return err
	}
	for _, e := range entries {
		if err := c.inner.Store(ctx, e); err != nil {
			return err
		}
	}
	c.invalidate()
	return nil
}

func (c *CachedMemory) Query(ctx context.Context, query string, limit int) ([]domain.MemoryEntry, error) {
	key := cacheKey(query, limit)

	c.mu.RLock()
	if cached, ok := c.cache[key]; ok && time.Now().Before(cached.expiresAt) {
		c.mu.RUnlock()
		return cached.entries, nil
	}
	c.mu.RUnlock()

	entries, err := c.inner.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache[key] = cachedResult{entries: entries, expiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()

	return entries, nil
}

func (c *CachedMemory) Delete(ctx context.Context, id string) error {
	err := c.inner.Delete(ctx, id)
	if err == nil {
		c.invalidate()
	}
	return err
}

func (c *CachedMemory) Curate(ctx context.Context, messages []domain.Message) (*domain.CurateResult, error) {
	result, err := c.inner.Curate(ctx, messages)
	if err == nil {
		c.invalidate()
	}
	return result, err
}

func (c *CachedMemory) Sync(ctx context.Context) error        { return c.inner.Sync(ctx) }
func (c *CachedMemory) Name() string                          { return c.inner.Name() }
func (c *CachedMemory) IsAvailable() bool                     { return c.inner.IsAvailable() }

// invalidate clears the entire cache.
func (c *CachedMemory) invalidate() {
	c.mu.Lock()
	c.cache = make(map[string]cachedResult)
	c.mu.Unlock()
}

// CacheSize returns the number of cached entries (for testing).
func (c *CachedMemory) CacheSize() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}

func cacheKey(query string, limit int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", query, limit)))
	return hex.EncodeToString(h[:16]) // 128-bit key, sufficient for cache dedup
}
