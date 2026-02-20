package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIndexAll(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewMemoryIndex(dir)
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}

	now := time.Now()
	idx.Add(IndexEntry{ID: "a", Filename: "a.md", CreatedAt: now.Add(-2 * time.Hour)})
	idx.Add(IndexEntry{ID: "b", Filename: "b.md", CreatedAt: now.Add(-1 * time.Hour)})
	idx.Add(IndexEntry{ID: "c", Filename: "c.md", CreatedAt: now})

	all := idx.All()
	if len(all) != 3 {
		t.Fatalf("All() returned %d entries, want 3", len(all))
	}
	// Should be sorted newest first
	if all[0].ID != "c" {
		t.Errorf("first entry ID = %q, want %q", all[0].ID, "c")
	}
	if all[2].ID != "a" {
		t.Errorf("last entry ID = %q, want %q", all[2].ID, "a")
	}
}

func TestIndexLen(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewMemoryIndex(dir)
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}

	if idx.Len() != 0 {
		t.Errorf("Len() = %d, want 0", idx.Len())
	}

	idx.Add(IndexEntry{ID: "a", Filename: "a.md"})
	idx.Add(IndexEntry{ID: "b", Filename: "b.md"})

	if idx.Len() != 2 {
		t.Errorf("Len() = %d, want 2", idx.Len())
	}
}

func TestIndexSearchEmptyQuery(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewMemoryIndex(dir)
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}

	now := time.Now()
	idx.Add(IndexEntry{ID: "a", Filename: "a.md", ContentPreview: "hello world", CreatedAt: now})
	idx.Add(IndexEntry{ID: "b", Filename: "b.md", ContentPreview: "testing", CreatedAt: now})

	results := idx.Search("", 10)
	if len(results) != 2 {
		t.Errorf("Search('') returned %d results, want 2", len(results))
	}
}

func TestIndexSearchEmptyQueryWithLimit(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewMemoryIndex(dir)
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}

	now := time.Now()
	for i := 0; i < 5; i++ {
		idx.Add(IndexEntry{ID: string(rune('a' + i)), Filename: "f.md", ContentPreview: "content", CreatedAt: now})
	}

	results := idx.Search("", 2)
	if len(results) != 2 {
		t.Errorf("Search('', limit=2) returned %d results, want 2", len(results))
	}
}

func TestIndexGetFilename(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewMemoryIndex(dir)
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}

	idx.Add(IndexEntry{ID: "test-id", Filename: "test-file.md"})

	if fn := idx.GetFilename("test-id"); fn != "test-file.md" {
		t.Errorf("GetFilename = %q, want %q", fn, "test-file.md")
	}
	if fn := idx.GetFilename("nonexistent"); fn != "" {
		t.Errorf("GetFilename(nonexistent) = %q, want empty", fn)
	}
}

func TestIndexPersistence(t *testing.T) {
	dir := t.TempDir()

	// Create and populate index
	idx1, err := NewMemoryIndex(dir)
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}

	idx1.Add(IndexEntry{ID: "persist", Filename: "persist.md", ContentPreview: "persistent data"})

	// Reload
	idx2, err := NewMemoryIndex(dir)
	if err != nil {
		t.Fatalf("NewMemoryIndex (reload): %v", err)
	}

	if idx2.Len() != 1 {
		t.Errorf("reloaded index Len() = %d, want 1", idx2.Len())
	}
	if fn := idx2.GetFilename("persist"); fn != "persist.md" {
		t.Errorf("reloaded GetFilename = %q", fn)
	}
}

func TestIndexLoadCorrupt(t *testing.T) {
	dir := t.TempDir()

	// Write corrupt index.json
	indexPath := filepath.Join(dir, "index.json")
	if err := os.WriteFile(indexPath, []byte("not valid json"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := NewMemoryIndex(dir)
	if err == nil {
		t.Error("expected error for corrupt index file")
	}
}

func TestIndexRemove(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewMemoryIndex(dir)
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}

	idx.Add(IndexEntry{ID: "to-remove", Filename: "remove.md"})
	if idx.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", idx.Len())
	}

	if err := idx.Remove("to-remove"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if idx.Len() != 0 {
		t.Errorf("Len() after remove = %d, want 0", idx.Len())
	}
}

func TestTokenize(t *testing.T) {
	result := tokenize("Hello, World! Go is great.")
	// "hello", "world", "go", "is", "great" - but "is" is 2 chars so included
	found := make(map[string]bool)
	for _, w := range result {
		found[w] = true
	}
	if !found["hello"] || !found["world"] || !found["great"] {
		t.Errorf("tokenize result = %v", result)
	}
}

func TestTokenizeSingleChar(t *testing.T) {
	// Single character words should be filtered out
	result := tokenize("I a b c")
	// "i", "a", "b", "c" are all < 2 chars, should be empty
	if len(result) != 0 {
		t.Errorf("expected empty result for single-char words, got %v", result)
	}
}

func TestTokenizeDuplicates(t *testing.T) {
	result := tokenize("go go go golang")
	if len(result) != 2 { // "go" and "golang"
		t.Errorf("expected 2 unique tokens, got %d: %v", len(result), result)
	}
}

func TestScoreEntry(t *testing.T) {
	entry := IndexEntry{
		ID:             "test",
		Tags:           []string{"golang", "testing"},
		ContentPreview: "User prefers Go for backend development",
		CreatedAt:      time.Now(),
	}

	// Tag match should score higher
	score := scoreEntry(entry, []string{"golang"}, time.Now())
	if score <= 0 {
		t.Errorf("expected positive score for tag match, got %f", score)
	}

	// Content match
	score2 := scoreEntry(entry, []string{"backend"}, time.Now())
	if score2 <= 0 {
		t.Errorf("expected positive score for content match, got %f", score2)
	}

	// No match
	score3 := scoreEntry(entry, []string{"python"}, time.Now())
	if score3 != 0 {
		t.Errorf("expected 0 score for no match, got %f", score3)
	}
}

func TestIndexSaveErrorReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewMemoryIndex(dir)
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}

	idx.Add(IndexEntry{ID: "test", Filename: "test.md"})

	// Make directory read-only to prevent save
	os.Chmod(dir, 0444)
	defer os.Chmod(dir, 0755)

	// Adding another entry triggers save which should fail
	err = idx.Add(IndexEntry{ID: "test2", Filename: "test2.md"})
	if err == nil {
		t.Error("expected error when saving to read-only directory")
	}
}

func TestSearchWithLimit(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewMemoryIndex(dir)
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}

	// Add 5 entries
	for i := 0; i < 5; i++ {
		idx.Add(IndexEntry{
			ID:             fmt.Sprintf("entry-%d", i),
			Filename:       fmt.Sprintf("entry-%d.md", i),
			Tags:           []string{"common"},
			ContentPreview: "common content",
			CreatedAt:      time.Now().Add(-time.Duration(i) * time.Hour),
		})
	}

	// Search with limit
	results := idx.Search("common", 3)
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}
