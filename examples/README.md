# alfred-ai Examples

This directory contains example configurations for common use cases.

## Available Examples

### 1. [basic-cli/](basic-cli/)
Minimal CLI-only setup. Good starting point.
```bash
cd examples/basic-cli
export OPENAI_API_KEY=sk-...
../../alfred-ai --config=config.yaml
```

### 2. [telegram-bot/](telegram-bot/)
Telegram bot with memory and skills.
```bash
export TELEGRAM_BOT_TOKEN=...
export OPENAI_API_KEY=sk-...
cd examples/telegram-bot
../../alfred-ai --config=config.yaml
```

### 3. [multi-agent/](multi-agent/)
Multiple specialized agents with routing.
```bash
export OPENAI_API_KEY=sk-...
cd examples/multi-agent
../../alfred-ai --config=config.yaml
```

### 4. [encrypted-memory/](encrypted-memory/)
Full security: encryption, audit logging, sandboxing.
```bash
export ENCRYPTION_KEY=my-secret-passphrase
export OPENAI_API_KEY=sk-...
cd examples/encrypted-memory
../../alfred-ai --config=config.yaml
```

## Usage

Each example includes:
- `config.yaml` - Annotated configuration
- `README.md` - Setup instructions
- Environment requirements

Copy and modify for your needs.
