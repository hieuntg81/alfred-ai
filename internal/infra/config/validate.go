package config

import (
	"fmt"
	"net"
	"strings"
	"time"
)

// ValidationError accumulates config validation errors.
type ValidationError struct {
	Errors []string
}

func (v *ValidationError) Error() string {
	return "config validation failed:\n  - " + strings.Join(v.Errors, "\n  - ")
}

// HasErrors reports whether any validation errors have been recorded.
func (v *ValidationError) HasErrors() bool {
	return len(v.Errors) > 0
}

// Add records a formatted validation error.
func (v *ValidationError) Add(format string, args ...interface{}) {
	v.Errors = append(v.Errors, fmt.Sprintf(format, args...))
}

// Validate checks cfg for structural correctness. It returns a *ValidationError
// when one or more problems are found, allowing callers to inspect all issues.
func Validate(cfg *Config) error {
	ve := &ValidationError{}
	validateAgent(cfg, ve)
	validateLLM(cfg, ve)
	validateMemory(cfg, ve)
	validateTools(cfg, ve)
	validateChannels(cfg, ve)
	validateSecurity(cfg, ve)
	validateScheduler(cfg, ve)
	validateGateway(cfg, ve)
	validateAgents(cfg, ve)
	validateNodes(cfg, ve)
	validatePlugins(cfg, ve)
	validateTenants(cfg, ve)
	validateOffline(cfg, ve)
	validateCluster(cfg, ve)
	if ve.HasErrors() {
		return ve
	}
	return nil
}

func validateAgent(cfg *Config, ve *ValidationError) {
	if cfg.Agent.MaxIterations <= 0 {
		ve.Add("agent.max_iterations must be > 0")
	}
	if cfg.Agent.Timeout <= 0 {
		ve.Add("agent.timeout must be > 0")
	}
	if cfg.Agent.SystemPrompt == "" {
		ve.Add("agent.system_prompt must not be empty")
	}
	if cfg.Agent.Compression.Enabled {
		if cfg.Agent.Compression.Threshold <= 0 {
			ve.Add("agent.compression.threshold must be > 0 when compression is enabled")
		}
		if cfg.Agent.Compression.KeepRecent <= 0 {
			ve.Add("agent.compression.keep_recent must be > 0 when compression is enabled")
		}
	}
}

var validProviderTypes = map[string]bool{
	"openai":     true,
	"anthropic":  true,
	"gemini":     true,
	"openrouter": true,
	"ollama":     true,
	"bedrock":    true,
}

func validateLLM(cfg *Config, ve *ValidationError) {
	if cfg.LLM.DefaultProvider == "" {
		ve.Add("llm.default_provider must not be empty")
	}

	if len(cfg.LLM.Providers) == 0 {
		return
	}

	seen := make(map[string]bool)
	foundDefault := false
	for i, p := range cfg.LLM.Providers {
		if p.Name == "" {
			ve.Add("llm.providers[%d].name must not be empty", i)
			continue
		}
		if seen[p.Name] {
			ve.Add("llm.providers[%d]: duplicate provider name %q", i, p.Name)
		}
		seen[p.Name] = true

		if p.Type != "" && !validProviderTypes[p.Type] {
			ve.Add("llm.providers[%d].type %q is invalid (want: openai, anthropic, gemini, openrouter, ollama, bedrock)", i, p.Type)
		}
		if p.APIKey == "" && p.Type != "bedrock" {
			ve.Add("llm.providers[%d] (%s): api_key is empty (set via ALFREDAI_LLM_PROVIDER_%s_API_KEY)",
				i, p.Name, strings.ToUpper(p.Name))
		}
		if p.Type == "bedrock" && p.Region == "" {
			ve.Add("llm.providers[%d] (%s): region is required for bedrock provider", i, p.Name)
		}
		if p.Name == cfg.LLM.DefaultProvider {
			foundDefault = true
		}
	}

	if !foundDefault && cfg.LLM.DefaultProvider != "" {
		ve.Add("llm.default_provider %q does not match any configured provider", cfg.LLM.DefaultProvider)
	}
}

var validMemoryProviders = map[string]bool{
	"noop":      true,
	"markdown":  true,
	"vector":    true,
	"byterover": true,
}

func validateMemory(cfg *Config, ve *ValidationError) {
	if !validMemoryProviders[cfg.Memory.Provider] {
		ve.Add("memory.provider %q is invalid (want: noop, markdown, vector, byterover)", cfg.Memory.Provider)
	}
	if cfg.Memory.Provider == "byterover" {
		if cfg.Memory.ByteRover.BaseURL == "" {
			ve.Add("memory.byterover.base_url is required when provider is byterover")
		}
		if cfg.Memory.ByteRover.APIKey == "" {
			ve.Add("memory.byterover.api_key is required when provider is byterover")
		}
	}
	validateSearch(cfg, ve)
}

func validateSearch(cfg *Config, ve *ValidationError) {
	s := cfg.Memory.Search
	if s.DecayHalfLife < 0 {
		ve.Add("memory.search.decay_half_life must be >= 0")
	}
	if s.MMRDiversity < 0 || s.MMRDiversity > 1 {
		ve.Add("memory.search.mmr_diversity must be between 0 and 1")
	}
	if s.EmbeddingCacheSize < 0 {
		ve.Add("memory.search.embedding_cache_size must be >= 0")
	}
}

var validSearchBackends = map[string]bool{
	"searxng": true,
}

var validFilesystemBackends = map[string]bool{
	"local": true,
}

var validShellBackends = map[string]bool{
	"local": true,
}

var validBrowserBackends = map[string]bool{
	"chromedp": true,
}

var validCanvasBackends = map[string]bool{
	"local": true,
}

func validateTools(cfg *Config, ve *ValidationError) {
	if cfg.Tools.SandboxRoot == "" {
		ve.Add("tools.sandbox_root must not be empty")
	}
	if !validSearchBackends[cfg.Tools.SearchBackend] {
		ve.Add("tools.search_backend %q is invalid (want: searxng)", cfg.Tools.SearchBackend)
	}
	if cfg.Tools.SearchBackend == "searxng" && cfg.Tools.SearXNGURL == "" {
		ve.Add("tools.searxng_url is required when search_backend is searxng")
	}
	if !validFilesystemBackends[cfg.Tools.FilesystemBackend] {
		ve.Add("tools.filesystem_backend %q is invalid (want: local)", cfg.Tools.FilesystemBackend)
	}
	if !validShellBackends[cfg.Tools.ShellBackend] {
		ve.Add("tools.shell_backend %q is invalid (want: local)", cfg.Tools.ShellBackend)
	}
	if cfg.Tools.ShellTimeout <= 0 {
		ve.Add("tools.shell_timeout must be > 0")
	}
	if cfg.Tools.BrowserEnabled {
		if !validBrowserBackends[cfg.Tools.BrowserBackend] {
			ve.Add("tools.browser_backend %q is invalid (want: chromedp)", cfg.Tools.BrowserBackend)
		}
		if cfg.Tools.BrowserTimeout <= 0 {
			ve.Add("tools.browser_timeout must be > 0 when browser is enabled")
		}
	}
	if cfg.Tools.CanvasEnabled {
		if !validCanvasBackends[cfg.Tools.CanvasBackend] {
			ve.Add("tools.canvas_backend %q is invalid (want: local)", cfg.Tools.CanvasBackend)
		}
		if cfg.Tools.CanvasRoot == "" {
			ve.Add("tools.canvas_root must not be empty when canvas is enabled")
		}
		if cfg.Tools.CanvasMaxSize <= 0 {
			ve.Add("tools.canvas_max_size must be > 0 when canvas is enabled")
		}
	}
	if cfg.Tools.ProcessEnabled {
		if cfg.Tools.ProcessMaxSessions <= 0 {
			ve.Add("tools.process_max_sessions must be > 0 when process is enabled")
		}
		if cfg.Tools.ProcessSessionTTL <= 0 {
			ve.Add("tools.process_session_ttl must be > 0 when process is enabled")
		}
		if cfg.Tools.ProcessOutputMax <= 0 {
			ve.Add("tools.process_output_max must be > 0 when process is enabled")
		}
	}
	if cfg.Tools.WorkflowEnabled {
		if cfg.Tools.WorkflowDir == "" {
			ve.Add("tools.workflow_dir must not be empty when workflow is enabled")
		}
		if cfg.Tools.WorkflowTimeout <= 0 {
			ve.Add("tools.workflow_timeout must be > 0 when workflow is enabled")
		}
		if cfg.Tools.WorkflowMaxOutput <= 0 {
			ve.Add("tools.workflow_max_output must be > 0 when workflow is enabled")
		}
		if cfg.Tools.WorkflowMaxRunning <= 0 {
			ve.Add("tools.workflow_max_running must be > 0 when workflow is enabled")
		}
	}
	if cfg.Tools.LLMTaskEnabled {
		if cfg.Tools.LLMTaskTimeout <= 0 {
			ve.Add("tools.llm_task_timeout must be > 0 when llm_task is enabled")
		}
		if cfg.Tools.LLMTaskMaxTokens <= 0 {
			ve.Add("tools.llm_task_max_tokens must be > 0 when llm_task is enabled")
		}
		if cfg.Tools.LLMTaskMaxPromptSize <= 0 {
			ve.Add("tools.llm_task_max_prompt_size must be > 0 when llm_task is enabled")
		}
		if cfg.Tools.LLMTaskMaxInputSize <= 0 {
			ve.Add("tools.llm_task_max_input_size must be > 0 when llm_task is enabled")
		}
	}
	if cfg.Tools.CameraEnabled {
		if !cfg.Nodes.Enabled {
			ve.Add("tools.camera_enabled requires nodes.enabled to be true")
		}
		if cfg.Tools.CameraMaxPayloadSize <= 0 {
			ve.Add("tools.camera_max_payload_size must be > 0 when camera is enabled")
		}
		if cfg.Tools.CameraMaxClipDuration <= 0 {
			ve.Add("tools.camera_max_clip_duration must be > 0 when camera is enabled")
		}
		if cfg.Tools.CameraTimeout <= 0 {
			ve.Add("tools.camera_timeout must be > 0 when camera is enabled")
		}
	}
	if cfg.Tools.LocationEnabled {
		if !cfg.Nodes.Enabled {
			ve.Add("tools.location_enabled requires nodes.enabled to be true")
		}
		if cfg.Tools.LocationTimeout <= 0 {
			ve.Add("tools.location_timeout must be > 0 when location is enabled")
		}
		validAccuracies := map[string]bool{"coarse": true, "balanced": true, "precise": true}
		if !validAccuracies[cfg.Tools.LocationDefaultAccuracy] {
			ve.Add("tools.location_default_accuracy %q is invalid (want: coarse, balanced, precise)", cfg.Tools.LocationDefaultAccuracy)
		}
	}
	if cfg.Tools.NotesEnabled {
		if cfg.Tools.NotesDataDir == "" {
			ve.Add("tools.notes_data_dir must not be empty when notes is enabled")
		}
	}
	if cfg.Tools.GitHubEnabled {
		if cfg.Tools.GitHubTimeout <= 0 {
			ve.Add("tools.github_timeout must be > 0 when github is enabled")
		}
		if cfg.Tools.GitHubMaxRequestsPerMinute <= 0 {
			ve.Add("tools.github_max_requests_per_minute must be > 0 when github is enabled")
		}
	}
	if cfg.Tools.EmailEnabled {
		if cfg.Tools.EmailTimeout <= 0 {
			ve.Add("tools.email_timeout must be > 0 when email is enabled")
		}
		if cfg.Tools.EmailMaxSendsPerHour <= 0 {
			ve.Add("tools.email_max_sends_per_hour must be > 0 when email is enabled")
		}
	}
	if cfg.Tools.CalendarEnabled {
		if cfg.Tools.CalendarTimeout <= 0 {
			ve.Add("tools.calendar_timeout must be > 0 when calendar is enabled")
		}
	}
	if cfg.Tools.SmartHomeEnabled {
		if cfg.Tools.SmartHomeURL == "" {
			ve.Add("tools.smarthome_url must not be empty when smart_home is enabled")
		}
		if cfg.Tools.SmartHomeTimeout <= 0 {
			ve.Add("tools.smarthome_timeout must be > 0 when smart_home is enabled")
		}
		if cfg.Tools.SmartHomeMaxCallsPerMinute <= 0 {
			ve.Add("tools.smarthome_max_calls_per_minute must be > 0 when smart_home is enabled")
		}
	}
	if cfg.Tools.MCPEnabled {
		if len(cfg.Tools.MCPServers) == 0 {
			ve.Add("tools.mcp_servers must not be empty when mcp is enabled")
		}
		validMCPTransports := map[string]bool{"stdio": true, "http": true}
		names := make(map[string]bool)
		for i, s := range cfg.Tools.MCPServers {
			if s.Name == "" {
				ve.Add("tools.mcp_servers[%d].name must not be empty", i)
			} else if names[s.Name] {
				ve.Add("tools.mcp_servers[%d].name %q is duplicate", i, s.Name)
			}
			names[s.Name] = true
			if !validMCPTransports[s.Transport] {
				ve.Add("tools.mcp_servers[%d].transport %q is invalid (want: stdio, http)", i, s.Transport)
			}
			if s.Transport == "stdio" && s.Command == "" {
				ve.Add("tools.mcp_servers[%d].command is required for stdio transport", i)
			}
			if s.Transport == "http" && s.URL == "" {
				ve.Add("tools.mcp_servers[%d].url is required for http transport", i)
			}
		}
	}
	validateVoiceCall(cfg, ve)
	if cfg.Tools.MQTTEnabled {
		if cfg.Tools.MQTTBrokerURL == "" {
			ve.Add("tools.mqtt_broker_url is required when mqtt is enabled")
		}
	}
}

func validateVoiceCall(cfg *Config, ve *ValidationError) {
	vc := cfg.Tools.VoiceCall
	if !vc.Enabled {
		return
	}

	validProviders := map[string]bool{"twilio": true, "mock": true}
	if !validProviders[vc.Provider] {
		ve.Add("tools.voice_call.provider %q is invalid (want: twilio, mock)", vc.Provider)
	}

	if vc.FromNumber == "" {
		ve.Add("tools.voice_call.from_number is required when voice_call is enabled")
	} else if !isE164(vc.FromNumber) {
		ve.Add("tools.voice_call.from_number %q is not a valid E.164 phone number", vc.FromNumber)
	}

	if vc.Provider == "twilio" {
		if vc.TwilioAccountSID == "" {
			ve.Add("tools.voice_call.twilio_account_sid is required when provider is twilio")
		}
		if vc.TwilioAuthToken == "" {
			ve.Add("tools.voice_call.twilio_auth_token is required when provider is twilio")
		}
		if vc.WebhookPublicURL == "" {
			ve.Add("tools.voice_call.webhook_public_url is required when provider is twilio")
		}
	}

	if vc.MaxConcurrent <= 0 {
		ve.Add("tools.voice_call.max_concurrent must be > 0")
	}
	if vc.MaxDuration <= 0 {
		ve.Add("tools.voice_call.max_duration must be > 0")
	}
	if vc.Timeout <= 0 {
		ve.Add("tools.voice_call.timeout must be > 0")
	}

	validModes := map[string]bool{"notify": true, "conversation": true}
	if !validModes[vc.DefaultMode] {
		ve.Add("tools.voice_call.default_mode %q is invalid (want: notify, conversation)", vc.DefaultMode)
	}

	// Validate phone numbers in allowlist.
	for i, num := range vc.AllowedNumbers {
		if !isE164(num) {
			ve.Add("tools.voice_call.allowed_numbers[%d] %q is not a valid E.164 phone number", i, num)
		}
	}

	if vc.DefaultTo != "" && !isE164(vc.DefaultTo) {
		ve.Add("tools.voice_call.default_to %q is not a valid E.164 phone number", vc.DefaultTo)
	}
}

// isE164 validates an E.164 phone number format.
func isE164(phone string) bool {
	if len(phone) < 2 || len(phone) > 16 {
		return false
	}
	if phone[0] != '+' {
		return false
	}
	if phone[1] < '1' || phone[1] > '9' {
		return false
	}
	for _, c := range phone[2:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

var validChannelTypes = map[string]bool{
	"cli":        true,
	"http":       true,
	"telegram":   true,
	"discord":    true,
	"slack":      true,
	"webchat":    true,
	"whatsapp":   true,
	"matrix":     true,
	"googlechat": true,
	"teams":      true,
	"signal":     true,
	"irc":        true,
}

func validateChannels(cfg *Config, ve *ValidationError) {
	for i, ch := range cfg.Channels {
		if !validChannelTypes[ch.Type] {
			ve.Add("channels[%d].type %q is invalid (want: cli, http, telegram, discord, slack, webchat, whatsapp, matrix, googlechat, teams, signal, irc)", i, ch.Type)
			continue
		}
		switch ch.Type {
		case "http":
			if ch.HTTP == nil || ch.HTTP.Addr == "" {
				ve.Add("channels[%d] (http): http.addr is required", i)
			}
		case "telegram":
			if ch.Telegram == nil || ch.Telegram.Token == "" {
				ve.Add("channels[%d] (telegram): telegram.token is required (set via ALFREDAI_TELEGRAM_TOKEN)", i)
			}
		case "discord":
			if ch.Discord == nil || ch.Discord.Token == "" {
				ve.Add("channels[%d] (discord): discord.token is required (set via ALFREDAI_DISCORD_TOKEN)", i)
			}
		case "slack":
			if ch.Slack == nil {
				ve.Add("channels[%d] (slack): slack config section is required", i)
			} else {
				if ch.Slack.BotToken == "" {
					ve.Add("channels[%d] (slack): slack.bot_token is required (set via ALFREDAI_SLACK_BOT_TOKEN)", i)
				}
				if ch.Slack.AppToken == "" {
					ve.Add("channels[%d] (slack): slack.app_token is required (set via ALFREDAI_SLACK_APP_TOKEN)", i)
				}
			}
		case "whatsapp":
			if ch.WhatsApp == nil {
				ve.Add("channels[%d] (whatsapp): whatsapp config section is required", i)
			} else {
				if ch.WhatsApp.Token == "" {
					ve.Add("channels[%d] (whatsapp): whatsapp.token is required (set via ALFREDAI_WHATSAPP_TOKEN)", i)
				}
				if ch.WhatsApp.PhoneID == "" {
					ve.Add("channels[%d] (whatsapp): whatsapp.phone_id is required", i)
				}
				if ch.WhatsApp.VerifyToken == "" {
					ve.Add("channels[%d] (whatsapp): whatsapp.verify_token is required", i)
				}
			}
		case "matrix":
			if ch.Matrix == nil {
				ve.Add("channels[%d] (matrix): matrix config section is required", i)
			} else {
				if ch.Matrix.Homeserver == "" {
					ve.Add("channels[%d] (matrix): matrix.homeserver is required", i)
				}
				if ch.Matrix.AccessToken == "" {
					ve.Add("channels[%d] (matrix): matrix.access_token is required (set via ALFREDAI_MATRIX_ACCESS_TOKEN)", i)
				}
				if ch.Matrix.UserID == "" {
					ve.Add("channels[%d] (matrix): matrix.user_id is required", i)
				}
			}
		case "googlechat":
			if ch.GoogleChat == nil || ch.GoogleChat.CredentialsFile == "" {
				ve.Add("channels[%d] (googlechat): googlechat.credentials_file is required", i)
			}
		case "teams":
			if ch.Teams == nil {
				ve.Add("channels[%d] (teams): teams config section is required", i)
			} else {
				if ch.Teams.AppID == "" {
					ve.Add("channels[%d] (teams): teams.app_id is required", i)
				}
				if ch.Teams.AppSecret == "" {
					ve.Add("channels[%d] (teams): teams.app_secret is required", i)
				}
			}
		case "signal":
			if ch.Signal == nil {
				ve.Add("channels[%d] (signal): signal config section is required", i)
			} else {
				if ch.Signal.APIURL == "" {
					ve.Add("channels[%d] (signal): signal.api_url is required", i)
				}
				if ch.Signal.Phone == "" {
					ve.Add("channels[%d] (signal): signal.phone is required", i)
				}
			}
		case "irc":
			if ch.IRC == nil {
				ve.Add("channels[%d] (irc): irc config section is required", i)
			} else {
				if ch.IRC.Server == "" {
					ve.Add("channels[%d] (irc): irc.server is required", i)
				}
				if ch.IRC.Nick == "" {
					ve.Add("channels[%d] (irc): irc.nick is required", i)
				}
			}
		}
	}
}

func validateSecurity(cfg *Config, ve *ValidationError) {
	if cfg.Security.Audit.Enabled && cfg.Security.Audit.Path == "" {
		ve.Add("security.audit.path is required when audit is enabled")
	}
}

func validateScheduler(cfg *Config, ve *ValidationError) {
	if !cfg.Scheduler.Enabled {
		return
	}
	for i, t := range cfg.Scheduler.Tasks {
		if t.Name == "" {
			ve.Add("scheduler.tasks[%d].name is required", i)
		}
		if t.Schedule == "" {
			ve.Add("scheduler.tasks[%d].schedule is required", i)
		}
		if t.Action == "" {
			ve.Add("scheduler.tasks[%d].action is required", i)
		}
	}
}

func validateGateway(cfg *Config, ve *ValidationError) {
	if !cfg.Gateway.Enabled {
		return
	}
	if cfg.Gateway.Addr == "" {
		ve.Add("gateway.addr is required when gateway is enabled")
		return
	}
	if _, _, err := net.SplitHostPort(cfg.Gateway.Addr); err != nil {
		ve.Add("gateway.addr %q is not a valid host:port", cfg.Gateway.Addr)
	}
}

func validateAgents(cfg *Config, ve *ValidationError) {
	if cfg.Agents == nil {
		return
	}
	if cfg.Agents.Default == "" {
		ve.Add("agents.default must not be empty")
	}

	validRouting := map[string]bool{"default": true, "prefix": true, "config": true, "": true}
	if !validRouting[cfg.Agents.Routing] {
		ve.Add("agents.routing %q is invalid (want: default, prefix, config)", cfg.Agents.Routing)
	}

	seen := make(map[string]bool)
	foundDefault := false
	for i, inst := range cfg.Agents.Instances {
		if inst.ID == "" {
			ve.Add("agents.instances[%d].id must not be empty", i)
			continue
		}
		if seen[inst.ID] {
			ve.Add("agents.instances[%d]: duplicate agent ID %q", i, inst.ID)
		}
		seen[inst.ID] = true
		if inst.ID == cfg.Agents.Default {
			foundDefault = true
		}
	}

	if cfg.Agents.Default != "" && !foundDefault {
		ve.Add("agents.default %q does not match any configured instance", cfg.Agents.Default)
	}
}

func validateNodes(cfg *Config, ve *ValidationError) {
	if !cfg.Nodes.Enabled {
		return
	}
	if cfg.Nodes.HeartbeatInterval <= 0 {
		ve.Add("nodes.heartbeat_interval must be > 0 when nodes are enabled")
	}
	if cfg.Nodes.InvokeTimeout <= 0 {
		ve.Add("nodes.invoke_timeout must be > 0 when nodes are enabled")
	}
}

func validatePlugins(cfg *Config, ve *ValidationError) {
	if !cfg.Plugins.Enabled {
		return
	}
	if len(cfg.Plugins.Dirs) == 0 {
		ve.Add("plugins.dirs must have at least one entry when plugins are enabled")
	}
	if cfg.Plugins.WASMEnabled {
		if len(cfg.Plugins.Dirs) == 0 {
			ve.Add("plugins.dirs must have at least one entry when wasm_enabled is true")
		}
		if cfg.Plugins.WASMMaxMemoryMB < 0 || cfg.Plugins.WASMMaxMemoryMB > 512 {
			ve.Add("plugins.wasm_max_memory_mb must be between 1 and 512 (got %d)", cfg.Plugins.WASMMaxMemoryMB)
		}
		if cfg.Plugins.WASMExecTimeout != "" {
			d, err := time.ParseDuration(cfg.Plugins.WASMExecTimeout)
			if err != nil {
				ve.Add("plugins.wasm_exec_timeout %q is not a valid duration", cfg.Plugins.WASMExecTimeout)
			} else if d < 1*time.Second || d > 5*time.Minute {
				ve.Add("plugins.wasm_exec_timeout must be between 1s and 5m (got %s)", d)
			}
		}
	}
}

func validateTenants(cfg *Config, ve *ValidationError) {
	if cfg.Tenants == nil || !cfg.Tenants.Enabled {
		return
	}
	if cfg.Tenants.DataDir == "" {
		ve.Add("tenants.data_dir must be set when tenants are enabled")
	}
}

func validateOffline(cfg *Config, ve *ValidationError) {
	if cfg.Offline == nil || !cfg.Offline.Enabled {
		return
	}
	if cfg.Offline.CheckPeriod != "" {
		if _, err := time.ParseDuration(cfg.Offline.CheckPeriod); err != nil {
			ve.Add("offline.check_period %q is not a valid duration", cfg.Offline.CheckPeriod)
		}
	}
}

func validateCluster(cfg *Config, ve *ValidationError) {
	if cfg.Cluster == nil || !cfg.Cluster.Enabled {
		return
	}
	if cfg.Cluster.RedisURL == "" {
		ve.Add("cluster.redis_url is required when cluster mode is enabled")
	}
	if cfg.Cluster.LockTTL != "" {
		if _, err := time.ParseDuration(cfg.Cluster.LockTTL); err != nil {
			ve.Add("cluster.lock_ttl %q is not a valid duration", cfg.Cluster.LockTTL)
		}
	}
}
