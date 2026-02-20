package domain

import (
	"context"
	"time"
)

// AuditEventType classifies audit log entries.
type AuditEventType string

const (
	AuditLLMCall      AuditEventType = "llm_call"
	AuditToolExec     AuditEventType = "tool_exec"
	AuditMemorySync   AuditEventType = "memory_sync"
	AuditMemoryStore  AuditEventType = "memory_store"
	AuditMemoryDelete AuditEventType = "memory_delete"
	AuditMemoryExport AuditEventType = "memory_export"
	AuditKeyRotation  AuditEventType = "key_rotation"
	AuditConsentGiven AuditEventType = "consent_given"

	// Node system audit events (Phase 5).
	AuditNodeRegister    AuditEventType = "node_register"
	AuditNodeUnregister  AuditEventType = "node_unregister"
	AuditNodeInvoke      AuditEventType = "node_invoke"
	AuditNodeTokenGen    AuditEventType = "node_token_gen"
	AuditNodeTokenRevoke AuditEventType = "node_token_revoke"

	// Compliance audit events.
	AuditAccessLog      AuditEventType = "access"
	AuditAccessDenied   AuditEventType = "access_denied"
	AuditDataEvent      AuditEventType = "data_event"
	AuditSecretDetected AuditEventType = "secret_detected"
	AuditSessionCreate  AuditEventType = "session_create"
	AuditSessionDelete  AuditEventType = "session_delete"

	// Tenant audit events (Phase 4).
	AuditTenantCreate AuditEventType = "tenant_create"
	AuditTenantUpdate AuditEventType = "tenant_update"
	AuditTenantDelete AuditEventType = "tenant_delete"

	// GDPR/compliance audit events (Phase 4).
	AuditGDPRExport    AuditEventType = "gdpr_export"
	AuditGDPRDelete    AuditEventType = "gdpr_delete"
	AuditGDPRAnonymize AuditEventType = "gdpr_anonymize"
	AuditRBACDenied    AuditEventType = "rbac_denied"
)

// AuditEvent represents a single auditable action.
type AuditEvent struct {
	Timestamp time.Time         `json:"timestamp"`
	Type      AuditEventType    `json:"type"`
	Detail    map[string]string `json:"detail"`

	// Compliance fields (optional, zero values omitted).
	Actor    string `json:"actor,omitempty"`
	Resource string `json:"resource,omitempty"`
	Action   string `json:"action,omitempty"`
	Outcome  string `json:"outcome,omitempty"`
}

// AuditLogger writes audit events to a persistent log.
type AuditLogger interface {
	Log(ctx context.Context, event AuditEvent) error
	Close() error
}
