package tool

import (
	"errors"
	"strings"

	"alfred-ai/internal/domain"
)

// retryableSentinels lists domain errors that indicate transient failures
// worth retrying. These typically represent backend/network issues that
// resolve on their own.
var retryableSentinels = []error{
	// Node system (gRPC).
	domain.ErrNodeUnreachable,
	domain.ErrNodeInvoke,

	// Category sentinels — catch all subsystem timeouts and provider errors.
	domain.ErrTimeout,
	domain.ErrProviderError,

	// Rate limiting / context overflow.
	domain.ErrRateLimit,
	domain.ErrContextOverflow,
}

// retryablePatterns are substrings in error messages that indicate transient failures.
// Checked case-insensitively.
var retryablePatterns = []string{
	"connection refused",
	"connection reset",
	"no such host",
	"timeout",
	"deadline exceeded",
	"temporarily unavailable",
	"service unavailable",
	"try again",
	"unavailable",        // gRPC UNAVAILABLE
	"resourceexhausted",  // gRPC RESOURCE_EXHAUSTED (case-insensitive, no separator)
}

// classifyToolError returns true if the error is transient and the tool call
// may succeed on retry. Returns false for nil, permanent, or unknown errors.
func classifyToolError(err error) bool {
	if err == nil {
		return false
	}

	// Check domain sentinels via errors.Is (handles wrapped errors).
	for _, sentinel := range retryableSentinels {
		if errors.Is(err, sentinel) {
			return true
		}
	}

	// String-based fallback for errors without sentinel wrapping.
	lower := strings.ToLower(err.Error())
	for _, p := range retryablePatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}

	return false
}
