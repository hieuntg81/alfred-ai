package main

import (
	"context"
	"log/slog"

	"alfred-ai/internal/adapter/llm"
	"alfred-ai/internal/adapter/skill"
	"alfred-ai/internal/adapter/tool"
	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
	"alfred-ai/internal/usecase"
	"alfred-ai/internal/usecase/process"
)

// AgentComponents holds all agent-related components
type AgentComponents struct {
	Agent          *usecase.Agent
	ToolRegistry   *tool.Registry
	SessionManager *usecase.SessionManager
	ContextBuilder *usecase.ContextBuilder
	Compressor     *usecase.Compressor
	Approver       domain.ToolApprover
	ProcessManager *process.Manager // can be nil
}

// initAgent initializes agent components (tools, context builder, agent)
// Returns the components and any error
func initAgent(
	ctx context.Context,
	cfg *config.Config,
	llmProvider domain.LLMProvider,
	llmRegistry *llm.Registry,
	mem domain.MemoryProvider,
	security *SecurityComponents,
	bus domain.EventBus,
	log *slog.Logger,
) (*AgentComponents, error) {
	// 1. Init tool approver (if enabled)
	var approver domain.ToolApprover
	if cfg.Agent.ToolApproval.Enabled {
		approver = usecase.NewConfigApprover(
			cfg.Agent.ToolApproval.AlwaysApprove,
			cfg.Agent.ToolApproval.AlwaysDeny,
		)
		log.Info("tool approval enabled",
			"always_approve", cfg.Agent.ToolApproval.AlwaysApprove,
			"always_deny", cfg.Agent.ToolApproval.AlwaysDeny,
		)
	}

	// 2. Init tool backends + registry
	toolRegistry := tool.NewRegistry(log)

	fsBackend := createFilesystemBackend(cfg)
	toolRegistry.Register(tool.NewFilesystemTool(fsBackend, security.Sandbox, log))

	// Process manager (opt-in, needed before shell tool for background support)
	var processManager *process.Manager
	if cfg.Tools.ProcessEnabled {
		processManager = process.NewManager(process.ManagerConfig{
			MaxSessions:     cfg.Tools.ProcessMaxSessions,
			SessionTTL:      cfg.Tools.ProcessSessionTTL,
			OutputBufferMax: cfg.Tools.ProcessOutputMax,
		}, bus, log)
		log.Info("process management enabled",
			"max_sessions", cfg.Tools.ProcessMaxSessions,
			"session_ttl", cfg.Tools.ProcessSessionTTL,
		)
	}

	shellBackend := createShellBackend(cfg)
	var shellOpts []tool.ShellToolOption
	if processManager != nil {
		shellOpts = append(shellOpts, tool.WithProcessManager(processManager))
	}
	toolRegistry.Register(tool.NewShellTool(shellBackend, cfg.Tools.AllowedCommands, security.Sandbox, log, shellOpts...))

	if processManager != nil {
		toolRegistry.Register(tool.NewProcessTool(processManager, log))
		log.Info("process tool enabled")
	}

	toolRegistry.Register(tool.NewWebTool(log))

	searchBackend := createSearchBackend(cfg, log)
	toolRegistry.Register(tool.NewWebSearchTool(searchBackend, cfg.Tools.SearchCacheTTL, log))
	log.Info("web search tool enabled", "backend", cfg.Tools.SearchBackend)

	// Browser tool (opt-in, excluded from edge builds)
	if cfg.Tools.BrowserEnabled && !edgeBuild {
		browserBackend, err := createBrowserBackend(cfg, log)
		if err != nil {
			log.Warn("browser backend init failed, tool disabled", "error", err)
		} else {
			toolRegistry.Register(tool.NewBrowserTool(browserBackend, log))
			log.Info("browser tool enabled", "backend", cfg.Tools.BrowserBackend)
		}
	}

	// Canvas tool (opt-in, excluded from edge builds)
	if cfg.Tools.CanvasEnabled && !edgeBuild {
		canvasBackend, err := createCanvasBackend(cfg)
		if err != nil {
			log.Warn("canvas backend init failed, tool disabled", "error", err)
		} else {
			toolRegistry.Register(tool.NewCanvasTool(
				canvasBackend, bus,
				cfg.Tools.CanvasMaxSize, log,
			))
			log.Info("canvas tool enabled", "backend", cfg.Tools.CanvasBackend, "root", cfg.Tools.CanvasRoot)
		}
	}

	// Notes tool (opt-in)
	if cfg.Tools.NotesEnabled {
		notesBackend, err := tool.NewLocalNotesBackend(cfg.Tools.NotesDataDir)
		if err != nil {
			log.Warn("notes backend init failed, tool disabled", "error", err)
		} else {
			toolRegistry.Register(tool.NewNotesTool(notesBackend, log))
			log.Info("notes tool enabled", "data_dir", cfg.Tools.NotesDataDir)
		}
	}

	// GitHub tool (opt-in, excluded from edge builds)
	if cfg.Tools.GitHubEnabled && !edgeBuild {
		toolRegistry.Register(tool.NewGitHubTool(nil, cfg.Tools.GitHubTimeout, cfg.Tools.GitHubMaxRequestsPerMinute, log))
		log.Info("github tool enabled", "timeout", cfg.Tools.GitHubTimeout, "max_rpm", cfg.Tools.GitHubMaxRequestsPerMinute)
	}

	// Email tool (opt-in, excluded from edge builds)
	if cfg.Tools.EmailEnabled && !edgeBuild {
		toolRegistry.Register(tool.NewEmailTool(nil, cfg.Tools.EmailTimeout, cfg.Tools.EmailMaxSendsPerHour, cfg.Tools.EmailAllowedDomains, log))
		log.Info("email tool enabled", "timeout", cfg.Tools.EmailTimeout, "max_sends_per_hour", cfg.Tools.EmailMaxSendsPerHour)
	}

	// Calendar tool (opt-in, excluded from edge builds)
	if cfg.Tools.CalendarEnabled && !edgeBuild {
		toolRegistry.Register(tool.NewCalendarTool(nil, cfg.Tools.CalendarTimeout, log))
		log.Info("calendar tool enabled", "timeout", cfg.Tools.CalendarTimeout)
	}

	// Smart Home tool (opt-in, available in all builds including edge).
	if cfg.Tools.SmartHomeEnabled {
		toolRegistry.Register(tool.NewSmartHomeTool(nil, cfg.Tools.SmartHomeURL, cfg.Tools.SmartHomeToken, cfg.Tools.SmartHomeTimeout, cfg.Tools.SmartHomeMaxCallsPerMinute, log))
		log.Info("smart home tool enabled", "url", cfg.Tools.SmartHomeURL, "timeout", cfg.Tools.SmartHomeTimeout)
	}

	// MCP bridge (opt-in, excluded from edge builds): connect to MCP servers and register discovered tools.
	if cfg.Tools.MCPEnabled && len(cfg.Tools.MCPServers) > 0 && !edgeBuild {
		bridge, err := tool.NewMCPBridge(ctx, cfg.Tools.MCPServers, log)
		if err != nil {
			log.Error("mcp bridge init failed", "error", err)
		} else {
			for _, t := range bridge.Tools() {
				toolRegistry.Register(t)
			}
			log.Info("mcp bridge enabled", "servers", len(cfg.Tools.MCPServers), "tools", len(bridge.Tools()))
		}
	}

	// MQTT tool (opt-in, available in all builds including edge).
	if cfg.Tools.MQTTEnabled {
		mqttBackend := tool.NewMockMQTTBackend() // TODO: replace with real MQTT client when paho dependency is added
		toolRegistry.Register(tool.NewMQTTTool(mqttBackend, log))
		log.Info("mqtt tool enabled", "broker", cfg.Tools.MQTTBrokerURL)
	}

	// GPIO tool (opt-in, edge builds only).
	registerGPIOTool(cfg, toolRegistry, log)

	// Serial tool (opt-in, edge builds only).
	registerSerialTool(cfg, toolRegistry, log)

	// BLE tool (opt-in, edge builds only).
	registerBLETool(cfg, toolRegistry, log)

	// 3. Init session manager
	sessionMgr := usecase.NewSessionManager("./data/sessions")

	// 4. Init context builder
	model := ""
	if len(cfg.LLM.Providers) > 0 {
		model = cfg.LLM.Providers[0].Model
	}
	ctxBuilder := usecase.NewContextBuilder(
		cfg.Agent.SystemPrompt,
		model,
		50,
	)

	// Set extended thinking budget from primary provider config (if configured).
	if len(cfg.LLM.Providers) > 0 && cfg.LLM.Providers[0].ThinkingBudget > 0 {
		ctxBuilder.SetThinkingBudget(cfg.LLM.Providers[0].ThinkingBudget)
	}

	// 5. Load skills (if enabled)
	if cfg.Skills.Enabled {
		skillProvider := skill.NewFileSkillProvider(cfg.Skills.Dir)
		skills, err := skillProvider.Load(ctx)
		if err != nil {
			log.Warn("failed to load skills", "error", err, "dir", cfg.Skills.Dir)
		} else {
			// Build model router if routing config is present.
			var skillOpts []skill.SkillToolOption
			skillOpts = append(skillOpts, skill.WithLogger(log))
			if len(cfg.LLM.ModelRouting) > 0 && llmRegistry != nil {
				router := llm.NewPreferenceRouter(cfg.LLM.ModelRouting, llmRegistry, llmProvider)
				skillOpts = append(skillOpts, skill.WithModelRouter(router))
				log.Info("skill model routing enabled", "routes", cfg.LLM.ModelRouting)
			}

			var promptSkills []domain.Skill
			for _, s := range skills {
				// Register tool-type skills
				if s.Trigger == "tool" || s.Trigger == "both" {
					toolRegistry.Register(skill.NewSkillTool(s, skillOpts...))
				}
				// Collect prompt-type skills for context builder
				if s.Trigger == "prompt" || s.Trigger == "both" {
					promptSkills = append(promptSkills, s)
				}
			}
			ctxBuilder.SetSkills(promptSkills)

			// Validate skill tool requirements against the tool registry.
			for _, s := range skills {
				for _, requiredTool := range s.Tools {
					if _, err := toolRegistry.Get(requiredTool); err != nil {
						log.Warn("skill requires unavailable tool",
							"skill", s.Name,
							"tool", requiredTool,
						)
					}
				}
			}

			log.Info("skills loaded", "count", len(skills))
		}
	}

	// 6. Init compressor (if enabled)
	var compressor *usecase.Compressor
	if cfg.Agent.Compression.Enabled {
		compressor = usecase.NewCompressor(llmProvider, usecase.CompressionConfig{
			Enabled:    true,
			Threshold:  cfg.Agent.Compression.Threshold,
			KeepRecent: cfg.Agent.Compression.KeepRecent,
		}, log)
		log.Info("context compression enabled",
			"threshold", cfg.Agent.Compression.Threshold,
			"keep_recent", cfg.Agent.Compression.KeepRecent,
		)
	}

	// 7. Init context guard (if enabled)
	var contextGuard *usecase.ContextGuard
	if cfg.Agent.ContextGuard.Enabled {
		provider := cfg.LLM.DefaultProvider
		tokenCounter := usecase.NewTokenCounter(provider, model)
		contextGuard = usecase.NewContextGuard(usecase.ContextGuardConfig{
			MaxTokens:     cfg.Agent.ContextGuard.MaxTokens,
			ReserveTokens: cfg.Agent.ContextGuard.ReserveTokens,
			SafetyMargin:  cfg.Agent.ContextGuard.SafetyMargin,
		}, tokenCounter, compressor, log)
		log.Info("context guard enabled",
			"max_tokens", cfg.Agent.ContextGuard.MaxTokens,
			"reserve_tokens", cfg.Agent.ContextGuard.ReserveTokens,
			"safety_margin", cfg.Agent.ContextGuard.SafetyMargin,
		)
	}

	// 8. Init agent
	agent := usecase.NewAgent(usecase.AgentDeps{
		LLM:            llmProvider,
		Memory:         mem,
		Tools:          toolRegistry,
		ContextBuilder: ctxBuilder,
		Logger:         log,
		MaxIterations:  cfg.Agent.MaxIterations,
		AuditLogger:    security.AuditLogger,
		Compressor:     compressor,
		Bus:            bus,
		Approver:       approver,
		ContextGuard:   contextGuard,
	})

	// 9. Init sub-agent (if enabled)
	if cfg.Agent.SubAgent.Enabled {
		subAgentFactory := func() *usecase.Agent {
			// Create agents WITHOUT sub_agent tool to prevent recursion
			return usecase.NewAgent(usecase.AgentDeps{
				LLM:            llmProvider,
				Memory:         mem,
				Tools:          toolRegistry,
				ContextBuilder: ctxBuilder,
				Logger:         log,
				MaxIterations:  cfg.Agent.SubAgent.MaxIterations,
				AuditLogger:    security.AuditLogger,
				Compressor:     compressor,
				Bus:            bus,
				Approver:       approver,
			})
		}

		subAgentMgr := usecase.NewSubAgentManager(subAgentFactory, usecase.SubAgentConfig{
			MaxSubAgents:  cfg.Agent.SubAgent.MaxSubAgents,
			MaxIterations: cfg.Agent.SubAgent.MaxIterations,
			Timeout:       cfg.Agent.SubAgent.Timeout,
		}, log)

		toolRegistry.Register(tool.NewSubAgentTool(subAgentMgr))
		log.Info("sub-agent spawning enabled",
			"max_sub_agents", cfg.Agent.SubAgent.MaxSubAgents,
			"max_iterations", cfg.Agent.SubAgent.MaxIterations,
		)
	}

	return &AgentComponents{
		Agent:          agent,
		ToolRegistry:   toolRegistry,
		SessionManager: sessionMgr,
		ContextBuilder: ctxBuilder,
		Compressor:     compressor,
		Approver:       approver,
		ProcessManager: processManager,
	}, nil
}

// createSearchBackend builds the configured search backend.
func createSearchBackend(cfg *config.Config, log *slog.Logger) tool.SearchBackend {
	switch cfg.Tools.SearchBackend {
	case "searxng":
		return tool.NewSearXNGBackend(cfg.Tools.SearXNGURL, log)
	default:
		return tool.NewSearXNGBackend(cfg.Tools.SearXNGURL, log)
	}
}

// createFilesystemBackend builds the configured filesystem backend.
func createFilesystemBackend(cfg *config.Config) tool.FilesystemBackend {
	switch cfg.Tools.FilesystemBackend {
	case "local":
		return tool.NewLocalFilesystemBackend()
	default:
		return tool.NewLocalFilesystemBackend()
	}
}

// createShellBackend builds the configured shell backend.
func createShellBackend(cfg *config.Config) tool.ShellBackend {
	switch cfg.Tools.ShellBackend {
	case "local":
		return tool.NewLocalShellBackend(cfg.Tools.ShellTimeout)
	default:
		return tool.NewLocalShellBackend(cfg.Tools.ShellTimeout)
	}
}

// createCanvasBackend builds the configured canvas backend.
func createCanvasBackend(cfg *config.Config) (tool.CanvasBackend, error) {
	switch cfg.Tools.CanvasBackend {
	case "local":
		return tool.NewLocalCanvasBackend(cfg.Tools.CanvasRoot)
	default:
		return tool.NewLocalCanvasBackend(cfg.Tools.CanvasRoot)
	}
}

// createBrowserBackend builds the configured browser backend.
func createBrowserBackend(cfg *config.Config, log *slog.Logger) (tool.BrowserBackend, error) {
	switch cfg.Tools.BrowserBackend {
	case "chromedp":
		return tool.NewChromeDPBackend(tool.ChromeDPConfig{
			RemoteURL: cfg.Tools.BrowserCDPURL,
			Headless:  cfg.Tools.BrowserHeadless,
			Timeout:   cfg.Tools.BrowserTimeout,
		}, log)
	default:
		return tool.NewChromeDPBackend(tool.ChromeDPConfig{
			RemoteURL: cfg.Tools.BrowserCDPURL,
			Headless:  cfg.Tools.BrowserHeadless,
			Timeout:   cfg.Tools.BrowserTimeout,
		}, log)
	}
}
