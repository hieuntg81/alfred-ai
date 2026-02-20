package chat

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"alfred-ai/internal/adapter/tui/components"
	"alfred-ai/internal/adapter/tui/theme"
	"alfred-ai/internal/adapter/tui/uxerror"
	"alfred-ai/internal/domain"
)

// DefaultSessionID is the session identifier used by the TUI channel.
const DefaultSessionID = "cli-default"

// ChatModelDeps are dependencies injected into the chat model.
type ChatModelDeps struct {
	Handler   domain.MessageHandler
	Privacy   domain.PrivacyController
	OnClear   func()
	OnGenBump func(gen uint64) // notifies the channel of a new request generation
	Logger    *slog.Logger
	AgentName string
	ModelName string
}

// ChatModel is the root Bubble Tea model for the chat TUI.
type ChatModel struct {
	deps ChatModelDeps

	// Sub-models
	chatView  components.ChatViewModel
	input     components.InputAreaModel
	statusBar components.StatusBarModel
	tabBar    components.TabBarModel
	toolPane  components.ToolOutputModel
	split     components.SplitPaneModel
	spinner   spinner.Model
	searchBar components.SearchBarModel
	modal     components.ModalModel

	// State
	waiting   bool   // true while waiting for handler response
	streaming bool   // true during simulated streaming
	streamBuf []rune // full response to stream (runes for Unicode safety)
	streamPos int    // current rune position in streamBuf
	width     int
	height    int
	quitting  bool
	vimMode   bool // true when input is blurred and vim keys are active

	// Streaming config.
	streamCfg StreamConfig

	// Request lifecycle: gen is incremented on every new request.
	// Stale OutboundMsg / HandlerDoneMsg with an older gen are discarded.
	gen      uint64
	cancelFn context.CancelFunc // cancels the in-flight handler goroutine

	// Tool tracking for current response (accumulated between ToolStarted/Completed msgs).
	pendingTools   []components.ToolCallSummary
	toolStartTimes map[string]time.Time
}

// NewChatModel creates the root chat model.
func NewChatModel(deps ChatModelDeps) ChatModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(theme.ColorInfo)

	agentName := deps.AgentName
	if agentName == "" {
		agentName = theme.SymbolBot
	}

	tabs := []components.Tab{
		{ID: "default", Label: agentName},
	}

	sb := components.NewStatusBar()
	sb.AgentName = agentName
	sb.ModelName = deps.ModelName
	sb.Hints = defaultHints()

	// Configure chat view with ring buffer.
	chatView := components.NewChatView()
	chatView.SetMaxMessages(1000)

	// Set up input area with autocomplete for slash commands.
	inputArea := components.NewInputArea()
	inputArea.Autocomplete = components.NewAutocomplete([]components.CommandDef{
		{Name: "/help", Description: "Show available commands"},
		{Name: "/clear", Description: "Clear conversation"},
		{Name: "/quit", Description: "Exit alfred-ai"},
		{Name: "/cancel", Description: "Cancel active request"},
		{Name: "/speed", Description: "Cycle streaming speed"},
		{Name: "/privacy", Description: "Show privacy settings"},
		{Name: "/export", Description: "Export memory data"},
		{Name: "/delete", Description: "Delete memory entries"},
	})

	return ChatModel{
		deps:           deps,
		chatView:       chatView,
		input:          inputArea,
		statusBar:      sb,
		tabBar:         components.NewTabBar(tabs),
		toolPane:       components.NewToolOutput(),
		split:          components.NewSplitPane(0.65),
		spinner:        s,
		searchBar:      components.NewSearchBar(),
		modal:          components.NewModal(),
		streamCfg:      DefaultStreamConfig(),
		toolStartTimes: make(map[string]time.Time),
	}
}

// Init initializes sub-models.
func (m ChatModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
	)
}

// Update handles all incoming messages.
func (m ChatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		if m.modal.Visible {
			m.modal.SetSize(m.width, m.height)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case components.InputSubmitMsg:
		return m.handleSubmit(msg.Value)

	case OutboundMsg:
		// Discard responses from a stale (cancelled) request.
		if msg.Gen != 0 && msg.Gen != m.gen {
			return m, nil
		}
		return m.handleOutbound(msg)

	case HandlerDoneMsg:
		// Discard completion from a stale (cancelled) request.
		if msg.Gen != m.gen {
			return m, nil
		}
		if msg.Err != nil {
			// Ignore context.Canceled — the user already saw "Request cancelled."
			if msg.Err != context.Canceled {
				friendly := uxerror.Humanize(msg.Err)
				m.chatView.AddMessage(components.ChatMessage{
					Role:    components.RoleError,
					Content: friendly.Render(),
				})
			}
			m.streaming = false
			m.waiting = false
			m.vimMode = false
			m.input.SetEnabled(true)
			m.statusBar.Extra = ""
			m.statusBar.Hints = defaultHints()
		}
		return m, nil

	case StreamTickMsg:
		return m.handleStreamTick()

	case ToolStartedMsg:
		m.toolPane.StartTool(msg.Name)
		m.statusBar.Extra = theme.SymbolSpinner + " Calling " + msg.Name + "..."
		m.toolStartTimes[msg.Name] = time.Now()
		return m, nil

	case ToolCompletedMsg:
		m.toolPane.CompleteTool(msg.Name, msg.Result, msg.IsError)
		duration := time.Duration(0)
		if start, ok := m.toolStartTimes[msg.Name]; ok {
			duration = time.Since(start)
			delete(m.toolStartTimes, msg.Name)
		}
		m.pendingTools = append(m.pendingTools, components.ToolCallSummary{
			Name:     msg.Name,
			Duration: duration,
			IsError:  msg.IsError,
		})
		if m.waiting {
			m.statusBar.Extra = theme.SymbolSpinner + " Thinking..."
		}
		return m, nil

	case ToolExpandMsg:
		m.modal.SetSize(m.width, m.height)
		m.modal.Open(msg.Name, msg.Result)
		return m, nil

	case QuitMsg:
		m.quitting = true
		return m, tea.Quit

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Update sub-models (filter mouse events from reaching the input).
	if !m.waiting {
		if _, isMouse := msg.(tea.MouseMsg); !isMouse {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	var cmd tea.Cmd
	m.chatView, cmd = m.chatView.Update(msg)
	cmds = append(cmds, cmd)

	if m.split.Visible && m.split.Focused == components.PaneRight {
		m.toolPane, cmd = m.toolPane.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// View renders the entire chat UI.
func (m ChatModel) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}
	if m.width == 0 {
		return "  Initializing..."
	}

	// If modal is open, render it as a full overlay.
	if m.modal.Visible {
		return m.modal.View()
	}

	// Tab bar.
	tabBar := m.tabBar.View()

	// Main content area.
	var mainContent string
	chatContent := m.chatView.View()

	if m.split.Visible {
		toolContent := m.toolPane.View()
		mainContent = m.split.Render(chatContent, toolContent)
	} else {
		mainContent = chatContent
	}

	// Search bar (shown below content when active).
	searchView := m.searchBar.View()

	// Input area with optional spinner.
	inputView := m.input.View()
	if m.waiting {
		spinnerStr := m.spinner.View() + " " + m.statusBar.Extra
		inputView = lipgloss.NewStyle().Faint(true).Render("> waiting for response...") +
			"\n" + spinnerStr
	}

	// Status bar.
	statusView := m.statusBar.View()

	// Compose vertically.
	parts := []string{tabBar, mainContent}
	if searchView != "" {
		parts = append(parts, searchView)
	}
	parts = append(parts, components.Divider(m.width), inputView, statusView)

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// layout recalculates sizes for all sub-models.
func (m *ChatModel) layout() {
	tabBarH := 1
	inputH := 3
	statusH := 1
	dividerH := 1
	searchBarH := 0
	if m.searchBar.Mode != components.SearchInactive {
		searchBarH = 1
	}
	contentH := m.height - tabBarH - inputH - statusH - dividerH - searchBarH
	if contentH < 5 {
		contentH = 5
	}

	m.tabBar.SetWidth(m.width)
	m.statusBar.SetWidth(m.width)
	m.split.SetSize(m.width, contentH)

	leftW := m.split.LeftWidth()
	m.chatView.SetSize(leftW, contentH)
	m.input.SetWidth(m.width)

	if m.split.Visible {
		rightW := m.split.RightWidth()
		m.toolPane.SetSize(rightW, contentH-1)
	}
}

// isSGRMouseSequence detects SGR mouse escape sequences that may leak
// through as key input (e.g. "<65;38;21M"). These are emitted when
// mouse cell motion tracking is enabled and some terminals pass them
// as key events instead of tea.MouseMsg.
func isSGRMouseSequence(s string) bool {
	if len(s) < 5 || s[0] != '<' {
		return false
	}
	last := s[len(s)-1]
	if last != 'M' && last != 'm' {
		return false
	}
	for _, r := range s[1 : len(s)-1] {
		if r != ';' && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// isMouseEscapeLeak detects mouse escape sequences that leaked through
// as key input instead of tea.MouseMsg. Covers SGR, X11 basic, and
// URXVT formats that appear during rapid trackpad scrolling.
func isMouseEscapeLeak(s string) bool {
	// SGR format: <digits;digits;digitsM/m
	if isSGRMouseSequence(s) {
		return true
	}
	// X11 basic mouse format: [M or [m followed by coordinate bytes.
	if len(s) >= 2 && s[0] == '[' && (s[1] == 'M' || s[1] == 'm') {
		return true
	}
	// URXVT format: [digits;digits;digitsM
	if len(s) >= 5 && s[0] == '[' && s[len(s)-1] == 'M' {
		allValid := true
		for _, r := range s[1 : len(s)-1] {
			if r != ';' && (r < '0' || r > '9') {
				allValid = false
				break
			}
		}
		if allValid {
			return true
		}
	}
	return false
}

// handleKey processes keyboard input.
func (m ChatModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Filter out mouse escape sequences that leaked through as key events.
	if isMouseEscapeLeak(msg.String()) {
		return m, nil
	}

	// If modal is open, route all keys to it.
	if m.modal.Visible {
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Update(msg)
		return m, cmd
	}

	// If search input is active, route keys to search bar.
	if m.searchBar.Mode == components.SearchInput {
		var cmd tea.Cmd
		m.searchBar, cmd = m.searchBar.Update(msg)
		if m.searchBar.Mode == components.SearchActive {
			// Search on raw message content (not rendered ANSI output).
			lines := m.chatView.RawLines()
			m.searchBar.Search(lines)
		}
		return m, cmd
	}

	switch msg.Type {
	case tea.KeyCtrlC:
		if m.waiting {
			m.cancelRequest("Request cancelled.")
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit

	case tea.KeyCtrlT:
		m.split.Toggle()
		m.layout()
		return m, nil

	case tea.KeyTab:
		if m.split.Visible {
			m.split.SwitchFocus()
			if m.split.Focused == components.PaneRight {
				m.statusBar.Hints = []components.KeyHint{
					{Key: "Tab", Desc: "Switch"},
					{Key: "j/k", Desc: "Scroll"},
					{Key: "Ctrl+T", Desc: "Close"},
				}
			} else {
				m.statusBar.Hints = defaultHints()
			}
		}
		return m, nil

	case tea.KeyCtrlN:
		m.tabBar.Next()
		return m, nil

	case tea.KeyCtrlP:
		m.tabBar.Prev()
		return m, nil

	case tea.KeyCtrlL:
		return m.handleSlashCommand("/clear", nil)

	case tea.KeyEsc:
		// If search is active, close it.
		if m.searchBar.Mode != components.SearchInactive {
			m.searchBar.Deactivate()
			return m, nil
		}
		// Enter vim mode (blur input).
		if !m.vimMode && !m.waiting {
			m.vimMode = true
			m.input.SetEnabled(false)
			m.statusBar.Hints = vimHints()
			return m, nil
		}

	case tea.KeyPgUp, tea.KeyPgDown:
		var cmd tea.Cmd
		m.chatView, cmd = m.chatView.Update(msg)
		return m, cmd
	}

	// Vim mode: j/k scroll, / search, n/N navigate, i to exit, Enter to expand tool.
	if m.vimMode {
		switch msg.String() {
		case "j", "down":
			m.chatView.Viewport.LineDown(3)
			return m, nil
		case "k", "up":
			m.chatView.Viewport.LineUp(3)
			return m, nil
		case "/":
			m.searchBar.SetWidth(m.width)
			m.searchBar.Activate()
			return m, nil
		case "n":
			if m.searchBar.Mode == components.SearchActive {
				if line := m.searchBar.NextMatch(); line >= 0 {
					m.chatView.Viewport.SetYOffset(line)
				}
			}
			return m, nil
		case "N":
			if m.searchBar.Mode == components.SearchActive {
				if line := m.searchBar.PrevMatch(); line >= 0 {
					m.chatView.Viewport.SetYOffset(line)
				}
			}
			return m, nil
		case "enter":
			// When tool pane is focused, expand the last completed tool result.
			if m.split.Visible && m.split.Focused == components.PaneRight {
				if idx := m.toolPane.LastCompletedIdx(); idx >= 0 {
					if name, result, ok := m.toolPane.FullResult(idx); ok {
						m.modal.SetSize(m.width, m.height)
						m.modal.Open(name, result)
					}
				}
			}
			return m, nil
		case "i":
			m.vimMode = false
			m.input.SetEnabled(true)
			m.statusBar.Hints = defaultHints()
			return m, nil
		case "g":
			m.chatView.Viewport.GotoTop()
			return m, nil
		case "G":
			m.chatView.Viewport.GotoBottom()
			return m, nil
		}
		return m, nil
	}

	// Forward to input area.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func vimHints() []components.KeyHint {
	return []components.KeyHint{
		{Key: "j/k", Desc: "Scroll"},
		{Key: "/", Desc: "Search"},
		{Key: "n/N", Desc: "Next/prev"},
		{Key: "g/G", Desc: "Top/bottom"},
		{Key: "i", Desc: "Input"},
	}
}

// handleSubmit processes user input submission.
func (m ChatModel) handleSubmit(value string) (tea.Model, tea.Cmd) {
	// Check for slash commands.
	if cmd, args, ok := components.ParseSlashCommand(value); ok {
		return m.handleSlashCommand(cmd, args)
	}

	// Cancel any in-flight request before starting a new one.
	if m.cancelFn != nil {
		m.cancelFn()
	}

	// Add user message to chat.
	m.chatView.AddMessage(components.ChatMessage{
		Role:      components.RoleUser,
		Content:   value,
		Timestamp: time.Now(),
	})

	// Reset tool tracking for new request.
	m.pendingTools = nil
	m.toolStartTimes = make(map[string]time.Time)

	// Bump generation so stale responses are discarded.
	m.gen++
	if m.deps.OnGenBump != nil {
		m.deps.OnGenBump(m.gen)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFn = cancel

	// Disable input, enable vim mode, and show spinner.
	m.waiting = true
	m.streaming = false
	m.vimMode = true
	m.input.SetEnabled(false)
	m.statusBar.Extra = theme.SymbolSpinner + " Thinking..."

	// Fire handler.
	inbound := domain.InboundMessage{
		SessionID:   DefaultSessionID,
		Content:     value,
		ChannelName: "cli",
	}

	return m, sendMessageCmd(ctx, m.deps.Handler, inbound, m.gen)
}

// handleOutbound processes an agent response.
func (m ChatModel) handleOutbound(msg OutboundMsg) (tea.Model, tea.Cmd) {
	content := msg.Message.Content
	role := components.RoleAssistant
	if msg.Message.IsError {
		role = components.RoleError
	}

	// Capture tool calls accumulated during this response.
	toolCalls := m.pendingTools
	m.pendingTools = nil

	// Add message — either streamed or shown instantly.
	m.chatView.AddMessage(components.ChatMessage{
		Role:      role,
		Timestamp: time.Now(),
		ToolCalls: toolCalls,
	})

	if m.streamCfg.Speed == StreamInstant {
		// Show everything immediately.
		m.chatView.UpdateLastMessage(content)
		m.streaming = false
		m.waiting = false
		m.vimMode = false
		m.input.SetEnabled(true)
		m.statusBar.Extra = ""
		m.statusBar.Hints = defaultHints()
		return m, nil
	}

	// Start simulated streaming using runes for Unicode safety.
	m.streamBuf = []rune(content)
	m.streamPos = 0
	m.streaming = true

	return m, streamTickCmd(m.streamCfg.TickRate)
}

// handleStreamTick progressively renders the response.
func (m ChatModel) handleStreamTick() (tea.Model, tea.Cmd) {
	if !m.streaming {
		return m, nil
	}

	// Advance by a chunk of runes (not bytes) for Unicode safety.
	end := m.streamPos + m.streamCfg.ChunkSize
	if end >= len(m.streamBuf) {
		end = len(m.streamBuf)
	}

	m.streamPos = end
	m.chatView.UpdateLastMessage(string(m.streamBuf[:m.streamPos]))

	if m.streamPos >= len(m.streamBuf) {
		// Done streaming.
		m.streaming = false
		m.waiting = false
		m.vimMode = false
		m.input.SetEnabled(true)
		m.statusBar.Extra = ""
		m.statusBar.Hints = defaultHints()
		return m, nil
	}

	return m, streamTickCmd(m.streamCfg.TickRate)
}

// handleSlashCommand processes a slash command.
func (m ChatModel) handleSlashCommand(cmd string, args []string) (tea.Model, tea.Cmd) {
	switch cmd {
	case "/help":
		m.chatView.AddMessage(components.ChatMessage{
			Role: components.RoleSystem,
			Content: `Available commands:
  /help      - Show this help
  /clear     - Clear conversation
  /quit      - Exit alfred-ai
  /cancel    - Cancel active request
  /speed     - Cycle streaming speed (normal/fast/instant)
  /privacy   - Show privacy settings
  /export    - Export memory data
  /delete    - Delete memory entries

Keybindings:
  Enter      - Send message
  Alt+Enter  - New line
  Ctrl+T     - Toggle tool pane
  Tab        - Switch pane focus
  Ctrl+N/P   - Next/prev tab
  Ctrl+L     - Clear conversation
  Ctrl+C     - Cancel/Quit
  PgUp/PgDn  - Scroll chat`,
		})
		return m, nil

	case "/quit", "/exit":
		m.quitting = true
		return m, tea.Quit

	case "/clear":
		m.chatView.Clear()
		m.toolPane.Clear()
		if m.deps.OnClear != nil {
			m.deps.OnClear()
		}
		m.chatView.AddMessage(components.ChatMessage{
			Role:    components.RoleSystem,
			Content: theme.SymbolSuccess + " Session cleared.",
		})
		return m, nil

	case "/cancel":
		if m.waiting {
			m.cancelRequest("Request cancelled.")
		} else {
			m.chatView.AddMessage(components.ChatMessage{
				Role:    components.RoleSystem,
				Content: "No active request to cancel.",
			})
		}
		return m, nil

	case "/privacy":
		return m.handlePrivacy()

	case "/export":
		return m.handleExport(args)

	case "/delete":
		return m.handleDelete(args)

	case "/speed":
		newSpeed := CycleStreamSpeed(m.streamCfg.Speed)
		m.streamCfg = StreamConfigForSpeed(newSpeed)
		m.chatView.AddMessage(components.ChatMessage{
			Role:    components.RoleSystem,
			Content: fmt.Sprintf("Streaming speed: %s", newSpeed),
		})
		return m, nil

	default:
		m.chatView.AddMessage(components.ChatMessage{
			Role:    components.RoleSystem,
			Content: fmt.Sprintf("Unknown command: %s. Type /help for available commands.", cmd),
		})
		return m, nil
	}
}

func (m ChatModel) handlePrivacy() (tea.Model, tea.Cmd) {
	if m.deps.Privacy == nil {
		m.chatView.AddMessage(components.ChatMessage{
			Role:    components.RoleSystem,
			Content: "Privacy controls not configured.",
		})
		return m, nil
	}

	consent := m.deps.Privacy.GetConsent()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Consent: granted=%v", consent.Granted))
	if consent.GrantedAt != "" {
		sb.WriteString(fmt.Sprintf(" (at %s)", consent.GrantedAt))
	}
	sb.WriteString("\n")

	info := m.deps.Privacy.DataFlow()
	if len(info.Flows) > 0 {
		sb.WriteString("\nData flows:\n")
		for _, f := range info.Flows {
			enc := ""
			if f.Encrypted {
				enc = " [encrypted]"
			}
			sb.WriteString(fmt.Sprintf("  %s %s %s %s (%s)%s\n",
				theme.SymbolBullet, f.Source, theme.SymbolArrowR, f.Destination, f.Purpose, enc))
		}
	}

	m.chatView.AddMessage(components.ChatMessage{
		Role:    components.RoleSystem,
		Content: sb.String(),
	})
	return m, nil
}

func (m ChatModel) handleExport(args []string) (tea.Model, tea.Cmd) {
	if m.deps.Privacy == nil {
		m.chatView.AddMessage(components.ChatMessage{
			Role:    components.RoleSystem,
			Content: "Privacy controls not configured.",
		})
		return m, nil
	}

	outputPath := "./data/export.json"
	if len(args) > 0 {
		outputPath = args[0]
	}

	result, err := m.deps.Privacy.Export(context.Background(), outputPath)
	if err != nil {
		m.chatView.AddMessage(components.ChatMessage{
			Role:    components.RoleError,
			Content: fmt.Sprintf("Export failed: %v", err),
		})
		return m, nil
	}
	m.chatView.AddMessage(components.ChatMessage{
		Role:    components.RoleSystem,
		Content: fmt.Sprintf("%s Exported %d entries to %s", theme.SymbolSuccess, result.EntryCount, result.Path),
	})
	return m, nil
}

func (m ChatModel) handleDelete(args []string) (tea.Model, tea.Cmd) {
	if m.deps.Privacy == nil {
		m.chatView.AddMessage(components.ChatMessage{
			Role:    components.RoleSystem,
			Content: "Privacy controls not configured.",
		})
		return m, nil
	}

	if len(args) < 1 {
		m.chatView.AddMessage(components.ChatMessage{
			Role:    components.RoleSystem,
			Content: "Usage: /delete <id> or /delete all",
		})
		return m, nil
	}

	target := args[0]
	if strings.ToLower(target) == "all" {
		if err := m.deps.Privacy.DeleteAll(context.Background()); err != nil {
			m.chatView.AddMessage(components.ChatMessage{
				Role:    components.RoleError,
				Content: fmt.Sprintf("Delete all failed: %v", err),
			})
			return m, nil
		}
		m.chatView.AddMessage(components.ChatMessage{
			Role:    components.RoleSystem,
			Content: theme.SymbolSuccess + " All memory entries deleted.",
		})
		return m, nil
	}

	if err := m.deps.Privacy.DeleteEntry(context.Background(), target); err != nil {
		m.chatView.AddMessage(components.ChatMessage{
			Role:    components.RoleError,
			Content: fmt.Sprintf("Delete failed: %v", err),
		})
		return m, nil
	}
	m.chatView.AddMessage(components.ChatMessage{
		Role:    components.RoleSystem,
		Content: fmt.Sprintf("%s Entry %s deleted.", theme.SymbolSuccess, target),
	})
	return m, nil
}

// cancelRequest cancels the in-flight handler goroutine, bumps the generation
// counter so any stale responses are discarded, and resets the UI state.
func (m *ChatModel) cancelRequest(reason string) {
	if m.cancelFn != nil {
		m.cancelFn()
		m.cancelFn = nil
	}
	m.gen++ // ensure stale OutboundMsg / HandlerDoneMsg are ignored
	m.waiting = false
	m.streaming = false
	m.vimMode = false
	m.input.SetEnabled(true)
	m.statusBar.Extra = ""
	m.statusBar.Hints = defaultHints()
	m.pendingTools = nil
	m.toolStartTimes = make(map[string]time.Time)
	m.chatView.AddMessage(components.ChatMessage{
		Role:    components.RoleSystem,
		Content: reason,
	})
}

func defaultHints() []components.KeyHint {
	return []components.KeyHint{
		{Key: "Enter", Desc: "Send"},
		{Key: "Alt+Enter", Desc: "Newline"},
		{Key: "Ctrl+T", Desc: "Tools"},
		{Key: "?", Desc: "/help"},
		{Key: "Ctrl+C", Desc: "Quit"},
	}
}
