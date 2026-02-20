package usecase

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

func TestSessionManagerGet(t *testing.T) {
	sm := NewSessionManager(t.TempDir())
	_ = sm.GetOrCreate("s1")

	got, err := sm.Get("s1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ExternalKey != "s1" {
		t.Errorf("ExternalKey = %q, want s1", got.ExternalKey)
	}
	if len(got.ID) != 26 {
		t.Errorf("ID should be a 26-char ULID, got %q (%d chars)", got.ID, len(got.ID))
	}
}

func TestSessionManagerGetNotFound(t *testing.T) {
	sm := NewSessionManager(t.TempDir())

	_, err := sm.Get("nope")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, domain.ErrSessionNotFound) {
		t.Errorf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestSessionManagerDelete(t *testing.T) {
	dir := t.TempDir()
	sm := NewSessionManager(dir)
	_ = sm.GetOrCreate("del1")

	// Save to disk first.
	if err := sm.Save("del1"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file exists.
	path := filepath.Join(dir, "del1.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("session file should exist: %v", err)
	}

	// Delete.
	if err := sm.Delete("del1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Should be gone from memory.
	_, err := sm.Get("del1")
	if !errors.Is(err, domain.ErrSessionNotFound) {
		t.Errorf("Get after delete: %v", err)
	}

	// Should be gone from disk.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("session file should be removed")
	}
}

func TestSessionManagerDeleteNotFound(t *testing.T) {
	sm := NewSessionManager(t.TempDir())

	err := sm.Delete("nope")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, domain.ErrSessionNotFound) {
		t.Errorf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestReapStaleSessions(t *testing.T) {
	dir := t.TempDir()
	sm := NewSessionManager(dir)

	// Create two sessions: one stale, one fresh.
	stale := sm.GetOrCreate("stale")
	stale.mu.Lock()
	stale.UpdatedAt = time.Now().Add(-2 * time.Hour)
	stale.mu.Unlock()

	_ = sm.GetOrCreate("fresh") // UpdatedAt = now

	reaped := sm.ReapStaleSessions(1 * time.Hour)
	if reaped != 1 {
		t.Errorf("reaped = %d, want 1", reaped)
	}

	// fresh should still exist
	if _, err := sm.Get("fresh"); err != nil {
		t.Errorf("fresh session should still exist: %v", err)
	}
	// stale should be gone
	if _, err := sm.Get("stale"); err == nil {
		t.Error("stale session should have been reaped")
	}
}

func TestReapStaleSessionsNone(t *testing.T) {
	sm := NewSessionManager(t.TempDir())
	_ = sm.GetOrCreate("s1")
	_ = sm.GetOrCreate("s2")

	reaped := sm.ReapStaleSessions(1 * time.Hour)
	if reaped != 0 {
		t.Errorf("reaped = %d, want 0 (all fresh)", reaped)
	}
}

func TestReapStaleSessionsAll(t *testing.T) {
	sm := NewSessionManager(t.TempDir())

	for _, id := range []string{"a", "b", "c"} {
		s := sm.GetOrCreate(id)
		s.mu.Lock()
		s.UpdatedAt = time.Now().Add(-48 * time.Hour)
		s.mu.Unlock()
	}

	reaped := sm.ReapStaleSessions(24 * time.Hour)
	if reaped != 3 {
		t.Errorf("reaped = %d, want 3", reaped)
	}

	sessions := sm.ListSessions()
	if len(sessions) != 0 {
		t.Errorf("remaining sessions = %d, want 0", len(sessions))
	}
}

func TestReapStaleSessionsDiskCleanup(t *testing.T) {
	dir := t.TempDir()
	sm := NewSessionManager(dir)

	s := sm.GetOrCreate("disk-stale")
	sm.Save("disk-stale")

	// Make stale.
	s.mu.Lock()
	s.UpdatedAt = time.Now().Add(-2 * time.Hour)
	s.mu.Unlock()

	path := filepath.Join(dir, "disk-stale.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist before reap: %v", err)
	}

	reaped := sm.ReapStaleSessions(1 * time.Hour)
	if reaped != 1 {
		t.Errorf("reaped = %d, want 1", reaped)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("disk file should be removed after reap")
	}
}
