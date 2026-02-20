package usecase

import (
	"context"
	"fmt"
	"sync"
)

// SessionLocker provides operation-level mutual exclusion per session.
// It prevents two concurrent HandleMessage calls from operating on
// the same session simultaneously.
type SessionLocker struct {
	mu    sync.Mutex
	locks map[string]*sessionMutex
}

type sessionMutex struct {
	mu       sync.Mutex
	refCount int
}

// NewSessionLocker creates a new session locker.
func NewSessionLocker() *SessionLocker {
	return &SessionLocker{
		locks: make(map[string]*sessionMutex),
	}
}

// Lock acquires the lock for the given session ID. It blocks until the
// lock is acquired or the context is cancelled. Returns an unlock function
// that MUST be called when the operation is complete.
func (sl *SessionLocker) Lock(ctx context.Context, sessionID string) (unlock func(), err error) {
	// Get or create the per-session mutex.
	sl.mu.Lock()
	sm, ok := sl.locks[sessionID]
	if !ok {
		sm = &sessionMutex{}
		sl.locks[sessionID] = sm
	}
	sm.refCount++
	sl.mu.Unlock()

	// Try to acquire the session mutex with context cancellation support.
	acquired := make(chan struct{})
	go func() {
		sm.mu.Lock()
		close(acquired)
	}()

	select {
	case <-acquired:
		// Lock acquired successfully.
		return func() {
			sm.mu.Unlock()
			sl.mu.Lock()
			sm.refCount--
			if sm.refCount == 0 {
				delete(sl.locks, sessionID)
			}
			sl.mu.Unlock()
		}, nil

	case <-ctx.Done():
		// Context cancelled before lock acquired.
		// Must clean up: wait for the goroutine to finish acquiring,
		// then immediately release to prevent a permanent held lock.
		go func() {
			<-acquired
			sm.mu.Unlock()
			sl.mu.Lock()
			sm.refCount--
			if sm.refCount == 0 {
				delete(sl.locks, sessionID)
			}
			sl.mu.Unlock()
		}()
		return nil, fmt.Errorf("session lock: %w", ctx.Err())
	}
}

// ActiveCount returns the number of sessions with active or pending locks.
// Intended for testing.
func (sl *SessionLocker) ActiveCount() int {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	return len(sl.locks)
}
