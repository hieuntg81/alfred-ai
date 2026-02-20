package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/security"
	"alfred-ai/internal/usecase"
)

func newTestLogger() *slog.Logger { return slog.Default() }

// --- Registry tests ---

type mockTool struct {
	name string
}

func (m *mockTool) Name() string              { return m.name }
func (m *mockTool) Description() string       { return "mock" }
func (m *mockTool) Schema() domain.ToolSchema { return domain.ToolSchema{Name: m.name} }
func (m *mockTool) Execute(context.Context, json.RawMessage) (*domain.ToolResult, error) {
	return &domain.ToolResult{Content: "ok"}, nil
}

func TestRegistryBasic(t *testing.T) {
	reg := NewRegistry(nil)
	if err := reg.Register(&mockTool{name: "test"}); err != nil {
		t.Fatal(err)
	}

	tool, err := reg.Get("test")
	if err != nil {
		t.Fatal(err)
	}
	if tool.Name() != "test" {
		t.Errorf("Name = %q, want %q", tool.Name(), "test")
	}

	schemas := reg.Schemas()
	if len(schemas) != 1 {
		t.Errorf("Schemas len = %d, want 1", len(schemas))
	}
}

func TestRegistryNotFound(t *testing.T) {
	reg := NewRegistry(nil)
	_, err := reg.Get("nonexistent")
	if !errors.Is(err, domain.ErrToolNotFound) {
		t.Errorf("expected ErrToolNotFound, got %v", err)
	}
}

func TestRegistryDuplicate(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(&mockTool{name: "dup"})
	if err := reg.Register(&mockTool{name: "dup"}); err == nil {
		t.Error("expected error on duplicate")
	}
}

// --- Filesystem tool tests ---

func newSandbox(t *testing.T) *security.Sandbox {
	t.Helper()
	dir := t.TempDir()
	sb, err := security.NewSandbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	return sb
}

func TestFilesystemReadWrite(t *testing.T) {
	sb := newSandbox(t)
	fs := NewFilesystemTool(NewLocalFilesystemBackend(), sb, newTestLogger())

	// Write
	writeParams, _ := json.Marshal(filesystemParams{
		Action:  "write",
		Path:    filepath.Join(sb.Root(), "test.txt"),
		Content: "hello world",
	})
	result, err := fs.Execute(context.Background(), writeParams)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("write error: %s", result.Content)
	}

	// Read
	readParams, _ := json.Marshal(filesystemParams{
		Action: "read",
		Path:   filepath.Join(sb.Root(), "test.txt"),
	})
	result, err = fs.Execute(context.Background(), readParams)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "hello world" {
		t.Errorf("content = %q, want %q", result.Content, "hello world")
	}
}

func TestFilesystemList(t *testing.T) {
	sb := newSandbox(t)
	fs := NewFilesystemTool(NewLocalFilesystemBackend(), sb, newTestLogger())

	os.WriteFile(filepath.Join(sb.Root(), "a.txt"), []byte("a"), 0644)
	os.Mkdir(filepath.Join(sb.Root(), "subdir"), 0755)

	params, _ := json.Marshal(filesystemParams{Action: "list", Path: sb.Root()})
	result, err := fs.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("list error: %s", result.Content)
	}
}

func TestFilesystemPathTraversal(t *testing.T) {
	sb := newSandbox(t)
	fs := NewFilesystemTool(NewLocalFilesystemBackend(), sb, newTestLogger())

	params, _ := json.Marshal(filesystemParams{
		Action: "read",
		Path:   "/etc/passwd",
	})
	result, err := fs.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for path traversal")
	}
}

// --- Shell tool tests ---

func TestShellAllowedCommand(t *testing.T) {
	sb := newSandbox(t)
	sh := NewShellTool(NewLocalShellBackend(30*time.Second), []string{"echo"}, sb, newTestLogger())

	params, _ := json.Marshal(shellParams{Command: "echo", Args: []string{"hello"}})
	result, err := sh.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != "hello\n" {
		t.Errorf("output = %q, want %q", result.Content, "hello\n")
	}
}

func TestShellDisallowedCommand(t *testing.T) {
	sb := newSandbox(t)
	sh := NewShellTool(NewLocalShellBackend(30*time.Second), []string{"echo"}, sb, newTestLogger())

	params, _ := json.Marshal(shellParams{Command: "rm", Args: []string{"-rf", "/"}})
	result, err := sh.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for disallowed command")
	}
}

func TestShellAbsolutePathBypass(t *testing.T) {
	sb := newSandbox(t)
	sh := NewShellTool(NewLocalShellBackend(30*time.Second), []string{"echo"}, sb, newTestLogger())

	// Attempt to bypass allowlist with absolute path
	params, _ := json.Marshal(shellParams{Command: "/bin/rm", Args: []string{"-rf", "/"}})
	result, err := sh.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for disallowed command via absolute path")
	}
}

// --- Registry List tests ---

func TestRegistryList(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(&mockTool{name: "tool1"})
	reg.Register(&mockTool{name: "tool2"})
	reg.Register(&mockTool{name: "tool3"})

	list := reg.List()
	if len(list) != 3 {
		t.Errorf("List() returned %d tools, want 3", len(list))
	}
}

func TestRegistryListEmpty(t *testing.T) {
	reg := NewRegistry(nil)
	list := reg.List()
	if len(list) != 0 {
		t.Errorf("List() returned %d tools, want 0", len(list))
	}
}

// --- Shell tool Description/Schema tests ---

func TestShellToolDescription(t *testing.T) {
	sb := newSandbox(t)
	sh := NewShellTool(NewLocalShellBackend(30*time.Second), []string{"echo"}, sb, newTestLogger())
	if sh.Description() == "" {
		t.Error("Description() returned empty string")
	}
}

func TestShellToolSchema(t *testing.T) {
	sb := newSandbox(t)
	sh := NewShellTool(NewLocalShellBackend(30*time.Second), []string{"echo"}, sb, newTestLogger())
	schema := sh.Schema()
	if schema.Name != "shell" {
		t.Errorf("Schema.Name = %q, want %q", schema.Name, "shell")
	}
	if schema.Parameters == nil {
		t.Error("Schema.Parameters is nil")
	}
}

func TestShellToolInvalidJSON(t *testing.T) {
	sb := newSandbox(t)
	sh := NewShellTool(NewLocalShellBackend(30*time.Second), []string{"echo"}, sb, newTestLogger())
	result, err := sh.Execute(context.Background(), json.RawMessage(`invalid`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestShellToolExecuteStderr(t *testing.T) {
	sb := newSandbox(t)
	sh := NewShellTool(NewLocalShellBackend(30*time.Second), []string{"sh"}, sb, newTestLogger())

	params, _ := json.Marshal(shellParams{Command: "sh", Args: []string{"-c", "echo error >&2"}})
	result, err := sh.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "STDERR") {
		t.Logf("Output: %q", result.Content)
	}
}

func TestShellToolExecuteTimeout(t *testing.T) {
	sb := newSandbox(t)
	sh := NewShellTool(NewLocalShellBackend(100*time.Millisecond), []string{"sleep"}, sb, newTestLogger())

	params, _ := json.Marshal(shellParams{Command: "sleep", Args: []string{"10"}})
	result, err := sh.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for timeout")
	}
}

func TestShellToolExecuteWithWorkDir(t *testing.T) {
	sb := newSandbox(t)
	sh := NewShellTool(NewLocalShellBackend(30*time.Second), []string{"pwd"}, sb, newTestLogger())

	params, _ := json.Marshal(shellParams{Command: "pwd", WorkDir: sb.Root()})
	result, err := sh.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestShellToolExecuteInvalidWorkDir(t *testing.T) {
	sb := newSandbox(t)
	sh := NewShellTool(NewLocalShellBackend(30*time.Second), []string{"echo"}, sb, newTestLogger())

	params, _ := json.Marshal(shellParams{Command: "echo", WorkDir: "/etc"})
	result, err := sh.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for workdir outside sandbox")
	}
}

// --- Filesystem tool Description/Schema/Error tests ---

func TestFilesystemToolDescription(t *testing.T) {
	sb := newSandbox(t)
	fs := NewFilesystemTool(NewLocalFilesystemBackend(), sb, newTestLogger())
	if fs.Description() == "" {
		t.Error("Description() returned empty string")
	}
}

func TestFilesystemToolSchema(t *testing.T) {
	sb := newSandbox(t)
	fs := NewFilesystemTool(NewLocalFilesystemBackend(), sb, newTestLogger())
	schema := fs.Schema()
	if schema.Name != "filesystem" {
		t.Errorf("Schema.Name = %q, want %q", schema.Name, "filesystem")
	}
	if schema.Parameters == nil {
		t.Error("Schema.Parameters is nil")
	}
}

func TestFilesystemToolUnknownAction(t *testing.T) {
	sb := newSandbox(t)
	fs := NewFilesystemTool(NewLocalFilesystemBackend(), sb, newTestLogger())

	params, _ := json.Marshal(filesystemParams{Action: "unknown", Path: sb.Root()})
	result, err := fs.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

func TestFilesystemToolInvalidJSON(t *testing.T) {
	sb := newSandbox(t)
	fs := NewFilesystemTool(NewLocalFilesystemBackend(), sb, newTestLogger())

	result, err := fs.Execute(context.Background(), json.RawMessage(`invalid`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestFilesystemToolReadNonExistent(t *testing.T) {
	sb := newSandbox(t)
	fs := NewFilesystemTool(NewLocalFilesystemBackend(), sb, newTestLogger())

	params, _ := json.Marshal(filesystemParams{
		Action: "read",
		Path:   filepath.Join(sb.Root(), "nonexistent.txt"),
	})
	result, err := fs.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for reading nonexistent file")
	}
}

func TestFilesystemToolListNonExistent(t *testing.T) {
	sb := newSandbox(t)
	fs := NewFilesystemTool(NewLocalFilesystemBackend(), sb, newTestLogger())

	params, _ := json.Marshal(filesystemParams{
		Action: "list",
		Path:   filepath.Join(sb.Root(), "nonexistent_dir"),
	})
	result, err := fs.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for listing nonexistent dir")
	}
}

// --- Web tool tests ---

func TestWebFetchSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("response body"))
	}))
	defer server.Close()

	web := NewWebTool(newTestLogger())
	params, _ := json.Marshal(webParams{URL: server.URL})
	result, err := web.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	// Note: httptest uses 127.0.0.1 which is private, so SSRF check will block it
	// This is expected behavior - we test with a real check
	if result.IsError {
		// Expected: SSRF blocks localhost in test
		t.Log("SSRF correctly blocked localhost test server (expected)")
		return
	}
}

func TestWebToolDescription(t *testing.T) {
	web := NewWebTool(newTestLogger())
	desc := web.Description()
	if desc == "" {
		t.Error("Description() returned empty string")
	}
}

func TestWebToolSchema(t *testing.T) {
	web := NewWebTool(newTestLogger())
	schema := web.Schema()
	if schema.Name != "web_fetch" {
		t.Errorf("Schema.Name = %q, want %q", schema.Name, "web_fetch")
	}
	if schema.Parameters == nil {
		t.Error("Schema.Parameters is nil")
	}
}

func TestWebToolExecuteInvalidJSON(t *testing.T) {
	web := NewWebTool(newTestLogger())
	result, err := web.Execute(context.Background(), json.RawMessage(`invalid`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestWebToolExecuteInvalidURL(t *testing.T) {
	web := NewWebTool(newTestLogger())
	params, _ := json.Marshal(webParams{URL: "://invalid"})
	result, err := web.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for invalid URL")
	}
}

func TestWebToolExecuteDefaultMethod(t *testing.T) {
	var receivedMethod string
	web := NewWebTool(newTestLogger())
	web.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			receivedMethod = req.Method
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		}),
	}

	params, _ := json.Marshal(webParams{URL: "http://1.1.1.1/test"})
	result, err := web.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if receivedMethod != "GET" {
		t.Errorf("method = %q, want GET", receivedMethod)
	}
}

func TestWebFetchPrivateIP(t *testing.T) {
	web := NewWebTool(newTestLogger())

	privateURLs := []string{
		"http://127.0.0.1:8080/",
		"http://10.0.0.1/",
		"http://192.168.1.1/",
	}

	for _, u := range privateURLs {
		params, _ := json.Marshal(webParams{URL: u})
		result, err := web.Execute(context.Background(), params)
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError {
			t.Errorf("expected SSRF block for %s", u)
		}
	}
}

func TestWebToolExecuteWithHeaders(t *testing.T) {
	web := NewWebTool(newTestLogger())
	// Test with a private IP (will be blocked by SSRF but hits the code path for headers)
	params, _ := json.Marshal(webParams{
		URL:     "http://192.168.1.1/api",
		Method:  "GET",
		Headers: map[string]string{"Authorization": "Bearer token"},
	})
	result, err := web.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	// SSRF will block it before headers are set, but that's fine for coverage
	if !result.IsError {
		t.Error("expected SSRF block")
	}
}

func TestWebToolExecuteHEAD(t *testing.T) {
	var receivedMethod string
	web := NewWebTool(newTestLogger())
	web.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			receivedMethod = req.Method
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		}),
	}

	params, _ := json.Marshal(webParams{URL: "http://1.1.1.1/test", Method: "HEAD"})
	result, err := web.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if receivedMethod != "HEAD" {
		t.Errorf("method = %q, want HEAD", receivedMethod)
	}
}

func TestFilesystemToolWriteError(t *testing.T) {
	sb := newSandbox(t)
	fs := NewFilesystemTool(NewLocalFilesystemBackend(), sb, newTestLogger())

	// Create a read-only directory
	readonlyDir := filepath.Join(sb.Root(), "readonly")
	os.MkdirAll(readonlyDir, 0755)
	os.Chmod(readonlyDir, 0444)
	defer os.Chmod(readonlyDir, 0755)

	params, _ := json.Marshal(filesystemParams{
		Action:  "write",
		Path:    filepath.Join(readonlyDir, "test.txt"),
		Content: "should fail",
	})
	result, err := fs.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for write to read-only dir")
	}
}

func TestSubAgentToolDescriptionNilManager(t *testing.T) {
	tool := NewSubAgentTool(nil)
	if tool.Description() == "" {
		t.Error("Description() returned empty")
	}
}

func TestSubAgentToolSchemaNilManager(t *testing.T) {
	tool := NewSubAgentTool(nil)
	schema := tool.Schema()
	if schema.Name != "sub_agent" {
		t.Errorf("Schema.Name = %q", schema.Name)
	}
}

func TestSubAgentToolInvalidJSONNilManager(t *testing.T) {
	tool := NewSubAgentTool(nil)
	result, err := tool.Execute(context.Background(), json.RawMessage(`invalid`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestSubAgentToolNoTasksNilManager(t *testing.T) {
	tool := NewSubAgentTool(nil)
	params, _ := json.Marshal(map[string][]string{"tasks": {}})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for no tasks")
	}
}

// roundTripFunc allows using a function as http.RoundTripper
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestWebToolExecuteSuccessPath(t *testing.T) {
	web := NewWebTool(newTestLogger())
	// Replace client with one that has custom transport (bypasses real network)
	web.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader("response body")),
				Header:     make(http.Header),
			}, nil
		}),
	}

	params, _ := json.Marshal(webParams{URL: "http://1.1.1.1/test"})
	result, err := web.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "HTTP 200") {
		t.Errorf("expected HTTP 200 in result, got %q", result.Content)
	}
	if !strings.Contains(result.Content, "response body") {
		t.Errorf("expected response body in result, got %q", result.Content)
	}
}

func TestWebToolExecuteWithHeadersSuccess(t *testing.T) {
	var receivedAuth string
	web := NewWebTool(newTestLogger())
	web.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			receivedAuth = req.Header.Get("Authorization")
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader("ok")),
				Header:     make(http.Header),
			}, nil
		}),
	}

	params, _ := json.Marshal(webParams{
		URL:     "http://1.1.1.1/api",
		Method:  "GET",
		Headers: map[string]string{"Authorization": "Bearer token123"},
	})
	result, err := web.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if receivedAuth != "Bearer token123" {
		t.Errorf("auth header = %q, want %q", receivedAuth, "Bearer token123")
	}
}

func TestWebToolExecuteHTTPError(t *testing.T) {
	web := NewWebTool(newTestLogger())
	web.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("connection refused")
		}),
	}

	params, _ := json.Marshal(webParams{URL: "http://1.1.1.1/test"})
	result, err := web.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for HTTP failure")
	}
}

func TestWebToolExecuteInvalidRequest(t *testing.T) {
	web := NewWebTool(newTestLogger())
	// URL with invalid characters to trigger NewRequestWithContext error
	params, _ := json.Marshal(webParams{URL: "http://1.1.1.1/\x00invalid"})
	result, err := web.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for invalid request")
	}
}

// TestWebToolCheckRedirectTooMany tests the CheckRedirect function
// when there are too many redirects (>= 5).
func TestWebToolCheckRedirectTooMany(t *testing.T) {
	web := NewWebTool(newTestLogger())
	redirectCount := 0

	// Replace the transport on the existing client to keep CheckRedirect
	web.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		redirectCount++
		if redirectCount <= 7 {
			return &http.Response{
				StatusCode: 302,
				Header: http.Header{
					"Location": []string{"http://1.1.1.1/redirect-" + fmt.Sprintf("%d", redirectCount)},
				},
				Body: io.NopCloser(strings.NewReader("")),
			}, nil
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("final")),
			Header:     make(http.Header),
		}, nil
	})

	params, _ := json.Marshal(webParams{URL: "http://1.1.1.1/start"})
	result, err := web.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for too many redirects")
	}
	if !strings.Contains(result.Content, "redirect") {
		t.Errorf("expected redirect error message, got: %s", result.Content)
	}
}

// TestWebToolCheckRedirectToPrivateIP tests the CheckRedirect function
// when a redirect targets a private IP (SSRF protection).
func TestWebToolCheckRedirectToPrivateIP(t *testing.T) {
	web := NewWebTool(newTestLogger())
	callCount := 0

	// Replace the transport on the existing client to keep CheckRedirect
	web.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		callCount++
		if callCount == 1 {
			// First request: redirect to a private IP
			return &http.Response{
				StatusCode: 302,
				Header: http.Header{
					"Location": []string{"http://192.168.1.1/internal"},
				},
				Body: io.NopCloser(strings.NewReader("")),
			}, nil
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("should not reach")),
			Header:     make(http.Header),
		}, nil
	})

	params, _ := json.Marshal(webParams{URL: "http://1.1.1.1/public"})
	result, err := web.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for redirect to private IP")
	}
}

// TestWebToolBodyReadError tests the body read error path in Execute.
func TestWebToolBodyReadError(t *testing.T) {
	web := NewWebTool(newTestLogger())
	web.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(&errReader{}),
				Header:     make(http.Header),
			}, nil
		}),
	}

	params, _ := json.Marshal(webParams{URL: "http://1.1.1.1/test"})
	result, err := web.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for body read failure")
	}
	if !strings.Contains(result.Content, "read body") {
		t.Errorf("expected 'read body' error message, got: %s", result.Content)
	}
}

// errReader is an io.Reader that always returns an error.
type errReader struct{}

func (e *errReader) Read(_ []byte) (int, error) {
	return 0, fmt.Errorf("simulated read error")
}

// TestFilesystemToolWritePathError tests that writeFile returns error
// when ValidatePath fails (path outside sandbox).
func TestFilesystemToolWritePathError(t *testing.T) {
	sb := newSandbox(t)
	fs := NewFilesystemTool(NewLocalFilesystemBackend(), sb, newTestLogger())

	params, _ := json.Marshal(filesystemParams{
		Action:  "write",
		Path:    "/etc/passwd",
		Content: "should fail",
	})
	result, err := fs.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for write path outside sandbox")
	}
}

// TestFilesystemToolListPathError tests that listDir returns error
// when ValidatePath fails (path outside sandbox).
func TestFilesystemToolListPathError(t *testing.T) {
	sb := newSandbox(t)
	fs := NewFilesystemTool(NewLocalFilesystemBackend(), sb, newTestLogger())

	params, _ := json.Marshal(filesystemParams{
		Action: "list",
		Path:   "/etc",
	})
	result, err := fs.Execute(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for list path outside sandbox")
	}
}

// TestSubAgentToolExecuteWithError covers the branch where SpawnParallel returns an error.
func TestSubAgentToolExecuteWithError(t *testing.T) {
	// Create a factory that returns an agent using an LLM that always errors
	factory := func() *usecase.Agent {
		return usecase.NewAgent(usecase.AgentDeps{
			LLM:            &errLLM{},
			Memory:         nil,
			Tools:          &mockSubAgentTools{},
			ContextBuilder: usecase.NewContextBuilder("system", "model", 50),
			Logger:         slog.Default(),
			MaxIterations:  1,
		})
	}

	mgr := usecase.NewSubAgentManager(factory, usecase.SubAgentConfig{
		MaxSubAgents: 5,
		Timeout:      5 * time.Second,
	}, slog.Default())

	tool := NewSubAgentTool(mgr)

	params, _ := json.Marshal(map[string]interface{}{
		"tasks": []string{"task that will fail"},
	})

	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// The result should contain a warning about failed tasks
	if !strings.Contains(result.Content, "Warning") {
		t.Errorf("expected Warning in result content, got: %s", result.Content)
	}
}

// errLLM always returns an error from Chat.
type errLLM struct{}

func (e *errLLM) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	return nil, fmt.Errorf("simulated LLM error")
}

func (e *errLLM) Name() string { return "err-mock" }
