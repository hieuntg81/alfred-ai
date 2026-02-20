package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"alfred-ai/cmd/agent/daemon"
	"alfred-ai/cmd/agent/setup"
	"alfred-ai/internal/adapter/channel"
	"alfred-ai/internal/adapter/llm"
	"alfred-ai/internal/adapter/memory"
	"alfred-ai/internal/adapter/tool"
	"alfred-ai/internal/adapter/tui/chat"
	"alfred-ai/internal/adapter/tui/dashboard"
	tuisetup "alfred-ai/internal/adapter/tui/setup"
	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
	"alfred-ai/internal/infra/logger"
	"alfred-ai/internal/infra/tracer"
	"alfred-ai/internal/usecase"
	"alfred-ai/internal/usecase/eventbus"
	"alfred-ai/internal/usecase/multiagent"
)

func main() {
	// Handle help flag first
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "--help", "-h", "help":
			showUsage()
			return
		}
	}

	if len(os.Args) < 2 || strings.HasPrefix(os.Args[1], "-") {
		if err := run(); err != nil {
			fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
			os.Exit(1)
		}
		return
	}

	switch os.Args[1] {
	case "setup":
		if err := runSetup(); err != nil {
			fmt.Fprintf(os.Stderr, "setup: %v\n", err)
			os.Exit(1)
		}
	case "dashboard":
		if err := runDashboard(); err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: %v\n", err)
			os.Exit(1)
		}
	case "daemon":
		if err := runDaemon(); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: %v\n", err)
			os.Exit(1)
		}
	case "plugin":
		if err := runPlugin(); err != nil {
			fmt.Fprintf(os.Stderr, "plugin: %v\n", err)
			os.Exit(1)
		}
	case "doctor":
		if err := runDoctor(); err != nil {
			fmt.Fprintf(os.Stderr, "doctor: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\nRun 'alfred-ai --help' for usage information.\n", os.Args[1])
		os.Exit(1)
	}
}

func showUsage() {
	fmt.Println(`alfred-ai - Privacy-first AI agent framework

USAGE:
    alfred-ai [COMMAND] [FLAGS]

COMMANDS:
    setup       Launch interactive setup wizard
    dashboard   Launch monitoring dashboard
    daemon      Manage alfred-ai as system service
                Subcommands: install, uninstall, status
    plugin      Plugin development tools
                Subcommands: list, validate, init
    doctor      Run health checks on your setup

    (no command) - Run bot with existing config

FLAGS:
    -h, --help         Show this help message
    --config PATH      Specify config file path (default: ./config.yaml)
    --provider NAME    LLM provider (openai, anthropic, gemini, openrouter)
    --model NAME       Model name (e.g. gpt-4o, claude-sonnet-4-5-20250929)
    --key KEY          API key for the provider

CONFIGURATION:
    Config file: ./config.yaml
    Environment: ALFREDAI_* variables override config

EXAMPLES:
    alfred-ai setup              # Interactive setup
    alfred-ai                    # Run with config.yaml
    alfred-ai --config /path/to/config.yaml    # Run with custom config
    alfred-ai --provider openai --model gpt-4o --key sk-...  # Quick start
    alfred-ai daemon install     # Install as system service
    alfred-ai doctor             # Check system health

LEARN MORE:
    Documentation: ./docs/
    Quick Start:   ./docs/getting-started.md`)
}

func showFirstRunMessage() {
	fmt.Println(`👋 Welcome to alfred-ai!

No configuration found. Let's get you started:

Option 1 (Recommended): Guided Setup
  Run: alfred-ai setup
  Time: ~5 minutes
  • Interactive wizard with helpful explanations
  • Pre-configured templates for common use cases
  • Automatic testing to verify everything works

Option 2: Manual Configuration
  Run: alfred-ai --help
  Create config.yaml manually following the documentation

Option 3: Quick Start with Environment Variables
  Set these environment variables:
    ALFREDAI_LLM_DEFAULT_PROVIDER=openai
    ALFREDAI_LLM_PROVIDER_OPENAI_API_KEY=sk-...
  Then run: alfred-ai

For most users, we recommend starting with 'alfred-ai setup'.`)
}

// cliFlags holds optional CLI flags that can bypass the setup wizard.
type cliFlags struct {
	Provider string
	Model    string
	APIKey   string
}

// parseFlags extracts --provider, --model, --key from os.Args.
func parseFlags() cliFlags {
	var flags cliFlags
	for i := 1; i < len(os.Args); i++ {
		switch {
		case os.Args[i] == "--provider" && i+1 < len(os.Args):
			flags.Provider = os.Args[i+1]
			i++
		case strings.HasPrefix(os.Args[i], "--provider="):
			flags.Provider = strings.TrimPrefix(os.Args[i], "--provider=")
		case os.Args[i] == "--model" && i+1 < len(os.Args):
			flags.Model = os.Args[i+1]
			i++
		case strings.HasPrefix(os.Args[i], "--model="):
			flags.Model = strings.TrimPrefix(os.Args[i], "--model=")
		case os.Args[i] == "--key" && i+1 < len(os.Args):
			flags.APIKey = os.Args[i+1]
			i++
		case strings.HasPrefix(os.Args[i], "--key="):
			flags.APIKey = strings.TrimPrefix(os.Args[i], "--key=")
		}
	}
	return flags
}

// buildQuickConfig creates a minimal config from CLI flags, bypassing the
// setup wizard and config file loading.
func buildQuickConfig(flags cliFlags) (*config.Config, error) {
	if flags.Provider == "" || flags.Model == "" || flags.APIKey == "" {
		return nil, fmt.Errorf("--provider, --model, and --key must all be specified")
	}

	cfg := config.Defaults()
	cfg.LLM.DefaultProvider = flags.Provider
	cfg.LLM.Providers = []config.ProviderConfig{
		{
			Name:   flags.Provider,
			Type:   flags.Provider,
			Model:  flags.Model,
			APIKey: flags.APIKey,
		},
	}

	config.ApplyEnvOverrides(cfg)
	return cfg, nil
}

func run() error {
	// 1. Config
	flags := parseFlags()

	var cfg *config.Config
	var err error

	if flags.Provider != "" {
		// Quick start via CLI flags — skip wizard / config file.
		cfg, err = buildQuickConfig(flags)
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}
	} else {
		cfgPath := configPath()

		// Check for first run (no config file)
		if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
			showFirstRunMessage()
			return nil
		}

		cfg, err = config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}
	}

	// 2. Logger & Tracer
	log, logCloser, err := logger.New(cfg.Logger)
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}
	defer logCloser()

	ctx := context.Background()
	tracerShutdown, err := tracer.Setup(ctx, cfg.Tracer)
	if err != nil {
		return fmt.Errorf("tracer: %w", err)
	}
	defer tracerShutdown(ctx)

	// 3. Security (sandbox, encryption, audit)
	security, securityCleanup, err := initSecurity(cfg, log)
	if err != nil {
		return fmt.Errorf("security: %w", err)
	}
	defer securityCleanup()

	// 4. LLM providers
	llmComponents, err := initLLM(cfg, log)
	if err != nil {
		return fmt.Errorf("llm: %w", err)
	}

	// 5. Event bus
	bus := eventbus.New(log)
	defer bus.Close()

	// 6. Memory
	var contentEnc domain.ContentEncryptor
	if security.Encryptor != nil {
		contentEnc = security.Encryptor
	}
	mem, memCloser, err := initMemory(cfg.Memory, log, contentEnc)
	if err != nil {
		return fmt.Errorf("memory: %w", err)
	}
	if memCloser != nil {
		defer memCloser()
	}

	// 7. Agent components
	agentComp, err := initAgent(ctx, cfg, llmComponents.DefaultLLM, llmComponents.Registry, mem, security, bus, log)
	if err != nil {
		return fmt.Errorf("agent: %w", err)
	}

	// 8. Features (plugins, nodes, privacy, curator)
	features, err := initFeatures(ctx, cfg, llmComponents.DefaultLLM, mem, agentComp.ToolRegistry, security, bus, log)
	if err != nil {
		return fmt.Errorf("features: %w", err)
	}

	// 9. Runtime (router, channels, scheduler, gateway)
	runtime, runtimeCleanup, err := initRuntime(ctx, cfg, llmComponents.Registry, llmComponents.DefaultLLM,
		agentComp, features, security, mem, bus, log)
	if err != nil {
		return fmt.Errorf("runtime: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := runtimeCleanup(shutdownCtx); err != nil {
			log.Error("runtime cleanup error", "error", err)
		}
	}()

	// 10. Graceful shutdown
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 11. Start scheduler
	if runtime.Scheduler != nil {
		go runtime.Scheduler.Start(ctx)
	}

	// 12. Start gateway
	if runtime.Gateway != nil {
		go func() {
			if err := runtime.Gateway.Start(ctx); err != nil {
				log.Error("gateway server error", "error", err)
			}
		}()
	}

	// 13. Create message handler
	handler := func(sendFn func(context.Context, domain.OutboundMessage) error) domain.MessageHandler {
		return func(ctx context.Context, msg domain.InboundMessage) error {
			out, err := runtime.Router.Handle(ctx, msg)
			if err != nil {
				return sendFn(ctx, domain.OutboundMessage{
					SessionID: msg.SessionID,
					Content:   fmt.Sprintf("%v", err),
					IsError:   true,
				})
			}
			return sendFn(ctx, out)
		}
	}

	// 14. Start
	log.Info("alfred-ai starting",
		"provider", cfg.LLM.DefaultProvider,
		"memory", mem.Name(),
		"tools", len(agentComp.ToolRegistry.List()),
		"encryption", security.Encryptor != nil,
		"audit", security.AuditLogger != nil,
		"channels", len(runtime.Channels),
	)

	// Start channels
	if len(runtime.Channels) == 1 {
		// Single channel (usually CLI): block on it
		ch := runtime.Channels[0]
		return ch.Start(ctx, handler(ch.Send))
	}

	// Multiple channels: start all in parallel
	var wg sync.WaitGroup
	errCh := make(chan error, len(runtime.Channels))

	for _, ch := range runtime.Channels {
		wg.Add(1)
		go func(c domain.Channel) {
			defer wg.Done()
			if err := c.Start(ctx, handler(c.Send)); err != nil {
				errCh <- fmt.Errorf("channel %s: %w", c.Name(), err)
			}
		}(ch)
	}

	// Wait for context cancellation
	<-ctx.Done()

	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}
func configPath() string {
	// Check --config flag in os.Args.
	for i, arg := range os.Args {
		if arg == "--config" && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
		if strings.HasPrefix(arg, "--config=") {
			return strings.TrimPrefix(arg, "--config=")
		}
	}
	if p := os.Getenv("ALFREDAI_CONFIG"); p != "" {
		return p
	}
	return "config.yaml"
}

func runSetup() error {
	model := tuisetup.NewWizardModel()
	p := tea.NewProgram(model, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		return fmt.Errorf("setup wizard: %w", err)
	}

	wizardResult, ok := result.(tuisetup.WizardModel)
	if !ok {
		return fmt.Errorf("unexpected wizard result type")
	}

	if wizardResult.Cancelled() {
		fmt.Println("Setup cancelled.")
		return nil
	}

	cfg := wizardResult.Config()
	path := configPath()
	return setup.SaveConfig(cfg, path, os.Stdout)
}

func runDaemon() error {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: alfred-ai daemon <install|uninstall|status>")
		os.Exit(1)
	}

	switch os.Args[2] {
	case "install":
		cfg := daemon.DefaultConfig()
		cfg.ConfigPath = configPath()
		if err := cfg.Validate(); err != nil {
			return err
		}
		return daemon.Install(cfg)
	case "uninstall":
		return daemon.Uninstall("alfred-ai")
	case "status":
		status, err := daemon.Status("alfred-ai")
		if err != nil {
			return err
		}
		if status.Running {
			fmt.Printf("alfred-ai is running (PID %d)\n", status.PID)
		} else {
			fmt.Println("alfred-ai is not running")
		}
		return nil
	default:
		return fmt.Errorf("unknown daemon command: %s (want: install, uninstall, status)", os.Args[2])
	}
}

func runDashboard() error {
	cfgPath := configPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("config: %w (run 'alfred-ai setup' first)", err)
	}

	// Read config file as YAML string for the config tab.
	configYAML, _ := os.ReadFile(cfgPath)

	// Initialize minimal components for the dashboard.
	log, logCloser, err := logger.New(cfg.Logger)
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}
	defer logCloser()

	bus := eventbus.New(log)
	defer bus.Close()

	var mem domain.MemoryProvider
	mem, memCloser, err := initMemory(cfg.Memory, log, nil)
	if err != nil {
		log.Warn("memory init failed, dashboard will show limited info", "error", err)
		mem = nil
	}
	if memCloser != nil {
		defer memCloser()
	}

	deps := dashboard.DashboardDeps{
		Bus:    bus,
		Memory: mem,
		Config: string(configYAML),
	}

	model := dashboard.NewDashboardModel(deps)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	model.SetProgramSender(func(msg tea.Msg) { p.Send(msg) })

	_, err = p.Run()
	return err
}

// initMemory creates the appropriate memory provider based on config.
// Returns the provider, an optional closer (for vector store), and any error.
func initMemory(cfg config.MemoryConfig, log *slog.Logger, enc domain.ContentEncryptor) (domain.MemoryProvider, func() error, error) {
	switch cfg.Provider {
	case "markdown":
		var opts []memory.MarkdownOption
		if enc != nil {
			opts = append(opts, memory.WithEncryptor(enc))
		}
		mem, err := memory.NewMarkdownMemory(cfg.DataDir, opts...)
		return mem, nil, err
	case "vector":
		return buildVectorMemory(cfg, log)
	case "byterover":
		client := memory.NewMockByteRoverClient() // TODO: replace with real client when API is ready
		var opts []memory.ByteRoverOption
		if enc != nil {
			opts = append(opts, memory.WithByteRoverEncryptor(enc))
		}
		return memory.NewByteRoverMemory(client, log, opts...), nil, nil
	case "noop", "":
		return memory.NewNoopMemory(), nil, nil
	default:
		return nil, nil, fmt.Errorf("unknown memory provider: %s", cfg.Provider)
	}
}

// createLLMProvider creates an LLM provider based on the type field.
func createLLMProvider(pc config.ProviderConfig, log *slog.Logger) (domain.LLMProvider, error) {
	// Edge builds only support Ollama (local inference).
	if edgeBuild && pc.Type != "ollama" {
		return nil, fmt.Errorf("edge build only supports ollama provider (got %q)", pc.Type)
	}

	switch pc.Type {
	case "openai", "":
		return llm.NewOpenAIProvider(pc, log), nil
	case "anthropic":
		return llm.NewAnthropicProvider(pc, log), nil
	case "gemini":
		return llm.NewGeminiProvider(pc, log), nil
	case "openrouter":
		return llm.NewOpenRouterProvider(pc, log), nil
	case "ollama":
		return llm.NewOllamaProvider(pc, log), nil
	case "bedrock":
		return createBedrockProvider(pc, log)
	default:
		return nil, fmt.Errorf("unknown provider type: %s", pc.Type)
	}
}

// initMultiAgent sets up the multi-agent architecture: workspace, registry,
// per-agent instances, broker, delegate tools, and router strategy.
func initMultiAgent(
	cfg *config.Config,
	llmRegistry *llm.Registry,
	toolRegistry *tool.Registry,
	mem domain.MemoryProvider,
	bus domain.EventBus,
	auditLogger domain.AuditLogger,
	compressor *usecase.Compressor,
	approver domain.ToolApprover,
	log *slog.Logger,
) (*usecase.Router, *multiagent.Registry) {
	agentsCfg := cfg.Agents

	// 1. Create workspace for per-agent directories.
	workspaceDir := "./data"
	if agentsCfg.DataDir != "" {
		workspaceDir = agentsCfg.DataDir
	}
	workspace := multiagent.NewWorkspace(workspaceDir)

	// 2. Create registry.
	registry := multiagent.NewRegistry(agentsCfg.Default, log)

	// 3. Create broker for cross-agent delegation.
	broker := multiagent.NewBroker(registry, bus, log)

	// 4. Register each agent instance.
	for _, instCfg := range agentsCfg.Instances {
		// Resolve LLM provider.
		provider := instCfg.Provider
		if provider == "" {
			provider = cfg.LLM.DefaultProvider
		}
		agentLLM, err := llmRegistry.Get(provider)
		if err != nil {
			log.Error("multi-agent: failed to resolve provider", "agent_id", instCfg.ID, "provider", provider, "error", err)
			continue
		}

		// Create scoped tool executor.
		scopedTools := usecase.NewScopedToolExecutor(toolRegistry, instCfg.Tools)

		// Create per-agent session directory.
		sessionDir, err := workspace.SessionDir(instCfg.ID)
		if err != nil {
			log.Error("multi-agent: failed to create session dir", "agent_id", instCfg.ID, "error", err)
			continue
		}
		agentSessions := usecase.NewSessionManager(sessionDir)

		// Build model name.
		agentModel := instCfg.Model
		if agentModel == "" && len(cfg.LLM.Providers) > 0 {
			for _, p := range cfg.LLM.Providers {
				if p.Name == provider {
					agentModel = p.Model
					break
				}
			}
		}

		// Build system prompt.
		systemPrompt := instCfg.SystemPrompt
		if systemPrompt == "" {
			systemPrompt = cfg.Agent.SystemPrompt
		}

		// Create context builder.
		ctxBuilder := usecase.NewContextBuilder(systemPrompt, agentModel, 50)

		// Set extended thinking budget from the agent's provider config.
		for _, p := range cfg.LLM.Providers {
			if p.Name == provider && p.ThinkingBudget > 0 {
				ctxBuilder.SetThinkingBudget(p.ThinkingBudget)
				break
			}
		}

		// Build identity.
		identity := domain.AgentIdentity{
			ID:           instCfg.ID,
			Name:         instCfg.Name,
			Description:  instCfg.Description,
			SystemPrompt: systemPrompt,
			Model:        agentModel,
			Provider:     provider,
			Tools:        instCfg.Tools,
			Skills:       instCfg.Skills,
			MaxIter:      instCfg.MaxIter,
			Metadata:     instCfg.Metadata,
		}

		// Create agent.
		agentInst := usecase.NewAgent(usecase.AgentDeps{
			LLM:            agentLLM,
			Memory:         mem,
			Tools:          scopedTools,
			ContextBuilder: ctxBuilder,
			Logger:         log.With("agent_id", instCfg.ID),
			MaxIterations:  cfg.Agent.MaxIterations,
			AuditLogger:    auditLogger,
			Compressor:     compressor,
			Bus:            bus,
			Approver:       approver,
			Identity:       identity,
		})

		// Get workspace dir for reference.
		agentDir, err := workspace.AgentDir(instCfg.ID)
		if err != nil {
			log.Error("multi-agent: failed to create agent dir", "agent_id", instCfg.ID, "error", err)
			continue
		}

		// Register agent instance.
		if err := registry.Register(&multiagent.AgentInstance{
			Identity:  identity,
			Agent:     agentInst,
			Sessions:  agentSessions,
			Workspace: agentDir,
		}); err != nil {
			log.Error("multi-agent: failed to register agent", "agent_id", instCfg.ID, "error", err)
			continue
		}
	}

	// Verify the default agent was successfully registered.
	if _, err := registry.Default(); err != nil {
		log.Error("multi-agent: default agent not registered, falling back may fail",
			"default", agentsCfg.Default, "error", err)
	}

	// 5. Register delegate tool on each agent (if >1 agent).
	// Note: the delegate tool is registered on the shared tool registry,
	// but since each agent uses ScopedToolExecutor, they only see it if
	// "delegate" is in their tools list. For convenience, we register it once.
	if len(agentsCfg.Instances) > 1 {
		// Use the first agent's ID as the default owner for the shared delegate tool.
		// In practice, the delegate tool's agentID is only used for Schema() display.
		delegateTool := tool.NewDelegateTool(broker, registry, agentsCfg.Default)
		if err := toolRegistry.Register(delegateTool); err != nil {
			log.Warn("multi-agent: failed to register delegate tool", "error", err)
		}
	}

	// 6. Build routing strategy.
	var agentRouter domain.AgentRouter
	switch agentsCfg.Routing {
	case "prefix":
		// Build name→ID map from registered agents.
		nameMap := make(map[string]string)
		for _, inst := range agentsCfg.Instances {
			nameMap[strings.ToLower(inst.Name)] = inst.ID
			nameMap[strings.ToLower(inst.ID)] = inst.ID
		}
		agentRouter = multiagent.NewPrefixRouterWithLogger(agentsCfg.Default, nameMap, log)
	case "config":
		var rules []multiagent.RoutingRule
		for _, rc := range agentsCfg.RoutingRules {
			rules = append(rules, multiagent.RoutingRule{
				Channel: rc.Channel,
				GroupID: rc.GroupID,
				AgentID: rc.AgentID,
			})
		}
		agentRouter = multiagent.NewConfigRouterWithLogger(agentsCfg.Default, rules, log)
	default: // "default" or empty
		agentRouter = multiagent.NewDefaultRouterWithLogger(agentsCfg.Default, log)
	}

	// 7. Create multi-agent router.
	router := usecase.NewMultiRouter(registry.Lookup(), agentRouter, bus, log)

	log.Info("multi-agent mode enabled",
		"agents", len(agentsCfg.Instances),
		"default", agentsCfg.Default,
		"routing", agentsCfg.Routing,
	)

	return router, registry
}

// buildChannels creates channels based on config. Returns all channels and the TUI channel (if any).
func buildChannels(cfg *config.Config, log *slog.Logger, privacyMgr domain.PrivacyController) ([]domain.Channel, *chat.TUIChannel, error) {
	// Default: TUI CLI if no channels configured
	if len(cfg.Channels) == 0 {
		tui := chat.NewTUIChannel(log)
		tui.SetPrivacy(privacyMgr)
		return []domain.Channel{tui}, tui, nil
	}

	var channels []domain.Channel
	var tuiCh *chat.TUIChannel

	for _, cc := range cfg.Channels {
		switch cc.Type {
		case "cli":
			tui := chat.NewTUIChannel(log)
			tui.SetPrivacy(privacyMgr)
			tuiCh = tui
			channels = append(channels, tui)
		case "http":
			addr := ""
			if cc.HTTP != nil {
				addr = cc.HTTP.Addr
			}
			if addr == "" {
				addr = ":8080"
			}
			channels = append(channels, channel.NewHTTPChannel(addr, log))
		case "telegram":
			if cc.Telegram == nil || cc.Telegram.Token == "" {
				log.Warn("telegram channel configured but no token provided, skipping")
				continue
			}
			var opts []channel.TelegramOption
			if cc.MentionOnly {
				opts = append(opts, channel.WithTelegramMentionOnly(true))
			}
			channels = append(channels, channel.NewTelegramChannel(cc.Telegram.Token, log, opts...))
		case "discord":
			ch, err := buildDiscordChannel(cc, log)
			if err != nil {
				return nil, nil, fmt.Errorf("discord: %w", err)
			}
			channels = append(channels, ch)
		case "slack":
			ch, err := buildSlackChannel(cc, log)
			if err != nil {
				return nil, nil, fmt.Errorf("slack: %w", err)
			}
			channels = append(channels, ch)
		case "whatsapp":
			if cc.WhatsApp == nil || cc.WhatsApp.Token == "" {
				log.Warn("whatsapp channel configured but no token provided, skipping")
				continue
			}
			var opts []channel.WhatsAppOption
			if cc.MentionOnly {
				opts = append(opts, channel.WithWhatsAppMentionOnly(true))
			}
			webhookAddr := cc.WhatsApp.WebhookAddr
			if webhookAddr == "" {
				webhookAddr = ":3335"
			}
			channels = append(channels, channel.NewWhatsAppChannel(
				cc.WhatsApp.Token, cc.WhatsApp.PhoneID, cc.WhatsApp.VerifyToken,
				cc.WhatsApp.AppSecret, webhookAddr, log, opts...,
			))
		case "matrix":
			if cc.Matrix == nil || cc.Matrix.AccessToken == "" {
				log.Warn("matrix channel configured but no access token provided, skipping")
				continue
			}
			var opts []channel.MatrixOption
			if cc.MentionOnly {
				opts = append(opts, channel.WithMatrixMentionOnly(true))
			}
			channels = append(channels, channel.NewMatrixChannel(
				cc.Matrix.Homeserver, cc.Matrix.AccessToken,
				cc.Matrix.UserID, log, opts...,
			))
		case "teams":
			if cc.Teams == nil || cc.Teams.AppID == "" || cc.Teams.AppSecret == "" {
				log.Warn("teams channel configured but missing app_id or app_secret, skipping")
				continue
			}
			var opts []channel.TeamsOption
			if cc.MentionOnly {
				opts = append(opts, channel.WithTeamsMentionOnly(true))
			}
			if cc.Teams.TenantID != "" {
				opts = append(opts, channel.WithTeamsTenantID(cc.Teams.TenantID))
			}
			if cc.Teams.WebhookAddr != "" {
				opts = append(opts, channel.WithTeamsWebhookAddr(cc.Teams.WebhookAddr))
			}
			channels = append(channels, channel.NewTeamsChannel(
				cc.Teams.AppID, cc.Teams.AppSecret, log, opts...,
			))
		case "googlechat":
			if cc.GoogleChat == nil || cc.GoogleChat.CredentialsFile == "" {
				log.Warn("googlechat channel configured but no credentials_file provided, skipping")
				continue
			}
			var opts []channel.GoogleChatOption
			if cc.MentionOnly {
				opts = append(opts, channel.WithGoogleChatMentionOnly(true))
			}
			if cc.GoogleChat.SpaceID != "" {
				opts = append(opts, channel.WithGoogleChatSpaceID(cc.GoogleChat.SpaceID))
			}
			if cc.GoogleChat.WebhookAddr != "" {
				opts = append(opts, channel.WithGoogleChatWebhookAddr(cc.GoogleChat.WebhookAddr))
			}
			channels = append(channels, channel.NewGoogleChatChannel(
				cc.GoogleChat.CredentialsFile, log, opts...,
			))
		case "signal":
			if cc.Signal == nil || cc.Signal.APIURL == "" {
				log.Warn("signal channel configured but no api_url provided, skipping")
				continue
			}
			var opts []channel.SignalOption
			if cc.Signal.PollInterval > 0 {
				opts = append(opts, channel.WithSignalPollInterval(cc.Signal.PollInterval))
			}
			channels = append(channels, channel.NewSignalChannel(
				cc.Signal.APIURL, cc.Signal.Phone, log, opts...,
			))
		case "irc":
			if cc.IRC == nil || cc.IRC.Server == "" || cc.IRC.Nick == "" {
				log.Warn("irc channel configured but missing server or nick, skipping")
				continue
			}
			var opts []channel.IRCOption
			if cc.IRC.Password != "" {
				opts = append(opts, channel.WithIRCPassword(cc.IRC.Password))
			}
			if cc.IRC.UseTLS {
				opts = append(opts, channel.WithIRCUseTLS(true))
			}
			channels = append(channels, channel.NewIRCChannel(
				cc.IRC.Server, cc.IRC.Nick, cc.IRC.Channels, log, opts...,
			))
		case "webchat":
			channels = append(channels, channel.NewWebChatChannel(log))
		default:
			return nil, nil, fmt.Errorf("unknown channel type: %s", cc.Type)
		}
	}

	if len(channels) == 0 {
		// Fallback to TUI CLI
		tui := chat.NewTUIChannel(log)
		tui.SetPrivacy(privacyMgr)
		return []domain.Channel{tui}, tui, nil
	}

	return channels, tuiCh, nil
}
