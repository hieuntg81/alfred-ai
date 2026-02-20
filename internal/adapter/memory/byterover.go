package memory

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"alfred-ai/internal/domain"
)

// ByteRoverOption configures ByteRoverMemory.
type ByteRoverOption func(*ByteRoverMemory)

// WithByteRoverEncryptor sets a content encryptor for at-rest encryption.
func WithByteRoverEncryptor(enc domain.ContentEncryptor) ByteRoverOption {
	return func(b *ByteRoverMemory) {
		b.encryptor = enc
	}
}

// ByteRoverMemory implements domain.MemoryProvider via ByteRoverClient.
type ByteRoverMemory struct {
	client     ByteRoverClient
	logger     *slog.Logger
	lastSyncAt time.Time
	encryptor  domain.ContentEncryptor
}

// NewByteRoverMemory creates a ByteRover-backed memory provider.
func NewByteRoverMemory(client ByteRoverClient, logger *slog.Logger, opts ...ByteRoverOption) *ByteRoverMemory {
	b := &ByteRoverMemory{
		client: client,
		logger: logger,
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

func (b *ByteRoverMemory) Store(ctx context.Context, entry domain.MemoryEntry) error {
	if entry.ID == "" {
		id, err := generateID()
		if err != nil {
			return domain.NewDomainError("ByteRoverMemory.Store", domain.ErrMemoryStore, err.Error())
		}
		entry.ID = id
	}

	content := entry.Content
	if b.encryptor != nil {
		encrypted, err := b.encryptor.Encrypt(content)
		if err != nil {
			return domain.NewDomainError("ByteRoverMemory.Store", domain.ErrEncryption, err.Error())
		}
		content = encrypted
	}

	if err := b.client.WriteContext(ctx, entry.ID, content, entry.Tags, entry.Metadata); err != nil {
		return domain.NewDomainError("ByteRoverMemory.Store", domain.ErrMemoryStore, err.Error())
	}
	return nil
}

func (b *ByteRoverMemory) Query(ctx context.Context, query string, limit int) ([]domain.MemoryEntry, error) {
	results, err := b.client.Query(ctx, query, limit)
	if err != nil {
		return nil, domain.NewDomainError("ByteRoverMemory.Query", domain.ErrMemoryUnavailable, err.Error())
	}

	entries := make([]domain.MemoryEntry, len(results))
	for i, r := range results {
		content := r.Content
		if b.encryptor != nil {
			decrypted, err := b.encryptor.Decrypt(content)
			if err != nil {
				return nil, domain.NewDomainError("ByteRoverMemory.Query", domain.ErrEncryption, err.Error())
			}
			content = decrypted
		}
		entries[i] = domain.MemoryEntry{
			ID:        r.ID,
			Content:   content,
			Tags:      r.Tags,
			Metadata:  r.Metadata,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
		}
	}
	return entries, nil
}

func (b *ByteRoverMemory) Delete(ctx context.Context, id string) error {
	if err := b.client.DeleteContext(ctx, id); err != nil {
		return domain.NewDomainError("ByteRoverMemory.Delete", domain.ErrMemoryDelete, err.Error())
	}
	return nil
}

func (b *ByteRoverMemory) Curate(_ context.Context, _ []domain.Message) (*domain.CurateResult, error) {
	// Curation is handled externally by the Curator usecase
	return &domain.CurateResult{}, nil
}

func (b *ByteRoverMemory) Sync(ctx context.Context) error {
	// Push local changes
	// For now, sync is a placeholder that checks connectivity
	status, err := b.client.SyncStatus(ctx)
	if err != nil {
		return domain.NewDomainError("ByteRoverMemory.Sync", domain.ErrByteRoverSync, err.Error())
	}

	b.logger.Info("byterover sync status",
		"in_sync", status.InSync,
		"pending_push", status.PendingPush,
		"pending_pull", status.PendingPull,
	)

	if !status.InSync {
		// Pull remote changes
		pulled, err := b.client.Pull(ctx, b.lastSyncAt)
		if err != nil {
			return domain.NewDomainError("ByteRoverMemory.Sync", domain.ErrByteRoverSync,
				fmt.Sprintf("pull: %v", err))
		}
		b.logger.Info("byterover pulled entries", "count", len(pulled))
	}

	b.lastSyncAt = time.Now()
	return nil
}

func (b *ByteRoverMemory) Name() string      { return "byterover" }
func (b *ByteRoverMemory) IsAvailable() bool { return b.client != nil }
