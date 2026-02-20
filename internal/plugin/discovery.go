package plugin

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"alfred-ai/internal/domain"
)

// ScanDirectories walks each directory looking for plugin.yaml manifest files.
func ScanDirectories(dirs []string) ([]domain.PluginManifest, error) {
	var manifests []domain.PluginManifest
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read plugin dir %s: %w", dir, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			manifestPath := filepath.Join(dir, entry.Name(), "plugin.yaml")
			data, err := os.ReadFile(manifestPath)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("read manifest %s: %w", manifestPath, err)
			}
			var m domain.PluginManifest
			if err := yaml.Unmarshal(data, &m); err != nil {
				// Skip malformed manifests
				continue
			}
			if m.Name == "" {
				continue
			}

			// If manifest declares a WASM binary, verify it exists and tag the type.
			if m.WASMConfig != nil && m.WASMConfig.Binary != "" {
				pluginDir := filepath.Join(dir, entry.Name())
				wasmPath := filepath.Join(pluginDir, m.WASMConfig.Binary)
				if _, err := os.Stat(wasmPath); err != nil {
					// .wasm binary missing — skip this plugin.
					continue
				}
				hasWASMType := false
				for _, t := range m.Types {
					if t == domain.PluginTypeWASM {
						hasWASMType = true
						break
					}
				}
				if !hasWASMType {
					m.Types = append(m.Types, domain.PluginTypeWASM)
				}
			}

			manifests = append(manifests, m)
		}
	}
	return manifests, nil
}
