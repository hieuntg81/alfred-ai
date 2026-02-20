package tabs

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"alfred-ai/internal/adapter/tui/theme"
)

// ConfigModel displays the current config as read-only YAML with secrets masked.
type ConfigModel struct {
	Viewport viewport.Model
	content  string
	ready    bool
	width    int
	height   int
}

// NewConfig creates a config viewer tab.
func NewConfig() ConfigModel {
	return ConfigModel{}
}

// SetSize sets dimensions.
func (m *ConfigModel) SetSize(w, h int) {
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
	if m.content != "" {
		m.Viewport.SetContent(m.content)
	}
}

// SetContent sets the YAML config string (should be pre-masked).
func (m *ConfigModel) SetContent(yaml string) {
	m.content = MaskSecrets(yaml)
	if m.ready {
		m.Viewport.SetContent(m.content)
	}
}

// Update handles viewport scrolling.
func (m ConfigModel) Update(msg tea.Msg) (ConfigModel, tea.Cmd) {
	if !m.ready {
		return m, nil
	}
	var cmd tea.Cmd
	m.Viewport, cmd = m.Viewport.Update(msg)
	return m, cmd
}

// View renders the config tab.
func (m ConfigModel) View() string {
	if !m.ready {
		return ""
	}
	header := theme.TextMuted.Render("  Read-only configuration view (secrets masked)")
	return header + "\n" + m.Viewport.View()
}

// MaskSecrets replaces values of known secret keys with asterisks.
func MaskSecrets(yaml string) string {
	secretKeys := []string{"api_key", "token", "secret", "password", "passphrase"}
	lines := strings.Split(yaml, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		for _, key := range secretKeys {
			if strings.HasPrefix(trimmed, key+":") || strings.HasPrefix(trimmed, key+" :") {
				// Mask the value part.
				idx := strings.Index(line, ":")
				if idx >= 0 && idx+1 < len(line) {
					val := strings.TrimSpace(line[idx+1:])
					if val != "" && val != "\"\"" && val != "''" {
						lines[i] = line[:idx+1] + " ****"
					}
				}
			}
		}
	}

	return strings.Join(lines, "\n")
}
