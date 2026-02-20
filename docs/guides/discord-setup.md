# Discord Channel Setup

Connect alfred-ai to Discord as a bot that responds in server channels and DMs.

## Prerequisites

- alfred-ai binary installed
- A Discord account
- A Discord server where you have admin permissions
- An LLM provider API key

## Step 1: Create a Discord Application

1. Go to the [Discord Developer Portal](https://discord.com/developers/applications)
2. Click "New Application" and give it a name (e.g., "Alfred AI")
3. Go to the **Bot** tab
4. Click "Add Bot"
5. Under "Privileged Gateway Intents", enable:
   - **Message Content Intent** (required to read messages)
6. Copy the bot token from the Bot tab

## Step 2: Invite the Bot to Your Server

1. Go to the **OAuth2 > URL Generator** tab
2. Select scopes: `bot`
3. Select permissions: `Send Messages`, `Read Message History`, `Read Messages/View Channels`
4. Copy the generated URL and open it in your browser
5. Select your server and authorize

## Step 3: Configure alfred-ai

```yaml
llm:
  default_provider: openai
  providers:
    - name: openai
      type: openai
      api_key: ${OPENAI_API_KEY}
      model: gpt-4o

channels:
  - type: discord
    token: ${DISCORD_BOT_TOKEN}
```

## Step 4: Run

```bash
export OPENAI_API_KEY=sk-proj-...
export DISCORD_BOT_TOKEN=your-discord-bot-token
./alfred-ai
```

## Step 5: Test

1. Go to your Discord server
2. Mention the bot: `@Alfred AI hello!`
3. The bot should respond in the channel

## Troubleshooting

### Bot shows as offline
- Check the token is correct
- Verify "Message Content Intent" is enabled in the Developer Portal
- Check logs for connection errors

### Bot doesn't respond to messages
- Ensure the bot has permission to read and send messages in the channel
- By default, the bot responds to mentions only; check `mention_only` setting

### "Invalid token" error
- Regenerate the token in the Developer Portal > Bot tab
- Make sure you're using the Bot token, not the Application ID

## Advanced Options

### Mention-only mode (default)
```yaml
channels:
  - type: discord
    token: ${DISCORD_BOT_TOKEN}
    mention_only: true  # Only respond when @mentioned
```

### Multiple servers
The bot automatically works across all servers it's invited to. Each server gets its own session context.
