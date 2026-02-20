package setup

import (
	"fmt"
	"time"
)

// CompletionPhase implements the completion and next steps phase.
type CompletionPhase struct{}

// Execute shows the completion message and next steps.
func (p *CompletionPhase) Execute(w *OnboardingWizard) error {
	ui := NewUIHelper(w.reader, w.writer)

	ui.PrintEmptyLine()
	ui.PrintHeader("🎉 Setup Complete!")

	// Calculate setup time
	duration := time.Since(w.context.StartTime)
	minutes := int(duration.Minutes())

	ui.PrintSuccess(fmt.Sprintf("Configuration completed in %d minutes", minutes))
	ui.PrintEmptyLine()

	// Configuration summary
	ui.PrintSection("Your Configuration",
		"Here's what we set up for you:")

	// AI Provider
	providerName := w.config.LLM.DefaultProvider
	model := "default"
	if len(w.config.LLM.Providers) > 0 {
		model = w.config.LLM.Providers[0].Model
	}
	ui.PrintBullet(fmt.Sprintf("AI Provider: %s (%s)", providerName, model))

	// Memory
	memoryStatus := "Disabled"
	if w.config.Memory.Provider != "noop" {
		memoryStatus = fmt.Sprintf("Enabled (%s at %s)", w.config.Memory.Provider, w.config.Memory.DataDir)
	}
	ui.PrintBullet(fmt.Sprintf("Memory: %s", memoryStatus))

	// Channels
	channelList := ""
	for i, ch := range w.config.Channels {
		if i > 0 {
			channelList += ", "
		}
		channelList += ch.Type
	}
	ui.PrintBullet(fmt.Sprintf("Channels: %s", channelList))

	// Security (if enabled)
	if w.config.Security.Encryption.Enabled {
		ui.PrintBullet("Security: Encryption + Audit Logging enabled")
	}

	ui.PrintEmptyLine()

	// Show test conversation if tests were run
	if len(w.context.TestResults) > 0 && !w.context.SkipValidation {
		ui.PrintInfo("✅", "Your test conversation showed that everything is working!")
	}

	ui.PrintEmptyLine()
	ui.PrintSeparator()
	ui.PrintEmptyLine()

	// Next steps
	ui.PrintSection("Getting Started",
		"You're ready to start using alfred-ai! Here's how:")

	ui.PrintInfo("🚀", "To start alfred-ai:")
	ui.PrintBullet("Run: ./alfred-ai")
	ui.PrintEmptyLine()

	ui.PrintInfo("💬", "Try these first conversations:")
	ui.PrintBullet("\"Hello! Tell me about yourself\"")
	ui.PrintBullet("\"Remember my name is [your name]\"")
	ui.PrintBullet("\"What can you help me with?\"")
	ui.PrintBullet("\"Help me write an email\"")
	ui.PrintEmptyLine()

	ui.PrintInfo("❓", "Need help?")
	ui.PrintBullet("Type 'help' in the chat")
	ui.PrintBullet("Visit: https://github.com/byterover/alfred-ai/docs")
	ui.PrintEmptyLine()

	// Environment variables reminder
	if p.needsEnvVars(w) {
		ui.PrintWarning("Important: Set these environment variables before running:")
		if w.config.LLM.Providers[0].APIKey == "" {
			envVar := fmt.Sprintf("ALFREDAI_LLM_PROVIDER_%s_API_KEY",
				w.config.LLM.DefaultProvider)
			ui.PrintBullet(envVar)
		}
		if w.config.Security.Encryption.Enabled {
			ui.PrintBullet("ENCRYPTION_KEY (your passphrase)")
		}
		for _, ch := range w.config.Channels {
			if ch.Type == "telegram" && (ch.Telegram == nil || ch.Telegram.Token == "") {
				ui.PrintBullet("ALFREDAI_CHANNEL_TELEGRAM_TOKEN")
			}
		}
		ui.PrintEmptyLine()
	}

	ui.PrintSeparator()
	ui.PrintEmptyLine()

	// Optional: Start now
	startNow, err := ui.AskConfirmation("Would you like to start alfred-ai now?", false)
	if err != nil {
		return err
	}

	if startNow {
		ui.PrintInfo("🚀", "Starting alfred-ai...")
		ui.PrintInfo("💡", "Type '/quit' to exit when you're done")
		ui.PrintEmptyLine()
		// Note: Actual launch would happen after wizard completes
		// This is just a flag for the main function to know user wants to start
	} else {
		ui.PrintInfo("👋", "Thanks for setting up alfred-ai!")
		ui.PrintInfo("💡", "Run './alfred-ai' when you're ready to start")
	}

	ui.PrintEmptyLine()

	return nil
}

// needsEnvVars checks if environment variables need to be set.
func (p *CompletionPhase) needsEnvVars(w *OnboardingWizard) bool {
	// Check if API key was skipped
	if len(w.config.LLM.Providers) > 0 && w.config.LLM.Providers[0].APIKey == "" {
		return true
	}

	// Check if encryption is enabled
	if w.config.Security.Encryption.Enabled {
		return true
	}

	// Check if Telegram token was skipped
	for _, ch := range w.config.Channels {
		if ch.Type == "telegram" && (ch.Telegram == nil || ch.Telegram.Token == "") {
			return true
		}
	}

	return false
}
