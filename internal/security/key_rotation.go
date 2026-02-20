package security

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// KeyInfo describes a single encryption key and its metadata.
type KeyInfo struct {
	ID        string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// KeyStore abstracts how encryption keys are stored and rotated.
type KeyStore interface {
	CurrentKey(ctx context.Context) ([]byte, error)
	Rotate(ctx context.Context) (newKey []byte, err error)
	ListExpiring(ctx context.Context, within time.Duration) ([]KeyInfo, error)
}

// EncryptorKeyStore wraps an AESContentEncryptor to implement KeyStore.
type EncryptorKeyStore struct {
	enc       *AESContentEncryptor
	createdAt time.Time
	mu        sync.RWMutex
}

// NewEncryptorKeyStore wraps an existing encryptor as a KeyStore.
func NewEncryptorKeyStore(enc *AESContentEncryptor) *EncryptorKeyStore {
	return &EncryptorKeyStore{
		enc:       enc,
		createdAt: time.Now(),
	}
}

func (s *EncryptorKeyStore) CurrentKey(_ context.Context) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	s.enc.mu.RLock()
	defer s.enc.mu.RUnlock()
	key := make([]byte, len(s.enc.key))
	copy(key, s.enc.key)
	return key, nil
}

func (s *EncryptorKeyStore) Rotate(_ context.Context) ([]byte, error) {
	// Generate a random passphrase for the new key.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("generate passphrase: %w", err)
	}
	passphrase := base64.StdEncoding.EncodeToString(raw)

	if err := s.enc.Rotate(passphrase); err != nil {
		return nil, fmt.Errorf("rotate key: %w", err)
	}

	s.mu.Lock()
	s.createdAt = time.Now()
	s.mu.Unlock()

	return []byte(passphrase), nil
}

func (s *EncryptorKeyStore) ListExpiring(_ context.Context, within time.Duration) ([]KeyInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// We only have one key; report it if it's within the expiry window.
	// Since we don't track expiry explicitly, we report nothing.
	return nil, nil
}

// KeyRotator periodically rotates encryption keys.
type KeyRotator struct {
	store    KeyStore
	interval time.Duration
	onRotate func(newKey []byte) // notification callback, can be nil
	logger   *slog.Logger
	cancel   context.CancelFunc
	done     chan struct{}
	mu       sync.Mutex
	running  bool
}

// NewKeyRotator creates a new key rotator.
func NewKeyRotator(store KeyStore, interval time.Duration, logger *slog.Logger) *KeyRotator {
	return &KeyRotator{
		store:    store,
		interval: interval,
		logger:   logger,
		done:     make(chan struct{}),
	}
}

// SetOnRotate sets a callback that fires after each successful rotation.
func (r *KeyRotator) SetOnRotate(fn func(newKey []byte)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onRotate = fn
}

// Start begins the periodic rotation loop. Blocks until context is cancelled.
func (r *KeyRotator) Start(ctx context.Context) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	ctx, r.cancel = context.WithCancel(ctx)
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
		close(r.done)
	}()

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	r.logger.Info("key rotator started", "interval", r.interval)

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("key rotator stopped")
			return
		case <-ticker.C:
			if err := r.RotateNow(ctx); err != nil {
				r.logger.Error("scheduled key rotation failed", "error", err)
			}
		}
	}
}

// Stop stops the rotation loop.
func (r *KeyRotator) Stop() {
	r.mu.Lock()
	if r.cancel != nil {
		r.cancel()
	}
	r.mu.Unlock()
	<-r.done
}

// RotateNow performs an immediate key rotation.
func (r *KeyRotator) RotateNow(ctx context.Context) error {
	newKey, err := r.store.Rotate(ctx)
	if err != nil {
		return fmt.Errorf("key rotation: %w", err)
	}

	r.logger.Info("key rotated successfully")

	r.mu.Lock()
	fn := r.onRotate
	r.mu.Unlock()

	if fn != nil {
		fn(newKey)
	}
	return nil
}
