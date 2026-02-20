# Telegram Bot Example

Run alfred-ai as a Telegram bot with persistent memory.

## Prerequisites

- OpenAI API key
- Telegram bot token (from [@BotFather](https://t.me/botfather))

## Setup

1. Create a Telegram bot:
   - Talk to [@BotFather](https://t.me/botfather)
   - Send `/newbot`
   - Follow instructions
   - Copy the bot token

2. Run the bot:
```bash
export TELEGRAM_BOT_TOKEN=123456:ABC-DEF...
export OPENAI_API_KEY=sk-...
../../alfred-ai --config=config.yaml
```

3. Find your bot on Telegram and start chatting!

## What's Included

- OpenAI GPT-4 provider
- Markdown memory with auto-curation
- Telegram channel integration
- Per-user conversation history
