package node

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"sync"

	"alfred-ai/internal/domain"
)

// Auth manages device token authentication for nodes.
type Auth struct {
	mu     sync.RWMutex
	tokens map[string]string // nodeID -> hex(sha256(token))
}

// NewAuth creates a new Auth instance.
func NewAuth() *Auth {
	return &Auth{
		tokens: make(map[string]string),
	}
}

// GenerateToken creates a new random token for a node, replacing any existing one.
// Returns the raw token (hex-encoded 32 bytes). Only the hash is stored.
func (a *Auth) GenerateToken(nodeID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(raw)

	a.mu.Lock()
	a.tokens[nodeID] = hashToken(token)
	a.mu.Unlock()

	return token, nil
}

// ValidateToken checks if the provided token matches the stored hash for the node.
// Always computes the hash to prevent timing-based nodeID enumeration.
func (a *Auth) ValidateToken(nodeID, token string) error {
	a.mu.RLock()
	stored, ok := a.tokens[nodeID]
	a.mu.RUnlock()

	// Always hash the candidate token to prevent timing side-channel that
	// would allow an attacker to enumerate valid nodeIDs.
	candidate := hashToken(token)

	if !ok {
		// Use a dummy comparison so the total time is indistinguishable
		// from a valid-nodeID-wrong-token path.
		subtle.ConstantTimeCompare([]byte(candidate), []byte(candidate))
		return domain.NewDomainError("Auth.ValidateToken", domain.ErrNodeAuth, "invalid credentials")
	}

	if subtle.ConstantTimeCompare([]byte(stored), []byte(candidate)) != 1 {
		return domain.NewDomainError("Auth.ValidateToken", domain.ErrNodeAuth, "invalid credentials")
	}
	return nil
}

// RevokeToken removes the stored token for a node.
func (a *Auth) RevokeToken(nodeID string) {
	a.mu.Lock()
	delete(a.tokens, nodeID)
	a.mu.Unlock()
}

// HasToken returns true if a token exists for the given node.
func (a *Auth) HasToken(nodeID string) bool {
	a.mu.RLock()
	_, ok := a.tokens[nodeID]
	a.mu.RUnlock()
	return ok
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
