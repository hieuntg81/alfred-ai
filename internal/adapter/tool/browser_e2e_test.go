package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// TestBrowserE2E_RawJSON exercises Execute with raw JSON strings, simulating
// real LLM tool calls. No struct marshaling — pure JSON in, result out.
func TestBrowserE2E_RawJSON(t *testing.T) {
	backend := &mockBrowserBackend{
		contentResult: &PageContent{
			Title: "Example",
			URL:   "https://example.com",
			Text:  "[h1] Welcome\n[link selector=\"a\" href=\"/about\"] About",
			Links: []PageLink{{Index: 0, Text: "About", Href: "/about", Selector: "a"}},
		},
		screenshotData: "data:image/jpeg;base64,/9j/4AAQ",
		evaluateResult: "Hello from JS",
		tabs: []TabInfo{
			{TargetID: "ABC123", Title: "Tab 1", URL: "https://example.com", Active: true},
			{TargetID: "DEF456", Title: "Tab 2", URL: "https://other.com", Active: false},
		},
		tabOpenID:    "NEW789",
		statusResult: &BrowserStatus{Connected: true, Backend: "mock", TabCount: 2, ActiveTabURL: "https://example.com"},
	}
	bt := NewBrowserTool(backend, slog.Default())

	tests := []struct {
		name      string
		json      string
		wantError bool
		contains  string // substring expected in result.Content
	}{
		{
			name:     "navigate with url",
			json:     `{"action":"navigate","url":"https://example.com/page"}`,
			contains: "Navigated to https://example.com/page",
		},
		{
			name:     "get_content no selector",
			json:     `{"action":"get_content"}`,
			contains: "Page: Example",
		},
		{
			name:     "get_content scoped",
			json:     `{"action":"get_content","selector":"#main"}`,
			contains: "Page: Example",
		},
		{
			name:     "screenshot viewport",
			json:     `{"action":"screenshot"}`,
			contains: "Screenshot captured",
		},
		{
			name:     "screenshot full page",
			json:     `{"action":"screenshot","full_page":true}`,
			contains: "Screenshot captured",
		},
		{
			name:     "click element",
			json:     `{"action":"click","selector":"a[href='/about']"}`,
			contains: "Clicked element",
		},
		{
			name:     "type into input",
			json:     `{"action":"type","selector":"input#search","text":"query text"}`,
			contains: "Typed 10 chars",
		},
		{
			name:     "evaluate safe JS",
			json:     `{"action":"evaluate","expression":"document.querySelectorAll('a').length"}`,
			contains: "Hello from JS",
		},
		{
			name:     "wait_visible",
			json:     `{"action":"wait_visible","selector":"#loaded"}`,
			contains: "Element is visible",
		},
		{
			name:     "tab_list",
			json:     `{"action":"tab_list"}`,
			contains: "ABC123",
		},
		{
			name:     "tab_open with url",
			json:     `{"action":"tab_open","url":"https://example.com/new"}`,
			contains: "NEW789",
		},
		{
			name:     "tab_open blank",
			json:     `{"action":"tab_open"}`,
			contains: "Opened new tab",
		},
		{
			name:     "tab_close",
			json:     `{"action":"tab_close","target_id":"DEF456"}`,
			contains: "Closed tab: DEF456",
		},
		{
			name:     "tab_focus",
			json:     `{"action":"tab_focus","target_id":"ABC123"}`,
			contains: "Focused tab: ABC123",
		},
		{
			name:     "status",
			json:     `{"action":"status"}`,
			contains: `"connected": true`,
		},

		// Error cases with raw JSON.
		{
			name:      "malformed json",
			json:      `{"action": navigate}`,
			wantError: true,
			contains:  "invalid params",
		},
		{
			name:      "missing action",
			json:      `{}`,
			wantError: true,
			contains:  "unknown action",
		},
		{
			name:      "navigate no url",
			json:      `{"action":"navigate"}`,
			wantError: true,
			contains:  "'url' is required",
		},
		{
			name:      "navigate ssrf",
			json:      `{"action":"navigate","url":"http://169.254.169.254/metadata"}`,
			wantError: true,
		},
		{
			name:      "click no selector",
			json:      `{"action":"click"}`,
			wantError: true,
			contains:  "'selector' is required",
		},
		{
			name:      "type no text",
			json:      `{"action":"type","selector":"#x"}`,
			wantError: true,
			contains:  "'text' is required",
		},
		{
			name:      "evaluate blocked require",
			json:      `{"action":"evaluate","expression":"require('child_process')"}`,
			wantError: true,
			contains:  "expression blocked",
		},
		{
			name:      "evaluate blocked fs standalone",
			json:      `{"action":"evaluate","expression":"fs.readFileSync('/etc/passwd')"}`,
			wantError: true,
			contains:  "expression blocked",
		},
		{
			name:     "evaluate allowed refs.current",
			json:     `{"action":"evaluate","expression":"this.refs.current.value"}`,
			contains: "Hello from JS",
		},
		{
			name:      "tab_close no target_id",
			json:      `{"action":"tab_close"}`,
			wantError: true,
			contains:  "'target_id' is required",
		},
		{
			name:      "unknown action",
			json:      `{"action":"scroll_down"}`,
			wantError: true,
			contains:  `unknown action "scroll_down"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := bt.Execute(context.Background(), json.RawMessage(tt.json))
			if err != nil {
				t.Fatalf("Execute returned Go error (should never happen): %v", err)
			}
			if result == nil {
				t.Fatal("result is nil")
			}
			if tt.wantError && !result.IsError {
				t.Errorf("expected IsError=true, got false. Content: %s", result.Content)
			}
			if !tt.wantError && result.IsError {
				t.Errorf("unexpected IsError=true. Content: %s", result.Content)
			}
			if tt.contains != "" && !strings.Contains(result.Content, tt.contains) {
				t.Errorf("expected content to contain %q, got: %s", tt.contains, result.Content)
			}
		})
	}
}

// TestBrowserE2E_BackendErrorPropagation verifies that backend failures are
// surfaced as IsError results for every action, never as Go errors.
func TestBrowserE2E_BackendErrorPropagation(t *testing.T) {
	backendErr := fmt.Errorf("simulated failure")
	backend := &mockBrowserBackend{
		navigateErr:   backendErr,
		contentErr:    backendErr,
		screenshotErr: backendErr,
		clickErr:      backendErr,
		typeErr:       backendErr,
		evaluateErr:   backendErr,
		waitErr:       backendErr,
		tabListErr:    backendErr,
		tabOpenErr:    backendErr,
		tabCloseErr:   backendErr,
		tabFocusErr:   backendErr,
		statusErr:     backendErr,
	}
	bt := NewBrowserTool(backend, slog.Default())

	actions := []string{
		`{"action":"navigate","url":"https://example.com"}`,
		`{"action":"get_content"}`,
		`{"action":"screenshot"}`,
		`{"action":"click","selector":"#x"}`,
		`{"action":"type","selector":"#x","text":"y"}`,
		`{"action":"evaluate","expression":"1+1"}`,
		`{"action":"wait_visible","selector":"#x"}`,
		`{"action":"tab_list"}`,
		`{"action":"tab_open","url":"https://example.com"}`,
		`{"action":"tab_close","target_id":"t1"}`,
		`{"action":"tab_focus","target_id":"t1"}`,
		`{"action":"status"}`,
	}

	for _, raw := range actions {
		t.Run(raw, func(t *testing.T) {
			result, err := bt.Execute(context.Background(), json.RawMessage(raw))
			if err != nil {
				t.Fatalf("Execute returned Go error: %v", err)
			}
			if !result.IsError {
				t.Errorf("expected IsError for failing backend, got success: %s", result.Content)
			}
			if !strings.Contains(result.Content, "simulated failure") {
				t.Errorf("expected error message to propagate, got: %s", result.Content)
			}
		})
	}
}

// TestBrowserE2E_ScreenshotSizeGuard verifies the screenshot size guard rejects
// oversized images with a clear message instead of truncating.
func TestBrowserE2E_ScreenshotSizeGuard(t *testing.T) {
	backend := &mockBrowserBackend{
		screenshotData: strings.Repeat("X", maxScreenshotBase64+100),
	}
	bt := NewBrowserTool(backend, slog.Default())

	result, err := bt.Execute(context.Background(), json.RawMessage(`{"action":"screenshot"}`))
	if err != nil {
		t.Fatalf("Execute returned Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for oversized screenshot")
	}
	if !strings.Contains(result.Content, "screenshot too large") {
		t.Errorf("expected 'screenshot too large' message, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "full_page=false") {
		t.Errorf("expected actionable suggestion, got: %s", result.Content)
	}
	// Ensure no truncated data leaks into the response.
	if strings.Contains(result.Content, "XXXX") {
		t.Error("oversized screenshot data should NOT be included in the error response")
	}
}

// TestBrowserE2E_SecurityBoundary runs a battery of attack-like expressions
// through the full Execute path and verifies they're all blocked.
func TestBrowserE2E_SecurityBoundary(t *testing.T) {
	backend := &mockBrowserBackend{evaluateResult: "SHOULD_NOT_REACH"}
	bt := NewBrowserTool(backend, slog.Default())

	attacks := []struct {
		name string
		expr string
	}{
		{"require module", "require('fs')"},
		{"require http", "require('http').createServer()"},
		{"process exit", "process.exit(0)"},
		{"process env", "process.exit(1)"},
		{"child_process spawn", "child_process.spawn('bash')"},
		{"proto pollution", "{}.__proto__.polluted = true"},
		{"constructor escape", "[].constructor.constructor('return process')()"},
		{"fs standalone", "fs.readFileSync('/etc/shadow')"},
		{"fs after dot", "globalThis.fs.writeFileSync('/tmp/x','y')"},
		{"fs after paren", "(fs.unlinkSync('/tmp/x'))"},
		{"fs after space", "var a = fs.readdirSync('/')"},
		{"fs upper case", "FS.ReadFileSync('/etc/passwd')"},

		// Mixed case evasion attempts.
		{"mixed case require", "REQUIRE('fs')"},
		{"mixed case process", "Process.Exit(1)"},
	}

	for _, att := range attacks {
		t.Run(att.name, func(t *testing.T) {
			raw := fmt.Sprintf(`{"action":"evaluate","expression":"%s"}`, att.expr)
			result, err := bt.Execute(context.Background(), json.RawMessage(raw))
			if err != nil {
				t.Fatalf("Execute returned Go error: %v", err)
			}
			if !result.IsError {
				t.Errorf("SECURITY: expression should be blocked: %s", att.expr)
			}
			if result.Content == "SHOULD_NOT_REACH" {
				t.Errorf("SECURITY: expression reached backend execution: %s", att.expr)
			}
		})
	}

	// Legitimate expressions that should pass through.
	legitimateExprs := []struct {
		name string
		expr string
	}{
		{"dom query", "document.querySelectorAll('a').length"},
		{"refs property", "this.refs.current.value"},
		{"prefs access", "window.prefs.darkMode"},
		{"buffs array", "game.buffs.filter(b => b.active)"},
		{"math", "Math.floor(Math.random() * 100)"},
		{"json stringify", "JSON.stringify({key: 'value'})"},
		{"fetch api", "fetch('/api/data').then(r => r.json())"},
		{"canvas ops", "canvas.getContext('2d').fillRect(0,0,10,10)"},
		{"offset props", "el.offsetWidth + el.offsetHeight"},
	}

	backend.evaluateResult = "OK"
	for _, leg := range legitimateExprs {
		t.Run("allowed_"+leg.name, func(t *testing.T) {
			raw := fmt.Sprintf(`{"action":"evaluate","expression":"%s"}`, leg.expr)
			result, err := bt.Execute(context.Background(), json.RawMessage(raw))
			if err != nil {
				t.Fatalf("Execute returned Go error: %v", err)
			}
			if result.IsError {
				t.Errorf("should NOT be blocked: %q, got: %s", leg.expr, result.Content)
			}
		})
	}
}
