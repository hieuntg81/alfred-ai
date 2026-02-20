package memory

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// IndexEntry represents a single entry in the memory index.
type IndexEntry struct {
	ID             string    `json:"id"`
	Filename       string    `json:"filename"`
	Tags           []string  `json:"tags"`
	ContentPreview string    `json:"content_preview"`
	CreatedAt      time.Time `json:"created_at"`
}

// MemoryIndex is an in-memory index backed by index.json.
type MemoryIndex struct {
	mu      sync.RWMutex
	entries map[string]IndexEntry
	path    string // path to index.json
}

// NewMemoryIndex creates or loads an index from the given directory.
func NewMemoryIndex(dir string) (*MemoryIndex, error) {
	idx := &MemoryIndex{
		entries: make(map[string]IndexEntry),
		path:    filepath.Join(dir, "index.json"),
	}
	if err := idx.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load index: %w", err)
	}
	return idx, nil
}

// Add inserts or updates an entry and persists the index.
func (idx *MemoryIndex) Add(entry IndexEntry) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.entries[entry.ID] = entry
	return idx.save()
}

// Remove deletes an entry by ID and persists.
func (idx *MemoryIndex) Remove(id string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	delete(idx.entries, id)
	return idx.save()
}

// searchResult holds a scored index match.
type searchResult struct {
	Entry IndexEntry
	Score float64
}

// GetFilename returns the filename for a given entry ID, or empty string if not found.
func (idx *MemoryIndex) GetFilename(id string) string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if e, ok := idx.entries[id]; ok {
		return e.Filename
	}
	return ""
}

// All returns all index entries sorted by recency (newest first).
func (idx *MemoryIndex) All() []IndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	entries := make([]IndexEntry, 0, len(idx.entries))
	for _, e := range idx.entries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CreatedAt.After(entries[j].CreatedAt)
	})
	return entries
}

// Search finds entries matching the query keywords, scored by relevance + recency.
// An empty query returns all entries sorted by recency.
func (idx *MemoryIndex) Search(query string, limit int) []IndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	keywords := tokenize(query)

	// Empty query: return all entries sorted by recency
	if len(keywords) == 0 {
		entries := make([]IndexEntry, 0, len(idx.entries))
		for _, e := range idx.entries {
			entries = append(entries, e)
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].CreatedAt.After(entries[j].CreatedAt)
		})
		if limit > 0 && len(entries) > limit {
			entries = entries[:limit]
		}
		return entries
	}

	var results []searchResult
	now := time.Now()

	for _, entry := range idx.entries {
		score := scoreEntry(entry, keywords, now)
		if score > 0 {
			results = append(results, searchResult{Entry: entry, Score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	entries := make([]IndexEntry, len(results))
	for i, r := range results {
		entries[i] = r.Entry
	}
	return entries
}

// Len returns the number of entries.
func (idx *MemoryIndex) Len() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.entries)
}

// load reads the index from disk.
func (idx *MemoryIndex) load() error {
	data, err := os.ReadFile(idx.path)
	if err != nil {
		return err
	}
	var entries []IndexEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("unmarshal index: %w", err)
	}
	for _, e := range entries {
		idx.entries[e.ID] = e
	}
	return nil
}

// save writes the index atomically (temp file + rename).
func (idx *MemoryIndex) save() error {
	entries := make([]IndexEntry, 0, len(idx.entries))
	for _, e := range idx.entries {
		entries = append(entries, e)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}

	dir := filepath.Dir(idx.path)
	tmp, err := os.CreateTemp(dir, "index-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, idx.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename index: %w", err)
	}
	return nil
}

// tokenize splits a string into lowercase keywords.
func tokenize(s string) []string {
	words := strings.Fields(strings.ToLower(s))
	unique := make(map[string]struct{}, len(words))
	var result []string
	for _, w := range words {
		w = strings.Trim(w, ".,!?;:'\"()[]{}") // strip punctuation
		if len(w) < 2 {
			continue
		}
		if _, ok := unique[w]; !ok {
			unique[w] = struct{}{}
			result = append(result, w)
		}
	}
	return result
}

// scoreEntry scores an index entry against query keywords with a recency bonus.
func scoreEntry(entry IndexEntry, keywords []string, now time.Time) float64 {
	var score float64

	preview := strings.ToLower(entry.ContentPreview)
	tagsLower := make([]string, len(entry.Tags))
	for i, t := range entry.Tags {
		tagsLower[i] = strings.ToLower(t)
	}

	for _, kw := range keywords {
		// Tag match (higher weight)
		for _, tag := range tagsLower {
			if strings.Contains(tag, kw) {
				score += 3.0
			}
		}
		// Content preview match
		if strings.Contains(preview, kw) {
			score += 1.0
		}
	}

	if score == 0 {
		return 0
	}

	// Recency bonus: entries from the last 7 days get up to +2.0 bonus
	daysSince := now.Sub(entry.CreatedAt).Hours() / 24
	recencyBonus := math.Max(0, 2.0*(1.0-daysSince/7.0))
	score += recencyBonus

	return score
}
