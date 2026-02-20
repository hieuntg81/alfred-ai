package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"alfred-ai/internal/domain"
)

// errorQueryMemory fails on Query
type errorQueryMemory struct {
	stubMemory
}

func (m *errorQueryMemory) Query(_ context.Context, _ string, _ int) ([]domain.MemoryEntry, error) {
	return nil, fmt.Errorf("query failed")
}

// errorDeleteMemory fails on Delete
type errorDeleteMemory struct {
	stubMemory
}

func (m *errorDeleteMemory) Delete(_ context.Context, _ string) error {
	return fmt.Errorf("delete failed")
}

// mockAuditLogger counts Log calls.
type mockAuditLogger struct {
	logCount int
}

func (a *mockAuditLogger) Log(_ context.Context, _ domain.AuditEvent) error {
	a.logCount++
	return nil
}
func (a *mockAuditLogger) Close() error { return nil }

func newTestPrivacyManager(t *testing.T) (*PrivacyManager, string) {
	t.Helper()
	dir := t.TempDir()
	mem := &stubMemory{}
	dataFlow := domain.DataFlowInfo{
		Flows: []domain.DataFlow{
			{Source: "conversation", Destination: "local markdown files", Purpose: "long-term memory", Encrypted: false},
			{Source: "conversation", Destination: "LLM provider", Purpose: "response generation", Encrypted: true},
		},
	}
	pm := NewPrivacyManager(dir, mem, nil, dataFlow)
	return pm, dir
}

func TestPrivacyManager_ConsentFlow(t *testing.T) {
	pm, _ := newTestPrivacyManager(t)
	ctx := context.Background()

	if !pm.NeedsConsent() {
		t.Error("should need consent initially")
	}

	if err := pm.GrantConsent(ctx); err != nil {
		t.Fatalf("GrantConsent: %v", err)
	}

	if pm.NeedsConsent() {
		t.Error("should not need consent after granting")
	}

	state := pm.GetConsent()
	if !state.Granted {
		t.Error("consent should be granted")
	}
	if state.GrantedAt == "" {
		t.Error("GrantedAt should be set")
	}
}

func TestPrivacyManager_ConsentPersistence(t *testing.T) {
	dir := t.TempDir()
	mem := &stubMemory{}
	dataFlow := domain.DataFlowInfo{}
	ctx := context.Background()

	pm1 := NewPrivacyManager(dir, mem, nil, dataFlow)
	pm1.GrantConsent(ctx)

	// Create new manager from same dir
	pm2 := NewPrivacyManager(dir, mem, nil, dataFlow)
	if pm2.NeedsConsent() {
		t.Error("consent should persist across restarts")
	}
}

func TestPrivacyManager_RevokeConsent(t *testing.T) {
	pm, _ := newTestPrivacyManager(t)
	ctx := context.Background()

	pm.GrantConsent(ctx)
	if pm.NeedsConsent() {
		t.Fatal("should not need consent after granting")
	}

	if err := pm.RevokeConsent(ctx); err != nil {
		t.Fatalf("RevokeConsent: %v", err)
	}

	if !pm.NeedsConsent() {
		t.Error("should need consent after revoking")
	}
}

func TestPrivacyManager_DataFlow(t *testing.T) {
	pm, _ := newTestPrivacyManager(t)
	info := pm.DataFlow()

	if len(info.Flows) != 2 {
		t.Fatalf("expected 2 flows, got %d", len(info.Flows))
	}
	if info.Flows[0].Source != "conversation" {
		t.Errorf("flow[0].Source = %q", info.Flows[0].Source)
	}
}

func TestPrivacyManager_Export(t *testing.T) {
	dir := t.TempDir()
	mem := &stubMemory{}
	ctx := context.Background()

	// Pre-populate memory
	mem.stored = []domain.MemoryEntry{
		{ID: "1", Content: "fact one", Tags: []string{"test"}},
		{ID: "2", Content: "fact two", Tags: []string{"test"}},
	}
	// Override Query to return stored entries
	exportMem := &exportableMemory{entries: mem.stored}

	pm := NewPrivacyManager(dir, exportMem, nil, domain.DataFlowInfo{})
	outputPath := filepath.Join(dir, "export.json")

	result, err := pm.Export(ctx, outputPath)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.EntryCount != 2 {
		t.Errorf("EntryCount = %d, want 2", result.EntryCount)
	}

	// Verify file contents
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var entries []domain.MemoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("exported %d entries, want 2", len(entries))
	}
}

func TestPrivacyManager_DeleteEntry(t *testing.T) {
	dir := t.TempDir()
	delMem := &deletableMemory{
		entries: []domain.MemoryEntry{
			{ID: "abc", Content: "to delete"},
		},
	}
	pm := NewPrivacyManager(dir, delMem, nil, domain.DataFlowInfo{})
	ctx := context.Background()

	if err := pm.DeleteEntry(ctx, "abc"); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	if len(delMem.deleted) != 1 || delMem.deleted[0] != "abc" {
		t.Errorf("expected delete of 'abc', got %v", delMem.deleted)
	}
}

func TestPrivacyManager_DeleteAll(t *testing.T) {
	dir := t.TempDir()
	delMem := &deletableMemory{
		entries: []domain.MemoryEntry{
			{ID: "1", Content: "one"},
			{ID: "2", Content: "two"},
			{ID: "3", Content: "three"},
		},
	}
	pm := NewPrivacyManager(dir, delMem, nil, domain.DataFlowInfo{})
	ctx := context.Background()

	if err := pm.DeleteAll(ctx); err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}
	if len(delMem.deleted) != 3 {
		t.Errorf("expected 3 deletes, got %d", len(delMem.deleted))
	}
}

func TestPrivacyManager_GrantConsentAuditNil(t *testing.T) {
	dir := t.TempDir()
	mem := &stubMemory{}
	pm := NewPrivacyManager(dir, mem, nil, domain.DataFlowInfo{})

	if err := pm.GrantConsent(context.Background()); err != nil {
		t.Fatalf("GrantConsent: %v", err)
	}
	if pm.NeedsConsent() {
		t.Error("should not need consent after granting")
	}
}

func TestPrivacyManager_RevokeConsentAuditNil(t *testing.T) {
	dir := t.TempDir()
	mem := &stubMemory{}
	pm := NewPrivacyManager(dir, mem, nil, domain.DataFlowInfo{})

	pm.GrantConsent(context.Background())
	if err := pm.RevokeConsent(context.Background()); err != nil {
		t.Fatalf("RevokeConsent: %v", err)
	}
	if !pm.NeedsConsent() {
		t.Error("should need consent after revoking")
	}
}

func TestPrivacyManager_ExportAuditNil(t *testing.T) {
	dir := t.TempDir()
	mem := &exportableMemory{entries: []domain.MemoryEntry{
		{ID: "1", Content: "data"},
	}}
	pm := NewPrivacyManager(dir, mem, nil, domain.DataFlowInfo{})

	result, err := pm.Export(context.Background(), filepath.Join(dir, "export.json"))
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.EntryCount != 1 {
		t.Errorf("EntryCount = %d", result.EntryCount)
	}
}

func TestPrivacyManager_ExportQueryError(t *testing.T) {
	dir := t.TempDir()
	mem := &errorQueryMemory{}
	pm := NewPrivacyManager(dir, mem, nil, domain.DataFlowInfo{})

	_, err := pm.Export(context.Background(), filepath.Join(dir, "export.json"))
	if err == nil {
		t.Error("expected error from Query failure")
	}
}

func TestPrivacyManager_DeleteEntryAuditNil(t *testing.T) {
	dir := t.TempDir()
	delMem := &deletableMemory{
		entries: []domain.MemoryEntry{{ID: "test"}},
	}
	pm := NewPrivacyManager(dir, delMem, nil, domain.DataFlowInfo{})

	if err := pm.DeleteEntry(context.Background(), "test"); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
}

func TestPrivacyManager_GrantConsentWithAudit(t *testing.T) {
	dir := t.TempDir()
	audit := &mockAuditLogger{}
	pm := NewPrivacyManager(dir, &stubMemory{}, audit, domain.DataFlowInfo{})

	if err := pm.GrantConsent(context.Background()); err != nil {
		t.Fatalf("GrantConsent: %v", err)
	}
	if audit.logCount != 1 {
		t.Errorf("expected 1 audit log, got %d", audit.logCount)
	}
}

func TestPrivacyManager_RevokeConsentWithAudit(t *testing.T) {
	dir := t.TempDir()
	audit := &mockAuditLogger{}
	pm := NewPrivacyManager(dir, &stubMemory{}, audit, domain.DataFlowInfo{})

	pm.GrantConsent(context.Background())
	if err := pm.RevokeConsent(context.Background()); err != nil {
		t.Fatalf("RevokeConsent: %v", err)
	}
	if audit.logCount != 2 { // grant + revoke
		t.Errorf("expected 2 audit logs, got %d", audit.logCount)
	}
}

func TestPrivacyManager_ExportWithAudit(t *testing.T) {
	dir := t.TempDir()
	audit := &mockAuditLogger{}
	mem := &exportableMemory{entries: []domain.MemoryEntry{{ID: "1", Content: "data"}}}
	pm := NewPrivacyManager(dir, mem, audit, domain.DataFlowInfo{})

	_, err := pm.Export(context.Background(), filepath.Join(dir, "export.json"))
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if audit.logCount != 1 {
		t.Errorf("expected 1 audit log, got %d", audit.logCount)
	}
}

func TestPrivacyManager_DeleteEntryWithAudit(t *testing.T) {
	dir := t.TempDir()
	audit := &mockAuditLogger{}
	delMem := &deletableMemory{entries: []domain.MemoryEntry{{ID: "abc"}}}
	pm := NewPrivacyManager(dir, delMem, audit, domain.DataFlowInfo{})

	if err := pm.DeleteEntry(context.Background(), "abc"); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	if audit.logCount != 1 {
		t.Errorf("expected 1 audit log, got %d", audit.logCount)
	}
}

func TestPrivacyManager_DeleteAllWithAudit(t *testing.T) {
	dir := t.TempDir()
	audit := &mockAuditLogger{}
	delMem := &deletableMemory{entries: []domain.MemoryEntry{{ID: "1"}, {ID: "2"}}}
	pm := NewPrivacyManager(dir, delMem, audit, domain.DataFlowInfo{})

	if err := pm.DeleteAll(context.Background()); err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}
	if audit.logCount != 1 {
		t.Errorf("expected 1 audit log, got %d", audit.logCount)
	}
}

func TestPrivacyManager_DeleteEntryError(t *testing.T) {
	dir := t.TempDir()
	delMem := &errorDeleteMemory{}
	pm := NewPrivacyManager(dir, delMem, nil, domain.DataFlowInfo{})

	err := pm.DeleteEntry(context.Background(), "test")
	if err == nil {
		t.Error("expected error from Delete failure")
	}
}

func TestPrivacyManager_SaveConsentError(t *testing.T) {
	// Use a path where MkdirAll will fail
	pm := NewPrivacyManager("/proc/nonexistent/consent", &stubMemory{}, nil, domain.DataFlowInfo{})

	err := pm.GrantConsent(context.Background())
	if err == nil {
		t.Error("expected error from saveConsent failure")
	}
}

func TestPrivacyManager_RevokeConsentSaveError(t *testing.T) {
	dir := t.TempDir()
	pm := NewPrivacyManager(dir, &stubMemory{}, nil, domain.DataFlowInfo{})
	pm.GrantConsent(context.Background())

	// Make the consent directory read-only to force write error
	pm.consentPath = "/proc/nonexistent/consent/consent.json"
	err := pm.RevokeConsent(context.Background())
	if err == nil {
		t.Error("expected error from saveConsent failure")
	}
}

func TestPrivacyManager_ExportWriteError(t *testing.T) {
	dir := t.TempDir()
	mem := &exportableMemory{entries: []domain.MemoryEntry{{ID: "1", Content: "data"}}}
	pm := NewPrivacyManager(dir, mem, nil, domain.DataFlowInfo{})

	// Try to export to a non-existent directory
	_, err := pm.Export(context.Background(), "/proc/nonexistent/export.json")
	if err == nil {
		t.Error("expected error from write failure")
	}
}

func TestPrivacyManager_DeleteAllQueryError(t *testing.T) {
	dir := t.TempDir()
	mem := &errorQueryMemory{}
	pm := NewPrivacyManager(dir, mem, nil, domain.DataFlowInfo{})

	err := pm.DeleteAll(context.Background())
	if err == nil {
		t.Error("expected error from Query failure in DeleteAll")
	}
}

func TestPrivacyManager_DeleteAllDeleteError(t *testing.T) {
	dir := t.TempDir()
	mem := &deleteErrorMemory{
		entries: []domain.MemoryEntry{{ID: "1", Content: "one"}},
	}
	pm := NewPrivacyManager(dir, mem, nil, domain.DataFlowInfo{})

	err := pm.DeleteAll(context.Background())
	if err == nil {
		t.Error("expected error from Delete failure in DeleteAll")
	}
}

// --- Test helpers ---

type deleteErrorMemory struct {
	entries []domain.MemoryEntry
}

func (m *deleteErrorMemory) Store(_ context.Context, _ domain.MemoryEntry) error { return nil }
func (m *deleteErrorMemory) Query(_ context.Context, _ string, _ int) ([]domain.MemoryEntry, error) {
	return m.entries, nil
}
func (m *deleteErrorMemory) Delete(_ context.Context, _ string) error {
	return fmt.Errorf("delete failed")
}
func (m *deleteErrorMemory) Curate(_ context.Context, _ []domain.Message) (*domain.CurateResult, error) {
	return &domain.CurateResult{}, nil
}
func (m *deleteErrorMemory) Sync(_ context.Context) error { return nil }
func (m *deleteErrorMemory) Name() string                 { return "delete-error" }
func (m *deleteErrorMemory) IsAvailable() bool            { return true }

type exportableMemory struct {
	entries []domain.MemoryEntry
}

func (m *exportableMemory) Store(_ context.Context, _ domain.MemoryEntry) error { return nil }
func (m *exportableMemory) Query(_ context.Context, _ string, _ int) ([]domain.MemoryEntry, error) {
	return m.entries, nil
}
func (m *exportableMemory) Delete(_ context.Context, _ string) error { return nil }
func (m *exportableMemory) Curate(_ context.Context, _ []domain.Message) (*domain.CurateResult, error) {
	return &domain.CurateResult{}, nil
}
func (m *exportableMemory) Sync(_ context.Context) error { return nil }
func (m *exportableMemory) Name() string                 { return "exportable" }
func (m *exportableMemory) IsAvailable() bool            { return true }

type deletableMemory struct {
	entries []domain.MemoryEntry
	deleted []string
}

func (m *deletableMemory) Store(_ context.Context, _ domain.MemoryEntry) error { return nil }
func (m *deletableMemory) Query(_ context.Context, _ string, _ int) ([]domain.MemoryEntry, error) {
	return m.entries, nil
}
func (m *deletableMemory) Delete(_ context.Context, id string) error {
	m.deleted = append(m.deleted, id)
	return nil
}
func (m *deletableMemory) Curate(_ context.Context, _ []domain.Message) (*domain.CurateResult, error) {
	return &domain.CurateResult{}, nil
}
func (m *deletableMemory) Sync(_ context.Context) error { return nil }
func (m *deletableMemory) Name() string                 { return "deletable" }
func (m *deletableMemory) IsAvailable() bool            { return true }
