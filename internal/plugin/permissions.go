package plugin

import (
	"fmt"

	"alfred-ai/internal/domain"
)

// ValidatePermissions checks that every permission declared by the manifest
// is allowed and none are denied.
func ValidatePermissions(manifest domain.PluginManifest, allowed, denied []string) error {
	denySet := make(map[string]bool, len(denied))
	for _, d := range denied {
		denySet[d] = true
	}
	allowSet := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		allowSet[a] = true
	}

	for _, perm := range manifest.Permissions {
		if denySet[perm] {
			return fmt.Errorf("%w: plugin %q requests denied permission %q",
				domain.ErrPermissionDenied, manifest.Name, perm)
		}
		// If an allow list is provided, only allow listed permissions.
		if len(allowSet) > 0 && !allowSet[perm] {
			return fmt.Errorf("%w: plugin %q requests unlisted permission %q",
				domain.ErrPermissionDenied, manifest.Name, perm)
		}
	}
	return nil
}
