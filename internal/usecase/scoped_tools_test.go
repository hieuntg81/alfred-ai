package usecase

import (
	"encoding/json"
	"testing"

	"alfred-ai/internal/domain"
)

func newTestToolExecutor() *mockToolExecutor {
	return &mockToolExecutor{
		tools: map[string]domain.Tool{
			"web_search":   &staticTool{name: "web_search", result: "results"},
			"memory_query": &staticTool{name: "memory_query", result: "memories"},
			"file_read":    &staticTool{name: "file_read", result: "content"},
		},
		schemas: []domain.ToolSchema{
			{Name: "web_search", Description: "Search the web"},
			{Name: "memory_query", Description: "Query memory"},
			{Name: "file_read", Description: "Read a file"},
		},
	}
}

func TestScopedToolGetAllowed(t *testing.T) {
	inner := newTestToolExecutor()
	scoped := NewScopedToolExecutor(inner, []string{"web_search", "memory_query"})

	tool, err := scoped.Get("web_search")
	if err != nil {
		t.Fatalf("Get allowed tool: %v", err)
	}
	result, _ := tool.Execute(nil, json.RawMessage(`{}`))
	if result.Content != "results" {
		t.Errorf("Content = %q, want %q", result.Content, "results")
	}
}

func TestScopedToolGetDenied(t *testing.T) {
	inner := newTestToolExecutor()
	scoped := NewScopedToolExecutor(inner, []string{"web_search"})

	_, err := scoped.Get("file_read")
	if err != domain.ErrToolNotFound {
		t.Errorf("Get denied tool: got %v, want ErrToolNotFound", err)
	}
}

func TestScopedToolSchemasFiltered(t *testing.T) {
	inner := newTestToolExecutor()
	scoped := NewScopedToolExecutor(inner, []string{"web_search", "memory_query"})

	schemas := scoped.Schemas()
	if len(schemas) != 2 {
		t.Fatalf("Schemas count = %d, want 2", len(schemas))
	}
	names := map[string]bool{}
	for _, s := range schemas {
		names[s.Name] = true
	}
	if !names["web_search"] || !names["memory_query"] {
		t.Errorf("unexpected schemas: %v", names)
	}
	if names["file_read"] {
		t.Error("file_read should be filtered out")
	}
}

func TestScopedToolEmptyAllowAll(t *testing.T) {
	inner := newTestToolExecutor()
	scoped := NewScopedToolExecutor(inner, []string{})

	// Empty allowedTools = pass-through, should return inner directly.
	if scoped != inner {
		t.Error("empty allowedTools should return inner directly")
	}

	schemas := scoped.Schemas()
	if len(schemas) != 3 {
		t.Errorf("Schemas count = %d, want 3", len(schemas))
	}
}

func TestScopedToolNilAllowAll(t *testing.T) {
	inner := newTestToolExecutor()
	scoped := NewScopedToolExecutor(inner, nil)

	// nil allowedTools = pass-through, should return inner directly.
	if scoped != inner {
		t.Error("nil allowedTools should return inner directly")
	}

	tool, err := scoped.Get("file_read")
	if err != nil {
		t.Fatalf("Get with nil allowed: %v", err)
	}
	if tool.Name() != "file_read" {
		t.Errorf("Name = %q, want %q", tool.Name(), "file_read")
	}
}
