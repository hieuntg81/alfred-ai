package tool

import (
	"errors"
	"fmt"
	"testing"

	"alfred-ai/internal/domain"
)

func TestClassifyToolError_Nil(t *testing.T) {
	if classifyToolError(nil) {
		t.Error("expected nil error to be non-retryable")
	}
}

func TestClassifyToolError_RetryableSentinels(t *testing.T) {
	sentinels := []struct {
		name     string
		sentinel error
	}{
		{"ErrNodeUnreachable", domain.ErrNodeUnreachable},
		{"ErrNodeInvoke", domain.ErrNodeInvoke},
		{"ErrTimeout", domain.ErrTimeout},
		{"ErrProviderError", domain.ErrProviderError},
		{"ErrRateLimit", domain.ErrRateLimit},
		{"ErrContextOverflow", domain.ErrContextOverflow},
	}
	for _, tt := range sentinels {
		t.Run(tt.name, func(t *testing.T) {
			if !classifyToolError(tt.sentinel) {
				t.Errorf("expected %s to be retryable", tt.name)
			}
		})
	}
}

func TestClassifyToolError_WrappedRetryableSentinels(t *testing.T) {
	wrapped := fmt.Errorf("camera snap on node-1: %w", domain.ErrTimeout)
	if !classifyToolError(wrapped) {
		t.Error("expected wrapped ErrTimeout to be retryable")
	}

	doubleWrapped := fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", domain.ErrNodeUnreachable))
	if !classifyToolError(doubleWrapped) {
		t.Error("expected double-wrapped ErrNodeUnreachable to be retryable")
	}
}

func TestClassifyToolError_PermanentSentinels(t *testing.T) {
	permanents := []struct {
		name     string
		sentinel error
	}{
		{"ErrNodeNotFound", domain.ErrNodeNotFound},
		{"ErrNodeCapability", domain.ErrNodeCapability},
		{"ErrNodeAuth", domain.ErrNodeAuth},
		{"ErrNodeNotAllowed", domain.ErrNodeNotAllowed},
		{"ErrPathOutsideSandbox", domain.ErrPathOutsideSandbox},
		{"ErrCommandNotAllowed", domain.ErrCommandNotAllowed},
		{"ErrSSRFBlocked", domain.ErrSSRFBlocked},
		{"ErrToolNotFound", domain.ErrToolNotFound},
		{"ErrToolApprovalDenied", domain.ErrToolApprovalDenied},
		{"ErrNotFound", domain.ErrNotFound},
		{"ErrDuplicate", domain.ErrDuplicate},
		{"ErrLimitReached", domain.ErrLimitReached},
		{"ErrPermissionDenied", domain.ErrPermissionDenied},
		{"ErrDisabled", domain.ErrDisabled},
		{"ErrInvalidInput", domain.ErrInvalidInput},
	}
	for _, tt := range permanents {
		t.Run(tt.name, func(t *testing.T) {
			if classifyToolError(tt.sentinel) {
				t.Errorf("expected %s to be non-retryable (permanent)", tt.name)
			}
		})
	}
}

func TestClassifyToolError_StringPatterns(t *testing.T) {
	retryables := []struct {
		name string
		err  string
	}{
		{"connection refused", "dial tcp 127.0.0.1:50051: connection refused"},
		{"connection reset", "read tcp 10.0.0.1:443: connection reset by peer"},
		{"no such host", "dial tcp: lookup node-1.local: no such host"},
		{"timeout", "http: request timeout after 30s"},
		{"deadline exceeded", "context deadline exceeded"},
		{"temporarily unavailable", "resource temporarily unavailable"},
		{"service unavailable", "HTTP 503: service unavailable"},
		{"try again", "server busy, please try again later"},
		{"gRPC unavailable", "rpc error: code = Unavailable desc = transport is closing"},
		{"gRPC resource exhausted", "rpc error: code = ResourceExhausted desc = rate limited"},
	}
	for _, tt := range retryables {
		t.Run(tt.name, func(t *testing.T) {
			err := errors.New(tt.err)
			if !classifyToolError(err) {
				t.Errorf("expected %q to be retryable", tt.err)
			}
		})
	}
}

func TestClassifyToolError_NonRetryableStrings(t *testing.T) {
	permanents := []struct {
		name string
		err  string
	}{
		{"not found", "camera device xyz not found"},
		{"permission denied", "permission denied: /etc/shadow"},
		{"invalid argument", "invalid phone number format"},
		{"already exists", "canvas already exists: my-canvas"},
		{"generic error", "something completely unexpected happened"},
		{"empty message", ""},
	}
	for _, tt := range permanents {
		t.Run(tt.name, func(t *testing.T) {
			err := errors.New(tt.err)
			if classifyToolError(err) {
				t.Errorf("expected %q to be non-retryable", tt.err)
			}
		})
	}
}

func TestClassifyToolError_WrappedWithRetryablePattern(t *testing.T) {
	// A non-sentinel error whose message contains a retryable pattern.
	inner := errors.New("dial tcp 10.0.0.1:443: connection refused")
	wrapped := fmt.Errorf("camera backend: %w", inner)
	if !classifyToolError(wrapped) {
		t.Error("expected wrapped connection refused to be retryable")
	}
}

func TestClassifyToolError_DomainErrorWithRetryableSentinel(t *testing.T) {
	// DomainError wrapping a retryable sentinel.
	derr := domain.NewDomainError("CameraTool.Snap", domain.ErrTimeout, "node-1 timed out")
	if !classifyToolError(derr) {
		t.Error("expected DomainError wrapping ErrTimeout to be retryable")
	}
}

func TestClassifyToolError_SubSystemErrorRetryable(t *testing.T) {
	// SubSystemError wrapping a retryable category sentinel.
	derr := domain.NewSubSystemError("camera", "CameraTool.Snap", domain.ErrTimeout, "node-1 timed out")
	if !classifyToolError(derr) {
		t.Error("expected SubSystemError wrapping ErrTimeout to be retryable")
	}
}

func TestClassifyToolError_DomainErrorWithPermanentSentinel(t *testing.T) {
	derr := domain.NewSubSystemError("camera", "CameraTool.Snap", domain.ErrDisabled, "camera off")
	if classifyToolError(derr) {
		t.Error("expected SubSystemError wrapping ErrDisabled to be non-retryable")
	}
}
