package usecase

import (
	"sync"
	"sync/atomic"
	"time"

	"alfred-ai/internal/domain"
)

// TenantQuotaTracker tracks per-tenant resource usage with daily resets.
// All counters are safe for concurrent access.
type TenantQuotaTracker struct {
	mu        sync.RWMutex
	counters  map[string]*tenantCounters
	resetDate string // YYYY-MM-DD of last reset
}

type tenantCounters struct {
	sessions  atomic.Int64
	toolCalls atomic.Int64
	llmTokens atomic.Int64
}

// NewTenantQuotaTracker creates a new quota tracker.
func NewTenantQuotaTracker() *TenantQuotaTracker {
	return &TenantQuotaTracker{
		counters:  make(map[string]*tenantCounters),
		resetDate: today(),
	}
}

func today() string {
	return time.Now().UTC().Format("2006-01-02")
}

// resetIfNewDay checks if the day has changed and resets all daily counters.
func (t *TenantQuotaTracker) resetIfNewDay() {
	d := today()
	if d == t.resetDate {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	// Double-check after acquiring write lock.
	if d == t.resetDate {
		return
	}
	t.counters = make(map[string]*tenantCounters)
	t.resetDate = d
}

func (t *TenantQuotaTracker) getCounters(tenantID string) *tenantCounters {
	t.resetIfNewDay()

	t.mu.RLock()
	c, ok := t.counters[tenantID]
	t.mu.RUnlock()
	if ok {
		return c
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	// Double-check.
	if c, ok := t.counters[tenantID]; ok {
		return c
	}
	c = &tenantCounters{}
	t.counters[tenantID] = c
	return c
}

// CheckSessionLimit verifies the tenant hasn't exceeded their session limit.
// Returns nil if OK, ErrTenantLimitHit if exceeded. Does NOT increment.
func (t *TenantQuotaTracker) CheckSessionLimit(tenantID string, limits domain.TenantLimits) error {
	if limits.MaxSessions <= 0 {
		return nil // no limit
	}
	c := t.getCounters(tenantID)
	if c.sessions.Load() >= int64(limits.MaxSessions) {
		return domain.ErrTenantLimitHit
	}
	return nil
}

// IncrementSessions increments the session counter for a tenant.
func (t *TenantQuotaTracker) IncrementSessions(tenantID string) {
	t.getCounters(tenantID).sessions.Add(1)
}

// DecrementSessions decrements the session counter for a tenant.
func (t *TenantQuotaTracker) DecrementSessions(tenantID string) {
	c := t.getCounters(tenantID)
	if c.sessions.Load() > 0 {
		c.sessions.Add(-1)
	}
}

// CheckToolCallLimit verifies the tenant hasn't exceeded their daily tool call limit.
func (t *TenantQuotaTracker) CheckToolCallLimit(tenantID string, limits domain.TenantLimits) error {
	if limits.MaxToolCalls <= 0 {
		return nil // no limit
	}
	c := t.getCounters(tenantID)
	if c.toolCalls.Load() >= int64(limits.MaxToolCalls) {
		return domain.ErrTenantLimitHit
	}
	return nil
}

// IncrementToolCalls increments the daily tool call counter for a tenant.
func (t *TenantQuotaTracker) IncrementToolCalls(tenantID string) {
	t.getCounters(tenantID).toolCalls.Add(1)
}

// CheckLLMTokenLimit verifies the tenant hasn't exceeded their daily LLM token limit.
func (t *TenantQuotaTracker) CheckLLMTokenLimit(tenantID string, limits domain.TenantLimits) error {
	if limits.MaxLLMTokens <= 0 {
		return nil // no limit
	}
	c := t.getCounters(tenantID)
	if c.llmTokens.Load() >= int64(limits.MaxLLMTokens) {
		return domain.ErrTenantLimitHit
	}
	return nil
}

// AddLLMTokens adds token usage for a tenant.
func (t *TenantQuotaTracker) AddLLMTokens(tenantID string, count int64) {
	t.getCounters(tenantID).llmTokens.Add(count)
}

// SessionCount returns the current session count for a tenant.
func (t *TenantQuotaTracker) SessionCount(tenantID string) int64 {
	return t.getCounters(tenantID).sessions.Load()
}

// ToolCallCount returns the current daily tool call count for a tenant.
func (t *TenantQuotaTracker) ToolCallCount(tenantID string) int64 {
	return t.getCounters(tenantID).toolCalls.Load()
}

// LLMTokenCount returns the current daily LLM token count for a tenant.
func (t *TenantQuotaTracker) LLMTokenCount(tenantID string) int64 {
	return t.getCounters(tenantID).llmTokens.Load()
}
