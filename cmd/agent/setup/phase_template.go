package setup

import (
	"fmt"
)

// TemplateSelectionPhase implements the template selection phase.
type TemplateSelectionPhase struct{}

// Execute presents template options and applies the selected template.
func (p *TemplateSelectionPhase) Execute(w *OnboardingWizard) error {
	ui := NewUIHelper(w.reader, w.writer)

	ui.PrintStepHeader(2, 6, "Choose Your Use Case")

	ui.PrintSection("Select a template",
		"We've created some ready-to-use configurations for common scenarios. "+
			"Choose the one that best fits what you want to do:")

	ui.PrintEmptyLine()

	// Get available templates
	templates := GetTemplates()

	// Display each template
	for i, tmpl := range templates {
		fmt.Fprintf(w.writer, "  %d) %s\n", i+1, tmpl.Name)
		fmt.Fprintf(w.writer, "     %s\n", tmpl.Description)
		fmt.Fprintf(w.writer, "     Difficulty: %s | Setup time: ~%d minutes\n",
			tmpl.Difficulty, tmpl.EstimateMin)
		fmt.Fprintln(w.writer)
	}

	// Get user choice
	choice, err := ui.AskChoice(len(templates))
	if err != nil {
		return err
	}

	// Apply selected template
	selected := templates[choice-1]
	w.template = &selected
	w.config = selected.Config

	ui.PrintEmptyLine()
	ui.PrintSuccess(fmt.Sprintf("Selected: %s", selected.Name))

	// Show what's been pre-configured
	ui.PrintInfo("✨", "Pre-configured for you:")

	switch selected.ID {
	case "personal-assistant":
		ui.PrintBullet("AI Provider: OpenAI GPT-4o-mini")
		ui.PrintBullet("Memory: Enabled (markdown files)")
		ui.PrintBullet("Interface: Command-line (CLI)")
	case "telegram-bot":
		ui.PrintBullet("AI Provider: OpenAI GPT-4o-mini")
		ui.PrintBullet("Memory: Enabled with auto-curation")
		ui.PrintBullet("Interface: Telegram messenger")
	case "secure-private":
		ui.PrintBullet("AI Provider: OpenAI GPT-4o-mini")
		ui.PrintBullet("Memory: Enabled with encryption")
		ui.PrintBullet("Security: Full (sandboxing + audit logs)")
		ui.PrintBullet("Interface: Command-line (CLI)")
	case "custom":
		ui.PrintBullet("Full manual configuration")
	}

	ui.PrintEmptyLine()
	ui.PrintInfo("💡", "We'll help you configure the remaining details in the next steps")

	ui.PrintEmptyLine()
	ui.PrintSeparator()

	return nil
}
