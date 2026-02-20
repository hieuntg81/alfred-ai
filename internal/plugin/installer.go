package plugin

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"alfred-ai/internal/domain"
)

// Installer handles downloading, verifying, and installing plugins from the registry.
type Installer struct {
	pluginDir string
	registry  *Registry
	client    *http.Client
	logger    *slog.Logger
}

// NewInstaller creates a new plugin installer.
func NewInstaller(pluginDir string, registry *Registry, logger *slog.Logger) *Installer {
	return &Installer{
		pluginDir: pluginDir,
		registry:  registry,
		client:    &http.Client{Timeout: 5 * time.Minute},
		logger:    logger,
	}
}

// Install downloads and installs a plugin by name from the registry.
func (i *Installer) Install(ctx context.Context, name string) error {
	entry, err := i.registry.Get(ctx, name)
	if err != nil {
		return err
	}

	destDir := filepath.Join(i.pluginDir, name)
	if _, err := os.Stat(destDir); err == nil {
		return fmt.Errorf("plugin %q already installed at %s", name, destDir)
	}

	return i.downloadAndExtract(ctx, entry, destDir)
}

// Update re-installs a plugin (remove + install).
func (i *Installer) Update(ctx context.Context, name string) error {
	entry, err := i.registry.Get(ctx, name)
	if err != nil {
		return err
	}

	destDir := filepath.Join(i.pluginDir, name)
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		return fmt.Errorf("plugin %q is not installed", name)
	}

	// Remove old version.
	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("remove old version: %w", err)
	}

	return i.downloadAndExtract(ctx, entry, destDir)
}

// Remove deletes an installed plugin.
func (i *Installer) Remove(name string) error {
	destDir := filepath.Join(i.pluginDir, name)
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		return fmt.Errorf("plugin %q is not installed", name)
	}

	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("remove plugin: %w", err)
	}
	return nil
}

// Installed returns manifests for all locally installed plugins.
func (i *Installer) Installed() ([]InstalledPlugin, error) {
	manifests, err := ScanDirectories([]string{i.pluginDir})
	if err != nil {
		return nil, err
	}
	var installed []InstalledPlugin
	for _, m := range manifests {
		installed = append(installed, InstalledPlugin{
			Name:    m.Name,
			Version: m.Version,
			Types:   m.Types,
		})
	}
	return installed, nil
}

// InstalledPlugin holds basic info about a locally installed plugin.
type InstalledPlugin struct {
	Name    string
	Version string
	Types   []domain.PluginType
}

func (i *Installer) downloadAndExtract(ctx context.Context, entry *RegistryEntry, destDir string) error {
	if entry.DownloadURL == "" {
		return fmt.Errorf("plugin %q has no download URL", entry.Name)
	}

	// Download to temp file.
	tmpFile, err := os.CreateTemp("", "alfredai-plugin-*.tar.gz")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.DownloadURL, nil)
	if err != nil {
		return fmt.Errorf("download request: %w", err)
	}

	resp, err := i.client.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	// Write to temp file and compute checksum simultaneously.
	hasher := sha256.New()
	writer := io.MultiWriter(tmpFile, hasher)

	if _, err := io.Copy(writer, resp.Body); err != nil {
		return fmt.Errorf("download write: %w", err)
	}

	// Verify checksum.
	gotChecksum := hex.EncodeToString(hasher.Sum(nil))
	if entry.Checksum != "" && gotChecksum != entry.Checksum {
		return fmt.Errorf("checksum mismatch: got %s, want %s", gotChecksum, entry.Checksum)
	}

	// Extract tar.gz.
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek temp file: %w", err)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create plugin dir: %w", err)
	}
	if err := extractTarGz(tmpFile, destDir); err != nil {
		// Cleanup on failure.
		os.RemoveAll(destDir)
		return fmt.Errorf("extract: %w", err)
	}

	// Validate the extracted plugin.
	manifests, err := ScanDirectories([]string{filepath.Dir(destDir)})
	if err != nil {
		os.RemoveAll(destDir)
		return fmt.Errorf("validate installed plugin: %w", err)
	}

	found := false
	for _, m := range manifests {
		if m.Name == entry.Name {
			found = true
			break
		}
	}
	if !found {
		os.RemoveAll(destDir)
		return fmt.Errorf("installed plugin %q has no valid manifest", entry.Name)
	}

	i.logger.Info("plugin installed", "name", entry.Name, "version", entry.Version)
	return nil
}

// extractTarGz extracts a .tar.gz file to destDir.
func extractTarGz(r io.Reader, destDir string) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar next: %w", err)
		}

		// Security: prevent path traversal.
		target := filepath.Join(destDir, header.Name)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(os.PathSeparator)) &&
			filepath.Clean(target) != filepath.Clean(destDir) {
			return fmt.Errorf("path traversal detected: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("mkdir parent %s: %w", target, err)
			}

			// Limit file size to 100MB.
			const maxFileSize = 100 << 20
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode)&0o755)
			if err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
			if _, err := io.Copy(f, io.LimitReader(tr, maxFileSize)); err != nil {
				f.Close()
				return fmt.Errorf("write %s: %w", target, err)
			}
			f.Close()
		}
	}
	return nil
}
