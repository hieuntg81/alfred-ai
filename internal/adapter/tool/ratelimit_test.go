package tool

import (
	"sync"
	"testing"
	"time"
)

func TestRateLimiterAllowUnderLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !rl.Allow() {
			t.Fatalf("call %d should be allowed", i+1)
		}
	}
}

func TestRateLimiterBlocksOverLimit(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)
	rl.Allow()
	rl.Allow()
	if rl.Allow() {
		t.Fatal("third call should be blocked")
	}
}

func TestRateLimiterSlidingWindow(t *testing.T) {
	now := time.Now()
	rl := NewRateLimiter(2, time.Minute)
	rl.now = func() time.Time { return now }

	rl.Allow()
	rl.Allow()

	// Advance time past the window.
	now = now.Add(61 * time.Second)
	if !rl.Allow() {
		t.Fatal("call should be allowed after window expires")
	}
}

func TestRateLimiterReset(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	rl.Allow()
	if rl.Allow() {
		t.Fatal("should be blocked before reset")
	}
	rl.Reset()
	if !rl.Allow() {
		t.Fatal("should be allowed after reset")
	}
}

func TestRateLimiterZeroLimit(t *testing.T) {
	rl := NewRateLimiter(0, time.Minute)
	if rl.Allow() {
		t.Fatal("zero limit should block all calls")
	}
}

func TestRateLimiterPartialWindowExpiry(t *testing.T) {
	now := time.Now()
	rl := NewRateLimiter(2, time.Minute)
	rl.now = func() time.Time { return now }

	rl.Allow() // t=0

	now = now.Add(30 * time.Second)
	rl.Allow() // t=30s

	// Advance so first call expires but second doesn't.
	now = now.Add(31 * time.Second) // t=61s
	if !rl.Allow() {
		t.Fatal("should allow after first call expires")
	}
	if rl.Allow() {
		t.Fatal("should block — two calls in window (t=30s and t=61s)")
	}
}

func TestRateLimiterConcurrentAccess(t *testing.T) {
	rl := NewRateLimiter(100, time.Minute)
	var wg sync.WaitGroup
	allowed := make(chan bool, 200)

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed <- rl.Allow()
		}()
	}
	wg.Wait()
	close(allowed)

	count := 0
	for a := range allowed {
		if a {
			count++
		}
	}
	if count != 100 {
		t.Errorf("expected exactly 100 allowed calls, got %d", count)
	}
}

func TestRateLimiterNewNotNil(t *testing.T) {
	rl := NewRateLimiter(5, time.Second)
	if rl == nil {
		t.Fatal("NewRateLimiter returned nil")
	}
	if rl.limit != 5 {
		t.Errorf("expected limit 5, got %d", rl.limit)
	}
	if rl.window != time.Second {
		t.Errorf("expected window 1s, got %v", rl.window)
	}
}
