package gateway

import (
	"crypto/subtle"

	"alfred-ai/internal/domain"
)

// ClientInfo holds metadata about an authenticated gateway client.
type ClientInfo struct {
	Name     string
	Roles    []string
	TenantID string // set from WS connect query param, empty = default tenant
}

// Authenticator validates incoming gateway connections.
type Authenticator interface {
	Authenticate(token string) (*ClientInfo, error)
}

type authEntry struct {
	token []byte
	info  *ClientInfo
}

// StaticTokenAuth authenticates clients against a static token list
// using constant-time comparison to prevent timing attacks.
type StaticTokenAuth struct {
	entries []authEntry
}

// NewStaticTokenAuth builds an authenticator from a set of token entries.
func NewStaticTokenAuth(entries []struct {
	Token string
	Name  string
	Roles []string
}) *StaticTokenAuth {
	a := &StaticTokenAuth{
		entries: make([]authEntry, len(entries)),
	}
	for i, e := range entries {
		a.entries[i] = authEntry{
			token: []byte(e.Token),
			info:  &ClientInfo{Name: e.Name, Roles: e.Roles},
		}
	}
	return a
}

// Authenticate returns client info if the token is valid.
// Uses constant-time comparison to prevent timing attacks.
func (s *StaticTokenAuth) Authenticate(token string) (*ClientInfo, error) {
	tokenBytes := []byte(token)
	for _, e := range s.entries {
		if subtle.ConstantTimeCompare(tokenBytes, e.token) == 1 {
			return e.info, nil
		}
	}
	return nil, domain.ErrGatewayAuthFailed
}
