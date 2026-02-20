package setup

import "alfred-ai/internal/infra/config"

// ChannelConfigPhase implements the channel configuration phase.
type ChannelConfigPhase struct{}

// Execute configures channels based on the selected template.
func (p *ChannelConfigPhase) Execute(w *OnboardingWizard) error {
	ui := NewUIHelper(w.reader, w.writer)

	// Skip if using CLI-only templates
	if w.template != nil && (w.template.ID == "personal-assistant" ||
		w.template.ID == "secure-private" || w.template.ID == "custom") {
		// CLI is already configured, nothing to do
		return nil
	}

	// Telegram bot setup
	if w.template != nil && w.template.ID == "telegram-bot" {
		return p.setupTelegramBot(w, ui)
	}

	return nil
}

// setupTelegramBot guides the user through Telegram bot configuration.
func (p *ChannelConfigPhase) setupTelegramBot(w *OnboardingWizard, ui *UIHelper) error {
	ui.PrintStepHeader(4, 6, "Telegram Bot Configuration")

	ui.PrintSection("Set up your Telegram Bot",
		"To use alfred-ai on Telegram, you need to create a bot and get a token from Telegram.")

	ui.PrintInfo("📝", "How to create a Telegram bot:")
	ui.PrintBullet("Open Telegram and search for @BotFather")
	ui.PrintBullet("Send the command: /newbot")
	ui.PrintBullet("Follow the prompts to choose a name and username")
	ui.PrintBullet("Copy the bot token (looks like: 123456789:ABCdefGHIjklMNOpqrsTUVwxyz)")

	ui.PrintEmptyLine()

	hasToken, err := ui.AskConfirmation("Do you have a Telegram bot token ready?", false)
	if err != nil {
		return err
	}

	if !hasToken {
		skip, err := ui.AskConfirmation(
			"Skip for now? (You can add it later via environment variable)", false)
		if err != nil {
			return err
		}

		if skip {
			ui.PrintWarning("Remember to set ALFREDAI_CHANNEL_TELEGRAM_TOKEN before running alfred-ai")
			ui.PrintEmptyLine()
			ui.PrintSeparator()
			return nil
		}

		ui.PrintInfo("ℹ️", "Press Enter when you have your token...")
		w.reader.ReadString('\n')
	}

	// Get token
	token, err := ui.AskSecret("Paste your Telegram bot token")
	if err != nil {
		return err
	}

	if token == "" {
		ui.PrintWarning("No token provided - you'll need to set it via environment variable")
	} else {
		// Update channel config
		for i := range w.config.Channels {
			if w.config.Channels[i].Type == "telegram" {
				if w.config.Channels[i].Telegram == nil {
					w.config.Channels[i].Telegram = &config.TelegramChannelConfig{}
				}
				w.config.Channels[i].Telegram.Token = token
				break
			}
		}
		ui.PrintSuccess("Telegram bot token configured!")
	}

	ui.PrintEmptyLine()
	ui.PrintSeparator()

	return nil
}
