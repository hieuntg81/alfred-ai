package memory

import (
	"fmt"
	"testing"
	"time"
)

// populateIndex inserts n entries with varied tags and content previews.
func populateIndex(b *testing.B, idx *MemoryIndex, n int) {
	b.Helper()

	tags := [][]string{
		{"golang", "programming"},
		{"python", "machine-learning"},
		{"cooking", "recipes"},
		{"travel", "europe"},
		{"music", "jazz"},
		{"fitness", "running"},
		{"books", "fiction"},
		{"photography", "nature"},
	}
	previews := []string{
		"User prefers Go for backend development and microservices",
		"Interested in machine learning algorithms and neural networks",
		"Favorite recipe is pasta carbonara with fresh ingredients",
		"Planning a trip to Italy and wants to visit Rome and Florence",
		"Enjoys jazz music especially Miles Davis and John Coltrane",
		"Runs 5k every morning and is training for a marathon",
		"Currently reading science fiction novels by Isaac Asimov",
		"Takes landscape photos with a mirrorless camera on weekends",
	}

	now := time.Now()
	for i := 0; i < n; i++ {
		idx.entries[fmt.Sprintf("entry-%06d", i)] = IndexEntry{
			ID:             fmt.Sprintf("entry-%06d", i),
			Filename:       fmt.Sprintf("2025-01-01-entry-%06d.md", i),
			Tags:           tags[i%len(tags)],
			ContentPreview: previews[i%len(previews)],
			CreatedAt:      now.Add(-time.Duration(i) * time.Hour),
		}
	}
}

func BenchmarkMarkdownIndexSearch(b *testing.B) {
	sizes := []int{100, 500, 1000, 5000, 10000}

	for _, n := range sizes {
		b.Run(fmt.Sprintf("corpus_%d", n), func(b *testing.B) {
			dir := b.TempDir()
			idx, err := NewMemoryIndex(dir)
			if err != nil {
				b.Fatalf("NewMemoryIndex: %v", err)
			}
			populateIndex(b, idx, n)

			queries := []string{
				"golang programming",
				"machine learning",
				"pasta recipe",
				"jazz music",
				"nonexistent query terms",
			}

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				q := queries[i%len(queries)]
				idx.Search(q, 10)
			}
		})
	}
}

func BenchmarkMarkdownIndexSearch_EmptyQuery(b *testing.B) {
	sizes := []int{100, 1000, 10000}

	for _, n := range sizes {
		b.Run(fmt.Sprintf("corpus_%d", n), func(b *testing.B) {
			dir := b.TempDir()
			idx, err := NewMemoryIndex(dir)
			if err != nil {
				b.Fatalf("NewMemoryIndex: %v", err)
			}
			populateIndex(b, idx, n)

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				idx.Search("", 10)
			}
		})
	}
}

func BenchmarkMarkdownIndexSearch_TagHeavy(b *testing.B) {
	dir := b.TempDir()
	idx, err := NewMemoryIndex(dir)
	if err != nil {
		b.Fatalf("NewMemoryIndex: %v", err)
	}

	// 1000 entries, each with 10 tags.
	now := time.Now()
	for i := 0; i < 1000; i++ {
		tags := make([]string, 10)
		for j := 0; j < 10; j++ {
			tags[j] = fmt.Sprintf("tag-%d-%d", i%50, j)
		}
		idx.entries[fmt.Sprintf("e-%d", i)] = IndexEntry{
			ID:             fmt.Sprintf("e-%d", i),
			Filename:       fmt.Sprintf("e-%d.md", i),
			Tags:           tags,
			ContentPreview: "generic content that does not match search terms",
			CreatedAt:      now.Add(-time.Duration(i) * time.Hour),
		}
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		idx.Search("tag-25-5", 10)
	}
}

func BenchmarkTokenize(b *testing.B) {
	inputs := []struct {
		name string
		text string
	}{
		{"short", "golang programming"},
		{"medium", "User prefers Go for backend development and microservices architecture"},
		{"long", "The quick brown fox jumps over the lazy dog, and the fox was very quick indeed because it had been training for months to improve its agility and speed across various terrains"},
	}
	for _, tt := range inputs {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				tokenize(tt.text)
			}
		})
	}
}

func BenchmarkScoreEntry(b *testing.B) {
	entry := IndexEntry{
		ID:             "bench",
		Tags:           []string{"golang", "programming", "backend", "microservices", "api"},
		ContentPreview: "User prefers Go for backend development and microservices architecture with REST APIs",
		CreatedAt:      time.Now().Add(-24 * time.Hour),
	}
	keywords := []string{"golang", "backend", "api"}
	now := time.Now()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scoreEntry(entry, keywords, now)
	}
}
