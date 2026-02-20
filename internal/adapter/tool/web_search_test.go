package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// mockSearchBackend implements SearchBackend for testing.
type mockSearchBackend struct {
	results   []SearchResult
	err       error
	callCount int
}

func (m *mockSearchBackend) Search(_ context.Context, _ string, _ int, _ string) ([]SearchResult, error) {
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	return m.results, nil
}

func (m *mockSearchBackend) Name() string { return "mock" }

func newMockBackend(results []SearchResult) *mockSearchBackend {
	return &mockSearchBackend{results: results}
}

func TestWebSearchToolName(t *testing.T) {
	ws := NewWebSearchTool(newMockBackend(nil), 0, newTestLogger())
	if ws.Name() != "web_search" {
		t.Errorf("Name() = %q, want %q", ws.Name(), "web_search")
	}
}

func TestWebSearchToolDescription(t *testing.T) {
	ws := NewWebSearchTool(newMockBackend(nil), 0, newTestLogger())
	if ws.Description() == "" {
		t.Error("Description() returned empty string")
	}
}

func TestWebSearchToolSchema(t *testing.T) {
	ws := NewWebSearchTool(newMockBackend(nil), 0, newTestLogger())
	schema := ws.Schema()
	if schema.Name != "web_search" {
		t.Errorf("Schema.Name = %q, want %q", schema.Name, "web_search")
	}
	if schema.Parameters == nil {
		t.Error("Schema.Parameters is nil")
	}
	var params map[string]interface{}
	if err := json.Unmarshal(schema.Parameters, &params); err != nil {
		t.Errorf("Schema.Parameters is invalid JSON: %v", err)
	}
}

func TestWebSearchToolInvalidJSON(t *testing.T) {
	ws := NewWebSearchTool(newMockBackend(nil), 0, newTestLogger())
	result, err := ws.Execute(context.Background(), json.RawMessage(`invalid`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestWebSearchToolEmptyQuery(t *testing.T) {
	ws := NewWebSearchTool(newMockBackend(nil), 0, newTestLogger())
	params, _ := json.Marshal(webSearchParams{Query: ""})
	result, err := ws.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for empty query")
	}
}

func TestWebSearchToolWhitespaceQuery(t *testing.T) {
	ws := NewWebSearchTool(newMockBackend(nil), 0, newTestLogger())
	params, _ := json.Marshal(webSearchParams{Query: "   "})
	result, err := ws.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for whitespace-only query")
	}
}

func TestWebSearchToolInvalidTimeRange(t *testing.T) {
	ws := NewWebSearchTool(newMockBackend(nil), 0, newTestLogger())
	params, _ := json.Marshal(webSearchParams{Query: "test", TimeRange: "invalid"})
	result, err := ws.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for invalid time_range")
	}
}

func TestWebSearchToolSuccess(t *testing.T) {
	backend := newMockBackend([]SearchResult{
		{Title: "Go Testing", URL: "https://go.dev/testing", Content: "Testing in Go"},
	})
	ws := NewWebSearchTool(backend, 0, newTestLogger())

	params, _ := json.Marshal(webSearchParams{Query: "golang testing"})
	result, err := ws.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Go Testing") {
		t.Errorf("result missing title, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "https://go.dev/testing") {
		t.Errorf("result missing URL, got: %s", result.Content)
	}
}

func TestWebSearchToolBackendError(t *testing.T) {
	backend := &mockSearchBackend{err: fmt.Errorf("connection refused")}
	ws := NewWebSearchTool(backend, 0, newTestLogger())

	params, _ := json.Marshal(webSearchParams{Query: "test"})
	result, err := ws.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for backend failure")
	}
}

func TestWebSearchToolCacheHit(t *testing.T) {
	backend := newMockBackend([]SearchResult{
		{Title: "Cached", URL: "https://example.com", Content: "cached result"},
	})
	ws := NewWebSearchTool(backend, 5*time.Minute, newTestLogger())

	params, _ := json.Marshal(webSearchParams{Query: "cache test"})

	// First call - should hit backend
	result1, _ := ws.Execute(context.Background(), params)
	if result1.IsError {
		t.Fatalf("first call error: %s", result1.Content)
	}

	// Second call - should hit cache
	result2, _ := ws.Execute(context.Background(), params)
	if result2.IsError {
		t.Fatalf("second call error: %s", result2.Content)
	}

	if backend.callCount != 1 {
		t.Errorf("expected 1 backend call, got %d", backend.callCount)
	}
	if result1.Content != result2.Content {
		t.Error("cached result differs from original")
	}
}

func TestWebSearchToolCountDefaults(t *testing.T) {
	backend := newMockBackend(nil)
	ws := NewWebSearchTool(backend, 0, newTestLogger())

	params, _ := json.Marshal(webSearchParams{Query: "test"})
	result, _ := ws.Execute(context.Background(), params)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestWebSearchToolCountClamped(t *testing.T) {
	var results []SearchResult
	for i := 0; i < 25; i++ {
		results = append(results, SearchResult{
			Title:   fmt.Sprintf("R%d", i),
			URL:     fmt.Sprintf("https://example.com/%d", i),
			Content: fmt.Sprintf("d%d", i),
		})
	}
	backend := newMockBackend(results)
	ws := NewWebSearchTool(backend, 0, newTestLogger())

	// Request count=50, should be clamped to 20
	params, _ := json.Marshal(webSearchParams{Query: "test", Count: 50})
	result, _ := ws.Execute(context.Background(), params)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	lines := strings.Split(result.Content, "\n")
	numbered := 0
	for _, line := range lines {
		if len(line) > 0 && line[0] >= '1' && line[0] <= '9' {
			numbered++
		}
	}
	if numbered > 20 {
		t.Errorf("expected at most 20 results, got %d", numbered)
	}
}

func TestWebSearchToolNoResults(t *testing.T) {
	backend := newMockBackend(nil)
	ws := NewWebSearchTool(backend, 0, newTestLogger())

	params, _ := json.Marshal(webSearchParams{Query: "xyznonexistent"})
	result, _ := ws.Execute(context.Background(), params)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "No search results") {
		t.Errorf("expected no-results message, got: %s", result.Content)
	}
}

func TestWebSearchToolTimeRangeValues(t *testing.T) {
	for _, tr := range []string{"day", "week", "month", "year"} {
		backend := newMockBackend(nil)
		ws := NewWebSearchTool(backend, 0, newTestLogger())

		params, _ := json.Marshal(webSearchParams{Query: "test", TimeRange: tr})
		result, err := ws.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("time_range=%q: %v", tr, err)
		}
		if result.IsError {
			t.Errorf("time_range=%q: unexpected error: %s", tr, result.Content)
		}
	}
}

func TestWebSearchToolMultipleResults(t *testing.T) {
	backend := newMockBackend([]SearchResult{
		{Title: "Result 1", URL: "https://example.com/1", Content: "First result"},
		{Title: "Result 2", URL: "https://example.com/2", Content: "Second result"},
		{Title: "Result 3", URL: "https://example.com/3", Content: "Third result"},
	})
	ws := NewWebSearchTool(backend, 0, newTestLogger())

	params, _ := json.Marshal(webSearchParams{Query: "test", Count: 3})
	result, _ := ws.Execute(context.Background(), params)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "1. Result 1") {
		t.Errorf("missing result 1 in output: %s", result.Content)
	}
	if !strings.Contains(result.Content, "2. Result 2") {
		t.Errorf("missing result 2 in output: %s", result.Content)
	}
	if !strings.Contains(result.Content, "3. Result 3") {
		t.Errorf("missing result 3 in output: %s", result.Content)
	}
}

func TestWebSearchToolCacheExpired(t *testing.T) {
	backend := newMockBackend([]SearchResult{
		{Title: "Result", URL: "https://example.com", Content: "desc"},
	})
	ws := NewWebSearchTool(backend, 10*time.Millisecond, newTestLogger())

	params, _ := json.Marshal(webSearchParams{Query: "expire test"})

	// First call - hits backend
	ws.Execute(context.Background(), params)

	// Wait for cache to expire
	time.Sleep(50 * time.Millisecond)

	// Second call - cache expired, should hit backend again
	ws.Execute(context.Background(), params)

	if backend.callCount != 2 {
		t.Errorf("expected 2 backend calls after cache expiry, got %d", backend.callCount)
	}
}

func TestWebSearchToolCacheLazyEviction(t *testing.T) {
	backend := newMockBackend([]SearchResult{
		{Title: "R", URL: "https://example.com", Content: "d"},
	})
	ws := NewWebSearchTool(backend, 10*time.Millisecond, newTestLogger())

	// Fill cache with >100 entries
	for i := 0; i < 105; i++ {
		params, _ := json.Marshal(webSearchParams{Query: fmt.Sprintf("query-%d", i)})
		ws.Execute(context.Background(), params)
	}

	// Wait for all entries to expire
	time.Sleep(50 * time.Millisecond)

	// Next put should trigger lazy eviction of expired entries
	params, _ := json.Marshal(webSearchParams{Query: "trigger-eviction"})
	ws.Execute(context.Background(), params)

	ws.mu.Lock()
	remaining := len(ws.cache)
	ws.mu.Unlock()

	if remaining != 1 {
		t.Errorf("expected 1 cache entry after eviction, got %d", remaining)
	}
}

func TestWebSearchToolCacheDifferentParams(t *testing.T) {
	backend := newMockBackend([]SearchResult{
		{Title: "Result", URL: "https://example.com", Content: "desc"},
	})
	ws := NewWebSearchTool(backend, 5*time.Minute, newTestLogger())

	params1, _ := json.Marshal(webSearchParams{Query: "query1"})
	params2, _ := json.Marshal(webSearchParams{Query: "query2"})

	ws.Execute(context.Background(), params1)
	ws.Execute(context.Background(), params2)

	if backend.callCount != 2 {
		t.Errorf("expected 2 backend calls for different queries, got %d", backend.callCount)
	}
}
