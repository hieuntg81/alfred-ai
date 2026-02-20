# Matrix Channel Setup

Connect alfred-ai to the Matrix network for self-hosted, federated chat.

## Prerequisites

- alfred-ai binary installed
- A Matrix homeserver (e.g., [Synapse](https://matrix.org/docs/guides/installing-synapse), [Conduit](https://conduit.rs/))
- A Matrix bot account on the homeserver
- An LLM provider API key

## Step 1: Create a Matrix Bot Account

On your homeserver, register a bot account:

```bash
# Using Synapse admin API
register_new_matrix_user -c /path/to/homeserver.yaml http://localhost:8008 \
  -u alfred-bot -p your-password --admin
```

Or create a regular account through the Matrix client and log in.

## Step 2: Get an Access Token

```bash
curl -X POST "https://your-homeserver/_matrix/client/r0/login" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "m.login.password",
    "user": "alfred-bot",
    "password": "your-password"
  }'
```

Copy the `access_token` from the response.

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
  - type: matrix
    matrix_homeserver: "https://your-homeserver"
    matrix_access_token: ${MATRIX_ACCESS_TOKEN}
    matrix_user_id: "@alfred-bot:your-homeserver"
```

## Step 4: Run

```bash
export OPENAI_API_KEY=sk-proj-...
export MATRIX_ACCESS_TOKEN=your-access-token
./alfred-ai
```

## Step 5: Test

1. Open your Matrix client (Element, etc.)
2. Invite the bot to a room
3. Send a message mentioning the bot or in a DM
4. The bot should respond

## Troubleshooting

### Bot doesn't join rooms
- Ensure the bot account has permission to join rooms
- Check that the homeserver URL is correct and accessible
- Verify the access token hasn't expired

### "Forbidden" errors
- The access token may be expired; generate a new one
- Check room permissions

### Bot responds to its own messages
- This is handled automatically; check logs if it occurs

## Advanced Options

### Mention-only mode
Only respond when the bot is mentioned:
```yaml
channels:
  - type: matrix
    matrix_homeserver: "https://your-homeserver"
    matrix_access_token: ${MATRIX_ACCESS_TOKEN}
    matrix_user_id: "@alfred-bot:your-homeserver"
    mention_only: true
```

### Federation
The bot works across federated homeservers. Invite it using its full Matrix ID (e.g., `@alfred-bot:your-homeserver`).
