package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"alfred-ai/internal/domain"
)

// TenantManager manages tenant lifecycle and provides isolated services per tenant.
type TenantManager struct {
	store      domain.TenantStore
	dataDir    string // root data directory for tenant-specific data
	authorizer domain.Authorizer // nil = skip RBAC checks
	logger     *slog.Logger
	mu         sync.RWMutex
}

// NewTenantManager creates a new TenantManager.
func NewTenantManager(store domain.TenantStore, dataDir string, logger *slog.Logger) *TenantManager {
	return &TenantManager{
		store:   store,
		dataDir: dataDir,
		logger:  logger,
	}
}

// SetAuthorizer enables service-layer RBAC for tenant operations.
func (m *TenantManager) SetAuthorizer(auth domain.Authorizer) { m.authorizer = auth }

// authorize checks RBAC if an authorizer is configured. Extracts roles from context.
func (m *TenantManager) authorize(ctx context.Context, perm domain.Permission) error {
	if m.authorizer == nil {
		return nil
	}
	roles := domain.RolesFromContext(ctx)
	if len(roles) == 0 {
		return nil // backward compat: no roles = no RBAC enforcement
	}
	return m.authorizer.Authorize(ctx, roles, perm)
}

// Get returns a tenant by ID.
func (m *TenantManager) Get(ctx context.Context, id string) (*domain.Tenant, error) {
	return m.store.Get(ctx, id)
}

// Create creates a new tenant and initializes its data directory.
func (m *TenantManager) Create(ctx context.Context, t *domain.Tenant) error {
	if err := m.authorize(ctx, domain.PermTenantManage); err != nil {
		return err
	}
	if t.ID == "" {
		return fmt.Errorf("tenant ID must not be empty")
	}
	if t.Name == "" {
		return fmt.Errorf("tenant name must not be empty")
	}
	if t.Plan == "" {
		t.Plan = domain.PlanFree
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.store.Create(ctx, t); err != nil {
		return err
	}

	// Create tenant data directories.
	tenantDir := m.TenantDataDir(t.ID)
	for _, sub := range []string{"sessions", "memory"} {
		if err := os.MkdirAll(filepath.Join(tenantDir, sub), 0700); err != nil {
			return fmt.Errorf("create tenant data dir: %w", err)
		}
	}

	m.logger.Info("tenant created", "tenant_id", t.ID, "name", t.Name, "plan", t.Plan)
	return nil
}

// Update updates tenant metadata.
func (m *TenantManager) Update(ctx context.Context, t *domain.Tenant) error {
	if err := m.authorize(ctx, domain.PermTenantManage); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store.Update(ctx, t)
}

// Delete removes a tenant. Does NOT delete the data directory (for safety).
func (m *TenantManager) Delete(ctx context.Context, id string) error {
	if err := m.authorize(ctx, domain.PermTenantManage); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.store.Delete(ctx, id); err != nil {
		return err
	}

	m.logger.Info("tenant deleted", "tenant_id", id)
	return nil
}

// List returns all tenants.
func (m *TenantManager) List(ctx context.Context) ([]*domain.Tenant, error) {
	return m.store.List(ctx)
}

// TenantDataDir returns the base data directory for a given tenant.
func (m *TenantManager) TenantDataDir(tenantID string) string {
	return filepath.Join(m.dataDir, "tenants", tenantID)
}

// TenantSessionsDir returns the sessions directory for a given tenant.
func (m *TenantManager) TenantSessionsDir(tenantID string) string {
	return filepath.Join(m.TenantDataDir(tenantID), "sessions")
}

// TenantMemoryDir returns the memory directory for a given tenant.
func (m *TenantManager) TenantMemoryDir(tenantID string) string {
	return filepath.Join(m.TenantDataDir(tenantID), "memory")
}
