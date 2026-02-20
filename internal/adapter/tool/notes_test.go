package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"alfred-ai/internal/domain"
)

// --- mock backend ---

type mockNotesBackend struct {
	notes map[string]string

	createErr error
	readErr   error
	updateErr error
	deleteErr error
	listErr   error
	searchErr error
}

func newMockNotesBackend() *mockNotesBackend {
	return &mockNotesBackend{notes: make(map[string]string)}
}

func (m *mockNotesBackend) Create(name, content string) error {
	if m.createErr != nil {
		return m.createErr
	}
	if _, exists := m.notes[name]; exists {
		return errAlreadyExists(name)
	}
	m.notes[name] = content
	return nil
}

func (m *mockNotesBackend) Read(name string) (string, error) {
	if m.readErr != nil {
		return "", m.readErr
	}
	c, ok := m.notes[name]
	if !ok {
		return "", errNotFound(name)
	}
	return c, nil
}

func (m *mockNotesBackend) Update(name, content string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	if _, ok := m.notes[name]; !ok {
		return errNotFound(name)
	}
	m.notes[name] = content
	return nil
}

func (m *mockNotesBackend) Delete(name string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if _, ok := m.notes[name]; !ok {
		return errNotFound(name)
	}
	delete(m.notes, name)
	return nil
}

func (m *mockNotesBackend) List() ([]NoteSummary, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var out []NoteSummary
	for name, content := range m.notes {
		out = append(out, NoteSummary{Name: name, Size: len(content)})
	}
	return out, nil
}

func (m *mockNotesBackend) Search(query string) ([]NoteSearchResult, error) {
	if m.searchErr != nil {
		return nil, m.searchErr
	}
	var results []NoteSearchResult
	lower := strings.ToLower(query)
	for name, content := range m.notes {
		for i, line := range strings.Split(content, "\n") {
			if strings.Contains(strings.ToLower(line), lower) {
				results = append(results, NoteSearchResult{Name: name, Line: i + 1, Snippet: line})
			}
		}
	}
	return results, nil
}

func errNotFound(name string) error    { return &noteErr{"note " + name + " not found"} }
func errAlreadyExists(name string) error { return &noteErr{"note " + name + " already exists"} }

type noteErr struct{ msg string }

func (e *noteErr) Error() string { return e.msg }

// --- helpers ---

func newTestNotesTool(t *testing.T) (*NotesTool, *mockNotesBackend) {
	t.Helper()
	b := newMockNotesBackend()
	return NewNotesTool(b, newTestLogger()), b
}

func execNotesTool(t *testing.T, tool *NotesTool, params any) *resultHelper {
	t.Helper()
	data, _ := json.Marshal(params)
	result, err := tool.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return &resultHelper{t: t, r: result}
}

type resultHelper struct {
	t *testing.T
	r *ToolResultForTest
}

// ToolResultForTest is an alias to work around unexported domain types in tests.
type ToolResultForTest = domain.ToolResult

func (h *resultHelper) expectSuccess() *resultHelper {
	h.t.Helper()
	if h.r.IsError {
		h.t.Fatalf("expected success, got error: %s", h.r.Content)
	}
	return h
}

func (h *resultHelper) expectError() *resultHelper {
	h.t.Helper()
	if !h.r.IsError {
		h.t.Fatalf("expected error, got success: %s", h.r.Content)
	}
	return h
}

func (h *resultHelper) expectContains(substr string) *resultHelper {
	h.t.Helper()
	if !strings.Contains(h.r.Content, substr) {
		h.t.Errorf("expected content to contain %q, got: %s", substr, h.r.Content)
	}
	return h
}

// --- metadata tests ---

func TestNotesToolName(t *testing.T) {
	tool, _ := newTestNotesTool(t)
	if tool.Name() != "notes" {
		t.Errorf("got name %q, want %q", tool.Name(), "notes")
	}
}

func TestNotesToolDescription(t *testing.T) {
	tool, _ := newTestNotesTool(t)
	if tool.Description() == "" {
		t.Error("description should not be empty")
	}
}

func TestNotesToolSchema(t *testing.T) {
	tool, _ := newTestNotesTool(t)
	schema := tool.Schema()
	if schema.Name != "notes" {
		t.Errorf("schema name: got %q, want %q", schema.Name, "notes")
	}
	var params map[string]any
	if err := json.Unmarshal(schema.Parameters, &params); err != nil {
		t.Fatalf("invalid schema JSON: %v", err)
	}
}

// --- action success tests ---

func TestNotesToolCreate(t *testing.T) {
	tool, _ := newTestNotesTool(t)
	execNotesTool(t, tool, map[string]any{
		"action": "create", "name": "my-note", "content": "hello world",
	}).expectSuccess().expectContains("created")
}

func TestNotesToolRead(t *testing.T) {
	tool, backend := newTestNotesTool(t)
	backend.notes["test"] = "note content"
	execNotesTool(t, tool, map[string]any{
		"action": "read", "name": "test",
	}).expectSuccess().expectContains("note content")
}

func TestNotesToolUpdate(t *testing.T) {
	tool, backend := newTestNotesTool(t)
	backend.notes["test"] = "old"
	execNotesTool(t, tool, map[string]any{
		"action": "update", "name": "test", "content": "new content",
	}).expectSuccess().expectContains("updated")
	if backend.notes["test"] != "new content" {
		t.Errorf("content not updated: %q", backend.notes["test"])
	}
}

func TestNotesToolDelete(t *testing.T) {
	tool, backend := newTestNotesTool(t)
	backend.notes["test"] = "data"
	execNotesTool(t, tool, map[string]any{
		"action": "delete", "name": "test",
	}).expectSuccess().expectContains("deleted")
	if _, ok := backend.notes["test"]; ok {
		t.Error("note should be deleted")
	}
}

func TestNotesToolList(t *testing.T) {
	tool, backend := newTestNotesTool(t)
	backend.notes["a"] = "aa"
	backend.notes["b"] = "bb"
	r := execNotesTool(t, tool, map[string]any{"action": "list"}).expectSuccess()
	var notes []NoteSummary
	if err := json.Unmarshal([]byte(r.r.Content), &notes); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(notes) != 2 {
		t.Errorf("got %d notes, want 2", len(notes))
	}
}

func TestNotesToolSearch(t *testing.T) {
	tool, backend := newTestNotesTool(t)
	backend.notes["readme"] = "line one\nfind me here\nline three"
	r := execNotesTool(t, tool, map[string]any{
		"action": "search", "query": "find me",
	}).expectSuccess()
	var results []NoteSearchResult
	if err := json.Unmarshal([]byte(r.r.Content), &results); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Line != 2 {
		t.Errorf("got line %d, want 2", results[0].Line)
	}
}

// --- validation error tests ---

func TestNotesToolCreateMissingName(t *testing.T) {
	tool, _ := newTestNotesTool(t)
	execNotesTool(t, tool, map[string]any{
		"action": "create", "content": "hello",
	}).expectError().expectContains("'name' is required")
}

func TestNotesToolCreateMissingContent(t *testing.T) {
	tool, _ := newTestNotesTool(t)
	execNotesTool(t, tool, map[string]any{
		"action": "create", "name": "test",
	}).expectError().expectContains("'content' is required")
}

func TestNotesToolReadMissingName(t *testing.T) {
	tool, _ := newTestNotesTool(t)
	execNotesTool(t, tool, map[string]any{
		"action": "read",
	}).expectError().expectContains("'name' is required")
}

func TestNotesToolUpdateMissingContent(t *testing.T) {
	tool, backend := newTestNotesTool(t)
	backend.notes["test"] = "old"
	execNotesTool(t, tool, map[string]any{
		"action": "update", "name": "test",
	}).expectError().expectContains("'content' is required")
}

func TestNotesToolDeleteMissingName(t *testing.T) {
	tool, _ := newTestNotesTool(t)
	execNotesTool(t, tool, map[string]any{
		"action": "delete",
	}).expectError().expectContains("'name' is required")
}

func TestNotesToolSearchMissingQuery(t *testing.T) {
	tool, _ := newTestNotesTool(t)
	execNotesTool(t, tool, map[string]any{
		"action": "search",
	}).expectError().expectContains("'query' is required")
}

func TestNotesToolInvalidName(t *testing.T) {
	tool, _ := newTestNotesTool(t)
	for _, name := range []string{"../evil", "a/b", "has space", "-leading"} {
		execNotesTool(t, tool, map[string]any{
			"action": "create", "name": name, "content": "x",
		}).expectError().expectContains("invalid note name")
	}
}

func TestNotesToolNameTooLong(t *testing.T) {
	tool, _ := newTestNotesTool(t)
	longName := strings.Repeat("a", maxNoteNameLen+1)
	execNotesTool(t, tool, map[string]any{
		"action": "create", "name": longName, "content": "x",
	}).expectError().expectContains("name too long")
}

func TestNotesToolUnknownAction(t *testing.T) {
	tool, _ := newTestNotesTool(t)
	execNotesTool(t, tool, map[string]any{"action": "bad"}).expectError().expectContains("unknown action")
}

func TestNotesToolInvalidJSON(t *testing.T) {
	tool, _ := newTestNotesTool(t)
	result, err := tool.Execute(context.Background(), []byte(`{invalid`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

// --- backend error propagation ---

func TestNotesToolReadNotFound(t *testing.T) {
	tool, _ := newTestNotesTool(t)
	execNotesTool(t, tool, map[string]any{
		"action": "read", "name": "missing",
	}).expectError().expectContains("not found")
}

func TestNotesToolUpdateNotFound(t *testing.T) {
	tool, _ := newTestNotesTool(t)
	execNotesTool(t, tool, map[string]any{
		"action": "update", "name": "missing", "content": "x",
	}).expectError().expectContains("not found")
}

func TestNotesToolDeleteNotFound(t *testing.T) {
	tool, _ := newTestNotesTool(t)
	execNotesTool(t, tool, map[string]any{
		"action": "delete", "name": "missing",
	}).expectError().expectContains("not found")
}

func TestNotesToolCreateDuplicate(t *testing.T) {
	tool, backend := newTestNotesTool(t)
	backend.notes["dup"] = "existing"
	execNotesTool(t, tool, map[string]any{
		"action": "create", "name": "dup", "content": "new",
	}).expectError().expectContains("already exists")
}

// --- edge cases ---

func TestNotesToolListEmpty(t *testing.T) {
	tool, _ := newTestNotesTool(t)
	execNotesTool(t, tool, map[string]any{"action": "list"}).expectSuccess().expectContains("No notes found")
}

func TestNotesToolSearchNoResults(t *testing.T) {
	tool, backend := newTestNotesTool(t)
	backend.notes["test"] = "nothing relevant"
	execNotesTool(t, tool, map[string]any{
		"action": "search", "query": "xyzzy",
	}).expectSuccess().expectContains("No matches found")
}

func TestNotesToolSearchCaseInsensitive(t *testing.T) {
	tool, backend := newTestNotesTool(t)
	backend.notes["test"] = "Hello World"
	r := execNotesTool(t, tool, map[string]any{
		"action": "search", "query": "hello",
	}).expectSuccess()
	var results []NoteSearchResult
	json.Unmarshal([]byte(r.r.Content), &results)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

// --- local backend integration tests ---

func TestLocalNotesBackendRoundTrip(t *testing.T) {
	dir := t.TempDir()
	b, err := NewLocalNotesBackend(dir)
	if err != nil {
		t.Fatalf("NewLocalNotesBackend: %v", err)
	}

	if err := b.Create("test", "hello"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	content, err := b.Read("test")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if content != "hello" {
		t.Errorf("got %q, want %q", content, "hello")
	}

	if err := b.Update("test", "updated"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	content, _ = b.Read("test")
	if content != "updated" {
		t.Errorf("after update: got %q, want %q", content, "updated")
	}

	notes, err := b.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(notes) != 1 {
		t.Errorf("List: got %d notes, want 1", len(notes))
	}

	results, err := b.Search("updated")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Search: got %d results, want 1", len(results))
	}

	if err := b.Delete("test"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = b.Read("test")
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestLocalNotesBackendCreateDuplicate(t *testing.T) {
	dir := t.TempDir()
	b, _ := NewLocalNotesBackend(dir)
	b.Create("dup", "first")
	err := b.Create("dup", "second")
	if err == nil {
		t.Error("expected error for duplicate create")
	}
}

func TestLocalNotesBackendReadNotFound(t *testing.T) {
	dir := t.TempDir()
	b, _ := NewLocalNotesBackend(dir)
	_, err := b.Read("missing")
	if err == nil {
		t.Error("expected error for missing note")
	}
}

// --- fuzz ---

func FuzzNotesTool_Execute(f *testing.F) {
	f.Add([]byte(`{"action":"create","name":"a","content":"b"}`))
	f.Add([]byte(`{"action":"list"}`))
	f.Add([]byte(`{"action":"search","query":"x"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`invalid json`))

	tool, _ := newTestNotesTool(&testing.T{})
	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic.
		tool.Execute(context.Background(), data)
	})
}
