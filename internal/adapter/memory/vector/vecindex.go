package vector

import (
	"context"
	"sort"
	"sync"

	"alfred-ai/internal/domain"
)

// vecIndex is an in-memory index of embedding vectors that avoids SQLite I/O
// on every vector search. Entries are loaded lazily on the first search and
// updated incrementally on Store/Delete operations.
type vecIndex struct {
	mu      sync.RWMutex
	entries map[string]vecEntry // id → entry with embedding
	loaded  bool
}

type vecEntry struct {
	entry     domain.MemoryEntry
	embedding []float32
}

func newVecIndex() *vecIndex {
	return &vecIndex{
		entries: make(map[string]vecEntry),
	}
}

// search performs in-memory cosine similarity search against all cached embeddings.
// Returns nil if the index has not been loaded yet.
func (idx *vecIndex) search(queryVec []float32, limit int) []domain.MemoryEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if !idx.loaded || len(idx.entries) == 0 {
		return nil
	}

	type scored struct {
		entry domain.MemoryEntry
		score float32
	}

	candidates := make([]scored, 0, len(idx.entries))
	for _, ve := range idx.entries {
		sim := cosineSimilarity(queryVec, ve.embedding)
		if sim <= 0 {
			continue
		}
		candidates = append(candidates, scored{entry: ve.entry, score: sim})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	n := min(limit, len(candidates))
	result := make([]domain.MemoryEntry, n)
	for i := 0; i < n; i++ {
		result[i] = candidates[i].entry
	}
	return result
}

// put adds or updates an entry in the index.
func (idx *vecIndex) put(entry domain.MemoryEntry, embedding []float32) {
	if embedding == nil {
		return
	}
	idx.mu.Lock()
	idx.entries[entry.ID] = vecEntry{entry: entry, embedding: embedding}
	idx.mu.Unlock()
}

// remove deletes an entry from the index.
func (idx *vecIndex) remove(id string) {
	idx.mu.Lock()
	delete(idx.entries, id)
	idx.mu.Unlock()
}

// isLoaded returns whether the index has been populated from the database.
func (idx *vecIndex) isLoaded() bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.loaded
}

// size returns the number of entries in the index.
func (idx *vecIndex) size() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.entries)
}

// loadFromDB populates the index from the database. This is called once
// on the first vector search. Subsequent calls are no-ops.
func (idx *vecIndex) loadFromDB(ctx context.Context, s *Store) error {
	idx.mu.Lock()
	if idx.loaded {
		idx.mu.Unlock()
		return nil
	}
	idx.mu.Unlock()

	rows, err := s.db.QueryContext(ctx,
		"SELECT id, content, tags, metadata, embedding, created_at, updated_at FROM entries WHERE embedding IS NOT NULL",
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	entries := make(map[string]vecEntry)
	for rows.Next() {
		var (
			entry        domain.MemoryEntry
			tagsJSON     string
			metaJSON     string
			embBlob      []byte
			createdAtStr string
			updatedAtStr string
		)
		if err := rows.Scan(&entry.ID, &entry.Content, &tagsJSON, &metaJSON, &embBlob, &createdAtStr, &updatedAtStr); err != nil {
			continue
		}

		emb := bytesToFloat32(embBlob)
		if emb == nil {
			continue
		}

		unmarshalEntryFields(&entry, tagsJSON, metaJSON, createdAtStr, updatedAtStr)
		entries[entry.ID] = vecEntry{entry: entry, embedding: emb}
	}

	idx.mu.Lock()
	idx.entries = entries
	idx.loaded = true
	idx.mu.Unlock()

	return rows.Err()
}
