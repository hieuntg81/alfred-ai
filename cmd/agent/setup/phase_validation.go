package setup

import (
	"fmt"
)

// ValidationPhase implements the validation and testing phase.
type ValidationPhase struct{}

// Execute validates the configuration and tests it end-to-end.
func (p *ValidationPhase) Execute(w *OnboardingWizard) error {
	ui := NewUIHelper(w.reader, w.writer)

	ui.PrintStepHeader(5, 6, "Testing Your Configuration")

	ui.PrintSection("Let's make sure everything works!",
		"We'll test your configuration by launching alfred-ai and having a quick "+
			"conversation. This ensures everything is set up correctly before we save.")

	ui.PrintEmptyLine()

	// Ask if user wants to run tests
	runTests, err := ui.AskConfirmation("Run automatic tests now?", true)
	if err != nil {
		return err
	}

	if !runTests {
		w.context.SkipValidation = true
		ui.PrintWarning("Skipping validation - configuration will be saved without testing")
		ui.PrintInfo("💡", "Make sure to test manually after setup")
		ui.PrintEmptyLine()
		ui.PrintSeparator()
		return nil
	}

	// Retry loop: run tests, allow model change on failure
	for {
		ui.PrintEmptyLine()
		ui.PrintInfo("🚀", "Starting tests...")
		ui.PrintEmptyLine()

		// Run the tests
		results, err := TestConfiguration(w.config, ui)
		w.context.TestResults = results

		ui.PrintEmptyLine()
		ui.PrintSeparator()
		ui.PrintEmptyLine()

		// Display results summary
		ui.PrintInfo("📊", "Test Results:")
		ui.PrintEmptyLine()

		allPassed := true
		for _, result := range results {
			if result.Success {
				ui.PrintSuccess(fmt.Sprintf("%s: %s", result.Step, result.Message))
			} else {
				allPassed = false
				ui.PrintError(fmt.Sprintf("%s: %s", result.Step, result.Message))
				if result.Error != nil {
					ui.PrintInfo("   ", fmt.Sprintf("Error: %v", result.Error))
				}
			}
		}

		ui.PrintEmptyLine()

		if allPassed {
			ui.PrintSuccess("All tests passed! Your configuration is working perfectly!")
			break
		}

		// Tests failed — show 3-option menu
		ui.PrintError("Some tests failed")
		ui.PrintEmptyLine()
		ui.PrintInfo("💡", "Common issues:")
		ui.PrintBullet("API key might be invalid or has no credits")
		ui.PrintBullet("Model might not support tool use or is unavailable")
		ui.PrintBullet("Network connectivity problems")
		ui.PrintBullet("Service might be temporarily down")

		ui.PrintEmptyLine()
		fmt.Fprintf(w.writer, "  1) Change model and re-test\n")
		fmt.Fprintf(w.writer, "  2) Save configuration anyway\n")
		fmt.Fprintf(w.writer, "  3) Abort setup\n")
		ui.PrintEmptyLine()

		choice, err := ui.AskChoice(3)
		if err != nil {
			return err
		}

		switch choice {
		case 1:
			// Change model and re-test
			ui.PrintEmptyLine()
			model, err := SelectModel(w, ui)
			if err != nil {
				return err
			}
			if len(w.config.LLM.Providers) > 0 {
				w.config.LLM.Providers[0].Model = model
			}
			ui.PrintSuccess(fmt.Sprintf("Model changed to %s, re-testing...", model))
			continue
		case 2:
			// Save anyway
			break
		case 3:
			return fmt.Errorf("validation failed and user chose to abort setup")
		}
		break
	}

	ui.PrintEmptyLine()

	// Final confirmation
	save, err := ui.AskConfirmation("Save this configuration?", true)
	if err != nil {
		return err
	}

	if !save {
		return fmt.Errorf("user chose not to save configuration")
	}

	ui.PrintEmptyLine()
	ui.PrintSeparator()

	return nil
}
