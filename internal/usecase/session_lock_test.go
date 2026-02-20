package usecase

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestSessionLockerBasic(t *testing.T) {
	sl := NewSessionLocker()

	unlock, err := sl.Lock(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}

	if sl.ActiveCount() != 1 {
		t.Errorf("ActiveCount = %d, want 1", sl.ActiveCount())
	}

	unlock()

	// After unlock, the session should be cleaned up.
	if sl.ActiveCount() != 0 {
		t.Errorf("ActiveCount after unlock = %d, want 0", sl.ActiveCount())
	}
}

func TestSessionLockerConcurrentSameSession(t *testing.T) {
	sl := NewSessionLocker()

	// Goroutine A holds the lock.
	unlock1, err := sl.Lock(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("Lock1: %v", err)
	}

	order := make(chan int, 2)

	// Goroutine B tries to lock the same session — should block.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		unlock2, err := sl.Lock(context.Background(), "session-1")
		if err != nil {
			t.Errorf("Lock2: %v", err)
			return
		}
		order <- 2
		unlock2()
	}()

	// Give goroutine B time to block.
	time.Sleep(50 * time.Millisecond)

	// A releases — B should now acquire.
	order <- 1
	unlock1()

	wg.Wait()
	close(order)

	// Verify ordering: 1 must come before 2.
	vals := make([]int, 0, 2)
	for v := range order {
		vals = append(vals, v)
	}
	if len(vals) != 2 || vals[0] != 1 || vals[1] != 2 {
		t.Errorf("order = %v, want [1, 2]", vals)
	}
}

func TestSessionLockerDifferentSessions(t *testing.T) {
	sl := NewSessionLocker()

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	for _, id := range []string{"session-a", "session-b"} {
		wg.Add(1)
		go func(sessionID string) {
			defer wg.Done()
			unlock, err := sl.Lock(context.Background(), sessionID)
			if err != nil {
				errCh <- err
				return
			}
			// Hold briefly to simulate work.
			time.Sleep(20 * time.Millisecond)
			unlock()
		}(id)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSessionLockerTimeout(t *testing.T) {
	sl := NewSessionLocker()

	// Goroutine A holds the lock.
	unlock1, err := sl.Lock(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("Lock1: %v", err)
	}
	defer unlock1()

	// Goroutine B tries with a short deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = sl.Lock(ctx, "session-1")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	// Wait a bit for cleanup goroutine to finish.
	time.Sleep(100 * time.Millisecond)
}

func TestSessionLockerCleanup(t *testing.T) {
	sl := NewSessionLocker()

	// Lock and unlock several sessions.
	for _, id := range []string{"s1", "s2", "s3"} {
		unlock, err := sl.Lock(context.Background(), id)
		if err != nil {
			t.Fatalf("Lock(%s): %v", id, err)
		}
		unlock()
	}

	if sl.ActiveCount() != 0 {
		t.Errorf("ActiveCount = %d, want 0 (all cleaned up)", sl.ActiveCount())
	}
}

func TestSessionLockerNilSafe(t *testing.T) {
	// This tests that agent code can safely check for nil SessionLocker.
	var sl *SessionLocker
	if sl != nil {
		t.Error("nil SessionLocker should be nil")
	}
}
