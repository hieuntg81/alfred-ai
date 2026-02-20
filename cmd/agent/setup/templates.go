package setup

import (
	"time"

	"alfred-ai/internal/infra/config"
)

// Template represents a pre-configured use-case template for quick setup.
type Template struct {
	ID          string
	Name        string
	Description string
	Difficulty  string // "Beginner", "Intermediate", "Advanced"
	EstimateMin int    // Estimated setup time in minutes
	Config      *config.Config
}

// GetTemplates returns all available onboarding templates.
func GetTemplates() []Template {
	return []Template{
		personalAssistantTemplate(),
		telegramBotTemplate(),
		securePrivateTemplate(),
		multiAgentTemplate(),
		voiceAssistantTemplate(),
		workflowAutomationTemplate(),
		developerAPITemplate(),
		advancedCustomTemplate(), // keep last — catch-all for power users
	}
}

// personalAssistantTemplate provides a simple CLI-based personal assistant.
func personalAssistantTemplate() Template {
	cfg := config.Defaults()

	// LLM: OpenAI GPT-4o-mini by default (will be configured in wizard)
	cfg.LLM.DefaultProvider = "openai"
	cfg.LLM.Providers = []config.ProviderConfig{
		{
			Name:  "openai",
			Type:  "openai",
			Model: "gpt-4o-mini",
		},
	}

	// Memory: Markdown files for persistence
	cfg.Memory.Provider = "markdown"
	cfg.Memory.DataDir = "./data/memory"

	// Channels: CLI only
	cfg.Channels = []config.ChannelConfig{
		{Type: "cli"},
	}

	// Agent: Standard settings
	cfg.Agent.MaxIterations = 10
	cfg.Agent.SystemPrompt = "You are a helpful AI assistant. You can remember conversations and help with various tasks."

	return Template{
		ID:          "personal-assistant",
		Name:        "Personal AI Assistant",
		Description: "Simple CLI chat for personal use. Perfect for beginners who want a private AI assistant on their computer.",
		Difficulty:  "Beginner",
		EstimateMin: 3,
		Config:      cfg,
	}
}

// telegramBotTemplate provides a Telegram bot configuration.
func telegramBotTemplate() Template {
	cfg := config.Defaults()

	// LLM: OpenAI GPT-4o-mini
	cfg.LLM.DefaultProvider = "openai"
	cfg.LLM.Providers = []config.ProviderConfig{
		{
			Name:  "openai",
			Type:  "openai",
			Model: "gpt-4o-mini",
		},
	}

	// Memory: Markdown with auto-curation
	cfg.Memory.Provider = "markdown"
	cfg.Memory.DataDir = "./data/memory"
	cfg.Memory.AutoCurate = true

	// Channels: Telegram (token will be filled in wizard)
	cfg.Channels = []config.ChannelConfig{
		{
			Type:     "telegram",
			Telegram: &config.TelegramChannelConfig{}, // Token will be set during onboarding
		},
	}

	// Agent: Friendly bot personality
	cfg.Agent.MaxIterations = 10
	cfg.Agent.SystemPrompt = "You are a friendly AI assistant available via Telegram. You remember past conversations and help users with their questions."

	return Template{
		ID:          "telegram-bot",
		Name:        "Telegram Bot",
		Description: "Chat with your AI via Telegram messenger. Great for accessing your assistant from anywhere.",
		Difficulty:  "Beginner",
		EstimateMin: 5,
		Config:      cfg,
	}
}

// securePrivateTemplate provides maximum security configuration.
func securePrivateTemplate() Template {
	cfg := config.Defaults()

	// LLM: OpenAI GPT-4o-mini
	cfg.LLM.DefaultProvider = "openai"
	cfg.LLM.Providers = []config.ProviderConfig{
		{
			Name:  "openai",
			Type:  "openai",
			Model: "gpt-4o-mini",
		},
	}

	// Memory: Markdown files
	cfg.Memory.Provider = "markdown"
	cfg.Memory.DataDir = "./data/memory"

	// Security: Full security features
	cfg.Security.Encryption.Enabled = true
	// Passphrase will be set during onboarding

	cfg.Security.Audit.Enabled = true
	cfg.Security.Audit.Path = "./data/audit.jsonl"

	// Tools: Sandboxed file operations
	cfg.Tools.SandboxRoot = "./workspace"

	// Channels: CLI only for security
	cfg.Channels = []config.ChannelConfig{
		{Type: "cli"},
	}

	// Agent: Security-focused
	cfg.Agent.MaxIterations = 10
	cfg.Agent.SystemPrompt = "You are a secure AI assistant with encrypted memory. You help with tasks while respecting privacy and security."

	return Template{
		ID:          "secure-private",
		Name:        "Secure & Private",
		Description: "Maximum security with encryption, audit logging, and sandboxed operations. Ideal for handling sensitive information.",
		Difficulty:  "Intermediate",
		EstimateMin: 8,
		Config:      cfg,
	}
}

// multiAgentTemplate provides a multi-agent setup with routing and gateway.
func multiAgentTemplate() Template {
	cfg := config.Defaults()

	// LLM: OpenAI GPT-4o-mini (will be configured in wizard)
	cfg.LLM.DefaultProvider = "openai"
	cfg.LLM.Providers = []config.ProviderConfig{
		{Name: "openai", Type: "openai", Model: "gpt-4o-mini"},
	}

	// Memory: Markdown with auto-curation
	cfg.Memory.Provider = "markdown"
	cfg.Memory.DataDir = "./data/memory"
	cfg.Memory.AutoCurate = true

	// Multi-agent: main + researcher
	cfg.Agents = &config.AgentsConfig{
		Default: "main",
		Routing: "default",
		Instances: []config.AgentInstanceConfig{
			{
				ID:           "main",
				Name:         "Main Assistant",
				Description:  "Primary agent that handles general requests",
				SystemPrompt: "You are a helpful main assistant. Route specialized tasks to other agents when needed.",
				Provider:     "openai",
				Model:        "gpt-4o-mini",
				MaxIter:      10,
			},
			{
				ID:           "researcher",
				Name:         "Research Agent",
				Description:  "Specialized in web search and research tasks",
				SystemPrompt: "You are a research specialist. Focus on finding accurate, up-to-date information.",
				Provider:     "openai",
				Model:        "gpt-4o-mini",
				MaxIter:      15,
			},
		},
	}

	// Gateway: API access for programmatic control
	cfg.Gateway.Enabled = true
	cfg.Gateway.Addr = ":8090"

	// Channels: CLI
	cfg.Channels = []config.ChannelConfig{
		{Type: "cli"},
	}

	cfg.Agent.MaxIterations = 10
	cfg.Agent.SystemPrompt = "You are a multi-agent coordinator."

	return Template{
		ID:          "multi-agent",
		Name:        "Multi-Agent System",
		Description: "Multiple specialized agents with routing. Includes gateway API access for programmatic control.",
		Difficulty:  "Advanced",
		EstimateMin: 10,
		Config:      cfg,
	}
}

// voiceAssistantTemplate provides a voice-enabled assistant with Twilio.
func voiceAssistantTemplate() Template {
	cfg := config.Defaults()

	// LLM: OpenAI GPT-4o-mini
	cfg.LLM.DefaultProvider = "openai"
	cfg.LLM.Providers = []config.ProviderConfig{
		{Name: "openai", Type: "openai", Model: "gpt-4o-mini"},
	}

	// Memory: Markdown
	cfg.Memory.Provider = "markdown"
	cfg.Memory.DataDir = "./data/memory"

	// Voice: Twilio (credentials set in wizard)
	cfg.Tools.VoiceCall = config.VoiceCallConfig{
		Enabled:       true,
		Provider:      "twilio",
		DefaultMode:   "conversation",
		MaxConcurrent: 1,
		MaxDuration:   5 * time.Minute,
		Timeout:       30 * time.Second,
		DataDir:       "./data/voice",
		WebhookAddr:   ":3334",
		WebhookPath:   "/voice/webhook",
		StreamPath:    "/voice/stream",
		TTSVoice:      "alloy",
		TTSModel:      "tts-1",
		STTModel:      "gpt-4o-transcribe",
	}

	// Channels: CLI
	cfg.Channels = []config.ChannelConfig{
		{Type: "cli"},
	}

	cfg.Agent.MaxIterations = 10
	cfg.Agent.SystemPrompt = "You are a voice-enabled AI assistant. You can make and receive phone calls via Twilio."

	return Template{
		ID:          "voice-assistant",
		Name:        "Voice Assistant",
		Description: "AI assistant with voice calling via Twilio. Make and receive phone calls with speech-to-text and text-to-speech.",
		Difficulty:  "Intermediate",
		EstimateMin: 10,
		Config:      cfg,
	}
}

// workflowAutomationTemplate provides workflow, cron, and process management.
func workflowAutomationTemplate() Template {
	cfg := config.Defaults()

	// LLM: OpenAI GPT-4o-mini
	cfg.LLM.DefaultProvider = "openai"
	cfg.LLM.Providers = []config.ProviderConfig{
		{Name: "openai", Type: "openai", Model: "gpt-4o-mini"},
	}

	// Memory: Markdown
	cfg.Memory.Provider = "markdown"
	cfg.Memory.DataDir = "./data/memory"

	// Tools: Workflow + Cron + Process
	cfg.Tools.WorkflowEnabled = true
	cfg.Tools.WorkflowDir = "./workflows"
	cfg.Tools.WorkflowDataDir = "./data/workflows"
	cfg.Tools.CronEnabled = true
	cfg.Tools.CronDataDir = "./data/cron"
	cfg.Tools.ProcessEnabled = true

	// Gateway: API access for triggering workflows
	cfg.Gateway.Enabled = true
	cfg.Gateway.Addr = ":8090"

	// Channels: CLI
	cfg.Channels = []config.ChannelConfig{
		{Type: "cli"},
	}

	cfg.Agent.MaxIterations = 15
	cfg.Agent.SystemPrompt = "You are a workflow automation assistant. You can create and manage workflows, schedule recurring tasks, and run background processes."

	return Template{
		ID:          "workflow-automation",
		Name:        "Workflow Automation",
		Description: "Automate tasks with workflows, cron scheduling, and background processes. Gateway enabled for API-driven triggering.",
		Difficulty:  "Intermediate",
		EstimateMin: 8,
		Config:      cfg,
	}
}

// developerAPITemplate provides a gateway-first setup with all tools enabled.
func developerAPITemplate() Template {
	cfg := config.Defaults()

	// LLM: OpenAI GPT-4o-mini
	cfg.LLM.DefaultProvider = "openai"
	cfg.LLM.Providers = []config.ProviderConfig{
		{Name: "openai", Type: "openai", Model: "gpt-4o-mini"},
	}

	// Memory: Markdown with auto-curation
	cfg.Memory.Provider = "markdown"
	cfg.Memory.DataDir = "./data/memory"
	cfg.Memory.AutoCurate = true

	// Gateway: Primary interface
	cfg.Gateway.Enabled = true
	cfg.Gateway.Addr = ":8090"

	// All major tools enabled
	cfg.Tools.CanvasEnabled = true
	cfg.Tools.CronEnabled = true
	cfg.Tools.CronDataDir = "./data/cron"
	cfg.Tools.ProcessEnabled = true
	cfg.Tools.WorkflowEnabled = true
	cfg.Tools.WorkflowDir = "./workflows"
	cfg.Tools.WorkflowDataDir = "./data/workflows"
	cfg.Tools.MessageEnabled = true
	cfg.Tools.LLMTaskEnabled = true

	// Channels: CLI (gateway is primary)
	cfg.Channels = []config.ChannelConfig{
		{Type: "cli"},
	}

	cfg.Agent.MaxIterations = 15
	cfg.Agent.SystemPrompt = "You are a powerful developer AI assistant with access to all available tools. Respond concisely and focus on technical accuracy."

	return Template{
		ID:          "developer-api",
		Name:        "Developer / API Mode",
		Description: "Gateway-first setup with all tools enabled. Designed for programmatic access via WebSocket API.",
		Difficulty:  "Advanced",
		EstimateMin: 5,
		Config:      cfg,
	}
}

// advancedCustomTemplate provides the advanced/custom path.
func advancedCustomTemplate() Template {
	cfg := config.Defaults()

	return Template{
		ID:          "custom",
		Name:        "Advanced/Custom",
		Description: "Full control over all settings. For experienced users who want to configure every detail.",
		Difficulty:  "Advanced",
		EstimateMin: 15,
		Config:      cfg,
	}
}

// GetTemplateByID returns a template by its ID.
func GetTemplateByID(id string) *Template {
	templates := GetTemplates()
	for _, t := range templates {
		if t.ID == id {
			return &t
		}
	}
	return nil
}
