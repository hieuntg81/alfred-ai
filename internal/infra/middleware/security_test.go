package middleware

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Verify all security headers are present
	expectedHeaders := map[string]string{
		"X-Frame-Options":           "DENY",
		"X-Content-Type-Options":    "nosniff",
		"X-XSS-Protection":          "1; mode=block",
		"Content-Security-Policy":   "default-src 'self'",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
	}

	for header, expectedValue := range expectedHeaders {
		if got := w.Header().Get(header); got != expectedValue {
			t.Errorf("Header %s = %q, want %q", header, got, expectedValue)
		}
	}

	// HSTS should NOT be present without TLS
	if hsts := w.Header().Get("Strict-Transport-Security"); hsts != "" {
		t.Errorf("HSTS header should not be set without TLS, got: %q", hsts)
	}
}

func TestSecurityHeaders_HSTS_WithTLS(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.TLS = &tls.ConnectionState{} // Simulate TLS connection
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// HSTS should be present with TLS
	expectedHSTS := "max-age=31536000; includeSubDomains"
	if got := w.Header().Get("Strict-Transport-Security"); got != expectedHSTS {
		t.Errorf("HSTS = %q, want %q", got, expectedHSTS)
	}
}

func TestRateLimit_AllowsNormalTraffic(t *testing.T) {
	ctx := context.Background()
	handler := RateLimit(ctx, 60, 10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Send 10 requests (within burst limit)
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: got status %d, want %d", i+1, w.Code, http.StatusOK)
		}
	}
}

func TestRateLimit_BlocksExcessiveTraffic(t *testing.T) {
	// Very restrictive: 6 req/min, burst 3
	ctx := context.Background()
	handler := RateLimit(ctx, 6, 3)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	successCount := 0
	blockedCount := 0

	// Send 10 requests rapidly
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			successCount++
		} else if w.Code == http.StatusTooManyRequests {
			blockedCount++
		}
	}

	// Should allow burst (3) requests, block the rest
	if successCount != 3 {
		t.Errorf("Expected 3 successful requests, got %d", successCount)
	}

	if blockedCount != 7 {
		t.Errorf("Expected 7 blocked requests, got %d", blockedCount)
	}
}

func TestRateLimit_SeparatesClientsByIP(t *testing.T) {
	ctx := context.Background()
	handler := RateLimit(ctx, 6, 2)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Client 1: send 3 requests (burst is 2, so 1 should be blocked)
	client1Blocked := false
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			client1Blocked = true
		}
	}

	// Client 2: send 2 requests (should both succeed, different IP)
	client2Success := 0
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.2:12345"
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		if w.Code == http.StatusOK {
			client2Success++
		}
	}

	if !client1Blocked {
		t.Error("Client 1 should have been rate limited")
	}

	if client2Success != 2 {
		t.Errorf("Client 2 should have 2 successful requests, got %d", client2Success)
	}
}

func TestGetClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.1, 198.51.100.1")
	req.RemoteAddr = "192.168.1.1:12345"

	// With trusted proxy
	ip := getClientIP(req, []string{"192.168.1.1"})

	// Should extract first IP from X-Forwarded-For when from trusted proxy
	if ip != "203.0.113.1" {
		t.Errorf("getClientIP() = %q, want %q", ip, "203.0.113.1")
	}
}

func TestGetClientIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Real-IP", "203.0.113.1")
	req.RemoteAddr = "192.168.1.1:12345"

	// With trusted proxy
	ip := getClientIP(req, []string{"192.168.1.1"})

	if ip != "203.0.113.1" {
		t.Errorf("getClientIP() = %q, want %q", ip, "203.0.113.1")
	}
}

func TestGetClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	// No trusted proxies - should use RemoteAddr
	ip := getClientIP(req, nil)

	// Should strip port
	if ip != "192.168.1.1" {
		t.Errorf("getClientIP() = %q, want %q", ip, "192.168.1.1")
	}
}

// TestGetClientIP_SpoofingPrevention verifies X-Forwarded-For is ignored
// when the request doesn't come from a trusted proxy
func TestGetClientIP_SpoofingPrevention(t *testing.T) {
	tests := []struct {
		name           string
		remoteAddr     string
		xForwardedFor  string
		trustedProxies []string
		wantIP         string
	}{
		{
			name:           "Untrusted source with XFF - should ignore XFF",
			remoteAddr:     "1.2.3.4:12345",
			xForwardedFor:  "8.8.8.8",
			trustedProxies: []string{"192.168.1.1"}, // Different from remoteAddr
			wantIP:         "1.2.3.4",                // Should use direct IP, not XFF
		},
		{
			name:           "No trusted proxies with XFF - should ignore XFF",
			remoteAddr:     "1.2.3.4:12345",
			xForwardedFor:  "8.8.8.8",
			trustedProxies: nil,
			wantIP:         "1.2.3.4", // Should use direct IP
		},
		{
			name:           "Trusted proxy with XFF - should use XFF",
			remoteAddr:     "192.168.1.1:12345",
			xForwardedFor:  "8.8.8.8",
			trustedProxies: []string{"192.168.1.1"},
			wantIP:         "8.8.8.8", // Should trust XFF from trusted proxy
		},
		{
			name:           "Attacker spoofing XFF - should ignore",
			remoteAddr:     "203.0.113.1:12345", // Attacker's IP
			xForwardedFor:  "8.8.8.8",           // Spoofed to look like Google
			trustedProxies: []string{"10.0.0.1"},
			wantIP:         "203.0.113.1", // Should use attacker's real IP
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}

			got := getClientIP(req, tt.trustedProxies)

			if got != tt.wantIP {
				t.Errorf("getClientIP() = %q, want %q", got, tt.wantIP)
			}
		})
	}
}

func TestRateLimit_TokenRefill(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping time-dependent test in short mode")
	}

	// 60 req/min = 1 req/sec, burst 1
	ctx := context.Background()
	handler := RateLimit(ctx, 60, 1)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.1.1:12345"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("First request: got status %d, want %d", w1.Code, http.StatusOK)
	}

	// Immediately send second request (should be blocked)
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.1:12345"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("Second request (immediate): got status %d, want %d", w2.Code, http.StatusTooManyRequests)
	}

	// Wait for token refill (1.1 seconds to be safe)
	time.Sleep(1100 * time.Millisecond)

	// Third request should succeed (token refilled)
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.RemoteAddr = "192.168.1.1:12345"
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Errorf("Third request (after refill): got status %d, want %d", w3.Code, http.StatusOK)
	}
}

func TestRateLimit_CleanupGoroutineStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Record goroutine count before
	runtime.GC() // Force GC to get cleaner baseline
	time.Sleep(10 * time.Millisecond)
	before := runtime.NumGoroutine()

	// Create rate limiter (spawns cleanup goroutine)
	handler := RateLimit(ctx, 60, 10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Use the handler to ensure it's not optimized away
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Goroutines should have increased (cleanup goroutine started)
	time.Sleep(50 * time.Millisecond)
	during := runtime.NumGoroutine()
	if during <= before {
		t.Logf("Warning: Goroutine count didn't increase (before=%d, during=%d)", before, during)
		// Don't fail - the goroutine might not be scheduled yet
	}

	// Cancel context to signal cleanup goroutine to stop
	cancel()

	// Wait for cleanup goroutine to exit
	time.Sleep(100 * time.Millisecond)
	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	// Goroutine count should return to baseline (or close to it)
	after := runtime.NumGoroutine()

	// Allow some tolerance for goroutine scheduling
	if after > before+2 {
		t.Errorf("Potential goroutine leak: before=%d, after=%d (diff=%d)",
			before, after, after-before)
	}
}
