package setup

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// UIHelper provides user-friendly terminal UI utilities for the onboarding wizard.
type UIHelper struct {
	reader *bufio.Reader
	writer io.Writer
}

// NewUIHelper creates a new UIHelper with the given I/O streams.
func NewUIHelper(r io.Reader, w io.Writer) *UIHelper {
	return &UIHelper{
		reader: bufio.NewReader(r),
		writer: w,
	}
}

// PrintHeader prints a section header with decorative borders.
func (h *UIHelper) PrintHeader(title string) {
	border := strings.Repeat("=", 60)
	fmt.Fprintf(h.writer, "\n%s\n", border)
	fmt.Fprintf(h.writer, "  %s\n", title)
	fmt.Fprintf(h.writer, "%s\n\n", border)
}

// PrintSection prints a titled section with wrapped description text.
func (h *UIHelper) PrintSection(title, description string) {
	fmt.Fprintf(h.writer, "\n┌─ %s\n", title)
	fmt.Fprintf(h.writer, "│\n")

	// Wrap description at 68 characters (accounting for prefix)
	wrapped := wordWrap(description, 68)
	for _, line := range wrapped {
		fmt.Fprintf(h.writer, "│  %s\n", line)
	}
	fmt.Fprintf(h.writer, "└─\n\n")
}

// PrintSuccess prints a success message with a checkmark icon.
func (h *UIHelper) PrintSuccess(message string) {
	fmt.Fprintf(h.writer, "✓ %s\n", message)
}

// PrintError prints an error message with an X icon.
func (h *UIHelper) PrintError(message string) {
	fmt.Fprintf(h.writer, "✗ %s\n", message)
}

// PrintProgress prints a progress bar with step information.
func (h *UIHelper) PrintProgress(step, total int, message string) {
	percentage := (step * 100) / total
	bar := progressBar(percentage, 30)
	fmt.Fprintf(h.writer, "[%s] %d%% - %s\n", bar, percentage, message)
}

// PrintInfo prints an informational message with an icon.
func (h *UIHelper) PrintInfo(icon, message string) {
	fmt.Fprintf(h.writer, "%s %s\n", icon, message)
}

// PrintBullet prints a bulleted list item.
func (h *UIHelper) PrintBullet(message string) {
	fmt.Fprintf(h.writer, "  • %s\n", message)
}

// PrintWarning prints a warning message with warning icon.
func (h *UIHelper) PrintWarning(message string) {
	fmt.Fprintf(h.writer, "⚠ %s\n", message)
}

// AskString asks the user for a string input with an optional default value.
func (h *UIHelper) AskString(prompt, defaultVal string) (string, error) {
	if defaultVal != "" {
		fmt.Fprintf(h.writer, "%s [%s]: ", prompt, defaultVal)
	} else {
		fmt.Fprintf(h.writer, "%s: ", prompt)
	}

	line, err := h.reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal, nil
	}
	return line, nil
}

// AskSecret asks the user for secret input (like API keys).
// Note: This doesn't hide the input on screen (would need terminal package for that).
func (h *UIHelper) AskSecret(prompt string) (string, error) {
	fmt.Fprintf(h.writer, "%s: ", prompt)
	line, err := h.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// AskConfirmation asks the user for a yes/no confirmation.
func (h *UIHelper) AskConfirmation(prompt string, defaultYes bool) (bool, error) {
	def := "y/N"
	if defaultYes {
		def = "Y/n"
	}

	fmt.Fprintf(h.writer, "%s [%s]: ", prompt, def)
	line, err := h.reader.ReadString('\n')
	if err != nil {
		return false, err
	}

	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return defaultYes, nil
	}

	return line == "y" || line == "yes", nil
}

// AskChoice asks the user to select from a numbered list (1 to max).
func (h *UIHelper) AskChoice(max int) (int, error) {
	for {
		fmt.Fprintf(h.writer, "Choice [1-%d]: ", max)
		line, err := h.reader.ReadString('\n')
		if err != nil {
			return 0, err
		}

		n, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || n < 1 || n > max {
			h.PrintError(fmt.Sprintf("Please enter a number between 1 and %d", max))
			continue
		}
		return n, nil
	}
}

// PrintEmptyLine prints a blank line for spacing.
func (h *UIHelper) PrintEmptyLine() {
	fmt.Fprintln(h.writer)
}

// PrintSeparator prints a horizontal line separator.
func (h *UIHelper) PrintSeparator() {
	fmt.Fprintln(h.writer, strings.Repeat("─", 60))
}

// wordWrap wraps text to the specified width, breaking on word boundaries.
func wordWrap(text string, width int) []string {
	var lines []string
	words := strings.Fields(text)

	if len(words) == 0 {
		return lines
	}

	currentLine := words[0]

	for _, word := range words[1:] {
		if len(currentLine)+1+len(word) <= width {
			currentLine += " " + word
		} else {
			lines = append(lines, currentLine)
			currentLine = word
		}
	}

	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	return lines
}

// progressBar generates an ASCII progress bar.
func progressBar(percentage, width int) string {
	if percentage < 0 {
		percentage = 0
	}
	if percentage > 100 {
		percentage = 100
	}

	filled := (percentage * width) / 100
	empty := width - filled

	return strings.Repeat("█", filled) + strings.Repeat("░", empty)
}

// PrintConversation prints a formatted conversation exchange.
func (h *UIHelper) PrintConversation(role, message string) {
	fmt.Fprintf(h.writer, "\n")
	if role == "user" {
		fmt.Fprintf(h.writer, "👤 You: %s\n", message)
	} else if role == "assistant" {
		fmt.Fprintf(h.writer, "🤖 Bot: %s\n", message)
	} else {
		fmt.Fprintf(h.writer, "%s: %s\n", role, message)
	}
}

// PrintStepHeader prints a step number with title (e.g., "Step 1/6: Welcome").
func (h *UIHelper) PrintStepHeader(current, total int, title string) {
	fmt.Fprintf(h.writer, "\n┌─ Step %d/%d: %s\n", current, total, title)
	fmt.Fprintf(h.writer, "└─\n\n")
}

// Confirm asks for final confirmation before proceeding with a critical action.
func (h *UIHelper) Confirm(message string) (bool, error) {
	fmt.Fprintf(h.writer, "\n%s\n", message)
	return h.AskConfirmation("Proceed?", true)
}
