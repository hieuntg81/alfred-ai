package vector

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	_ "modernc.org/sqlite"

	"alfred-ai/internal/domain"
)

// SearchOpts holds optional search tuning parameters.
// Zero values disable the corresponding feature.
type SearchOpts struct {
	DecayHalfLife      time.Duration // exponential recency decay; 0 = disabled
	MMRDiversity       float64       // 0-1; 0 = disabled
	MaxVectorCandidates int          // max entries to load for vector search; 0 = default (10000)
}

const defaultMaxVectorCandidates = 10000

// Store implements domain.MemoryProvider backed by SQLite + FTS5 with optional
// vector embeddings. When an EmbeddingProvider is supplied, Store generates
// embeddings on write and supports hybrid (BM25 + cosine) search.
//
// An in-memory vecIndex caches embeddings to avoid SQLite I/O on every vector
// search. The index is lazily loaded on the first search and incrementally
// updated on Store/Delete operations.
type Store struct {
	db       *sql.DB
	embedder domain.EmbeddingProvider
	logger   *slog.Logger
	dbPath   string
	opts     SearchOpts
	vecIdx   *vecIndex
}

// New opens (or creates) a SQLite database at dbPath, runs migrations, and
// returns a ready Store. Pass nil for embedder to use keyword-only search.
// Optional SearchOpts configure temporal decay and MMR diversity.
func New(dbPath string, embedder domain.EmbeddingProvider, logger *slog.Logger, opts ...SearchOpts) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("%w: open db: %v", domain.ErrVectorStore, err)
	}

	// SQLite write safety: single writer.
	db.SetMaxOpenConns(1)

	// Pragmas for performance.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("%w: pragma: %v", domain.ErrVectorStore, err)
		}
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("%w: migrate: %v", domain.ErrVectorStore, err)
	}

	var so SearchOpts
	if len(opts) > 0 {
		so = opts[0]
	}

	return &Store{
		db:       db,
		embedder: embedder,
		logger:   logger,
		dbPath:   dbPath,
		opts:     so,
		vecIdx:   newVecIndex(),
	}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Store implements domain.MemoryProvider.
func (s *Store) Store(ctx context.Context, entry domain.MemoryEntry) error {
	if entry.ID == "" {
		id, err := generateID()
		if err != nil {
			return fmt.Errorf("%w: generate id: %v", domain.ErrVectorStore, err)
		}
		entry.ID = id
	}

	now := time.Now().UTC()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now

	tags, err := json.Marshal(entry.Tags)
	if err != nil {
		return fmt.Errorf("%w: marshal tags: %v", domain.ErrVectorStore, err)
	}
	meta, err := json.Marshal(entry.Metadata)
	if err != nil {
		return fmt.Errorf("%w: marshal metadata: %v", domain.ErrVectorStore, err)
	}

	var embeddingBlob []byte
	if s.embedder != nil && entry.Content != "" {
		vecs, err := s.embedder.Embed(ctx, []string{entry.Content})
		if err != nil {
			s.logger.Warn("vector store: embedding failed, storing without vector",
				"id", entry.ID, "error", err)
		} else if len(vecs) > 0 {
			embeddingBlob = float32ToBytes(vecs[0])
		}
	}

	const upsert = `
		INSERT INTO entries (id, content, tags, metadata, embedding, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			content    = excluded.content,
			tags       = excluded.tags,
			metadata   = excluded.metadata,
			embedding  = excluded.embedding,
			updated_at = excluded.updated_at
	`

	_, err = s.db.ExecContext(ctx, upsert,
		entry.ID,
		entry.Content,
		string(tags),
		string(meta),
		embeddingBlob,
		entry.CreatedAt.Format(time.RFC3339),
		entry.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("%w: upsert: %v", domain.ErrVectorStore, err)
	}

	// Update in-memory vector index if loaded and embedding was generated.
	if embeddingBlob != nil && s.vecIdx.isLoaded() {
		emb := bytesToFloat32(embeddingBlob)
		s.vecIdx.put(entry, emb)
	}

	return nil
}

// StoreBatch stores multiple entries in a single transaction with a single
// batched embedding call. This is significantly more efficient than calling
// Store() in a loop when inserting many entries at once.
func (s *Store) StoreBatch(ctx context.Context, entries []domain.MemoryEntry) error {
	if len(entries) == 0 {
		return nil
	}

	now := time.Now().UTC()

	// Assign IDs and timestamps.
	for i := range entries {
		if entries[i].ID == "" {
			id, err := generateID()
			if err != nil {
				return fmt.Errorf("%w: generate id: %v", domain.ErrVectorStore, err)
			}
			entries[i].ID = id
		}
		if entries[i].CreatedAt.IsZero() {
			entries[i].CreatedAt = now
		}
		entries[i].UpdatedAt = now
	}

	// Batch embed: collect all non-empty content strings, generate embeddings
	// in a single API call.
	var embeddings [][]byte
	if s.embedder != nil {
		texts := make([]string, 0, len(entries))
		textIndices := make([]int, 0, len(entries))
		for i, e := range entries {
			if e.Content != "" {
				texts = append(texts, e.Content)
				textIndices = append(textIndices, i)
			}
		}

		embeddings = make([][]byte, len(entries))
		if len(texts) > 0 {
			vecs, err := s.embedder.Embed(ctx, texts)
			if err != nil {
				s.logger.Warn("vector store: batch embedding failed, storing without vectors", "error", err)
			} else {
				for j, idx := range textIndices {
					if j < len(vecs) {
						embeddings[idx] = float32ToBytes(vecs[j])
					}
				}
			}
		}
	}

	// Execute all inserts in a single transaction.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin tx: %v", domain.ErrVectorStore, err)
	}
	defer tx.Rollback() //nolint:errcheck

	const upsert = `
		INSERT INTO entries (id, content, tags, metadata, embedding, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			content    = excluded.content,
			tags       = excluded.tags,
			metadata   = excluded.metadata,
			embedding  = excluded.embedding,
			updated_at = excluded.updated_at
	`

	stmt, err := tx.PrepareContext(ctx, upsert)
	if err != nil {
		return fmt.Errorf("%w: prepare: %v", domain.ErrVectorStore, err)
	}
	defer stmt.Close()

	for i, entry := range entries {
		tags, err := json.Marshal(entry.Tags)
		if err != nil {
			return fmt.Errorf("%w: marshal tags: %v", domain.ErrVectorStore, err)
		}
		meta, err := json.Marshal(entry.Metadata)
		if err != nil {
			return fmt.Errorf("%w: marshal metadata: %v", domain.ErrVectorStore, err)
		}

		var emb []byte
		if embeddings != nil {
			emb = embeddings[i]
		}

		_, err = stmt.ExecContext(ctx,
			entry.ID,
			entry.Content,
			string(tags),
			string(meta),
			emb,
			entry.CreatedAt.Format(time.RFC3339),
			entry.UpdatedAt.Format(time.RFC3339),
		)
		if err != nil {
			return fmt.Errorf("%w: upsert entry %q: %v", domain.ErrVectorStore, entry.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: commit: %v", domain.ErrVectorStore, err)
	}

	// Update in-memory vector index for entries that got embeddings.
	if s.vecIdx.isLoaded() && embeddings != nil {
		for i, entry := range entries {
			if embeddings[i] != nil {
				emb := bytesToFloat32(embeddings[i])
				s.vecIdx.put(entry, emb)
			}
		}
	}

	return nil
}

// Query implements domain.MemoryProvider.
func (s *Store) Query(ctx context.Context, query string, limit int) ([]domain.MemoryEntry, error) {
	if limit <= 0 {
		limit = 10
	}
	return s.hybridSearch(ctx, query, limit)
}

// Delete implements domain.MemoryProvider.
func (s *Store) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM entries WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("%w: delete: %v", domain.ErrVectorStore, err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return domain.ErrMemoryDelete
	}
	s.vecIdx.remove(id)
	return nil
}

// Curate implements domain.MemoryProvider. The vector store delegates curation
// to the curator layer — this is a no-op that returns an empty result.
func (s *Store) Curate(_ context.Context, _ []domain.Message) (*domain.CurateResult, error) {
	return &domain.CurateResult{}, nil
}

// Sync implements domain.MemoryProvider. No-op for local SQLite.
func (s *Store) Sync(_ context.Context) error { return nil }

// Name implements domain.MemoryProvider.
func (s *Store) Name() string { return "vector" }

// IsAvailable implements domain.MemoryProvider.
func (s *Store) IsAvailable() bool { return s.db != nil }

// generateID returns a short random hex ID (8 bytes = 16 hex chars).
func generateID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// scanEntry reads a single entry row. JSON/time parse errors are logged
// but not returned since they indicate data corruption, not a retrieval failure.
func scanEntry(row interface{ Scan(dest ...any) error }) (domain.MemoryEntry, error) {
	var (
		entry     domain.MemoryEntry
		tagsJSON  string
		metaJSON  string
		createdAt string
		updatedAt string
	)
	if err := row.Scan(&entry.ID, &entry.Content, &tagsJSON, &metaJSON, &createdAt, &updatedAt); err != nil {
		return entry, err
	}
	unmarshalEntryFields(&entry, tagsJSON, metaJSON, createdAt, updatedAt)
	return entry, nil
}
