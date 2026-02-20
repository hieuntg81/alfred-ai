package setup

import (
	"fmt"
)

// WelcomePhase implements the welcome and introduction phase of onboarding.
type WelcomePhase struct{}

// Execute displays the welcome message and introduces alfred-ai.
func (p *WelcomePhase) Execute(w *OnboardingWizard) error {
	ui := NewUIHelper(w.reader, w.writer)

	// Main header
	ui.PrintHeader("Welcome to alfred-ai! 🤖")

	// Introduction
	ui.PrintSection("What is alfred-ai?",
		"alfred-ai is your personal AI assistant with long-term memory. "+
			"It can chat with you, remember conversations, help with tasks, "+
			"and run on your computer or as a chat bot on platforms like Telegram.")

	// What we'll do
	ui.PrintInfo("📋", "What we'll configure together:")
	ui.PrintBullet("Choose your AI provider (OpenAI, Anthropic, or Google)")
	ui.PrintBullet("Set up API access (we'll guide you)")
	ui.PrintBullet("Pick a use case (simple chat, Telegram bot, or secure mode)")
	ui.PrintBullet("Test everything to make sure it works")

	ui.PrintEmptyLine()

	// Time estimate
	ui.PrintInfo("⏱️", "This will take about 8-12 minutes")
	ui.PrintInfo("💡", "Don't worry - we'll guide you through each step!")

	ui.PrintEmptyLine()
	ui.PrintSeparator()
	ui.PrintEmptyLine()

	// Ask to continue
	fmt.Fprint(w.writer, "Ready to get started? Press Enter to continue...")
	_, err := w.reader.ReadString('\n')
	if err != nil {
		return err
	}

	return nil
}
