// Package theme provides a unified visual design system for the TUI.
// All styles use adaptive colors that work on both light and dark terminals.
//
// NO_COLOR (https://no-color.org/) is respected automatically by lipgloss via
// its color profile detection — when set, all color output is suppressed.
package theme

import (
	"github.com/charmbracelet/lipgloss"
)

// --- Adaptive Color Palette (4-6 primary colors) ---

var (
	ColorSuccess = lipgloss.AdaptiveColor{Light: "#2e7d32", Dark: "#66bb6a"}
	ColorError   = lipgloss.AdaptiveColor{Light: "#c62828", Dark: "#ef5350"}
	ColorWarning = lipgloss.AdaptiveColor{Light: "#e65100", Dark: "#ffa726"}
	ColorInfo    = lipgloss.AdaptiveColor{Light: "#0277bd", Dark: "#4fc3f7"}
	ColorAccent  = lipgloss.AdaptiveColor{Light: "#6a1b9a", Dark: "#ce93d8"}
	ColorMuted   = lipgloss.AdaptiveColor{Light: "#757575", Dark: "#9e9e9e"}

	ColorBorder       = lipgloss.AdaptiveColor{Light: "#bdbdbd", Dark: "#616161"}
	ColorBorderActive = lipgloss.AdaptiveColor{Light: "#1565c0", Dark: "#42a5f5"}

	ColorBg       = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#1e1e1e"}
	ColorBgAlt    = lipgloss.AdaptiveColor{Light: "#f5f5f5", Dark: "#2d2d2d"}
	ColorFg       = lipgloss.AdaptiveColor{Light: "#212121", Dark: "#e0e0e0"}
	ColorFgDim    = lipgloss.AdaptiveColor{Light: "#9e9e9e", Dark: "#757575"}
	ColorTabBg    = lipgloss.AdaptiveColor{Light: "#e0e0e0", Dark: "#333333"}
	ColorTabFg    = lipgloss.AdaptiveColor{Light: "#616161", Dark: "#9e9e9e"}
	ColorTabActBg = lipgloss.AdaptiveColor{Light: "#1565c0", Dark: "#42a5f5"}
	ColorTabActFg = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#1e1e1e"}
)

// --- Symbol variables (set by InitSymbols in symbols.go) ---
// These default to Unicode glyphs but fall back to ASCII on non-UTF8 terminals.

var (
	SymbolSuccess  = "✓"
	SymbolError    = "✗"
	SymbolWarning  = "⚠"
	SymbolInfo     = "●"
	SymbolSpinner  = "⏳"
	SymbolArrowR   = "→"
	SymbolBullet   = "•"
	SymbolEllipsis = "…"
	SymbolUser     = "You"
	SymbolBot      = "Alfred"
)

// --- Base styles ---

var (
	// Bold labels for role/keyword emphasis. Dim for secondary metadata.
	Bold = lipgloss.NewStyle().Bold(true)
	Dim  = lipgloss.NewStyle().Faint(true)

	// Semantic text styles.
	TextSuccess = lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true)
	TextError   = lipgloss.NewStyle().Foreground(ColorError).Bold(true)
	TextWarning = lipgloss.NewStyle().Foreground(ColorWarning).Bold(true)
	TextInfo    = lipgloss.NewStyle().Foreground(ColorInfo)
	TextAccent  = lipgloss.NewStyle().Foreground(ColorAccent)
	TextMuted   = lipgloss.NewStyle().Foreground(ColorMuted)
)

// --- Layout styles ---

var (
	// Border styles for visual grouping.
	BorderNormal = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder)

	BorderActive = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorderActive)

	// Focus-aware borders for split pane panels.
	FocusBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorderActive)

	UnfocusedBorder = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(ColorBorder)
)

// --- Message role styles ---

var (
	UserLabel = lipgloss.NewStyle().
			Foreground(ColorInfo).
			Bold(true)

	BotLabel = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true)

	SystemLabel = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Bold(true)

	ErrorLabel = lipgloss.NewStyle().
			Foreground(ColorError).
			Bold(true)

	ToolLabel = lipgloss.NewStyle().
			Foreground(ColorWarning).
			Bold(true)

	Timestamp = lipgloss.NewStyle().
			Foreground(ColorFgDim).
			Faint(true)
)

// --- Tab bar styles ---

var (
	TabNormal = lipgloss.NewStyle().
			Foreground(ColorTabFg).
			Background(ColorTabBg).
			Padding(0, 2)

	TabActive = lipgloss.NewStyle().
			Foreground(ColorTabActFg).
			Background(ColorTabActBg).
			Bold(true).
			Padding(0, 2)
)

// --- Status bar ---

var (
	StatusBar = lipgloss.NewStyle().
			Foreground(ColorFgDim).
			Background(ColorBgAlt).
			Padding(0, 1)

	StatusKey = lipgloss.NewStyle().
			Foreground(ColorInfo).
			Bold(true)
)

// --- Input area ---

var (
	InputPrompt = lipgloss.NewStyle().
			Foreground(ColorInfo).
			Bold(true)

	InputPlaceholder = lipgloss.NewStyle().
				Foreground(ColorFgDim)
)

// --- Wizard styles ---

var (
	WizardTitle = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true).
			Padding(0, 0, 1, 0)

	WizardStepActive = lipgloss.NewStyle().
				Foreground(ColorInfo).
				Bold(true)

	WizardStepDone = lipgloss.NewStyle().
			Foreground(ColorSuccess)

	WizardStepPending = lipgloss.NewStyle().
				Foreground(ColorMuted)

	ProgressFull = lipgloss.NewStyle().
			Foreground(ColorInfo)

	ProgressEmpty = lipgloss.NewStyle().
			Foreground(ColorMuted)
)

// --- Dashboard styles ---

var (
	StatCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(0, 1)

	StatValue = lipgloss.NewStyle().
			Foreground(ColorInfo).
			Bold(true)

	StatLabel = lipgloss.NewStyle().
			Foreground(ColorMuted)
)

// MaxContentWidth is the recommended max width for readable text content.
const MaxContentWidth = 100

// MinSplitWidth is the minimum terminal width that shows the split pane.
const MinSplitWidth = 100

// MinTabWidth is the minimum terminal width that shows tab labels (else collapse).
const MinTabWidth = 60

// Clamp returns v clamped to [lo, hi].
func Clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
