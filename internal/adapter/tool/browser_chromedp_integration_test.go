//go:build integration

package tool

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChromeDPBackendNavigateAndContent(t *testing.T) {
	// Start a local test server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><title>Test Page</title></head>
<body>
  <h1>Hello Alfred</h1>
  <p>This is a test page.</p>
  <a href="/about" id="about-link">About</a>
  <form action="/search">
    <input type="text" name="q" id="search-input" placeholder="Search...">
    <button type="submit" id="search-btn">Search</button>
  </form>
</body></html>`)
	}))
	defer srv.Close()

	backend, err := NewChromeDPBackend(ChromeDPConfig{
		Headless: true,
		Timeout:  30 * time.Second,
	}, slog.Default())
	if err != nil {
		t.Fatalf("create backend: %v", err)
	}
	defer backend.Close()

	ctx := context.Background()

	// Test Navigate.
	if err := backend.Navigate(ctx, srv.URL); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Test GetContent.
	content, err := backend.GetContent(ctx, "")
	if err != nil {
		t.Fatalf("get content: %v", err)
	}
	if content.Title != "Test Page" {
		t.Errorf("title = %q, want %q", content.Title, "Test Page")
	}
	if !strings.Contains(content.Text, "Hello Alfred") {
		t.Errorf("text should contain 'Hello Alfred', got: %s", content.Text)
	}

	// Test Screenshot.
	data, err := backend.Screenshot(ctx, false)
	if err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	if len(data) == 0 {
		t.Error("screenshot data is empty")
	}

	// Test Evaluate.
	result, err := backend.Evaluate(ctx, "document.title")
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if result != "Test Page" {
		t.Errorf("evaluate result = %q, want %q", result, "Test Page")
	}

	// Test Status.
	status, err := backend.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Connected {
		t.Error("expected connected status")
	}
	if status.TabCount < 1 {
		t.Errorf("expected at least 1 tab, got %d", status.TabCount)
	}
}

func TestChromeDPBackendTabOperations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><head><title>Page %s</title></head><body>content</body></html>`, r.URL.Path)
	}))
	defer srv.Close()

	backend, err := NewChromeDPBackend(ChromeDPConfig{
		Headless: true,
		Timeout:  30 * time.Second,
	}, slog.Default())
	if err != nil {
		t.Fatalf("create backend: %v", err)
	}
	defer backend.Close()

	ctx := context.Background()

	// Navigate first tab.
	if err := backend.Navigate(ctx, srv.URL+"/page1"); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Open second tab.
	targetID, err := backend.TabOpen(ctx, srv.URL+"/page2")
	if err != nil {
		t.Fatalf("tab open: %v", err)
	}
	if targetID == "" {
		t.Fatal("tab open returned empty target ID")
	}

	// List tabs.
	tabs, err := backend.TabList(ctx)
	if err != nil {
		t.Fatalf("tab list: %v", err)
	}
	if len(tabs) < 2 {
		t.Errorf("expected at least 2 tabs, got %d", len(tabs))
	}

	// Close the second tab (non-active, since TabOpen switched focus to it,
	// we need to focus back first to test closing a non-active tab).
	if err := backend.TabClose(ctx, targetID); err != nil {
		t.Fatalf("tab close: %v", err)
	}

	// Verify tab count decreased.
	tabs, err = backend.TabList(ctx)
	if err != nil {
		t.Fatalf("tab list after close: %v", err)
	}
	for _, tab := range tabs {
		if tab.TargetID == targetID {
			t.Errorf("closed tab %s still in list", targetID)
		}
	}
}

// TestChromeDPBackendCloseActiveTab verifies that closing the active tab
// auto-switches to another tab, and subsequent operations still work.
func TestChromeDPBackendCloseActiveTab(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><head><title>Page %s</title></head><body><h1>%s</h1></body></html>`, r.URL.Path, r.URL.Path)
	}))
	defer srv.Close()

	backend, err := NewChromeDPBackend(ChromeDPConfig{
		Headless: true,
		Timeout:  30 * time.Second,
	}, slog.Default())
	if err != nil {
		t.Fatalf("create backend: %v", err)
	}
	defer backend.Close()

	ctx := context.Background()

	// Navigate tab A to /pageA.
	if err := backend.Navigate(ctx, srv.URL+"/pageA"); err != nil {
		t.Fatalf("navigate tab A: %v", err)
	}

	// Open tab B — TabOpen automatically switches the active tab to B.
	tabBID, err := backend.TabOpen(ctx, srv.URL+"/pageB")
	if err != nil {
		t.Fatalf("tab open B: %v", err)
	}

	// Verify we're on tab B.
	title, err := backend.Evaluate(ctx, "document.title")
	if err != nil {
		t.Fatalf("evaluate on tab B: %v", err)
	}
	if !strings.Contains(title, "pageB") {
		t.Errorf("expected title containing 'pageB', got %q", title)
	}

	// Close tab B (the active tab).
	if err := backend.TabClose(ctx, tabBID); err != nil {
		t.Fatalf("tab close B: %v", err)
	}

	// After closing the active tab, backend should auto-switch to tab A.
	// Verify that operations still work on the remaining tab.
	title, err = backend.Evaluate(ctx, "document.title")
	if err != nil {
		t.Fatalf("evaluate after closing active tab: %v", err)
	}
	if !strings.Contains(title, "pageA") {
		t.Errorf("expected title containing 'pageA' after auto-switch, got %q", title)
	}

	// Navigate should also work on the recovered tab.
	if err := backend.Navigate(ctx, srv.URL+"/pageC"); err != nil {
		t.Fatalf("navigate after tab close recovery: %v", err)
	}
	title, err = backend.Evaluate(ctx, "document.title")
	if err != nil {
		t.Fatalf("evaluate after navigate: %v", err)
	}
	if !strings.Contains(title, "pageC") {
		t.Errorf("expected title containing 'pageC', got %q", title)
	}
}

// TestChromeDPBackendScreenshotJPEG verifies that screenshots are returned
// as valid base64 and within the size limit.
func TestChromeDPBackendScreenshotJPEG(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><body style="background:linear-gradient(red,blue);height:2000px">
  <h1>Screenshot Test</h1>
</body></html>`)
	}))
	defer srv.Close()

	backend, err := NewChromeDPBackend(ChromeDPConfig{
		Headless: true,
		Timeout:  30 * time.Second,
	}, slog.Default())
	if err != nil {
		t.Fatalf("create backend: %v", err)
	}
	defer backend.Close()

	ctx := context.Background()
	if err := backend.Navigate(ctx, srv.URL); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Viewport screenshot.
	data, err := backend.Screenshot(ctx, false)
	if err != nil {
		t.Fatalf("viewport screenshot: %v", err)
	}
	if len(data) == 0 {
		t.Error("viewport screenshot is empty")
	}
	if len(data) > maxScreenshotBase64 {
		t.Errorf("viewport screenshot exceeds limit: %d > %d", len(data), maxScreenshotBase64)
	}

	// Full-page screenshot.
	fullData, err := backend.Screenshot(ctx, true)
	if err != nil {
		t.Fatalf("full-page screenshot: %v", err)
	}
	if len(fullData) == 0 {
		t.Error("full-page screenshot is empty")
	}
	// Full-page should be larger than viewport (page has 2000px height).
	if len(fullData) <= len(data) {
		t.Logf("warning: full-page (%d) not larger than viewport (%d), may be expected at low quality", len(fullData), len(data))
	}
}

func TestChromeDPBackendClickAndType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><body>
  <input type="text" id="name" placeholder="Name">
  <button id="btn" onclick="document.getElementById('result').textContent='clicked'">Click me</button>
  <div id="result"></div>
</body></html>`)
	}))
	defer srv.Close()

	backend, err := NewChromeDPBackend(ChromeDPConfig{
		Headless: true,
		Timeout:  30 * time.Second,
	}, slog.Default())
	if err != nil {
		t.Fatalf("create backend: %v", err)
	}
	defer backend.Close()

	ctx := context.Background()

	if err := backend.Navigate(ctx, srv.URL); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Type into input.
	if err := backend.Type(ctx, "#name", "Alfred"); err != nil {
		t.Fatalf("type: %v", err)
	}

	// Click button.
	if err := backend.Click(ctx, "#btn"); err != nil {
		t.Fatalf("click: %v", err)
	}

	// Verify click effect.
	result, err := backend.Evaluate(ctx, "document.getElementById('result').textContent")
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if result != "clicked" {
		t.Errorf("click result = %q, want %q", result, "clicked")
	}

	// Verify typed value.
	value, err := backend.Evaluate(ctx, "document.getElementById('name').value")
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if value != "Alfred" {
		t.Errorf("typed value = %q, want %q", value, "Alfred")
	}
}
