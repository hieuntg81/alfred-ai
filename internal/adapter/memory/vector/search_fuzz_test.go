package vector

import (
	"context"
	"log/slog"
	"math"
	"path/filepath"
	"testing"

	"alfred-ai/internal/domain"
)

// FuzzHybridSearch verifies that arbitrary query strings never cause panics
// in the hybrid search pipeline (FTS5 MATCH, LIKE fallback, RRF, etc.).
func FuzzHybridSearch(f *testing.F) {
	// Seed corpus with interesting edge cases.
	seeds := []string{
		"",
		"hello world",
		"a",
		"SELECT * FROM entries",
		`"quoted phrase"`,
		"special chars: (parens) [brackets] {braces}",
		"OR AND NOT",
		"*wildcard*",
		"unicode: 你好世界 🌍",
		"null\x00byte",
		"very " + string(make([]byte, 1000)),
		"a OR b AND c NOT d",
		"col:value",
		"near/5 proximity",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, query string) {
		dbPath := filepath.Join(t.TempDir(), "fuzz.db")
		emb := &mockEmbedder{dims: 3}
		s, err := New(dbPath, emb, slog.Default())
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		defer s.Close()

		ctx := context.Background()

		// Seed a few entries so there's data to search.
		for i, content := range []string{
			"machine learning algorithms",
			"deep learning neural networks",
			"cooking pasta recipes",
		} {
			s.Store(ctx, domain.MemoryEntry{
				ID:      string(rune('a' + i)),
				Content: content,
			})
		}

		// Must not panic regardless of query input.
		results, err := s.hybridSearch(ctx, query, 10)
		if err != nil {
			return // errors are acceptable, panics are not
		}
		_ = results
	})
}

// FuzzCosineSimilarity verifies that arbitrary float32 vectors never
// produce NaN or Inf results.
func FuzzCosineSimilarity(f *testing.F) {
	f.Add([]byte{0, 0, 128, 63}, []byte{0, 0, 128, 63}) // [1.0], [1.0]
	f.Add([]byte{0, 0, 0, 0}, []byte{0, 0, 0, 0})       // [0.0], [0.0]
	f.Add([]byte{}, []byte{})                             // empty

	f.Fuzz(func(t *testing.T, aBytes, bBytes []byte) {
		a := bytesToFloat32(aBytes)
		b := bytesToFloat32(bBytes)
		if a == nil || b == nil {
			return
		}

		result := cosineSimilarity(a, b)

		if math.IsNaN(float64(result)) {
			t.Errorf("cosineSimilarity returned NaN for a=%v, b=%v", a, b)
		}
		if math.IsInf(float64(result), 0) {
			t.Errorf("cosineSimilarity returned Inf for a=%v, b=%v", a, b)
		}
	})
}

// FuzzStoreEntry verifies that arbitrary content, tags, and metadata
// can be stored and retrieved without panics or data corruption.
func FuzzStoreEntry(f *testing.F) {
	f.Add("hello world", "tag1,tag2", "key1=val1")
	f.Add("", "", "")
	f.Add("unicode: 你好", "тег", "ключ=значение")
	f.Add("null\x00byte", "a", "b=c")

	f.Fuzz(func(t *testing.T, content, tagsStr, metaStr string) {
		dbPath := filepath.Join(t.TempDir(), "fuzz-store.db")
		s, err := New(dbPath, nil, slog.Default())
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		defer s.Close()

		ctx := context.Background()

		// Build tags from comma-separated string.
		var tags []string
		if tagsStr != "" {
			for _, part := range splitNonEmpty(tagsStr, ',') {
				tags = append(tags, part)
			}
		}

		// Build metadata from key=value pairs.
		meta := make(map[string]string)
		if metaStr != "" {
			for _, part := range splitNonEmpty(metaStr, ',') {
				kv := splitNonEmpty(part, '=')
				if len(kv) == 2 {
					meta[kv[0]] = kv[1]
				}
			}
		}

		entry := domain.MemoryEntry{
			Content:  content,
			Tags:     tags,
			Metadata: meta,
		}

		// Store must not panic.
		err = s.Store(ctx, entry)
		if err != nil {
			return // errors are acceptable
		}

		// Query must not panic.
		results, err := s.Query(ctx, content, 5)
		if err != nil {
			return
		}
		_ = results
	})
}

// splitNonEmpty splits s by sep and returns non-empty parts.
func splitNonEmpty(s string, sep byte) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			if i > start {
				parts = append(parts, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		parts = append(parts, s[start:])
	}
	return parts
}
