# Telegram Channel Setup

Connect alfred-ai to Telegram as a bot that responds to messages in private chats and groups.

## Prerequisites

- alfred-ai binary installed
- A Telegram account
- An LLM provider API key

## Step 1: Create a Telegram Bot

1. Open Telegram and search for [@BotFather](https://t.me/botfather)
2. Send `/newbot`
3. Choose a display name (e.g., "Alfred AI")
4. Choose a username (must end in "bot", e.g., "my_alfred_ai_bot")
5. Copy the bot token (looks like `123456789:ABCdefGhIJKlmNoPQRsTUVwxyz`)

## Step 2: Configure alfred-ai

```yaml
llm:
  default_provider: openai
  providers:
    - name: openai
      type: openai
      api_key: ${OPENAI_API_KEY}
      model: gpt-4o

channels:
  - type: telegram
    token: ${TELEGRAM_BOT_TOKEN}
```

## Step 3: Run

```bash
export OPENAI_API_KEY=sk-proj-...
export TELEGRAM_BOT_TOKEN=123456789:ABCdefGhIJKlmNoPQRsTUVwxyz
./alfred-ai
```

## Step 4: Test

1. Open Telegram
2. Search for your bot by username
3. Send `/start` or any message
4. The bot should respond

## Troubleshooting

### Bot doesn't respond
- Verify the token: `echo $TELEGRAM_BOT_TOKEN`
- Check logs for errors
- Ensure the bot hasn't been disabled by BotFather

### Bot responds in private but not in groups
- Add the bot to the group as an admin
- Enable "mention only" mode to require `@botname` prefix:

```yaml
channels:
  - type: telegram
    token: ${TELEGRAM_BOT_TOKEN}
    mention_only: true
```

### Rate limiting
- Telegram limits bots to ~30 messages/second
- For high-traffic bots, consider using webhook mode (requires public URL)

## Advanced Options

### Mention-only mode
Only respond when the bot is mentioned with `@`:
```yaml
channels:
  - type: telegram
    token: ${TELEGRAM_BOT_TOKEN}
    mention_only: true
```

### Running with Docker
```bash
TELEGRAM_BOT_TOKEN=your-token docker compose up -d
```

### Multiple channels
```yaml
channels:
  - type: cli
  - type: telegram
    token: ${TELEGRAM_BOT_TOKEN}
```
