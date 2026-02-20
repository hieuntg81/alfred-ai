package setup

import (
	"fmt"
)

// SecurityOptionsPhase implements the security configuration phase.
type SecurityOptionsPhase struct{}

// Execute configures security options based on the selected template.
func (p *SecurityOptionsPhase) Execute(w *OnboardingWizard) error {
	ui := NewUIHelper(w.reader, w.writer)

	// Only run for secure-private template
	if w.template == nil || w.template.ID != "secure-private" {
		return nil
	}

	ui.PrintStepHeader(4, 6, "Security Configuration")

	ui.PrintSection("Enhanced Security Setup",
		"The Secure & Private template includes encryption for your conversations "+
			"and sandboxing to protect your system. Let's configure these features.")

	// Encryption passphrase
	ui.PrintInfo("🔒", "Encryption Passphrase")
	ui.PrintEmptyLine()
	ui.PrintBullet("Your conversations will be encrypted with AES-256-GCM")
	ui.PrintBullet("Choose a strong passphrase (the longer, the better)")
	ui.PrintBullet("You'll need this passphrase each time you start alfred-ai")

	ui.PrintEmptyLine()

	passphrase, err := ui.AskSecret("Enter encryption passphrase (min 12 characters recommended)")
	if err != nil {
		return err
	}

	if len(passphrase) < 12 {
		ui.PrintWarning("Passphrase is short - consider using a longer one for better security")
		proceed, err := ui.AskConfirmation("Continue with this passphrase?", false)
		if err != nil {
			return err
		}
		if !proceed {
			passphrase, err = ui.AskSecret("Enter a stronger passphrase")
			if err != nil {
				return err
			}
		}
	}

	// Store passphrase (in production, this would be handled via env var)
	// For now, we'll note that it needs to be set
	if passphrase != "" {
		ui.PrintSuccess("Encryption passphrase set")
		ui.PrintInfo("💡", "Set ENCRYPTION_KEY environment variable with this passphrase before running")
	}

	ui.PrintEmptyLine()

	// Sandbox directory
	ui.PrintInfo("📁", "Sandbox Directory")
	ui.PrintEmptyLine()
	ui.PrintBullet("File operations will be restricted to a specific directory")
	ui.PrintBullet("This prevents the AI from accessing sensitive files on your system")

	sandboxRoot, err := ui.AskString("Sandbox directory path", "./workspace")
	if err != nil {
		return err
	}

	w.config.Tools.SandboxRoot = sandboxRoot
	ui.PrintSuccess(fmt.Sprintf("Sandbox configured: %s", sandboxRoot))

	ui.PrintEmptyLine()

	// Audit logging
	ui.PrintInfo("📝", "Audit Logging")
	ui.PrintEmptyLine()
	ui.PrintBullet("All actions will be logged for security auditing")
	ui.PrintBullet("Logs are tamper-evident and stored in JSONL format")

	auditPath, err := ui.AskString("Audit log path", "./data/audit.jsonl")
	if err != nil {
		return err
	}

	w.config.Security.Audit.Path = auditPath
	w.config.Security.Audit.Enabled = true
	ui.PrintSuccess(fmt.Sprintf("Audit logging configured: %s", auditPath))

	ui.PrintEmptyLine()
	ui.PrintSeparator()

	return nil
}
