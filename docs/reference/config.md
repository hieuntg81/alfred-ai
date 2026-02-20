# alfred-ai Configuration Reference

Configuration is loaded from a YAML file (default: `alfredai.yaml`). If the file does not exist, built-in defaults are used. Environment variable overrides are applied after file parsing.

**File permissions:** The config file must have permissions `0600` or `0644`. More permissive modes are rejected at load time.

**Config includes:** Use the top-level `includes` key to merge additional YAML files. Paths are relative to the main config file. The main file takes precedence over included files. Circular includes are detected and rejected.

```yaml
includes:
  - secrets.yaml
  - tools.yaml
```

---

## Table of Contents

- [agent](#agent)
- [llm](#llm)
- [memory](#memory)
- [tools](#tools)
- [security](#security)
- [skills](#skills)
- [scheduler](#scheduler)
- [channels](#channels)
- [plugins](#plugins)
- [gateway](#gateway)
- [agents (multi-agent)](#agents-multi-agent)
- [nodes](#nodes)
- [logger](#logger)
- [tracer](#tracer)
- [Environment Variable Overrides](#environment-variable-overrides)
- [Secret Encryption](#secret-encryption)

---

## agent

Controls core agent loop behavior, context compression, sub-agent spawning, tool approval gating, and context window overflow prevention.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max_iterations` | int | `10` | Maximum tool-call iterations per request. Must be > 0. |
| `timeout` | duration | `120s` | Maximum wall-clock time per request. Must be > 0. |
| `system_prompt` | string | `"You are alfred-ai, a helpful AI assistant."` | System prompt sent to the LLM. Must not be empty. |

### agent.compression

Summarizes older messages to reduce context size.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable context compression. |
| `threshold` | int | `30` | Message count that triggers compression. Must be > 0 when enabled. |
| `keep_recent` | int | `10` | Number of recent messages to keep verbatim. Must be > 0 when enabled. |

### agent.sub_agent

Allows the agent to spawn child agents for subtasks.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable sub-agent spawning. |
| `max_sub_agents` | int | `5` | Maximum concurrent sub-agents. |
| `max_iterations` | int | `5` | Maximum iterations per sub-agent. |
| `timeout` | duration | `60s` | Timeout per sub-agent request. |

### agent.tool_approval

Requires human confirmation before executing certain tools.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable tool approval gating. |
| `always_approve` | []string | `[]` | Tool names that never require approval. |
| `always_deny` | []string | `[]` | Tool names that are always rejected. |

### agent.context_guard

Proactively prevents context window overflow.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable context guard. |
| `max_tokens` | int | `128000` | Model context window size in tokens. |
| `reserve_tokens` | int | `1000` | Tokens reserved for the response. |
| `safety_margin` | float64 | `0.15` | Fraction of `max_tokens` kept as a safety buffer (0.0 - 1.0). |

```yaml
agent:
  max_iterations: 15
  timeout: 180s
  system_prompt: "You are a home automation assistant."
  compression:
    enabled: true
    threshold: 40
    keep_recent: 15
  sub_agent:
    enabled: true
    max_sub_agents: 3
    max_iterations: 10
    timeout: 90s
  tool_approval:
    enabled: true
    always_approve: [memory_search, web_search]
    always_deny: [shell_exec]
  context_guard:
    enabled: true
    max_tokens: 128000
    reserve_tokens: 2000
    safety_margin: 0.2
```

---

## llm

Configures LLM providers, model failover, and circuit breaking.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `default_provider` | string | `"openai"` | Name of the default provider. Must match a configured provider `name`. |

### llm.providers[]

Each entry defines a single LLM provider.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | *required* | Unique identifier for this provider. |
| `type` | string | `""` | Provider type: `openai`, `anthropic`, `gemini`, `openrouter`, `ollama`. |
| `base_url` | string | `""` | Custom API base URL (useful for proxies or self-hosted models). |
| `api_key` | string | *required* | API key. Prefer env var override (see below). Supports `enc:` prefix for encrypted values. |
| `model` | string | `""` | Model identifier (e.g. `gpt-4o`, `claude-sonnet-4-20250514`, `gemini-2.0-flash`). |
| `conn_timeout` | duration | `0` | HTTP connection timeout. |
| `resp_timeout` | duration | `0` | HTTP response timeout. |

#### llm.providers[].pool

HTTP connection pool settings per provider.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max_idle_conns` | int | `0` | Maximum idle connections across all hosts. |
| `max_idle_conns_per_host` | int | `0` | Maximum idle connections per host. |
| `max_conns_per_host` | int | `0` | Maximum total connections per host. |
| `idle_conn_timeout` | duration | `0` | How long idle connections stay in the pool. |

### llm.failover

Automatic provider failover on errors.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable failover. |
| `fallbacks` | []string | `[]` | Ordered list of provider names to try on failure. |

### llm.circuit_breaker

Prevents repeated calls to a failing provider.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable circuit breaker. |
| `max_failures` | uint32 | `0` | Number of consecutive failures before the circuit opens. |
| `timeout` | duration | `0` | How long the circuit stays open before attempting a probe. |
| `interval` | duration | `0` | Interval for resetting the failure counter in half-open state. |

```yaml
llm:
  default_provider: openai
  providers:
    - name: openai
      type: openai
      api_key: ${OPENAI_API_KEY}
      model: gpt-4o
      conn_timeout: 10s
      resp_timeout: 120s
      pool:
        max_idle_conns: 10
        max_conns_per_host: 20
        idle_conn_timeout: 90s
    - name: anthropic
      type: anthropic
      api_key: ${ANTHROPIC_API_KEY}
      model: claude-sonnet-4-20250514
    - name: local
      type: ollama
      base_url: http://localhost:11434
      api_key: unused
      model: llama3
  failover:
    enabled: true
    fallbacks: [anthropic, local]
  circuit_breaker:
    enabled: true
    max_failures: 5
    timeout: 60s
    interval: 30s
```

---

## memory

Configures the memory subsystem, embedding provider, and vector search tuning.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `provider` | string | `"noop"` | Memory backend: `noop`, `markdown`, `vector`, `byterover`. |
| `data_dir` | string | `~/.alfredai/data/memory` | Directory for local memory storage. |
| `auto_curate` | bool | `false` | Automatically curate and organize memory entries. |

### memory.embedding

Text embedding provider used by the `vector` memory backend.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `provider` | string | `""` | Embedding provider: `openai`, `gemini`, or empty. |
| `model` | string | `""` | Embedding model name. |
| `api_key` | string | `""` | API key for the embedding provider. Supports `enc:` prefix. |

### memory.search

Vector search tuning parameters. All default to zero (disabled) for backward compatibility.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `decay_half_life` | duration | `0` | Time-decay half-life for recency scoring. 0 = disabled. Must be >= 0. |
| `mmr_diversity` | float64 | `0` | Maximal Marginal Relevance diversity factor (0.0 - 1.0). 0 = disabled. |
| `embedding_cache_size` | int | `0` | LRU cache size for embedding vectors. 0 = disabled. Must be >= 0. |
| `max_vector_candidates` | int | `0` | Maximum candidate vectors considered during search. 0 = default (10000). |

### memory.byterover

Required when `provider` is `byterover`.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `base_url` | string | *required* | ByteRover API base URL. |
| `api_key` | string | *required* | ByteRover API key. |
| `project_id` | string | `""` | ByteRover project identifier. |

```yaml
memory:
  provider: vector
  data_dir: /var/lib/alfredai/memory
  auto_curate: true
  embedding:
    provider: openai
    model: text-embedding-3-small
  search:
    decay_half_life: 168h
    mmr_diversity: 0.3
    embedding_cache_size: 1000
    max_vector_candidates: 5000
```

---

## tools

Controls the built-in tool subsystem. Most tools are disabled by default and must be explicitly enabled.

### Core Tools

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `sandbox_root` | string | `"."` | Root directory for filesystem and shell operations. Must not be empty. |
| `allowed_commands` | []string | `[ls, cat, grep, find, git, go, python]` | Shell commands the agent is allowed to execute. |
| `filesystem_backend` | string | `"local"` | Filesystem backend. Valid: `local`. |
| `shell_backend` | string | `"local"` | Shell backend. Valid: `local`. |
| `shell_timeout` | duration | `30s` | Timeout for shell command execution. Must be > 0. |

### Web Search

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `search_backend` | string | `"searxng"` | Web search backend. Valid: `searxng`. |
| `searxng_url` | string | `"http://localhost:6060"` | SearXNG instance URL. Required when `search_backend` is `searxng`. |
| `search_cache_ttl` | duration | `15m` | TTL for cached search results. |

### Browser

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `browser_enabled` | bool | `false` | Enable browser tool. |
| `browser_backend` | string | `"chromedp"` | Browser backend. Valid: `chromedp`. |
| `browser_cdp_url` | string | `""` | Chrome DevTools Protocol URL for remote browser. |
| `browser_headless` | bool | `true` | Run browser in headless mode. |
| `browser_timeout` | duration | `30s` | Timeout per browser operation. Must be > 0 when enabled. |

### Canvas

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `canvas_enabled` | bool | `false` | Enable canvas tool. |
| `canvas_backend` | string | `"local"` | Canvas backend. Valid: `local`. |
| `canvas_root` | string | `~/.alfredai/data/canvas` | Canvas file storage directory. Must not be empty when enabled. |
| `canvas_max_size` | int | `524288` | Maximum canvas file size in bytes (512 KiB). Must be > 0 when enabled. |

### Cron

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `cron_enabled` | bool | `false` | Enable cron job management tool. |
| `cron_data_dir` | string | `~/.alfredai/data/cron` | Cron data storage directory. |

### Process

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `process_enabled` | bool | `false` | Enable interactive process sessions. |
| `process_max_sessions` | int | `10` | Maximum concurrent process sessions. Must be > 0 when enabled. |
| `process_session_ttl` | duration | `30m` | Idle timeout before a session is reaped. Must be > 0 when enabled. |
| `process_output_max` | int | `1048576` | Maximum output buffer per session in bytes (1 MiB). Must be > 0 when enabled. |

### Message

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `message_enabled` | bool | `false` | Enable message sending tool (send, reply, broadcast across channels). |

### Workflow

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `workflow_enabled` | bool | `false` | Enable workflow execution tool. |
| `workflow_dir` | string | `"./workflows"` | Directory containing workflow definitions. Must not be empty when enabled. |
| `workflow_data_dir` | string | `~/.alfredai/data/workflows` | Workflow run data directory. |
| `workflow_timeout` | duration | `120s` | Maximum execution time per workflow run. Must be > 0 when enabled. |
| `workflow_max_output` | int | `1048576` | Maximum output per workflow run in bytes (1 MiB). Must be > 0 when enabled. |
| `workflow_max_running` | int | `5` | Maximum concurrent workflow runs. Must be > 0 when enabled. |
| `workflow_allowed_commands` | []string | `[]` | Additional shell commands permitted inside workflows. |

### LLM Task

Delegates subtasks to an LLM without consuming the main agent's context.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `llm_task_enabled` | bool | `false` | Enable LLM task delegation tool. |
| `llm_task_timeout` | duration | `30s` | Timeout per LLM task call. Must be > 0 when enabled. |
| `llm_task_max_tokens` | int | `4096` | Maximum output tokens per task. Must be > 0 when enabled. |
| `llm_task_max_prompt_size` | int | `32768` | Maximum prompt size in bytes (32 KiB). Must be > 0 when enabled. |
| `llm_task_max_input_size` | int | `262144` | Maximum input data size in bytes (256 KiB). Must be > 0 when enabled. |
| `llm_task_allowed_models` | []string | `[]` | Restrict task delegation to these model names. Empty = all configured models. |
| `llm_task_default_model` | string | `""` | Default model for LLM tasks when none is specified. |

### Camera

Requires `nodes.enabled: true`.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `camera_enabled` | bool | `false` | Enable camera tool. |
| `camera_max_payload_size` | int | `5242880` | Maximum payload size in bytes (5 MiB). Must be > 0 when enabled. |
| `camera_max_clip_duration` | duration | `60s` | Maximum video clip duration. Must be > 0 when enabled. |
| `camera_timeout` | duration | `30s` | Timeout per camera operation. Must be > 0 when enabled. |

### Location

Requires `nodes.enabled: true`.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `location_enabled` | bool | `false` | Enable location tool. |
| `location_timeout` | duration | `10s` | Timeout per location request. Must be > 0 when enabled. |
| `location_default_accuracy` | string | `"balanced"` | Default accuracy level: `coarse`, `balanced`, `precise`. |

### Notes

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `notes_enabled` | bool | `false` | Enable notes tool. |
| `notes_data_dir` | string | `~/.alfredai/data/notes` | Notes storage directory. Must not be empty when enabled. |

### GitHub

Requires `GITHUB_TOKEN` environment variable to be set for authentication.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `github_enabled` | bool | `false` | Enable GitHub tool. |
| `github_timeout` | duration | `15s` | Timeout per GitHub API call. Must be > 0 when enabled. |
| `github_max_requests_per_minute` | int | `30` | Rate limit for GitHub API requests. Must be > 0 when enabled. |

### Email

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `email_enabled` | bool | `false` | Enable email tool. |
| `email_timeout` | duration | `30s` | Timeout per email operation. Must be > 0 when enabled. |
| `email_max_sends_per_hour` | int | `10` | Hourly send rate limit. Must be > 0 when enabled. |
| `email_allowed_domains` | []string | `[]` | Restrict sending to these email domains. Empty = unrestricted. |

### Calendar

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `calendar_enabled` | bool | `false` | Enable calendar tool. |
| `calendar_timeout` | duration | `15s` | Timeout per calendar operation. Must be > 0 when enabled. |

### Smart Home

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `smarthome_enabled` | bool | `false` | Enable smart home tool. |
| `smarthome_url` | string | `""` | Home Assistant (or compatible) API URL. Required when enabled. |
| `smarthome_token` | string | `""` | Long-lived access token. |
| `smarthome_timeout` | duration | `10s` | Timeout per smart home API call. Must be > 0 when enabled. |
| `smarthome_max_calls_per_minute` | int | `60` | Rate limit for smart home API calls. Must be > 0 when enabled. |

### Voice Call

Twilio-based voice calling with OpenAI TTS/STT.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `voice_call.enabled` | bool | `false` | Enable voice call tool. |
| `voice_call.provider` | string | `"twilio"` | Voice provider: `twilio`, `mock`. |
| `voice_call.from_number` | string | *required* | Caller ID in E.164 format (e.g. `+15551234567`). Required when enabled. |
| `voice_call.default_to` | string | `""` | Default destination number (E.164). Optional. |
| `voice_call.default_mode` | string | `"notify"` | Default call mode: `notify`, `conversation`. |
| `voice_call.max_concurrent` | int | `1` | Maximum concurrent calls. Must be > 0. |
| `voice_call.max_duration` | duration | `5m` | Maximum call duration. Must be > 0. |
| `voice_call.transcript_timeout` | duration | `3m` | Timeout waiting for transcript completion. |
| `voice_call.timeout` | duration | `30s` | Timeout for call initiation. Must be > 0. |
| `voice_call.allowed_numbers` | []string | `[]` | E.164 phone number allowlist. Empty = unrestricted. |
| `voice_call.data_dir` | string | `~/.alfredai/data/voice-calls` | Call record storage. |

#### Twilio Credentials

Required when `voice_call.provider` is `twilio`.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `voice_call.twilio_account_sid` | string | *required* | Twilio Account SID. |
| `voice_call.twilio_auth_token` | string | *required* | Twilio Auth Token. Supports `enc:` prefix. |
| `voice_call.webhook_addr` | string | `":3334"` | Local address for the webhook server. |
| `voice_call.webhook_path` | string | `"/voice/webhook"` | Webhook endpoint path. |
| `voice_call.webhook_public_url` | string | *required* | Public URL for Twilio callbacks (e.g. ngrok URL). Required for Twilio. |
| `voice_call.webhook_skip_verify` | bool | `false` | Skip Twilio request signature verification (development only). |
| `voice_call.stream_path` | string | `"/voice/stream"` | WebSocket media stream path. |

#### TTS/STT

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `voice_call.openai_api_key` | string | `""` | OpenAI API key for TTS/STT. Falls back to the LLM provider registry. Supports `enc:` prefix. |
| `voice_call.tts_voice` | string | `"alloy"` | OpenAI TTS voice (e.g. `alloy`, `nova`, `shimmer`, `echo`, `fable`, `onyx`). |
| `voice_call.tts_model` | string | `"tts-1"` | TTS model: `tts-1`, `tts-1-hd`. |
| `voice_call.stt_model` | string | `"gpt-4o-transcribe"` | Speech-to-text model. |
| `voice_call.silence_duration_ms` | int | `800` | Voice activity detection silence threshold in milliseconds. |

```yaml
tools:
  sandbox_root: /home/alfred/workspace
  allowed_commands: [ls, cat, grep, find, git, go, python, node, npm]
  shell_timeout: 60s
  browser_enabled: true
  browser_headless: true
  browser_timeout: 45s
  workflow_enabled: true
  workflow_dir: ./workflows
  workflow_max_running: 3
  smarthome_enabled: true
  smarthome_url: http://homeassistant.local:8123
  smarthome_token: ${HASS_TOKEN}
  voice_call:
    enabled: true
    provider: twilio
    from_number: "+15551234567"
    twilio_account_sid: ${TWILIO_SID}
    twilio_auth_token: ${TWILIO_TOKEN}
    webhook_public_url: https://example.ngrok.io
```

---

## security

Controls content encryption, audit logging, and consent tracking.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `consent_dir` | string | `~/.alfredai/data` | Directory for consent records. |

### security.encryption

AES-256-GCM encryption for memory content at rest.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `encryption.enabled` | bool | `false` | Enable content encryption. |

The encryption key is provided via the `ALFREDAI_ENCRYPTION_KEY` environment variable. It is never stored in the config file.

### security.audit

Append-only audit log of agent actions.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `audit.enabled` | bool | `true` | Enable audit logging. |
| `audit.path` | string | `~/.alfredai/data/audit.jsonl` | Path to the audit log file (JSONL format). Required when enabled. |

```yaml
security:
  encryption:
    enabled: true
  audit:
    enabled: true
    path: /var/log/alfredai/audit.jsonl
  consent_dir: /var/lib/alfredai/consent
```

---

## skills

External skill definitions loaded from YAML files.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable the skill system. |
| `dir` | string | `"./skills"` | Directory containing skill YAML files. |

```yaml
skills:
  enabled: true
  dir: ./skills
```

---

## scheduler

Runs tasks on a schedule (cron expressions or duration intervals).

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable the scheduler. |

### scheduler.tasks[]

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | *required* | Unique task name. |
| `schedule` | string | *required* | Cron expression (e.g. `0 9 * * *`) or duration (e.g. `1h`). |
| `action` | string | *required* | Action to execute. |
| `agent_id` | string | `""` | Target agent ID (multi-agent mode). |
| `channel` | string | `""` | Target channel for output. |
| `message` | string | `""` | Message to send or prompt to evaluate. |
| `one_shot` | bool | `false` | Run once and then disable. |

```yaml
scheduler:
  enabled: true
  tasks:
    - name: daily-summary
      schedule: "0 9 * * *"
      action: prompt
      message: "Generate a summary of yesterday's activity."
      channel: telegram
    - name: hourly-check
      schedule: 1h
      action: prompt
      message: "Check system health."
      one_shot: false
```

---

## channels

A list of communication channel configurations. Each entry defines one channel.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `type` | string | *required* | Channel type: `cli`, `http`, `telegram`, `discord`, `slack`, `whatsapp`, `matrix`, `webchat`. |
| `mention_only` | bool | `false` | Only respond when the bot is mentioned (Discord, Slack). |
| `channel_ids` | []string | `[]` | Restrict to specific channel IDs. |

### Channel-specific fields

#### http

| Field | Type | Description |
|-------|------|-------------|
| `http_addr` | string | Listen address (e.g. `:8080`). Required. |

#### telegram

| Field | Type | Description |
|-------|------|-------------|
| `telegram_token` | string | Bot token from BotFather. Required. Env: `ALFREDAI_TELEGRAM_TOKEN`. |

#### discord

| Field | Type | Description |
|-------|------|-------------|
| `discord_token` | string | Bot token. Required. Env: `ALFREDAI_DISCORD_TOKEN`. |
| `guild_id` | string | Restrict to a specific Discord guild. |

#### slack

| Field | Type | Description |
|-------|------|-------------|
| `slack_bot_token` | string | Bot user OAuth token (`xoxb-`). Required. Env: `ALFREDAI_SLACK_BOT_TOKEN`. |
| `slack_app_token` | string | App-level token (`xapp-`). Required. Env: `ALFREDAI_SLACK_APP_TOKEN`. |

#### whatsapp

| Field | Type | Description |
|-------|------|-------------|
| `whatsapp_token` | string | Meta Graph API access token. Required. Env: `ALFREDAI_WHATSAPP_TOKEN`. |
| `whatsapp_phone_id` | string | WhatsApp Business phone number ID. Required. |
| `whatsapp_verify_token` | string | Webhook verification token. Required. |
| `whatsapp_app_secret` | string | App secret for request signature verification. |
| `whatsapp_webhook_addr` | string | Local address for the webhook server. |

#### matrix

| Field | Type | Description |
|-------|------|-------------|
| `matrix_homeserver` | string | Homeserver URL (e.g. `https://matrix.org`). Required. |
| `matrix_access_token` | string | Bot access token. Required. Env: `ALFREDAI_MATRIX_ACCESS_TOKEN`. |
| `matrix_user_id` | string | Bot user ID (e.g. `@alfred:matrix.org`). Required. |

```yaml
channels:
  - type: telegram
    telegram_token: ${TELEGRAM_TOKEN}
  - type: discord
    discord_token: ${DISCORD_TOKEN}
    mention_only: true
    channel_ids: ["123456789"]
  - type: slack
    slack_bot_token: ${SLACK_BOT_TOKEN}
    slack_app_token: ${SLACK_APP_TOKEN}
  - type: http
    http_addr: ":8080"
  - type: whatsapp
    whatsapp_token: ${WHATSAPP_TOKEN}
    whatsapp_phone_id: "110123456789"
    whatsapp_verify_token: "my-verify-token"
    whatsapp_webhook_addr: ":3333"
  - type: matrix
    matrix_homeserver: https://matrix.org
    matrix_access_token: ${MATRIX_TOKEN}
    matrix_user_id: "@alfred:matrix.org"
```

---

## plugins

External plugin system with optional WASM sandbox support.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable the plugin system. |
| `dirs` | []string | `["./plugins"]` | Directories to scan for plugins. Must have at least one entry when enabled. |
| `allow_permissions` | []string | `[]` | Permissions to grant to plugins. |
| `deny_permissions` | []string | `[]` | Permissions to deny to plugins. |
| `wasm_enabled` | bool | `false` | Enable WASM plugin sandbox. |
| `wasm_max_memory_mb` | int | `64` | Maximum memory per WASM plugin in MiB (1 - 512). |
| `wasm_exec_timeout` | string | `"30s"` | Execution timeout per WASM plugin call (1s - 5m). |

```yaml
plugins:
  enabled: true
  dirs: ["./plugins", "/opt/alfredai/plugins"]
  allow_permissions: [network, filesystem]
  wasm_enabled: true
  wasm_max_memory_mb: 128
  wasm_exec_timeout: "60s"
```

---

## gateway

WebSocket gateway for real-time communication with external clients.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable WebSocket gateway. |
| `addr` | string | `":8090"` | Listen address (host:port). Must be a valid host:port when enabled. |

### gateway.auth

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `auth.type` | string | `""` | Auth type: `"static"` or `""` (none). |

### gateway.auth.tokens[]

Required when `auth.type` is `"static"`.

| Field | Type | Description |
|-------|------|-------------|
| `token` | string | Bearer token value. Supports `enc:` prefix. |
| `name` | string | Human-readable name for the token. |
| `roles` | []string | Roles associated with this token. |

```yaml
gateway:
  enabled: true
  addr: ":8090"
  auth:
    type: static
    tokens:
      - token: ${GATEWAY_TOKEN}
        name: frontend
        roles: [admin]
```

---

## agents (multi-agent)

Configures multiple agent instances with independent system prompts, models, and tool sets. When this section is present (non-null), the system operates in multi-agent mode.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `default` | string | *required* | ID of the default agent instance. Must match an `instances[].id`. |
| `routing` | string | `"default"` | Routing strategy: `default`, `prefix`, `config`. |
| `data_dir` | string | `"./data"` | Shared workspace root for all agents. |

### agents.routing_rules[]

Used when `routing` is `"config"`.

| Field | Type | Description |
|-------|------|-------------|
| `channel` | string | Channel type or name. |
| `group_id` | string | Group or room identifier. |
| `agent_id` | string | Agent instance to route to. |

### agents.instances[]

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `id` | string | *required* | Unique agent identifier. |
| `name` | string | `""` | Display name. |
| `description` | string | `""` | Human-readable description of the agent's purpose. |
| `system_prompt` | string | `""` | System prompt override for this agent. |
| `model` | string | `""` | Model override. |
| `provider` | string | `""` | Provider override. |
| `tools` | []string | `[]` | Restrict available tools to this list. Empty = all tools. |
| `skills` | []string | `[]` | Restrict available skills to this list. Empty = all skills. |
| `max_iter` | int | `0` | Max iterations override. 0 = use global default. |
| `metadata` | map | `{}` | Arbitrary key-value metadata. |

```yaml
agents:
  default: general
  routing: config
  data_dir: ./data
  routing_rules:
    - channel: telegram
      group_id: "-100123456"
      agent_id: home
  instances:
    - id: general
      name: General Assistant
      system_prompt: "You are a general-purpose assistant."
      model: gpt-4o
      provider: openai
    - id: home
      name: Home Agent
      system_prompt: "You manage the smart home."
      model: gpt-4o-mini
      provider: openai
      tools: [smarthome, memory_search]
      max_iter: 8
```

---

## nodes

Remote node system for distributed tool execution (camera, location, etc.).

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable remote nodes. |
| `heartbeat_interval` | duration | `30s` | Interval between heartbeat checks. Must be > 0 when enabled. |
| `invoke_timeout` | duration | `30s` | Timeout for remote tool invocations. Must be > 0 when enabled. |
| `allowed_nodes` | []string | `[]` | Restrict to specific node IDs. Empty = accept all. |

### nodes.discovery

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `discovery.mdns` | bool | `false` | Enable mDNS node discovery. Requires the `mdns` build tag. |
| `discovery.scan_interval` | duration | `60s` | How often to scan for new nodes. |

```yaml
nodes:
  enabled: true
  heartbeat_interval: 15s
  invoke_timeout: 45s
  allowed_nodes: ["node-livingroom", "node-garage"]
  discovery:
    mdns: true
    scan_interval: 30s
```

---

## logger

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `level` | string | `"info"` | Log level: `debug`, `info`, `warn`, `error`. |
| `format` | string | `"text"` | Log format: `text`, `json`. |
| `output` | string | `"stderr"` | Log output destination: `stderr`, `stdout`, or a file path. |

```yaml
logger:
  level: debug
  format: json
  output: /var/log/alfredai/agent.log
```

---

## tracer

OpenTelemetry-compatible distributed tracing.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable tracing. |
| `exporter` | string | `"noop"` | Trace exporter backend (e.g. `otlp`, `jaeger`, `noop`). |
| `endpoint` | string | `""` | Exporter endpoint URL. |

```yaml
tracer:
  enabled: true
  exporter: otlp
  endpoint: http://localhost:4318
```

---

## Environment Variable Overrides

All environment variables follow the pattern `ALFREDAI_SECTION_FIELD`. They are applied after the YAML file is parsed and take precedence over file values.

### General

| Environment Variable | Config Path | Type |
|---------------------|-------------|------|
| `ALFREDAI_LLM_DEFAULT_PROVIDER` | `llm.default_provider` | string |
| `ALFREDAI_LOGGER_LEVEL` | `logger.level` | string |
| `ALFREDAI_TRACER_ENABLED` | `tracer.enabled` | bool (`"true"`) |
| `ALFREDAI_TRACER_EXPORTER` | `tracer.exporter` | string |

### Tools

| Environment Variable | Config Path | Type |
|---------------------|-------------|------|
| `ALFREDAI_TOOLS_SANDBOX_ROOT` | `tools.sandbox_root` | string |
| `ALFREDAI_TOOLS_SEARCH_BACKEND` | `tools.search_backend` | string |
| `ALFREDAI_TOOLS_SEARXNG_URL` | `tools.searxng_url` | string |
| `ALFREDAI_TOOLS_FILESYSTEM_BACKEND` | `tools.filesystem_backend` | string |
| `ALFREDAI_TOOLS_SHELL_BACKEND` | `tools.shell_backend` | string |
| `ALFREDAI_TOOLS_SHELL_TIMEOUT` | `tools.shell_timeout` | duration |
| `ALFREDAI_TOOLS_BROWSER_ENABLED` | `tools.browser_enabled` | bool (`"true"`) |
| `ALFREDAI_TOOLS_BROWSER_BACKEND` | `tools.browser_backend` | string |
| `ALFREDAI_TOOLS_BROWSER_CDP_URL` | `tools.browser_cdp_url` | string |
| `ALFREDAI_TOOLS_BROWSER_HEADLESS` | `tools.browser_headless` | bool (`"false"` to disable) |
| `ALFREDAI_TOOLS_BROWSER_TIMEOUT` | `tools.browser_timeout` | duration |
| `ALFREDAI_TOOLS_CANVAS_ENABLED` | `tools.canvas_enabled` | bool (`"true"`) |
| `ALFREDAI_TOOLS_CANVAS_BACKEND` | `tools.canvas_backend` | string |
| `ALFREDAI_TOOLS_CANVAS_ROOT` | `tools.canvas_root` | string |
| `ALFREDAI_TOOLS_CRON_ENABLED` | `tools.cron_enabled` | bool (`"true"`) |
| `ALFREDAI_TOOLS_CRON_DATA_DIR` | `tools.cron_data_dir` | string |
| `ALFREDAI_TOOLS_PROCESS_ENABLED` | `tools.process_enabled` | bool (`"true"`) |
| `ALFREDAI_TOOLS_PROCESS_MAX_SESSIONS` | `tools.process_max_sessions` | int |
| `ALFREDAI_TOOLS_PROCESS_SESSION_TTL` | `tools.process_session_ttl` | duration |
| `ALFREDAI_TOOLS_PROCESS_OUTPUT_MAX` | `tools.process_output_max` | int |
| `ALFREDAI_TOOLS_MESSAGE_ENABLED` | `tools.message_enabled` | bool (`"true"`) |
| `ALFREDAI_TOOLS_WORKFLOW_ENABLED` | `tools.workflow_enabled` | bool (`"true"`) |
| `ALFREDAI_TOOLS_WORKFLOW_DIR` | `tools.workflow_dir` | string |
| `ALFREDAI_TOOLS_WORKFLOW_DATA_DIR` | `tools.workflow_data_dir` | string |
| `ALFREDAI_TOOLS_WORKFLOW_TIMEOUT` | `tools.workflow_timeout` | duration |
| `ALFREDAI_TOOLS_WORKFLOW_MAX_OUTPUT` | `tools.workflow_max_output` | int |
| `ALFREDAI_TOOLS_WORKFLOW_MAX_RUNNING` | `tools.workflow_max_running` | int |
| `ALFREDAI_TOOLS_WORKFLOW_ALLOWED_COMMANDS` | `tools.workflow_allowed_commands` | comma-separated string |
| `ALFREDAI_TOOLS_LLM_TASK_ENABLED` | `tools.llm_task_enabled` | bool (`"true"`) |
| `ALFREDAI_TOOLS_LLM_TASK_TIMEOUT` | `tools.llm_task_timeout` | duration |
| `ALFREDAI_TOOLS_LLM_TASK_MAX_TOKENS` | `tools.llm_task_max_tokens` | int |
| `ALFREDAI_TOOLS_LLM_TASK_ALLOWED_MODELS` | `tools.llm_task_allowed_models` | comma-separated string |
| `ALFREDAI_TOOLS_LLM_TASK_MAX_PROMPT_SIZE` | `tools.llm_task_max_prompt_size` | int |
| `ALFREDAI_TOOLS_LLM_TASK_MAX_INPUT_SIZE` | `tools.llm_task_max_input_size` | int |
| `ALFREDAI_TOOLS_LLM_TASK_DEFAULT_MODEL` | `tools.llm_task_default_model` | string |
| `ALFREDAI_TOOLS_CAMERA_ENABLED` | `tools.camera_enabled` | bool (`"true"`) |
| `ALFREDAI_TOOLS_CAMERA_MAX_PAYLOAD_SIZE` | `tools.camera_max_payload_size` | int |
| `ALFREDAI_TOOLS_CAMERA_MAX_CLIP_DURATION` | `tools.camera_max_clip_duration` | duration |
| `ALFREDAI_TOOLS_CAMERA_TIMEOUT` | `tools.camera_timeout` | duration |
| `ALFREDAI_TOOLS_LOCATION_ENABLED` | `tools.location_enabled` | bool (`"true"`) |
| `ALFREDAI_TOOLS_LOCATION_TIMEOUT` | `tools.location_timeout` | duration |
| `ALFREDAI_TOOLS_LOCATION_DEFAULT_ACCURACY` | `tools.location_default_accuracy` | string |
| `ALFREDAI_TOOLS_NOTES_ENABLED` | `tools.notes_enabled` | bool (`"true"`) |
| `ALFREDAI_TOOLS_NOTES_DATA_DIR` | `tools.notes_data_dir` | string |
| `ALFREDAI_TOOLS_GITHUB_ENABLED` | `tools.github_enabled` | bool (`"true"`) |
| `ALFREDAI_TOOLS_GITHUB_TIMEOUT` | `tools.github_timeout` | duration |
| `ALFREDAI_TOOLS_GITHUB_MAX_REQUESTS_PER_MINUTE` | `tools.github_max_requests_per_minute` | int |
| `ALFREDAI_TOOLS_EMAIL_ENABLED` | `tools.email_enabled` | bool (`"true"`) |
| `ALFREDAI_TOOLS_EMAIL_TIMEOUT` | `tools.email_timeout` | duration |
| `ALFREDAI_TOOLS_EMAIL_MAX_SENDS_PER_HOUR` | `tools.email_max_sends_per_hour` | int |
| `ALFREDAI_TOOLS_EMAIL_ALLOWED_DOMAINS` | `tools.email_allowed_domains` | comma-separated string |
| `ALFREDAI_TOOLS_CALENDAR_ENABLED` | `tools.calendar_enabled` | bool (`"true"`) |
| `ALFREDAI_TOOLS_CALENDAR_TIMEOUT` | `tools.calendar_timeout` | duration |
| `ALFREDAI_TOOLS_SMARTHOME_ENABLED` | `tools.smarthome_enabled` | bool (`"true"`) |
| `ALFREDAI_TOOLS_SMARTHOME_URL` | `tools.smarthome_url` | string |
| `ALFREDAI_TOOLS_SMARTHOME_TOKEN` | `tools.smarthome_token` | string |
| `ALFREDAI_TOOLS_SMARTHOME_TIMEOUT` | `tools.smarthome_timeout` | duration |
| `ALFREDAI_TOOLS_SMARTHOME_MAX_CALLS_PER_MINUTE` | `tools.smarthome_max_calls_per_minute` | int |

### Voice Call

| Environment Variable | Config Path | Type |
|---------------------|-------------|------|
| `ALFREDAI_TOOLS_VOICE_CALL_ENABLED` | `tools.voice_call.enabled` | bool (`"true"`) |
| `ALFREDAI_TOOLS_VOICE_CALL_PROVIDER` | `tools.voice_call.provider` | string |
| `ALFREDAI_TOOLS_VOICE_CALL_FROM_NUMBER` | `tools.voice_call.from_number` | string |
| `ALFREDAI_TOOLS_VOICE_CALL_DEFAULT_TO` | `tools.voice_call.default_to` | string |
| `ALFREDAI_TOOLS_VOICE_CALL_DEFAULT_MODE` | `tools.voice_call.default_mode` | string |
| `ALFREDAI_TOOLS_VOICE_CALL_MAX_CONCURRENT` | `tools.voice_call.max_concurrent` | int |
| `ALFREDAI_TOOLS_VOICE_CALL_MAX_DURATION` | `tools.voice_call.max_duration` | duration |
| `ALFREDAI_TOOLS_VOICE_CALL_TIMEOUT` | `tools.voice_call.timeout` | duration |
| `ALFREDAI_TOOLS_VOICE_CALL_TWILIO_ACCOUNT_SID` | `tools.voice_call.twilio_account_sid` | string |
| `ALFREDAI_TOOLS_VOICE_CALL_TWILIO_AUTH_TOKEN` | `tools.voice_call.twilio_auth_token` | string |
| `ALFREDAI_TOOLS_VOICE_CALL_WEBHOOK_ADDR` | `tools.voice_call.webhook_addr` | string |
| `ALFREDAI_TOOLS_VOICE_CALL_WEBHOOK_PUBLIC_URL` | `tools.voice_call.webhook_public_url` | string |
| `ALFREDAI_TOOLS_VOICE_CALL_WEBHOOK_SKIP_VERIFY` | `tools.voice_call.webhook_skip_verify` | bool (`"true"`) |
| `ALFREDAI_TOOLS_VOICE_CALL_OPENAI_API_KEY` | `tools.voice_call.openai_api_key` | string |
| `ALFREDAI_TOOLS_VOICE_CALL_TTS_VOICE` | `tools.voice_call.tts_voice` | string |
| `ALFREDAI_TOOLS_VOICE_CALL_TTS_MODEL` | `tools.voice_call.tts_model` | string |
| `ALFREDAI_TOOLS_VOICE_CALL_STT_MODEL` | `tools.voice_call.stt_model` | string |
| `ALFREDAI_TOOLS_VOICE_CALL_DATA_DIR` | `tools.voice_call.data_dir` | string |

### Memory

| Environment Variable | Config Path | Type |
|---------------------|-------------|------|
| `ALFREDAI_MEMORY_PROVIDER` | `memory.provider` | string |
| `ALFREDAI_MEMORY_DATA_DIR` | `memory.data_dir` | string |
| `ALFREDAI_MEMORY_AUTO_CURATE` | `memory.auto_curate` | bool (`"true"`) |
| `ALFREDAI_EMBEDDING_PROVIDER` | `memory.embedding.provider` | string |
| `ALFREDAI_EMBEDDING_MODEL` | `memory.embedding.model` | string |
| `ALFREDAI_EMBEDDING_API_KEY` | `memory.embedding.api_key` | string |
| `ALFREDAI_MEMORY_SEARCH_DECAY_HALF_LIFE` | `memory.search.decay_half_life` | duration |
| `ALFREDAI_MEMORY_SEARCH_MMR_DIVERSITY` | `memory.search.mmr_diversity` | float64 |
| `ALFREDAI_MEMORY_SEARCH_EMBEDDING_CACHE_SIZE` | `memory.search.embedding_cache_size` | int |
| `ALFREDAI_BYTEROVER_BASE_URL` | `memory.byterover.base_url` | string |
| `ALFREDAI_BYTEROVER_API_KEY` | `memory.byterover.api_key` | string |
| `ALFREDAI_BYTEROVER_PROJECT_ID` | `memory.byterover.project_id` | string |

### Security

| Environment Variable | Config Path | Type |
|---------------------|-------------|------|
| `ALFREDAI_ENCRYPTION_KEY` | *(runtime passphrase)* | string |
| `ALFREDAI_SECURITY_ENCRYPTION_ENABLED` | `security.encryption.enabled` | bool (`"true"`) |
| `ALFREDAI_SECURITY_AUDIT_ENABLED` | `security.audit.enabled` | bool (`"true"` / `"false"`) |
| `ALFREDAI_SECURITY_AUDIT_PATH` | `security.audit.path` | string |
| `ALFREDAI_SECURITY_CONSENT_DIR` | `security.consent_dir` | string |

### Channels

| Environment Variable | Config Path | Description |
|---------------------|-------------|-------------|
| `ALFREDAI_TELEGRAM_TOKEN` | `channels[].telegram_token` | Applied to all telegram channels with an empty token. |
| `ALFREDAI_DISCORD_TOKEN` | `channels[].discord_token` | Applied to all discord channels with an empty token. |
| `ALFREDAI_SLACK_BOT_TOKEN` | `channels[].slack_bot_token` | Applied to all slack channels with an empty bot token. |
| `ALFREDAI_SLACK_APP_TOKEN` | `channels[].slack_app_token` | Applied to all slack channels with an empty app token. |
| `ALFREDAI_WHATSAPP_TOKEN` | `channels[].whatsapp_token` | Applied to all whatsapp channels with an empty token. |
| `ALFREDAI_MATRIX_ACCESS_TOKEN` | `channels[].matrix_access_token` | Applied to all matrix channels with an empty token. |

### LLM Providers

| Environment Variable | Config Path | Description |
|---------------------|-------------|-------------|
| `ALFREDAI_LLM_PROVIDER_<NAME>_API_KEY` | `llm.providers[].api_key` | Per-provider API key. `<NAME>` is the uppercased provider `name`. |

### Gateway

| Environment Variable | Config Path | Type |
|---------------------|-------------|------|
| `ALFREDAI_GATEWAY_ENABLED` | `gateway.enabled` | bool (`"true"`) |
| `ALFREDAI_GATEWAY_ADDR` | `gateway.addr` | string |

### Nodes

| Environment Variable | Config Path | Type |
|---------------------|-------------|------|
| `ALFREDAI_NODES_ENABLED` | `nodes.enabled` | bool (`"true"`) |
| `ALFREDAI_NODES_HEARTBEAT_INTERVAL` | `nodes.heartbeat_interval` | duration |
| `ALFREDAI_NODES_INVOKE_TIMEOUT` | `nodes.invoke_timeout` | duration |

---

## Secret Encryption

API keys and tokens in the config file can be encrypted at rest using AES-256-GCM with Argon2id key derivation.

### Encrypting a value

Use the `alfredai encrypt` command (or the `EncryptValue` Go function) with a passphrase to produce an encrypted string.

### Using encrypted values

Prefix the encrypted string with `enc:` in the config file:

```yaml
llm:
  providers:
    - name: openai
      api_key: "enc:abcdef0123456789:fedcba9876543210..."
```

### Decryption at load time

Set the `ALFREDAI_CONFIG_KEY` environment variable to the passphrase. The loader will automatically decrypt all `enc:` prefixed values in:

- `llm.providers[].api_key`
- `memory.embedding.api_key`
- Channel tokens (`telegram_token`, `discord_token`, `slack_bot_token`, `slack_app_token`, `whatsapp_token`, `whatsapp_app_secret`, `matrix_access_token`)
- `tools.voice_call.twilio_auth_token`
- `tools.voice_call.openai_api_key`
- `gateway.auth.tokens[].token`
