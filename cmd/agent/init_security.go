package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
	"alfred-ai/internal/security"
	"alfred-ai/internal/usecase"
)

// scannerAdapter bridges *security.SecretScanner to the usecase.SecretScanner interface.
type scannerAdapter struct {
	inner *security.SecretScanner
}

func (a *scannerAdapter) Apply(text string) (string, bool, []usecase.SecretMatch) {
	cleaned, blocked, matches := a.inner.Apply(text)
	out := make([]usecase.SecretMatch, len(matches))
	for i, m := range matches {
		out[i] = usecase.SecretMatch{
			PatternName: m.PatternName,
			Action:      string(m.Action),
			Start:       m.Start,
			End:         m.End,
		}
	}
	return cleaned, blocked, out
}

// SecurityComponents holds all security-related components
type SecurityComponents struct {
	Sandbox        *security.Sandbox
	Encryptor      *security.AESContentEncryptor
	AuditLogger    domain.AuditLogger
	FileAuditLogger *security.FileAuditLogger // concrete type for retention enforcement; nil when audit is disabled
	SecretScanner  *security.SecretScanner
	KeyRotator     *security.KeyRotator
	Authorizer     domain.Authorizer       // RBAC authorizer; nil when RBAC is disabled
	GDPRHandler    *security.GDPRHandler   // nil when audit or memory is unavailable
}

// initSecurity initializes all security components (sandbox, encryption, audit logging)
// Returns the components, a cleanup function, and any error
func initSecurity(cfg *config.Config, log *slog.Logger) (*SecurityComponents, func(), error) {
	comp := &SecurityComponents{}
	var cleanups []func()

	cleanup := func() {
		// Execute cleanups in reverse order (LIFO)
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	// 1. Initialize sandbox
	if cfg.Tools.SandboxRoot != "" {
		sb, err := security.NewSandbox(cfg.Tools.SandboxRoot)
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("sandbox: %w", err)
		}
		comp.Sandbox = sb
		log.Info("sandbox initialized", "root", cfg.Tools.SandboxRoot)
	}

	// 2. Initialize content encryption (if enabled)
	if cfg.Security.Encryption.Enabled {
		passphrase := os.Getenv("ALFREDAI_ENCRYPTION_KEY")
		if passphrase != "" {
			enc, err := security.NewAESContentEncryptor(passphrase)
			if err != nil {
				cleanup()
				return nil, nil, fmt.Errorf("encryption: %w", err)
			}
			comp.Encryptor = enc

			// Add cleanup to zeroize encryption key
			cleanups = append(cleanups, func() {
				enc.Zeroize()
			})

			log.Info("content encryption enabled", "algorithm", "AES-256-GCM")
		} else {
			log.Warn("encryption enabled but ALFREDAI_ENCRYPTION_KEY not set, skipping")
		}
	}

	// 3. Initialize audit logging (if enabled)
	if cfg.Security.Audit.Enabled {
		auditDir := filepath.Dir(cfg.Security.Audit.Path)
		if err := os.MkdirAll(auditDir, 0700); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("create audit dir: %w", err)
		}

		fileAudit, err := security.NewFileAuditLogger(cfg.Security.Audit.Path)
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("audit logger: %w", err)
		}

		// Configure retention policy if set.
		if cfg.Security.Audit.Retention.MaxAge != "" || cfg.Security.Audit.Retention.MaxSize != "" {
			var maxAge time.Duration
			if cfg.Security.Audit.Retention.MaxAge != "" {
				d, err := time.ParseDuration(cfg.Security.Audit.Retention.MaxAge)
				if err != nil {
					cleanup()
					return nil, nil, fmt.Errorf("parse audit retention max_age: %w", err)
				}
				maxAge = d
			}
			var maxSize int64
			if cfg.Security.Audit.Retention.MaxSize != "" {
				s, err := security.ParseRetentionMaxSize(cfg.Security.Audit.Retention.MaxSize)
				if err != nil {
					cleanup()
					return nil, nil, fmt.Errorf("parse audit retention max_size: %w", err)
				}
				maxSize = s
			}
			fileAudit.SetRetention(security.RetentionPolicy{
				MaxAge:  maxAge,
				MaxSize: maxSize,
			})
		}

		comp.AuditLogger = fileAudit
		comp.FileAuditLogger = fileAudit

		// Add cleanup to close audit logger
		cleanups = append(cleanups, func() {
			fileAudit.Close()
		})

		log.Info("audit logging enabled", "path", cfg.Security.Audit.Path)
	}

	// 4. Initialize secret scanning (if enabled)
	if cfg.Security.SecretScanning.Enabled {
		var custom []security.SecretPattern
		for _, p := range cfg.Security.SecretScanning.CustomPatterns {
			action, err := security.ParseAction(p.Action)
			if err != nil {
				cleanup()
				return nil, nil, fmt.Errorf("secret scanning custom pattern %q: %w", p.Name, err)
			}
			re, err := regexp.Compile(p.Pattern)
			if err != nil {
				cleanup()
				return nil, nil, fmt.Errorf("secret scanning custom pattern %q regex: %w", p.Name, err)
			}
			custom = append(custom, security.SecretPattern{
				Name:    p.Name,
				Pattern: re,
				Action:  action,
			})
		}
		comp.SecretScanner = security.NewSecretScanner(custom, log)
		log.Info("secret scanning enabled", "custom_patterns", len(custom))
	}

	// 5. Initialize RBAC (if enabled)
	if cfg.Security.RBAC.Enabled {
		comp.Authorizer = &usecase.RBACAuthorizer{}
		log.Info("RBAC enabled")
	}

	// 6. Initialize key rotation (if enabled + encryption active)
	if cfg.Security.KeyRotation.Enabled && comp.Encryptor != nil {
		interval := 720 * time.Hour // default 30 days
		if cfg.Security.KeyRotation.Interval != "" {
			d, err := time.ParseDuration(cfg.Security.KeyRotation.Interval)
			if err != nil {
				cleanup()
				return nil, nil, fmt.Errorf("parse key rotation interval: %w", err)
			}
			interval = d
		}
		keyStore := security.NewEncryptorKeyStore(comp.Encryptor)
		rotator := security.NewKeyRotator(keyStore, interval, log)
		comp.KeyRotator = rotator

		cleanups = append(cleanups, func() {
			rotator.Stop()
		})

		log.Info("key rotation enabled", "interval", interval)
	}

	return comp, cleanup, nil
}
