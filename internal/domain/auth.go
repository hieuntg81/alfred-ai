package domain

import "context"

// AuthRole represents a user authorization role.
type AuthRole string

const (
	AuthRoleAdmin    AuthRole = "admin"
	AuthRoleOperator AuthRole = "operator"
	AuthRoleUser     AuthRole = "user"
	AuthRoleViewer   AuthRole = "viewer"
)

// AllAuthRoles lists every valid authorization role for validation purposes.
var AllAuthRoles = []AuthRole{AuthRoleAdmin, AuthRoleOperator, AuthRoleUser, AuthRoleViewer}

// Permission represents a granular action that can be authorized.
type Permission string

const (
	PermAgentCreate   Permission = "agent:create"
	PermAgentDelete   Permission = "agent:delete"
	PermSessionView   Permission = "session:view"
	PermSessionDelete Permission = "session:delete"
	PermToolExecute   Permission = "tool:execute"
	PermMemoryRead    Permission = "memory:read"
	PermMemoryWrite   Permission = "memory:write"
	PermMemoryDelete  Permission = "memory:delete"
	PermConfigEdit    Permission = "config:edit"
	PermDashboard     Permission = "dashboard:view"
	PermCronManage    Permission = "cron:manage"
	PermProcessManage Permission = "process:manage"
	PermNodeManage    Permission = "node:manage"
	PermPluginManage  Permission = "plugin:manage"
	PermTenantManage  Permission = "tenant:manage"
)

// RolePermissions maps each role to its granted permissions.
// Higher roles include all permissions of lower roles plus their own.
var RolePermissions = map[AuthRole][]Permission{
	AuthRoleAdmin: {
		PermAgentCreate, PermAgentDelete,
		PermSessionView, PermSessionDelete,
		PermToolExecute,
		PermMemoryRead, PermMemoryWrite, PermMemoryDelete,
		PermConfigEdit, PermDashboard,
		PermCronManage, PermProcessManage,
		PermNodeManage, PermPluginManage, PermTenantManage,
	},
	AuthRoleOperator: {
		PermSessionView, PermSessionDelete,
		PermToolExecute,
		PermMemoryRead, PermMemoryWrite, PermMemoryDelete,
		PermDashboard,
		PermCronManage, PermProcessManage,
		PermNodeManage, PermPluginManage,
	},
	AuthRoleUser: {
		PermSessionView,
		PermToolExecute,
		PermMemoryRead, PermMemoryWrite,
		PermDashboard,
	},
	AuthRoleViewer: {
		PermSessionView,
		PermMemoryRead,
		PermDashboard,
	},
}

// Authorizer checks whether the caller has a specific permission.
type Authorizer interface {
	Authorize(ctx context.Context, roles []AuthRole, perm Permission) error
}

// Context helpers for RBAC roles (mirrors SessionID pattern in context.go).

const rolesCtxKey ctxKey = "roles"

// ContextWithRoles returns a new context carrying the given roles.
func ContextWithRoles(ctx context.Context, roles []AuthRole) context.Context {
	return context.WithValue(ctx, rolesCtxKey, roles)
}

// RolesFromContext extracts roles from the context.
// Returns nil if not set.
func RolesFromContext(ctx context.Context) []AuthRole {
	if v, ok := ctx.Value(rolesCtxKey).([]AuthRole); ok {
		return v
	}
	return nil
}

// IsValidAuthRole returns true if the given string represents a known role.
func IsValidAuthRole(s string) bool {
	for _, r := range AllAuthRoles {
		if string(r) == s {
			return true
		}
	}
	return false
}

// StringsToAuthRoles converts a string slice to an AuthRole slice,
// skipping any unrecognized values.
func StringsToAuthRoles(ss []string) []AuthRole {
	roles := make([]AuthRole, 0, len(ss))
	for _, s := range ss {
		if IsValidAuthRole(s) {
			roles = append(roles, AuthRole(s))
		}
	}
	return roles
}
