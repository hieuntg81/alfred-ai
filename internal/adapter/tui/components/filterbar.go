package components

import (
	"fmt"
	"strings"

	"alfred-ai/internal/adapter/tui/theme"
)

// FilterOption defines a single filter choice.
type FilterOption struct {
	ID       string // e.g. "tool." for event type prefix, or "error" for log level
	Label    string // display label
	Shortcut string // single key shortcut
}

// FilterBarModel renders a horizontal filter bar with keyboard shortcuts.
type FilterBarModel struct {
	Options  []FilterOption
	Active   string // currently active filter ID; empty = show all
	Total    int    // total item count
	Filtered int    // filtered item count
	width    int
}

// NewFilterBar creates a filter bar with the given options.
func NewFilterBar(options []FilterOption) FilterBarModel {
	return FilterBarModel{
		Options: options,
	}
}

// SetWidth updates the bar width.
func (m *FilterBarModel) SetWidth(w int) {
	m.width = w
}

// Toggle activates a filter. Calling with the same ID again clears the filter.
func (m *FilterBarModel) Toggle(id string) {
	if m.Active == id {
		m.Active = ""
	} else {
		m.Active = id
	}
}

// HandleShortcut checks if the key matches any filter shortcut.
// Returns true if the key was consumed.
func (m *FilterBarModel) HandleShortcut(key string) bool {
	for _, opt := range m.Options {
		if opt.Shortcut == key {
			m.Toggle(opt.ID)
			return true
		}
	}
	// "a" for "All" (clear filter)
	if key == "a" {
		m.Active = ""
		return true
	}
	return false
}

// SetCounts updates the total and filtered counts.
func (m *FilterBarModel) SetCounts(total, filtered int) {
	m.Total = total
	m.Filtered = filtered
}

// View renders the filter bar.
func (m FilterBarModel) View() string {
	var parts []string

	// "All" option.
	allLabel := "[a] All"
	if m.Active == "" {
		parts = append(parts, theme.TextInfo.Render(allLabel))
	} else {
		parts = append(parts, theme.TextMuted.Render(allLabel))
	}

	// Each filter option.
	for _, opt := range m.Options {
		label := fmt.Sprintf("[%s] %s", opt.Shortcut, opt.Label)
		if m.Active == opt.ID {
			parts = append(parts, theme.TextInfo.Render(label))
		} else {
			parts = append(parts, theme.TextMuted.Render(label))
		}
	}

	bar := "  Filter: " + strings.Join(parts, "  ")

	// Show counts if available.
	if m.Total > 0 {
		countStr := fmt.Sprintf("  Showing %d/%d", m.Filtered, m.Total)
		bar += theme.Dim.Render(countStr)
	}

	return bar
}
