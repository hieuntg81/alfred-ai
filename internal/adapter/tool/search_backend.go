package tool

import "context"

// SearchBackend abstracts a web search engine.
type SearchBackend interface {
	// Search performs a web search and returns results.
	Search(ctx context.Context, query string, count int, timeRange string) ([]SearchResult, error)
	// Name returns the backend identifier (e.g. "searxng").
	Name() string
}

// SearchResult represents a single search result.
type SearchResult struct {
	Title   string
	URL     string
	Content string
}
