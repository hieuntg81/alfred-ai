package tool

import (
	"sync"
	"time"
)

// RateLimiter implements a sliding-window rate limiter.
// It tracks timestamps of allowed calls and rejects new calls
// when the count within the window exceeds the limit.
type RateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	calls  []time.Time
	now    func() time.Time // for testing
}

// NewRateLimiter creates a rate limiter that allows limit calls per window.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		limit:  limit,
		window: window,
		now:    time.Now,
	}
}

// Allow returns true if a call is allowed under the rate limit, and records it.
// Returns false if the limit has been reached within the current window.
func (r *RateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	cutoff := now.Add(-r.window)

	// Trim expired entries.
	n := 0
	for _, t := range r.calls {
		if t.After(cutoff) {
			r.calls[n] = t
			n++
		}
	}
	r.calls = r.calls[:n]

	if len(r.calls) >= r.limit {
		return false
	}

	r.calls = append(r.calls, now)
	return true
}

// Reset clears all recorded calls. Useful for testing.
func (r *RateLimiter) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = r.calls[:0]
}
