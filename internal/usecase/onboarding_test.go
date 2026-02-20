package usecase

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnboardingHelperFirstContact(t *testing.T) {
	oh := NewOnboardingHelper()
	assert.True(t, oh.IsFirstContact("session-1"), "new session should be first contact")
	assert.True(t, oh.IsFirstContact("session-2"), "different session should also be first contact")
}

func TestOnboardingHelperMarkContacted(t *testing.T) {
	oh := NewOnboardingHelper()
	require.True(t, oh.IsFirstContact("session-1"))

	oh.MarkContacted("session-1")
	assert.False(t, oh.IsFirstContact("session-1"), "should not be first contact after marking")
	assert.True(t, oh.IsFirstContact("session-2"), "other sessions should be unaffected")
}

func TestOnboardingHelperReset(t *testing.T) {
	oh := NewOnboardingHelper()
	oh.MarkContacted("session-1")
	require.False(t, oh.IsFirstContact("session-1"))

	oh.Reset("session-1")
	assert.True(t, oh.IsFirstContact("session-1"), "should be first contact again after reset")
}

func TestOnboardingHelperResetUnknownSession(t *testing.T) {
	oh := NewOnboardingHelper()
	// Resetting a session that was never contacted should be a no-op.
	oh.Reset("nonexistent")
	assert.True(t, oh.IsFirstContact("nonexistent"))
}

func TestOnboardingHelperConcurrency(t *testing.T) {
	oh := NewOnboardingHelper()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(3)
		key := "session"
		go func() {
			defer wg.Done()
			oh.IsFirstContact(key)
		}()
		go func() {
			defer wg.Done()
			oh.MarkContacted(key)
		}()
		go func() {
			defer wg.Done()
			oh.Reset(key)
		}()
	}
	wg.Wait()
	// No data race = pass (run with -race).
}

func TestGetWelcomeMessage(t *testing.T) {
	tests := []struct {
		channel  string
		contains string
	}{
		{"cli", "Quick Tips"},
		{"telegram", "Try these to get started"},
		{"discord", "mention @alfred-ai"},
		{"slack", "enterprise-grade privacy"},
		{"http", "API Endpoints"},
		{"unknown", "privacy-first design"},
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			msg := GetWelcomeMessage(tt.channel)
			assert.NotEmpty(t, msg)
			assert.Contains(t, msg, tt.contains)
		})
	}
}

func TestGetHintForMilestone(t *testing.T) {
	tests := []struct {
		count    int
		wantHint bool
		contains string
	}{
		{5, true, "remember"},
		{10, true, "export"},
		{20, true, "/clear"},
		{50, true, "tool execution"},
		{100, true, "Power user"},
		{1, false, ""},
		{7, false, ""},
		{99, false, ""},
	}

	for _, tt := range tests {
		hint := GetHintForMilestone(tt.count)
		if tt.wantHint {
			assert.NotEmpty(t, hint, "milestone %d should produce a hint", tt.count)
			assert.Contains(t, hint, tt.contains, "milestone %d hint", tt.count)
		} else {
			assert.Empty(t, hint, "count %d should not produce a hint", tt.count)
		}
	}
}
