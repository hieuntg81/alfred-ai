package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"alfred-ai/internal/domain"
)

// PrivacyManager implements domain.PrivacyController.
type PrivacyManager struct {
	consentPath string
	consent     domain.ConsentState
	memory      domain.MemoryProvider
	audit       domain.AuditLogger
	dataFlow    domain.DataFlowInfo
}

// NewPrivacyManager creates a new privacy controller.
func NewPrivacyManager(
	consentDir string,
	memory domain.MemoryProvider,
	audit domain.AuditLogger,
	dataFlow domain.DataFlowInfo,
) *PrivacyManager {
	pm := &PrivacyManager{
		consentPath: filepath.Join(consentDir, "consent.json"),
		memory:      memory,
		audit:       audit,
		dataFlow:    dataFlow,
	}
	pm.loadConsent()
	return pm
}

func (pm *PrivacyManager) NeedsConsent() bool {
	return !pm.consent.Granted
}

func (pm *PrivacyManager) GrantConsent(ctx context.Context) error {
	pm.consent = domain.ConsentState{
		Granted:   true,
		GrantedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := pm.saveConsent(); err != nil {
		return fmt.Errorf("save consent: %w", err)
	}
	if pm.audit != nil {
		pm.audit.Log(ctx, domain.AuditEvent{
			Type:   domain.AuditConsentGiven,
			Detail: map[string]string{"action": "grant"},
		})
	}
	return nil
}

func (pm *PrivacyManager) GetConsent() domain.ConsentState {
	return pm.consent
}

func (pm *PrivacyManager) RevokeConsent(ctx context.Context) error {
	pm.consent = domain.ConsentState{Granted: false}
	if err := pm.saveConsent(); err != nil {
		return fmt.Errorf("save consent: %w", err)
	}
	if pm.audit != nil {
		pm.audit.Log(ctx, domain.AuditEvent{
			Type:   domain.AuditConsentGiven,
			Detail: map[string]string{"action": "revoke"},
		})
	}
	return nil
}

func (pm *PrivacyManager) DataFlow() domain.DataFlowInfo {
	return pm.dataFlow
}

func (pm *PrivacyManager) Export(ctx context.Context, outputPath string) (*domain.ExportResult, error) {
	entries, err := pm.memory.Query(ctx, "", 0)
	if err != nil {
		return nil, fmt.Errorf("query all entries: %w", err)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal entries: %w", err)
	}

	if err := os.WriteFile(outputPath, data, 0600); err != nil {
		return nil, fmt.Errorf("write export: %w", err)
	}

	if pm.audit != nil {
		pm.audit.Log(ctx, domain.AuditEvent{
			Type:   domain.AuditMemoryExport,
			Detail: map[string]string{"path": outputPath, "count": fmt.Sprintf("%d", len(entries))},
		})
	}

	return &domain.ExportResult{
		Path:       outputPath,
		EntryCount: len(entries),
	}, nil
}

func (pm *PrivacyManager) DeleteEntry(ctx context.Context, id string) error {
	if err := pm.memory.Delete(ctx, id); err != nil {
		return err
	}
	if pm.audit != nil {
		pm.audit.Log(ctx, domain.AuditEvent{
			Type:   domain.AuditMemoryDelete,
			Detail: map[string]string{"id": id},
		})
	}
	return nil
}

func (pm *PrivacyManager) DeleteAll(ctx context.Context) error {
	entries, err := pm.memory.Query(ctx, "", 0)
	if err != nil {
		return fmt.Errorf("query all entries: %w", err)
	}

	deleted := 0
	for _, e := range entries {
		if err := pm.memory.Delete(ctx, e.ID); err != nil {
			return fmt.Errorf("delete entry %s: %w", e.ID, err)
		}
		deleted++
	}

	if pm.audit != nil {
		pm.audit.Log(ctx, domain.AuditEvent{
			Type:   domain.AuditMemoryDelete,
			Detail: map[string]string{"action": "delete_all", "count": fmt.Sprintf("%d", deleted)},
		})
	}
	return nil
}

func (pm *PrivacyManager) loadConsent() {
	data, err := os.ReadFile(pm.consentPath)
	if err != nil {
		return // no consent file = needs consent
	}
	json.Unmarshal(data, &pm.consent)
}

func (pm *PrivacyManager) saveConsent() error {
	dir := filepath.Dir(pm.consentPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(pm.consent, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pm.consentPath, data, 0600)
}
