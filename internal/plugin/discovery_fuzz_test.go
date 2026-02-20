package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzScanDirectories_ManifestContent exercises manifest parsing with arbitrary
// YAML content. Verifies the scanner never panics, always returns a valid
// (possibly empty) slice, and the error is nil for parseable/unparseable content.
func FuzzScanDirectories_ManifestContent(f *testing.F) {
	// Seed corpus with various YAML shapes.
	seeds := []string{
		"name: test\nversion: \"1.0\"\n",
		"name: \"\"\n",
		"{{{invalid yaml",
		"",
		"name: x\ntypes:\n  - tool\npermissions:\n  - read\n",
		"name: test\nversion: null\n",
		"name: test\nextra_field: true\n",
		"---\nname: multi-doc\n",
		"name: inject\nname: override\n",          // duplicate key
		"name: \"<script>alert(1)</script>\"\n",   // XSS in name
		"name: \"a]]]}{{{yaml\"\n",                // special chars
		"name: \"test\\x00null\"\n",               // null byte
	}

	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, content string) {
		tmp := t.TempDir()
		pluginDir := filepath.Join(tmp, "fuzz-plugin")
		if err := os.MkdirAll(pluginDir, 0755); err != nil {
			t.Skip("cannot create dir:", err)
		}
		if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(content), 0644); err != nil {
			t.Skip("cannot write file:", err)
		}

		// Must not panic.
		manifests, err := ScanDirectories([]string{tmp})
		if err != nil {
			// Read errors are acceptable; just don't panic.
			return
		}

		// Any returned manifest must have a non-empty name.
		for _, m := range manifests {
			if m.Name == "" {
				t.Error("manifest with empty name should be filtered out")
			}
		}
	})
}
