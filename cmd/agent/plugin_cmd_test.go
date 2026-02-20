package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"alfred-ai/internal/infra/config"
	"alfred-ai/internal/plugin"
)

func TestPluginYAMLTemplate(t *testing.T) {
	yaml := pluginYAMLTemplate("myplugin")
	assert.Contains(t, yaml, "name: myplugin")
	assert.Contains(t, yaml, "plugin.wasm")
	assert.Contains(t, yaml, "max_memory_mb: 64")
	assert.Contains(t, yaml, "exec_timeout: 30s")
}

func TestPluginMainGoTemplate(t *testing.T) {
	code := pluginMainGoTemplate("myplugin")
	assert.Contains(t, code, "//go:build tinygo")
	assert.Contains(t, code, "//go:wasmimport alfred_v1 log")
	assert.Contains(t, code, "//export malloc")
	assert.Contains(t, code, "//export tool_execute")
	assert.Contains(t, code, "myplugin plugin initialized")
}

func TestPluginMakefileTemplate(t *testing.T) {
	makefile := pluginMakefileTemplate()
	assert.Contains(t, makefile, "tinygo build")
	assert.Contains(t, makefile, "plugin.wasm")
	assert.Contains(t, makefile, "-target wasi")
}

func TestPluginReadmeTemplate(t *testing.T) {
	readme := pluginReadmeTemplate("myplugin")
	assert.Contains(t, readme, "# myplugin")
	assert.Contains(t, readme, "make build")
}

func TestRunPluginInit(t *testing.T) {
	dir := t.TempDir()
	name := "testplugin"
	pluginDir := filepath.Join(dir, name)

	// Override os.Args to avoid interference.
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// Run init in the temp dir.
	oldWd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldWd)

	err := runPluginInit(name)
	require.NoError(t, err)

	// Verify files exist.
	assert.FileExists(t, filepath.Join(pluginDir, "plugin.yaml"))
	assert.FileExists(t, filepath.Join(pluginDir, "main.go"))
	assert.FileExists(t, filepath.Join(pluginDir, "Makefile"))
	assert.FileExists(t, filepath.Join(pluginDir, "README.md"))
}

func TestRunPluginInit_InvalidName(t *testing.T) {
	err := runPluginInit("bad/name")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid plugin name")
}

func TestRunPluginInit_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	name := "existing"
	os.MkdirAll(filepath.Join(dir, name), 0o755)

	oldWd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldWd)

	err := runPluginInit(name)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestRunPluginValidate_MissingManifest(t *testing.T) {
	dir := t.TempDir()
	err := runPluginValidate(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read manifest")
}

func TestRunPluginValidate_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(":::bad yaml"), 0o644)
	err := runPluginValidate(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestRunPluginValidate_MissingFields(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte("description: test\n"), 0o644)
	err := runPluginValidate(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestRunPluginValidate_ValidNonWASM(t *testing.T) {
	dir := t.TempDir()
	manifest := `name: testplugin
version: "1.0.0"
types:
  - tool
`
	os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(manifest), 0o644)
	err := runPluginValidate(dir)
	require.NoError(t, err)
}

func TestRegistryAndInstaller(t *testing.T) {
	cfg := config.Defaults()
	cfg.Plugins.RegistryURL = "https://example.com/plugins.json"
	cfg.Plugins.Dirs = []string{t.TempDir()}

	reg, inst := registryAndInstaller(cfg)
	assert.NotNil(t, reg)
	assert.NotNil(t, inst)
}

func TestRegistryAndInstaller_Defaults(t *testing.T) {
	cfg := config.Defaults()
	reg, inst := registryAndInstaller(cfg)
	assert.NotNil(t, reg)
	assert.NotNil(t, inst)
}

func TestRunPluginSearch_ServerDown(t *testing.T) {
	// runPluginSearch reads config from disk. Test the registry directly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entries := []plugin.RegistryEntry{
			{Name: "test-finder", Version: "1.0.0", Description: "Find things", Tags: []string{"search"}},
			{Name: "test-writer", Version: "2.0.0", Description: "Write things", Tags: []string{"io"}},
		}
		json.NewEncoder(w).Encode(entries)
	}))
	defer srv.Close()

	reg := plugin.NewRegistry(srv.URL, t.TempDir(), slogDiscard())
	results, err := reg.Search(t.Context(), "find")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "test-finder", results[0].Name)
}

func TestRunPluginPublish_Valid(t *testing.T) {
	dir := t.TempDir()
	manifest := `name: myplugin
version: "1.0.0"
description: "A test plugin"
author: "Test Author"
types:
  - tool
`
	os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(manifest), 0o644)

	err := runPluginPublish(dir)
	require.NoError(t, err)
}

func TestRunPluginPublish_MissingManifest(t *testing.T) {
	dir := t.TempDir()
	err := runPluginPublish(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read manifest")
}

func TestRunPluginPublish_MissingFields(t *testing.T) {
	dir := t.TempDir()
	// Valid YAML but missing required publishing fields (description, author).
	manifest := `name: incomplete
version: "1.0.0"
types:
  - tool
`
	os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(manifest), 0o644)

	err := runPluginPublish(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "publish validation failed")
}

func TestRunPluginPublish_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(":::bad"), 0o644)

	err := runPluginPublish(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "malformed plugin.yaml")
}

func TestRunPluginRemove_Direct(t *testing.T) {
	pluginDir := t.TempDir()
	name := "removable"
	os.MkdirAll(filepath.Join(pluginDir, name), 0o755)
	os.WriteFile(filepath.Join(pluginDir, name, "plugin.yaml"), []byte("name: removable\n"), 0o644)

	inst := plugin.NewInstaller(pluginDir, nil, slogDiscard())
	err := inst.Remove(name)
	require.NoError(t, err)
	assert.NoDirExists(t, filepath.Join(pluginDir, name))
}
