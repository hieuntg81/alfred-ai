package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"alfred-ai/internal/adapter/tui/theme"
)

// ToolExecution represents a single tool call and its result.
type ToolExecution struct {
	Name      string
	Status    string // "running", "completed", "error"
	StartedAt time.Time
	Duration  time.Duration
	Result    string
	IsError   bool
}

const maxToolExecutions = 100

// ToolOutputModel displays current/recent tool executions in a scrollable pane.
type ToolOutputModel struct {
	Viewport   viewport.Model
	executions []ToolExecution
	ready      bool
	width      int
	height     int
}

// NewToolOutput creates a tool output pane.
func NewToolOutput() ToolOutputModel {
	return ToolOutputModel{}
}

// SetSize sets the pane dimensions.
func (m *ToolOutputModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	if !m.ready {
		m.Viewport = viewport.New(w, h)
		m.Viewport.MouseWheelEnabled = true
		m.ready = true
	} else {
		m.Viewport.Width = w
		m.Viewport.Height = h
	}
	m.refreshContent()
}

// StartTool records the start of a tool execution.
func (m *ToolOutputModel) StartTool(name string) {
	m.executions = append(m.executions, ToolExecution{
		Name:      name,
		Status:    "running",
		StartedAt: time.Now(),
	})
	// Ring buffer: drop oldest completed entries when exceeding max.
	if len(m.executions) > maxToolExecutions {
		m.executions = m.executions[len(m.executions)-maxToolExecutions:]
	}
	m.refreshContent()
}

// CompleteTool marks the most recent execution of the given tool as completed.
func (m *ToolOutputModel) CompleteTool(name, result string, isError bool) {
	for i := len(m.executions) - 1; i >= 0; i-- {
		if m.executions[i].Name == name && m.executions[i].Status == "running" {
			m.executions[i].Duration = time.Since(m.executions[i].StartedAt)
			m.executions[i].Result = result
			m.executions[i].IsError = isError
			if isError {
				m.executions[i].Status = "error"
			} else {
				m.executions[i].Status = "completed"
			}
			break
		}
	}
	m.refreshContent()
}

// FullResult returns the name and full result of the execution at the given index.
func (m *ToolOutputModel) FullResult(i int) (name, result string, ok bool) {
	if i < 0 || i >= len(m.executions) {
		return "", "", false
	}
	e := m.executions[i]
	return e.Name, e.Result, true
}

// LastCompletedIdx returns the index of the most recent completed/error execution, or -1.
func (m *ToolOutputModel) LastCompletedIdx() int {
	for i := len(m.executions) - 1; i >= 0; i-- {
		if m.executions[i].Status == "completed" || m.executions[i].Status == "error" {
			return i
		}
	}
	return -1
}

// Clear removes all executions.
func (m *ToolOutputModel) Clear() {
	m.executions = nil
	m.refreshContent()
}

// Update handles viewport scrolling.
func (m ToolOutputModel) Update(msg tea.Msg) (ToolOutputModel, tea.Cmd) {
	if !m.ready {
		return m, nil
	}
	var cmd tea.Cmd
	m.Viewport, cmd = m.Viewport.Update(msg)
	return m, cmd
}

// View renders the tool output pane.
func (m ToolOutputModel) View() string {
	if !m.ready {
		return ""
	}

	header := theme.Bold.Render(" Tools")
	return header + "\n" + m.Viewport.View()
}

func (m *ToolOutputModel) refreshContent() {
	if !m.ready {
		return
	}

	if len(m.executions) == 0 {
		m.Viewport.SetContent(theme.TextMuted.Render("  No tool calls yet"))
		return
	}

	contentWidth := m.width - 4
	if contentWidth < 20 {
		contentWidth = 20
	}

	var sb strings.Builder
	for i, exec := range m.executions {
		if i > 0 {
			sb.WriteString("\n" + Divider(m.width-2) + "\n")
		}

		// Tool name + status icon.
		var statusIcon string
		switch exec.Status {
		case "running":
			statusIcon = theme.TextInfo.Render(theme.SymbolSpinner)
		case "completed":
			statusIcon = theme.TextSuccess.Render(theme.SymbolSuccess)
		case "error":
			statusIcon = theme.TextError.Render(theme.SymbolError)
		}

		sb.WriteString(fmt.Sprintf("  %s %s\n", statusIcon, theme.Bold.Render(exec.Name)))

		if exec.Status != "running" {
			sb.WriteString(fmt.Sprintf("  %s %s\n",
				theme.TextMuted.Render("Duration:"),
				exec.Duration.Round(time.Millisecond).String(),
			))
		}

		if exec.Result != "" {
			result := exec.Result
			// Truncate long results.
			maxLines := 10
			lines := strings.Split(result, "\n")
			truncated := len(lines) > maxLines
			if truncated {
				result = strings.Join(lines[:maxLines], "\n")
				result += fmt.Sprintf("\n  %s (%d more lines)",
					theme.SymbolEllipsis, len(lines)-maxLines)
			}
			sb.WriteString(theme.TextMuted.Render("  Result:") + "\n")
			for _, line := range strings.Split(result, "\n") {
				if len(line) > contentWidth {
					line = line[:contentWidth-1] + theme.SymbolEllipsis
				}
				sb.WriteString("  " + line + "\n")
			}
			if truncated {
				sb.WriteString(theme.Dim.Render("  (press Enter to view full output)") + "\n")
			}
		}
	}

	m.Viewport.SetContent(sb.String())
}
