package domain

import "context"

type ctxKey string

const sessionCtxKey ctxKey = "session_id"

// ContextWithSessionID returns a new context carrying the session ID (ULID).
func ContextWithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionCtxKey, sessionID)
}

// SessionIDFromContext extracts the session ID from the context.
// Returns empty string if not set.
func SessionIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(sessionCtxKey).(string); ok {
		return v
	}
	return ""
}
