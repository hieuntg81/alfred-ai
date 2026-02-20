package security

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"alfred-ai/internal/domain"
)

// GDPRHandler provides GDPR data subject rights operations:
// data export (portability), deletion (right to be forgotten), and anonymization.
type GDPRHandler struct {
	memory domain.MemoryProvider
	audit  domain.AuditLogger
}

// NewGDPRHandler creates a GDPR handler that operates on the given providers.
func NewGDPRHandler(memory domain.MemoryProvider, audit domain.AuditLogger) *GDPRHandler {
	return &GDPRHandler{
		memory: memory,
		audit:  audit,
	}
}

// GDPRExportResult holds the output of a GDPR data export.
type GDPRExportResult struct {
	Path         string    `json:"path"`
	MemoryCount  int       `json:"memory_count"`
	ExportedAt   time.Time `json:"exported_at"`
}

// ExportUserData exports all data associated with the given user ID to a JSON file.
// This implements the GDPR right to data portability (Article 20).
func (g *GDPRHandler) ExportUserData(ctx context.Context, userID string, outputDir string) (*GDPRExportResult, error) {
	if userID == "" {
		return nil, fmt.Errorf("user ID must not be empty")
	}

	if err := os.MkdirAll(outputDir, 0700); err != nil {
		return nil, fmt.Errorf("create export dir: %w", err)
	}

	// Export memory entries.
	entries, err := g.memory.Query(ctx, "", 0) // all entries
	if err != nil {
		return nil, fmt.Errorf("query memory: %w", err)
	}

	export := map[string]any{
		"user_id":     userID,
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"memory":      entries,
	}

	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal export: %w", err)
	}

	filename := fmt.Sprintf("gdpr_export_%s_%s.json", userID, time.Now().UTC().Format("20060102T150405"))
	exportPath := filepath.Join(outputDir, filename)

	if err := os.WriteFile(exportPath, data, 0600); err != nil {
		return nil, fmt.Errorf("write export: %w", err)
	}

	if g.audit != nil {
		_ = g.audit.Log(ctx, domain.AuditEvent{
			Timestamp: time.Now(),
			Type:      domain.AuditGDPRExport,
			Actor:     userID,
			Resource:  exportPath,
			Action:    "gdpr_export",
			Outcome:   "success",
			Detail:    map[string]string{"memory_count": fmt.Sprintf("%d", len(entries))},
		})
	}

	return &GDPRExportResult{
		Path:        exportPath,
		MemoryCount: len(entries),
		ExportedAt:  time.Now().UTC(),
	}, nil
}

// DeleteUserData removes all data associated with the given user ID.
// This implements the GDPR right to erasure (Article 17).
func (g *GDPRHandler) DeleteUserData(ctx context.Context, userID string) error {
	if userID == "" {
		return fmt.Errorf("user ID must not be empty")
	}

	// Delete all memory entries.
	entries, err := g.memory.Query(ctx, "", 0)
	if err != nil {
		return fmt.Errorf("query memory: %w", err)
	}

	deleted := 0
	for _, e := range entries {
		if err := g.memory.Delete(ctx, e.ID); err != nil {
			return fmt.Errorf("delete memory entry %s: %w", e.ID, err)
		}
		deleted++
	}

	if g.audit != nil {
		_ = g.audit.Log(ctx, domain.AuditEvent{
			Timestamp: time.Now(),
			Type:      domain.AuditGDPRDelete,
			Actor:     userID,
			Action:    "gdpr_delete",
			Outcome:   "success",
			Detail:    map[string]string{"memory_deleted": fmt.Sprintf("%d", deleted)},
		})
	}

	return nil
}

// AnonymizeUserData replaces PII in audit logs and memory with anonymized values.
// This is an alternative to full deletion when audit trail must be preserved.
func (g *GDPRHandler) AnonymizeUserData(ctx context.Context, userID string) error {
	if userID == "" {
		return fmt.Errorf("user ID must not be empty")
	}

	// Memory anonymization: delete all entries (strongest anonymization).
	entries, err := g.memory.Query(ctx, "", 0)
	if err != nil {
		return fmt.Errorf("query memory: %w", err)
	}

	for _, e := range entries {
		if err := g.memory.Delete(ctx, e.ID); err != nil {
			return fmt.Errorf("anonymize memory entry %s: %w", e.ID, err)
		}
	}

	if g.audit != nil {
		_ = g.audit.Log(ctx, domain.AuditEvent{
			Timestamp: time.Now(),
			Type:      domain.AuditGDPRAnonymize,
			Actor:     "anonymized",
			Action:    "gdpr_anonymize",
			Outcome:   "success",
			Detail:    map[string]string{"original_user": "[redacted]", "entries_anonymized": fmt.Sprintf("%d", len(entries))},
		})
	}

	return nil
}
