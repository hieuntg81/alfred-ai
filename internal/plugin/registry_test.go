package plugin

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func sampleEntries() []RegistryEntry {
	return []RegistryEntry{
		{
			Name:        "hello-world",
			Version:     "1.0.0",
			Description: "A simple hello world plugin",
			Author:      "alfred-team",
			DownloadURL: "https://example.com/hello-world-1.0.0.tar.gz",
			Checksum:    "abc123",
			Types:       []string{"tool"},
			Tags:        []string{"demo", "starter"},
			Verified:    true,
		},
		{
			Name:        "web-scraper",
			Version:     "2.1.0",
			Description: "Scrape web pages and extract content",
			Author:      "community",
			DownloadURL: "https://example.com/web-scraper-2.1.0.tar.gz",
			Checksum:    "def456",
			Types:       []string{"tool"},
			Tags:        []string{"web", "scraping", "http"},
			Verified:    false,
		},
		{
			Name:        "memory-redis",
			Version:     "0.5.0",
			Description: "Redis-backed memory provider",
			Author:      "alfred-team",
			DownloadURL: "https://example.com/memory-redis-0.5.0.tar.gz",
			Checksum:    "ghi789",
			Types:       []string{"memory"},
			Tags:        []string{"redis", "memory", "cache"},
			Verified:    true,
		},
	}
}

func startRegistryServer(t *testing.T, entries []RegistryEntry) *httptest.Server {
	t.Helper()
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))
}

func TestRegistryRefresh(t *testing.T) {
	entries := sampleEntries()
	srv := startRegistryServer(t, entries)
	defer srv.Close()

	reg := NewRegistry(srv.URL, t.TempDir(), testLogger())
	if err := reg.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	got, err := reg.Entries(context.Background())
	if err != nil {
		t.Fatalf("entries: %v", err)
	}

	if len(got) != len(entries) {
		t.Fatalf("entries count = %d, want %d", len(got), len(entries))
	}
}

func TestRegistrySearch(t *testing.T) {
	entries := sampleEntries()
	srv := startRegistryServer(t, entries)
	defer srv.Close()

	reg := NewRegistry(srv.URL, t.TempDir(), testLogger())

	tests := []struct {
		query string
		want  int
	}{
		{"hello", 1},
		{"web", 1},
		{"redis", 1},
		{"tool", 0}, // tool is a type, not in name/desc/tags unless it matches
		{"alfred-team", 2},
		{"scraping", 1},
		{"nonexistent", 0},
	}

	for _, tt := range tests {
		results, err := reg.Search(context.Background(), tt.query)
		if err != nil {
			t.Fatalf("search %q: %v", tt.query, err)
		}
		if len(results) != tt.want {
			t.Errorf("search(%q) = %d results, want %d", tt.query, len(results), tt.want)
		}
	}
}

func TestRegistryGet(t *testing.T) {
	entries := sampleEntries()
	srv := startRegistryServer(t, entries)
	defer srv.Close()

	reg := NewRegistry(srv.URL, t.TempDir(), testLogger())

	entry, err := reg.Get(context.Background(), "web-scraper")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if entry.Version != "2.1.0" {
		t.Errorf("version = %q, want 2.1.0", entry.Version)
	}

	_, err = reg.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent plugin")
	}
}

func TestRegistryCache(t *testing.T) {
	entries := sampleEntries()
	srv := startRegistryServer(t, entries)
	defer srv.Close()

	cacheDir := t.TempDir()

	// First registry fetches and caches.
	reg1 := NewRegistry(srv.URL, cacheDir, testLogger())
	if err := reg1.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	// Verify cache file exists.
	cachePath := filepath.Join(cacheDir, "registry.json")
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cache file missing: %v", err)
	}

	// Second registry loads from cache (server shut down).
	srv.Close()
	reg2 := NewRegistry("http://invalid-url", cacheDir, testLogger())
	got, err := reg2.Entries(context.Background())
	if err != nil {
		t.Fatalf("entries from cache: %v", err)
	}
	if len(got) != len(entries) {
		t.Fatalf("cached entries count = %d, want %d", len(got), len(entries))
	}
}

func TestRegistryHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	reg := NewRegistry(srv.URL, t.TempDir(), testLogger())
	err := reg.Refresh(context.Background())
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}

func TestRegistryInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	reg := NewRegistry(srv.URL, t.TempDir(), testLogger())
	err := reg.Refresh(context.Background())
	if err == nil {
		t.Fatal("expected error on invalid JSON")
	}
}
