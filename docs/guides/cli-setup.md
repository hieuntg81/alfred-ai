# CLI Channel Setup

The CLI channel provides an interactive terminal interface for chatting with alfred-ai. It's the default channel and ideal for development and testing.

## Prerequisites

- alfred-ai binary installed
- A configured LLM provider with API key

## Configuration

```yaml
channels:
  - type: cli
```

The CLI channel is the default when no channels are configured.

## Run and Test

```bash
# With config file
./alfred-ai

# Quick start (no config file needed)
./alfred-ai --provider openai --model gpt-4o --key sk-proj-...
```

You'll see an interactive prompt:
```
> Hello!
I'm alfred-ai, your helpful AI assistant. How can I help you today?
>
```

## Features

- Real-time streaming responses
- Privacy controls (toggle with `/privacy`)
- Chat history within session
- Full tool access (filesystem, shell, web search, etc.)

## Troubleshooting

### No prompt appears
- Check that your config.yaml is valid: `alfred-ai doctor`
- Ensure the API key is set in the environment

### Slow responses
- Check your internet connection
- Try a faster model (e.g., `gpt-4o-mini` instead of `gpt-4o`)

## Advanced Options

Run alongside other channels:
```yaml
channels:
  - type: cli
  - type: telegram
    token: ${TELEGRAM_BOT_TOKEN}
```

The CLI will run in addition to Telegram.
