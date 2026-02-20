package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"alfred-ai/internal/domain"
)

// mockBrowserBackend implements BrowserBackend for testing.
type mockBrowserBackend struct {
	navigateErr    error
	contentResult  *PageContent
	contentErr     error
	screenshotData string
	screenshotErr  error
	clickErr       error
	typeErr        error
	evaluateResult string
	evaluateErr    error
	waitErr        error
	tabs           []TabInfo
	tabListErr     error
	tabOpenID      string
	tabOpenErr     error
	tabCloseErr    error
	tabFocusErr    error
	statusResult   *BrowserStatus
	statusErr      error

	// Capture calls for assertions.
	navigatedURLs  []string
	clickedSels    []string
	typedInputs    []struct{ Sel, Text string }
	evaluatedExprs []string
}

func (m *mockBrowserBackend) Name() string { return "mock" }
func (m *mockBrowserBackend) Navigate(ctx context.Context, url string) error {
	m.navigatedURLs = append(m.navigatedURLs, url)
	return m.navigateErr
}
func (m *mockBrowserBackend) GetContent(ctx context.Context, selector string) (*PageContent, error) {
	return m.contentResult, m.contentErr
}
func (m *mockBrowserBackend) Screenshot(ctx context.Context, fullPage bool) (string, error) {
	return m.screenshotData, m.screenshotErr
}
func (m *mockBrowserBackend) Click(ctx context.Context, selector string) error {
	m.clickedSels = append(m.clickedSels, selector)
	return m.clickErr
}
func (m *mockBrowserBackend) Type(ctx context.Context, selector string, text string) error {
	m.typedInputs = append(m.typedInputs, struct{ Sel, Text string }{selector, text})
	return m.typeErr
}
func (m *mockBrowserBackend) Evaluate(ctx context.Context, expression string) (string, error) {
	m.evaluatedExprs = append(m.evaluatedExprs, expression)
	return m.evaluateResult, m.evaluateErr
}
func (m *mockBrowserBackend) WaitVisible(ctx context.Context, selector string) error {
	return m.waitErr
}
func (m *mockBrowserBackend) TabList(ctx context.Context) ([]TabInfo, error) {
	return m.tabs, m.tabListErr
}
func (m *mockBrowserBackend) TabOpen(ctx context.Context, url string) (string, error) {
	return m.tabOpenID, m.tabOpenErr
}
func (m *mockBrowserBackend) TabClose(ctx context.Context, targetID string) error {
	return m.tabCloseErr
}
func (m *mockBrowserBackend) TabFocus(ctx context.Context, targetID string) error {
	return m.tabFocusErr
}
func (m *mockBrowserBackend) Status(ctx context.Context) (*BrowserStatus, error) {
	return m.statusResult, m.statusErr
}
func (m *mockBrowserBackend) Close() error { return nil }

func newTestBrowserTool() (*BrowserTool, *mockBrowserBackend) {
	backend := &mockBrowserBackend{
		contentResult: &PageContent{
			Title: "Test Page",
			URL:   "https://example.com",
			Text:  "Hello World",
			Links: []PageLink{{Index: 0, Text: "Link", Href: "https://example.com/link", Selector: "a"}},
		},
		screenshotData: "iVBORw0KGgoAAAANSUhEUgAAAAUA",
		evaluateResult: "42",
		tabs: []TabInfo{
			{TargetID: "t1", Title: "Tab 1", URL: "https://example.com", Active: true},
		},
		tabOpenID: "t2",
		statusResult: &BrowserStatus{
			Connected:    true,
			Backend:      "mock",
			TabCount:     1,
			ActiveTabURL: "https://example.com",
		},
	}
	bt := NewBrowserTool(backend, slog.Default())
	return bt, backend
}

func execBrowser(t *testing.T, bt *BrowserTool, params interface{}) *domain.ToolResult {
	t.Helper()
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	result, err := bt.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	return result
}

// --- Basic tests ---

func TestBrowserToolName(t *testing.T) {
	bt, _ := newTestBrowserTool()
	if bt.Name() != "browser" {
		t.Errorf("Name() = %q, want %q", bt.Name(), "browser")
	}
}

func TestBrowserToolDescription(t *testing.T) {
	bt, _ := newTestBrowserTool()
	if bt.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestBrowserToolSchema(t *testing.T) {
	bt, _ := newTestBrowserTool()
	schema := bt.Schema()
	if schema.Name != "browser" {
		t.Errorf("Schema().Name = %q, want %q", schema.Name, "browser")
	}
	// Verify parameters is valid JSON
	var params map[string]interface{}
	if err := json.Unmarshal(schema.Parameters, &params); err != nil {
		t.Fatalf("Schema().Parameters is not valid JSON: %v", err)
	}
	if params["type"] != "object" {
		t.Error("Schema parameters should be type object")
	}
}

func TestBrowserToolInvalidJSON(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result, err := bt.Execute(context.Background(), json.RawMessage(`{invalid`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for invalid JSON")
	}
	if !strings.Contains(result.Content, "invalid params") {
		t.Errorf("error should mention invalid params, got: %s", result.Content)
	}
}

func TestBrowserToolUnknownAction(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "fly"})
	if !result.IsError {
		t.Error("expected IsError for unknown action")
	}
	if !strings.Contains(result.Content, "unknown action") {
		t.Errorf("error should mention unknown action, got: %s", result.Content)
	}
}

// --- Navigate tests ---

func TestBrowserNavigateSuccess(t *testing.T) {
	bt, backend := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "navigate", URL: "https://example.com"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Navigated to") {
		t.Errorf("expected navigation confirmation, got: %s", result.Content)
	}
	if len(backend.navigatedURLs) != 1 || backend.navigatedURLs[0] != "https://example.com" {
		t.Errorf("backend not called with correct URL: %v", backend.navigatedURLs)
	}
}

func TestBrowserNavigateEmptyURL(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "navigate", URL: ""})
	if !result.IsError {
		t.Error("expected error for empty URL")
	}
	if !strings.Contains(result.Content, "'url' is required") {
		t.Errorf("error should mention url required, got: %s", result.Content)
	}
}

func TestBrowserNavigateSSRFBlocked(t *testing.T) {
	bt, _ := newTestBrowserTool()
	privateIPs := []string{
		"http://127.0.0.1/",
		"http://192.168.1.1/",
		"http://10.0.0.1/",
		"http://172.16.0.1/",
		"http://169.254.169.254/",
		"http://[::1]/",
	}
	for _, ip := range privateIPs {
		result := execBrowser(t, bt, browserParams{Action: "navigate", URL: ip})
		if !result.IsError {
			t.Errorf("expected SSRF block for %s", ip)
		}
	}
}

func TestBrowserNavigateBackendError(t *testing.T) {
	bt, backend := newTestBrowserTool()
	backend.navigateErr = fmt.Errorf("connection refused")
	result := execBrowser(t, bt, browserParams{Action: "navigate", URL: "https://example.com"})
	if !result.IsError {
		t.Error("expected error from backend")
	}
	if !strings.Contains(result.Content, "navigate failed") {
		t.Errorf("error should mention navigate failed, got: %s", result.Content)
	}
}

// --- GetContent tests ---

func TestBrowserGetContentSuccess(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "get_content"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Test Page") {
		t.Errorf("expected page title in content, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Hello World") {
		t.Errorf("expected page text in content, got: %s", result.Content)
	}
}

func TestBrowserGetContentWithSelector(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "get_content", Selector: "#main"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestBrowserGetContentBackendError(t *testing.T) {
	bt, backend := newTestBrowserTool()
	backend.contentErr = fmt.Errorf("page not loaded")
	result := execBrowser(t, bt, browserParams{Action: "get_content"})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

// --- Screenshot tests ---

func TestBrowserScreenshotSuccess(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "screenshot"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Screenshot captured") {
		t.Errorf("expected screenshot confirmation, got: %s", result.Content)
	}
}

func TestBrowserScreenshotFullPage(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "screenshot", FullPage: true})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestBrowserScreenshotTooLarge(t *testing.T) {
	bt, backend := newTestBrowserTool()
	backend.screenshotData = strings.Repeat("A", maxScreenshotBase64+1)
	result := execBrowser(t, bt, browserParams{Action: "screenshot"})
	if !result.IsError {
		t.Error("expected error for oversized screenshot")
	}
	if !strings.Contains(result.Content, "screenshot too large") {
		t.Errorf("error should mention too large, got: %s", result.Content)
	}
}

func TestBrowserScreenshotBackendError(t *testing.T) {
	bt, backend := newTestBrowserTool()
	backend.screenshotErr = fmt.Errorf("capture failed")
	result := execBrowser(t, bt, browserParams{Action: "screenshot"})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

// --- Click tests ---

func TestBrowserClickSuccess(t *testing.T) {
	bt, backend := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "click", Selector: "#btn"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Clicked element") {
		t.Errorf("expected click confirmation, got: %s", result.Content)
	}
	if len(backend.clickedSels) != 1 || backend.clickedSels[0] != "#btn" {
		t.Errorf("backend not called with correct selector: %v", backend.clickedSels)
	}
}

func TestBrowserClickEmptySelector(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "click", Selector: ""})
	if !result.IsError {
		t.Error("expected error for empty selector")
	}
	if !strings.Contains(result.Content, "'selector' is required") {
		t.Errorf("error should mention selector required, got: %s", result.Content)
	}
}

func TestBrowserClickBackendError(t *testing.T) {
	bt, backend := newTestBrowserTool()
	backend.clickErr = fmt.Errorf("element not found")
	result := execBrowser(t, bt, browserParams{Action: "click", Selector: "#missing"})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

// --- Type tests ---

func TestBrowserTypeSuccess(t *testing.T) {
	bt, backend := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "type", Selector: "#input", Text: "hello"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Typed 5 chars") {
		t.Errorf("expected type confirmation, got: %s", result.Content)
	}
	if len(backend.typedInputs) != 1 {
		t.Fatalf("expected 1 type call, got %d", len(backend.typedInputs))
	}
	if backend.typedInputs[0].Sel != "#input" || backend.typedInputs[0].Text != "hello" {
		t.Errorf("backend called with wrong args: %+v", backend.typedInputs[0])
	}
}

func TestBrowserTypeEmptySelector(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "type", Selector: "", Text: "hello"})
	if !result.IsError {
		t.Error("expected error for empty selector")
	}
}

func TestBrowserTypeEmptyText(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "type", Selector: "#input", Text: ""})
	if !result.IsError {
		t.Error("expected error for empty text")
	}
}

// --- Evaluate tests ---

func TestBrowserEvaluateSuccess(t *testing.T) {
	bt, backend := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "evaluate", Expression: "1 + 1"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != "42" {
		t.Errorf("expected '42', got: %s", result.Content)
	}
	if len(backend.evaluatedExprs) != 1 || backend.evaluatedExprs[0] != "1 + 1" {
		t.Errorf("backend not called with correct expression: %v", backend.evaluatedExprs)
	}
}

func TestBrowserEvaluateEmptyExpression(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "evaluate", Expression: ""})
	if !result.IsError {
		t.Error("expected error for empty expression")
	}
}

func TestBrowserEvaluateBlocked(t *testing.T) {
	bt, _ := newTestBrowserTool()
	blockedExprs := []string{
		"require('fs')",
		"process.exit(1)",
		"child_process.exec('ls')",
		"obj.__proto__.polluted = true",
		"[].constructor.constructor('return this')()",
		"fs.readFileSync('/etc/passwd')",
		"obj.fs.writeFile('x','y')",
		"(fs.unlink('/tmp/x'))",
	}
	for _, expr := range blockedExprs {
		result := execBrowser(t, bt, browserParams{Action: "evaluate", Expression: expr})
		if !result.IsError {
			t.Errorf("expected blocked for expression: %s", expr)
		}
		if !strings.Contains(result.Content, "expression blocked") {
			t.Errorf("error should mention blocked, got: %s", result.Content)
		}
	}
}

func TestBrowserEvaluateWordPatternNoFalsePositive(t *testing.T) {
	bt, _ := newTestBrowserTool()
	// These contain "fs." as a substring but not as a standalone token.
	allowedExprs := []string{
		"this.refs.current",
		"prefs.theme",
		"buffs.length",
		"dfs.search()",
		"var offsetSize = 10",
	}
	for _, expr := range allowedExprs {
		result := execBrowser(t, bt, browserParams{Action: "evaluate", Expression: expr})
		if result.IsError {
			t.Errorf("should NOT be blocked: %q, got: %s", expr, result.Content)
		}
	}
}

func TestBrowserEvaluateTooLong(t *testing.T) {
	bt, _ := newTestBrowserTool()
	longExpr := strings.Repeat("x", maxJSExpressionLen+1)
	result := execBrowser(t, bt, browserParams{Action: "evaluate", Expression: longExpr})
	if !result.IsError {
		t.Error("expected error for too-long expression")
	}
	if !strings.Contains(result.Content, "expression too long") {
		t.Errorf("error should mention too long, got: %s", result.Content)
	}
}

// --- WaitVisible tests ---

func TestBrowserWaitVisibleSuccess(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "wait_visible", Selector: "#loader"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Element is visible") {
		t.Errorf("expected visible confirmation, got: %s", result.Content)
	}
}

func TestBrowserWaitVisibleEmptySelector(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "wait_visible", Selector: ""})
	if !result.IsError {
		t.Error("expected error for empty selector")
	}
}

// --- Tab tests ---

func TestBrowserTabListSuccess(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "tab_list"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "t1") {
		t.Errorf("expected tab ID in output, got: %s", result.Content)
	}
}

func TestBrowserTabOpenSuccess(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "tab_open", URL: "https://example.com"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "t2") {
		t.Errorf("expected target_id in output, got: %s", result.Content)
	}
}

func TestBrowserTabOpenNoURL(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "tab_open"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestBrowserTabOpenSSRFBlocked(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "tab_open", URL: "http://127.0.0.1/"})
	if !result.IsError {
		t.Error("expected SSRF block for tab_open with private IP")
	}
}

func TestBrowserTabCloseSuccess(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "tab_close", TargetID: "t1"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Closed tab") {
		t.Errorf("expected close confirmation, got: %s", result.Content)
	}
}

func TestBrowserTabCloseEmptyTargetID(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "tab_close", TargetID: ""})
	if !result.IsError {
		t.Error("expected error for empty target_id")
	}
}

func TestBrowserTabFocusSuccess(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "tab_focus", TargetID: "t1"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Focused tab") {
		t.Errorf("expected focus confirmation, got: %s", result.Content)
	}
}

func TestBrowserTabFocusEmptyTargetID(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "tab_focus", TargetID: ""})
	if !result.IsError {
		t.Error("expected error for empty target_id")
	}
}

// --- Status tests ---

func TestBrowserStatusSuccess(t *testing.T) {
	bt, _ := newTestBrowserTool()
	result := execBrowser(t, bt, browserParams{Action: "status"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "connected") {
		t.Errorf("expected connected status, got: %s", result.Content)
	}
}

func TestBrowserStatusBackendError(t *testing.T) {
	bt, backend := newTestBrowserTool()
	backend.statusErr = fmt.Errorf("not connected")
	result := execBrowser(t, bt, browserParams{Action: "status"})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}
