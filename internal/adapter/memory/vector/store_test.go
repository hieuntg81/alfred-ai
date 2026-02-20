package vector

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"alfred-ai/internal/domain"
)

func newTestStore(t *testing.T, embedder domain.EmbeddingProvider) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(dbPath, embedder, slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStoreAndRetrieve(t *testing.T) {
	s := newTestStore(t, nil)
	ctx := context.Background()

	err := s.Store(ctx, domain.MemoryEntry{
		ID:      "e1",
		Content: "hello world",
		Tags:    []string{"greeting"},
	})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	results, err := s.Query(ctx, "hello", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].ID != "e1" {
		t.Errorf("ID = %q, want e1", results[0].ID)
	}
	if results[0].Content != "hello world" {
		t.Errorf("Content = %q", results[0].Content)
	}
}

func TestStoreEmptyID(t *testing.T) {
	s := newTestStore(t, nil)
	ctx := context.Background()

	err := s.Store(ctx, domain.MemoryEntry{
		Content: "auto id",
	})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	results, err := s.Query(ctx, "auto", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].ID == "" {
		t.Error("expected auto-generated ID")
	}
}

func TestStoreUpsert(t *testing.T) {
	s := newTestStore(t, nil)
	ctx := context.Background()

	s.Store(ctx, domain.MemoryEntry{ID: "u1", Content: "version 1"})
	s.Store(ctx, domain.MemoryEntry{ID: "u1", Content: "version 2"})

	results, err := s.Query(ctx, "version", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1 (upsert)", len(results))
	}
	if results[0].Content != "version 2" {
		t.Errorf("Content = %q, want 'version 2'", results[0].Content)
	}
}

func TestDelete(t *testing.T) {
	s := newTestStore(t, nil)
	ctx := context.Background()

	s.Store(ctx, domain.MemoryEntry{ID: "d1", Content: "to delete"})

	err := s.Delete(ctx, "d1")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	results, err := s.Query(ctx, "delete", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results len = %d, want 0 after delete", len(results))
	}
}

func TestDeleteNotFound(t *testing.T) {
	s := newTestStore(t, nil)
	err := s.Delete(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent delete")
	}
}

func TestQueryEmpty(t *testing.T) {
	s := newTestStore(t, nil)
	ctx := context.Background()

	// Store some entries.
	s.Store(ctx, domain.MemoryEntry{ID: "q1", Content: "first entry"})
	s.Store(ctx, domain.MemoryEntry{ID: "q2", Content: "second entry"})

	// Empty query should return recent entries.
	results, err := s.Query(ctx, "", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("results len = %d, want 2", len(results))
	}
}

func TestWithoutEmbedder(t *testing.T) {
	s := newTestStore(t, nil)
	ctx := context.Background()

	s.Store(ctx, domain.MemoryEntry{ID: "n1", Content: "no embeddings"})
	results, err := s.Query(ctx, "embeddings", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
}

func TestClose(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "close.db")
	s, err := New(dbPath, nil, slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// After close, operations should fail.
	err = s.Store(context.Background(), domain.MemoryEntry{ID: "x", Content: "fail"})
	if err == nil {
		t.Error("expected error after close")
	}
}

func TestMigrationIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "idem.db")

	s1, err := New(dbPath, nil, slog.Default())
	if err != nil {
		t.Fatalf("New #1: %v", err)
	}
	s1.Store(context.Background(), domain.MemoryEntry{ID: "m1", Content: "migrate test"})
	s1.Close()

	// Re-open: migration should be idempotent.
	s2, err := New(dbPath, nil, slog.Default())
	if err != nil {
		t.Fatalf("New #2: %v", err)
	}
	defer s2.Close()

	results, err := s2.Query(context.Background(), "migrate", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("results len = %d, want 1", len(results))
	}
}

func TestNameAndAvailable(t *testing.T) {
	s := newTestStore(t, nil)
	if s.Name() != "vector" {
		t.Errorf("Name() = %q, want vector", s.Name())
	}
	if !s.IsAvailable() {
		t.Error("IsAvailable() = false, want true")
	}
}

func TestCurateNoOp(t *testing.T) {
	s := newTestStore(t, nil)
	result, err := s.Curate(context.Background(), nil)
	if err != nil {
		t.Fatalf("Curate: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil CurateResult")
	}
}

func TestSyncNoOp(t *testing.T) {
	s := newTestStore(t, nil)
	if err := s.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

// TestConcurrentReadWrite verifies the store handles concurrent reads and writes
// without data races or deadlocks. Run with -race to detect Go-level races.
func TestConcurrentReadWrite(t *testing.T) {
	s := newTestStore(t, nil)
	ctx := context.Background()

	const (
		numWriters = 10
		numReaders = 10
		opsPerGo   = 20
	)

	// Seed some initial data.
	for i := 0; i < 5; i++ {
		s.Store(ctx, domain.MemoryEntry{
			ID:      fmt.Sprintf("seed-%d", i),
			Content: fmt.Sprintf("seed content %d for concurrent test", i),
			Tags:    []string{"seed"},
		})
	}

	var wg sync.WaitGroup

	// Launch writers.
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGo; i++ {
				entry := domain.MemoryEntry{
					ID:      fmt.Sprintf("w%d-%d", id, i),
					Content: fmt.Sprintf("writer %d entry %d with concurrent content", id, i),
					Tags:    []string{"concurrent", fmt.Sprintf("writer-%d", id)},
				}
				if err := s.Store(ctx, entry); err != nil {
					t.Errorf("writer %d op %d: Store: %v", id, i, err)
				}
			}
		}(w)
	}

	// Launch readers.
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			queries := []string{"concurrent", "seed", "content", "writer", ""}
			for i := 0; i < opsPerGo; i++ {
				q := queries[i%len(queries)]
				results, err := s.Query(ctx, q, 5)
				if err != nil {
					t.Errorf("reader %d op %d: Query: %v", id, i, err)
				}
				_ = results
			}
		}(r)
	}

	wg.Wait()

	// Verify data integrity: at least the seed entries should be queryable.
	results, err := s.Query(ctx, "seed", 10)
	if err != nil {
		t.Fatalf("final Query: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least some seed entries after concurrent operations")
	}
}

// --- StoreBatch tests ---

func TestStoreBatchBasic(t *testing.T) {
	s := newTestStore(t, nil)
	ctx := context.Background()

	entries := []domain.MemoryEntry{
		{Content: "batch entry one", Tags: []string{"batch"}},
		{Content: "batch entry two", Tags: []string{"batch"}},
		{Content: "batch entry three", Tags: []string{"batch"}},
	}

	err := s.StoreBatch(ctx, entries)
	if err != nil {
		t.Fatalf("StoreBatch: %v", err)
	}

	results, err := s.Query(ctx, "batch", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("results len = %d, want 3", len(results))
	}
}

func TestStoreBatchEmpty(t *testing.T) {
	s := newTestStore(t, nil)
	err := s.StoreBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("StoreBatch(nil): %v", err)
	}
	err = s.StoreBatch(context.Background(), []domain.MemoryEntry{})
	if err != nil {
		t.Fatalf("StoreBatch(empty): %v", err)
	}
}

func TestStoreBatchAutoID(t *testing.T) {
	s := newTestStore(t, nil)
	ctx := context.Background()

	entries := []domain.MemoryEntry{
		{Content: "no id one"},
		{Content: "no id two"},
	}

	err := s.StoreBatch(ctx, entries)
	if err != nil {
		t.Fatalf("StoreBatch: %v", err)
	}

	results, err := s.Query(ctx, "no id", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("results len = %d, want 2", len(results))
	}
	// Each entry should have a unique auto-generated ID.
	if results[0].ID == "" || results[1].ID == "" {
		t.Error("expected auto-generated IDs")
	}
	if results[0].ID == results[1].ID {
		t.Error("expected unique IDs, got duplicates")
	}
}

func TestStoreBatchUpsert(t *testing.T) {
	s := newTestStore(t, nil)
	ctx := context.Background()

	// Initial batch.
	entries := []domain.MemoryEntry{
		{ID: "b1", Content: "version 1"},
		{ID: "b2", Content: "version 1"},
	}
	err := s.StoreBatch(ctx, entries)
	if err != nil {
		t.Fatalf("StoreBatch: %v", err)
	}

	// Upsert with updated content.
	entries2 := []domain.MemoryEntry{
		{ID: "b1", Content: "version 2"},
		{ID: "b2", Content: "version 2"},
	}
	err = s.StoreBatch(ctx, entries2)
	if err != nil {
		t.Fatalf("StoreBatch upsert: %v", err)
	}

	results, err := s.Query(ctx, "version", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2 (upsert should not duplicate)", len(results))
	}
	for _, r := range results {
		if r.Content != "version 2" {
			t.Errorf("entry %q Content = %q, want 'version 2'", r.ID, r.Content)
		}
	}
}

func TestStoreBatchWithEmbeddings(t *testing.T) {
	emb := &countingEmbedder{inner: &mockEmbedder{dims: 3}}
	s := newTestStore(t, emb)
	ctx := context.Background()

	entries := []domain.MemoryEntry{
		{Content: "alpha"},
		{Content: "beta"},
		{Content: "gamma"},
		{Content: ""}, // empty content, should be skipped for embedding
		{Content: "delta"},
	}

	err := s.StoreBatch(ctx, entries)
	if err != nil {
		t.Fatalf("StoreBatch: %v", err)
	}

	// Should have made exactly 1 Embed call (batched), not 4 separate calls.
	if emb.calls != 1 {
		t.Errorf("Embed call count = %d, want 1 (batched)", emb.calls)
	}

	// The batch call should have included 4 texts (excluding empty content).
	if emb.lastBatchSize != 4 {
		t.Errorf("Embed batch size = %d, want 4", emb.lastBatchSize)
	}

	results, err := s.Query(ctx, "alpha", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected results after batch store with embeddings")
	}
}

func TestStoreBatchConcurrent(t *testing.T) {
	s := newTestStore(t, nil)
	ctx := context.Background()

	var wg sync.WaitGroup
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func(batch int) {
			defer wg.Done()
			entries := make([]domain.MemoryEntry, 10)
			for i := range entries {
				entries[i] = domain.MemoryEntry{
					ID:      fmt.Sprintf("batch%d-%d", batch, i),
					Content: fmt.Sprintf("concurrent batch %d entry %d content", batch, i),
				}
			}
			if err := s.StoreBatch(ctx, entries); err != nil {
				t.Errorf("StoreBatch (batch %d): %v", batch, err)
			}
		}(g)
	}
	wg.Wait()

	results, err := s.Query(ctx, "concurrent", 100)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 50 {
		t.Errorf("results len = %d, want 50", len(results))
	}
}

// countingEmbedder wraps an embedder and counts calls.
type countingEmbedder struct {
	inner         domain.EmbeddingProvider
	calls         int
	lastBatchSize int
}

func (e *countingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	e.calls++
	e.lastBatchSize = len(texts)
	return e.inner.Embed(ctx, texts)
}
func (e *countingEmbedder) Dimensions() int { return e.inner.Dimensions() }
func (e *countingEmbedder) Name() string    { return "counting" }

// Compile-time interface checks.
var _ domain.MemoryProvider = (*Store)(nil)
var _ domain.BatchStorer = (*Store)(nil)
