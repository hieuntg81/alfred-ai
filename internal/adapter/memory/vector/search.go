package vector

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"alfred-ai/internal/domain"
)

// scoredEntry pairs a memory entry with its relevance score.
type scoredEntry struct {
	entry domain.MemoryEntry
	score float64
}

// hybridSearch combines keyword (FTS5) and vector (cosine) search using
// Reciprocal Rank Fusion, then optionally applies temporal decay and MMR.
func (s *Store) hybridSearch(ctx context.Context, query string, limit int) ([]domain.MemoryEntry, error) {
	fetchLimit := limit * 2

	kwResults, kwErr := s.keywordSearch(ctx, query, fetchLimit)
	vecResults, vecErr := s.vectorSearch(ctx, query, fetchLimit)

	// If both fail, return the first error.
	if kwErr != nil && vecErr != nil {
		return nil, kwErr
	}

	var scored []scoredEntry

	switch {
	case kwErr != nil:
		scored = entriesToScored(vecResults)
	case vecErr != nil || len(vecResults) == 0:
		scored = entriesToScored(kwResults)
	default:
		scored = reciprocalRankFusion(kwResults, vecResults)
	}

	// Apply temporal decay if configured.
	if s.opts.DecayHalfLife > 0 {
		applyTemporalDecay(scored, s.opts.DecayHalfLife, time.Now())
	}

	// Sort by score descending (decay may have reordered).
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Apply MMR if configured.
	if s.opts.MMRDiversity > 0 && len(scored) > 0 {
		scored = s.applyMMR(ctx, scored, limit, s.opts.MMRDiversity)
	}

	// Truncate to limit.
	if len(scored) > limit {
		scored = scored[:limit]
	}

	result := make([]domain.MemoryEntry, len(scored))
	for i, se := range scored {
		result[i] = se.entry
	}
	return result, nil
}

// keywordSearch performs FTS5 full-text search. If the query contains FTS5
// syntax errors, it falls back to a LIKE-based search.
func (s *Store) keywordSearch(ctx context.Context, query string, limit int) ([]domain.MemoryEntry, error) {
	if query == "" {
		rows, err := s.db.QueryContext(ctx,
			"SELECT id, content, tags, metadata, created_at, updated_at FROM entries ORDER BY updated_at DESC LIMIT ?",
			limit,
		)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanRows(rows)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, e.content, e.tags, e.metadata, e.created_at, e.updated_at
		 FROM entries_fts f
		 JOIN entries e ON e.rowid = f.rowid
		 WHERE entries_fts MATCH ?
		 ORDER BY bm25(entries_fts)
		 LIMIT ?`,
		query, limit,
	)
	if err != nil {
		// FTS5 syntax error — fall back to LIKE search.
		return s.likeSearch(ctx, query, limit)
	}
	defer rows.Close()
	return scanRows(rows)
}

// likeSearch is a fallback when FTS5 MATCH fails due to special characters.
func (s *Store) likeSearch(ctx context.Context, query string, limit int) ([]domain.MemoryEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, content, tags, metadata, created_at, updated_at FROM entries WHERE content LIKE ? ORDER BY updated_at DESC LIMIT ?",
		"%"+query+"%", limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRows(rows)
}

// vectorSearch embeds the query and finds the most similar entries by cosine similarity.
// It uses an in-memory vector index when available (avoiding SQLite I/O), and falls
// back to a database scan on first call to populate the index.
func (s *Store) vectorSearch(ctx context.Context, query string, limit int) ([]domain.MemoryEntry, error) {
	if s.embedder == nil {
		return nil, nil
	}

	vecs, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, nil
	}
	queryVec := vecs[0]

	// Try in-memory index first. If not loaded yet, load it from the DB.
	if !s.vecIdx.isLoaded() {
		if err := s.vecIdx.loadFromDB(ctx, s); err != nil {
			s.logger.Warn("vector store: failed to load vec index, falling back to DB scan", "error", err)
			return s.vectorSearchDB(ctx, queryVec, limit)
		}
	}

	results := s.vecIdx.search(queryVec, limit)
	if results != nil {
		return results, nil
	}

	// Fallback to DB scan (shouldn't happen after successful load, but defensive).
	return s.vectorSearchDB(ctx, queryVec, limit)
}

// vectorSearchDB is the original database-scan based vector search, used as a
// fallback when the in-memory index is unavailable.
func (s *Store) vectorSearchDB(ctx context.Context, queryVec []float32, limit int) ([]domain.MemoryEntry, error) {
	maxCandidates := s.opts.MaxVectorCandidates
	if maxCandidates <= 0 {
		maxCandidates = defaultMaxVectorCandidates
	}

	rows, err := s.db.QueryContext(ctx,
		"SELECT id, content, tags, metadata, embedding, created_at, updated_at FROM entries WHERE embedding IS NOT NULL ORDER BY updated_at DESC LIMIT ?",
		maxCandidates,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type vecCandidate struct {
		entry domain.MemoryEntry
		score float32
	}
	var candidates []vecCandidate

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
		sim := cosineSimilarity(queryVec, emb)
		if sim <= 0 {
			continue
		}

		unmarshalEntryFields(&entry, tagsJSON, metaJSON, createdAtStr, updatedAtStr)
		candidates = append(candidates, vecCandidate{entry: entry, score: sim})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	result := make([]domain.MemoryEntry, 0, min(limit, len(candidates)))
	for i := 0; i < len(candidates) && i < limit; i++ {
		result = append(result, candidates[i].entry)
	}
	return result, nil
}

// cosineSimilarity computes dot(a,b) / (||a|| * ||b||).
// Returns 0 for zero-length vectors, length mismatch, or NaN/Inf results.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	denom := float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB)))
	if denom == 0 {
		return 0
	}
	result := dot / denom
	if math.IsNaN(float64(result)) || math.IsInf(float64(result), 0) {
		return 0
	}
	return result
}

// entriesToScored converts a plain entry slice to scored entries with
// descending rank-based scores, for use when only one search source is available.
func entriesToScored(entries []domain.MemoryEntry) []scoredEntry {
	scored := make([]scoredEntry, len(entries))
	for i, e := range entries {
		scored[i] = scoredEntry{entry: e, score: 1.0 / float64(i+1)}
	}
	return scored
}

// reciprocalRankFusion merges two ranked lists using RRF (k=60).
func reciprocalRankFusion(list1, list2 []domain.MemoryEntry) []scoredEntry {
	const k = 60

	scores := make(map[string]float64)
	entries := make(map[string]domain.MemoryEntry)

	for rank, e := range list1 {
		scores[e.ID] += 1.0 / float64(k+rank+1)
		entries[e.ID] = e
	}
	for rank, e := range list2 {
		scores[e.ID] += 1.0 / float64(k+rank+1)
		entries[e.ID] = e
	}

	result := make([]scoredEntry, 0, len(scores))
	for id, s := range scores {
		result = append(result, scoredEntry{entry: entries[id], score: s})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].score > result[j].score
	})

	return result
}

// applyTemporalDecay multiplies each entry's score by an exponential decay
// factor based on the time elapsed since the entry's UpdatedAt timestamp.
// Formula: score *= exp(-ln(2) / halfLife * hoursSince)
func applyTemporalDecay(entries []scoredEntry, halfLife time.Duration, now time.Time) {
	ln2 := math.Log(2)
	halfLifeHours := halfLife.Hours()
	if halfLifeHours == 0 {
		return
	}

	for i := range entries {
		hours := now.Sub(entries[i].entry.UpdatedAt).Hours()
		if hours < 0 {
			hours = 0
		}
		decay := math.Exp(-ln2 / halfLifeHours * hours)
		entries[i].score *= decay
	}
}

// applyMMR performs Maximal Marginal Relevance re-ranking for result diversity.
// lambda = 1 - diversity (higher diversity = more penalization of similar results).
// Gracefully returns the input truncated to limit if embeddings are unavailable.
func (s *Store) applyMMR(ctx context.Context, candidates []scoredEntry, limit int, diversity float64) []scoredEntry {
	if len(candidates) <= 1 || diversity == 0 {
		return candidates
	}

	lambda := 1.0 - diversity

	// Fetch embeddings for candidate entries.
	embeddings := s.fetchEmbeddings(ctx, candidates)
	if len(embeddings) == 0 {
		// No embeddings available; skip MMR gracefully.
		return candidates
	}

	selected := make([]scoredEntry, 0, min(limit, len(candidates)))
	remaining := make([]int, len(candidates)) // indices into candidates
	for i := range remaining {
		remaining[i] = i
	}

	for len(selected) < limit && len(remaining) > 0 {
		bestIdx := -1
		bestScore := math.Inf(-1)

		for ri, ci := range remaining {
			candEmb := embeddings[candidates[ci].entry.ID]
			if candEmb == nil {
				// No embedding for this candidate; use raw score.
				mmrScore := lambda * candidates[ci].score
				if mmrScore > bestScore {
					bestScore = mmrScore
					bestIdx = ri
				}
				continue
			}

			// maxSim = max cosine similarity to any already-selected entry.
			var maxSim float64
			for _, sel := range selected {
				selEmb := embeddings[sel.entry.ID]
				if selEmb == nil {
					continue
				}
				sim := float64(cosineSimilarity(candEmb, selEmb))
				if sim > maxSim {
					maxSim = sim
				}
			}

			mmrScore := lambda*candidates[ci].score - (1-lambda)*maxSim
			if mmrScore > bestScore {
				bestScore = mmrScore
				bestIdx = ri
			}
		}

		if bestIdx < 0 {
			break
		}

		selected = append(selected, candidates[remaining[bestIdx]])
		remaining = append(remaining[:bestIdx], remaining[bestIdx+1:]...)
	}

	return selected
}

// fetchEmbeddings loads embedding vectors for the given candidates from the DB.
// Returns a map of entry ID → embedding vector. Missing embeddings are omitted.
func (s *Store) fetchEmbeddings(ctx context.Context, candidates []scoredEntry) map[string][]float32 {
	if len(candidates) == 0 {
		return nil
	}

	ids := make([]any, len(candidates))
	placeholders := make([]string, len(candidates))
	for i, c := range candidates {
		ids[i] = c.entry.ID
		placeholders[i] = "?"
	}

	query := "SELECT id, embedding FROM entries WHERE id IN (" +
		strings.Join(placeholders, ",") +
		") AND embedding IS NOT NULL"

	rows, err := s.db.QueryContext(ctx, query, ids...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := make(map[string][]float32, len(candidates))
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			continue
		}
		if vec := bytesToFloat32(blob); vec != nil {
			result[id] = vec
		}
	}
	return result
}

// float32ToBytes converts a float32 slice to little-endian bytes.
func float32ToBytes(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// bytesToFloat32 converts little-endian bytes back to a float32 slice.
func bytesToFloat32(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// --- helpers ---

func scanRows(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]domain.MemoryEntry, error) {
	var entries []domain.MemoryEntry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func unmarshalEntryFields(entry *domain.MemoryEntry, tagsJSON, metaJSON, createdAt, updatedAt string) {
	if err := json.Unmarshal([]byte(tagsJSON), &entry.Tags); err != nil {
		slog.Warn("vector store: corrupt tags JSON", "id", entry.ID, "error", err)
	}
	if err := json.Unmarshal([]byte(metaJSON), &entry.Metadata); err != nil {
		slog.Warn("vector store: corrupt metadata JSON", "id", entry.ID, "error", err)
	}
	var parseErr error
	if entry.CreatedAt, parseErr = time.Parse(time.RFC3339, createdAt); parseErr != nil {
		slog.Warn("vector store: corrupt created_at", "id", entry.ID, "error", parseErr)
	}
	if entry.UpdatedAt, parseErr = time.Parse(time.RFC3339, updatedAt); parseErr != nil {
		slog.Warn("vector store: corrupt updated_at", "id", entry.ID, "error", parseErr)
	}
}

func truncate(entries []domain.MemoryEntry, limit int) []domain.MemoryEntry {
	if len(entries) <= limit {
		return entries
	}
	return entries[:limit]
}
