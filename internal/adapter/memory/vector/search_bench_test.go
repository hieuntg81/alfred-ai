package vector

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"

	"alfred-ai/internal/domain"
)

// newBenchStore creates a Store for benchmarks (does not use t.Cleanup).
func newBenchStore(b *testing.B, embedder domain.EmbeddingProvider) *Store {
	b.Helper()
	dbPath := filepath.Join(b.TempDir(), "bench.db")
	s, err := New(dbPath, embedder, slog.Default())
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	b.Cleanup(func() { s.Close() })
	return s
}

// seedStore inserts n entries with varied content and tags.
func seedStore(b *testing.B, s *Store, n int) {
	b.Helper()
	ctx := context.Background()

	contents := []string{
		"User prefers Go for backend development and microservices architecture",
		"Machine learning models with neural networks and deep learning techniques",
		"Favorite recipe is pasta carbonara with fresh parmesan and eggs",
		"Planning a trip to Italy and wants to visit Rome and Florence",
		"Enjoys jazz music especially Miles Davis and John Coltrane albums",
		"Runs 5k every morning and is training for a half marathon",
		"Currently reading science fiction novels by Isaac Asimov and Arthur Clarke",
		"Takes landscape photographs with mirrorless camera on weekends in mountains",
		"Works remotely as a software engineer building distributed systems",
		"Studies quantum computing and its applications in cryptography",
	}

	tagSets := [][]string{
		{"golang", "programming", "backend"},
		{"python", "machine-learning", "ai"},
		{"cooking", "recipes", "italian"},
		{"travel", "europe", "italy"},
		{"music", "jazz", "albums"},
		{"fitness", "running", "marathon"},
		{"books", "fiction", "scifi"},
		{"photography", "nature", "landscape"},
		{"career", "remote", "engineering"},
		{"science", "computing", "quantum"},
	}

	for i := 0; i < n; i++ {
		entry := domain.MemoryEntry{
			ID:      fmt.Sprintf("entry-%06d", i),
			Content: fmt.Sprintf("%s (entry %d with unique content for differentiation)", contents[i%len(contents)], i),
			Tags:    tagSets[i%len(tagSets)],
		}
		if err := s.Store(ctx, entry); err != nil {
			b.Fatalf("Store entry %d: %v", i, err)
		}
	}
}

// --- Keyword (FTS5) Search Benchmarks ---

func BenchmarkVectorStoreKeywordSearch(b *testing.B) {
	sizes := []int{100, 500, 1000, 5000, 10000, 50000}

	for _, n := range sizes {
		b.Run(fmt.Sprintf("corpus_%d", n), func(b *testing.B) {
			s := newBenchStore(b, nil) // keyword-only
			seedStore(b, s, n)

			queries := []string{
				"golang programming",
				"machine learning",
				"pasta recipe",
				"jazz music",
				"nonexistent query terms",
			}

			ctx := context.Background()
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				q := queries[i%len(queries)]
				s.keywordSearch(ctx, q, 10)
			}
		})
	}
}

func BenchmarkVectorStoreKeywordSearch_EmptyQuery(b *testing.B) {
	sizes := []int{100, 1000, 5000, 50000}

	for _, n := range sizes {
		b.Run(fmt.Sprintf("corpus_%d", n), func(b *testing.B) {
			s := newBenchStore(b, nil)
			seedStore(b, s, n)

			ctx := context.Background()
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				s.keywordSearch(ctx, "", 10)
			}
		})
	}
}

// --- Hybrid Search Benchmarks (keyword + vector) ---

func BenchmarkVectorStoreHybridSearch(b *testing.B) {
	sizes := []int{100, 500, 1000, 5000, 10000}

	for _, n := range sizes {
		b.Run(fmt.Sprintf("corpus_%d", n), func(b *testing.B) {
			emb := &mockEmbedder{dims: 64} // small dims for benchmark speed
			s := newBenchStore(b, emb)
			seedStore(b, s, n)

			queries := []string{
				"golang programming",
				"machine learning",
				"pasta recipe",
				"jazz music",
				"nonexistent query terms",
			}

			ctx := context.Background()
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				q := queries[i%len(queries)]
				s.hybridSearch(ctx, q, 10)
			}
		})
	}
}

// --- Vector-only Search Benchmarks ---

func BenchmarkVectorStoreVectorSearch(b *testing.B) {
	sizes := []int{100, 500, 1000, 5000, 10000}

	for _, n := range sizes {
		b.Run(fmt.Sprintf("corpus_%d", n), func(b *testing.B) {
			emb := &mockEmbedder{dims: 64}
			s := newBenchStore(b, emb)
			seedStore(b, s, n)

			ctx := context.Background()
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				s.vectorSearch(ctx, "golang programming", 10)
			}
		})
	}
}

// --- RRF Fusion Benchmarks ---

func BenchmarkRRF(b *testing.B) {
	sizes := []int{10, 50, 100, 500}

	for _, n := range sizes {
		b.Run(fmt.Sprintf("lists_%d", n), func(b *testing.B) {
			list1 := make([]domain.MemoryEntry, n)
			list2 := make([]domain.MemoryEntry, n)
			for i := 0; i < n; i++ {
				list1[i] = domain.MemoryEntry{ID: fmt.Sprintf("a-%d", i)}
				list2[i] = domain.MemoryEntry{ID: fmt.Sprintf("b-%d", i)}
			}
			// 50% overlap.
			for i := 0; i < n/2; i++ {
				list2[i].ID = list1[i].ID
			}

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				reciprocalRankFusion(list1, list2)
			}
		})
	}
}

// --- Scale Benchmarks (10K+) ---
// These benchmarks measure performance at production-relevant corpus sizes.

func BenchmarkVectorStoreScale_KeywordTopK(b *testing.B) {
	topKs := []int{5, 10, 25, 50}
	const corpusSize = 10000

	for _, k := range topKs {
		b.Run(fmt.Sprintf("top_%d", k), func(b *testing.B) {
			s := newBenchStore(b, nil)
			seedStore(b, s, corpusSize)

			ctx := context.Background()
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				s.keywordSearch(ctx, "golang programming distributed", k)
			}
		})
	}
}

func BenchmarkVectorStoreScale_StoreInsert(b *testing.B) {
	sizes := []int{1000, 5000, 10000}

	for _, preload := range sizes {
		b.Run(fmt.Sprintf("preload_%d", preload), func(b *testing.B) {
			s := newBenchStore(b, nil)
			seedStore(b, s, preload)

			ctx := context.Background()
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				entry := domain.MemoryEntry{
					ID:      fmt.Sprintf("bench-insert-%d", i),
					Content: fmt.Sprintf("Benchmark insert entry number %d with unique text for deduplication", i),
					Tags:    []string{"benchmark", "insert"},
				}
				s.Store(ctx, entry)
			}
		})
	}
}

// --- VecIndex vs DB Scan Benchmarks ---

func BenchmarkVectorSearchIndexed(b *testing.B) {
	sizes := []int{100, 500, 1000, 5000, 10000}

	for _, n := range sizes {
		b.Run(fmt.Sprintf("corpus_%d", n), func(b *testing.B) {
			emb := &mockEmbedder{dims: 64}
			s := newBenchStore(b, emb)
			seedStore(b, s, n)

			ctx := context.Background()

			// Force index load before benchmark.
			s.vectorSearch(ctx, "golang programming", 10)
			if !s.vecIdx.isLoaded() {
				b.Fatal("vecIdx should be loaded")
			}

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				s.vectorSearch(ctx, "golang programming", 10)
			}
		})
	}
}

func BenchmarkVectorSearchDBScan(b *testing.B) {
	sizes := []int{100, 500, 1000, 5000, 10000}

	for _, n := range sizes {
		b.Run(fmt.Sprintf("corpus_%d", n), func(b *testing.B) {
			emb := &mockEmbedder{dims: 64}
			s := newBenchStore(b, emb)
			seedStore(b, s, n)

			ctx := context.Background()

			// Get query vector for direct DB scan benchmark.
			vecs, _ := emb.Embed(ctx, []string{"golang programming"})
			queryVec := vecs[0]

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				s.vectorSearchDB(ctx, queryVec, 10)
			}
		})
	}
}

// --- Cosine Similarity at Different Dimensions ---

func BenchmarkCosineSimilarity_Dims(b *testing.B) {
	dims := []int{64, 384, 768, 1536, 3072}

	for _, d := range dims {
		b.Run(fmt.Sprintf("dims_%d", d), func(b *testing.B) {
			a := make([]float32, d)
			bv := make([]float32, d)
			for i := range a {
				a[i] = float32(i) / float32(d)
				bv[i] = float32(d-i) / float32(d)
			}

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				cosineSimilarity(a, bv)
			}
		})
	}
}
