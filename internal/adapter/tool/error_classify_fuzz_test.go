package tool

import (
	"errors"
	"testing"
)

func FuzzClassifyToolError(f *testing.F) {
	// Seed corpus: retryable patterns, permanent patterns, empty, garbage.
	seeds := []string{
		"connection refused",
		"connection reset by peer",
		"no such host",
		"context deadline exceeded",
		"service unavailable",
		"resource temporarily unavailable",
		"try again later",
		"rpc error: code = Unavailable",
		"rpc error: code = ResourceExhausted",
		"timeout",
		"permission denied",
		"not found",
		"already exists",
		"invalid argument",
		"",
		"completely random error",
		"camera device not found on node",
		"dial tcp 10.0.0.1:50051: connection refused",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, msg string) {
		// Must not panic regardless of input.
		_ = classifyToolError(errors.New(msg))
	})
}
