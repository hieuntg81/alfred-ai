package security

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"alfred-ai/internal/domain"
)

// privateRanges lists all private/reserved CIDR blocks to block for SSRF.
var privateRanges = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"0.0.0.0/8",
	"::1/128",
	"fc00::/7",
	"fe80::/10",
}

var parsedRanges []*net.IPNet

func init() {
	for _, cidr := range privateRanges {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("invalid CIDR %q: %v", cidr, err))
		}
		parsedRanges = append(parsedRanges, ipnet)
	}
}

// ValidateURL checks that a URL does not resolve to a private/reserved IP.
func ValidateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return domain.NewDomainError("ValidateURL", domain.ErrSSRFBlocked, fmt.Sprintf("invalid URL: %v", err))
	}

	// Whitelist only HTTP and HTTPS schemes
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		// OK - continue validation
	case "":
		return domain.NewDomainError(
			"ValidateURL",
			domain.ErrSSRFBlocked,
			"missing URL scheme, only http/https allowed",
		)
	default:
		return domain.NewDomainError(
			"ValidateURL",
			domain.ErrSSRFBlocked,
			fmt.Sprintf("scheme %q not allowed, only http/https", u.Scheme),
		)
	}

	host := u.Hostname()
	if host == "" {
		return domain.NewDomainError("ValidateURL", domain.ErrSSRFBlocked, "empty hostname")
	}

	// Try direct IP parse first
	if ip := net.ParseIP(host); ip != nil {
		if IsPrivateIP(ip) {
			return domain.NewDomainError("ValidateURL", domain.ErrSSRFBlocked,
				fmt.Sprintf("IP %s is private/reserved", ip))
		}
		return nil
	}

	// DNS resolution
	ips, err := net.LookupIP(host)
	if err != nil {
		return domain.NewDomainError("ValidateURL", domain.ErrSSRFBlocked,
			fmt.Sprintf("DNS lookup failed: %v", err))
	}

	for _, ip := range ips {
		if IsPrivateIP(ip) {
			return domain.NewDomainError("ValidateURL", domain.ErrSSRFBlocked,
				fmt.Sprintf("host %s resolves to private IP %s", host, ip))
		}
	}

	return nil
}

// IsPrivateIP checks if an IP falls within any private/reserved range.
func IsPrivateIP(ip net.IP) bool {
	// Normalize IPv4-mapped IPv6 to IPv4
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}

	for _, ipnet := range parsedRanges {
		if ipnet.Contains(ip) {
			return true
		}
	}
	return false
}

// NewSSRFSafeTransport creates an HTTP transport that prevents DNS rebinding attacks
// by validating IPs at dial time and connecting directly to the validated IP.
// This prevents TOCTOU vulnerabilities where DNS resolution could change between
// validation and actual connection.
func NewSSRFSafeTransport() *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid address: %w", err)
			}

			// Resolve DNS once
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, domain.NewDomainError(
					"SSRFSafeTransport.Dial",
					err,
					fmt.Sprintf("DNS lookup failed for %s", host),
				)
			}

			if len(ips) == 0 {
				return nil, domain.NewDomainError(
					"SSRFSafeTransport.Dial",
					fmt.Errorf("no IPs resolved"),
					host,
				)
			}

			// Validate ALL resolved IPs
			for _, ip := range ips {
				normalized := ip.IP
				// Handle IPv4-mapped IPv6
				if v4 := normalized.To4(); v4 != nil {
					normalized = v4
				}
				if IsPrivateIP(normalized) {
					return nil, domain.NewDomainError(
						"SSRFSafeTransport.Dial",
						domain.ErrSSRFBlocked,
						fmt.Sprintf("%s resolves to private IP %s", host, ip.IP),
					)
				}
			}

			// Connect directly to first validated IP (no second DNS lookup)
			dialer := &net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}
			return dialer.DialContext(ctx, network,
				net.JoinHostPort(ips[0].IP.String(), port))
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}
