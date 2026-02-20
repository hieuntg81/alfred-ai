package plugin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"alfred-ai/internal/domain"
)

// ---------------------------------------------------------------------------
// Existing tests (migrated to testify)
// ---------------------------------------------------------------------------

func TestValidatePermissionsAllowed(t *testing.T) {
	m := domain.PluginManifest{
		Name:        "test",
		Permissions: []string{"read", "write"},
	}
	assert.NoError(t, ValidatePermissions(m, []string{"read", "write", "exec"}, nil))
}

func TestValidatePermissionsDenied(t *testing.T) {
	m := domain.PluginManifest{
		Name:        "test",
		Permissions: []string{"exec"},
	}
	err := ValidatePermissions(m, nil, []string{"exec"})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrPermissionDenied)
}

func TestValidatePermissionsUnknown(t *testing.T) {
	m := domain.PluginManifest{
		Name:        "test",
		Permissions: []string{"network"},
	}
	// "network" is not in the allow list.
	err := ValidatePermissions(m, []string{"read", "write"}, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrPermissionDenied)
}

func TestValidatePermissionsNoRestrictions(t *testing.T) {
	m := domain.PluginManifest{
		Name:        "test",
		Permissions: []string{"anything"},
	}
	// No allow list and no deny list: everything is permitted.
	assert.NoError(t, ValidatePermissions(m, nil, nil))
}

func TestValidatePermissionsEmpty(t *testing.T) {
	m := domain.PluginManifest{
		Name:        "test",
		Permissions: nil,
	}
	assert.NoError(t, ValidatePermissions(m, []string{"read"}, []string{"exec"}))
}

// ---------------------------------------------------------------------------
// New tests
// ---------------------------------------------------------------------------

func TestValidatePermissions_DenyTakesPrecedence(t *testing.T) {
	// A permission that appears in both allow and deny lists should be denied.
	m := domain.PluginManifest{
		Name:        "conflict",
		Permissions: []string{"exec"},
	}
	err := ValidatePermissions(m, []string{"exec", "read"}, []string{"exec"})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrPermissionDenied)
	assert.Contains(t, err.Error(), "denied")
}

func TestValidatePermissions_MultiplePerms_PartialDeny(t *testing.T) {
	// First permission is fine, second is denied — should fail.
	m := domain.PluginManifest{
		Name:        "partial",
		Permissions: []string{"read", "exec"},
	}
	err := ValidatePermissions(m, []string{"read", "write"}, []string{"exec"})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrPermissionDenied)
}

func TestValidatePermissions_ErrorMessageContent(t *testing.T) {
	m := domain.PluginManifest{
		Name:        "my-plugin",
		Permissions: []string{"dangerous"},
	}
	err := ValidatePermissions(m, nil, []string{"dangerous"})
	require.Error(t, err)
	// Error message should contain the plugin name and the denied permission.
	assert.Contains(t, err.Error(), "my-plugin")
	assert.Contains(t, err.Error(), "dangerous")
}
