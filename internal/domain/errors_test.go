package domain

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainErrorFormat(t *testing.T) {
	err := NewDomainError("Tool.Execute", ErrToolNotFound, "tool 'foo'")
	want := "Tool.Execute: tool 'foo': tool not found"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestDomainErrorFormatNoDetail(t *testing.T) {
	err := NewDomainError("Agent.Run", ErrMaxIterations, "")
	want := "Agent.Run: agent reached max iterations"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestDomainErrorUnwrap(t *testing.T) {
	err := NewDomainError("Sandbox.Validate", ErrPathOutsideSandbox, "/etc/passwd")
	if !errors.Is(err, ErrPathOutsideSandbox) {
		t.Error("errors.Is should match ErrPathOutsideSandbox")
	}
}

func TestDomainErrorAs(t *testing.T) {
	err := NewDomainError("LLM.Chat", ErrProviderNotFound, "groq")
	var de *DomainError
	if !errors.As(err, &de) {
		t.Fatal("errors.As should match *DomainError")
	}
	if de.Op != "LLM.Chat" {
		t.Errorf("Op = %q, want %q", de.Op, "LLM.Chat")
	}
}

// --- ErrorCode tests ---

func TestErrorCodeOf_DirectSentinel(t *testing.T) {
	assert.Equal(t, CodeToolNotFound, ErrorCodeOf(ErrToolNotFound))
	assert.Equal(t, CodeSessionNotFound, ErrorCodeOf(ErrSessionNotFound))
	assert.Equal(t, CodeRateLimit, ErrorCodeOf(ErrRateLimit))
	assert.Equal(t, CodeForbidden, ErrorCodeOf(ErrForbidden))
}

func TestErrorCodeOf_DomainError(t *testing.T) {
	err := NewDomainError("Tool.Execute", ErrToolNotFound, "tool 'foo'")
	assert.Equal(t, CodeToolNotFound, ErrorCodeOf(err))
}

func TestErrorCodeOf_WrappedError(t *testing.T) {
	// fmt.Errorf with %w wraps the sentinel.
	wrapped := fmt.Errorf("context: %w", ErrNodeUnreachable)
	assert.Equal(t, CodeNodeUnreachable, ErrorCodeOf(wrapped))
}

func TestErrorCodeOf_UnknownError(t *testing.T) {
	assert.Equal(t, CodeUnknown, ErrorCodeOf(fmt.Errorf("some random error")))
}

func TestErrorCodeOf_Nil(t *testing.T) {
	assert.Equal(t, CodeUnknown, ErrorCodeOf(nil))
}

func TestDomainError_Code(t *testing.T) {
	err := NewDomainError("Manager.Get", ErrNodeNotFound, "node-1")
	assert.Equal(t, CodeNodeNotFound, err.Code())
}

func TestDomainError_CodeUnknownSentinel(t *testing.T) {
	err := NewDomainError("Op", fmt.Errorf("custom"), "detail")
	assert.Equal(t, CodeUnknown, err.Code())
}

func TestAllSentinelsHaveCodes(t *testing.T) {
	// Verify every sentinel in errorCodeMap maps to a non-empty code.
	require.NotEmpty(t, errorCodeMap)
	for sentinel, code := range errorCodeMap {
		assert.NotEmpty(t, code, "sentinel %v has empty code", sentinel)
		assert.NotEqual(t, CodeUnknown, code, "sentinel %v maps to UNKNOWN", sentinel)
	}
}

// --- NewSubSystemError tests ---

func TestNewSubSystemError_Format(t *testing.T) {
	err := NewSubSystemError("workflow", "Run", ErrNotFound, "wf-123")
	// SubSystem is metadata, not included in Error() output.
	assert.Equal(t, "Run: wf-123: not found", err.Error())
}

func TestNewSubSystemError_SubSystemField(t *testing.T) {
	err := NewSubSystemError("workflow", "Run", ErrNotFound, "wf-123")
	assert.Equal(t, "workflow", err.SubSystem)
}

func TestNewSubSystemError_Unwrap(t *testing.T) {
	err := NewSubSystemError("voicecall", "Dial", ErrTimeout, "")
	assert.True(t, errors.Is(err, ErrTimeout))
}

func TestNewSubSystemError_BackwardCompatible(t *testing.T) {
	// Zero-valued SubSystem for NewDomainError (no regression).
	err := NewDomainError("Op", ErrToolNotFound, "x")
	assert.Equal(t, "", err.SubSystem)
}

// --- Auth sentinel merge tests ---

func TestAuthSentinel_GatewayWrapsAuthInvalid(t *testing.T) {
	// ErrGatewayAuthFailed wraps ErrAuthInvalid.
	assert.True(t, errors.Is(ErrGatewayAuthFailed, ErrAuthInvalid))
	// Direct identity still works.
	assert.True(t, errors.Is(ErrGatewayAuthFailed, ErrGatewayAuthFailed))
	// ErrorCodeOf still maps to the specific code.
	assert.Equal(t, CodeGatewayAuth, ErrorCodeOf(ErrGatewayAuthFailed))
}

func TestAuthSentinel_NodeWrapsAuthInvalid(t *testing.T) {
	assert.True(t, errors.Is(ErrNodeAuth, ErrAuthInvalid))
	assert.True(t, errors.Is(ErrNodeAuth, ErrNodeAuth))
	assert.Equal(t, CodeNodeAuth, ErrorCodeOf(ErrNodeAuth))
}

// --- SubSystem-aware ErrorCodeOf tests ---

func TestErrorCodeOf_SubSystemNotFound(t *testing.T) {
	err := NewSubSystemError("workflow", "Get", ErrNotFound, "wf-abc")
	assert.Equal(t, CodeWorkflowNotFound, ErrorCodeOf(err))
}

func TestErrorCodeOf_SubSystemTimeout(t *testing.T) {
	err := NewSubSystemError("camera", "Capture", ErrTimeout, "")
	assert.Equal(t, CodeCameraTimeout, ErrorCodeOf(err))
}

func TestErrorCodeOf_SubSystemFallback(t *testing.T) {
	// Unknown subsystem falls back to category code.
	err := NewSubSystemError("unknown-subsystem", "Op", ErrNotFound, "")
	assert.Equal(t, CodeNotFound, ErrorCodeOf(err))
}

func TestErrorCodeOf_CategorySentinelDirect(t *testing.T) {
	// Direct category sentinel (not wrapped in DomainError) uses category code.
	assert.Equal(t, CodeNotFound, ErrorCodeOf(ErrNotFound))
	assert.Equal(t, CodeTimeout, ErrorCodeOf(ErrTimeout))
	assert.Equal(t, CodeDuplicate, ErrorCodeOf(ErrDuplicate))
}

func TestDomainError_CodeSubSystem(t *testing.T) {
	err := NewSubSystemError("voicecall", "Dial", ErrProviderError, "twilio down")
	assert.Equal(t, CodeVoiceCallProvider, err.Code())
}

func TestDomainError_CodeSubSystemFallback(t *testing.T) {
	err := NewSubSystemError("unknown", "Op", ErrTimeout, "")
	assert.Equal(t, CodeTimeout, err.Code())
}

// --- WrapOp tests ---

func TestWrapOp_Nil(t *testing.T) {
	assert.Nil(t, WrapOp("anything", nil))
}

func TestWrapOp_Format(t *testing.T) {
	err := WrapOp("Session.Load", ErrSessionNotFound)
	assert.Equal(t, "Session.Load: session not found", err.Error())
}

func TestWrapOp_PreservesIs(t *testing.T) {
	err := WrapOp("Session.Load", ErrSessionNotFound)
	assert.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestWrapOp_PreservesErrorCode(t *testing.T) {
	err := WrapOp("Session.Load", ErrSessionNotFound)
	assert.Equal(t, CodeSessionNotFound, ErrorCodeOf(err))
}

func TestWrapOp_Chain(t *testing.T) {
	inner := WrapOp("inner", ErrToolFailure)
	outer := WrapOp("outer", inner)
	assert.Equal(t, "outer: inner: tool execution failed", outer.Error())
	assert.True(t, errors.Is(outer, ErrToolFailure))
}

// --- IsRetryableError tests ---

func TestIsRetryableError_RateLimit(t *testing.T) {
	assert.True(t, IsRetryableError(ErrRateLimit))
}

func TestIsRetryableError_ContextOverflow(t *testing.T) {
	assert.True(t, IsRetryableError(ErrContextOverflow))
}

func TestIsRetryableError_Wrapped(t *testing.T) {
	err := fmt.Errorf("llm call: %w", ErrRateLimit)
	assert.True(t, IsRetryableError(err))
}

func TestIsRetryableError_DomainError(t *testing.T) {
	err := NewDomainError("LLM.Chat", ErrRateLimit, "openai")
	assert.True(t, IsRetryableError(err))
}

func TestIsRetryableError_NotRetryable(t *testing.T) {
	assert.False(t, IsRetryableError(ErrToolNotFound))
	assert.False(t, IsRetryableError(ErrAuthInvalid))
	assert.False(t, IsRetryableError(fmt.Errorf("random error")))
}

func TestIsRetryableError_Nil(t *testing.T) {
	assert.False(t, IsRetryableError(nil))
}
