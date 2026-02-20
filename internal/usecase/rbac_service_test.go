package usecase

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"alfred-ai/internal/domain"

	"github.com/stretchr/testify/assert"
)

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

func TestRouterRBAC_ViewerCannotExecuteTools(t *testing.T) {
	// Create a minimal router with RBAC enabled.
	agent := &Agent{}
	sessions := NewSessionManager(t.TempDir())
	r := NewRouter(agent, sessions, nil, noopLogger())
	r.SetAuthorizer(&RBACAuthorizer{})

	// Context with viewer role (no PermToolExecute).
	ctx := domain.ContextWithRoles(context.Background(), []domain.AuthRole{domain.AuthRoleViewer})

	_, err := r.Handle(ctx, domain.InboundMessage{
		SessionID:   "test",
		Content:     "hello",
		ChannelName: "test",
		SenderName:  "user",
	})
	assert.ErrorIs(t, err, domain.ErrForbidden)
}

func TestRouterRBAC_UserCanExecuteTools(t *testing.T) {
	// Context with user role (has PermToolExecute).
	ctx := domain.ContextWithRoles(context.Background(), []domain.AuthRole{domain.AuthRoleUser})

	authorizer := &RBACAuthorizer{}
	err := authorizer.Authorize(ctx, domain.RolesFromContext(ctx), domain.PermToolExecute)
	assert.NoError(t, err)
}

func TestRouterRBAC_NoRolesSkipsCheck(t *testing.T) {
	// Context without roles — backward compat, should skip RBAC.
	ctx := context.Background()

	agent := &Agent{}
	sessions := NewSessionManager(t.TempDir())
	r := NewRouter(agent, sessions, nil, noopLogger())
	r.SetAuthorizer(&RBACAuthorizer{})

	// This will fail for other reasons (nil LLM) but NOT for RBAC.
	_, err := r.Handle(ctx, domain.InboundMessage{
		SessionID:   "test",
		Content:     "hello",
		ChannelName: "test",
		SenderName:  "user",
	})
	// Should not be ErrForbidden.
	assert.NotErrorIs(t, err, domain.ErrForbidden)
}

func TestTenantManager_RBACRejectsViewer(t *testing.T) {
	store := &mockTenantStore{}
	mgr := NewTenantManager(store, t.TempDir(), noopLogger())
	mgr.SetAuthorizer(&RBACAuthorizer{})

	ctx := domain.ContextWithRoles(context.Background(), []domain.AuthRole{domain.AuthRoleViewer})

	err := mgr.Create(ctx, &domain.Tenant{ID: "t1", Name: "Test"})
	assert.ErrorIs(t, err, domain.ErrForbidden)
}

func TestTenantManager_RBACAllowsAdmin(t *testing.T) {
	store := &mockTenantStore{}
	mgr := NewTenantManager(store, t.TempDir(), noopLogger())
	mgr.SetAuthorizer(&RBACAuthorizer{})

	ctx := domain.ContextWithRoles(context.Background(), []domain.AuthRole{domain.AuthRoleAdmin})

	err := mgr.Create(ctx, &domain.Tenant{ID: "t1", Name: "Test"})
	assert.NoError(t, err)
}

// mockTenantStore implements domain.TenantStore for testing.
type mockTenantStore struct {
	tenants []*domain.Tenant
}

func (s *mockTenantStore) Get(_ context.Context, id string) (*domain.Tenant, error) {
	for _, t := range s.tenants {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, domain.ErrTenantNotFound
}

func (s *mockTenantStore) Create(_ context.Context, t *domain.Tenant) error {
	s.tenants = append(s.tenants, t)
	return nil
}

func (s *mockTenantStore) Update(_ context.Context, t *domain.Tenant) error {
	for i, existing := range s.tenants {
		if existing.ID == t.ID {
			s.tenants[i] = t
			return nil
		}
	}
	return domain.ErrTenantNotFound
}

func (s *mockTenantStore) Delete(_ context.Context, id string) error {
	for i, t := range s.tenants {
		if t.ID == id {
			s.tenants = append(s.tenants[:i], s.tenants[i+1:]...)
			return nil
		}
	}
	return domain.ErrTenantNotFound
}

func (s *mockTenantStore) List(_ context.Context) ([]*domain.Tenant, error) {
	return s.tenants, nil
}
