package domain

import (
	"errors"
	"fmt"
)

// Category sentinels — use with NewSubSystemError for subsystem-specific errors.
// These are the preferred sentinels for new code; subsystem-specific sentinels below
// are being migrated to category sentinels progressively.
var (
	ErrNotFound         = fmt.Errorf("not found")
	ErrDuplicate        = fmt.Errorf("duplicate")
	ErrTimeout          = fmt.Errorf("operation timed out")
	ErrLimitReached     = fmt.Errorf("limit reached")
	ErrPermissionDenied = fmt.Errorf("permission denied")
	ErrDisabled         = fmt.Errorf("disabled")
	ErrInvalidInput     = fmt.Errorf("invalid input")
	ErrProviderError    = fmt.Errorf("provider error")
)

// Sentinel errors for the domain layer.
var (
	ErrProviderNotFound    = fmt.Errorf("llm provider not found")
	ErrToolNotFound        = fmt.Errorf("tool not found")
	ErrMemoryUnavailable   = fmt.Errorf("memory provider unavailable")
	ErrMaxIterations       = fmt.Errorf("agent reached max iterations")
	ErrSessionNotFound     = fmt.Errorf("session not found")
	ErrPathOutsideSandbox  = fmt.Errorf("path is outside sandbox boundary")
	ErrCommandNotAllowed   = fmt.Errorf("command not in allowlist")
	ErrSSRFBlocked         = fmt.Errorf("request to private/reserved IP blocked")
	ErrConfigLoad          = fmt.Errorf("failed to load configuration")
	ErrDecryption          = fmt.Errorf("decryption failed")
	ErrMemoryStore         = fmt.Errorf("memory store failed")
	ErrMemoryIndex         = fmt.Errorf("memory index operation failed")
	ErrCurateFailed        = fmt.Errorf("curation failed")
	ErrByteRoverSync       = fmt.Errorf("byterover sync failed")
	ErrEncryption          = fmt.Errorf("encryption operation failed")
	ErrAuditWrite          = fmt.Errorf("audit log write failed")
	ErrConsentRequired     = fmt.Errorf("user consent required")
	ErrMemoryDelete        = fmt.Errorf("memory delete failed")
	ErrToolApprovalDenied  = fmt.Errorf("tool approval denied")
	ErrToolApprovalTimeout = fmt.Errorf("tool approval timed out")

	// Gateway / RPC errors.
	ErrGatewayAuthFailed = fmt.Errorf("gateway: %w", ErrAuthInvalid)
	ErrRPCMethodNotFound = fmt.Errorf("rpc method not found")
	ErrRPCInvalidPayload = fmt.Errorf("rpc payload invalid")

	// RBAC errors.
	ErrForbidden = fmt.Errorf("forbidden: insufficient permissions")

	// Multi-tenant errors.
	ErrTenantNotFound  = fmt.Errorf("tenant not found")
	ErrTenantDuplicate = fmt.Errorf("tenant already exists")
	ErrTenantLimitHit  = fmt.Errorf("tenant resource limit exceeded")

	// Resilience errors.
	ErrContextOverflow = fmt.Errorf("context window exceeded")
	ErrRateLimit       = fmt.Errorf("rate limit exceeded")
	ErrAuthInvalid     = fmt.Errorf("authentication failed")
	ErrToolFailure     = fmt.Errorf("tool execution failed")

	// Embedding / vector errors.
	ErrEmbeddingFailed = fmt.Errorf("embedding generation failed")
	ErrVectorStore     = fmt.Errorf("vector store operation failed")
	ErrVectorSearch    = fmt.Errorf("vector search failed")

	// Node system errors.
	ErrNodeNotFound    = fmt.Errorf("node not found")
	ErrNodeDuplicate   = fmt.Errorf("node already registered")
	ErrNodeUnreachable = fmt.Errorf("node unreachable")
	ErrNodeCapability  = fmt.Errorf("capability not found on node")
	ErrNodeAuth        = fmt.Errorf("node: %w", ErrAuthInvalid)
	ErrNodeInvoke      = fmt.Errorf("node invocation failed")
	ErrNodeNotAllowed  = fmt.Errorf("node not in allowlist")
)

// DomainError wraps a sentinel error with context.
type DomainError struct {
	Op        string // operation name (e.g., "Tool.Execute")
	Err       error  // underlying sentinel or wrapped error
	Detail    string // human-readable detail
	SubSystem string // subsystem identifier (e.g., "workflow", "voicecall"); used for ErrorCode dispatch
}

func (e *DomainError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s: %s: %s", e.Op, e.Detail, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Op, e.Err)
}

func (e *DomainError) Unwrap() error { return e.Err }

// NewDomainError creates a new DomainError.
func NewDomainError(op string, err error, detail string) *DomainError {
	return &DomainError{Op: op, Err: err, Detail: detail}
}

// NewSubSystemError creates a DomainError tagged with a subsystem for ErrorCode dispatch.
// Use this with category sentinels (ErrNotFound, ErrTimeout, etc.) so that ErrorCodeOf
// can map the combination of sentinel + subsystem to a specific ErrorCode.
func NewSubSystemError(subsystem, op string, err error, detail string) *DomainError {
	return &DomainError{Op: op, Err: err, Detail: detail, SubSystem: subsystem}
}

// WrapOp adds operation context to an error using fmt.Errorf wrapping.
// Returns nil if err is nil, enabling idiomatic use: return domain.WrapOp("op", err)
func WrapOp(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}

// IsRetryableError reports whether err is a transient error that may succeed on retry.
func IsRetryableError(err error) bool {
	return errors.Is(err, ErrRateLimit) || errors.Is(err, ErrContextOverflow)
}

// ErrorCode is a machine-parseable error category for monitoring and alerting.
type ErrorCode string

// Error codes grouped by subsystem. Every sentinel error maps to exactly one code.
// Codes for migrated sentinels are retained for subSystemCodeMap dispatch and
// monitoring backward compatibility.
const (
	CodeUnknown            ErrorCode = "UNKNOWN"
	CodeProviderNotFound   ErrorCode = "PROVIDER_NOT_FOUND"
	CodeToolNotFound       ErrorCode = "TOOL_NOT_FOUND"
	CodeToolFailure        ErrorCode = "TOOL_FAILURE"
	CodeToolApprovalDenied ErrorCode = "TOOL_APPROVAL_DENIED"
	CodeToolApprovalTimout ErrorCode = "TOOL_APPROVAL_TIMEOUT"
	CodeMemoryUnavailable  ErrorCode = "MEMORY_UNAVAILABLE"
	CodeMemoryStore        ErrorCode = "MEMORY_STORE"
	CodeMemoryIndex        ErrorCode = "MEMORY_INDEX"
	CodeMemoryDelete       ErrorCode = "MEMORY_DELETE"
	CodeMaxIterations      ErrorCode = "MAX_ITERATIONS"
	CodeSessionNotFound    ErrorCode = "SESSION_NOT_FOUND"
	CodePathOutsideSandbox ErrorCode = "PATH_OUTSIDE_SANDBOX"
	CodeCommandNotAllowed  ErrorCode = "COMMAND_NOT_ALLOWED"
	CodeSSRFBlocked        ErrorCode = "SSRF_BLOCKED"
	CodeConfigLoad         ErrorCode = "CONFIG_LOAD"
	CodeEncryption         ErrorCode = "ENCRYPTION"
	CodeDecryption         ErrorCode = "DECRYPTION"
	CodeCurateFailed       ErrorCode = "CURATE_FAILED"
	CodeByteRoverSync      ErrorCode = "BYTEROVER_SYNC"
	CodeAuditWrite         ErrorCode = "AUDIT_WRITE"
	CodeConsentRequired    ErrorCode = "CONSENT_REQUIRED"
	CodeGatewayAuth        ErrorCode = "GATEWAY_AUTH"
	CodeRPCMethodNotFound  ErrorCode = "RPC_METHOD_NOT_FOUND"
	CodeRPCInvalidPayload  ErrorCode = "RPC_INVALID_PAYLOAD"
	CodeContextOverflow    ErrorCode = "CONTEXT_OVERFLOW"
	CodeRateLimit          ErrorCode = "RATE_LIMIT"
	CodeAuthInvalid        ErrorCode = "AUTH_INVALID"
	CodeEmbeddingFailed    ErrorCode = "EMBEDDING_FAILED"
	CodeVectorStore        ErrorCode = "VECTOR_STORE"
	CodeVectorSearch       ErrorCode = "VECTOR_SEARCH"
	CodeNodeNotFound       ErrorCode = "NODE_NOT_FOUND"
	CodeNodeDuplicate      ErrorCode = "NODE_DUPLICATE"
	CodeNodeUnreachable    ErrorCode = "NODE_UNREACHABLE"
	CodeNodeCapability     ErrorCode = "NODE_CAPABILITY"
	CodeNodeAuth           ErrorCode = "NODE_AUTH"
	CodeNodeInvoke         ErrorCode = "NODE_INVOKE"
	CodeNodeNotAllowed     ErrorCode = "NODE_NOT_ALLOWED"
	CodeForbidden          ErrorCode = "FORBIDDEN"
	CodeTenantNotFound     ErrorCode = "TENANT_NOT_FOUND"
	CodeTenantDuplicate    ErrorCode = "TENANT_DUPLICATE"
	CodeTenantLimitHit     ErrorCode = "TENANT_LIMIT_HIT"

	// Subsystem-specific codes used by subSystemCodeMap for migrated sentinels.
	CodePluginNotFound     ErrorCode = "PLUGIN_NOT_FOUND"
	CodePluginPermission   ErrorCode = "PLUGIN_PERMISSION"
	CodePluginDuplicate    ErrorCode = "PLUGIN_DUPLICATE"
	CodeAgentNotFound      ErrorCode = "AGENT_NOT_FOUND"
	CodeAgentDuplicate     ErrorCode = "AGENT_DUPLICATE"
	CodeWASMLoad           ErrorCode = "WASM_LOAD"
	CodeWASMExec           ErrorCode = "WASM_EXEC"
	CodeWASMTimeout        ErrorCode = "WASM_TIMEOUT"
	CodeWASMCapability     ErrorCode = "WASM_CAPABILITY"
	CodeBrowserNotConn     ErrorCode = "BROWSER_NOT_CONNECTED"
	CodeBrowserTimeout     ErrorCode = "BROWSER_TIMEOUT"
	CodeBrowserJSBlocked   ErrorCode = "BROWSER_JS_BLOCKED"
	CodeCanvasNotFound     ErrorCode = "CANVAS_NOT_FOUND"
	CodeCanvasExists       ErrorCode = "CANVAS_EXISTS"
	CodeCanvasContentSize  ErrorCode = "CANVAS_CONTENT_SIZE"
	CodeCanvasNameInvalid  ErrorCode = "CANVAS_NAME_INVALID"
	CodeProcessNotFound    ErrorCode = "PROCESS_NOT_FOUND"
	CodeProcessMaxSessions ErrorCode = "PROCESS_MAX_SESSIONS"
	CodeProcessNotRunning  ErrorCode = "PROCESS_NOT_RUNNING"
	CodeProcessStdinClosed ErrorCode = "PROCESS_STDIN_CLOSED"
	CodeWorkflowNotFound   ErrorCode = "WORKFLOW_NOT_FOUND"
	CodePipelineNotFound   ErrorCode = "PIPELINE_NOT_FOUND"
	CodeWorkflowPaused     ErrorCode = "WORKFLOW_PAUSED"
	CodeWorkflowMaxRunning ErrorCode = "WORKFLOW_MAX_RUNNING"
	CodeWorkflowStepFailed ErrorCode = "WORKFLOW_STEP_FAILED"
	CodeWorkflowTimeout    ErrorCode = "WORKFLOW_TIMEOUT"
	CodeWorkflowInvalid    ErrorCode = "WORKFLOW_INVALID_STEP"
	CodeWorkflowResume     ErrorCode = "WORKFLOW_RESUME_INVALID"
	CodeWorkflowToolCall   ErrorCode = "WORKFLOW_TOOL_CALL"
	CodeCameraPayload      ErrorCode = "CAMERA_PAYLOAD_TOO_LARGE"
	CodeCameraClipTooLong  ErrorCode = "CAMERA_CLIP_TOO_LONG"
	CodeCameraDeviceNotFnd ErrorCode = "CAMERA_DEVICE_NOT_FOUND"
	CodeCameraDisabled     ErrorCode = "CAMERA_DISABLED"
	CodeCameraTimeout      ErrorCode = "CAMERA_TIMEOUT"
	CodeLocationDisabled   ErrorCode = "LOCATION_DISABLED"
	CodeLocationPermission ErrorCode = "LOCATION_PERMISSION"
	CodeLocationTimeout    ErrorCode = "LOCATION_TIMEOUT"
	CodeLocationUnavail    ErrorCode = "LOCATION_UNAVAILABLE"
	CodeVoiceCallNotFound  ErrorCode = "VOICE_CALL_NOT_FOUND"
	CodeVoiceCallEnded     ErrorCode = "VOICE_CALL_ENDED"
	CodeVoiceCallMax       ErrorCode = "VOICE_CALL_MAX_CONCURRENT"
	CodeVoiceCallProvider  ErrorCode = "VOICE_CALL_PROVIDER"
	CodeVoiceCallPhone     ErrorCode = "VOICE_CALL_INVALID_PHONE"
	CodeVoiceCallWebhook   ErrorCode = "VOICE_CALL_WEBHOOK"

	// Category error codes — fallback codes when no subsystem-specific code matches.
	CodeNotFound         ErrorCode = "NOT_FOUND"
	CodeDuplicate        ErrorCode = "DUPLICATE"
	CodeTimeout          ErrorCode = "TIMEOUT"
	CodeLimitReached     ErrorCode = "LIMIT_REACHED"
	CodePermissionDenied ErrorCode = "PERMISSION_DENIED"
	CodeDisabled         ErrorCode = "DISABLED"
	CodeInvalidInput     ErrorCode = "INVALID_INPUT"
	CodeProviderError    ErrorCode = "PROVIDER_ERROR"
)

// errorCodeMap maps sentinel errors to their machine-parseable codes.
var errorCodeMap = map[error]ErrorCode{
	// Category sentinels (fallback codes).
	ErrNotFound:         CodeNotFound,
	ErrDuplicate:        CodeDuplicate,
	ErrTimeout:          CodeTimeout,
	ErrLimitReached:     CodeLimitReached,
	ErrPermissionDenied: CodePermissionDenied,
	ErrDisabled:         CodeDisabled,
	ErrInvalidInput:     CodeInvalidInput,
	ErrProviderError:    CodeProviderError,

	// Active sentinels.
	ErrProviderNotFound:    CodeProviderNotFound,
	ErrToolNotFound:        CodeToolNotFound,
	ErrToolFailure:         CodeToolFailure,
	ErrToolApprovalDenied:  CodeToolApprovalDenied,
	ErrToolApprovalTimeout: CodeToolApprovalTimout,
	ErrMemoryUnavailable:   CodeMemoryUnavailable,
	ErrMemoryStore:         CodeMemoryStore,
	ErrMemoryIndex:         CodeMemoryIndex,
	ErrMemoryDelete:        CodeMemoryDelete,
	ErrMaxIterations:       CodeMaxIterations,
	ErrSessionNotFound:     CodeSessionNotFound,
	ErrPathOutsideSandbox:  CodePathOutsideSandbox,
	ErrCommandNotAllowed:   CodeCommandNotAllowed,
	ErrSSRFBlocked:         CodeSSRFBlocked,
	ErrConfigLoad:          CodeConfigLoad,
	ErrDecryption:          CodeDecryption,
	ErrEncryption:          CodeEncryption,
	ErrCurateFailed:        CodeCurateFailed,
	ErrByteRoverSync:       CodeByteRoverSync,
	ErrAuditWrite:          CodeAuditWrite,
	ErrConsentRequired:     CodeConsentRequired,
	ErrGatewayAuthFailed:   CodeGatewayAuth,
	ErrRPCMethodNotFound:   CodeRPCMethodNotFound,
	ErrRPCInvalidPayload:   CodeRPCInvalidPayload,
	ErrContextOverflow:     CodeContextOverflow,
	ErrRateLimit:           CodeRateLimit,
	ErrAuthInvalid:         CodeAuthInvalid,
	ErrEmbeddingFailed:     CodeEmbeddingFailed,
	ErrVectorStore:         CodeVectorStore,
	ErrVectorSearch:        CodeVectorSearch,
	ErrNodeNotFound:        CodeNodeNotFound,
	ErrNodeDuplicate:       CodeNodeDuplicate,
	ErrNodeUnreachable:     CodeNodeUnreachable,
	ErrNodeCapability:      CodeNodeCapability,
	ErrNodeAuth:            CodeNodeAuth,
	ErrNodeInvoke:          CodeNodeInvoke,
	ErrNodeNotAllowed:      CodeNodeNotAllowed,
	ErrForbidden:           CodeForbidden,
	ErrTenantNotFound:      CodeTenantNotFound,
	ErrTenantDuplicate:     CodeTenantDuplicate,
	ErrTenantLimitHit:      CodeTenantLimitHit,
}

// subSystemCodeMap maps (category sentinel, subsystem) pairs to specific ErrorCodes.
// This enables NewSubSystemError-based errors to resolve to the same monitoring codes
// as the legacy subsystem-specific sentinels they replace.
var subSystemCodeMap = map[error]map[string]ErrorCode{
	ErrNotFound: {
		"workflow":  CodeWorkflowNotFound,
		"pipeline":  CodePipelineNotFound,
		"canvas":    CodeCanvasNotFound,
		"process":   CodeProcessNotFound,
		"voicecall": CodeVoiceCallNotFound,
		"camera":    CodeCameraDeviceNotFnd,
		"plugin":    CodePluginNotFound,
		"agent":     CodeAgentNotFound,
		"node":      CodeNodeNotFound,
	},
	ErrDuplicate: {
		"canvas": CodeCanvasExists,
		"plugin": CodePluginDuplicate,
		"agent":  CodeAgentDuplicate,
		"node":   CodeNodeDuplicate,
	},
	ErrTimeout: {
		"workflow": CodeWorkflowTimeout,
		"browser":  CodeBrowserTimeout,
		"camera":   CodeCameraTimeout,
		"location": CodeLocationTimeout,
		"wasm":     CodeWASMTimeout,
	},
	ErrLimitReached: {
		"workflow":  CodeWorkflowMaxRunning,
		"process":   CodeProcessMaxSessions,
		"voicecall": CodeVoiceCallMax,
		"canvas":    CodeCanvasContentSize,
		"camera":    CodeCameraPayload,
		"tenant":    CodeTenantLimitHit,
	},
	ErrPermissionDenied: {
		"plugin":   CodePluginPermission,
		"location": CodeLocationPermission,
		"node":     CodeNodeNotAllowed,
		"wasm":     CodeWASMCapability,
	},
	ErrDisabled: {
		"camera":   CodeCameraDisabled,
		"location": CodeLocationDisabled,
	},
	ErrInvalidInput: {
		"workflow":  CodeWorkflowInvalid,
		"voicecall": CodeVoiceCallPhone,
		"canvas":    CodeCanvasNameInvalid,
	},
	ErrProviderError: {
		"voicecall": CodeVoiceCallProvider,
		"location":  CodeLocationUnavail,
		"embedding": CodeEmbeddingFailed,
	},
}

// ErrorCodeOf returns the machine-parseable error code for the given error.
// It unwraps DomainError and uses errors.Is to match sentinel errors.
// For DomainErrors with a SubSystem, it also checks the subSystemCodeMap
// to resolve category sentinels to specific codes.
// Returns CodeUnknown if no matching sentinel is found.
func ErrorCodeOf(err error) ErrorCode {
	if err == nil {
		return CodeUnknown
	}

	// Fast path: direct sentinel lookup.
	if code, ok := errorCodeMap[err]; ok {
		return code
	}

	// Unwrap DomainError to check its inner sentinel and subsystem.
	var de *DomainError
	if errors.As(err, &de) {
		// Check subsystem-specific mapping first (higher specificity).
		if de.SubSystem != "" {
			if subsysMap, ok := subSystemCodeMap[de.Err]; ok {
				if code, ok := subsysMap[de.SubSystem]; ok {
					return code
				}
			}
		}
		if code, ok := errorCodeMap[de.Err]; ok {
			return code
		}
	}

	// Walk the error chain with errors.Is.
	for sentinel, code := range errorCodeMap {
		if errors.Is(err, sentinel) {
			return code
		}
	}

	return CodeUnknown
}

// Code returns the ErrorCode for this DomainError's underlying sentinel.
// If SubSystem is set, checks the subSystemCodeMap for a specific code.
func (e *DomainError) Code() ErrorCode {
	if e.SubSystem != "" {
		if subsysMap, ok := subSystemCodeMap[e.Err]; ok {
			if code, ok := subsysMap[e.SubSystem]; ok {
				return code
			}
		}
	}
	if code, ok := errorCodeMap[e.Err]; ok {
		return code
	}
	return CodeUnknown
}
