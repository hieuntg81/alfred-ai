# Slack Channel Setup

Connect alfred-ai to Slack as a workspace app that responds in channels and DMs.

## Prerequisites

- alfred-ai binary installed
- A Slack workspace where you have admin permissions
- An LLM provider API key

## Step 1: Create a Slack App

1. Go to [Slack API Apps](https://api.slack.com/apps)
2. Click "Create New App" > "From scratch"
3. Name it (e.g., "Alfred AI") and select your workspace
4. Go to **OAuth & Permissions** and add these Bot Token Scopes:
   - `app_mentions:read`
   - `chat:write`
   - `channels:history`
   - `groups:history`
   - `im:history`
   - `mpim:history`
5. Go to **Socket Mode** and enable it
6. Create an App-Level Token with `connections:write` scope — copy this token
7. Go to **Event Subscriptions** and enable events
8. Subscribe to bot events: `app_mention`, `message.im`
9. Install the app to your workspace
10. Copy the **Bot User OAuth Token** from OAuth & Permissions

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
  - type: slack
    token: ${SLACK_BOT_TOKEN}      # Bot User OAuth Token (xoxb-...)
    app_token: ${SLACK_APP_TOKEN}  # App-Level Token (xapp-...)
```

## Step 3: Run

```bash
export OPENAI_API_KEY=sk-proj-...
export SLACK_BOT_TOKEN=xoxb-...
export SLACK_APP_TOKEN=xapp-...
./alfred-ai
```

## Step 4: Test

1. Open your Slack workspace
2. Invite the bot to a channel: `/invite @Alfred AI`
3. Mention the bot: `@Alfred AI hello!`
4. Or DM the bot directly

## Troubleshooting

### Bot doesn't respond
- Verify both tokens are set correctly (bot token starts with `xoxb-`, app token with `xapp-`)
- Check that Socket Mode is enabled
- Ensure the bot is invited to the channel
- Check logs for WebSocket connection errors

### "not_authed" error
- Regenerate the Bot User OAuth Token
- Reinstall the app to your workspace

### Bot responds slowly
- Slack has a 3-second timeout for acknowledgements; alfred-ai handles this automatically
- For long-running requests, the bot shows a typing indicator

## Advanced Options

### Mention-only mode
Only respond when `@mentioned`:
```yaml
channels:
  - type: slack
    token: ${SLACK_BOT_TOKEN}
    app_token: ${SLACK_APP_TOKEN}
    mention_only: true
```

### Thread replies
The bot automatically responds in threads when replying to threaded messages.
