package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RegistryEntry describes a plugin available in the remote registry.
type RegistryEntry struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	RepoURL     string   `json:"repo_url"`
	DownloadURL string   `json:"download_url"` // direct URL to .tar.gz
	Checksum    string   `json:"checksum"`      // SHA256 hex
	Types       []string `json:"types"`
	Tags        []string `json:"tags"`
	Verified    bool     `json:"verified"`
	MinVersion  string   `json:"min_version"` // minimum alfredai version
}

// Registry is a client for the remote plugin registry (a JSON file on GitHub).
type Registry struct {
	url      string
	cacheDir string
	cacheTTL time.Duration
	client   *http.Client

	mu      sync.RWMutex
	entries []RegistryEntry
	fetched time.Time

	logger *slog.Logger
}

// NewRegistry creates a registry client. The cacheDir is used to store a local
// copy of the registry index.
func NewRegistry(url, cacheDir string, logger *slog.Logger) *Registry {
	return &Registry{
		url:      url,
		cacheDir: cacheDir,
		cacheTTL: 15 * time.Minute,
		client:   &http.Client{Timeout: 30 * time.Second},
		logger:   logger,
	}
}

// Refresh fetches the latest registry index from the remote URL.
func (r *Registry) Refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return fmt.Errorf("registry request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("registry fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registry fetch: HTTP %d", resp.StatusCode)
	}

	var entries []RegistryEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return fmt.Errorf("registry decode: %w", err)
	}

	r.mu.Lock()
	r.entries = entries
	r.fetched = time.Now()
	r.mu.Unlock()

	// Cache to disk.
	if r.cacheDir != "" {
		if err := r.saveCache(entries); err != nil {
			r.logger.Warn("failed to cache registry index", "error", err)
		}
	}

	return nil
}

// Entries returns all registry entries, refreshing if stale.
func (r *Registry) Entries(ctx context.Context) ([]RegistryEntry, error) {
	r.mu.RLock()
	stale := time.Since(r.fetched) > r.cacheTTL
	entries := r.entries
	r.mu.RUnlock()

	if !stale && entries != nil {
		return entries, nil
	}

	// Try loading from cache first.
	if entries == nil && r.cacheDir != "" {
		if cached, err := r.loadCache(); err == nil {
			r.mu.Lock()
			r.entries = cached
			r.mu.Unlock()
			entries = cached
		}
	}

	// Refresh from remote.
	if err := r.Refresh(ctx); err != nil {
		if entries != nil {
			// Return stale cache on network error.
			r.logger.Warn("using stale registry cache", "error", err)
			return entries, nil
		}
		return nil, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.entries, nil
}

// Search returns entries matching the query (case-insensitive substring match
// against name, description, tags, and author).
func (r *Registry) Search(ctx context.Context, query string) ([]RegistryEntry, error) {
	entries, err := r.Entries(ctx)
	if err != nil {
		return nil, err
	}

	q := strings.ToLower(query)
	var results []RegistryEntry
	for _, e := range entries {
		if matchesQuery(e, q) {
			results = append(results, e)
		}
	}
	return results, nil
}

// Get returns a specific entry by name.
func (r *Registry) Get(ctx context.Context, name string) (*RegistryEntry, error) {
	entries, err := r.Entries(ctx)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].Name == name {
			return &entries[i], nil
		}
	}
	return nil, fmt.Errorf("plugin %q not found in registry", name)
}

func matchesQuery(e RegistryEntry, q string) bool {
	if strings.Contains(strings.ToLower(e.Name), q) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Description), q) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Author), q) {
		return true
	}
	for _, tag := range e.Tags {
		if strings.Contains(strings.ToLower(tag), q) {
			return true
		}
	}
	return false
}

func (r *Registry) saveCache(entries []RegistryEntry) error {
	if err := os.MkdirAll(r.cacheDir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.cacheDir, "registry.json"), data, 0o644)
}

func (r *Registry) loadCache() ([]RegistryEntry, error) {
	data, err := os.ReadFile(filepath.Join(r.cacheDir, "registry.json"))
	if err != nil {
		return nil, err
	}
	var entries []RegistryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}
