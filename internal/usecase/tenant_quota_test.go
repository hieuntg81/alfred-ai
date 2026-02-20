package usecase

import (
	"testing"

	"alfred-ai/internal/domain"

	"github.com/stretchr/testify/assert"
)

func TestTenantQuotaTracker_SessionLimit(t *testing.T) {
	tracker := NewTenantQuotaTracker()
	limits := domain.TenantLimits{MaxSessions: 2}

	// Under limit.
	assert.NoError(t, tracker.CheckSessionLimit("t1", limits))

	tracker.IncrementSessions("t1")
	assert.NoError(t, tracker.CheckSessionLimit("t1", limits))

	tracker.IncrementSessions("t1")
	// At limit.
	assert.ErrorIs(t, tracker.CheckSessionLimit("t1", limits), domain.ErrTenantLimitHit)

	// Different tenant is independent.
	assert.NoError(t, tracker.CheckSessionLimit("t2", limits))

	// Decrement brings back under limit.
	tracker.DecrementSessions("t1")
	assert.NoError(t, tracker.CheckSessionLimit("t1", limits))
}

func TestTenantQuotaTracker_ToolCallLimit(t *testing.T) {
	tracker := NewTenantQuotaTracker()
	limits := domain.TenantLimits{MaxToolCalls: 3}

	for i := 0; i < 3; i++ {
		assert.NoError(t, tracker.CheckToolCallLimit("t1", limits))
		tracker.IncrementToolCalls("t1")
	}
	assert.ErrorIs(t, tracker.CheckToolCallLimit("t1", limits), domain.ErrTenantLimitHit)
}

func TestTenantQuotaTracker_LLMTokenLimit(t *testing.T) {
	tracker := NewTenantQuotaTracker()
	limits := domain.TenantLimits{MaxLLMTokens: 1000}

	assert.NoError(t, tracker.CheckLLMTokenLimit("t1", limits))

	tracker.AddLLMTokens("t1", 500)
	assert.NoError(t, tracker.CheckLLMTokenLimit("t1", limits))

	tracker.AddLLMTokens("t1", 500)
	assert.ErrorIs(t, tracker.CheckLLMTokenLimit("t1", limits), domain.ErrTenantLimitHit)
}

func TestTenantQuotaTracker_ZeroLimitMeansNoLimit(t *testing.T) {
	tracker := NewTenantQuotaTracker()
	limits := domain.TenantLimits{} // all zeros

	// No limits should always pass.
	for i := 0; i < 100; i++ {
		tracker.IncrementSessions("t1")
		tracker.IncrementToolCalls("t1")
		tracker.AddLLMTokens("t1", 9999)
	}

	assert.NoError(t, tracker.CheckSessionLimit("t1", limits))
	assert.NoError(t, tracker.CheckToolCallLimit("t1", limits))
	assert.NoError(t, tracker.CheckLLMTokenLimit("t1", limits))
}

func TestTenantQuotaTracker_Counters(t *testing.T) {
	tracker := NewTenantQuotaTracker()

	tracker.IncrementSessions("t1")
	tracker.IncrementSessions("t1")
	tracker.IncrementToolCalls("t1")
	tracker.AddLLMTokens("t1", 42)

	assert.Equal(t, int64(2), tracker.SessionCount("t1"))
	assert.Equal(t, int64(1), tracker.ToolCallCount("t1"))
	assert.Equal(t, int64(42), tracker.LLMTokenCount("t1"))
}
