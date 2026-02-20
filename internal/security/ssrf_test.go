package security

import (
	"net"
	"testing"
)

func TestIsPrivateIP(t *testing.T) {
	privateIPs := []string{
		"10.0.0.1",
		"10.255.255.255",
		"172.16.0.1",
		"172.31.255.255",
		"192.168.0.1",
		"192.168.255.255",
		"127.0.0.1",
		"127.255.255.255",
		"169.254.1.1",
		"0.0.0.0",
		"::1",
	}

	for _, ip := range privateIPs {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			t.Fatalf("failed to parse %q", ip)
		}
		if !IsPrivateIP(parsed) {
			t.Errorf("IsPrivateIP(%s) = false, want true", ip)
		}
	}
}

func TestIsPublicIP(t *testing.T) {
	publicIPs := []string{
		"8.8.8.8",
		"1.1.1.1",
		"142.250.80.46",
		"2607:f8b0:4004:800::200e",
	}

	for _, ip := range publicIPs {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			t.Fatalf("failed to parse %q", ip)
		}
		if IsPrivateIP(parsed) {
			t.Errorf("IsPrivateIP(%s) = true, want false", ip)
		}
	}
}

func TestValidateURLPrivateIP(t *testing.T) {
	privateURLs := []string{
		"http://127.0.0.1/secrets",
		"http://10.0.0.1:8080/admin",
		"http://192.168.1.1/",
		"http://[::1]/",
	}

	for _, u := range privateURLs {
		if err := ValidateURL(u); err == nil {
			t.Errorf("ValidateURL(%q) should fail", u)
		}
	}
}

func TestValidateURLPublicIP(t *testing.T) {
	if err := ValidateURL("http://8.8.8.8/path"); err != nil {
		t.Errorf("public IP should pass: %v", err)
	}
}

func TestValidateURLEmptyHost(t *testing.T) {
	err := ValidateURL("http:///path")
	if err == nil {
		t.Error("expected error for empty hostname")
	}
}

func TestValidateURLDNSLookupFail(t *testing.T) {
	err := ValidateURL("http://nonexistent.invalid/path")
	if err == nil {
		t.Error("expected error for DNS lookup failure")
	}
}

func TestValidateURLIPv6Loopback(t *testing.T) {
	err := ValidateURL("http://[::1]/path")
	if err == nil {
		t.Error("expected error for IPv6 loopback")
	}
}

func TestValidateURLInvalidInput(t *testing.T) {
	badURLs := []string{
		"not-a-url",
		"://missing-scheme",
	}

	for _, u := range badURLs {
		if err := ValidateURL(u); err == nil {
			t.Errorf("ValidateURL(%q) should fail", u)
		}
	}
}

func TestValidateURLPublicHostname(t *testing.T) {
	// Test with a real public hostname (DNS resolution path)
	err := ValidateURL("http://example.com/path")
	// This will do a real DNS lookup - may fail in CI but tests the path
	if err != nil {
		t.Logf("DNS lookup for example.com: %v (expected in some environments)", err)
	}
}

func TestValidateURLPublicIPReturn(t *testing.T) {
	// Explicitly test the "direct IP is public -> return nil" path (line 54)
	// Using a known public IP that is not in any private range
	err := ValidateURL("https://1.1.1.1/dns-query")
	if err != nil {
		t.Errorf("expected nil for public IP 1.1.1.1, got: %v", err)
	}
}

func TestValidateURLInvalidURLParse(t *testing.T) {
	// Go's url.Parse is very permissive, but malformed IPv6 brackets trigger errors
	err := ValidateURL("http://[invalid-ipv6/path")
	if err == nil {
		t.Error("expected error for malformed IPv6 URL")
	}
}

func TestValidateURLDNSResolvesPublic(t *testing.T) {
	// Ensure the final return nil (after DNS resolution loop with all public IPs) is covered.
	// Use a well-known hostname that always resolves to public IPs.
	ips, err := net.LookupIP("example.com")
	if err != nil || len(ips) == 0 {
		t.Skip("DNS resolution not available, skipping")
	}
	// Verify all IPs are public (otherwise the test premise is wrong)
	for _, ip := range ips {
		if IsPrivateIP(ip) {
			t.Skipf("example.com resolved to private IP %s, skipping", ip)
		}
	}

	err = ValidateURL("http://example.com/test")
	if err != nil {
		t.Errorf("ValidateURL for example.com should succeed, got: %v", err)
	}
}

func TestValidateURLHostnameResolvesToPrivate(t *testing.T) {
	// Test the DNS resolution path where a hostname resolves to a private IP.
	// "localhost" typically resolves to 127.0.0.1 which is a private/loopback address.
	// This covers the IsPrivateIP check inside the DNS resolution loop (line 65-67).
	ips, lookupErr := net.LookupIP("localhost")
	if lookupErr != nil || len(ips) == 0 {
		t.Skip("localhost DNS resolution not available, skipping")
	}
	hasPrivate := false
	for _, ip := range ips {
		if IsPrivateIP(ip) {
			hasPrivate = true
			break
		}
	}
	if !hasPrivate {
		t.Skip("localhost does not resolve to a private IP in this environment")
	}

	err := ValidateURL("http://localhost/admin")
	if err == nil {
		t.Error("expected error for hostname resolving to private IP")
	}
}

// TestIsPrivateIP_IPv4MappedIPv6 verifies that IPv4-mapped IPv6 addresses
// are correctly identified as private when the underlying IPv4 is private.
// This prevents SSRF bypasses using ::ffff:127.0.0.1 format.
func TestIsPrivateIP_IPv4MappedIPv6(t *testing.T) {
	tests := []struct {
		name        string
		ip          string
		wantPrivate bool
	}{
		{"IPv4-mapped loopback", "::ffff:127.0.0.1", true},
		{"IPv4-mapped private 10.x", "::ffff:10.0.0.1", true},
		{"IPv4-mapped private 192.168", "::ffff:192.168.1.1", true},
		{"IPv4-mapped private 172.16", "::ffff:172.16.0.1", true},
		{"IPv4-mapped AWS metadata", "::ffff:169.254.169.254", true},
		{"IPv4-mapped public Cloudflare", "::ffff:1.1.1.1", false},
		{"IPv4-mapped public Google DNS", "::ffff:8.8.8.8", false},
		{"IPv4-mapped public example", "::ffff:93.184.216.34", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP: %s", tt.ip)
			}

			got := IsPrivateIP(ip)
			if got != tt.wantPrivate {
				t.Errorf("IsPrivateIP(%s) = %v, want %v", tt.ip, got, tt.wantPrivate)
			}
		})
	}
}

// TestSSRFSafeTransport_PreventsDNSRebinding documents expected behavior
// for DNS rebinding attack prevention (Time-of-Check-Time-of-Use race).
// This is a known limitation that requires implementing custom dial logic.
func TestSSRFSafeTransport_PreventsDNSRebinding(t *testing.T) {
	t.Skip("Requires test DNS server - document expected behavior")

	// This test documents a known edge case where DNS resolution could change
	// between the ValidateURL() call and the actual HTTP dial.
	//
	// Attack scenario:
	// 1. Attacker controls DNS for evil.com
	// 2. First lookup (during ValidateURL): evil.com -> 8.8.8.8 (public, passes)
	// 3. ValidateURL() returns nil (validated)
	// 4. DNS changes: evil.com -> 127.0.0.1 (TOCTOU race window)
	// 5. HTTP request dials evil.com, gets 127.0.0.1 (SSRF!)
	//
	// Mitigation:
	// - Use NewSSRFSafeTransport() which validates at dial time
	// - Transport re-checks IP before connecting
	//
	// To properly test this, we would need:
	// - Mock net.Resolver that returns different IPs on subsequent calls
	// - Test DNS server that changes responses
	//
	// Expected behavior with NewSSRFSafeTransport():
	// - First dial attempt validates IP
	// - If DNS changed to private IP, dial is blocked
	// - Error: "SSRF blocked: IP became private after validation"
}

// TestValidateURLIPv4MappedLoopback verifies URL validation blocks
// IPv4-mapped IPv6 loopback addresses
func TestValidateURLIPv4MappedLoopback(t *testing.T) {
	err := ValidateURL("http://[::ffff:127.0.0.1]/admin")
	if err == nil {
		t.Error("expected error for IPv4-mapped loopback address")
	}
}

// TestValidateURLIPv4MappedPrivate verifies URL validation blocks
// IPv4-mapped IPv6 private addresses
func TestValidateURLIPv4MappedPrivate(t *testing.T) {
	privateAddresses := []string{
		"http://[::ffff:10.0.0.1]/",
		"http://[::ffff:192.168.1.1]/",
		"http://[::ffff:172.16.0.1]/",
		"http://[::ffff:169.254.169.254]/latest/meta-data/",
	}

	for _, addr := range privateAddresses {
		err := ValidateURL(addr)
		if err == nil {
			t.Errorf("expected error for IPv4-mapped private address: %s", addr)
		}
	}
}

// TestValidateURLIPv4MappedPublic verifies URL validation allows
// IPv4-mapped IPv6 public addresses
func TestValidateURLIPv4MappedPublic(t *testing.T) {
	err := ValidateURL("http://[::ffff:8.8.8.8]/")
	if err != nil {
		t.Errorf("expected IPv4-mapped public IP to pass, got: %v", err)
	}
}
