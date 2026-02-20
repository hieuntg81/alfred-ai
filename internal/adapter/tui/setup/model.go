package setup

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	agentsetup "alfred-ai/cmd/agent/setup"
	"alfred-ai/internal/adapter/tui/components"
	"alfred-ai/internal/adapter/tui/components/wizard"
	"alfred-ai/internal/adapter/tui/theme"
	"alfred-ai/internal/infra/config"
)

// listItem implements list.Item for the bubbles list component.
type listItem struct {
	title string
	desc  string
	id    string
}

func (i listItem) Title() string       { return i.title }
func (i listItem) Description() string { return i.desc }
func (i listItem) FilterValue() string { return i.title }

// WizardModel is the root Bubble Tea model for the setup wizard.
type WizardModel struct {
	// Phase management
	phase Phase
	steps wizard.StepIndicatorModel

	// Sub-models
	list      list.Model
	field     wizard.FormFieldModel
	validator wizard.APIValidatorModel
	spinner   spinner.Model

	// State
	cfg            *config.Config
	template       *agentsetup.Template
	provider       string
	apiKey         string
	model          string
	models         []agentsetup.ModelInfo
	cancelled      bool
	width          int
	height         int
	fetchingModels bool
}

// NewWizardModel creates the wizard model.
func NewWizardModel() WizardModel {
	phases := AllPhases()
	var steps []wizard.Step
	for _, p := range phases {
		steps = append(steps, wizard.Step{Name: p.Name})
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(theme.ColorInfo)

	return WizardModel{
		phase:     PhaseWelcome,
		steps:     wizard.NewStepIndicator(steps),
		validator: wizard.NewAPIValidator(),
		spinner:   s,
		cfg:       config.Defaults(),
	}
}

// Config returns the built configuration (call after Run completes).
func (m WizardModel) Config() *config.Config {
	return m.cfg
}

// Cancelled reports whether the user cancelled the wizard.
func (m WizardModel) Cancelled() bool {
	return m.cancelled
}

// Init initializes the wizard.
func (m WizardModel) Init() tea.Cmd {
	return m.spinner.Tick
}

// Update handles messages.
func (m WizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.steps.SetWidth(m.width - 4)
		if m.list.Items() != nil {
			m.list.SetSize(m.width-4, m.height-12)
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			m.cancelled = true
			return m, tea.Quit
		case tea.KeyEsc:
			if m.phase > PhaseWelcome {
				return m.prevPhase()
			}
			m.cancelled = true
			return m, tea.Quit
		}

	case PhaseAdvanceMsg:
		return m.nextPhase()

	case ValidationResultMsg:
		m.validator.HandleResult(msg.Err)
		if msg.Err == nil {
			// Validation succeeded, fetch models.
			m.fetchingModels = true
			return m, fetchModelsCmd(m.provider, m.apiKey)
		}
		return m, nil

	case ModelsResultMsg:
		m.models = msg.Models
		m.fetchingModels = false
		return m.enterPhase(PhaseLLMModel)
	}

	// Delegate to phase-specific update.
	switch m.phase {
	case PhaseWelcome:
		return m.updateWelcome(msg)
	case PhaseTemplate:
		return m.updateTemplate(msg)
	case PhaseLLM:
		return m.updateLLMProvider(msg)
	case PhaseLLMKey:
		return m.updateLLMKey(msg)
	case PhaseLLMModel:
		return m.updateLLMModel(msg)
	case PhaseChannels:
		return m.updateChannels(msg)
	case PhaseSecurity:
		return m.updateSecurity(msg)
	case PhaseCompletion:
		return m.updateCompletion(msg)
	}

	// Spinner
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// View renders the current phase.
func (m WizardModel) View() string {
	if m.width == 0 {
		return "  Initializing..."
	}

	// Title + step indicator.
	title := theme.WizardTitle.Render("Alfred-AI Setup")
	stepView := m.steps.View()

	var content string
	switch m.phase {
	case PhaseWelcome:
		content = m.viewWelcome()
	case PhaseTemplate:
		content = m.viewTemplate()
	case PhaseLLM:
		content = m.viewLLMProvider()
	case PhaseLLMKey:
		content = m.viewLLMKey()
	case PhaseLLMModel:
		content = m.viewLLMModel()
	case PhaseChannels:
		content = m.viewChannels()
	case PhaseSecurity:
		content = m.viewSecurity()
	case PhaseCompletion:
		content = m.viewCompletion()
	}

	// Footer hints.
	hints := []components.KeyHint{
		{Key: "Esc", Desc: "Back"},
		{Key: "Enter", Desc: "Select"},
		{Key: "Ctrl+C", Desc: "Quit"},
	}
	sb := components.NewStatusBar()
	sb.Hints = hints
	sb.SetWidth(m.width)
	footer := sb.View()

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		stepView,
		"",
		content,
		"",
		footer,
	)
}

// --- Phase navigation ---

func (m WizardModel) nextPhase() (tea.Model, tea.Cmd) {
	next := m.phase + 1
	if next >= PhaseCount {
		return m, tea.Quit
	}

	// Skip security phase for non-secure templates.
	if next == PhaseSecurity && (m.template == nil || m.template.ID != "secure-private") {
		next = PhaseCompletion
	}

	return m.enterPhase(next)
}

func (m WizardModel) prevPhase() (tea.Model, tea.Cmd) {
	prev := m.phase - 1
	if prev < PhaseWelcome {
		prev = PhaseWelcome
	}

	// Skip security phase going back.
	if prev == PhaseSecurity && (m.template == nil || m.template.ID != "secure-private") {
		prev = PhaseChannels
	}

	return m.enterPhase(prev)
}

func (m WizardModel) enterPhase(p Phase) (tea.Model, tea.Cmd) {
	m.phase = p
	m.steps.SetCurrent(int(p))

	switch p {
	case PhaseTemplate:
		m.list = m.buildTemplateList()
	case PhaseLLM:
		m.list = m.buildProviderList()
	case PhaseLLMKey:
		url, _ := agentsetup.GetAPIKeyInstructions(m.provider)
		m.field = wizard.NewSecretField(
			fmt.Sprintf("Enter your %s API key:", strings.ToUpper(m.provider)),
			"sk-...",
		)
		m.field.Description = fmt.Sprintf("Get your key at: %s", url)
		m.validator.Reset()
		return m, nil
	case PhaseLLMModel:
		m.list = m.buildModelList()
	case PhaseSecurity:
		m.field = wizard.NewSecretField(
			"Enter encryption passphrase (or leave empty to skip):",
			"minimum 12 characters",
		)
	}

	return m, nil
}

// --- Phase updates ---

func (m WizardModel) updateWelcome(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEnter {
		return m.nextPhase()
	}
	return m, nil
}

func (m WizardModel) updateTemplate(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEnter {
		if item, ok := m.list.SelectedItem().(listItem); ok {
			tmpl := agentsetup.GetTemplateByID(item.id)
			if tmpl != nil {
				m.template = tmpl
				m.cfg = tmpl.Config
			}
			return m.nextPhase()
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m WizardModel) updateLLMProvider(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEnter {
		if item, ok := m.list.SelectedItem().(listItem); ok {
			m.provider = item.id
			return m.nextPhase()
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m WizardModel) updateLLMKey(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case wizard.FieldSubmitMsg:
		m.apiKey = msg.Value
		if m.apiKey == "" {
			m.field.SetError("API key cannot be empty")
			return m, nil
		}
		m.field.ClearError()
		m.validator.Start()
		return m, tea.Batch(
			m.spinner.Tick,
			validateAPIKeyCmd(m.provider, m.apiKey),
		)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.validator, cmd = m.validator.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.field, cmd = m.field.Update(msg)
	return m, cmd
}

func (m WizardModel) updateLLMModel(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEnter {
		if item, ok := m.list.SelectedItem().(listItem); ok {
			m.model = item.id
			// Apply to config.
			m.cfg.LLM.DefaultProvider = m.provider
			m.cfg.LLM.Providers = []config.ProviderConfig{
				{
					Name:   m.provider,
					Type:   m.provider,
					Model:  m.model,
					APIKey: m.apiKey,
				},
			}
			return m.nextPhase()
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m WizardModel) updateChannels(msg tea.Msg) (tea.Model, tea.Cmd) {
	// For now, skip channel config and use template defaults.
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEnter {
		return m.nextPhase()
	}
	return m, nil
}

func (m WizardModel) updateSecurity(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Security phase: enter encryption passphrase.
	switch msg := msg.(type) {
	case wizard.FieldSubmitMsg:
		if msg.Value != "" && len(msg.Value) < 12 {
			m.field.SetError("Passphrase should be at least 12 characters for strong security")
			return m, nil
		}
		if msg.Value != "" {
			m.cfg.Security.Encryption.Enabled = true
		}
		return m.nextPhase()
	}

	var cmd tea.Cmd
	m.field, cmd = m.field.Update(msg)
	return m, cmd
}

func (m WizardModel) updateCompletion(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEnter {
		return m, tea.Quit
	}
	return m, nil
}

// --- Phase views ---

func (m WizardModel) viewWelcome() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		theme.Bold.Render("Welcome to Alfred-AI!"),
		"",
		"This wizard will help you set up your AI assistant.",
		"",
		theme.TextMuted.Render("What you'll configure:"),
		"  "+theme.SymbolBullet+" Choose a template for your use case",
		"  "+theme.SymbolBullet+" Set up your AI provider (OpenAI, Anthropic, etc.)",
		"  "+theme.SymbolBullet+" Configure channels and security",
		"",
		theme.TextInfo.Render("Press Enter to begin"),
	)
}

func (m WizardModel) viewTemplate() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		theme.Bold.Render("Choose a template:"),
		"",
		m.list.View(),
	)
}

func (m WizardModel) viewLLMProvider() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		theme.Bold.Render("Choose your AI provider:"),
		"",
		m.list.View(),
	)
}

func (m WizardModel) viewLLMKey() string {
	parts := []string{
		m.field.View(),
	}

	if m.validator.Validating || m.validator.Success || m.validator.ErrMsg != "" {
		parts = append(parts, "", m.validator.View())
	}

	if m.fetchingModels {
		parts = append(parts, "", m.spinner.View()+" Fetching available models...")
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m WizardModel) viewLLMModel() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		theme.Bold.Render("Choose your model:"),
		"",
		m.list.View(),
	)
}

func (m WizardModel) viewChannels() string {
	chNames := "CLI only"
	if m.template != nil && len(m.template.Config.Channels) > 0 {
		var names []string
		for _, ch := range m.template.Config.Channels {
			names = append(names, ch.Type)
		}
		chNames = strings.Join(names, ", ")
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		theme.Bold.Render("Channels"),
		"",
		fmt.Sprintf("Channels from template: %s", theme.TextInfo.Render(chNames)),
		"",
		theme.TextMuted.Render("Press Enter to continue"),
	)
}

func (m WizardModel) viewSecurity() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		theme.Bold.Render("Security Configuration"),
		"",
		theme.TextMuted.Render("AES-256-GCM encryption protects your data at rest."),
		"",
		m.field.View(),
	)
}

func (m WizardModel) viewCompletion() string {
	providerName := m.provider
	if providerName == "" {
		providerName = "default"
	}
	modelName := m.model
	if modelName == "" {
		modelName = "default"
	}

	templateName := "Custom"
	if m.template != nil {
		templateName = m.template.Name
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		theme.TextSuccess.Render(theme.SymbolSuccess+" Setup Complete!"),
		"",
		theme.Bold.Render("Configuration Summary:"),
		fmt.Sprintf("  Template:  %s", theme.TextInfo.Render(templateName)),
		fmt.Sprintf("  Provider:  %s", theme.TextInfo.Render(providerName)),
		fmt.Sprintf("  Model:     %s", theme.TextInfo.Render(modelName)),
		fmt.Sprintf("  Memory:    %s", theme.TextInfo.Render(m.cfg.Memory.Provider)),
		"",
		theme.Bold.Render("Next steps:"),
		"  1. Run: "+theme.TextInfo.Render("./alfred-ai"),
		"  2. Type a message to start chatting",
		"  3. Use /help for available commands",
		"",
		theme.TextInfo.Render("Press Enter to save and exit"),
	)
}

// --- List builders ---

func (m WizardModel) buildTemplateList() list.Model {
	templates := agentsetup.GetTemplates()
	var items []list.Item
	for _, t := range templates {
		items = append(items, listItem{
			title: t.Name,
			desc:  fmt.Sprintf("[%s, ~%d min] %s", t.Difficulty, t.EstimateMin, t.Description),
			id:    t.ID,
		})
	}

	l := list.New(items, list.NewDefaultDelegate(), m.width-4, m.height-12)
	l.Title = ""
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)
	l.Styles.Title = lipgloss.NewStyle()
	return l
}

func (m WizardModel) buildProviderList() list.Model {
	providers := Providers()
	var items []list.Item
	for _, p := range providers {
		items = append(items, listItem{
			title: p.Name,
			desc:  p.Description,
			id:    p.ID,
		})
	}

	l := list.New(items, list.NewDefaultDelegate(), m.width-4, m.height-12)
	l.Title = ""
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(true)
	return l
}

func (m WizardModel) buildModelList() list.Model {
	var items []list.Item

	// Mark recommended model.
	recommended := agentsetup.RecommendedModel(m.provider)
	for _, model := range m.models {
		desc := model.Description
		if model.ID == recommended {
			desc += " (Recommended)"
		}
		items = append(items, listItem{
			title: model.ID,
			desc:  desc,
			id:    model.ID,
		})
	}

	if len(items) == 0 {
		items = append(items, listItem{
			title: recommended,
			desc:  "Default model",
			id:    recommended,
		})
	}

	l := list.New(items, list.NewDefaultDelegate(), m.width-4, m.height-12)
	l.Title = ""
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)
	return l
}
