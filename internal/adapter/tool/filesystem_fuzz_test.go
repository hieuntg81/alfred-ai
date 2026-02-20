package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alfred-ai/internal/security"
)

// FuzzFilesystemTool fuzzes filesystem operations to find path traversal and TOCTOU vulnerabilities.
// This targets the symlink TOCTOU race between ValidatePath() and actual file I/O operations.
func FuzzFilesystemTool(f *testing.F) {
	dir := f.TempDir()
	sb, err := security.NewSandbox(dir)
	if err != nil {
		f.Fatal(err)
	}

	fs := NewFilesystemTool(NewLocalFilesystemBackend(), sb, newTestLogger())

	// Create test files in sandbox
	os.WriteFile(filepath.Join(dir, "safe.txt"), []byte("safe content"), 0644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)

	// Seed corpus - path traversal attacks
	f.Add(`{"action":"read","path":"safe.txt"}`)                                                        // Valid baseline
	f.Add(`{"action":"read","path":"../../../../etc/passwd"}`)                                          // Path traversal
	f.Add(`{"action":"read","path":".."}`)                                                              // Relative traversal
	f.Add(`{"action":"read","path":"/etc/shadow"}`)                                                     // Absolute path
	f.Add(`{"action":"read","path":"safe.txt\x00../../etc/passwd"}`)                                    // Null byte
	f.Add(`{"action":"write","path":"test.txt","content":"` + strings.Repeat("A", 10*1024*1024) + `"}`) // 10MB DoS
	f.Add(`{"action":"list","path":""}`)                                                                // Empty path
	f.Add(`{"action":"delete","path":"test.txt"}`)                                                      // Invalid action
	f.Add(`{"action":"write","path":"../../escape.txt","content":"x"}`)                                 // Write escape

	f.Fuzz(func(t *testing.T, input string) {
		result, err := fs.Execute(context.Background(), json.RawMessage(input))

		if err == nil && result != nil && !result.IsError {
			var params filesystemParams
			if json.Unmarshal([]byte(input), &params) == nil {
				// Invariant 1: Path MUST be within sandbox
				resolved, pathErr := sb.ValidatePath(params.Path)
				if pathErr == nil && !strings.HasPrefix(resolved, dir) {
					t.Errorf("SECURITY: Sandbox escape - path %q resolved to %q outside %q",
						params.Path, resolved, dir)
				}

				// Invariant 2: Action MUST be valid enum
				validActions := map[string]bool{"read": true, "write": true, "list": true}
				if !validActions[params.Action] {
					t.Errorf("SECURITY: Invalid action %q executed", params.Action)
				}

				// Invariant 3: Content size should be limited (prevent DoS)
				if params.Action == "write" && len(params.Content) > 100*1024*1024 {
					t.Errorf("SECURITY: DoS - content size %d bytes exceeds limit", len(params.Content))
				}

				// Invariant 4: Result should not leak sensitive paths
				suspiciousPatterns := []string{"/etc/", "/root/", "/home/", "C:\\Windows"}
				for _, pattern := range suspiciousPatterns {
					if strings.Contains(result.Content, pattern) {
						t.Errorf("SECURITY: Information leak - result contains %q", pattern)
					}
				}
			}
		}
	})
}

// FuzzFilesystemTOCTOU tests for symlink race conditions (Time-Of-Check-Time-Of-Use).
// This is a critical vulnerability where a symlink can be swapped between path validation
// and the actual file operation, allowing reads outside the sandbox.
func FuzzFilesystemTOCTOU(f *testing.F) {
	dir := f.TempDir()
	sb, err := security.NewSandbox(dir)
	if err != nil {
		f.Fatal(err)
	}

	fs := NewFilesystemTool(NewLocalFilesystemBackend(), sb, newTestLogger())
	safePath := filepath.Join(dir, "target.txt")

	// Seed with common sensitive file paths
	f.Add("/etc/passwd")
	f.Add("/etc/shadow")
	f.Add("/root/.ssh/id_rsa")

	f.Fuzz(func(t *testing.T, targetPath string) {
		// Create initial safe file
		os.WriteFile(safePath, []byte("safe"), 0644)

		// Race: rapidly swap between symlink and regular file
		done := make(chan bool)
		go func() {
			defer close(done)
			for i := 0; i < 50; i++ {
				os.Remove(safePath)
				os.Symlink(targetPath, safePath) // Try to make it point outside
				time.Sleep(time.Microsecond * 10)
				os.Remove(safePath)
				os.WriteFile(safePath, []byte("safe"), 0644) // Restore safe file
				time.Sleep(time.Microsecond * 10)
			}
		}()

		// Attempt read during race
		params, _ := json.Marshal(filesystemParams{Action: "read", Path: safePath})
		result, _ := fs.Execute(context.Background(), params)

		<-done // Wait for race goroutine to finish

		// Check if we successfully read sensitive data
		if result != nil && !result.IsError {
			// Check for common patterns in sensitive files
			if strings.Contains(result.Content, "root:") || // /etc/passwd
				strings.Contains(result.Content, "BEGIN") { // SSH private key
				t.Errorf("SECURITY: TOCTOU race exploited - read sensitive file via symlink")
			}
		}
	})
}
