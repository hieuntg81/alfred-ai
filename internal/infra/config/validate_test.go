package config

import (
	"strings"
	"testing"
	"time"
)

func TestValidateDefaultsPass(t *testing.T) {
	cfg := Defaults()
	if err := Validate(cfg); err != nil {
		t.Fatalf("Defaults should pass validation: %v", err)
	}
}

func TestValidateAgentMaxIterationsZero(t *testing.T) {
	cfg := Defaults()
	cfg.Agent.MaxIterations = 0
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "agent.max_iterations must be > 0")
}

func TestValidateAgentTimeoutZero(t *testing.T) {
	cfg := Defaults()
	cfg.Agent.Timeout = 0
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "agent.timeout must be > 0")
}

func TestValidateAgentSystemPromptEmpty(t *testing.T) {
	cfg := Defaults()
	cfg.Agent.SystemPrompt = ""
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "agent.system_prompt must not be empty")
}

func TestValidateAgentCompressionThreshold(t *testing.T) {
	cfg := Defaults()
	cfg.Agent.Compression.Enabled = true
	cfg.Agent.Compression.Threshold = 0
	cfg.Agent.Compression.KeepRecent = 0
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "agent.compression.threshold must be > 0")
	assertContains(t, err.Error(), "agent.compression.keep_recent must be > 0")
}

func TestValidateLLMDefaultProviderEmpty(t *testing.T) {
	cfg := Defaults()
	cfg.LLM.DefaultProvider = ""
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "llm.default_provider must not be empty")
}

func TestValidateLLMDuplicateProvider(t *testing.T) {
	cfg := Defaults()
	cfg.LLM.Providers = []ProviderConfig{
		{Name: "openai", APIKey: "sk-1"},
		{Name: "openai", APIKey: "sk-2"},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "duplicate provider name")
}

func TestValidateLLMInvalidType(t *testing.T) {
	cfg := Defaults()
	cfg.LLM.Providers = []ProviderConfig{
		{Name: "openai", Type: "invalid", APIKey: "sk-1"},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), `type "invalid" is invalid`)
}

func TestValidateLLMDefaultNotInProviders(t *testing.T) {
	cfg := Defaults()
	cfg.LLM.DefaultProvider = "missing"
	cfg.LLM.Providers = []ProviderConfig{
		{Name: "openai", APIKey: "sk-1"},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), `default_provider "missing" does not match`)
}

func TestValidateLLMAPIKeyEmpty(t *testing.T) {
	cfg := Defaults()
	cfg.LLM.Providers = []ProviderConfig{
		{Name: "openai", APIKey: ""},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "api_key is empty")
	assertContains(t, err.Error(), "ALFREDAI_LLM_PROVIDER_OPENAI_API_KEY")
}

func TestValidateMemoryInvalidProvider(t *testing.T) {
	cfg := Defaults()
	cfg.Memory.Provider = "unknown"
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), `memory.provider "unknown" is invalid`)
}

func TestValidateMemoryByteRoverMissingFields(t *testing.T) {
	cfg := Defaults()
	cfg.Memory.Provider = "byterover"
	cfg.Memory.ByteRover.BaseURL = ""
	cfg.Memory.ByteRover.APIKey = ""
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "memory.byterover.base_url is required")
	assertContains(t, err.Error(), "memory.byterover.api_key is required")
}

func TestValidateToolsSandboxEmpty(t *testing.T) {
	cfg := Defaults()
	cfg.Tools.SandboxRoot = ""
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "tools.sandbox_root must not be empty")
}

func TestValidateChannelsInvalidType(t *testing.T) {
	cfg := Defaults()
	cfg.Channels = []ChannelConfig{{Type: "unknown"}}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), `type "unknown" is invalid`)
}

func TestValidateChannelsTelegramMissingToken(t *testing.T) {
	cfg := Defaults()
	cfg.Channels = []ChannelConfig{{Type: "telegram"}}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "telegram.token is required")
}

func TestValidateChannelsHTTPMissingAddr(t *testing.T) {
	cfg := Defaults()
	cfg.Channels = []ChannelConfig{{Type: "http"}}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "http.addr is required")
}

func TestValidateChannelsDiscordMissingToken(t *testing.T) {
	cfg := Defaults()
	cfg.Channels = []ChannelConfig{{Type: "discord"}}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "discord.token is required")
}

func TestValidateChannelsSlackMissingTokens(t *testing.T) {
	cfg := Defaults()
	cfg.Channels = []ChannelConfig{{Type: "slack"}}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "slack config section is required")
}

func TestValidateChannelsSlackMissingBotToken(t *testing.T) {
	cfg := Defaults()
	cfg.Channels = []ChannelConfig{{Type: "slack", Slack: &SlackChannelConfig{}}}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "slack.bot_token is required")
	assertContains(t, err.Error(), "slack.app_token is required")
}

func TestValidateWhatsAppChannelMissingToken(t *testing.T) {
	cfg := Defaults()
	cfg.Channels = []ChannelConfig{{Type: "whatsapp", WhatsApp: &WhatsAppChannelConfig{PhoneID: "123", VerifyToken: "verify"}}}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "whatsapp.token is required")
}

func TestValidateWhatsAppChannelMissingPhoneID(t *testing.T) {
	cfg := Defaults()
	cfg.Channels = []ChannelConfig{{Type: "whatsapp", WhatsApp: &WhatsAppChannelConfig{Token: "token", VerifyToken: "verify"}}}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "whatsapp.phone_id is required")
}

func TestValidateWhatsAppChannelMissingVerifyToken(t *testing.T) {
	cfg := Defaults()
	cfg.Channels = []ChannelConfig{{Type: "whatsapp", WhatsApp: &WhatsAppChannelConfig{Token: "token", PhoneID: "123"}}}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "whatsapp.verify_token is required")
}

func TestValidateWhatsAppChannelValid(t *testing.T) {
	cfg := Defaults()
	cfg.Channels = []ChannelConfig{{
		Type: "whatsapp",
		WhatsApp: &WhatsAppChannelConfig{
			Token:       "token",
			PhoneID:     "123",
			VerifyToken: "verify",
		},
	}}
	err := Validate(cfg)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateMatrixChannelMissingHomeserver(t *testing.T) {
	cfg := Defaults()
	cfg.Channels = []ChannelConfig{{Type: "matrix", Matrix: &MatrixChannelConfig{AccessToken: "token", UserID: "@bot:matrix.org"}}}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "matrix.homeserver is required")
}

func TestValidateMatrixChannelMissingAccessToken(t *testing.T) {
	cfg := Defaults()
	cfg.Channels = []ChannelConfig{{Type: "matrix", Matrix: &MatrixChannelConfig{Homeserver: "https://matrix.org", UserID: "@bot:matrix.org"}}}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "matrix.access_token is required")
}

func TestValidateMatrixChannelMissingUserID(t *testing.T) {
	cfg := Defaults()
	cfg.Channels = []ChannelConfig{{Type: "matrix", Matrix: &MatrixChannelConfig{Homeserver: "https://matrix.org", AccessToken: "token"}}}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "matrix.user_id is required")
}

func TestValidateMatrixChannelValid(t *testing.T) {
	cfg := Defaults()
	cfg.Channels = []ChannelConfig{{
		Type: "matrix",
		Matrix: &MatrixChannelConfig{
			Homeserver:  "https://matrix.org",
			AccessToken: "token",
			UserID:      "@bot:matrix.org",
		},
	}}
	err := Validate(cfg)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateSecurityAuditMissingPath(t *testing.T) {
	cfg := Defaults()
	cfg.Security.Audit.Enabled = true
	cfg.Security.Audit.Path = ""
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "security.audit.path is required")
}

func TestValidateSchedulerTaskMissingFields(t *testing.T) {
	cfg := Defaults()
	cfg.Scheduler.Enabled = true
	cfg.Scheduler.Tasks = []ScheduledTaskConfig{{}}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "scheduler.tasks[0].name is required")
	assertContains(t, err.Error(), "scheduler.tasks[0].schedule is required")
	assertContains(t, err.Error(), "scheduler.tasks[0].action is required")
}

func TestValidateGatewayInvalidAddr(t *testing.T) {
	cfg := Defaults()
	cfg.Gateway.Enabled = true
	cfg.Gateway.Addr = ""
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "gateway.addr is required")
}

func TestValidateGatewayBadHostPort(t *testing.T) {
	cfg := Defaults()
	cfg.Gateway.Enabled = true
	cfg.Gateway.Addr = "not-valid"
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "not a valid host:port")
}

func TestValidateAgentsDefaultNotFound(t *testing.T) {
	cfg := Defaults()
	cfg.Agents = &AgentsConfig{
		Default: "missing",
		Instances: []AgentInstanceConfig{
			{ID: "bot1"},
		},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), `agents.default "missing" does not match`)
}

func TestValidateAgentsDuplicateID(t *testing.T) {
	cfg := Defaults()
	cfg.Agents = &AgentsConfig{
		Default: "bot1",
		Instances: []AgentInstanceConfig{
			{ID: "bot1"},
			{ID: "bot1"},
		},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), `duplicate agent ID "bot1"`)
}

func TestValidateAgentsInvalidRouting(t *testing.T) {
	cfg := Defaults()
	cfg.Agents = &AgentsConfig{
		Default: "bot1",
		Routing: "invalid",
		Instances: []AgentInstanceConfig{
			{ID: "bot1"},
		},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), `agents.routing "invalid" is invalid`)
}

func TestValidateNodesInvalidIntervals(t *testing.T) {
	cfg := Defaults()
	cfg.Nodes.Enabled = true
	cfg.Nodes.HeartbeatInterval = 0
	cfg.Nodes.InvokeTimeout = 0
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "nodes.heartbeat_interval must be > 0")
	assertContains(t, err.Error(), "nodes.invoke_timeout must be > 0")
}

func TestValidatePluginsEnabledNoDirs(t *testing.T) {
	cfg := Defaults()
	cfg.Plugins.Enabled = true
	cfg.Plugins.Dirs = nil
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "plugins.dirs must have at least one entry")
}

func TestValidateMultipleErrors(t *testing.T) {
	cfg := Defaults()
	cfg.Agent.MaxIterations = 0
	cfg.Agent.SystemPrompt = ""
	cfg.LLM.DefaultProvider = ""
	cfg.Memory.Provider = "invalid"

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if len(ve.Errors) < 4 {
		t.Errorf("expected at least 4 errors, got %d: %v", len(ve.Errors), ve.Errors)
	}
}

func TestValidationErrorFormat(t *testing.T) {
	ve := &ValidationError{}
	ve.Add("first error")
	ve.Add("second error")

	msg := ve.Error()
	if !strings.HasPrefix(msg, "config validation failed:") {
		t.Errorf("unexpected prefix: %s", msg)
	}
	if !strings.Contains(msg, "first error") || !strings.Contains(msg, "second error") {
		t.Errorf("missing error details: %s", msg)
	}
}

func TestValidateLLMOpenRouterType(t *testing.T) {
	cfg := Defaults()
	cfg.LLM.DefaultProvider = "openrouter"
	cfg.LLM.Providers = []ProviderConfig{
		{Name: "openrouter", Type: "openrouter", APIKey: "sk-or-test"},
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("openrouter type should be valid: %v", err)
	}
}

func TestValidateValidConfig(t *testing.T) {
	cfg := Defaults()
	cfg.LLM.Providers = []ProviderConfig{
		{Name: "openai", Type: "openai", APIKey: "sk-test"},
	}
	cfg.Channels = []ChannelConfig{
		{Type: "cli"},
		{Type: "telegram", Telegram: &TelegramChannelConfig{Token: "tok"}},
		{Type: "http", HTTP: &HTTPChannelConfig{Addr: ":8080"}},
	}
	cfg.Nodes.Enabled = true
	cfg.Nodes.HeartbeatInterval = 30 * time.Second
	cfg.Nodes.InvokeTimeout = 30 * time.Second
	cfg.Gateway.Enabled = true
	cfg.Gateway.Addr = ":8090"
	cfg.Scheduler.Enabled = true
	cfg.Scheduler.Tasks = []ScheduledTaskConfig{
		{Name: "cleanup", Schedule: "@daily", Action: "run_cleanup"},
	}
	cfg.Plugins.Enabled = true
	cfg.Plugins.Dirs = []string{"./plugins"}

	if err := Validate(cfg); err != nil {
		t.Fatalf("valid config should pass: %v", err)
	}
}

func TestValidateSearchDecayHalfLifeNegative(t *testing.T) {
	cfg := Defaults()
	cfg.Memory.Search.DecayHalfLife = -1 * time.Second
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "memory.search.decay_half_life must be >= 0")
}

func TestValidateSearchMMRDiversityOutOfRange(t *testing.T) {
	cfg := Defaults()
	cfg.Memory.Search.MMRDiversity = 1.5
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error for mmr_diversity > 1")
	}
	assertContains(t, err.Error(), "memory.search.mmr_diversity must be between 0 and 1")

	cfg2 := Defaults()
	cfg2.Memory.Search.MMRDiversity = -0.1
	err = Validate(cfg2)
	if err == nil {
		t.Fatal("expected validation error for mmr_diversity < 0")
	}
	assertContains(t, err.Error(), "memory.search.mmr_diversity must be between 0 and 1")
}

func TestValidateSearchEmbeddingCacheSizeNegative(t *testing.T) {
	cfg := Defaults()
	cfg.Memory.Search.EmbeddingCacheSize = -1
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "memory.search.embedding_cache_size must be >= 0")
}

func TestValidateSearchZeroValuesPass(t *testing.T) {
	cfg := Defaults()
	// All zero = disabled = valid
	cfg.Memory.Search.DecayHalfLife = 0
	cfg.Memory.Search.MMRDiversity = 0
	cfg.Memory.Search.EmbeddingCacheSize = 0
	if err := Validate(cfg); err != nil {
		t.Fatalf("zero search config should pass: %v", err)
	}
}

func TestValidateSearchValidValues(t *testing.T) {
	cfg := Defaults()
	cfg.Memory.Search.DecayHalfLife = 24 * time.Hour
	cfg.Memory.Search.MMRDiversity = 0.3
	cfg.Memory.Search.EmbeddingCacheSize = 256
	if err := Validate(cfg); err != nil {
		t.Fatalf("valid search config should pass: %v", err)
	}
}

// --- Notes tool validation ---

func TestValidateNotesEnabledMissingDataDir(t *testing.T) {
	cfg := Defaults()
	cfg.Tools.NotesEnabled = true
	cfg.Tools.NotesDataDir = ""
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "tools.notes_data_dir must not be empty")
}

func TestValidateNotesEnabledValid(t *testing.T) {
	cfg := Defaults()
	cfg.Tools.NotesEnabled = true
	cfg.Tools.NotesDataDir = "/tmp/notes"
	if err := Validate(cfg); err != nil {
		t.Fatalf("expected valid: %v", err)
	}
}

func TestValidateNotesDisabledNoValidation(t *testing.T) {
	cfg := Defaults()
	cfg.Tools.NotesEnabled = false
	cfg.Tools.NotesDataDir = ""
	if err := Validate(cfg); err != nil {
		t.Fatalf("disabled tool should not be validated: %v", err)
	}
}

// --- GitHub tool validation ---

func TestValidateGitHubEnabledBadTimeout(t *testing.T) {
	cfg := Defaults()
	cfg.Tools.GitHubEnabled = true
	cfg.Tools.GitHubTimeout = 0
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "tools.github_timeout must be > 0")
}

func TestValidateGitHubEnabledBadRateLimit(t *testing.T) {
	cfg := Defaults()
	cfg.Tools.GitHubEnabled = true
	cfg.Tools.GitHubMaxRequestsPerMinute = 0
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "tools.github_max_requests_per_minute must be > 0")
}

func TestValidateGitHubEnabledValid(t *testing.T) {
	cfg := Defaults()
	cfg.Tools.GitHubEnabled = true
	if err := Validate(cfg); err != nil {
		t.Fatalf("expected valid: %v", err)
	}
}

// --- Email tool validation ---

func TestValidateEmailEnabledBadTimeout(t *testing.T) {
	cfg := Defaults()
	cfg.Tools.EmailEnabled = true
	cfg.Tools.EmailTimeout = 0
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "tools.email_timeout must be > 0")
}

func TestValidateEmailEnabledBadSendLimit(t *testing.T) {
	cfg := Defaults()
	cfg.Tools.EmailEnabled = true
	cfg.Tools.EmailMaxSendsPerHour = 0
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "tools.email_max_sends_per_hour must be > 0")
}

func TestValidateEmailEnabledValid(t *testing.T) {
	cfg := Defaults()
	cfg.Tools.EmailEnabled = true
	if err := Validate(cfg); err != nil {
		t.Fatalf("expected valid: %v", err)
	}
}

// --- Calendar tool validation ---

func TestValidateCalendarEnabledBadTimeout(t *testing.T) {
	cfg := Defaults()
	cfg.Tools.CalendarEnabled = true
	cfg.Tools.CalendarTimeout = 0
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "tools.calendar_timeout must be > 0")
}

func TestValidateCalendarEnabledValid(t *testing.T) {
	cfg := Defaults()
	cfg.Tools.CalendarEnabled = true
	if err := Validate(cfg); err != nil {
		t.Fatalf("expected valid: %v", err)
	}
}

// --- Smart Home tool validation ---

func TestValidateSmartHomeEnabledMissingURL(t *testing.T) {
	cfg := Defaults()
	cfg.Tools.SmartHomeEnabled = true
	cfg.Tools.SmartHomeURL = ""
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "tools.smarthome_url must not be empty")
}

func TestValidateSmartHomeEnabledBadTimeout(t *testing.T) {
	cfg := Defaults()
	cfg.Tools.SmartHomeEnabled = true
	cfg.Tools.SmartHomeURL = "http://ha:8123"
	cfg.Tools.SmartHomeTimeout = 0
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "tools.smarthome_timeout must be > 0")
}

func TestValidateSmartHomeEnabledBadCallsPerMinute(t *testing.T) {
	cfg := Defaults()
	cfg.Tools.SmartHomeEnabled = true
	cfg.Tools.SmartHomeURL = "http://ha:8123"
	cfg.Tools.SmartHomeMaxCallsPerMinute = 0
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertContains(t, err.Error(), "tools.smarthome_max_calls_per_minute must be > 0")
}

func TestValidateSmartHomeEnabledValid(t *testing.T) {
	cfg := Defaults()
	cfg.Tools.SmartHomeEnabled = true
	cfg.Tools.SmartHomeURL = "http://ha:8123"
	if err := Validate(cfg); err != nil {
		t.Fatalf("expected valid: %v", err)
	}
}

func TestValidateSmartHomeDisabledNoValidation(t *testing.T) {
	cfg := Defaults()
	cfg.Tools.SmartHomeEnabled = false
	cfg.Tools.SmartHomeURL = ""
	cfg.Tools.SmartHomeTimeout = 0
	if err := Validate(cfg); err != nil {
		t.Fatalf("disabled tool should not be validated: %v", err)
	}
}

// --- WASM plugin config validation ---

func TestValidateWASMEnabledValid(t *testing.T) {
	cfg := Defaults()
	cfg.Plugins.Enabled = true
	cfg.Plugins.Dirs = []string{"./plugins"}
	cfg.Plugins.WASMEnabled = true
	cfg.Plugins.WASMMaxMemoryMB = 64
	cfg.Plugins.WASMExecTimeout = "30s"
	if err := Validate(cfg); err != nil {
		t.Fatalf("valid WASM config should pass: %v", err)
	}
}

func TestValidateWASMMemoryTooLarge(t *testing.T) {
	cfg := Defaults()
	cfg.Plugins.Enabled = true
	cfg.Plugins.Dirs = []string{"./plugins"}
	cfg.Plugins.WASMEnabled = true
	cfg.Plugins.WASMMaxMemoryMB = 1024 // > 512
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error for memory > 512")
	}
	assertContains(t, err.Error(), "plugins.wasm_max_memory_mb must be between 1 and 512")
}

func TestValidateWASMMemoryNegative(t *testing.T) {
	cfg := Defaults()
	cfg.Plugins.Enabled = true
	cfg.Plugins.Dirs = []string{"./plugins"}
	cfg.Plugins.WASMEnabled = true
	cfg.Plugins.WASMMaxMemoryMB = -1
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error for negative memory")
	}
	assertContains(t, err.Error(), "plugins.wasm_max_memory_mb must be between 1 and 512")
}

func TestValidateWASMInvalidTimeout(t *testing.T) {
	cfg := Defaults()
	cfg.Plugins.Enabled = true
	cfg.Plugins.Dirs = []string{"./plugins"}
	cfg.Plugins.WASMEnabled = true
	cfg.Plugins.WASMExecTimeout = "not-a-duration"
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error for invalid timeout")
	}
	assertContains(t, err.Error(), "plugins.wasm_exec_timeout")
	assertContains(t, err.Error(), "not a valid duration")
}

func TestValidateWASMTimeoutTooShort(t *testing.T) {
	cfg := Defaults()
	cfg.Plugins.Enabled = true
	cfg.Plugins.Dirs = []string{"./plugins"}
	cfg.Plugins.WASMEnabled = true
	cfg.Plugins.WASMExecTimeout = "500ms"
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error for timeout < 1s")
	}
	assertContains(t, err.Error(), "plugins.wasm_exec_timeout must be between 1s and 5m")
}

func TestValidateWASMTimeoutTooLong(t *testing.T) {
	cfg := Defaults()
	cfg.Plugins.Enabled = true
	cfg.Plugins.Dirs = []string{"./plugins"}
	cfg.Plugins.WASMEnabled = true
	cfg.Plugins.WASMExecTimeout = "10m"
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error for timeout > 5m")
	}
	assertContains(t, err.Error(), "plugins.wasm_exec_timeout must be between 1s and 5m")
}

func TestValidateWASMDisabledNoValidation(t *testing.T) {
	cfg := Defaults()
	cfg.Plugins.Enabled = true
	cfg.Plugins.Dirs = []string{"./plugins"}
	cfg.Plugins.WASMEnabled = false
	cfg.Plugins.WASMMaxMemoryMB = 9999 // would be invalid if enabled
	if err := Validate(cfg); err != nil {
		t.Fatalf("disabled WASM should not be validated: %v", err)
	}
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("expected %q to contain %q", s, substr)
	}
}
