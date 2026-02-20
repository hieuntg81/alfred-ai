package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Existing tests (migrated to testify)
// ---------------------------------------------------------------------------

func TestScanDirectories(t *testing.T) {
	tmp := t.TempDir()

	// Create a valid plugin.
	pluginDir := filepath.Join(tmp, "myplugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(`
name: myplugin
version: "1.0.0"
description: "A test plugin"
author: "test"
types:
  - tool
permissions:
  - read
`), 0644))

	// Create another valid plugin.
	pluginDir2 := filepath.Join(tmp, "another")
	require.NoError(t, os.MkdirAll(pluginDir2, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir2, "plugin.yaml"), []byte(`
name: another
version: "0.1.0"
`), 0644))

	manifests, err := ScanDirectories([]string{tmp})
	require.NoError(t, err)
	require.Len(t, manifests, 2)

	found := map[string]bool{}
	for _, m := range manifests {
		found[m.Name] = true
	}
	assert.True(t, found["myplugin"], "missing myplugin")
	assert.True(t, found["another"], "missing another")
}

func TestScanInvalidManifest(t *testing.T) {
	tmp := t.TempDir()

	// Create a plugin with broken YAML.
	pluginDir := filepath.Join(tmp, "broken")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(`{{{not yaml`), 0644))

	// Create a valid one.
	pluginDir2 := filepath.Join(tmp, "valid")
	require.NoError(t, os.MkdirAll(pluginDir2, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir2, "plugin.yaml"), []byte(`name: valid`), 0644))

	manifests, err := ScanDirectories([]string{tmp})
	require.NoError(t, err)
	require.Len(t, manifests, 1, "broken manifest should be skipped")
	assert.Equal(t, "valid", manifests[0].Name)
}

func TestScanDirectoriesNonexistent(t *testing.T) {
	manifests, err := ScanDirectories([]string{"/nonexistent/path"})
	require.NoError(t, err)
	assert.Empty(t, manifests)
}

// ---------------------------------------------------------------------------
// New tests
// ---------------------------------------------------------------------------

func TestScanDirectories_EmptyNameSkipped(t *testing.T) {
	tmp := t.TempDir()
	pluginDir := filepath.Join(tmp, "empty-name")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))
	// Valid YAML but name is empty string.
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(`
name: ""
version: "1.0"
`), 0644))

	manifests, err := ScanDirectories([]string{tmp})
	require.NoError(t, err)
	assert.Empty(t, manifests, "plugin with empty name should be skipped")
}

func TestScanDirectories_DirWithoutManifest(t *testing.T) {
	tmp := t.TempDir()
	// Directory exists but has no plugin.yaml.
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "no-manifest"), 0755))

	manifests, err := ScanDirectories([]string{tmp})
	require.NoError(t, err)
	assert.Empty(t, manifests)
}

func TestScanDirectories_MultipleDirectories(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// Plugin in dir1.
	p1 := filepath.Join(dir1, "alpha")
	require.NoError(t, os.MkdirAll(p1, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(p1, "plugin.yaml"), []byte("name: alpha\nversion: \"1.0\"\n"), 0644))

	// Plugin in dir2.
	p2 := filepath.Join(dir2, "beta")
	require.NoError(t, os.MkdirAll(p2, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(p2, "plugin.yaml"), []byte("name: beta\nversion: \"2.0\"\n"), 0644))

	manifests, err := ScanDirectories([]string{dir1, dir2})
	require.NoError(t, err)
	require.Len(t, manifests, 2)

	names := map[string]bool{}
	for _, m := range manifests {
		names[m.Name] = true
	}
	assert.True(t, names["alpha"])
	assert.True(t, names["beta"])
}

func TestScanDirectories_FileEntrySkipped(t *testing.T) {
	tmp := t.TempDir()
	// Create a regular file (not directory) inside the plugin dir.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "not-a-dir.txt"), []byte("hello"), 0644))

	// Also create a valid plugin dir.
	pluginDir := filepath.Join(tmp, "valid")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte("name: valid\nversion: \"1.0\"\n"), 0644))

	manifests, err := ScanDirectories([]string{tmp})
	require.NoError(t, err)
	require.Len(t, manifests, 1)
	assert.Equal(t, "valid", manifests[0].Name)
}

func TestScanDirectories_EmptyDirList(t *testing.T) {
	// nil dirs.
	manifests, err := ScanDirectories(nil)
	require.NoError(t, err)
	assert.Empty(t, manifests)

	// Empty slice.
	manifests, err = ScanDirectories([]string{})
	require.NoError(t, err)
	assert.Empty(t, manifests)
}
