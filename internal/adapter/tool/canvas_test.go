package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

// mockCanvasBackend implements CanvasBackend for testing.
type mockCanvasBackend struct {
	createResult *CanvasInfo
	createErr    error
	updateResult *CanvasInfo
	updateErr    error
	readResult   *CanvasContent
	readErr      error
	deleteErr    error
	listResult   []CanvasInfo
	listErr      error

	// Capture calls for assertions.
	createdCanvases []struct{ SessionID, Name, Content string }
	deletedCanvases []struct{ SessionID, Name string }
}

func (m *mockCanvasBackend) Name() string { return "mock" }
func (m *mockCanvasBackend) Close() error { return nil }

func (m *mockCanvasBackend) Create(_ context.Context, sessionID, name, content string) (*CanvasInfo, error) {
	m.createdCanvases = append(m.createdCanvases, struct{ SessionID, Name, Content string }{sessionID, name, content})
	return m.createResult, m.createErr
}

func (m *mockCanvasBackend) Update(_ context.Context, sessionID, name, content string) (*CanvasInfo, error) {
	return m.updateResult, m.updateErr
}

func (m *mockCanvasBackend) Read(_ context.Context, sessionID, name string) (*CanvasContent, error) {
	return m.readResult, m.readErr
}

func (m *mockCanvasBackend) Delete(_ context.Context, sessionID, name string) error {
	m.deletedCanvases = append(m.deletedCanvases, struct{ SessionID, Name string }{sessionID, name})
	return m.deleteErr
}

func (m *mockCanvasBackend) List(_ context.Context, sessionID string) ([]CanvasInfo, error) {
	return m.listResult, m.listErr
}

func newTestCanvasTool(backend *mockCanvasBackend) *CanvasTool {
	return NewCanvasTool(backend, nil, 0, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
}

// ctxWithSession returns a context carrying the given session ID.
func ctxWithSession(sessionID string) context.Context {
	return domain.ContextWithSessionID(context.Background(), sessionID)
}

func execCanvas(t *testing.T, ct *CanvasTool, params interface{}) (string, bool) {
	t.Helper()
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	result, err := ct.Execute(ctxWithSession("test-session"), data)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	return result.Content, result.IsError
}

// --- Basic tests ---

func TestCanvasToolName(t *testing.T) {
	ct := newTestCanvasTool(&mockCanvasBackend{})
	if ct.Name() != "canvas" {
		t.Errorf("Name() = %q, want %q", ct.Name(), "canvas")
	}
}

func TestCanvasToolDescription(t *testing.T) {
	ct := newTestCanvasTool(&mockCanvasBackend{})
	if ct.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestCanvasToolSchema(t *testing.T) {
	ct := newTestCanvasTool(&mockCanvasBackend{})
	schema := ct.Schema()
	if schema.Name != "canvas" {
		t.Errorf("Schema().Name = %q, want %q", schema.Name, "canvas")
	}
	if len(schema.Parameters) == 0 {
		t.Error("Schema().Parameters should not be empty")
	}
	// Verify JSON is valid
	var parsed map[string]interface{}
	if err := json.Unmarshal(schema.Parameters, &parsed); err != nil {
		t.Errorf("Schema().Parameters is not valid JSON: %v", err)
	}
}

func TestCanvasToolInvalidJSON(t *testing.T) {
	ct := newTestCanvasTool(&mockCanvasBackend{})
	result, err := ct.Execute(ctxWithSession("test-session"), json.RawMessage(`{invalid`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestCanvasToolUnknownAction(t *testing.T) {
	ct := newTestCanvasTool(&mockCanvasBackend{})
	content, isErr := execCanvas(t, ct, canvasParams{Action: "fly"})
	if !isErr {
		t.Error("expected error for unknown action")
	}
	if !strings.Contains(content, "unknown action") {
		t.Errorf("error should mention unknown action, got: %s", content)
	}
}

// --- No session context test ---

func TestCanvasNoSessionContext(t *testing.T) {
	ct := newTestCanvasTool(&mockCanvasBackend{})
	// Execute with a bare context (no session ID injected)
	data, _ := json.Marshal(canvasParams{Action: "create", Name: "test", Content: "<html></html>"})
	result, err := ct.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when session context is missing")
	}
	if !strings.Contains(result.Content, "no session context") {
		t.Errorf("error should mention missing session context, got: %s", result.Content)
	}
}

// --- Create tests ---

func TestCanvasCreateSuccess(t *testing.T) {
	mb := &mockCanvasBackend{
		createResult: &CanvasInfo{Name: "dashboard", Size: 42},
	}
	ct := newTestCanvasTool(mb)
	content, isErr := execCanvas(t, ct, canvasParams{
		Action:  "create",
		Name:    "dashboard",
		Content: "<html><body>Hello</body></html>",
	})
	if isErr {
		t.Errorf("unexpected error: %s", content)
	}
	if !strings.Contains(content, "dashboard") {
		t.Errorf("response should mention canvas name, got: %s", content)
	}
	if len(mb.createdCanvases) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(mb.createdCanvases))
	}
	if mb.createdCanvases[0].Name != "dashboard" {
		t.Errorf("created canvas name = %q, want %q", mb.createdCanvases[0].Name, "dashboard")
	}
}

func TestCanvasCreateEmptyName(t *testing.T) {
	ct := newTestCanvasTool(&mockCanvasBackend{})
	_, isErr := execCanvas(t, ct, canvasParams{
		Action:  "create",
		Name:    "",
		Content: "<html></html>",
	})
	if !isErr {
		t.Error("expected error for empty name")
	}
}

func TestCanvasCreateEmptyContent(t *testing.T) {
	ct := newTestCanvasTool(&mockCanvasBackend{})
	_, isErr := execCanvas(t, ct, canvasParams{
		Action:  "create",
		Name:    "test",
		Content: "",
	})
	if !isErr {
		t.Error("expected error for empty content")
	}
}

func TestCanvasCreateInvalidName(t *testing.T) {
	ct := newTestCanvasTool(&mockCanvasBackend{})
	tests := []struct {
		name string
		desc string
	}{
		{"../escape", "path traversal"},
		{"has spaces", "spaces"},
		{".hidden", "starts with dot"},
		{"foo/bar", "contains slash"},
		{"-start", "starts with hyphen"},
		{"_start", "starts with underscore"},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			_, isErr := execCanvas(t, ct, canvasParams{
				Action:  "create",
				Name:    tt.name,
				Content: "<html></html>",
			})
			if !isErr {
				t.Errorf("expected error for name %q (%s)", tt.name, tt.desc)
			}
		})
	}
}

func TestCanvasCreateValidNames(t *testing.T) {
	mb := &mockCanvasBackend{
		createResult: &CanvasInfo{Name: "test", Size: 13},
	}
	ct := newTestCanvasTool(mb)
	validNames := []string{"a", "dashboard", "my-chart", "data_viz", "Chart2024", "a1b2c3"}
	for _, name := range validNames {
		t.Run(name, func(t *testing.T) {
			_, isErr := execCanvas(t, ct, canvasParams{
				Action:  "create",
				Name:    name,
				Content: "<html></html>",
			})
			if isErr {
				t.Errorf("name %q should be valid", name)
			}
		})
	}
}

func TestCanvasCreateContentTooLarge(t *testing.T) {
	ct := NewCanvasTool(&mockCanvasBackend{}, nil, 100,
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	_, isErr := execCanvas(t, ct, canvasParams{
		Action:  "create",
		Name:    "big",
		Content: strings.Repeat("x", 101),
	})
	if !isErr {
		t.Error("expected error for content too large")
	}
}

func TestCanvasCreateBlockedContent(t *testing.T) {
	mb := &mockCanvasBackend{
		createResult: &CanvasInfo{Name: "test", Size: 100},
	}
	ct := newTestCanvasTool(mb)
	for _, pattern := range blockedHTMLPatterns {
		t.Run(pattern, func(t *testing.T) {
			content := fmt.Sprintf("<html><body>%s</body></html>", pattern)
			_, isErr := execCanvas(t, ct, canvasParams{
				Action:  "create",
				Name:    "test",
				Content: content,
			})
			if !isErr {
				t.Errorf("expected blocked content for pattern %q", pattern)
			}
		})
	}
}

func TestCanvasCreateLimitReached(t *testing.T) {
	existing := make([]CanvasInfo, maxCanvasesPerSession)
	for i := range existing {
		existing[i] = CanvasInfo{Name: fmt.Sprintf("canvas-%d", i)}
	}
	mb := &mockCanvasBackend{listResult: existing}
	ct := newTestCanvasTool(mb)
	_, isErr := execCanvas(t, ct, canvasParams{
		Action:  "create",
		Name:    "overflow",
		Content: "<html></html>",
	})
	if !isErr {
		t.Error("expected error for canvas limit reached")
	}
}

func TestCanvasCreateBackendError(t *testing.T) {
	mb := &mockCanvasBackend{createErr: fmt.Errorf("disk full")}
	ct := newTestCanvasTool(mb)
	content, isErr := execCanvas(t, ct, canvasParams{
		Action:  "create",
		Name:    "test",
		Content: "<html></html>",
	})
	if !isErr {
		t.Error("expected error from backend")
	}
	if !strings.Contains(content, "disk full") {
		t.Errorf("error should contain backend error, got: %s", content)
	}
}

// --- Update tests ---

func TestCanvasUpdateSuccess(t *testing.T) {
	mb := &mockCanvasBackend{
		updateResult: &CanvasInfo{Name: "dashboard", Size: 50},
	}
	ct := newTestCanvasTool(mb)
	content, isErr := execCanvas(t, ct, canvasParams{
		Action:  "update",
		Name:    "dashboard",
		Content: "<html><body>Updated</body></html>",
	})
	if isErr {
		t.Errorf("unexpected error: %s", content)
	}
	if !strings.Contains(content, "updated") {
		t.Errorf("response should mention update, got: %s", content)
	}
}

func TestCanvasUpdateEmptyContent(t *testing.T) {
	ct := newTestCanvasTool(&mockCanvasBackend{})
	_, isErr := execCanvas(t, ct, canvasParams{
		Action: "update",
		Name:   "test",
	})
	if !isErr {
		t.Error("expected error for empty content")
	}
}

func TestCanvasUpdateBackendError(t *testing.T) {
	mb := &mockCanvasBackend{updateErr: fmt.Errorf("not found")}
	ct := newTestCanvasTool(mb)
	_, isErr := execCanvas(t, ct, canvasParams{
		Action:  "update",
		Name:    "missing",
		Content: "<html></html>",
	})
	if !isErr {
		t.Error("expected error from backend")
	}
}

func TestCanvasUpdateBlockedContent(t *testing.T) {
	ct := newTestCanvasTool(&mockCanvasBackend{})
	_, isErr := execCanvas(t, ct, canvasParams{
		Action:  "update",
		Name:    "test",
		Content: "<html><script>eval('bad')</script></html>",
	})
	if !isErr {
		t.Error("expected error for blocked content in update")
	}
}

func TestCanvasUpdateContentTooLarge(t *testing.T) {
	ct := NewCanvasTool(&mockCanvasBackend{}, nil, 100,
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	_, isErr := execCanvas(t, ct, canvasParams{
		Action:  "update",
		Name:    "test",
		Content: strings.Repeat("x", 101),
	})
	if !isErr {
		t.Error("expected error for content too large in update")
	}
}

// --- Read tests ---

func TestCanvasReadSuccess(t *testing.T) {
	mb := &mockCanvasBackend{
		readResult: &CanvasContent{
			CanvasInfo: CanvasInfo{Name: "test", Size: 20},
			Content:    "<html>read me</html>",
		},
	}
	ct := newTestCanvasTool(mb)
	content, isErr := execCanvas(t, ct, canvasParams{Action: "read", Name: "test"})
	if isErr {
		t.Errorf("unexpected error: %s", content)
	}
	if content != "<html>read me</html>" {
		t.Errorf("unexpected content: %s", content)
	}
}

func TestCanvasReadNotFound(t *testing.T) {
	mb := &mockCanvasBackend{readErr: fmt.Errorf("canvas \"missing\" not found")}
	ct := newTestCanvasTool(mb)
	_, isErr := execCanvas(t, ct, canvasParams{Action: "read", Name: "missing"})
	if !isErr {
		t.Error("expected error for not found")
	}
}

// --- Delete tests ---

func TestCanvasDeleteSuccess(t *testing.T) {
	mb := &mockCanvasBackend{}
	ct := newTestCanvasTool(mb)
	content, isErr := execCanvas(t, ct, canvasParams{Action: "delete", Name: "old"})
	if isErr {
		t.Errorf("unexpected error: %s", content)
	}
	if len(mb.deletedCanvases) != 1 {
		t.Fatalf("expected 1 delete call, got %d", len(mb.deletedCanvases))
	}
}

func TestCanvasDeleteBackendError(t *testing.T) {
	mb := &mockCanvasBackend{deleteErr: fmt.Errorf("permission denied")}
	ct := newTestCanvasTool(mb)
	_, isErr := execCanvas(t, ct, canvasParams{Action: "delete", Name: "locked"})
	if !isErr {
		t.Error("expected error from backend")
	}
}

// --- List tests ---

func TestCanvasListSuccess(t *testing.T) {
	mb := &mockCanvasBackend{
		listResult: []CanvasInfo{
			{Name: "a", Size: 10},
			{Name: "b", Size: 20},
		},
	}
	ct := newTestCanvasTool(mb)
	content, isErr := execCanvas(t, ct, canvasParams{Action: "list"})
	if isErr {
		t.Errorf("unexpected error: %s", content)
	}
	if !strings.Contains(content, "\"a\"") || !strings.Contains(content, "\"b\"") {
		t.Errorf("list should contain canvas names, got: %s", content)
	}
}

func TestCanvasListEmpty(t *testing.T) {
	ct := newTestCanvasTool(&mockCanvasBackend{})
	content, isErr := execCanvas(t, ct, canvasParams{Action: "list"})
	if isErr {
		t.Errorf("unexpected error: %s", content)
	}
	if !strings.Contains(content, "No canvases") {
		t.Errorf("expected empty list message, got: %s", content)
	}
}

func TestCanvasListBackendError(t *testing.T) {
	mb := &mockCanvasBackend{listErr: fmt.Errorf("io error")}
	ct := newTestCanvasTool(mb)
	content, isErr := execCanvas(t, ct, canvasParams{Action: "list"})
	if !isErr {
		t.Error("expected error from backend")
	}
	if !strings.Contains(content, "io error") {
		t.Errorf("error should contain backend error, got: %s", content)
	}
}

// --- Present tests ---

func TestCanvasPresentSuccess(t *testing.T) {
	mb := &mockCanvasBackend{
		readResult: &CanvasContent{
			CanvasInfo: CanvasInfo{Name: "chart", Size: 30},
			Content:    "<html>chart</html>",
		},
	}
	ct := newTestCanvasTool(mb)
	content, isErr := execCanvas(t, ct, canvasParams{Action: "present", Name: "chart"})
	if isErr {
		t.Errorf("unexpected error: %s", content)
	}
	if !strings.Contains(content, "displayed") {
		t.Errorf("response should mention displayed, got: %s", content)
	}
}

func TestCanvasPresentNotFound(t *testing.T) {
	mb := &mockCanvasBackend{readErr: fmt.Errorf("canvas \"gone\" not found")}
	ct := newTestCanvasTool(mb)
	_, isErr := execCanvas(t, ct, canvasParams{Action: "present", Name: "gone"})
	if !isErr {
		t.Error("expected error for not found")
	}
}

// --- Hide tests ---

func TestCanvasHideSuccess(t *testing.T) {
	ct := newTestCanvasTool(&mockCanvasBackend{})
	content, isErr := execCanvas(t, ct, canvasParams{Action: "hide"})
	if isErr {
		t.Errorf("unexpected error: %s", content)
	}
	if !strings.Contains(content, "hidden") {
		t.Errorf("response should mention hidden, got: %s", content)
	}
}

// --- Snapshot tests ---

func TestCanvasSnapshotSuccess(t *testing.T) {
	now := time.Now()
	mb := &mockCanvasBackend{
		readResult: &CanvasContent{
			CanvasInfo: CanvasInfo{Name: "snap", Size: 25, UpdatedAt: now},
			Content:    "<html>snapshot</html>",
		},
	}
	ct := newTestCanvasTool(mb)
	content, isErr := execCanvas(t, ct, canvasParams{Action: "snapshot", Name: "snap"})
	if isErr {
		t.Errorf("unexpected error: %s", content)
	}
	if !strings.Contains(content, "<html>snapshot</html>") {
		t.Errorf("snapshot should contain full content, got: %s", content)
	}
}

func TestCanvasSnapshotNotFound(t *testing.T) {
	mb := &mockCanvasBackend{readErr: fmt.Errorf("not found")}
	ct := newTestCanvasTool(mb)
	_, isErr := execCanvas(t, ct, canvasParams{Action: "snapshot", Name: "gone"})
	if !isErr {
		t.Error("expected error for not found")
	}
}

// --- EvalJS tests ---

func TestCanvasEvalJSSuccess(t *testing.T) {
	ct := newTestCanvasTool(&mockCanvasBackend{})
	content, isErr := execCanvas(t, ct, canvasParams{
		Action:     "eval_js",
		Name:       "test",
		Expression: "document.title",
	})
	if isErr {
		t.Errorf("unexpected error: %s", content)
	}
	if !strings.Contains(content, "evaluation requested") {
		t.Errorf("response should mention evaluation, got: %s", content)
	}
}

func TestCanvasEvalJSEmptyExpression(t *testing.T) {
	ct := newTestCanvasTool(&mockCanvasBackend{})
	_, isErr := execCanvas(t, ct, canvasParams{
		Action:     "eval_js",
		Expression: "",
	})
	if !isErr {
		t.Error("expected error for empty expression")
	}
}

func TestCanvasEvalJSTooLong(t *testing.T) {
	ct := newTestCanvasTool(&mockCanvasBackend{})
	_, isErr := execCanvas(t, ct, canvasParams{
		Action:     "eval_js",
		Expression: strings.Repeat("x", maxJSExpressionLen+1),
	})
	if !isErr {
		t.Error("expected error for expression too long")
	}
}

func TestCanvasEvalJSBlockedPatterns(t *testing.T) {
	ct := newTestCanvasTool(&mockCanvasBackend{})
	for _, pattern := range blockedJSPatterns {
		t.Run(pattern, func(t *testing.T) {
			_, isErr := execCanvas(t, ct, canvasParams{
				Action:     "eval_js",
				Expression: fmt.Sprintf("var x = %s;", pattern),
			})
			if !isErr {
				t.Errorf("expected blocked expression for pattern %q", pattern)
			}
		})
	}
}

func TestCanvasEvalJSWordPatternNoFalsePositive(t *testing.T) {
	ct := newTestCanvasTool(&mockCanvasBackend{})
	// "prefs.theme" should NOT trigger "fs." word pattern
	_, isErr := execCanvas(t, ct, canvasParams{
		Action:     "eval_js",
		Expression: "prefs.theme",
	})
	if isErr {
		t.Error("prefs.theme should not trigger fs. word pattern")
	}
}

// --- Name validation table-driven ---

func TestCanvasNameValidation(t *testing.T) {
	ct := newTestCanvasTool(&mockCanvasBackend{})
	tests := []struct {
		name  string
		valid bool
	}{
		{"dashboard", true},
		{"my-chart", true},
		{"data_viz", true},
		{"a1", true},
		{"Z", true},
		{"", false},
		{" ", false},
		{"../etc/passwd", false},
		{"has space", false},
		{".hidden", false},
		{"-start", false},
		{"_under", false},
		{"foo/bar", false},
		{strings.Repeat("a", maxCanvasNameLen+1), false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.name), func(t *testing.T) {
			err := ct.validateName(tt.name)
			if tt.valid && err != nil {
				t.Errorf("name %q should be valid, got error: %v", tt.name, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("name %q should be invalid", tt.name)
			}
		})
	}
}

// --- Path leak test ---

func TestCanvasListDoesNotLeakPath(t *testing.T) {
	mb := &mockCanvasBackend{
		listResult: []CanvasInfo{
			{Name: "a", Size: 10, Path: "/secret/internal/path/index.html"},
		},
	}
	ct := newTestCanvasTool(mb)
	content, isErr := execCanvas(t, ct, canvasParams{Action: "list"})
	if isErr {
		t.Errorf("unexpected error: %s", content)
	}
	if strings.Contains(content, "/secret/internal") {
		t.Error("list output should not leak filesystem paths")
	}
}

// --- Content validation ---

func TestCanvasAllowedContent(t *testing.T) {
	validContents := []string{
		"<html><body><h1>Hello</h1></body></html>",
		"<style>body { color: red; }</style>",
		"<script>console.log('hi');</script>",
		"<div>Some text with refs.current and prefs.foo</div>",
	}
	for _, c := range validContents {
		t.Run(c[:20], func(t *testing.T) {
			if err := validateCanvasContent(c); err != nil {
				t.Errorf("content should be valid: %v", err)
			}
		})
	}
}

// --- Local backend integration tests ---

func TestLocalCanvasBackendRoundTrip(t *testing.T) {
	root := t.TempDir()
	backend, err := NewLocalCanvasBackend(root)
	if err != nil {
		t.Fatalf("NewLocalCanvasBackend: %v", err)
	}
	defer backend.Close()

	ctx := context.Background()
	sid := "01JMKX7R5PQNV8RBWT6BCCX2F" // ULID format
	name := "test-canvas"
	html := "<html><body>Hello World</body></html>"

	// Create
	info, err := backend.Create(ctx, sid, name, html)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if info.Name != name {
		t.Errorf("Create: name = %q, want %q", info.Name, name)
	}
	if info.Size != len(html) {
		t.Errorf("Create: size = %d, want %d", info.Size, len(html))
	}

	// Create duplicate should fail
	_, err = backend.Create(ctx, sid, name, html)
	if err == nil {
		t.Error("Create duplicate should fail")
	}

	// Read
	content, err := backend.Read(ctx, sid, name)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if content.Content != html {
		t.Errorf("Read: content mismatch")
	}

	// Update
	newHTML := "<html><body>Updated</body></html>"
	info, err = backend.Update(ctx, sid, name, newHTML)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if info.Size != len(newHTML) {
		t.Errorf("Update: size = %d, want %d", info.Size, len(newHTML))
	}

	// Read updated
	content, err = backend.Read(ctx, sid, name)
	if err != nil {
		t.Fatalf("Read after update: %v", err)
	}
	if content.Content != newHTML {
		t.Errorf("Read after update: content mismatch")
	}

	// List
	list, err := backend.List(ctx, sid)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List: expected 1 canvas, got %d", len(list))
	}

	// Delete
	if err := backend.Delete(ctx, sid, name); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// List after delete
	list, err = backend.List(ctx, sid)
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("List after delete: expected 0, got %d", len(list))
	}
}

func TestLocalCanvasBackendMultipleSessions(t *testing.T) {
	root := t.TempDir()
	backend, err := NewLocalCanvasBackend(root)
	if err != nil {
		t.Fatalf("NewLocalCanvasBackend: %v", err)
	}
	defer backend.Close()

	ctx := context.Background()

	sid1 := "01JMKX7R5PQNV8RBWT6BCCX21" // ULID format
	sid2 := "01JMKX7R5PQNV8RBWT6BCCX22"

	// Create in two sessions
	_, err = backend.Create(ctx, sid1, "canvas-a", "<html>s1</html>")
	if err != nil {
		t.Fatalf("Create s1: %v", err)
	}
	_, err = backend.Create(ctx, sid2, "canvas-a", "<html>s2</html>")
	if err != nil {
		t.Fatalf("Create s2: %v", err)
	}

	// Verify isolation
	c1, _ := backend.Read(ctx, sid1, "canvas-a")
	c2, _ := backend.Read(ctx, sid2, "canvas-a")
	if c1.Content == c2.Content {
		t.Error("sessions should be isolated")
	}

	l1, _ := backend.List(ctx, sid1)
	l2, _ := backend.List(ctx, sid2)
	if len(l1) != 1 || len(l2) != 1 {
		t.Errorf("expected 1 canvas per session, got s1=%d s2=%d", len(l1), len(l2))
	}
}

func TestLocalCanvasBackendEmptySession(t *testing.T) {
	root := t.TempDir()
	backend, err := NewLocalCanvasBackend(root)
	if err != nil {
		t.Fatalf("NewLocalCanvasBackend: %v", err)
	}
	defer backend.Close()

	list, err := backend.List(context.Background(), "01JMKX000000000000NONEXIST")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list != nil {
		t.Errorf("expected nil list for nonexistent session, got %d items", len(list))
	}
}

func TestLocalCanvasBackendReadNotFound(t *testing.T) {
	root := t.TempDir()
	backend, err := NewLocalCanvasBackend(root)
	if err != nil {
		t.Fatalf("NewLocalCanvasBackend: %v", err)
	}
	defer backend.Close()

	_, err = backend.Read(context.Background(), "01JMKX7R5PQNV8RBWT6BCCX2F", "missing")
	if err == nil {
		t.Error("expected error for reading non-existent canvas")
	}
}

func TestLocalCanvasBackendDeleteNotFound(t *testing.T) {
	root := t.TempDir()
	backend, err := NewLocalCanvasBackend(root)
	if err != nil {
		t.Fatalf("NewLocalCanvasBackend: %v", err)
	}
	defer backend.Close()

	err = backend.Delete(context.Background(), "01JMKX7R5PQNV8RBWT6BCCX2F", "missing")
	if err == nil {
		t.Error("expected error for deleting non-existent canvas")
	}
}

func TestLocalCanvasBackendUpdateNotFound(t *testing.T) {
	root := t.TempDir()
	backend, err := NewLocalCanvasBackend(root)
	if err != nil {
		t.Fatalf("NewLocalCanvasBackend: %v", err)
	}
	defer backend.Close()

	_, err = backend.Update(context.Background(), "01JMKX7R5PQNV8RBWT6BCCX2F", "missing", "<html></html>")
	if err == nil {
		t.Error("expected error for updating non-existent canvas")
	}
}
