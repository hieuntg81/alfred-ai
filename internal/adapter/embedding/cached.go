package embedding

import (
	"container/list"
	"context"
	"hash/fnv"
	"sync"

	"alfred-ai/internal/domain"
)

// lruEntry pairs a hash key with its embedding vector in the LRU list.
type lruEntry struct {
	key uint64
	vec []float32
}

// CachedEmbedder wraps a domain.EmbeddingProvider with an LRU cache for
// single-text queries (search queries). Batch calls pass through uncached.
type CachedEmbedder struct {
	inner   domain.EmbeddingProvider
	maxSize int

	mu    sync.RWMutex
	cache map[uint64]*list.Element // hash → list element
	order *list.List               // LRU order: most-recently-used at back
}

// NewCachedEmbedder wraps inner with an LRU embedding cache of maxSize entries.
// If maxSize <= 0, the inner provider is returned directly (no caching).
func NewCachedEmbedder(inner domain.EmbeddingProvider, maxSize int) domain.EmbeddingProvider {
	if maxSize <= 0 {
		return inner
	}
	return &CachedEmbedder{
		inner:   inner,
		maxSize: maxSize,
		cache:   make(map[uint64]*list.Element, maxSize),
		order:   list.New(),
	}
}

// Embed implements domain.EmbeddingProvider.
// Single-text calls are cached; batch (len > 1) calls pass through.
func (c *CachedEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) != 1 {
		return c.inner.Embed(ctx, texts)
	}

	key := hashText(texts[0])

	// Fast path: cache hit.
	c.mu.RLock()
	elem, ok := c.cache[key]
	c.mu.RUnlock()

	if ok {
		// Promote in LRU order (needs write lock).
		c.mu.Lock()
		c.order.MoveToBack(elem)
		c.mu.Unlock()
		return [][]float32{elem.Value.(*lruEntry).vec}, nil
	}

	// Cache miss: embed via inner provider.
	result, err := c.inner.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return result, nil
	}

	// Store in cache.
	c.mu.Lock()
	c.put(key, result[0])
	c.mu.Unlock()

	return result, nil
}

// Dimensions implements domain.EmbeddingProvider.
func (c *CachedEmbedder) Dimensions() int { return c.inner.Dimensions() }

// Name implements domain.EmbeddingProvider.
func (c *CachedEmbedder) Name() string { return c.inner.Name() }

// hashText returns an FNV-1a hash of the input text.
func hashText(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

// put inserts a key/value into the cache, evicting the LRU entry if at capacity.
// Caller must hold c.mu (write lock).
func (c *CachedEmbedder) put(key uint64, vec []float32) {
	if elem, exists := c.cache[key]; exists {
		c.order.MoveToBack(elem)
		elem.Value.(*lruEntry).vec = vec
		return
	}

	if c.order.Len() >= c.maxSize {
		oldest := c.order.Front()
		c.order.Remove(oldest)
		delete(c.cache, oldest.Value.(*lruEntry).key)
	}

	entry := &lruEntry{key: key, vec: vec}
	elem := c.order.PushBack(entry)
	c.cache[key] = elem
}

// Compile-time interface check.
var _ domain.EmbeddingProvider = (*CachedEmbedder)(nil)
