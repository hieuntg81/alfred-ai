package usecase

import (
	"errors"
	"fmt"
	"testing"

	"alfred-ai/internal/domain"
)

func TestClassifyNilError(t *testing.T) {
	c := NewErrorClassifier()
	got := c.Classify(nil)
	if got.Category != ErrorCategoryUnknown {
		t.Errorf("Category = %d, want Unknown", got.Category)
	}
	if got.Original != nil {
		t.Errorf("Original = %v, want nil", got.Original)
	}
}

func TestClassifyRateLimit429(t *testing.T) {
	c := NewErrorClassifier()
	err := fmt.Errorf("API error 429: rate limit exceeded")
	got := c.Classify(err)

	if got.Category != ErrorCategoryRetryable {
		t.Errorf("Category = %d, want Retryable", got.Category)
	}
	if !errors.Is(got.Sentinel, domain.ErrRateLimit) {
		t.Errorf("Sentinel = %v, want ErrRateLimit", got.Sentinel)
	}
	if got.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429", got.StatusCode)
	}
}

func TestClassifyAuth401(t *testing.T) {
	c := NewErrorClassifier()
	err := fmt.Errorf("API error 401: unauthorized")
	got := c.Classify(err)

	if got.Category != ErrorCategoryPermanent {
		t.Errorf("Category = %d, want Permanent", got.Category)
	}
	if !errors.Is(got.Sentinel, domain.ErrAuthInvalid) {
		t.Errorf("Sentinel = %v, want ErrAuthInvalid", got.Sentinel)
	}
}

func TestClassifyAuth403(t *testing.T) {
	c := NewErrorClassifier()
	err := fmt.Errorf("API error 403: forbidden")
	got := c.Classify(err)

	if got.Category != ErrorCategoryPermanent {
		t.Errorf("Category = %d, want Permanent", got.Category)
	}
	if !errors.Is(got.Sentinel, domain.ErrAuthInvalid) {
		t.Errorf("Sentinel = %v, want ErrAuthInvalid", got.Sentinel)
	}
	if got.StatusCode != 403 {
		t.Errorf("StatusCode = %d, want 403", got.StatusCode)
	}
}

func TestClassifyContextOverflow400(t *testing.T) {
	c := NewErrorClassifier()
	err := fmt.Errorf("API error 400: This request would exceed the context length limit")
	got := c.Classify(err)

	if got.Category != ErrorCategoryRetryable {
		t.Errorf("Category = %d, want Retryable", got.Category)
	}
	if !errors.Is(got.Sentinel, domain.ErrContextOverflow) {
		t.Errorf("Sentinel = %v, want ErrContextOverflow", got.Sentinel)
	}
}

func TestClassifyBadRequest400(t *testing.T) {
	c := NewErrorClassifier()
	err := fmt.Errorf("API error 400: invalid json in request body")
	got := c.Classify(err)

	if got.Category != ErrorCategoryPermanent {
		t.Errorf("Category = %d, want Permanent", got.Category)
	}
	if got.Sentinel != nil {
		t.Errorf("Sentinel = %v, want nil", got.Sentinel)
	}
}

func TestClassifyServerError500(t *testing.T) {
	c := NewErrorClassifier()
	err := fmt.Errorf("API error 500: internal server error")
	got := c.Classify(err)

	if got.Category != ErrorCategoryRetryable {
		t.Errorf("Category = %d, want Retryable", got.Category)
	}
	if got.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", got.StatusCode)
	}
}

func TestClassifyServerError503(t *testing.T) {
	c := NewErrorClassifier()
	err := fmt.Errorf("API error 503: service unavailable")
	got := c.Classify(err)

	if got.Category != ErrorCategoryRetryable {
		t.Errorf("Category = %d, want Retryable", got.Category)
	}
}

func TestClassifyConnectionRefused(t *testing.T) {
	c := NewErrorClassifier()
	err := fmt.Errorf("http request: dial tcp 127.0.0.1:8080: connection refused")
	got := c.Classify(err)

	if got.Category != ErrorCategoryRetryable {
		t.Errorf("Category = %d, want Retryable", got.Category)
	}
}

func TestClassifyTimeout(t *testing.T) {
	c := NewErrorClassifier()
	err := fmt.Errorf("http request: context deadline exceeded")
	got := c.Classify(err)

	if got.Category != ErrorCategoryRetryable {
		t.Errorf("Category = %d, want Retryable", got.Category)
	}
}

func TestClassifyUnknownError(t *testing.T) {
	c := NewErrorClassifier()
	err := fmt.Errorf("something completely unexpected happened")
	got := c.Classify(err)

	if got.Category != ErrorCategoryUnknown {
		t.Errorf("Category = %d, want Unknown", got.Category)
	}
}

func TestClassifyRateLimitString(t *testing.T) {
	c := NewErrorClassifier()
	err := fmt.Errorf("too many requests, please slow down")
	got := c.Classify(err)

	if got.Category != ErrorCategoryRetryable {
		t.Errorf("Category = %d, want Retryable", got.Category)
	}
	if !errors.Is(got.Sentinel, domain.ErrRateLimit) {
		t.Errorf("Sentinel = %v, want ErrRateLimit", got.Sentinel)
	}
}
