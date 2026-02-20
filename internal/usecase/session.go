package usecase

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"alfred-ai/internal/domain"
	"github.com/oklog/ulid/v2"
)

// Session represents an active conversation session.
type Session struct {
	mu          sync.RWMutex
	ID          string           `json:"id"`           // ULID (internal, globally unique)
	ExternalKey string           `json:"external_key"` // channel lookup key (e.g. "cli:cli-default")
	TenantID    string           `json:"tenant_id,omitempty"` // empty = default/single-tenant
	Msgs        []domain.Message `json:"messages"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

// NewSession creates a new empty session with a generated ULID.
// The externalKey is the channel-scoped lookup key (e.g. "cli:cli-default").
func NewSession(externalKey string) *Session {
	now := time.Now()
	return &Session{
		ID:          generateULID(now),
		ExternalKey: externalKey,
		Msgs:        make([]domain.Message, 0),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func generateULID(t time.Time) string {
	entropy := ulid.Monotonic(rand.New(rand.NewSource(t.UnixNano())), 0)
	return ulid.MustNew(ulid.Timestamp(t), entropy).String()
}

// AddMessage appends a message and updates the timestamp (thread-safe).
func (s *Session) AddMessage(msg domain.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	s.Msgs = append(s.Msgs, msg)
	s.UpdatedAt = time.Now()
}

// Messages returns a copy of the message history (thread-safe).
func (s *Session) Messages() []domain.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make([]domain.Message, len(s.Msgs))
	copy(cp, s.Msgs)
	return cp
}

// Truncate keeps only the last N messages.
func (s *Session) Truncate(maxMessages int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Msgs) <= maxMessages {
		return
	}
	s.Msgs = s.Msgs[len(s.Msgs)-maxMessages:]
}

// SessionManager manages multiple sessions with persistence.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	dataDir  string
}

// NewSessionManager creates a session manager with a data directory for persistence.
func NewSessionManager(dataDir string) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
		dataDir:  dataDir,
	}
}

// validateSessionID checks if a session ID is safe for filesystem use.
// It rejects path separators, parent directory references, and null bytes.
func (sm *SessionManager) validateSessionID(id string) error {
	if id == "" {
		return fmt.Errorf("session ID cannot be empty")
	}

	// Reject path-unsafe characters
	if strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("session ID contains path separators: %q", id)
	}

	if strings.Contains(id, "..") {
		return fmt.Errorf("session ID contains parent directory reference: %q", id)
	}

	if strings.Contains(id, "\x00") {
		return fmt.Errorf("session ID contains null byte: %q", id)
	}

	// Additional safety: check that filepath.Clean doesn't change it
	// (indicates path manipulation attempts)
	clean := filepath.Clean(id)
	if clean != id {
		return fmt.Errorf("session ID not clean path: %q vs %q", id, clean)
	}

	return nil
}

// GetOrCreate returns an existing session or creates a new one.
// If a tenant ID is present in the context, new sessions are stamped with it,
// and existing sessions are validated for tenant ownership.
func (sm *SessionManager) GetOrCreate(id string) *Session {
	return sm.GetOrCreateWithTenant(id, "")
}

// GetOrCreateWithTenant is the tenant-aware variant of GetOrCreate.
// When tenantID is non-empty, it is set on new sessions and validated on existing ones.
func (sm *SessionManager) GetOrCreateWithTenant(id string, tenantID string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if s, ok := sm.sessions[id]; ok {
		// Validate tenant ownership (empty tenantID = backward compat, skip check).
		if tenantID != "" && s.TenantID != "" && s.TenantID != tenantID {
			// Tenant mismatch — treat as if session doesn't exist, create a new one.
			s = NewSession(id)
			s.TenantID = tenantID
			sm.sessions[id] = s
		}
		return s
	}

	s := NewSession(id)
	s.TenantID = tenantID

	// Try to load from disk
	if loaded, err := sm.loadFromDisk(id); err == nil {
		// Validate loaded session's tenant.
		if tenantID == "" || loaded.TenantID == "" || loaded.TenantID == tenantID {
			s = loaded
			if tenantID != "" && s.TenantID == "" {
				s.TenantID = tenantID
			}
		}
	}

	sm.sessions[id] = s
	return s
}

// Save persists a session to disk as JSON.
func (sm *SessionManager) Save(id string) error {
	if err := sm.validateSessionID(id); err != nil {
		return domain.NewDomainError("SessionManager.Save", err, id)
	}

	sm.mu.RLock()
	s, ok := sm.sessions[id]
	sm.mu.RUnlock()

	if !ok {
		return domain.NewDomainError("SessionManager.Save", domain.ErrSessionNotFound, id)
	}

	if err := os.MkdirAll(sm.dataDir, 0700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	s.mu.RLock()
	data, err := json.MarshalIndent(s, "", "  ")
	s.mu.RUnlock()

	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	path := filepath.Join(sm.dataDir, id+".json")
	return os.WriteFile(path, data, 0600)
}

// Get returns an existing session or ErrSessionNotFound.
func (sm *SessionManager) Get(id string) (*Session, error) {
	return sm.GetWithTenant(id, "")
}

// GetWithTenant returns an existing session, validating tenant ownership.
func (sm *SessionManager) GetWithTenant(id string, tenantID string) (*Session, error) {
	sm.mu.RLock()
	s, ok := sm.sessions[id]
	sm.mu.RUnlock()
	if !ok {
		return nil, domain.NewDomainError("SessionManager.Get", domain.ErrSessionNotFound, id)
	}
	// Validate tenant ownership (empty tenantID = backward compat, skip check).
	if tenantID != "" && s.TenantID != "" && s.TenantID != tenantID {
		return nil, domain.NewDomainError("SessionManager.Get", domain.ErrSessionNotFound, id)
	}
	return s, nil
}

// Delete removes a session from memory and disk.
func (sm *SessionManager) Delete(id string) error {
	return sm.DeleteWithTenant(id, "")
}

// DeleteWithTenant removes a session, validating tenant ownership.
func (sm *SessionManager) DeleteWithTenant(id string, tenantID string) error {
	if err := sm.validateSessionID(id); err != nil {
		return domain.NewDomainError("SessionManager.Delete", err, id)
	}

	sm.mu.Lock()
	s, ok := sm.sessions[id]
	if ok {
		// Validate tenant ownership.
		if tenantID != "" && s.TenantID != "" && s.TenantID != tenantID {
			sm.mu.Unlock()
			return domain.NewDomainError("SessionManager.Delete", domain.ErrSessionNotFound, id)
		}
		delete(sm.sessions, id)
	}
	sm.mu.Unlock()

	if !ok {
		return domain.NewDomainError("SessionManager.Delete", domain.ErrSessionNotFound, id)
	}

	path := filepath.Join(sm.dataDir, id+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove session file: %w", err)
	}
	return nil
}

// ListSessions returns all active session IDs.
func (sm *SessionManager) ListSessions() []string {
	return sm.ListSessionsForTenant("")
}

// ListSessionsForTenant returns session IDs for a given tenant.
// Empty tenantID returns all sessions (backward compat).
func (sm *SessionManager) ListSessionsForTenant(tenantID string) []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	ids := make([]string, 0, len(sm.sessions))
	for id, s := range sm.sessions {
		if tenantID == "" || s.TenantID == "" || s.TenantID == tenantID {
			ids = append(ids, id)
		}
	}
	return ids
}

// ReapStaleSessions deletes sessions not updated within maxAge and returns the
// count of reaped sessions. Both in-memory state and on-disk files are removed.
func (sm *SessionManager) ReapStaleSessions(maxAge time.Duration) int {
	cutoff := time.Now().Add(-maxAge)

	// Phase 1: identify stale sessions under read lock (no nested locks).
	sm.mu.RLock()
	var staleIDs []string
	for id, s := range sm.sessions {
		s.mu.RLock()
		stale := s.UpdatedAt.Before(cutoff)
		s.mu.RUnlock()
		if stale {
			staleIDs = append(staleIDs, id)
		}
	}
	sm.mu.RUnlock()

	if len(staleIDs) == 0 {
		return 0
	}

	// Phase 2: delete under write lock.
	sm.mu.Lock()
	for _, id := range staleIDs {
		delete(sm.sessions, id)
	}
	sm.mu.Unlock()

	// Phase 3: clean up disk files (no lock needed).
	for _, id := range staleIDs {
		// Validate session ID before constructing file path
		if err := sm.validateSessionID(id); err != nil {
			// Skip invalid IDs (shouldn't happen in normal operation)
			continue
		}
		path := filepath.Join(sm.dataDir, id+".json")
		os.Remove(path)
	}
	return len(staleIDs)
}

func (sm *SessionManager) loadFromDisk(id string) (*Session, error) {
	if err := sm.validateSessionID(id); err != nil {
		return nil, domain.NewDomainError("SessionManager.loadFromDisk", err, id)
	}

	path := filepath.Join(sm.dataDir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}

	// Migrate legacy sessions: if ExternalKey is empty, the old ID was the
	// external key and we need to assign a proper ULID.
	if s.ExternalKey == "" {
		s.ExternalKey = s.ID
		s.ID = generateULID(time.Now())
	}

	return &s, nil
}
