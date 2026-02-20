package tenant

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"alfred-ai/internal/domain"
)

func newTestStore(t *testing.T) *SQLiteTenantStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "tenants.db")
	store, err := NewSQLiteTenantStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteTenantStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSQLiteTenantStore_CRUD(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Create
	tenant := &domain.Tenant{
		ID:   "t1",
		Name: "Acme Corp",
		Plan: domain.PlanPro,
		Config: domain.TenantConfig{
			SystemPrompt: "You are Acme's assistant.",
			Model:        "gpt-4o",
		},
		Limits: domain.TenantLimits{
			MaxSessions: 100,
			MaxAgents:   5,
		},
	}
	if err := store.Create(ctx, tenant); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Get
	got, err := store.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Acme Corp" {
		t.Errorf("Name = %q, want %q", got.Name, "Acme Corp")
	}
	if got.Plan != domain.PlanPro {
		t.Errorf("Plan = %q, want %q", got.Plan, domain.PlanPro)
	}
	if got.Config.SystemPrompt != "You are Acme's assistant." {
		t.Errorf("Config.SystemPrompt = %q", got.Config.SystemPrompt)
	}
	if got.Limits.MaxSessions != 100 {
		t.Errorf("Limits.MaxSessions = %d, want 100", got.Limits.MaxSessions)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}

	// Update
	got.Name = "Acme Inc"
	got.Plan = domain.PlanEnterprise
	if err := store.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	updated, err := store.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if updated.Name != "Acme Inc" {
		t.Errorf("Name after update = %q, want %q", updated.Name, "Acme Inc")
	}
	if updated.Plan != domain.PlanEnterprise {
		t.Errorf("Plan after update = %q, want %q", updated.Plan, domain.PlanEnterprise)
	}

	// List
	tenant2 := &domain.Tenant{ID: "t2", Name: "Beta LLC", Plan: domain.PlanFree}
	if err := store.Create(ctx, tenant2); err != nil {
		t.Fatalf("Create t2: %v", err)
	}
	all, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List count = %d, want 2", len(all))
	}

	// Delete
	if err := store.Delete(ctx, "t1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = store.Get(ctx, "t1")
	if !errors.Is(err, domain.ErrTenantNotFound) {
		t.Errorf("Get after delete: got %v, want ErrTenantNotFound", err)
	}
}

func TestSQLiteTenantStore_NotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent")
	if !errors.Is(err, domain.ErrTenantNotFound) {
		t.Errorf("Get nonexistent: got %v, want ErrTenantNotFound", err)
	}

	err = store.Update(ctx, &domain.Tenant{ID: "nonexistent", Name: "x", Plan: domain.PlanFree})
	if !errors.Is(err, domain.ErrTenantNotFound) {
		t.Errorf("Update nonexistent: got %v, want ErrTenantNotFound", err)
	}

	err = store.Delete(ctx, "nonexistent")
	if !errors.Is(err, domain.ErrTenantNotFound) {
		t.Errorf("Delete nonexistent: got %v, want ErrTenantNotFound", err)
	}
}

func TestSQLiteTenantStore_EmptyList(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	all, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("List count = %d, want 0", len(all))
	}
}

func TestSQLiteTenantStore_ConfigRoundtrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant := &domain.Tenant{
		ID:   "tc",
		Name: "Config Test",
		Plan: domain.PlanPro,
		Config: domain.TenantConfig{
			SystemPrompt: "test prompt",
			Model:        "claude-3",
			Provider:     "anthropic",
			Tools:        []string{"shell", "web"},
			Metadata:     map[string]string{"env": "staging"},
		},
		Limits: domain.TenantLimits{
			MaxSessions:  50,
			MaxAgents:    3,
			MaxToolCalls: 1000,
			MaxStorageMB: 500,
			MaxLLMTokens: 100000,
		},
	}
	if err := store.Create(ctx, tenant); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.Get(ctx, "tc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Config.Provider != "anthropic" {
		t.Errorf("Config.Provider = %q", got.Config.Provider)
	}
	if len(got.Config.Tools) != 2 || got.Config.Tools[0] != "shell" {
		t.Errorf("Config.Tools = %v", got.Config.Tools)
	}
	if got.Config.Metadata["env"] != "staging" {
		t.Errorf("Config.Metadata = %v", got.Config.Metadata)
	}
	if got.Limits.MaxToolCalls != 1000 {
		t.Errorf("Limits.MaxToolCalls = %d", got.Limits.MaxToolCalls)
	}
	if got.Limits.MaxLLMTokens != 100000 {
		t.Errorf("Limits.MaxLLMTokens = %d", got.Limits.MaxLLMTokens)
	}
}
