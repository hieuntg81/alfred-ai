package security

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"alfred-ai/internal/domain"
)

// mockMemory is a minimal in-memory MemoryProvider for testing.
type mockMemory struct {
	mu      sync.Mutex
	entries []domain.MemoryEntry
}

func (m *mockMemory) Store(_ context.Context, entry domain.MemoryEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, entry)
	return nil
}

func (m *mockMemory) Query(_ context.Context, _ string, _ int) ([]domain.MemoryEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]domain.MemoryEntry, len(m.entries))
	copy(cp, m.entries)
	return cp, nil
}

func (m *mockMemory) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, e := range m.entries {
		if e.ID == id {
			m.entries = append(m.entries[:i], m.entries[i+1:]...)
			return nil
		}
	}
	return domain.ErrMemoryDelete
}

func (m *mockMemory) Curate(_ context.Context, _ []domain.Message) (*domain.CurateResult, error) {
	return &domain.CurateResult{}, nil
}

func (m *mockMemory) Sync(_ context.Context) error { return nil }
func (m *mockMemory) Name() string                  { return "mock" }
func (m *mockMemory) IsAvailable() bool              { return true }
func (m *mockMemory) Close() error                   { return nil }

// mockAudit captures audit events for verification.
type mockAudit struct {
	mu     sync.Mutex
	events []domain.AuditEvent
}

func (a *mockAudit) Log(_ context.Context, event domain.AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event)
	return nil
}

func (a *mockAudit) Close() error { return nil }

func (a *mockAudit) lastEvent() domain.AuditEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.events) == 0 {
		return domain.AuditEvent{}
	}
	return a.events[len(a.events)-1]
}

func TestGDPRHandler_ExportUserData(t *testing.T) {
	mem := &mockMemory{
		entries: []domain.MemoryEntry{
			{ID: "e1", Content: "Hello world"},
			{ID: "e2", Content: "Sensitive data"},
		},
	}
	audit := &mockAudit{}
	handler := NewGDPRHandler(mem, audit)

	outputDir := filepath.Join(t.TempDir(), "exports")
	result, err := handler.ExportUserData(context.Background(), "user-123", outputDir)
	if err != nil {
		t.Fatalf("ExportUserData: %v", err)
	}

	if result.MemoryCount != 2 {
		t.Errorf("MemoryCount = %d, want 2", result.MemoryCount)
	}
	if result.Path == "" {
		t.Error("Path should not be empty")
	}

	// Verify the export file is valid JSON.
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var exported map[string]any
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatalf("Unmarshal export: %v", err)
	}
	if exported["user_id"] != "user-123" {
		t.Errorf("export user_id = %v", exported["user_id"])
	}

	// Verify audit event.
	evt := audit.lastEvent()
	if evt.Type != domain.AuditGDPRExport {
		t.Errorf("audit type = %q, want %q", evt.Type, domain.AuditGDPRExport)
	}
	if evt.Actor != "user-123" {
		t.Errorf("audit actor = %q, want %q", evt.Actor, "user-123")
	}
}

func TestGDPRHandler_DeleteUserData(t *testing.T) {
	mem := &mockMemory{
		entries: []domain.MemoryEntry{
			{ID: "e1", Content: "To be deleted"},
			{ID: "e2", Content: "Also deleted"},
		},
	}
	audit := &mockAudit{}
	handler := NewGDPRHandler(mem, audit)

	if err := handler.DeleteUserData(context.Background(), "user-456"); err != nil {
		t.Fatalf("DeleteUserData: %v", err)
	}

	// Verify all entries deleted.
	remaining, _ := mem.Query(context.Background(), "", 0)
	if len(remaining) != 0 {
		t.Errorf("remaining entries = %d, want 0", len(remaining))
	}

	// Verify audit event.
	evt := audit.lastEvent()
	if evt.Type != domain.AuditGDPRDelete {
		t.Errorf("audit type = %q, want %q", evt.Type, domain.AuditGDPRDelete)
	}
}

func TestGDPRHandler_AnonymizeUserData(t *testing.T) {
	mem := &mockMemory{
		entries: []domain.MemoryEntry{
			{ID: "e1", Content: "PII data"},
		},
	}
	audit := &mockAudit{}
	handler := NewGDPRHandler(mem, audit)

	if err := handler.AnonymizeUserData(context.Background(), "user-789"); err != nil {
		t.Fatalf("AnonymizeUserData: %v", err)
	}

	// Verify entries removed.
	remaining, _ := mem.Query(context.Background(), "", 0)
	if len(remaining) != 0 {
		t.Errorf("remaining entries = %d, want 0", len(remaining))
	}

	// Verify audit event uses anonymized actor.
	evt := audit.lastEvent()
	if evt.Type != domain.AuditGDPRAnonymize {
		t.Errorf("audit type = %q, want %q", evt.Type, domain.AuditGDPRAnonymize)
	}
	if evt.Actor != "anonymized" {
		t.Errorf("audit actor = %q, want %q", evt.Actor, "anonymized")
	}
	if !strings.Contains(evt.Detail["original_user"], "[redacted]") {
		t.Errorf("original_user should be redacted, got %q", evt.Detail["original_user"])
	}
}

func TestGDPRHandler_EmptyUserID(t *testing.T) {
	handler := NewGDPRHandler(&mockMemory{}, nil)

	if _, err := handler.ExportUserData(context.Background(), "", t.TempDir()); err == nil {
		t.Error("expected error for empty user ID on export")
	}
	if err := handler.DeleteUserData(context.Background(), ""); err == nil {
		t.Error("expected error for empty user ID on delete")
	}
	if err := handler.AnonymizeUserData(context.Background(), ""); err == nil {
		t.Error("expected error for empty user ID on anonymize")
	}
}

func TestGDPRHandler_NilAuditLogger(t *testing.T) {
	mem := &mockMemory{
		entries: []domain.MemoryEntry{{ID: "e1", Content: "test"}},
	}
	handler := NewGDPRHandler(mem, nil) // nil audit logger

	// Should not panic.
	outputDir := filepath.Join(t.TempDir(), "exports")
	_, err := handler.ExportUserData(context.Background(), "user", outputDir)
	if err != nil {
		t.Fatalf("ExportUserData with nil audit: %v", err)
	}

	mem.entries = []domain.MemoryEntry{{ID: "e2", Content: "test"}}
	if err := handler.DeleteUserData(context.Background(), "user"); err != nil {
		t.Fatalf("DeleteUserData with nil audit: %v", err)
	}
}

func TestComplianceAuditLogger_DefaultFields(t *testing.T) {
	inner := &mockAudit{}
	compliance := NewComplianceAuditLogger(inner)

	// Log an event with no compliance fields set.
	err := compliance.Log(context.Background(), domain.AuditEvent{
		Type: domain.AuditToolExec,
	})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	evt := inner.lastEvent()
	if evt.Actor != "system" {
		t.Errorf("Actor = %q, want %q", evt.Actor, "system")
	}
	if evt.Action != "tool_exec" {
		t.Errorf("Action = %q, want %q", evt.Action, "tool_exec")
	}
	if evt.Outcome != "success" {
		t.Errorf("Outcome = %q, want %q", evt.Outcome, "success")
	}
	if evt.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
}

func TestComplianceAuditLogger_PreservesExplicitFields(t *testing.T) {
	inner := &mockAudit{}
	compliance := NewComplianceAuditLogger(inner)

	err := compliance.Log(context.Background(), domain.AuditEvent{
		Type:    domain.AuditAccessDenied,
		Actor:   "admin-user",
		Action:  "session_delete",
		Outcome: "denied",
	})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	evt := inner.lastEvent()
	if evt.Actor != "admin-user" {
		t.Errorf("Actor = %q, want %q", evt.Actor, "admin-user")
	}
	if evt.Action != "session_delete" {
		t.Errorf("Action = %q, want %q", evt.Action, "session_delete")
	}
	if evt.Outcome != "denied" {
		t.Errorf("Outcome = %q, want %q", evt.Outcome, "denied")
	}
}
