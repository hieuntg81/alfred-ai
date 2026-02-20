package domain

import (
	"context"
	"time"
)

// TenantPlan represents the subscription tier for a tenant.
type TenantPlan string

const (
	PlanFree       TenantPlan = "free"
	PlanPro        TenantPlan = "pro"
	PlanEnterprise TenantPlan = "enterprise"
)

// Tenant represents an isolated organizational unit.
type Tenant struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Plan      TenantPlan   `json:"plan"`
	Config    TenantConfig `json:"config"`
	Limits    TenantLimits `json:"limits"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// TenantLimits defines resource quotas for a tenant.
type TenantLimits struct {
	MaxSessions  int `json:"max_sessions"`
	MaxAgents    int `json:"max_agents"`
	MaxToolCalls int `json:"max_tool_calls"` // per day
	MaxStorageMB int `json:"max_storage_mb"`
	MaxLLMTokens int `json:"max_llm_tokens"` // per day
}

// TenantConfig holds tenant-specific overrides for the default config.
type TenantConfig struct {
	SystemPrompt string            `json:"system_prompt,omitempty"`
	Model        string            `json:"model,omitempty"`
	Provider     string            `json:"provider,omitempty"`
	Tools        []string          `json:"tools,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// TenantStore persists tenant metadata.
type TenantStore interface {
	Get(ctx context.Context, id string) (*Tenant, error)
	Create(ctx context.Context, t *Tenant) error
	Update(ctx context.Context, t *Tenant) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]*Tenant, error)
}

// Context helpers for tenant ID (mirrors SessionID pattern in context.go).

const tenantCtxKey ctxKey = "tenant_id"

// ContextWithTenantID returns a new context carrying the tenant ID.
func ContextWithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantCtxKey, tenantID)
}

// TenantIDFromContext extracts the tenant ID from the context.
// Returns empty string if not set.
func TenantIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(tenantCtxKey).(string); ok {
		return v
	}
	return ""
}
