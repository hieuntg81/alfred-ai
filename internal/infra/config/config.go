package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
	"gopkg.in/yaml.v3"
)

// PluginsConfig holds plugin system settings.
type PluginsConfig struct {
	Enabled          bool     `yaml:"enabled"`
	Dirs             []string `yaml:"dirs"`
	AllowPermissions []string `yaml:"allow_permissions"`
	DenyPermissions  []string `yaml:"deny_permissions"`
	WASMEnabled      bool     `yaml:"wasm_enabled"`
	WASMMaxMemoryMB  int      `yaml:"wasm_max_memory_mb"`  // global default, 64
	WASMExecTimeout  string   `yaml:"wasm_exec_timeout"`   // global default, "30s"
	RegistryURL      string   `yaml:"registry_url"`
}

// Config is the top-level application configuration.
type Config struct {
	Agent     AgentConfig     `yaml:"agent"`
	LLM       LLMConfig       `yaml:"llm"`
	Memory    MemoryConfig    `yaml:"memory"`
	Tools     ToolsConfig     `yaml:"tools"`
	Logger    LoggerConfig    `yaml:"logger"`
	Tracer    TracerConfig    `yaml:"tracer"`
	Security  SecurityConfig  `yaml:"security"`
	Skills    SkillsConfig    `yaml:"skills"`
	Scheduler SchedulerConfig `yaml:"scheduler"`
	Channels  []ChannelConfig `yaml:"channels"`
	Plugins   PluginsConfig   `yaml:"plugins"`
	Gateway   GatewayConfig   `yaml:"gateway"`
	Agents    *AgentsConfig   `yaml:"agents,omitempty"`  // nil = single-agent mode
	Nodes     NodesConfig     `yaml:"nodes"`
	Tenants   *TenantsConfig  `yaml:"tenants,omitempty"` // nil = single-tenant mode
	Offline   *OfflineConfig  `yaml:"offline,omitempty"` // nil = no offline support
	Cluster   *ClusterConfig  `yaml:"cluster,omitempty"` // nil = standalone mode
	Includes  []string        `yaml:"includes,omitempty"`
}

// TenantsConfig holds multi-tenant settings.
type TenantsConfig struct {
	Enabled bool   `yaml:"enabled"`
	DataDir string `yaml:"data_dir"` // root for tenant data dirs
}

// OfflineConfig holds offline/edge mode settings.
type OfflineConfig struct {
	Enabled     bool   `yaml:"enabled"`
	CheckURL    string `yaml:"check_url"`    // URL to check connectivity (default: https://1.1.1.1)
	CheckPeriod string `yaml:"check_period"` // duration string (default: 30s)
	QueueDir    string `yaml:"queue_dir"`    // directory for queued messages
}

// ClusterConfig holds horizontal scaling / cluster settings.
type ClusterConfig struct {
	Enabled  bool   `yaml:"enabled"`
	NodeID   string `yaml:"node_id"`   // auto-generated if empty
	RedisURL string `yaml:"redis_url"` // e.g. "redis://localhost:6379"
	LockTTL  string `yaml:"lock_ttl"`  // duration string (default: 30s)
}

// AgentsConfig holds multi-agent settings (Phase 3).
type AgentsConfig struct {
	Default      string                `yaml:"default"`
	Routing      string                `yaml:"routing"`            // "default", "prefix", "config"
	DataDir      string                `yaml:"data_dir,omitempty"` // workspace root (default: "./data")
	RoutingRules []RoutingRuleConfig   `yaml:"routing_rules,omitempty"`
	Instances    []AgentInstanceConfig `yaml:"instances"`
}

// RoutingRuleConfig maps a (channel, group) pair to an agent.
type RoutingRuleConfig struct {
	Channel string `yaml:"channel"`
	GroupID string `yaml:"group_id"`
	AgentID string `yaml:"agent_id"`
}

// AgentInstanceConfig defines a single agent instance.
type AgentInstanceConfig struct {
	ID           string            `yaml:"id"`
	Name         string            `yaml:"name"`
	Description  string            `yaml:"description"`
	SystemPrompt string            `yaml:"system_prompt"`
	Model        string            `yaml:"model"`
	Provider     string            `yaml:"provider"`
	Tools        []string          `yaml:"tools,omitempty"`
	Skills       []string          `yaml:"skills,omitempty"`
	MaxIter      int               `yaml:"max_iter,omitempty"`
	Metadata     map[string]string `yaml:"metadata,omitempty"`
}

// NodesConfig holds remote node system settings (Phase 5).
type NodesConfig struct {
	Enabled           bool                `yaml:"enabled"`
	HeartbeatInterval time.Duration       `yaml:"heartbeat_interval"`
	InvokeTimeout     time.Duration       `yaml:"invoke_timeout"`
	AllowedNodes      []string            `yaml:"allowed_nodes,omitempty"`
	Discovery         NodeDiscoveryConfig `yaml:"discovery"`
}

// NodeDiscoveryConfig holds node discovery settings.
// NOTE: The MDNS field is a config-level flag, but mDNS support also requires
// the binary to be built with the "mdns" build tag. If MDNS is true but the
// binary lacks the tag, the noop discoverer is used and mDNS will not work.
type NodeDiscoveryConfig struct {
	MDNS         bool          `yaml:"mdns"`
	ScanInterval time.Duration `yaml:"scan_interval"`
}

// SkillsConfig holds skill system settings.
type SkillsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Dir     string `yaml:"dir"`
}

// SchedulerConfig holds cron/scheduler settings.
type SchedulerConfig struct {
	Enabled bool                  `yaml:"enabled"`
	Tasks   []ScheduledTaskConfig `yaml:"tasks"`
}

// ScheduledTaskConfig defines a single scheduled task.
type ScheduledTaskConfig struct {
	Name     string `yaml:"name"`
	Schedule string `yaml:"schedule"` // cron expression or duration string
	Action   string `yaml:"action"`
	AgentID  string `yaml:"agent_id,omitempty"`
	Channel  string `yaml:"channel,omitempty"`
	Message  string `yaml:"message,omitempty"`
	OneShot  bool   `yaml:"one_shot,omitempty"`
}

// GatewayConfig holds WebSocket gateway settings.
type GatewayConfig struct {
	Enabled bool       `yaml:"enabled"`
	Addr    string     `yaml:"addr"`
	Auth    AuthConfig `yaml:"auth"`
}

// AuthConfig holds gateway authentication settings.
type AuthConfig struct {
	Type   string        `yaml:"type"` // "static" or ""
	Tokens []TokenConfig `yaml:"tokens,omitempty"`
}

// TokenConfig holds a single gateway auth token.
type TokenConfig struct {
	Token string   `yaml:"token"`
	Name  string   `yaml:"name"`
	Roles []string `yaml:"roles"`
}

// ChannelConfig holds settings for a single channel.
type ChannelConfig struct {
	Type        string   `yaml:"type"`
	MentionOnly bool     `yaml:"mention_only,omitempty"`
	ChannelIDs  []string `yaml:"channel_ids,omitempty"`

	// Per-channel nested config (only one should be set, matching Type).
	HTTP       *HTTPChannelConfig       `yaml:"http,omitempty"`
	Telegram   *TelegramChannelConfig   `yaml:"telegram,omitempty"`
	Discord    *DiscordChannelConfig    `yaml:"discord,omitempty"`
	Slack      *SlackChannelConfig      `yaml:"slack,omitempty"`
	WhatsApp   *WhatsAppChannelConfig   `yaml:"whatsapp,omitempty"`
	Matrix     *MatrixChannelConfig     `yaml:"matrix,omitempty"`
	GoogleChat *GoogleChatChannelConfig `yaml:"googlechat,omitempty"`
	Teams      *TeamsChannelConfig      `yaml:"teams,omitempty"`
	Signal     *SignalChannelConfig     `yaml:"signal,omitempty"`
	IRC        *IRCChannelConfig        `yaml:"irc,omitempty"`
}

// HTTPChannelConfig holds HTTP channel settings.
type HTTPChannelConfig struct {
	Addr string `yaml:"addr"`
}

// TelegramChannelConfig holds Telegram channel settings.
type TelegramChannelConfig struct {
	Token string `yaml:"token"`
}

// DiscordChannelConfig holds Discord channel settings.
type DiscordChannelConfig struct {
	Token   string `yaml:"token"`
	GuildID string `yaml:"guild_id,omitempty"`
}

// SlackChannelConfig holds Slack channel settings.
type SlackChannelConfig struct {
	BotToken string `yaml:"bot_token"`
	AppToken string `yaml:"app_token"`
}

// WhatsAppChannelConfig holds WhatsApp channel settings.
type WhatsAppChannelConfig struct {
	Token       string `yaml:"token"`
	PhoneID     string `yaml:"phone_id"`
	VerifyToken string `yaml:"verify_token"`
	AppSecret   string `yaml:"app_secret,omitempty"`
	WebhookAddr string `yaml:"webhook_addr,omitempty"`
}

// MatrixChannelConfig holds Matrix channel settings.
type MatrixChannelConfig struct {
	Homeserver  string `yaml:"homeserver"`
	AccessToken string `yaml:"access_token"`
	UserID      string `yaml:"user_id"`
}

// GoogleChatChannelConfig holds Google Chat channel settings.
type GoogleChatChannelConfig struct {
	CredentialsFile string `yaml:"credentials_file"`
	SpaceID         string `yaml:"space_id,omitempty"`
	WebhookAddr     string `yaml:"webhook_addr,omitempty"`
}

// TeamsChannelConfig holds Microsoft Teams channel settings.
type TeamsChannelConfig struct {
	AppID       string `yaml:"app_id"`
	AppSecret   string `yaml:"app_secret"`
	WebhookAddr string `yaml:"webhook_addr,omitempty"`
	TenantID    string `yaml:"tenant_id,omitempty"`
}

// SignalChannelConfig holds Signal channel settings.
type SignalChannelConfig struct {
	APIURL       string        `yaml:"api_url"`
	Phone        string        `yaml:"phone"`
	PollInterval time.Duration `yaml:"poll_interval,omitempty"`
}

// IRCChannelConfig holds IRC channel settings.
type IRCChannelConfig struct {
	Server   string   `yaml:"server"`
	Nick     string   `yaml:"nick"`
	Password string   `yaml:"password,omitempty"`
	Channels []string `yaml:"channels"`
	UseTLS   bool     `yaml:"use_tls,omitempty"`
}

// SecurityConfig holds security and privacy settings.
type SecurityConfig struct {
	Encryption     EncryptionConfig `yaml:"encryption"`
	Audit          AuditConfig      `yaml:"audit"`
	ConsentDir     string           `yaml:"consent_dir"`
	KeyRotation    KeyRotationConfig `yaml:"key_rotation"`
	SecretScanning SecretScanConfig  `yaml:"secret_scanning"`
	RBAC           RBACConfig        `yaml:"rbac"`
}

// RBACConfig holds role-based access control settings.
type RBACConfig struct {
	Enabled bool `yaml:"enabled"`
}

// KeyRotationConfig holds encryption key rotation settings.
type KeyRotationConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Interval string `yaml:"interval"` // duration string, e.g. "720h" (30 days)
}

// SecretScanConfig holds secret scanning settings.
type SecretScanConfig struct {
	Enabled        bool                   `yaml:"enabled"`
	CustomPatterns []SecretPatternConfig  `yaml:"custom_patterns,omitempty"`
}

// SecretPatternConfig defines a custom secret scanning pattern.
type SecretPatternConfig struct {
	Name    string `yaml:"name"`
	Pattern string `yaml:"pattern"`
	Action  string `yaml:"action"` // "redact", "warn", "block"
}

// EncryptionConfig holds content encryption settings.
// Passphrase is read from ALFREDAI_ENCRYPTION_KEY env var.
type EncryptionConfig struct {
	Enabled bool `yaml:"enabled"`
}

// AuditConfig holds audit logging settings.
type AuditConfig struct {
	Enabled   bool            `yaml:"enabled"`
	Path      string          `yaml:"path"`
	Retention RetentionConfig `yaml:"retention"`
}

// RetentionConfig holds audit log retention policy settings.
type RetentionConfig struct {
	MaxAge  string `yaml:"max_age"`  // duration string, e.g. "2160h" (90 days)
	MaxSize string `yaml:"max_size"` // e.g. "100MB"
}

// ToolApprovalConfig holds tool approval gating settings.
type ToolApprovalConfig struct {
	Enabled       bool     `yaml:"enabled"`
	AlwaysApprove []string `yaml:"always_approve"`
	AlwaysDeny    []string `yaml:"always_deny"`
}

// AgentConfig holds agent behavior settings.
type AgentConfig struct {
	MaxIterations int                  `yaml:"max_iterations"`
	Timeout       time.Duration        `yaml:"timeout"`
	SystemPrompt  string               `yaml:"system_prompt"`
	Compression   CompressionConfig    `yaml:"compression"`
	SubAgent      SubAgentConfig       `yaml:"sub_agent"`
	ToolApproval  ToolApprovalConfig   `yaml:"tool_approval"`
	ContextGuard  ContextGuardConfig   `yaml:"context_guard"`
}

// ContextGuardConfig controls proactive context window overflow prevention.
type ContextGuardConfig struct {
	Enabled       bool    `yaml:"enabled"`
	MaxTokens     int     `yaml:"max_tokens"`      // default: 128000
	ReserveTokens int     `yaml:"reserve_tokens"`   // default: 1000
	SafetyMargin  float64 `yaml:"safety_margin"`    // default: 0.15
}

// CompressionConfig controls context compression behavior.
type CompressionConfig struct {
	Enabled    bool `yaml:"enabled"`
	Threshold  int  `yaml:"threshold"`
	KeepRecent int  `yaml:"keep_recent"`
}

// SubAgentConfig controls sub-agent spawning behavior.
type SubAgentConfig struct {
	Enabled       bool          `yaml:"enabled"`
	MaxSubAgents  int           `yaml:"max_sub_agents"`
	MaxIterations int           `yaml:"max_iterations"`
	Timeout       time.Duration `yaml:"timeout"`
}

// FailoverConfig holds model failover settings.
type FailoverConfig struct {
	Enabled   bool     `yaml:"enabled"`
	Fallbacks []string `yaml:"fallbacks"`
}

// LLMConfig holds LLM provider settings.
type LLMConfig struct {
	DefaultProvider string               `yaml:"default_provider"`
	Providers       []ProviderConfig     `yaml:"providers"`
	Failover        FailoverConfig       `yaml:"failover"`
	CircuitBreaker  CircuitBreakerConfig `yaml:"circuit_breaker"`
	ModelRouting    map[string]string    `yaml:"model_routing,omitempty"` // preference → provider name, e.g. "fast" → "groq"
}

// CircuitBreakerConfig holds circuit breaker settings for LLM providers.
type CircuitBreakerConfig struct {
	Enabled     bool          `yaml:"enabled"`
	MaxFailures uint32        `yaml:"max_failures"`
	Timeout     time.Duration `yaml:"timeout"`
	Interval    time.Duration `yaml:"interval"`
}

// PoolConfig holds HTTP connection pool settings for LLM providers.
type PoolConfig struct {
	MaxIdleConns        int           `yaml:"max_idle_conns"`
	MaxIdleConnsPerHost int           `yaml:"max_idle_conns_per_host"`
	MaxConnsPerHost     int           `yaml:"max_conns_per_host"`
	IdleConnTimeout     time.Duration `yaml:"idle_conn_timeout"`
}

// ProviderConfig holds settings for a single LLM provider.
type ProviderConfig struct {
	Name           string        `yaml:"name"`
	Type           string        `yaml:"type"`
	BaseURL        string        `yaml:"base_url"`
	APIKey         string        `yaml:"api_key"`
	Model          string        `yaml:"model"`
	Region         string        `yaml:"region,omitempty"`
	ConnTimeout    time.Duration `yaml:"conn_timeout"`
	RespTimeout    time.Duration `yaml:"resp_timeout"`
	Pool           PoolConfig    `yaml:"pool"`
	ThinkingBudget int           `yaml:"thinking_budget,omitempty"`
}

// MemoryConfig holds memory provider settings.
type MemoryConfig struct {
	Provider   string          `yaml:"provider"`
	DataDir    string          `yaml:"data_dir"`
	AutoCurate bool            `yaml:"auto_curate"`
	ByteRover  ByteRoverConfig `yaml:"byterover"`
	Embedding  EmbeddingConfig `yaml:"embedding"`
	Search     SearchConfig    `yaml:"search"`
}

// SearchConfig holds vector memory search tuning parameters.
// All fields default to zero (disabled) for backward compatibility.
type SearchConfig struct {
	DecayHalfLife       time.Duration `yaml:"decay_half_life"`        // 0 = disabled
	MMRDiversity        float64       `yaml:"mmr_diversity"`          // 0-1, 0 = disabled
	EmbeddingCacheSize  int           `yaml:"embedding_cache_size"`   // 0 = disabled
	MaxVectorCandidates int           `yaml:"max_vector_candidates"`  // 0 = default (10000)
}

// EmbeddingConfig holds text embedding provider settings.
type EmbeddingConfig struct {
	Provider string `yaml:"provider"` // "openai", "gemini", ""
	Model    string `yaml:"model"`
	APIKey   string `yaml:"api_key,omitempty"`
}

// ByteRoverConfig holds ByteRover API settings.
type ByteRoverConfig struct {
	BaseURL   string `yaml:"base_url"`
	APIKey    string `yaml:"api_key"`
	ProjectID string `yaml:"project_id"`
}

// VoiceCallConfig holds voice call tool settings.
type VoiceCallConfig struct {
	Enabled           bool          `yaml:"enabled"`
	Provider          string        `yaml:"provider"`              // "twilio" | "mock"
	FromNumber        string        `yaml:"from_number"`           // E.164
	DefaultTo         string        `yaml:"default_to"`            // E.164 optional
	DefaultMode       string        `yaml:"default_mode"`          // "notify" | "conversation"
	MaxConcurrent     int           `yaml:"max_concurrent"`
	MaxDuration       time.Duration `yaml:"max_duration"`
	TranscriptTimeout time.Duration `yaml:"transcript_timeout"`
	Timeout           time.Duration `yaml:"timeout"`
	AllowedNumbers    []string      `yaml:"allowed_numbers"`       // E.164 allowlist
	DataDir           string        `yaml:"data_dir"`              // JSONL store path

	// Twilio credentials.
	TwilioAccountSID string `yaml:"twilio_account_sid"`
	TwilioAuthToken  string `yaml:"twilio_auth_token"`

	// Webhook server.
	WebhookAddr       string `yaml:"webhook_addr"`         // ":3334"
	WebhookPath       string `yaml:"webhook_path"`         // "/voice/webhook"
	WebhookPublicURL  string `yaml:"webhook_public_url"`   // ngrok/cloudflare URL
	WebhookSkipVerify bool   `yaml:"webhook_skip_verify"`  // dev-only
	StreamPath        string `yaml:"stream_path"`          // "/voice/stream"

	// TTS/STT.
	OpenAIAPIKey      string `yaml:"openai_api_key"`       // fallback; prefers LLM registry
	TTSVoice          string `yaml:"tts_voice"`            // "alloy", "nova", etc.
	TTSModel          string `yaml:"tts_model"`            // "tts-1", "tts-1-hd"
	STTModel          string `yaml:"stt_model"`            // "gpt-4o-transcribe"
	SilenceDurationMs int    `yaml:"silence_duration_ms"`  // VAD silence threshold
}

// ToolsConfig holds tool system settings.
type ToolsConfig struct {
	SandboxRoot             string        `yaml:"sandbox_root"`
	AllowedCommands         []string      `yaml:"allowed_commands"`
	SearchBackend           string        `yaml:"search_backend"`
	SearXNGURL              string        `yaml:"searxng_url"`
	SearchCacheTTL          time.Duration `yaml:"search_cache_ttl"`
	FilesystemBackend       string        `yaml:"filesystem_backend"`
	ShellBackend            string        `yaml:"shell_backend"`
	ShellTimeout            time.Duration `yaml:"shell_timeout"`
	BrowserEnabled          bool          `yaml:"browser_enabled"`
	BrowserBackend          string        `yaml:"browser_backend"`
	BrowserCDPURL           string        `yaml:"browser_cdp_url"`
	BrowserHeadless         bool          `yaml:"browser_headless"`
	BrowserTimeout          time.Duration `yaml:"browser_timeout"`
	CanvasEnabled           bool          `yaml:"canvas_enabled"`
	CanvasBackend           string        `yaml:"canvas_backend"`
	CanvasRoot              string        `yaml:"canvas_root"`
	CanvasMaxSize           int           `yaml:"canvas_max_size"`
	CronEnabled             bool          `yaml:"cron_enabled"`
	CronDataDir             string        `yaml:"cron_data_dir"`
	ProcessEnabled          bool          `yaml:"process_enabled"`
	ProcessMaxSessions      int           `yaml:"process_max_sessions"`
	ProcessSessionTTL       time.Duration `yaml:"process_session_ttl"`
	ProcessOutputMax        int           `yaml:"process_output_max"`
	MessageEnabled          bool          `yaml:"message_enabled"`
	WorkflowEnabled         bool          `yaml:"workflow_enabled"`
	WorkflowDir             string        `yaml:"workflow_dir"`
	WorkflowDataDir         string        `yaml:"workflow_data_dir"`
	WorkflowTimeout         time.Duration `yaml:"workflow_timeout"`
	WorkflowMaxOutput       int           `yaml:"workflow_max_output"`
	WorkflowMaxRunning      int           `yaml:"workflow_max_running"`
	WorkflowAllowedCommands []string      `yaml:"workflow_allowed_commands"`
	LLMTaskEnabled          bool          `yaml:"llm_task_enabled"`
	LLMTaskTimeout          time.Duration `yaml:"llm_task_timeout"`
	LLMTaskMaxTokens        int           `yaml:"llm_task_max_tokens"`
	LLMTaskAllowedModels    []string      `yaml:"llm_task_allowed_models"`
	LLMTaskMaxPromptSize    int           `yaml:"llm_task_max_prompt_size"`
	LLMTaskMaxInputSize     int           `yaml:"llm_task_max_input_size"`
	LLMTaskDefaultModel     string        `yaml:"llm_task_default_model"`
	CameraEnabled           bool          `yaml:"camera_enabled"`
	CameraMaxPayloadSize    int           `yaml:"camera_max_payload_size"`
	CameraMaxClipDuration   time.Duration `yaml:"camera_max_clip_duration"`
	CameraTimeout           time.Duration `yaml:"camera_timeout"`
	LocationEnabled         bool          `yaml:"location_enabled"`
	LocationTimeout         time.Duration `yaml:"location_timeout"`
	LocationDefaultAccuracy string        `yaml:"location_default_accuracy"`
	VoiceCall               VoiceCallConfig `yaml:"voice_call"`

	// Notes tool.
	NotesEnabled bool   `yaml:"notes_enabled"`
	NotesDataDir string `yaml:"notes_data_dir"`

	// GitHub tool.
	GitHubEnabled              bool          `yaml:"github_enabled"`
	GitHubTimeout              time.Duration `yaml:"github_timeout"`
	GitHubMaxRequestsPerMinute int           `yaml:"github_max_requests_per_minute"`

	// Email tool.
	EmailEnabled         bool          `yaml:"email_enabled"`
	EmailTimeout         time.Duration `yaml:"email_timeout"`
	EmailMaxSendsPerHour int           `yaml:"email_max_sends_per_hour"`
	EmailAllowedDomains  []string      `yaml:"email_allowed_domains"`

	// Calendar tool.
	CalendarEnabled bool          `yaml:"calendar_enabled"`
	CalendarTimeout time.Duration `yaml:"calendar_timeout"`

	// Smart Home tool.
	SmartHomeEnabled           bool          `yaml:"smarthome_enabled"`
	SmartHomeURL               string        `yaml:"smarthome_url"`
	SmartHomeToken             string        `yaml:"smarthome_token"`
	SmartHomeTimeout           time.Duration `yaml:"smarthome_timeout"`
	SmartHomeMaxCallsPerMinute int           `yaml:"smarthome_max_calls_per_minute"`

	// MCP (Model Context Protocol) bridge.
	MCPEnabled bool        `yaml:"mcp_enabled"`
	MCPServers []MCPServer `yaml:"mcp_servers,omitempty"`

	// MQTT for IoT device communication.
	MQTTEnabled  bool   `yaml:"mqtt_enabled"`
	MQTTBrokerURL string `yaml:"mqtt_broker_url"` // e.g. "tcp://localhost:1883"
	MQTTClientID  string `yaml:"mqtt_client_id"`
	MQTTUsername  string `yaml:"mqtt_username"`
	MQTTPassword  string `yaml:"mqtt_password"`

	// GPIO for edge/IoT pin control (edge builds only).
	GPIOEnabled bool `yaml:"gpio_enabled"`

	// Serial for USB serial communication (edge builds only).
	SerialEnabled bool `yaml:"serial_enabled"`

	// BLE for Bluetooth Low Energy communication (edge builds only).
	BLEEnabled bool `yaml:"ble_enabled"`
}

// MCPServer configures an MCP server connection.
type MCPServer struct {
	Name      string            `yaml:"name"`
	Transport string            `yaml:"transport"` // "stdio" or "http"
	Command   string            `yaml:"command,omitempty"`
	Args      []string          `yaml:"args,omitempty"`
	URL       string            `yaml:"url,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
}

// LoggerConfig holds logging settings.
type LoggerConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	Output string `yaml:"output"`
}

// TracerConfig holds tracing settings.
type TracerConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Exporter string `yaml:"exporter"`
	Endpoint string `yaml:"endpoint"`
}

// defaultDataDir returns the persistent data directory under $HOME/.alfredai/data.
// Falls back to "./data" if $HOME cannot be determined.
func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "./data"
	}
	return filepath.Join(home, ".alfredai", "data")
}

// Defaults returns a Config with sensible defaults.
func Defaults() *Config {
	dataDir := defaultDataDir()
	return &Config{
		Agent: AgentConfig{
			MaxIterations: 10,
			Timeout:       120 * time.Second,
			SystemPrompt:  "You are alfred-ai, a helpful AI assistant.",
			Compression: CompressionConfig{
				Enabled:    false,
				Threshold:  30,
				KeepRecent: 10,
			},
			SubAgent: SubAgentConfig{
				Enabled:       false,
				MaxSubAgents:  5,
				MaxIterations: 5,
				Timeout:       60 * time.Second,
			},
			ContextGuard: ContextGuardConfig{
				Enabled:       false,
				MaxTokens:     128000,
				ReserveTokens: 1000,
				SafetyMargin:  0.15,
			},
		},
		LLM: LLMConfig{
			DefaultProvider: "openai",
		},
		Memory: MemoryConfig{
			Provider:   "noop",
			DataDir:    filepath.Join(dataDir, "memory"),
			AutoCurate: false,
		},
		Tools: ToolsConfig{
			SandboxRoot: ".",
			AllowedCommands: []string{
				"ls", "cat", "grep", "find", "git", "go", "python",
			},
			SearchBackend:      "searxng",
			SearXNGURL:         "http://localhost:6060",
			SearchCacheTTL:     15 * time.Minute,
			FilesystemBackend:  "local",
			ShellBackend:       "local",
			ShellTimeout:       30 * time.Second,
			BrowserEnabled:     false,
			BrowserBackend:     "chromedp",
			BrowserCDPURL:      "",
			BrowserHeadless:    true,
			BrowserTimeout:     30 * time.Second,
			CanvasEnabled:      false,
			CanvasBackend:      "local",
			CanvasRoot:         filepath.Join(dataDir, "canvas"),
			CanvasMaxSize:      512 * 1024,
			CronEnabled:        false,
			CronDataDir:        filepath.Join(dataDir, "cron"),
			ProcessEnabled:     false,
			ProcessMaxSessions: 10,
			ProcessSessionTTL:  30 * time.Minute,
			ProcessOutputMax:   1024 * 1024,
			MessageEnabled:     false,
			WorkflowEnabled:    false,
			WorkflowDir:        "./workflows",
			WorkflowDataDir:    filepath.Join(dataDir, "workflows"),
			WorkflowTimeout:    120 * time.Second,
			WorkflowMaxOutput:  1024 * 1024, // 1 MiB
			WorkflowMaxRunning: 5,
			LLMTaskEnabled:     false,
			LLMTaskTimeout:     30 * time.Second,
			LLMTaskMaxTokens:   4096,
			LLMTaskMaxPromptSize:  32 * 1024,  // 32 KiB
			LLMTaskMaxInputSize:   256 * 1024, // 256 KiB
			CameraEnabled:         false,
			CameraMaxPayloadSize:  5 * 1024 * 1024, // 5 MiB
			CameraMaxClipDuration: 60 * time.Second,
			CameraTimeout:         30 * time.Second,
			LocationEnabled:         false,
			LocationTimeout:         10 * time.Second,
			LocationDefaultAccuracy: "balanced",
			VoiceCall: VoiceCallConfig{
				Enabled:           false,
				Provider:          "twilio",
				DefaultMode:       "notify",
				MaxConcurrent:     1,
				MaxDuration:       5 * time.Minute,
				TranscriptTimeout: 3 * time.Minute,
				Timeout:           30 * time.Second,
				DataDir:           filepath.Join(dataDir, "voice-calls"),
				WebhookAddr:       ":3334",
				WebhookPath:       "/voice/webhook",
				StreamPath:        "/voice/stream",
				TTSVoice:          "alloy",
				TTSModel:          "tts-1",
				STTModel:          "gpt-4o-transcribe",
				SilenceDurationMs: 800,
			},
			NotesEnabled:               false,
			NotesDataDir:               filepath.Join(dataDir, "notes"),
			GitHubEnabled:              false,
			GitHubTimeout:              15 * time.Second,
			GitHubMaxRequestsPerMinute: 30,
			EmailEnabled:               false,
			EmailTimeout:               30 * time.Second,
			EmailMaxSendsPerHour:       10,
			CalendarEnabled:            false,
			CalendarTimeout:            15 * time.Second,
			SmartHomeEnabled:           false,
			SmartHomeURL:               "",
			SmartHomeTimeout:           10 * time.Second,
			SmartHomeMaxCallsPerMinute: 60,
		},
		Logger: LoggerConfig{
			Level:  "info",
			Format: "text",
			Output: "stderr",
		},
		Tracer: TracerConfig{
			Enabled:  false,
			Exporter: "noop",
		},
		Security: SecurityConfig{
			Encryption: EncryptionConfig{Enabled: false},
			Audit: AuditConfig{
				Enabled: true,
				Path:    filepath.Join(dataDir, "audit.jsonl"),
			},
			ConsentDir: dataDir,
		},
		Skills: SkillsConfig{
			Enabled: false,
			Dir:     "./skills",
		},
		Scheduler: SchedulerConfig{
			Enabled: false,
		},
		Plugins: PluginsConfig{
			Enabled: false,
			Dirs:    []string{"./plugins"},
		},
		Gateway: GatewayConfig{
			Enabled: false,
			Addr:    ":8090",
		},
		Nodes: NodesConfig{
			Enabled:           false,
			HeartbeatInterval: 30 * time.Second,
			InvokeTimeout:     30 * time.Second,
			Discovery: NodeDiscoveryConfig{
				MDNS:         false,
				ScanInterval: 60 * time.Second,
			},
		},
	}
}

// Load reads a YAML config file, applies env var overrides, and decrypts secrets.
func Load(path string) (*Config, error) {
	cfg := Defaults()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			ApplyEnvOverrides(cfg)
			if err := Validate(cfg); err != nil {
				return nil, err
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}

	if err := validatePermissions(absPath); err != nil {
		return nil, err
	}

	// First pass: unmarshal to get the includes list.
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Process includes (merges included files into cfg).
	hasIncludes := len(cfg.Includes) > 0
	if hasIncludes {
		visited := map[string]bool{absPath: true}
		if err := processIncludes(cfg, filepath.Dir(absPath), visited, 0); err != nil {
			return nil, err
		}

		// Second pass: re-unmarshal main config so it takes precedence over includes.
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config (second pass): %w", err)
		}
		cfg.Includes = nil
	}

	ApplyEnvOverrides(cfg)

	passphrase := os.Getenv("ALFREDAI_CONFIG_KEY")
	if passphrase != "" {
		if err := decryptSecrets(cfg, passphrase); err != nil {
			return nil, fmt.Errorf("decrypt secrets: %w", err)
		}
	}

	if err := Validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// ApplyEnvOverrides maps ALFREDAI_* env vars to config fields.
func ApplyEnvOverrides(cfg *Config) {
	if v := os.Getenv("ALFREDAI_LLM_DEFAULT_PROVIDER"); v != "" {
		cfg.LLM.DefaultProvider = v
	}
	if v := os.Getenv("ALFREDAI_LOGGER_LEVEL"); v != "" {
		cfg.Logger.Level = v
	}
	if v := os.Getenv("ALFREDAI_TRACER_ENABLED"); v == "true" {
		cfg.Tracer.Enabled = true
	}
	if v := os.Getenv("ALFREDAI_TRACER_EXPORTER"); v != "" {
		cfg.Tracer.Exporter = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_SANDBOX_ROOT"); v != "" {
		cfg.Tools.SandboxRoot = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_SEARCH_BACKEND"); v != "" {
		cfg.Tools.SearchBackend = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_SEARXNG_URL"); v != "" {
		cfg.Tools.SearXNGURL = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_FILESYSTEM_BACKEND"); v != "" {
		cfg.Tools.FilesystemBackend = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_SHELL_BACKEND"); v != "" {
		cfg.Tools.ShellBackend = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_SHELL_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Tools.ShellTimeout = d
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_BROWSER_ENABLED"); v == "true" {
		cfg.Tools.BrowserEnabled = true
	}
	if v := os.Getenv("ALFREDAI_TOOLS_BROWSER_BACKEND"); v != "" {
		cfg.Tools.BrowserBackend = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_BROWSER_CDP_URL"); v != "" {
		cfg.Tools.BrowserCDPURL = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_BROWSER_HEADLESS"); v == "false" {
		cfg.Tools.BrowserHeadless = false
	}
	if v := os.Getenv("ALFREDAI_TOOLS_BROWSER_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Tools.BrowserTimeout = d
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_CANVAS_ENABLED"); v == "true" {
		cfg.Tools.CanvasEnabled = true
	}
	if v := os.Getenv("ALFREDAI_TOOLS_CANVAS_BACKEND"); v != "" {
		cfg.Tools.CanvasBackend = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_CANVAS_ROOT"); v != "" {
		cfg.Tools.CanvasRoot = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_CRON_ENABLED"); v == "true" {
		cfg.Tools.CronEnabled = true
	}
	if v := os.Getenv("ALFREDAI_TOOLS_CRON_DATA_DIR"); v != "" {
		cfg.Tools.CronDataDir = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_PROCESS_ENABLED"); v == "true" {
		cfg.Tools.ProcessEnabled = true
	}
	if v := os.Getenv("ALFREDAI_TOOLS_PROCESS_MAX_SESSIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Tools.ProcessMaxSessions = n
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_PROCESS_SESSION_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Tools.ProcessSessionTTL = d
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_PROCESS_OUTPUT_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Tools.ProcessOutputMax = n
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_MESSAGE_ENABLED"); v == "true" {
		cfg.Tools.MessageEnabled = true
	}
	if v := os.Getenv("ALFREDAI_TOOLS_WORKFLOW_ENABLED"); v == "true" {
		cfg.Tools.WorkflowEnabled = true
	}
	if v := os.Getenv("ALFREDAI_TOOLS_WORKFLOW_DIR"); v != "" {
		cfg.Tools.WorkflowDir = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_WORKFLOW_DATA_DIR"); v != "" {
		cfg.Tools.WorkflowDataDir = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_WORKFLOW_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Tools.WorkflowTimeout = d
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_WORKFLOW_MAX_OUTPUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Tools.WorkflowMaxOutput = n
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_WORKFLOW_MAX_RUNNING"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Tools.WorkflowMaxRunning = n
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_WORKFLOW_ALLOWED_COMMANDS"); v != "" {
		cfg.Tools.WorkflowAllowedCommands = splitAndTrim(v, ",")
	}

	if v := os.Getenv("ALFREDAI_TOOLS_LLM_TASK_ENABLED"); v == "true" {
		cfg.Tools.LLMTaskEnabled = true
	}
	if v := os.Getenv("ALFREDAI_TOOLS_LLM_TASK_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Tools.LLMTaskTimeout = d
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_LLM_TASK_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Tools.LLMTaskMaxTokens = n
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_LLM_TASK_ALLOWED_MODELS"); v != "" {
		cfg.Tools.LLMTaskAllowedModels = splitAndTrim(v, ",")
	}
	if v := os.Getenv("ALFREDAI_TOOLS_LLM_TASK_MAX_PROMPT_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Tools.LLMTaskMaxPromptSize = n
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_LLM_TASK_MAX_INPUT_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Tools.LLMTaskMaxInputSize = n
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_LLM_TASK_DEFAULT_MODEL"); v != "" {
		cfg.Tools.LLMTaskDefaultModel = v
	}

	if v := os.Getenv("ALFREDAI_TOOLS_CAMERA_ENABLED"); v == "true" {
		cfg.Tools.CameraEnabled = true
	}
	if v := os.Getenv("ALFREDAI_TOOLS_CAMERA_MAX_PAYLOAD_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Tools.CameraMaxPayloadSize = n
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_CAMERA_MAX_CLIP_DURATION"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Tools.CameraMaxClipDuration = d
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_CAMERA_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Tools.CameraTimeout = d
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_LOCATION_ENABLED"); v == "true" {
		cfg.Tools.LocationEnabled = true
	}
	if v := os.Getenv("ALFREDAI_TOOLS_LOCATION_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Tools.LocationTimeout = d
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_LOCATION_DEFAULT_ACCURACY"); v != "" {
		cfg.Tools.LocationDefaultAccuracy = v
	}

	// Voice call overrides.
	if v := os.Getenv("ALFREDAI_TOOLS_VOICE_CALL_ENABLED"); v == "true" {
		cfg.Tools.VoiceCall.Enabled = true
	}
	if v := os.Getenv("ALFREDAI_TOOLS_VOICE_CALL_PROVIDER"); v != "" {
		cfg.Tools.VoiceCall.Provider = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_VOICE_CALL_FROM_NUMBER"); v != "" {
		cfg.Tools.VoiceCall.FromNumber = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_VOICE_CALL_DEFAULT_TO"); v != "" {
		cfg.Tools.VoiceCall.DefaultTo = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_VOICE_CALL_DEFAULT_MODE"); v != "" {
		cfg.Tools.VoiceCall.DefaultMode = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_VOICE_CALL_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Tools.VoiceCall.MaxConcurrent = n
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_VOICE_CALL_MAX_DURATION"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Tools.VoiceCall.MaxDuration = d
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_VOICE_CALL_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Tools.VoiceCall.Timeout = d
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_VOICE_CALL_TWILIO_ACCOUNT_SID"); v != "" {
		cfg.Tools.VoiceCall.TwilioAccountSID = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_VOICE_CALL_TWILIO_AUTH_TOKEN"); v != "" {
		cfg.Tools.VoiceCall.TwilioAuthToken = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_VOICE_CALL_WEBHOOK_ADDR"); v != "" {
		cfg.Tools.VoiceCall.WebhookAddr = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_VOICE_CALL_WEBHOOK_PUBLIC_URL"); v != "" {
		cfg.Tools.VoiceCall.WebhookPublicURL = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_VOICE_CALL_WEBHOOK_SKIP_VERIFY"); v == "true" {
		cfg.Tools.VoiceCall.WebhookSkipVerify = true
	}
	if v := os.Getenv("ALFREDAI_TOOLS_VOICE_CALL_OPENAI_API_KEY"); v != "" {
		cfg.Tools.VoiceCall.OpenAIAPIKey = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_VOICE_CALL_TTS_VOICE"); v != "" {
		cfg.Tools.VoiceCall.TTSVoice = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_VOICE_CALL_TTS_MODEL"); v != "" {
		cfg.Tools.VoiceCall.TTSModel = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_VOICE_CALL_STT_MODEL"); v != "" {
		cfg.Tools.VoiceCall.STTModel = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_VOICE_CALL_DATA_DIR"); v != "" {
		cfg.Tools.VoiceCall.DataDir = v
	}

	// Notes tool overrides.
	if v := os.Getenv("ALFREDAI_TOOLS_NOTES_ENABLED"); v == "true" {
		cfg.Tools.NotesEnabled = true
	}
	if v := os.Getenv("ALFREDAI_TOOLS_NOTES_DATA_DIR"); v != "" {
		cfg.Tools.NotesDataDir = v
	}

	// GitHub tool overrides.
	if v := os.Getenv("ALFREDAI_TOOLS_GITHUB_ENABLED"); v == "true" {
		cfg.Tools.GitHubEnabled = true
	}
	if v := os.Getenv("ALFREDAI_TOOLS_GITHUB_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Tools.GitHubTimeout = d
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_GITHUB_MAX_REQUESTS_PER_MINUTE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Tools.GitHubMaxRequestsPerMinute = n
		}
	}

	// Email tool overrides.
	if v := os.Getenv("ALFREDAI_TOOLS_EMAIL_ENABLED"); v == "true" {
		cfg.Tools.EmailEnabled = true
	}
	if v := os.Getenv("ALFREDAI_TOOLS_EMAIL_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Tools.EmailTimeout = d
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_EMAIL_MAX_SENDS_PER_HOUR"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Tools.EmailMaxSendsPerHour = n
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_EMAIL_ALLOWED_DOMAINS"); v != "" {
		cfg.Tools.EmailAllowedDomains = splitAndTrim(v, ",")
	}

	// Calendar tool overrides.
	if v := os.Getenv("ALFREDAI_TOOLS_CALENDAR_ENABLED"); v == "true" {
		cfg.Tools.CalendarEnabled = true
	}
	if v := os.Getenv("ALFREDAI_TOOLS_CALENDAR_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Tools.CalendarTimeout = d
		}
	}

	// Smart Home tool overrides.
	if v := os.Getenv("ALFREDAI_TOOLS_SMARTHOME_ENABLED"); v == "true" {
		cfg.Tools.SmartHomeEnabled = true
	}
	if v := os.Getenv("ALFREDAI_TOOLS_SMARTHOME_URL"); v != "" {
		cfg.Tools.SmartHomeURL = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_SMARTHOME_TOKEN"); v != "" {
		cfg.Tools.SmartHomeToken = v
	}
	if v := os.Getenv("ALFREDAI_TOOLS_SMARTHOME_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Tools.SmartHomeTimeout = d
		}
	}
	if v := os.Getenv("ALFREDAI_TOOLS_SMARTHOME_MAX_CALLS_PER_MINUTE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Tools.SmartHomeMaxCallsPerMinute = n
		}
	}

	// Edge IoT tool overrides.
	if v := os.Getenv("ALFREDAI_TOOLS_GPIO_ENABLED"); v == "true" {
		cfg.Tools.GPIOEnabled = true
	}
	if v := os.Getenv("ALFREDAI_TOOLS_SERIAL_ENABLED"); v == "true" {
		cfg.Tools.SerialEnabled = true
	}
	if v := os.Getenv("ALFREDAI_TOOLS_BLE_ENABLED"); v == "true" {
		cfg.Tools.BLEEnabled = true
	}

	if v := os.Getenv("ALFREDAI_MEMORY_PROVIDER"); v != "" {
		cfg.Memory.Provider = v
	}
	if v := os.Getenv("ALFREDAI_MEMORY_DATA_DIR"); v != "" {
		cfg.Memory.DataDir = v
	}
	if v := os.Getenv("ALFREDAI_MEMORY_AUTO_CURATE"); v == "true" {
		cfg.Memory.AutoCurate = true
	}
	if v := os.Getenv("ALFREDAI_EMBEDDING_PROVIDER"); v != "" {
		cfg.Memory.Embedding.Provider = v
	}
	if v := os.Getenv("ALFREDAI_EMBEDDING_MODEL"); v != "" {
		cfg.Memory.Embedding.Model = v
	}
	if v := os.Getenv("ALFREDAI_EMBEDDING_API_KEY"); v != "" {
		cfg.Memory.Embedding.APIKey = v
	}

	if v := os.Getenv("ALFREDAI_MEMORY_SEARCH_DECAY_HALF_LIFE"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			cfg.Memory.Search.DecayHalfLife = d
		}
	}
	if v := os.Getenv("ALFREDAI_MEMORY_SEARCH_MMR_DIVERSITY"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Memory.Search.MMRDiversity = f
		}
	}
	if v := os.Getenv("ALFREDAI_MEMORY_SEARCH_EMBEDDING_CACHE_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Memory.Search.EmbeddingCacheSize = n
		}
	}

	if v := os.Getenv("ALFREDAI_BYTEROVER_BASE_URL"); v != "" {
		cfg.Memory.ByteRover.BaseURL = v
	}
	if v := os.Getenv("ALFREDAI_BYTEROVER_API_KEY"); v != "" {
		cfg.Memory.ByteRover.APIKey = v
	}
	if v := os.Getenv("ALFREDAI_BYTEROVER_PROJECT_ID"); v != "" {
		cfg.Memory.ByteRover.ProjectID = v
	}

	if v := os.Getenv("ALFREDAI_SECURITY_ENCRYPTION_ENABLED"); v == "true" {
		cfg.Security.Encryption.Enabled = true
	}
	if v := os.Getenv("ALFREDAI_SECURITY_AUDIT_ENABLED"); v == "true" {
		cfg.Security.Audit.Enabled = true
	} else if v == "false" {
		cfg.Security.Audit.Enabled = false
	}
	if v := os.Getenv("ALFREDAI_SECURITY_AUDIT_PATH"); v != "" {
		cfg.Security.Audit.Path = v
	}
	if v := os.Getenv("ALFREDAI_SECURITY_CONSENT_DIR"); v != "" {
		cfg.Security.ConsentDir = v
	}

	// Per-provider API key overrides: ALFREDAI_LLM_PROVIDER_<NAME>_API_KEY
	for i := range cfg.LLM.Providers {
		envKey := fmt.Sprintf("ALFREDAI_LLM_PROVIDER_%s_API_KEY",
			strings.ToUpper(cfg.LLM.Providers[i].Name))
		if v := os.Getenv(envKey); v != "" {
			cfg.LLM.Providers[i].APIKey = v
		}
	}

	// Channel token overrides (env vars populate nested config structs).
	if v := os.Getenv("ALFREDAI_TELEGRAM_TOKEN"); v != "" {
		for i := range cfg.Channels {
			if cfg.Channels[i].Type == "telegram" {
				if cfg.Channels[i].Telegram == nil {
					cfg.Channels[i].Telegram = &TelegramChannelConfig{}
				}
				if cfg.Channels[i].Telegram.Token == "" {
					cfg.Channels[i].Telegram.Token = v
				}
			}
		}
	}

	// Node system overrides
	if v := os.Getenv("ALFREDAI_NODES_ENABLED"); v == "true" {
		cfg.Nodes.Enabled = true
	}
	if v := os.Getenv("ALFREDAI_NODES_HEARTBEAT_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Nodes.HeartbeatInterval = d
		}
	}
	if v := os.Getenv("ALFREDAI_NODES_INVOKE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Nodes.InvokeTimeout = d
		}
	}

	// Gateway overrides
	if v := os.Getenv("ALFREDAI_GATEWAY_ENABLED"); v == "true" {
		cfg.Gateway.Enabled = true
	}
	if v := os.Getenv("ALFREDAI_GATEWAY_ADDR"); v != "" {
		cfg.Gateway.Addr = v
	}

	if v := os.Getenv("ALFREDAI_DISCORD_TOKEN"); v != "" {
		for i := range cfg.Channels {
			if cfg.Channels[i].Type == "discord" {
				if cfg.Channels[i].Discord == nil {
					cfg.Channels[i].Discord = &DiscordChannelConfig{}
				}
				if cfg.Channels[i].Discord.Token == "" {
					cfg.Channels[i].Discord.Token = v
				}
			}
		}
	}

	if v := os.Getenv("ALFREDAI_SLACK_BOT_TOKEN"); v != "" {
		for i := range cfg.Channels {
			if cfg.Channels[i].Type == "slack" {
				if cfg.Channels[i].Slack == nil {
					cfg.Channels[i].Slack = &SlackChannelConfig{}
				}
				if cfg.Channels[i].Slack.BotToken == "" {
					cfg.Channels[i].Slack.BotToken = v
				}
			}
		}
	}
	if v := os.Getenv("ALFREDAI_SLACK_APP_TOKEN"); v != "" {
		for i := range cfg.Channels {
			if cfg.Channels[i].Type == "slack" {
				if cfg.Channels[i].Slack == nil {
					cfg.Channels[i].Slack = &SlackChannelConfig{}
				}
				if cfg.Channels[i].Slack.AppToken == "" {
					cfg.Channels[i].Slack.AppToken = v
				}
			}
		}
	}

	if v := os.Getenv("ALFREDAI_WHATSAPP_TOKEN"); v != "" {
		for i := range cfg.Channels {
			if cfg.Channels[i].Type == "whatsapp" {
				if cfg.Channels[i].WhatsApp == nil {
					cfg.Channels[i].WhatsApp = &WhatsAppChannelConfig{}
				}
				if cfg.Channels[i].WhatsApp.Token == "" {
					cfg.Channels[i].WhatsApp.Token = v
				}
			}
		}
	}

	if v := os.Getenv("ALFREDAI_MATRIX_ACCESS_TOKEN"); v != "" {
		for i := range cfg.Channels {
			if cfg.Channels[i].Type == "matrix" {
				if cfg.Channels[i].Matrix == nil {
					cfg.Channels[i].Matrix = &MatrixChannelConfig{}
				}
				if cfg.Channels[i].Matrix.AccessToken == "" {
					cfg.Channels[i].Matrix.AccessToken = v
				}
			}
		}
	}

	if v := os.Getenv("ALFREDAI_TEAMS_APP_ID"); v != "" {
		for i := range cfg.Channels {
			if cfg.Channels[i].Type == "teams" {
				if cfg.Channels[i].Teams == nil {
					cfg.Channels[i].Teams = &TeamsChannelConfig{}
				}
				if cfg.Channels[i].Teams.AppID == "" {
					cfg.Channels[i].Teams.AppID = v
				}
			}
		}
	}
	if v := os.Getenv("ALFREDAI_TEAMS_APP_SECRET"); v != "" {
		for i := range cfg.Channels {
			if cfg.Channels[i].Type == "teams" {
				if cfg.Channels[i].Teams == nil {
					cfg.Channels[i].Teams = &TeamsChannelConfig{}
				}
				if cfg.Channels[i].Teams.AppSecret == "" {
					cfg.Channels[i].Teams.AppSecret = v
				}
			}
		}
	}

	if v := os.Getenv("ALFREDAI_GOOGLECHAT_CREDENTIALS_FILE"); v != "" {
		for i := range cfg.Channels {
			if cfg.Channels[i].Type == "googlechat" {
				if cfg.Channels[i].GoogleChat == nil {
					cfg.Channels[i].GoogleChat = &GoogleChatChannelConfig{}
				}
				if cfg.Channels[i].GoogleChat.CredentialsFile == "" {
					cfg.Channels[i].GoogleChat.CredentialsFile = v
				}
			}
		}
	}
	if v := os.Getenv("ALFREDAI_GOOGLECHAT_SPACE_ID"); v != "" {
		for i := range cfg.Channels {
			if cfg.Channels[i].Type == "googlechat" {
				if cfg.Channels[i].GoogleChat == nil {
					cfg.Channels[i].GoogleChat = &GoogleChatChannelConfig{}
				}
				if cfg.Channels[i].GoogleChat.SpaceID == "" {
					cfg.Channels[i].GoogleChat.SpaceID = v
				}
			}
		}
	}

	if v := os.Getenv("ALFREDAI_SIGNAL_API_URL"); v != "" {
		for i := range cfg.Channels {
			if cfg.Channels[i].Type == "signal" {
				if cfg.Channels[i].Signal == nil {
					cfg.Channels[i].Signal = &SignalChannelConfig{}
				}
				if cfg.Channels[i].Signal.APIURL == "" {
					cfg.Channels[i].Signal.APIURL = v
				}
			}
		}
	}
	if v := os.Getenv("ALFREDAI_SIGNAL_PHONE"); v != "" {
		for i := range cfg.Channels {
			if cfg.Channels[i].Type == "signal" {
				if cfg.Channels[i].Signal == nil {
					cfg.Channels[i].Signal = &SignalChannelConfig{}
				}
				if cfg.Channels[i].Signal.Phone == "" {
					cfg.Channels[i].Signal.Phone = v
				}
			}
		}
	}

	if v := os.Getenv("ALFREDAI_IRC_SERVER"); v != "" {
		for i := range cfg.Channels {
			if cfg.Channels[i].Type == "irc" {
				if cfg.Channels[i].IRC == nil {
					cfg.Channels[i].IRC = &IRCChannelConfig{}
				}
				if cfg.Channels[i].IRC.Server == "" {
					cfg.Channels[i].IRC.Server = v
				}
			}
		}
	}
	if v := os.Getenv("ALFREDAI_IRC_NICK"); v != "" {
		for i := range cfg.Channels {
			if cfg.Channels[i].Type == "irc" {
				if cfg.Channels[i].IRC == nil {
					cfg.Channels[i].IRC = &IRCChannelConfig{}
				}
				if cfg.Channels[i].IRC.Nick == "" {
					cfg.Channels[i].IRC.Nick = v
				}
			}
		}
	}
	if v := os.Getenv("ALFREDAI_IRC_PASSWORD"); v != "" {
		for i := range cfg.Channels {
			if cfg.Channels[i].Type == "irc" {
				if cfg.Channels[i].IRC == nil {
					cfg.Channels[i].IRC = &IRCChannelConfig{}
				}
				if cfg.Channels[i].IRC.Password == "" {
					cfg.Channels[i].IRC.Password = v
				}
			}
		}
	}
}

// splitAndTrim splits s by sep and trims whitespace from each element.
func splitAndTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// decryptSecrets finds "enc:..." values in provider API keys and decrypts them.
func decryptSecrets(cfg *Config, passphrase string) error {
	for i := range cfg.LLM.Providers {
		key := cfg.LLM.Providers[i].APIKey
		if strings.HasPrefix(key, "enc:") {
			decrypted, err := DecryptValue(strings.TrimPrefix(key, "enc:"), passphrase)
			if err != nil {
				return fmt.Errorf("provider %s api_key: %w", cfg.LLM.Providers[i].Name, err)
			}
			cfg.LLM.Providers[i].APIKey = decrypted
		}
	}

	// Decrypt embedding API key.
	if strings.HasPrefix(cfg.Memory.Embedding.APIKey, "enc:") {
		decrypted, err := DecryptValue(strings.TrimPrefix(cfg.Memory.Embedding.APIKey, "enc:"), passphrase)
		if err != nil {
			return fmt.Errorf("embedding api_key: %w", err)
		}
		cfg.Memory.Embedding.APIKey = decrypted
	}

	// Decrypt channel tokens.
	for i := range cfg.Channels {
		var fields []*string
		ch := &cfg.Channels[i]
		if ch.Telegram != nil {
			fields = append(fields, &ch.Telegram.Token)
		}
		if ch.Discord != nil {
			fields = append(fields, &ch.Discord.Token)
		}
		if ch.Slack != nil {
			fields = append(fields, &ch.Slack.BotToken, &ch.Slack.AppToken)
		}
		if ch.WhatsApp != nil {
			fields = append(fields, &ch.WhatsApp.Token, &ch.WhatsApp.AppSecret)
		}
		if ch.Matrix != nil {
			fields = append(fields, &ch.Matrix.AccessToken)
		}
		if ch.Teams != nil {
			fields = append(fields, &ch.Teams.AppSecret)
		}
		for _, fp := range fields {
			if strings.HasPrefix(*fp, "enc:") {
				decrypted, err := DecryptValue(strings.TrimPrefix(*fp, "enc:"), passphrase)
				if err != nil {
					return fmt.Errorf("channel %s token: %w", ch.Type, err)
				}
				*fp = decrypted
			}
		}
	}

	// Decrypt voice call secrets.
	voiceCallSecrets := []*string{
		&cfg.Tools.VoiceCall.TwilioAuthToken,
		&cfg.Tools.VoiceCall.OpenAIAPIKey,
	}
	for _, fp := range voiceCallSecrets {
		if strings.HasPrefix(*fp, "enc:") {
			decrypted, err := DecryptValue(strings.TrimPrefix(*fp, "enc:"), passphrase)
			if err != nil {
				return fmt.Errorf("voice call secret: %w", err)
			}
			*fp = decrypted
		}
	}

	// Decrypt gateway auth tokens.
	for i := range cfg.Gateway.Auth.Tokens {
		tok := cfg.Gateway.Auth.Tokens[i].Token
		if strings.HasPrefix(tok, "enc:") {
			decrypted, err := DecryptValue(strings.TrimPrefix(tok, "enc:"), passphrase)
			if err != nil {
				return fmt.Errorf("gateway auth token %s: %w", cfg.Gateway.Auth.Tokens[i].Name, err)
			}
			cfg.Gateway.Auth.Tokens[i].Token = decrypted
		}
	}

	return nil
}

// EncryptValue encrypts a plaintext value with AES-256-GCM using a passphrase.
func EncryptValue(plaintext, passphrase string) (string, error) {
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	key := deriveKey(passphrase, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	// Format: hex(salt) + ":" + hex(nonce+ciphertext)
	return hex.EncodeToString(salt) + ":" + hex.EncodeToString(ciphertext), nil
}

// DecryptValue decrypts an AES-256-GCM encrypted value.
func DecryptValue(encrypted, passphrase string) (string, error) {
	parts := strings.SplitN(encrypted, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid encrypted format")
	}

	salt, err := hex.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("decode salt: %w", err)
	}

	data, err := hex.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}

	key := deriveKey(passphrase, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}

// deriveKey uses Argon2id to derive a 32-byte key from passphrase + salt.
func deriveKey(passphrase string, salt []byte) []byte {
	return argon2.IDKey([]byte(passphrase), salt, 1, 64*1024, 4, 32)
}

// validatePermissions checks the config file has restrictive permissions.
func validatePermissions(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat config: %w", err)
	}
	mode := info.Mode().Perm()
	// Allow 0600 and 0644 (readable by others but not writable)
	if mode&0o077 > 0o044 {
		return fmt.Errorf("config file %s has insecure permissions %o (want 0600 or 0644)", path, mode)
	}
	return nil
}
