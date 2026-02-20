package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/plugin/wasm"
)

// Compile-time check: Manager implements domain.PluginManager.
var _ domain.PluginManager = (*Manager)(nil)

// Manager manages the lifecycle of in-process plugins.
type Manager struct {
	mu        sync.RWMutex
	plugins   map[string]domain.Plugin
	manifests map[string]domain.PluginManifest
	hooks     []domain.PluginHook
	logger    *slog.Logger
	bus       domain.EventBus
	dirs      []string

	// Permission lists for validation.
	allowPerms []string
	denyPerms  []string

	// WASM runtime shared across all WASM plugins.
	wasmRuntime *wasm.Runtime
}

// NewManager creates a plugin manager.
func NewManager(logger *slog.Logger, bus domain.EventBus, dirs []string, allowPerms, denyPerms []string) *Manager {
	return &Manager{
		plugins:    make(map[string]domain.Plugin),
		manifests:  make(map[string]domain.PluginManifest),
		logger:     logger,
		bus:        bus,
		dirs:       dirs,
		allowPerms: allowPerms,
		denyPerms:  denyPerms,
	}
}

// Discover scans configured directories for plugin manifests.
func (m *Manager) Discover() ([]domain.PluginManifest, error) {
	return ScanDirectories(m.dirs)
}

// Load initialises and registers a plugin.
func (m *Manager) Load(p domain.Plugin) error {
	manifest := p.Manifest()

	if err := ValidatePermissions(manifest, m.allowPerms, m.denyPerms); err != nil {
		return err
	}

	// Pre-check: reject duplicate names before expensive Init.
	m.mu.RLock()
	_, exists := m.plugins[manifest.Name]
	m.mu.RUnlock()
	if exists {
		return fmt.Errorf("%w: %s", domain.ErrDuplicate, manifest.Name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	deps := domain.PluginDeps{
		Logger:   m.logger.With("plugin", manifest.Name),
		EventBus: m.bus,
	}

	if err := p.Init(ctx, deps); err != nil {
		return fmt.Errorf("init plugin %q: %w", manifest.Name, err)
	}

	m.mu.Lock()
	// Double-check after Init to handle TOCTOU race.
	if _, exists := m.plugins[manifest.Name]; exists {
		m.mu.Unlock()
		// Best-effort close; the plugin was already Init'd.
		_ = p.Close()
		return fmt.Errorf("%w: %s", domain.ErrDuplicate, manifest.Name)
	}

	m.plugins[manifest.Name] = p
	m.manifests[manifest.Name] = manifest

	if hook, ok := p.(domain.PluginHook); ok {
		m.hooks = append(m.hooks, hook)
	}
	m.mu.Unlock()

	m.logger.Info("plugin loaded", "name", manifest.Name, "version", manifest.Version)
	m.publishEvent(domain.EventPluginLoaded, manifest.Name)
	return nil
}

// Unload calls Close on a plugin and removes it.
func (m *Manager) Unload(name string) error {
	m.mu.Lock()

	p, ok := m.plugins[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("%w: %s", domain.ErrNotFound, name)
	}

	if err := p.Close(); err != nil {
		m.logger.Warn("plugin close error", "name", name, "error", err)
	}

	delete(m.plugins, name)
	delete(m.manifests, name)

	// Rebuild hooks slice.
	m.hooks = m.hooks[:0]
	for _, pp := range m.plugins {
		if hook, ok := pp.(domain.PluginHook); ok {
			m.hooks = append(m.hooks, hook)
		}
	}
	m.mu.Unlock()

	m.logger.Info("plugin unloaded", "name", name)
	m.publishEvent(domain.EventPluginUnloaded, name)
	return nil
}

// publishEvent publishes a plugin lifecycle event if the bus is available.
func (m *Manager) publishEvent(eventType domain.EventType, pluginName string) {
	if m.bus == nil {
		return
	}
	m.bus.Publish(context.Background(), domain.Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Payload:   mustJSON(map[string]string{"plugin": pluginName}),
	})
}

// mustJSON marshals v to json.RawMessage, panicking on error (programmer error).
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("plugin: marshal event payload: %v", err))
	}
	return b
}

// List returns all loaded plugin manifests.
func (m *Manager) List() []domain.PluginManifest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]domain.PluginManifest, 0, len(m.manifests))
	for _, manifest := range m.manifests {
		result = append(result, manifest)
	}
	return result
}

// GetHooks returns all plugins that implement PluginHook.
func (m *Manager) GetHooks() []domain.PluginHook {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]domain.PluginHook, len(m.hooks))
	copy(result, m.hooks)
	return result
}

// LoadWASM loads a WASM plugin from a manifest that declares a WASMConfig.
// pluginDir is the directory containing the plugin.yaml and .wasm binary.
func (m *Manager) LoadWASM(ctx context.Context, manifest domain.PluginManifest, pluginDir string) error {
	if manifest.WASMConfig == nil || manifest.WASMConfig.Binary == "" {
		return fmt.Errorf("%w: manifest has no wasm config", domain.ErrInvalidInput)
	}

	// Lazily create the shared WASM runtime.
	if m.wasmRuntime == nil {
		rt, err := wasm.NewRuntime(ctx, wasm.DefaultRuntimeConfig(), m.logger)
		if err != nil {
			return fmt.Errorf("create wasm runtime: %w", err)
		}
		m.wasmRuntime = rt
	}

	// Build sandbox from manifest config.
	sandbox := wasm.NewSandbox(*manifest.WASMConfig, m.logger.With("plugin", manifest.Name))

	wasmPath := filepath.Join(pluginDir, manifest.WASMConfig.Binary)
	plugin, err := wasm.LoadPlugin(ctx, m.wasmRuntime, wasmPath, manifest, sandbox)
	if err != nil {
		return err
	}

	return m.Load(plugin)
}

// DiscoverAndLoadWASM discovers plugins and auto-loads any WASM plugins found.
func (m *Manager) DiscoverAndLoadWASM(ctx context.Context) ([]domain.PluginManifest, error) {
	manifests, err := m.Discover()
	if err != nil {
		return nil, err
	}

	for _, manifest := range manifests {
		if manifest.WASMConfig == nil || manifest.WASMConfig.Binary == "" {
			continue
		}

		// Find the plugin directory by scanning dirs.
		for _, dir := range m.dirs {
			pluginDir := filepath.Join(dir, manifest.Name)
			wasmPath := filepath.Join(pluginDir, manifest.WASMConfig.Binary)
			if _, statErr := filepath.Abs(wasmPath); statErr == nil {
				if loadErr := m.LoadWASM(ctx, manifest, pluginDir); loadErr != nil {
					m.logger.Warn("failed to load wasm plugin", "name", manifest.Name, "error", loadErr)
				}
				break
			}
		}
	}

	return manifests, nil
}

// Shutdown closes the WASM runtime and all loaded plugins.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, p := range m.plugins {
		if err := p.Close(); err != nil {
			m.logger.Warn("plugin close error during shutdown", "name", name, "error", err)
		}
		delete(m.plugins, name)
		delete(m.manifests, name)
	}
	m.hooks = m.hooks[:0]

	if m.wasmRuntime != nil {
		if err := m.wasmRuntime.Close(ctx); err != nil {
			return err
		}
		m.wasmRuntime = nil
	}

	return nil
}
