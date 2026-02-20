package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

const encPrefix = "enc:"

// AESContentEncryptor implements domain.ContentEncryptor using AES-256-GCM.
// Key is derived from a passphrase via Argon2id and held only in memory.
type AESContentEncryptor struct {
	mu   sync.RWMutex
	key  []byte // 32 bytes
	salt []byte // 16 bytes
}

// NewAESContentEncryptor creates an encryptor from a passphrase.
// Returns error if passphrase is empty.
func NewAESContentEncryptor(passphrase string) (*AESContentEncryptor, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("passphrase must not be empty")
	}

	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	key := deriveContentKey(passphrase, salt)
	return &AESContentEncryptor{key: key, salt: salt}, nil
}

// Encrypt encrypts plaintext and returns "enc:" + base64(nonce + ciphertext).
func (e *AESContentEncryptor) Encrypt(plaintext string) (string, error) {
	e.mu.RLock()
	key := make([]byte, len(e.key))
	copy(key, e.key)
	e.mu.RUnlock()

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts ciphertext. If it doesn't have the "enc:" prefix,
// the input is returned as-is (backward compat with plaintext content).
func (e *AESContentEncryptor) Decrypt(ciphertext string) (string, error) {
	if !strings.HasPrefix(ciphertext, encPrefix) {
		return ciphertext, nil // plaintext passthrough
	}

	e.mu.RLock()
	key := make([]byte, len(e.key))
	copy(key, e.key)
	e.mu.RUnlock()

	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, encPrefix))
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, sealed := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}

// IsEncrypted checks if a string has the "enc:" prefix.
func (e *AESContentEncryptor) IsEncrypted(s string) bool {
	return strings.HasPrefix(s, encPrefix)
}

// Rotate re-derives the key from a new passphrase with a fresh salt.
func (e *AESContentEncryptor) Rotate(newPassphrase string) error {
	if newPassphrase == "" {
		return fmt.Errorf("passphrase must not be empty")
	}

	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return fmt.Errorf("generate salt: %w", err)
	}

	newKey := deriveContentKey(newPassphrase, salt)

	e.mu.Lock()
	defer e.mu.Unlock()

	// Zero old key
	for i := range e.key {
		e.key[i] = 0
	}
	e.key = newKey
	e.salt = salt
	return nil
}

// Zeroize clears the key bytes from memory. Call on shutdown.
func (e *AESContentEncryptor) Zeroize() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.key {
		e.key[i] = 0
	}
}

// deriveContentKey uses Argon2id to derive a 32-byte key.
func deriveContentKey(passphrase string, salt []byte) []byte {
	return argon2.IDKey([]byte(passphrase), salt, 1, 64*1024, 4, 32)
}
