package usecase

import (
	"context"

	"alfred-ai/internal/domain"
)

// RBACAuthorizer implements domain.Authorizer using the static role-permission map.
type RBACAuthorizer struct{}

// Authorize checks if any of the given roles grants the specified permission.
// Returns domain.ErrForbidden if none of the roles have the permission.
func (a *RBACAuthorizer) Authorize(_ context.Context, roles []domain.AuthRole, perm domain.Permission) error {
	for _, role := range roles {
		perms, ok := domain.RolePermissions[role]
		if !ok {
			continue
		}
		for _, p := range perms {
			if p == perm {
				return nil
			}
		}
	}
	return domain.ErrForbidden
}
