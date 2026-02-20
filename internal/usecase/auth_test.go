package usecase

import (
	"context"
	"testing"

	"alfred-ai/internal/domain"
)

func TestRBACAuthorizer_AdminHasAllPermissions(t *testing.T) {
	a := &RBACAuthorizer{}
	ctx := context.Background()

	allPerms := []domain.Permission{
		domain.PermAgentCreate, domain.PermAgentDelete,
		domain.PermSessionView, domain.PermSessionDelete,
		domain.PermToolExecute,
		domain.PermMemoryRead, domain.PermMemoryWrite, domain.PermMemoryDelete,
		domain.PermConfigEdit, domain.PermDashboard,
		domain.PermCronManage, domain.PermProcessManage,
		domain.PermNodeManage, domain.PermPluginManage, domain.PermTenantManage,
	}

	for _, perm := range allPerms {
		if err := a.Authorize(ctx, []domain.AuthRole{domain.AuthRoleAdmin}, perm); err != nil {
			t.Errorf("admin should have permission %s, got error: %v", perm, err)
		}
	}
}

func TestRBACAuthorizer_RolePermissions(t *testing.T) {
	a := &RBACAuthorizer{}
	ctx := context.Background()

	tests := []struct {
		name    string
		roles   []domain.AuthRole
		perm    domain.Permission
		allowed bool
	}{
		// Operator
		{"operator can view sessions", []domain.AuthRole{domain.AuthRoleOperator}, domain.PermSessionView, true},
		{"operator can execute tools", []domain.AuthRole{domain.AuthRoleOperator}, domain.PermToolExecute, true},
		{"operator can manage cron", []domain.AuthRole{domain.AuthRoleOperator}, domain.PermCronManage, true},
		{"operator cannot manage tenants", []domain.AuthRole{domain.AuthRoleOperator}, domain.PermTenantManage, false},
		{"operator cannot edit config", []domain.AuthRole{domain.AuthRoleOperator}, domain.PermConfigEdit, false},
		{"operator cannot create agents", []domain.AuthRole{domain.AuthRoleOperator}, domain.PermAgentCreate, false},

		// User
		{"user can view sessions", []domain.AuthRole{domain.AuthRoleUser}, domain.PermSessionView, true},
		{"user can execute tools", []domain.AuthRole{domain.AuthRoleUser}, domain.PermToolExecute, true},
		{"user can read memory", []domain.AuthRole{domain.AuthRoleUser}, domain.PermMemoryRead, true},
		{"user can write memory", []domain.AuthRole{domain.AuthRoleUser}, domain.PermMemoryWrite, true},
		{"user cannot delete memory", []domain.AuthRole{domain.AuthRoleUser}, domain.PermMemoryDelete, false},
		{"user cannot delete sessions", []domain.AuthRole{domain.AuthRoleUser}, domain.PermSessionDelete, false},
		{"user cannot manage cron", []domain.AuthRole{domain.AuthRoleUser}, domain.PermCronManage, false},
		{"user cannot manage processes", []domain.AuthRole{domain.AuthRoleUser}, domain.PermProcessManage, false},

		// Viewer
		{"viewer can view sessions", []domain.AuthRole{domain.AuthRoleViewer}, domain.PermSessionView, true},
		{"viewer can read memory", []domain.AuthRole{domain.AuthRoleViewer}, domain.PermMemoryRead, true},
		{"viewer can view dashboard", []domain.AuthRole{domain.AuthRoleViewer}, domain.PermDashboard, true},
		{"viewer cannot execute tools", []domain.AuthRole{domain.AuthRoleViewer}, domain.PermToolExecute, false},
		{"viewer cannot write memory", []domain.AuthRole{domain.AuthRoleViewer}, domain.PermMemoryWrite, false},
		{"viewer cannot delete sessions", []domain.AuthRole{domain.AuthRoleViewer}, domain.PermSessionDelete, false},

		// Multiple roles (union of permissions)
		{"viewer+user gets both permissions", []domain.AuthRole{domain.AuthRoleViewer, domain.AuthRoleUser}, domain.PermToolExecute, true},
		{"user+operator gets operator perms", []domain.AuthRole{domain.AuthRoleUser, domain.AuthRoleOperator}, domain.PermCronManage, true},

		// Edge cases
		{"empty roles denied", []domain.AuthRole{}, domain.PermSessionView, false},
		{"unknown role denied", []domain.AuthRole{domain.AuthRole("unknown")}, domain.PermSessionView, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := a.Authorize(ctx, tt.roles, tt.perm)
			if tt.allowed && err != nil {
				t.Errorf("expected allowed, got error: %v", err)
			}
			if !tt.allowed && err == nil {
				t.Error("expected denied, got nil")
			}
			if !tt.allowed && err != nil && err != domain.ErrForbidden {
				t.Errorf("expected ErrForbidden, got: %v", err)
			}
		})
	}
}

func TestRBACAuthorizer_NilContext(t *testing.T) {
	a := &RBACAuthorizer{}
	err := a.Authorize(context.Background(), []domain.AuthRole{domain.AuthRoleAdmin}, domain.PermToolExecute)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
