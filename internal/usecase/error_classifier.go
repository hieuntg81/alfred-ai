package usecase

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"alfred-ai/internal/domain"
)

// ErrorCategory indicates whether an error is retryable or permanent.
type ErrorCategory int

const (
	ErrorCategoryUnknown   ErrorCategory = iota
	ErrorCategoryRetryable               // 429, 5xx, connection errors, context overflow
	ErrorCategoryPermanent               // 401, 403, 400 (non-overflow), malformed
)

// ClassifiedError holds the result of error classification.
type ClassifiedError struct {
	Original   error
	Category   ErrorCategory
	Sentinel   error // mapped domain sentinel (e.g. domain.ErrRateLimit), or nil
	StatusCode int   // extracted HTTP status, or 0 if unknown
}

// ErrorClassifier analyzes LLM provider errors and categorizes them.
type ErrorClassifier struct{}

// NewErrorClassifier creates a new classifier.
func NewErrorClassifier() *ErrorClassifier {
	return &ErrorClassifier{}
}

// apiErrorPattern matches "API error <status_code>:" produced by all LLM providers.
var apiErrorPattern = regexp.MustCompile(`API error (\d+):`)

// contextOverflowKeywords are body keywords that indicate a context length issue
// within a 400 response.
var contextOverflowKeywords = []string{
	"context", "token", "length", "too long", "maximum",
}

// Classify inspects an error (typically from an LLM provider) and returns
// a ClassifiedError with category and mapped sentinel.
func (c *ErrorClassifier) Classify(err error) ClassifiedError {
	if err == nil {
		return ClassifiedError{}
	}

	// Check wrapped domain sentinels first (from mapHTTPError).
	if sentinel := c.classifyBySentinel(err); sentinel.Category != ErrorCategoryUnknown {
		return sentinel
	}

	errStr := err.Error()

	// Try to extract HTTP status code from "API error NNN:" pattern.
	if matches := apiErrorPattern.FindStringSubmatch(errStr); len(matches) == 2 {
		code, _ := strconv.Atoi(matches[1])
		return c.classifyByStatus(err, code, errStr)
	}

	// String-based fallback for non-API errors (network, timeout, etc.).
	return c.classifyByString(err, errStr)
}

// classifyBySentinel checks if the error wraps a known domain sentinel.
func (c *ErrorClassifier) classifyBySentinel(err error) ClassifiedError {
	switch {
	case errors.Is(err, domain.ErrRateLimit):
		return ClassifiedError{
			Original: err, Category: ErrorCategoryRetryable,
			Sentinel: domain.ErrRateLimit,
		}
	case errors.Is(err, domain.ErrContextOverflow):
		return ClassifiedError{
			Original: err, Category: ErrorCategoryRetryable,
			Sentinel: domain.ErrContextOverflow,
		}
	case errors.Is(err, domain.ErrAuthInvalid):
		return ClassifiedError{
			Original: err, Category: ErrorCategoryPermanent,
			Sentinel: domain.ErrAuthInvalid,
		}
	default:
		return ClassifiedError{Original: err, Category: ErrorCategoryUnknown}
	}
}

func (c *ErrorClassifier) classifyByStatus(err error, code int, body string) ClassifiedError {
	switch {
	case code == 429:
		return ClassifiedError{
			Original: err, Category: ErrorCategoryRetryable,
			Sentinel: domain.ErrRateLimit, StatusCode: code,
		}
	case code == 401 || code == 403:
		return ClassifiedError{
			Original: err, Category: ErrorCategoryPermanent,
			Sentinel: domain.ErrAuthInvalid, StatusCode: code,
		}
	case code == 413:
		return ClassifiedError{
			Original: err, Category: ErrorCategoryRetryable,
			Sentinel: domain.ErrContextOverflow, StatusCode: code,
		}
	case code == 400:
		lower := strings.ToLower(body)
		for _, kw := range contextOverflowKeywords {
			if strings.Contains(lower, kw) {
				return ClassifiedError{
					Original: err, Category: ErrorCategoryRetryable,
					Sentinel: domain.ErrContextOverflow, StatusCode: code,
				}
			}
		}
		return ClassifiedError{
			Original: err, Category: ErrorCategoryPermanent, StatusCode: code,
		}
	case code >= 500 && code < 600:
		return ClassifiedError{
			Original: err, Category: ErrorCategoryRetryable, StatusCode: code,
		}
	default:
		return ClassifiedError{
			Original: err, Category: ErrorCategoryPermanent, StatusCode: code,
		}
	}
}

func (c *ErrorClassifier) classifyByString(err error, errStr string) ClassifiedError {
	lower := strings.ToLower(errStr)

	// Rate limit patterns.
	for _, p := range []string{"rate limit", "too many requests"} {
		if strings.Contains(lower, p) {
			return ClassifiedError{
				Original: err, Category: ErrorCategoryRetryable,
				Sentinel: domain.ErrRateLimit,
			}
		}
	}

	// Context overflow patterns.
	for _, p := range []string{"context length", "token limit", "maximum context"} {
		if strings.Contains(lower, p) {
			return ClassifiedError{
				Original: err, Category: ErrorCategoryRetryable,
				Sentinel: domain.ErrContextOverflow,
			}
		}
	}

	// Transient network / timeout patterns.
	for _, p := range []string{
		"connection refused", "no such host", "timeout",
		"deadline exceeded", "connection reset",
	} {
		if strings.Contains(lower, p) {
			return ClassifiedError{
				Original: err, Category: ErrorCategoryRetryable,
			}
		}
	}

	return ClassifiedError{Original: err, Category: ErrorCategoryUnknown}
}
