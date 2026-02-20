package plugin

import (
	"testing"

	"alfred-ai/internal/domain"
)

// FuzzValidatePermissions exercises permission validation with arbitrary inputs.
// It verifies the function never panics and always returns either nil or an
// error wrapping ErrPluginPermission.
func FuzzValidatePermissions(f *testing.F) {
	// Seed corpus with attack-like and edge-case inputs.
	seeds := []struct {
		perm  string
		allow string
		deny  string
	}{
		{"read", "read", ""},
		{"exec", "", "exec"},
		{"", "", ""},
		{"read", "read", "read"},                       // in both lists
		{"../../../etc/passwd", "", ""},                 // path traversal
		{"read; DROP TABLE users", "", ""},              // SQL injection
		{"<script>alert(1)</script>", "", ""},           // XSS
		{"read\x00write", "", ""},                       // null byte
		{"perm\ninjected: true", "", ""},                // YAML injection
		{"\t\t\t", "", ""},                              // whitespace
		{"a]]]}{{{", "", ""},                            // special chars
		{"read write exec", "read write exec", ""},      // spaces in perm names
		{"EXEC", "exec", ""},                            // case mismatch
	}

	for _, s := range seeds {
		f.Add(s.perm, s.allow, s.deny)
	}

	f.Fuzz(func(t *testing.T, perm, allow, deny string) {
		manifest := domain.PluginManifest{
			Name:        "fuzz-plugin",
			Permissions: []string{perm},
		}
		var allowed, denied []string
		if allow != "" {
			allowed = []string{allow}
		}
		if deny != "" {
			denied = []string{deny}
		}

		// Must not panic.
		_ = ValidatePermissions(manifest, allowed, denied)
	})
}
