package middleware

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// SecurityHeaders adds OWASP-recommended security headers to all responses
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Anti-clickjacking: prevent embedding in iframes
		w.Header().Set("X-Frame-Options", "DENY")

		// Prevent MIME sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// XSS protection (legacy but still useful for older browsers)
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Content Security Policy: restrict resource loading
		w.Header().Set("Content-Security-Policy", "default-src 'self'")

		// HSTS: enforce HTTPS (only if using TLS)
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security",
				"max-age=31536000; includeSubDomains")
		}

		// Referrer policy: control referrer information
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		next.ServeHTTP(w, r)
	})
}

// RateLimitConfig holds configuration for the rate limiter
type RateLimitConfig struct {
	RequestsPerMin int      // Maximum requests allowed per minute
	BurstSize      int      // Maximum burst of requests allowed
	TrustedProxies []string // List of trusted proxy IPs (for X-Forwarded-For)
}

// RateLimit implements token bucket rate limiting per client IP
// ctx: context for goroutine lifecycle management
// requestsPerMin: maximum requests allowed per minute
// burstSize: maximum burst of requests allowed
func RateLimit(ctx context.Context, requestsPerMin, burstSize int) func(http.Handler) http.Handler {
	return RateLimitWithConfig(ctx, RateLimitConfig{
		RequestsPerMin: requestsPerMin,
		BurstSize:      burstSize,
		TrustedProxies: nil, // Default: don't trust proxy headers
	})
}

// RateLimitWithConfig implements token bucket rate limiting with trusted proxy support
// This prevents X-Forwarded-For spoofing by only trusting proxy headers from known sources
//
// Security Model:
//   - Default (no trusted proxies): X-Forwarded-For is IGNORED, uses direct connection IP
//   - With trusted proxies: X-Forwarded-For is trusted ONLY from configured proxy IPs
//   - This prevents attackers from spoofing their IP address to bypass rate limiting
//
// Example usage:
//
//	// Default: no trusted proxies (most secure)
//	middleware.RateLimit(ctx, 100, 20)
//
//	// Behind trusted proxy/load balancer:
//	middleware.RateLimitWithConfig(ctx, RateLimitConfig{
//	    RequestsPerMin: 100,
//	    BurstSize: 20,
//	    TrustedProxies: []string{"10.0.0.1", "10.0.0.2"}, // Your proxy IPs
//	})
//
// For Kubernetes/cloud deployments, set TrustedProxies to your ingress controller IPs
func RateLimitWithConfig(ctx context.Context, cfg RateLimitConfig) func(http.Handler) http.Handler {
	type client struct {
		limiter  *rate.Limiter
		lastSeen time.Time
	}

	clients := make(map[string]*client)
	mu := &sync.Mutex{}

	// Cleanup goroutine: remove stale client entries
	// Uses ticker and context cancellation for proper lifecycle management
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				mu.Lock()
				for ip, c := range clients {
					if time.Since(c.lastSeen) > 3*time.Minute {
						delete(clients, ip)
					}
				}
				mu.Unlock()
			case <-ctx.Done():
				// Graceful shutdown: cleanup goroutine stops
				return
			}
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIP(r, cfg.TrustedProxies)

			mu.Lock()
			if _, exists := clients[ip]; !exists {
				// Create limiter: requestsPerMin spread over 60 seconds
				clients[ip] = &client{
					limiter: rate.NewLimiter(rate.Limit(cfg.RequestsPerMin)/60.0, cfg.BurstSize),
				}
			}
			clients[ip].lastSeen = time.Now()
			limiter := clients[ip].limiter
			mu.Unlock()

			if !limiter.Allow() {
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// getClientIP extracts the client IP from the request
// Only trusts X-Forwarded-For and X-Real-IP headers if the direct connection
// comes from a trusted proxy. This prevents X-Forwarded-For spoofing attacks.
//
// trustedProxies: list of IP addresses of trusted proxies/load balancers
// If nil or empty, proxy headers are ignored (secure default)
func getClientIP(r *http.Request, trustedProxies []string) string {
	// Extract direct connection IP (the actual TCP peer)
	directIP := r.RemoteAddr
	// Strip port if present
	if idx := strings.LastIndex(directIP, ":"); idx > 0 {
		directIP = directIP[:idx]
	}

	// If no trusted proxies configured, use direct IP only (secure default)
	if len(trustedProxies) == 0 {
		return directIP
	}

	// Check if the direct connection is from a trusted proxy
	isTrustedProxy := false
	for _, trustedIP := range trustedProxies {
		if directIP == trustedIP {
			isTrustedProxy = true
			break
		}
	}

	// Only trust proxy headers if connected from trusted source
	if !isTrustedProxy {
		// Not from trusted proxy - use direct IP to prevent spoofing
		return directIP
	}

	// Trusted proxy - extract real client IP from headers
	// Try X-Forwarded-For first (standard header for proxy chains)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take first IP in the list (original client)
		if idx := strings.Index(xff, ","); idx > 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Try X-Real-IP (some proxies use this)
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// No proxy headers - fall back to direct IP
	return directIP
}
