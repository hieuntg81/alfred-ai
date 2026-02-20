// Package tabs provides individual tab models for the dashboard.
package tabs

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"alfred-ai/internal/adapter/tui/theme"
)

// AgentStat represents an agent's status for display.
type AgentStat struct {
	ID       string
	Provider string
	Model    string
	Sessions int
	Active   bool
}

// OverviewModel displays agent status and statistics.
type OverviewModel struct {
	Agents       []AgentStat
	MessageCount int
	ToolCount    int
	MemoryCount  int
	ErrorCount   int
	StartedAt    time.Time
	width        int
	height       int
}

// NewOverview creates an overview tab.
func NewOverview() OverviewModel {
	return OverviewModel{
		StartedAt: time.Now(),
	}
}

// SetSize sets dimensions.
func (m *OverviewModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// SetAgent adds or updates an agent in the agents list.
func (m *OverviewModel) SetAgent(agent AgentStat) {
	for i, a := range m.Agents {
		if a.ID == agent.ID {
			m.Agents[i] = agent
			return
		}
	}
	m.Agents = append(m.Agents, agent)
}

// IncrementMessages increments the message counter.
func (m *OverviewModel) IncrementMessages() { m.MessageCount++ }

// IncrementTools increments the tool counter.
func (m *OverviewModel) IncrementTools() { m.ToolCount++ }

// IncrementMemory increments the memory counter.
func (m *OverviewModel) IncrementMemory() { m.MemoryCount++ }

// IncrementErrors increments the error counter.
func (m *OverviewModel) IncrementErrors() { m.ErrorCount++ }

// Update is a no-op for the overview tab.
func (m OverviewModel) Update(_ tea.Msg) (OverviewModel, tea.Cmd) {
	return m, nil
}

// View renders the overview tab.
func (m OverviewModel) View() string {
	var sb strings.Builder

	// Agent table.
	sb.WriteString(theme.Bold.Render("  Agents") + "\n")
	if len(m.Agents) == 0 {
		sb.WriteString(theme.TextMuted.Render("  No agents registered") + "\n")
	} else {
		// Simple table rendering.
		sb.WriteString(fmt.Sprintf("  %-12s %-12s %-20s %-10s %s\n",
			theme.Dim.Render("ID"),
			theme.Dim.Render("Provider"),
			theme.Dim.Render("Model"),
			theme.Dim.Render("Sessions"),
			theme.Dim.Render("Status"),
		))
		sb.WriteString("  " + strings.Repeat("─", m.width-6) + "\n")

		for _, a := range m.Agents {
			status := theme.TextSuccess.Render(theme.SymbolInfo + " active")
			if !a.Active {
				status = theme.TextMuted.Render(theme.SymbolInfo + " idle")
			}

			model := a.Model
			if len(model) > 18 {
				model = model[:17] + theme.SymbolEllipsis
			}

			sb.WriteString(fmt.Sprintf("  %-12s %-12s %-20s %-10d %s\n",
				a.ID, a.Provider, model, a.Sessions, status))
		}
	}

	sb.WriteString("\n")

	// Statistics.
	sb.WriteString(theme.Bold.Render("  Statistics") + "\n")

	uptime := time.Since(m.StartedAt).Round(time.Second)
	stats := []struct {
		label string
		value string
	}{
		{"Messages", fmt.Sprintf("%d", m.MessageCount)},
		{"Tools", fmt.Sprintf("%d", m.ToolCount)},
		{"Memory", fmt.Sprintf("%d", m.MemoryCount)},
		{"Errors", fmt.Sprintf("%d", m.ErrorCount)},
		{"Uptime", uptime.String()},
	}

	var statParts []string
	for _, s := range stats {
		card := fmt.Sprintf("%s: %s",
			theme.TextMuted.Render(s.label),
			theme.StatValue.Render(s.value),
		)
		statParts = append(statParts, card)
	}

	sb.WriteString("  " + strings.Join(statParts, "  "+lipgloss.NewStyle().Foreground(theme.ColorBorder).Render("|")+"  ") + "\n")

	return sb.String()
}
