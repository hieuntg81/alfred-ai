# Getting Started

This guide walks you through installing alfred-ai, configuring it, and having your first conversation.

## Prerequisites

### Required

- **Go 1.24+**: [Download here](https://golang.org/dl/) (for source builds or `go install`)
- **API Key** from at least one LLM provider:
  - [OpenAI](https://platform.openai.com/api-keys) — most popular, pay-as-you-go
  - [Anthropic](https://console.anthropic.com/settings/keys) — great for long conversations
  - [Google Gemini](https://aistudio.google.com/app/apikey) — free tier available
  - [OpenRouter](https://openrouter.ai/) — access multiple providers with one key

### Optional

- **Docker**: For containerized deployment
- **Ollama**: For local/offline LLM inference
- **SearXNG**: For privacy-respecting web search
- **Chromium**: For the browser tool

## Installation

### Docker (fastest)

```bash
git clone https://github.com/hieuntg81/alfred-ai && cd alfred-ai
cp .env.example .env
# Edit .env with your API key
docker compose up -d
```

To enable web search:

```bash
docker compose --profile search up -d
```

### Go Install

```bash
go install github.com/hieuntg81/alfred-ai/cmd/agent@latest
```

### From Source

```bash
git clone https://github.com/hieuntg81/alfred-ai && cd alfred-ai
make build
```

#### Edge/IoT Build

For GPIO, BLE, and serial support on edge devices:

```bash
make build BUILD_TAGS=edge
```

## Configuration

### Interactive Setup (Recommended)

```bash
alfred-ai setup
```

The setup wizard guides you through:

1. **Template selection** — Personal assistant, Telegram bot, Secure & Private, or Advanced
2. **LLM provider** — Choose a provider and enter your API key (validated in real-time)
3. **Channel configuration** — CLI, Telegram, Discord, Slack, etc.
4. **Security settings** — Encryption, sandboxing, audit logging
5. **Automatic testing** — Sends test messages to verify everything works

Setup time: 3-15 minutes depending on the template.

### Quick Start with CLI Flags

```bash
alfred-ai --provider openai --model gpt-4o --key sk-proj-...
```

### Manual Configuration

Create `config.yaml`:

```yaml
llm:
  default_provider: openai
  providers:
    - name: openai
      type: openai
      api_key: ${OPENAI_API_KEY}
      model: gpt-4o

memory:
  provider: markdown
  data_dir: ./data/memory

channels:
  - type: cli
```

Set your API key and run:

```bash
export OPENAI_API_KEY=sk-proj-...
./alfred-ai
```

See the [Configuration Reference](reference/config.md) for all options.

## Your First Conversation

```
> Hello! What can you help me with?
```

The agent responds using your configured LLM. Try these:

- `"Remember my name is Alice and I prefer Python"` — tests memory
- `"Search the web for Go 1.24 release notes"` — tests web search (requires SearXNG)
- `"List files in the current directory"` — tests filesystem tool

## Enabling Long-Term Memory

Memory lets the agent remember context across sessions.

```yaml
memory:
  provider: markdown
  data_dir: ./data/memory
  auto_curate: true
```

For semantic search with embeddings:

```yaml
memory:
  provider: vector
  data_dir: ./data/memory
  auto_curate: true
  embedding:
    provider: openai
    model: text-embedding-3-small
  search:
    decay_half_life: 168h     # Prefer recent memories
    mmr_diversity: 0.3        # Reduce redundant results
    embedding_cache_size: 1000
```

## Configuring LLM Failover

For reliability, configure multiple providers with automatic failover:

```yaml
llm:
  default_provider: openai
  providers:
    - name: openai
      type: openai
      api_key: ${OPENAI_API_KEY}
      model: gpt-4o
    - name: anthropic
      type: anthropic
      api_key: ${ANTHROPIC_API_KEY}
      model: claude-sonnet-4-5-20250929
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
```

## Enabling Skills

alfred-ai ships with 35 built-in skills:

```yaml
skills:
  enabled: true
  dir: ./skills
```

Skills are activated based on their trigger type — see the [Skills Index](reference/skills.md).

## Enabling Security Features

### Sandbox

Restrict file operations to a specific directory:

```yaml
tools:
  sandbox_root: ./workspace
```

### Content Encryption

```yaml
security:
  encryption:
    enabled: true
    # Set ALFREDAI_ENCRYPTION_KEY env var with a strong passphrase
```

### Audit Logging

```yaml
security:
  audit:
    enabled: true
    path: ./data/audit.jsonl
```

See the [Security documentation](security.md) for comprehensive security hardening.

## Channel Guides

Set up alfred-ai on your preferred platform:

| Channel | Guide | Use Case |
|---------|-------|----------|
| CLI | [cli-setup.md](guides/cli-setup.md) | Local development and testing |
| Telegram | [telegram-setup.md](guides/telegram-setup.md) | Personal/group chat bot |
| Discord | [discord-setup.md](guides/discord-setup.md) | Server bot with slash commands |
| Slack | [slack-setup.md](guides/slack-setup.md) | Workspace assistant |
| WhatsApp | [whatsapp-setup.md](guides/whatsapp-setup.md) | Business messaging |
| Matrix | [matrix-setup.md](guides/matrix-setup.md) | Self-hosted, federated chat |
| WebChat | [webchat-setup.md](guides/webchat-setup.md) | Embed in your website |
| HTTP API | [http-api-setup.md](guides/http-api-setup.md) | Custom integrations |

## Health Check

Verify your setup:

```bash
alfred-ai doctor
```

This checks configuration validity, API connectivity, memory backend, and tool dependencies.

## Troubleshooting

### "llm provider not found"

- Check API key is set: `echo $OPENAI_API_KEY`
- Verify provider type matches (`openai`, `anthropic`, `gemini`, `openrouter`, `ollama`, `bedrock`)

### "memory provider unavailable"

- Check path exists: `mkdir -p ./data/memory`
- Verify write permissions

### "rate limit exceeded"

- Reduce request frequency
- Enable failover to a backup provider
- Check provider rate limits on their dashboard

For more troubleshooting help, see [Troubleshooting](troubleshooting.md).

## Next Steps

- Run `alfred-ai doctor` to verify your setup
- Read the [Configuration Reference](reference/config.md) for all options
- Check [Security](security.md) for security hardening
- Explore [channel guides](guides/) for your platform
- See the [Deployment Guide](deployment.md) for production hosting
