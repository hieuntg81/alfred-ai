package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"alfred-ai/internal/security"
)

// FuzzWebTool fuzzes web fetch to find SSRF bypass and header injection vulnerabilities.
// Targets SSRF protection (private IP blocking) and header validation (CRLF injection).
func FuzzWebTool(f *testing.F) {
	web := NewWebTool(newTestLogger())

	// Seed corpus - SSRF attack patterns
	f.Add(`{"url":"http://example.com"}`)                                                // Valid baseline
	f.Add(`{"url":"http://127.0.0.1/admin"}`)                                            // Localhost
	f.Add(`{"url":"http://[::1]/internal"}`)                                             // IPv6 localhost
	f.Add(`{"url":"http://169.254.169.254/latest/meta-data"}`)                           // AWS metadata
	f.Add(`{"url":"http://0x7f000001/"}`)                                                // Hex IP
	f.Add(`{"url":"http://2130706433/"}`)                                                // Integer IP (127.0.0.1)
	f.Add(`{"url":"http://[::ffff:127.0.0.1]/"}`)                                        // IPv4-mapped IPv6
	f.Add(`{"url":"file:///etc/passwd"}`)                                                // File scheme
	f.Add(`{"url":"http://evil.com","headers":{"Host":"internal\r\nX-Injected: true"}}`) // Header injection
	f.Add(`{"url":"http://example.com","method":"POST"}`)                                // Invalid method
	f.Add(`{"url":"http://","method":"GET"}`)                                            // Malformed URL

	f.Fuzz(func(t *testing.T, input string) {
		result, err := web.Execute(context.Background(), json.RawMessage(input))

		if err == nil && result != nil && !result.IsError {
			var params webParams
			if json.Unmarshal([]byte(input), &params) == nil {
				// Invariant 1: URL must not target private IPs (SSRF protection)
				if err := security.ValidateURL(params.URL); err != nil {
					t.Errorf("SECURITY: SSRF bypass - URL %q passed validation", params.URL)
				}

				// Invariant 2: Method must be GET or HEAD only
				if params.Method != "" && params.Method != "GET" && params.Method != "HEAD" {
					t.Errorf("SECURITY: Invalid HTTP method %q allowed", params.Method)
				}

				// Invariant 3: Headers should not contain CRLF (prevents header injection)
				for k, v := range params.Headers {
					if strings.ContainsAny(k, "\r\n") || strings.ContainsAny(v, "\r\n") {
						t.Errorf("SECURITY: CRLF injection in header %q: %q", k, v)
					}
				}
			}
		}
	})
}
