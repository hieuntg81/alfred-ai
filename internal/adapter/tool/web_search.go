package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/tracer"
)

const (
	defaultSearchCount = 5
	maxSearchCount     = 20
	defaultCacheTTL    = 15 * time.Minute
)

// cacheEntry holds a cached search result with its expiration time.
type cacheEntry struct {
	result    string
	expiresAt time.Time
}

// WebSearchTool performs web searches via a pluggable SearchBackend.
type WebSearchTool struct {
	backend  SearchBackend
	cacheTTL time.Duration
	logger   *slog.Logger

	mu    sync.Mutex
	cache map[string]cacheEntry
}

// NewWebSearchTool creates a web search tool backed by the given SearchBackend.
func NewWebSearchTool(backend SearchBackend, cacheTTL time.Duration, logger *slog.Logger) *WebSearchTool {
	if cacheTTL <= 0 {
		cacheTTL = defaultCacheTTL
	}
	return &WebSearchTool{
		backend:  backend,
		cacheTTL: cacheTTL,
		logger:   logger,
		cache:    make(map[string]cacheEntry),
	}
}

func (t *WebSearchTool) Name() string        { return "web_search" }
func (t *WebSearchTool) Description() string { return "Search the web" }

func (t *WebSearchTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"query": {"type": "string", "description": "The search query"},
				"count": {"type": "integer", "minimum": 1, "maximum": 20, "description": "Number of results (default: 5)"},
				"time_range": {"type": "string", "enum": ["day", "week", "month", "year"], "description": "Time range filter (optional)"}
			},
			"required": ["query"]
		}`),
	}
}

type webSearchParams struct {
	Query     string `json:"query"`
	Count     int    `json:"count,omitempty"`
	TimeRange string `json:"time_range,omitempty"`
}

func (t *WebSearchTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	return Execute(ctx, "tool.web_search", t.logger, params,
		func(ctx context.Context, span trace.Span, p webSearchParams) (any, error) {
			// Validate query
			if strings.TrimSpace(p.Query) == "" {
				return nil, fmt.Errorf("query must not be empty")
			}

			span.SetAttributes(tracer.StringAttr("tool.query", p.Query))

			// Validate and default count
			if p.Count <= 0 {
				p.Count = defaultSearchCount
			}
			if p.Count > maxSearchCount {
				p.Count = maxSearchCount
			}

			// Validate time_range
			if err := ValidateEnum("time_range", p.TimeRange, "day", "week", "month", "year"); err != nil {
				return nil, err
			}

			// Build cache key
			cacheKey := fmt.Sprintf("%s|%d|%s", p.Query, p.Count, p.TimeRange)

			// Check cache
			if cached, ok := t.getCached(cacheKey); ok {
				t.logger.Debug("web search cache hit", "query", p.Query)
				span.SetAttributes(tracer.StringAttr("tool.cache", "hit"))
				return cached, nil
			}

			// Execute search via backend
			results, err := t.backend.Search(ctx, p.Query, p.Count, p.TimeRange)
			if err != nil {
				return nil, err
			}

			// Defensive: cap results to requested count
			if len(results) > p.Count {
				results = results[:p.Count]
			}

			// Format results for LLM
			content := formatSearchResults(p.Query, results)

			// Store in cache
			t.putCache(cacheKey, content)

			t.logger.Debug("web search completed", "query", p.Query, "results", len(results))
			return content, nil
		},
	)
}

// formatSearchResults converts search results to a compact text format for LLM consumption.
func formatSearchResults(query string, results []SearchResult) string {
	if len(results) == 0 {
		return fmt.Sprintf("No search results found for %q.", query)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Search results for %q:\n\n", query)
	for i, r := range results {
		fmt.Fprintf(&sb, "%d. %s\n   URL: %s\n   %s\n\n", i+1, r.Title, r.URL, r.Content)
	}
	return sb.String()
}

// getCached returns a cached result if it exists and has not expired.
func (t *WebSearchTool) getCached(key string) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.cache[key]
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expiresAt) {
		delete(t.cache, key)
		return "", false
	}
	return entry.result, true
}

// putCache stores a result in the cache with the configured TTL.
func (t *WebSearchTool) putCache(key, result string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.cache[key] = cacheEntry{
		result:    result,
		expiresAt: time.Now().Add(t.cacheTTL),
	}

	// Lazy eviction: remove expired entries if cache grows large
	if len(t.cache) > 100 {
		now := time.Now()
		for k, v := range t.cache {
			if now.After(v.expiresAt) {
				delete(t.cache, k)
			}
		}
	}
}
