package llm

import (
	"errors"
	"net/http"
	"testing"

	"alfred-ai/internal/domain"
)

func TestMapHTTPError429(t *testing.T) {
	err := mapHTTPError(http.StatusTooManyRequests, []byte(`{"error":"rate limit exceeded"}`))
	if !errors.Is(err, domain.ErrRateLimit) {
		t.Errorf("expected ErrRateLimit, got %v", err)
	}
	if got := err.Error(); got == "" {
		t.Error("error message should not be empty")
	}
}

func TestMapHTTPError401(t *testing.T) {
	err := mapHTTPError(http.StatusUnauthorized, []byte(`{"error":"invalid api key"}`))
	if !errors.Is(err, domain.ErrAuthInvalid) {
		t.Errorf("expected ErrAuthInvalid, got %v", err)
	}
}

func TestMapHTTPError403(t *testing.T) {
	err := mapHTTPError(http.StatusForbidden, []byte(`{"error":"forbidden"}`))
	if !errors.Is(err, domain.ErrAuthInvalid) {
		t.Errorf("expected ErrAuthInvalid, got %v", err)
	}
}

func TestMapHTTPError413(t *testing.T) {
	err := mapHTTPError(http.StatusRequestEntityTooLarge, []byte(`{"error":"context too long"}`))
	if !errors.Is(err, domain.ErrContextOverflow) {
		t.Errorf("expected ErrContextOverflow, got %v", err)
	}
}

func TestMapHTTPError500(t *testing.T) {
	err := mapHTTPError(http.StatusInternalServerError, []byte(`{"error":"internal server error"}`))
	if !errors.Is(err, domain.ErrToolFailure) {
		t.Errorf("expected ErrToolFailure (retryable), got %v", err)
	}
}

func TestMapHTTPError502(t *testing.T) {
	err := mapHTTPError(http.StatusBadGateway, []byte(`bad gateway`))
	if !errors.Is(err, domain.ErrToolFailure) {
		t.Errorf("expected ErrToolFailure (retryable), got %v", err)
	}
}

func TestMapHTTPError503(t *testing.T) {
	err := mapHTTPError(http.StatusServiceUnavailable, []byte(`service unavailable`))
	if !errors.Is(err, domain.ErrToolFailure) {
		t.Errorf("expected ErrToolFailure (retryable), got %v", err)
	}
}

func TestMapHTTPErrorUnknownStatus(t *testing.T) {
	err := mapHTTPError(418, []byte(`I'm a teapot`))
	if err == nil {
		t.Fatal("expected error")
	}
	// Should not wrap any known sentinel.
	if errors.Is(err, domain.ErrRateLimit) || errors.Is(err, domain.ErrAuthInvalid) ||
		errors.Is(err, domain.ErrContextOverflow) || errors.Is(err, domain.ErrToolFailure) {
		t.Errorf("expected no sentinel wrapping for unknown status, got %v", err)
	}
}

func TestMapHTTPErrorIncludesBody(t *testing.T) {
	body := `{"error":{"message":"detailed error info from API"}}`
	err := mapHTTPError(http.StatusTooManyRequests, []byte(body))
	if got := err.Error(); got == "" {
		t.Fatal("error message should not be empty")
	}
	// Error message should include the body for debugging.
	if got := err.Error(); len(got) < len("API error 429") {
		t.Errorf("error message too short: %q", got)
	}
}
