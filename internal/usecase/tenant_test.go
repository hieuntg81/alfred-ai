package usecase

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"alfred-ai/internal/domain"
)

// memTenantStore is an in-memory implementation of domain.TenantStore for testing.
type memTenantStore struct {
	mu      sync.RWMutex
	tenants map[string]*domain.Tenant
}

func newMemTenantStore() *memTenantStore {
	return &memTenantStore{tenants: make(map[string]*domain.Tenant)}
}

func (s *memTenantStore) Get(_ context.Context, id string) (*domain.Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tenants[id]
	if !ok {
		return nil, domain.ErrTenantNotFound
	}
	cp := *t
	return &cp, nil
}

func (s *memTenantStore) Create(_ context.Context, t *domain.Tenant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tenants[t.ID]; ok {
		return domain.ErrTenantDuplicate
	}
	cp := *t
	s.tenants[t.ID] = &cp
	return nil
}

func (s *memTenantStore) Update(_ context.Context, t *domain.Tenant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tenants[t.ID]; !ok {
		return domain.ErrTenantNotFound
	}
	cp := *t
	s.tenants[t.ID] = &cp
	return nil
}

func (s *memTenantStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tenants[id]; !ok {
		return domain.ErrTenantNotFound
	}
	delete(s.tenants, id)
	return nil
}

func (s *memTenantStore) List(_ context.Context) ([]*domain.Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*domain.Tenant
	for _, t := range s.tenants {
		cp := *t
		result = append(result, &cp)
	}
	return result, nil
}

func newTestTenantManager(t *testing.T) *TenantManager {
	t.Helper()
	store := newMemTenantStore()
	dataDir := t.TempDir()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewTenantManager(store, dataDir, log)
}

func TestTenantManager_CreateAndGet(t *testing.T) {
	m := newTestTenantManager(t)
	ctx := context.Background()

	tenant := &domain.Tenant{
		ID:   "t1",
		Name: "Acme",
		Plan: domain.PlanPro,
	}
	if err := m.Create(ctx, tenant); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := m.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Acme" {
		t.Errorf("Name = %q, want %q", got.Name, "Acme")
	}

	// Verify data directories were created.
	sessDir := m.TenantSessionsDir("t1")
	if _, err := os.Stat(sessDir); os.IsNotExist(err) {
		t.Errorf("sessions dir not created: %s", sessDir)
	}
	memDir := m.TenantMemoryDir("t1")
	if _, err := os.Stat(memDir); os.IsNotExist(err) {
		t.Errorf("memory dir not created: %s", memDir)
	}
}

func TestTenantManager_CreateDefaultPlan(t *testing.T) {
	m := newTestTenantManager(t)
	ctx := context.Background()

	tenant := &domain.Tenant{ID: "t2", Name: "Beta"}
	if err := m.Create(ctx, tenant); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tenant.Plan != domain.PlanFree {
		t.Errorf("Plan = %q, want %q", tenant.Plan, domain.PlanFree)
	}
}

func TestTenantManager_CreateValidation(t *testing.T) {
	m := newTestTenantManager(t)
	ctx := context.Background()

	if err := m.Create(ctx, &domain.Tenant{Name: "No ID"}); err == nil {
		t.Error("expected error for empty ID")
	}
	if err := m.Create(ctx, &domain.Tenant{ID: "x"}); err == nil {
		t.Error("expected error for empty Name")
	}
}

func TestTenantManager_Update(t *testing.T) {
	m := newTestTenantManager(t)
	ctx := context.Background()

	m.Create(ctx, &domain.Tenant{ID: "t3", Name: "Gamma", Plan: domain.PlanFree})

	got, _ := m.Get(ctx, "t3")
	got.Name = "Gamma Inc"
	got.Plan = domain.PlanEnterprise
	if err := m.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}

	updated, _ := m.Get(ctx, "t3")
	if updated.Name != "Gamma Inc" {
		t.Errorf("Name = %q, want %q", updated.Name, "Gamma Inc")
	}
}

func TestTenantManager_Delete(t *testing.T) {
	m := newTestTenantManager(t)
	ctx := context.Background()

	m.Create(ctx, &domain.Tenant{ID: "t4", Name: "Delta", Plan: domain.PlanFree})
	if err := m.Delete(ctx, "t4"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := m.Get(ctx, "t4")
	if !errors.Is(err, domain.ErrTenantNotFound) {
		t.Errorf("Get after delete: got %v, want ErrTenantNotFound", err)
	}
}

func TestTenantManager_List(t *testing.T) {
	m := newTestTenantManager(t)
	ctx := context.Background()

	m.Create(ctx, &domain.Tenant{ID: "t5", Name: "E", Plan: domain.PlanFree})
	m.Create(ctx, &domain.Tenant{ID: "t6", Name: "F", Plan: domain.PlanPro})

	all, err := m.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("List count = %d, want 2", len(all))
	}
}

func TestTenantManager_DataDirPaths(t *testing.T) {
	m := NewTenantManager(newMemTenantStore(), "/data", slog.Default())

	if got := m.TenantDataDir("abc"); got != filepath.Join("/data", "tenants", "abc") {
		t.Errorf("TenantDataDir = %q", got)
	}
	if got := m.TenantSessionsDir("abc"); got != filepath.Join("/data", "tenants", "abc", "sessions") {
		t.Errorf("TenantSessionsDir = %q", got)
	}
	if got := m.TenantMemoryDir("abc"); got != filepath.Join("/data", "tenants", "abc", "memory") {
		t.Errorf("TenantMemoryDir = %q", got)
	}
}
