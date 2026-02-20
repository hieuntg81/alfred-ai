package theme

import (
	"os"
	"strings"
)

// SymbolSet holds all UI symbols, allowing runtime switching between
// Unicode and ASCII fallback sets.
type SymbolSet struct {
	Success  string
	Error    string
	Warning  string
	Info     string
	Spinner  string
	ArrowR   string
	Bullet   string
	Ellipsis string
	User     string
	Bot      string
}

var unicodeSymbols = SymbolSet{
	Success:  "\u2713", // ✓
	Error:    "\u2717", // ✗
	Warning:  "\u26A0", // ⚠
	Info:     "\u25CF", // ●
	Spinner:  "\u23F3", // ⏳
	ArrowR:   "\u2192", // →
	Bullet:   "\u2022", // •
	Ellipsis: "\u2026", // …
	User:     "You",
	Bot:      "Alfred",
}

var asciiSymbols = SymbolSet{
	Success:  "[OK]",
	Error:    "[ERR]",
	Warning:  "[!]",
	Info:     "[i]",
	Spinner:  "[...]",
	ArrowR:   "->",
	Bullet:   "*",
	Ellipsis: "...",
	User:     "You",
	Bot:      "Alfred",
}

// DetectUnicodeSupport checks whether the terminal likely supports Unicode.
// Priority: ALFREDAI_ASCII_SYMBOLS env (explicit override) > locale detection.
func DetectUnicodeSupport() bool {
	// Explicit override: set ALFREDAI_ASCII_SYMBOLS=1 to force ASCII.
	if v := os.Getenv("ALFREDAI_ASCII_SYMBOLS"); v == "1" || strings.EqualFold(v, "true") {
		return false
	}

	// Check locale environment variables for UTF-8 indication.
	for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		val := strings.ToLower(os.Getenv(key))
		if strings.Contains(val, "utf-8") || strings.Contains(val, "utf8") {
			return true
		}
	}

	// Most modern terminals support Unicode; default to true.
	return true
}

// InitSymbols sets the package-level Symbol* variables based on terminal
// capabilities. Called automatically by init(), but can be called again
// if the environment changes (e.g., in tests).
func InitSymbols() {
	set := unicodeSymbols
	if !DetectUnicodeSupport() {
		set = asciiSymbols
	}

	SymbolSuccess = set.Success
	SymbolError = set.Error
	SymbolWarning = set.Warning
	SymbolInfo = set.Info
	SymbolSpinner = set.Spinner
	SymbolArrowR = set.ArrowR
	SymbolBullet = set.Bullet
	SymbolEllipsis = set.Ellipsis
	SymbolUser = set.User
	SymbolBot = set.Bot
}

func init() {
	InitSymbols()
}
