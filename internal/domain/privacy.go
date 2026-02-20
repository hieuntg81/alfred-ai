package domain

import "context"

// ContentEncryptor provides symmetric encryption for memory content.
type ContentEncryptor interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(ciphertext string) (string, error)
	IsEncrypted(s string) bool
	Rotate(newPassphrase string) error
}

// ConsentState tracks whether the user has granted data-processing consent.
type ConsentState struct {
	Granted   bool   `json:"granted"`
	GrantedAt string `json:"granted_at,omitempty"`
}

// DataFlowInfo describes what data the system processes and where it goes.
type DataFlowInfo struct {
	Flows []DataFlow `json:"flows"`
}

// DataFlow describes a single data processing path.
type DataFlow struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Purpose     string `json:"purpose"`
	Encrypted   bool   `json:"encrypted"`
}

// ExportResult holds the output of a data export operation.
type ExportResult struct {
	Path       string `json:"path"`
	EntryCount int    `json:"entry_count"`
}

// PrivacyController manages consent, data export, and deletion.
type PrivacyController interface {
	NeedsConsent() bool
	GrantConsent(ctx context.Context) error
	GetConsent() ConsentState
	RevokeConsent(ctx context.Context) error
	DataFlow() DataFlowInfo
	Export(ctx context.Context, outputPath string) (*ExportResult, error)
	DeleteEntry(ctx context.Context, id string) error
	DeleteAll(ctx context.Context) error
}
