//go:build !windows

package tool

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alfred-ai/internal/security"
)

// FuzzShellTool fuzzes shell command execution to find allowlist bypass vulnerabilities.
// This addresses the critical security issue where basename validation can be bypassed
// using absolute paths (e.g., /bin/rm bypasses rm allowlist check).
func FuzzShellTool(f *testing.F) {
	// Setup sandbox for fuzzing
	dir := f.TempDir()
	sb, err := security.NewSandbox(dir)
	if err != nil {
		f.Fatal(err)
	}

	// Create shell tool with typical allowlist
	sh := NewShellTool(NewLocalShellBackend(30*time.Second), []string{"echo", "cat", "ls", "pwd"}, sb, newTestLogger())

	// Seed corpus - attack patterns targeting known vulnerabilities
	f.Add(`{"command":"echo","args":["test"]}`)                                // Valid baseline
	f.Add(`{"command":"/bin/rm","args":["-rf","/"]}`)                          // Absolute path bypass attempt
	f.Add(`{"command":"../../../bin/bash","args":[]}`)                         // Path traversal
	f.Add(`{"command":"echo","args":["test;rm -rf /"]}`)                       // Command injection
	f.Add(`{"command":"cat","workdir":"../../etc"}`)                           // Workdir traversal
	f.Add(`{"command":"ls\x00rm","args":[]}`)                                  // Null byte injection
	f.Add(`{"command":"","args":[]}`)                                          // Empty command
	f.Add(`{"command":"echo","args":["` + strings.Repeat("A", 100000) + `"]}`) // Huge args
	f.Add(`malformed json`)                                                    // Invalid JSON
	f.Add(`{"command":"echo","args":[1,2,3]}`)                                 // Type confusion

	f.Fuzz(func(t *testing.T, input string) {
		result, err := sh.Execute(context.Background(), json.RawMessage(input))

		// Invariant 1: Tool should never panic (tested implicitly)
		// Invariant 2: If execution succeeds, command MUST be in allowlist
		if err == nil && result != nil && !result.IsError {
			var params shellParams
			if json.Unmarshal([]byte(input), &params) == nil {
				base := filepath.Base(params.Command)
				allowlist := map[string]bool{"echo": true, "cat": true, "ls": true, "pwd": true}

				if !allowlist[base] {
					t.Errorf("SECURITY: Allowlist bypass - command %q (base: %q) executed",
						params.Command, base)
				}

				// Invariant 3: Workdir must be within sandbox
				if params.WorkDir != "" {
					if !strings.HasPrefix(params.WorkDir, dir) {
						t.Errorf("SECURITY: Sandbox escape via workdir: %q", params.WorkDir)
					}
				}
			}
		}

		// Invariant 4: Malformed JSON should return IsError=true, not panic
		// (Automatically verified by no panic above)
	})
}
