package dashboard

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"alfred-ai/internal/adapter/tui/components"
	"alfred-ai/internal/adapter/tui/dashboard/tabs"
	"alfred-ai/internal/domain"
)

// Ensure *DashboardModel satisfies tea.Model.
var _ tea.Model = (*DashboardModel)(nil)

// DashboardTab identifies which tab is active.
type DashboardTab int

const (
	TabOverview DashboardTab = iota
	TabMemory
	TabEvents
	TabLogs
	TabConfig
)

// DashboardDeps are dependencies for the dashboard.
type DashboardDeps struct {
	Bus          domain.EventBus
	Memory       domain.MemoryProvider
	Config       string // YAML string for config viewer
	AgentName    string
	ModelName    string
	ProviderName string
}

// DashboardModel is the root Bubble Tea model for the monitoring dashboard.
type DashboardModel struct {
	deps DashboardDeps

	// Tab management.
	activeTab DashboardTab
	tabBar    components.TabBarModel

	// Tab sub-models.
	overview tabs.OverviewModel
	memory   tabs.MemoryModel
	events   tabs.EventsModel
	logs     tabs.LogsModel
	config   tabs.ConfigModel

	// Layout.
	width  int
	height int

	// Event bus unsubscribe.
	programSend func(tea.Msg)
	unsubscribe func()
}

// NewDashboardModel creates the dashboard model.
func NewDashboardModel(deps DashboardDeps) *DashboardModel {
	tabItems := []components.Tab{
		{ID: "overview", Label: "Overview"},
		{ID: "memory", Label: "Memory"},
		{ID: "events", Label: "Events"},
		{ID: "logs", Label: "Logs"},
		{ID: "config", Label: "Config"},
	}

	m := &DashboardModel{
		deps:     deps,
		tabBar:   components.NewTabBar(tabItems),
		overview: tabs.NewOverview(),
		memory:   tabs.NewMemory(),
		events:   tabs.NewEvents(),
		logs:     tabs.NewLogs(),
		config:   tabs.NewConfig(),
	}

	if deps.Config != "" {
		m.config.SetContent(deps.Config)
	}

	if deps.AgentName != "" {
		m.overview.SetAgent(tabs.AgentStat{
			ID:       deps.AgentName,
			Provider: deps.ProviderName,
			Model:    deps.ModelName,
			Active:   true,
		})
	}

	return m
}

// SetProgramSender sets the function used to inject messages from the EventBus.
// Must be called before Run().
func (m *DashboardModel) SetProgramSender(send func(tea.Msg)) {
	m.programSend = send
}

// Init subscribes to the EventBus.
func (m *DashboardModel) Init() tea.Cmd {
	if m.deps.Bus != nil && m.programSend != nil {
		m.unsubscribe = m.deps.Bus.SubscribeAll(func(_ context.Context, event domain.Event) {
			m.programSend(EventBusMsg{Event: event})
		})
	}
	return nil
}

// Update handles messages.
func (m *DashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			if m.unsubscribe != nil {
				m.unsubscribe()
			}
			return m, tea.Quit
		case tea.KeyTab:
			m.tabBar.Next()
			m.activeTab = DashboardTab(m.tabBar.Active)
			return m, nil
		case tea.KeyShiftTab:
			m.tabBar.Prev()
			m.activeTab = DashboardTab(m.tabBar.Active)
			return m, nil
		}

		// Number keys for direct tab switching.
		if msg.Type == tea.KeyRunes {
			switch string(msg.Runes) {
			case "1":
				m.setTab(TabOverview)
				return m, nil
			case "2":
				m.setTab(TabMemory)
				return m, nil
			case "3":
				m.setTab(TabEvents)
				return m, nil
			case "4":
				m.setTab(TabLogs)
				return m, nil
			case "5":
				m.setTab(TabConfig)
				return m, nil
			case "q":
				if m.unsubscribe != nil {
					m.unsubscribe()
				}
				return m, tea.Quit
			}
		}

	case EventBusMsg:
		return m.handleEvent(msg.Event)

	case MemoryQueryResultMsg:
		if msg.Err != nil {
			m.memory.SetError(msg.Err.Error())
		} else {
			m.memory.SetResults(msg.Entries)
		}
		return m, nil

	case components.MemoryQueryMsg:
		if m.deps.Memory != nil {
			return m, queryMemoryCmd(m.deps.Memory, msg.Query)
		}
	}

	// Delegate to active tab.
	switch m.activeTab {
	case TabOverview:
		var cmd tea.Cmd
		m.overview, cmd = m.overview.Update(msg)
		cmds = append(cmds, cmd)
	case TabMemory:
		var cmd tea.Cmd
		m.memory, cmd = m.memory.Update(msg)
		cmds = append(cmds, cmd)
	case TabEvents:
		var cmd tea.Cmd
		m.events, cmd = m.events.Update(msg)
		cmds = append(cmds, cmd)
	case TabLogs:
		var cmd tea.Cmd
		m.logs, cmd = m.logs.Update(msg)
		cmds = append(cmds, cmd)
	case TabConfig:
		var cmd tea.Cmd
		m.config, cmd = m.config.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// View renders the dashboard.
func (m *DashboardModel) View() string {
	if m.width == 0 {
		return "  Initializing..."
	}

	tabView := m.tabBar.View()

	var content string
	switch m.activeTab {
	case TabOverview:
		content = m.overview.View()
	case TabMemory:
		content = m.memory.View()
	case TabEvents:
		content = m.events.View()
	case TabLogs:
		content = m.logs.View()
	case TabConfig:
		content = m.config.View()
	}

	// Status bar.
	hints := []components.KeyHint{
		{Key: "Tab", Desc: "Switch"},
		{Key: "1-5", Desc: "Jump"},
		{Key: "j/k", Desc: "Scroll"},
		{Key: "q", Desc: "Quit"},
	}
	sb := components.NewStatusBar()
	sb.Hints = hints
	sb.SetWidth(m.width)

	footer := sb.View()

	return lipgloss.JoinVertical(lipgloss.Left,
		tabView,
		content,
		footer,
	)
}

func (m *DashboardModel) layout() {
	tabH := 1
	footerH := 1
	contentH := m.height - tabH - footerH
	if contentH < 5 {
		contentH = 5
	}

	m.tabBar.SetWidth(m.width)
	m.overview.SetSize(m.width, contentH)
	m.memory.SetSize(m.width, contentH)
	m.events.SetSize(m.width, contentH)
	m.logs.SetSize(m.width, contentH)
	m.config.SetSize(m.width, contentH)
}

func (m *DashboardModel) setTab(tab DashboardTab) {
	m.activeTab = tab
	m.tabBar.SetActive(int(tab))
}

func (m *DashboardModel) handleEvent(event domain.Event) (tea.Model, tea.Cmd) {
	// Feed event to the events tab (always).
	m.events.AddEvent(event)

	// Feed to logs tab as a log entry.
	m.logs.AddEntry(tabs.LogEntry{
		Time:    event.Timestamp,
		Level:   eventToLogLevel(event.Type),
		Message: formatEventForLog(event),
	})

	// Update overview counters.
	switch {
	case event.Type == domain.EventMessageReceived || event.Type == domain.EventMessageSent:
		m.overview.IncrementMessages()
	case strings.HasPrefix(string(event.Type), "tool."):
		if event.Type == domain.EventToolCallCompleted {
			m.overview.IncrementTools()
		}
	case strings.HasPrefix(string(event.Type), "memory."):
		m.overview.IncrementMemory()
	case event.Type == domain.EventAgentError:
		m.overview.IncrementErrors()
	}

	return m, nil
}

func eventToLogLevel(t domain.EventType) string {
	switch {
	case strings.HasPrefix(string(t), "agent.error"):
		return "error"
	case strings.HasPrefix(string(t), "tool."):
		return "info"
	case strings.HasPrefix(string(t), "llm."):
		return "debug"
	default:
		return "info"
	}
}

func formatEventForLog(e domain.Event) string {
	msg := string(e.Type)
	if e.SessionID != "" {
		msg += " [" + e.SessionID + "]"
	}
	return msg
}
