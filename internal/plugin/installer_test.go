package plugin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// makePluginTarGz creates a .tar.gz containing plugin files (no top-level directory).
// The installer creates the target directory; the tar contents go directly into it.
func makePluginTarGz(t *testing.T, name string) ([]byte, string) {
	t.Helper()

	manifest := `name: ` + name + `
version: "1.0.0"
description: "Test plugin"
author: "test"
types:
  - tool
`
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	// Add manifest directly (no leading directory).
	manifestBytes := []byte(manifest)
	tw.WriteHeader(&tar.Header{
		Name:     "plugin.yaml",
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     int64(len(manifestBytes)),
	})
	tw.Write(manifestBytes)

	tw.Close()
	gzw.Close()

	data := buf.Bytes()
	hash := sha256.Sum256(data)
	return data, hex.EncodeToString(hash[:])
}

func TestInstallerInstall(t *testing.T) {
	tarData, checksum := makePluginTarGz(t, "test-plugin")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/plugins.json" {
			entries := []RegistryEntry{{
				Name:        "test-plugin",
				Version:     "1.0.0",
				Description: "A test plugin",
				DownloadURL: "http://" + r.Host + "/download/test-plugin-1.0.0.tar.gz",
				Checksum:    checksum,
				Types:       []string{"tool"},
			}}
			json.NewEncoder(w).Encode(entries)
			return
		}
		if r.URL.Path == "/download/test-plugin-1.0.0.tar.gz" {
			w.Write(tarData)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	pluginDir := t.TempDir()
	reg := NewRegistry(srv.URL+"/plugins.json", t.TempDir(), testLogger())
	inst := NewInstaller(pluginDir, reg, testLogger())

	if err := inst.Install(context.Background(), "test-plugin"); err != nil {
		t.Fatalf("install: %v", err)
	}

	// Verify plugin directory exists.
	manifestPath := filepath.Join(pluginDir, "test-plugin", "plugin.yaml")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
}

func TestInstallerInstallAlreadyExists(t *testing.T) {
	tarData, checksum := makePluginTarGz(t, "existing")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/plugins.json" {
			entries := []RegistryEntry{{
				Name:        "existing",
				Version:     "1.0.0",
				DownloadURL: "http://" + r.Host + "/dl",
				Checksum:    checksum,
			}}
			json.NewEncoder(w).Encode(entries)
			return
		}
		w.Write(tarData)
	}))
	defer srv.Close()

	pluginDir := t.TempDir()
	os.MkdirAll(filepath.Join(pluginDir, "existing"), 0o755)

	reg := NewRegistry(srv.URL+"/plugins.json", t.TempDir(), testLogger())
	inst := NewInstaller(pluginDir, reg, testLogger())

	err := inst.Install(context.Background(), "existing")
	if err == nil {
		t.Fatal("expected error for already installed plugin")
	}
}

func TestInstallerUpdate(t *testing.T) {
	tarData, checksum := makePluginTarGz(t, "update-me")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/plugins.json" {
			entries := []RegistryEntry{{
				Name:        "update-me",
				Version:     "2.0.0",
				DownloadURL: "http://" + r.Host + "/download",
				Checksum:    checksum,
				Types:       []string{"tool"},
			}}
			json.NewEncoder(w).Encode(entries)
			return
		}
		w.Write(tarData)
	}))
	defer srv.Close()

	pluginDir := t.TempDir()
	// Create old version.
	oldDir := filepath.Join(pluginDir, "update-me")
	os.MkdirAll(oldDir, 0o755)
	os.WriteFile(filepath.Join(oldDir, "plugin.yaml"), []byte("name: update-me\nversion: \"1.0.0\"\ntypes:\n  - tool\n"), 0o644)

	reg := NewRegistry(srv.URL+"/plugins.json", t.TempDir(), testLogger())
	inst := NewInstaller(pluginDir, reg, testLogger())

	if err := inst.Update(context.Background(), "update-me"); err != nil {
		t.Fatalf("update: %v", err)
	}

	// Verify new version exists.
	if _, err := os.Stat(filepath.Join(pluginDir, "update-me", "plugin.yaml")); err != nil {
		t.Fatalf("manifest missing after update: %v", err)
	}
}

func TestInstallerUpdateNotInstalled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entries := []RegistryEntry{{Name: "missing", Version: "1.0.0"}}
		json.NewEncoder(w).Encode(entries)
	}))
	defer srv.Close()

	reg := NewRegistry(srv.URL, t.TempDir(), testLogger())
	inst := NewInstaller(t.TempDir(), reg, testLogger())

	err := inst.Update(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for not-installed plugin")
	}
}

func TestInstallerRemove(t *testing.T) {
	pluginDir := t.TempDir()
	os.MkdirAll(filepath.Join(pluginDir, "removable"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, "removable", "plugin.yaml"), []byte("name: removable\n"), 0o644)

	inst := NewInstaller(pluginDir, nil, testLogger())

	if err := inst.Remove("removable"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pluginDir, "removable")); !os.IsNotExist(err) {
		t.Fatal("plugin directory should be removed")
	}
}

func TestInstallerRemoveNotInstalled(t *testing.T) {
	inst := NewInstaller(t.TempDir(), nil, testLogger())
	err := inst.Remove("nonexistent")
	if err == nil {
		t.Fatal("expected error for not-installed plugin")
	}
}

func TestInstallerChecksumMismatch(t *testing.T) {
	tarData, _ := makePluginTarGz(t, "bad-checksum")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/plugins.json" {
			entries := []RegistryEntry{{
				Name:        "bad-checksum",
				Version:     "1.0.0",
				DownloadURL: "http://" + r.Host + "/download",
				Checksum:    "0000000000000000000000000000000000000000000000000000000000000000",
			}}
			json.NewEncoder(w).Encode(entries)
			return
		}
		w.Write(tarData)
	}))
	defer srv.Close()

	reg := NewRegistry(srv.URL+"/plugins.json", t.TempDir(), testLogger())
	inst := NewInstaller(t.TempDir(), reg, testLogger())

	err := inst.Install(context.Background(), "bad-checksum")
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
}

func TestInstallerInstalled(t *testing.T) {
	pluginDir := t.TempDir()
	// Create two plugins.
	for _, name := range []string{"plugin-a", "plugin-b"} {
		dir := filepath.Join(pluginDir, name)
		os.MkdirAll(dir, 0o755)
		manifest := "name: " + name + "\nversion: \"1.0.0\"\ntypes:\n  - tool\n"
		os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(manifest), 0o644)
	}

	inst := NewInstaller(pluginDir, nil, testLogger())
	installed, err := inst.Installed()
	if err != nil {
		t.Fatalf("installed: %v", err)
	}
	if len(installed) != 2 {
		t.Fatalf("installed count = %d, want 2", len(installed))
	}
}

func TestExtractTarGzPathTraversal(t *testing.T) {
	// Create a tar with a path traversal attempt.
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	tw.WriteHeader(&tar.Header{
		Name:     "../../../etc/passwd",
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     5,
	})
	tw.Write([]byte("owned"))
	tw.Close()
	gzw.Close()

	destDir := t.TempDir()
	err := extractTarGz(&buf, destDir)
	if err == nil {
		t.Fatal("expected path traversal error")
	}
}
