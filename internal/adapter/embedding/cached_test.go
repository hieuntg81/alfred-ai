package embedding

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"alfred-ai/internal/domain"
)

// countingEmbedder tracks how many times Embed is called.
type countingEmbedder struct {
	calls atomic.Int64
	dims  int
}

func (e *countingEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls.Add(1)
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, e.dims)
		for j := range v {
			v[j] = float32(len(t)+i+j) / 100.0
		}
		out[i] = v
	}
	return out, nil
}

func (e *countingEmbedder) Dimensions() int { return e.dims }
func (e *countingEmbedder) Name() string    { return "counting" }

func TestCachedEmbedderHitMiss(t *testing.T) {
	inner := &countingEmbedder{dims: 3}
	cached := NewCachedEmbedder(inner, 10).(*CachedEmbedder)
	ctx := context.Background()

	// First call: miss.
	r1, err := cached.Embed(ctx, []string{"hello"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if inner.calls.Load() != 1 {
		t.Errorf("calls = %d, want 1 (miss)", inner.calls.Load())
	}

	// Second call: hit.
	r2, err := cached.Embed(ctx, []string{"hello"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if inner.calls.Load() != 1 {
		t.Errorf("calls = %d, want 1 (should be cached)", inner.calls.Load())
	}

	// Results should be identical.
	if len(r1) != 1 || len(r2) != 1 {
		t.Fatalf("result lengths: %d, %d", len(r1), len(r2))
	}
	for i := range r1[0] {
		if r1[0][i] != r2[0][i] {
			t.Errorf("r1[0][%d]=%f != r2[0][%d]=%f", i, r1[0][i], i, r2[0][i])
		}
	}
}

func TestCachedEmbedderBatchPassthrough(t *testing.T) {
	inner := &countingEmbedder{dims: 3}
	cached := NewCachedEmbedder(inner, 10).(*CachedEmbedder)
	ctx := context.Background()

	// Batch (len > 1) should pass through to inner every time.
	_, err := cached.Embed(ctx, []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if inner.calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", inner.calls.Load())
	}

	_, err = cached.Embed(ctx, []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if inner.calls.Load() != 2 {
		t.Errorf("calls = %d, want 2 (batch not cached)", inner.calls.Load())
	}
}

func TestCachedEmbedderEviction(t *testing.T) {
	inner := &countingEmbedder{dims: 2}
	cached := NewCachedEmbedder(inner, 3).(*CachedEmbedder)
	ctx := context.Background()

	// Fill cache with 3 entries.
	for i := 0; i < 3; i++ {
		cached.Embed(ctx, []string{fmt.Sprintf("text-%d", i)})
	}
	if inner.calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", inner.calls.Load())
	}

	// All 3 should be cached.
	for i := 0; i < 3; i++ {
		cached.Embed(ctx, []string{fmt.Sprintf("text-%d", i)})
	}
	if inner.calls.Load() != 3 {
		t.Errorf("calls = %d, want 3 (all cached)", inner.calls.Load())
	}

	// Add a 4th entry — should evict the LRU (text-0).
	cached.Embed(ctx, []string{"text-3"})
	if inner.calls.Load() != 4 {
		t.Errorf("calls = %d, want 4", inner.calls.Load())
	}

	// text-0 should now be evicted (it was LRU).
	cached.Embed(ctx, []string{"text-0"})
	if inner.calls.Load() != 5 {
		t.Errorf("calls = %d, want 5 (text-0 evicted)", inner.calls.Load())
	}

	// Re-inserting text-0 evicted text-1 (next LRU). text-2 should still be cached.
	cached.Embed(ctx, []string{"text-2"})
	if inner.calls.Load() != 5 {
		t.Errorf("calls = %d, want 5 (text-2 still cached)", inner.calls.Load())
	}
}

func TestCachedEmbedderLRUPromotion(t *testing.T) {
	inner := &countingEmbedder{dims: 2}
	cached := NewCachedEmbedder(inner, 3).(*CachedEmbedder)
	ctx := context.Background()

	// Insert a, b, c. Order: [a, b, c].
	cached.Embed(ctx, []string{"a"})
	cached.Embed(ctx, []string{"b"})
	cached.Embed(ctx, []string{"c"})

	// Access "a" to promote it. Order: [b, c, a].
	cached.Embed(ctx, []string{"a"})

	// Insert "d" — should evict "b" (now LRU), not "a".
	cached.Embed(ctx, []string{"d"})
	callsBefore := inner.calls.Load()

	// "a" should still be cached.
	cached.Embed(ctx, []string{"a"})
	if inner.calls.Load() != callsBefore {
		t.Error("'a' should still be cached after promotion")
	}

	// "b" should be evicted.
	cached.Embed(ctx, []string{"b"})
	if inner.calls.Load() != callsBefore+1 {
		t.Error("'b' should have been evicted")
	}
}

func TestCachedEmbedderConcurrency(t *testing.T) {
	inner := &countingEmbedder{dims: 3}
	cached := NewCachedEmbedder(inner, 100)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			text := fmt.Sprintf("concurrent-%d", n%10) // 10 unique keys
			for j := 0; j < 20; j++ {
				result, err := cached.Embed(ctx, []string{text})
				if err != nil {
					t.Errorf("Embed: %v", err)
					return
				}
				if len(result) != 1 || len(result[0]) != 3 {
					t.Errorf("unexpected result shape")
					return
				}
			}
		}(i)
	}
	wg.Wait()

	// Should have cached some calls.
	calls := inner.calls.Load()
	if calls >= 1000 {
		t.Errorf("expected cache hits to reduce calls, got %d", calls)
	}
}

func TestCachedEmbedderDelegation(t *testing.T) {
	inner := &countingEmbedder{dims: 384}
	cached := NewCachedEmbedder(inner, 10)

	if cached.Dimensions() != 384 {
		t.Errorf("Dimensions() = %d, want 384", cached.Dimensions())
	}
	if cached.Name() != "counting" {
		t.Errorf("Name() = %q, want %q", cached.Name(), "counting")
	}
}

func TestNewCachedEmbedderZeroSize(t *testing.T) {
	inner := &countingEmbedder{dims: 3}

	// maxSize 0 should return inner directly.
	result := NewCachedEmbedder(inner, 0)
	if result != inner {
		t.Error("expected inner to be returned directly when maxSize=0")
	}

	// Negative maxSize should also return inner directly.
	result = NewCachedEmbedder(inner, -1)
	if result != inner {
		t.Error("expected inner to be returned directly when maxSize<0")
	}
}

func TestCachedEmbedderEmptyInput(t *testing.T) {
	inner := &countingEmbedder{dims: 3}
	cached := NewCachedEmbedder(inner, 10)
	ctx := context.Background()

	// Empty slice: batch passthrough.
	result, err := cached.Embed(ctx, []string{})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d", len(result))
	}
}

// Compile-time interface check.
var _ domain.EmbeddingProvider = (*CachedEmbedder)(nil)
