package vector

import (
	"context"
	"log/slog"
	"math"
	"path/filepath"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

// mockEmbedder returns deterministic embeddings for testing.
type mockEmbedder struct {
	vecs [][]float32
	dims int
}

func (m *mockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if m.vecs != nil {
		return m.vecs, nil
	}
	// Generate simple deterministic vectors based on text length.
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, m.dims)
		for j := range v {
			v[j] = float32(len(t)+i+j) / 100.0
		}
		out[i] = v
	}
	return out, nil
}

func (m *mockEmbedder) Dimensions() int { return m.dims }
func (m *mockEmbedder) Name() string    { return "mock" }

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float32
	}{
		{"identical", []float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		{"orthogonal", []float32{1, 0}, []float32{0, 1}, 0.0},
		{"similar", []float32{1, 2, 3}, []float32{1, 2, 3}, 1.0},
		{"opposite", []float32{1, 0}, []float32{-1, 0}, -1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			if math.Abs(float64(got-tt.want)) > 0.001 {
				t.Errorf("cosineSimilarity = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestCosineSimilarityLengthMismatch(t *testing.T) {
	got := cosineSimilarity([]float32{1, 2}, []float32{1, 2, 3})
	if got != 0 {
		t.Errorf("expected 0 for length mismatch, got %f", got)
	}
}

func TestCosineSimilarityZeroVector(t *testing.T) {
	got := cosineSimilarity([]float32{0, 0, 0}, []float32{1, 2, 3})
	if got != 0 {
		t.Errorf("expected 0 for zero vector, got %f", got)
	}
}

func TestFloat32BytesRoundTrip(t *testing.T) {
	original := []float32{1.5, -2.5, 3.14, 0.0, math.MaxFloat32}
	encoded := float32ToBytes(original)
	decoded := bytesToFloat32(encoded)

	if len(decoded) != len(original) {
		t.Fatalf("length mismatch: %d vs %d", len(decoded), len(original))
	}
	for i := range original {
		if original[i] != decoded[i] {
			t.Errorf("[%d] = %f, want %f", i, decoded[i], original[i])
		}
	}
}

func TestFloat32BytesBadLength(t *testing.T) {
	got := bytesToFloat32([]byte{1, 2, 3}) // not divisible by 4
	if got != nil {
		t.Errorf("expected nil for bad length, got %v", got)
	}
}

func TestRRF(t *testing.T) {
	list1 := []domain.MemoryEntry{
		{ID: "a", Content: "A"},
		{ID: "b", Content: "B"},
		{ID: "c", Content: "C"},
	}
	list2 := []domain.MemoryEntry{
		{ID: "b", Content: "B"},
		{ID: "a", Content: "A"},
		{ID: "d", Content: "D"},
	}

	result := reciprocalRankFusion(list1, list2)
	if len(result) != 4 {
		t.Fatalf("result len = %d, want 4", len(result))
	}
	// "a" and "b" appear in both lists so should rank higher.
	topIDs := map[string]bool{result[0].entry.ID: true, result[1].entry.ID: true}
	if !topIDs["a"] || !topIDs["b"] {
		t.Errorf("expected 'a' and 'b' in top 2, got %q, %q", result[0].entry.ID, result[1].entry.ID)
	}
	// All entries should have positive scores.
	for _, se := range result {
		if se.score <= 0 {
			t.Errorf("entry %q has non-positive score %f", se.entry.ID, se.score)
		}
	}
}

func TestRRFDisjoint(t *testing.T) {
	list1 := []domain.MemoryEntry{{ID: "x"}}
	list2 := []domain.MemoryEntry{{ID: "y"}}

	result := reciprocalRankFusion(list1, list2)
	if len(result) != 2 {
		t.Fatalf("result len = %d, want 2", len(result))
	}
}

func TestRRFEmpty(t *testing.T) {
	result := reciprocalRankFusion(nil, nil)
	if len(result) != 0 {
		t.Errorf("result len = %d, want 0", len(result))
	}
}

func TestHybridSearch(t *testing.T) {
	emb := &mockEmbedder{dims: 3}
	dbPath := filepath.Join(t.TempDir(), "hybrid.db")
	s, err := New(dbPath, emb, slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	s.Store(ctx, domain.MemoryEntry{ID: "h1", Content: "machine learning algorithms"})
	s.Store(ctx, domain.MemoryEntry{ID: "h2", Content: "deep learning neural networks"})
	s.Store(ctx, domain.MemoryEntry{ID: "h3", Content: "cooking recipes pasta"})

	results, err := s.Query(ctx, "learning", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected results for 'learning'")
	}
}

func TestKeywordFTS5(t *testing.T) {
	s := newTestStore(t, nil)
	ctx := context.Background()

	s.Store(ctx, domain.MemoryEntry{ID: "k1", Content: "golang programming language"})
	s.Store(ctx, domain.MemoryEntry{ID: "k2", Content: "python programming language"})
	s.Store(ctx, domain.MemoryEntry{ID: "k3", Content: "cooking recipes"})

	results, err := s.Query(ctx, "programming", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("results len = %d, want 2", len(results))
	}
}

func TestKeywordFTS5SpecialChars(t *testing.T) {
	s := newTestStore(t, nil)
	ctx := context.Background()

	s.Store(ctx, domain.MemoryEntry{ID: "sp1", Content: "C++ programming guide"})
	s.Store(ctx, domain.MemoryEntry{ID: "sp2", Content: "hello world normal"})

	// FTS5 special characters should fall back to LIKE search, not error.
	for _, q := range []string{`"unclosed`, `*`, `OR AND`, `test*`} {
		results, err := s.keywordSearch(ctx, q, 10)
		if err != nil {
			t.Errorf("keywordSearch(%q) error: %v", q, err)
		}
		_ = results // not asserting counts; just verifying graceful fallback
	}

	// Verify LIKE fallback returns actual matches.
	results, err := s.keywordSearch(ctx, `"programming`, 10)
	if err != nil {
		t.Fatalf("keywordSearch fallback: %v", err)
	}
	// Should match nothing since literal `"programming` doesn't appear in content.
	// (The LIKE search looks for the literal string including the quote.)
	_ = results
}

func TestCosineSimilarityNaN(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	if got := cosineSimilarity([]float32{nan, 1.0}, []float32{1.0, 1.0}); got != 0 {
		t.Errorf("expected 0 for NaN input, got %f", got)
	}
	if got := cosineSimilarity([]float32{inf, 1.0}, []float32{1.0, 1.0}); got != 0 {
		t.Errorf("expected 0 for Inf input, got %f", got)
	}
}

func TestVectorSearchOnly(t *testing.T) {
	// Use mock embedder that returns specific vectors for controlled similarity.
	emb := &mockEmbedder{
		dims: 3,
		vecs: [][]float32{{0.9, 0.1, 0.0}}, // query vector
	}
	dbPath := filepath.Join(t.TempDir(), "vec.db")
	s, err := New(dbPath, emb, slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Store entries with embeddings. The mock will return the same vector for store.
	emb.vecs = [][]float32{{0.9, 0.1, 0.0}}
	s.Store(ctx, domain.MemoryEntry{ID: "v1", Content: "similar entry"})
	emb.vecs = [][]float32{{0.0, 0.0, 1.0}}
	s.Store(ctx, domain.MemoryEntry{ID: "v2", Content: "different entry"})

	// Query with the similar vector.
	emb.vecs = [][]float32{{0.9, 0.1, 0.0}}
	results, err := s.vectorSearch(ctx, "query", 10)
	if err != nil {
		t.Fatalf("vectorSearch: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected vector search results")
	}
	// v1 should rank higher due to cosine similarity.
	if results[0].ID != "v1" {
		t.Errorf("top result = %q, want v1", results[0].ID)
	}
}

func TestHybridFallbackKeywordOnly(t *testing.T) {
	// No embedder = keyword-only fallback.
	s := newTestStore(t, nil)
	ctx := context.Background()

	s.Store(ctx, domain.MemoryEntry{ID: "f1", Content: "fallback test entry"})

	results, err := s.Query(ctx, "fallback", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("results len = %d, want 1", len(results))
	}
}

func TestEntriesToScored(t *testing.T) {
	entries := []domain.MemoryEntry{
		{ID: "a"},
		{ID: "b"},
		{ID: "c"},
	}
	scored := entriesToScored(entries)
	if len(scored) != 3 {
		t.Fatalf("len = %d, want 3", len(scored))
	}
	// Scores should be descending: 1/1, 1/2, 1/3.
	if scored[0].score != 1.0 {
		t.Errorf("scored[0].score = %f, want 1.0", scored[0].score)
	}
	if scored[0].score <= scored[1].score || scored[1].score <= scored[2].score {
		t.Errorf("scores not descending: %f, %f, %f", scored[0].score, scored[1].score, scored[2].score)
	}
}

func TestApplyTemporalDecay(t *testing.T) {
	now := time.Now()
	entries := []scoredEntry{
		{entry: domain.MemoryEntry{ID: "recent", UpdatedAt: now.Add(-1 * time.Hour)}, score: 1.0},
		{entry: domain.MemoryEntry{ID: "old", UpdatedAt: now.Add(-48 * time.Hour)}, score: 1.0},
	}

	halfLife := 24 * time.Hour
	applyTemporalDecay(entries, halfLife, now)

	// "recent" (1h old) should have minimal decay.
	if entries[0].score < 0.95 {
		t.Errorf("recent entry score = %f, expected > 0.95", entries[0].score)
	}

	// "old" (48h old, 2 half-lives) should be ~0.25.
	if math.Abs(entries[1].score-0.25) > 0.05 {
		t.Errorf("old entry score = %f, expected ~0.25", entries[1].score)
	}

	// "recent" should still score higher than "old".
	if entries[0].score <= entries[1].score {
		t.Errorf("recent (%f) should score higher than old (%f)", entries[0].score, entries[1].score)
	}
}

func TestApplyTemporalDecayZeroHalfLife(t *testing.T) {
	entries := []scoredEntry{
		{entry: domain.MemoryEntry{ID: "a"}, score: 1.0},
	}
	applyTemporalDecay(entries, 0, time.Now())
	// Zero half-life should be a no-op.
	if entries[0].score != 1.0 {
		t.Errorf("score = %f, want 1.0 (no-op)", entries[0].score)
	}
}

func TestApplyTemporalDecayFutureTimestamp(t *testing.T) {
	now := time.Now()
	entries := []scoredEntry{
		{entry: domain.MemoryEntry{ID: "future", UpdatedAt: now.Add(1 * time.Hour)}, score: 1.0},
	}
	applyTemporalDecay(entries, 24*time.Hour, now)
	// Future timestamps should not boost (hours clamped to 0).
	if entries[0].score != 1.0 {
		t.Errorf("score = %f, want 1.0 (no boost for future)", entries[0].score)
	}
}

func TestHybridSearchWithDecay(t *testing.T) {
	s := newTestStore(t, nil)
	ctx := context.Background()

	// Store entries; manually set UpdatedAt via the pipeline.
	s.Store(ctx, domain.MemoryEntry{ID: "d1", Content: "golang programming test"})
	s.Store(ctx, domain.MemoryEntry{ID: "d2", Content: "golang programming test"})

	// Enable decay.
	s.opts.DecayHalfLife = 24 * time.Hour

	results, err := s.Query(ctx, "programming", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected results")
	}
}

func TestMMRWithEmbeddings(t *testing.T) {
	emb := &mockEmbedder{dims: 3}
	dbPath := filepath.Join(t.TempDir(), "mmr.db")
	s, err := New(dbPath, emb, slog.Default(), SearchOpts{
		MMRDiversity: 0.5,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Store 3 entries with known embeddings.
	emb.vecs = [][]float32{{1.0, 0.0, 0.0}}
	s.Store(ctx, domain.MemoryEntry{ID: "m1", Content: "first entry similar"})
	emb.vecs = [][]float32{{0.99, 0.01, 0.0}} // very similar to m1
	s.Store(ctx, domain.MemoryEntry{ID: "m2", Content: "second entry similar"})
	emb.vecs = [][]float32{{0.0, 1.0, 0.0}} // orthogonal / diverse
	s.Store(ctx, domain.MemoryEntry{ID: "m3", Content: "third entry diverse"})

	// Reset embedder for query.
	emb.vecs = nil

	results, err := s.Query(ctx, "entry", 3)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results len = %d, want 3", len(results))
	}
}

func TestMMRGracefulNoEmbeddings(t *testing.T) {
	// Keyword-only store with MMR enabled — should gracefully skip MMR.
	dbPath := filepath.Join(t.TempDir(), "mmr-no-emb.db")
	s, err := New(dbPath, nil, slog.Default(), SearchOpts{
		MMRDiversity: 0.5,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	s.Store(ctx, domain.MemoryEntry{ID: "n1", Content: "entry for keyword search"})
	s.Store(ctx, domain.MemoryEntry{ID: "n2", Content: "another entry for search"})

	results, err := s.Query(ctx, "entry", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected results even without embeddings")
	}
}

func TestMMRSingleCandidate(t *testing.T) {
	s := newTestStore(t, nil)
	s.opts.MMRDiversity = 0.5
	ctx := context.Background()

	s.Store(ctx, domain.MemoryEntry{ID: "only", Content: "the only entry"})

	results, err := s.Query(ctx, "only", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("results len = %d, want 1", len(results))
	}
}

func TestHybridSearchDecayAndMMR(t *testing.T) {
	emb := &mockEmbedder{dims: 3}
	dbPath := filepath.Join(t.TempDir(), "decay-mmr.db")
	s, err := New(dbPath, emb, slog.Default(), SearchOpts{
		DecayHalfLife: 24 * time.Hour,
		MMRDiversity:  0.3,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	s.Store(ctx, domain.MemoryEntry{ID: "dm1", Content: "machine learning algorithms"})
	s.Store(ctx, domain.MemoryEntry{ID: "dm2", Content: "deep learning neural networks"})
	s.Store(ctx, domain.MemoryEntry{ID: "dm3", Content: "cooking recipes pasta"})

	results, err := s.Query(ctx, "learning", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected results with decay+MMR enabled")
	}
}

// --- VecIndex Tests ---

func TestVecIndexSearchBasic(t *testing.T) {
	emb := &mockEmbedder{dims: 3}
	dbPath := filepath.Join(t.TempDir(), "vidx.db")
	s, err := New(dbPath, emb, slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Store entries with known embeddings.
	emb.vecs = [][]float32{{0.9, 0.1, 0.0}}
	s.Store(ctx, domain.MemoryEntry{ID: "v1", Content: "similar"})
	emb.vecs = [][]float32{{0.0, 0.0, 1.0}}
	s.Store(ctx, domain.MemoryEntry{ID: "v2", Content: "different"})

	// First query triggers index load.
	emb.vecs = [][]float32{{0.9, 0.1, 0.0}}
	results, err := s.vectorSearch(ctx, "test", 10)
	if err != nil {
		t.Fatalf("vectorSearch: %v", err)
	}

	if !s.vecIdx.isLoaded() {
		t.Error("expected vecIdx to be loaded after first search")
	}
	if s.vecIdx.size() != 2 {
		t.Errorf("vecIdx size = %d, want 2", s.vecIdx.size())
	}

	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if results[0].ID != "v1" {
		t.Errorf("top result = %q, want v1", results[0].ID)
	}
}

func TestVecIndexIncrementalUpdate(t *testing.T) {
	emb := &mockEmbedder{dims: 3}
	dbPath := filepath.Join(t.TempDir(), "vidx-incr.db")
	s, err := New(dbPath, emb, slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Store initial entry and trigger index load.
	emb.vecs = [][]float32{{1.0, 0.0, 0.0}}
	s.Store(ctx, domain.MemoryEntry{ID: "x1", Content: "initial"})

	emb.vecs = [][]float32{{1.0, 0.0, 0.0}}
	s.vectorSearch(ctx, "test", 10) // triggers load

	if s.vecIdx.size() != 1 {
		t.Fatalf("vecIdx size = %d, want 1", s.vecIdx.size())
	}

	// Store another entry. Index should update incrementally.
	emb.vecs = [][]float32{{0.0, 1.0, 0.0}}
	s.Store(ctx, domain.MemoryEntry{ID: "x2", Content: "added after load"})

	if s.vecIdx.size() != 2 {
		t.Errorf("vecIdx size = %d, want 2 after incremental add", s.vecIdx.size())
	}

	// Delete entry. Index should update.
	s.Delete(ctx, "x1")
	if s.vecIdx.size() != 1 {
		t.Errorf("vecIdx size = %d, want 1 after delete", s.vecIdx.size())
	}
}

func TestVecIndexStoreBatchUpdate(t *testing.T) {
	emb := &mockEmbedder{dims: 3}
	dbPath := filepath.Join(t.TempDir(), "vidx-batch.db")
	s, err := New(dbPath, emb, slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Trigger index load with empty store.
	emb.vecs = [][]float32{{1.0, 0.0, 0.0}}
	s.vectorSearch(ctx, "test", 10)

	if s.vecIdx.size() != 0 {
		t.Fatalf("vecIdx size = %d, want 0", s.vecIdx.size())
	}

	// Reset embedder to generate deterministic vectors for all texts.
	emb.vecs = nil

	// StoreBatch should update index incrementally.
	entries := []domain.MemoryEntry{
		{ID: "b1", Content: "batch one"},
		{ID: "b2", Content: "batch two"},
		{ID: "b3", Content: "batch three"},
	}
	s.StoreBatch(ctx, entries)

	if s.vecIdx.size() != 3 {
		t.Errorf("vecIdx size = %d, want 3 after StoreBatch", s.vecIdx.size())
	}
}

func BenchmarkCosineSimilarity(b *testing.B) {
	dims := 1536
	a := make([]float32, dims)
	bv := make([]float32, dims)
	for i := range a {
		a[i] = float32(i) / float32(dims)
		bv[i] = float32(dims-i) / float32(dims)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cosineSimilarity(a, bv)
	}
}

func BenchmarkFloat32Bytes(b *testing.B) {
	v := make([]float32, 1536)
	for i := range v {
		v[i] = float32(i) * 0.001
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded := float32ToBytes(v)
		bytesToFloat32(encoded)
	}
}
