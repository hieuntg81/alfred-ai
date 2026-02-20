# WhatsApp Channel Setup

Connect alfred-ai to WhatsApp Business API for messaging.

## Prerequisites

- alfred-ai binary installed
- A [Meta Developer](https://developers.facebook.com/) account
- A Meta Business App with WhatsApp product enabled
- An LLM provider API key
- A publicly accessible URL (for webhooks)

## Step 1: Set Up WhatsApp Business API

1. Go to [Meta for Developers](https://developers.facebook.com/)
2. Create a new app or use an existing one
3. Add the "WhatsApp" product
4. Go to **WhatsApp > Getting Started**
5. Note down:
   - **Temporary access token** (or generate a permanent one)
   - **Phone number ID**
   - **WhatsApp Business Account ID**
6. Go to **App Settings > Basic** and copy your **App Secret**

## Step 2: Configure Webhooks

1. In the WhatsApp settings, go to **Configuration > Webhooks**
2. Set the callback URL to: `https://your-domain:3335/whatsapp/webhook`
3. Set a verify token (any string you choose)
4. Subscribe to: `messages`

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
  - type: whatsapp
    whatsapp_token: ${WHATSAPP_TOKEN}
    whatsapp_phone_id: ${WHATSAPP_PHONE_ID}
    whatsapp_verify_token: ${WHATSAPP_VERIFY_TOKEN}
    whatsapp_app_secret: ${WHATSAPP_APP_SECRET}
    whatsapp_webhook_addr: ":3335"
```

## Step 4: Run

```bash
export OPENAI_API_KEY=sk-proj-...
export WHATSAPP_TOKEN=your-access-token
export WHATSAPP_PHONE_ID=your-phone-number-id
export WHATSAPP_VERIFY_TOKEN=your-verify-token
export WHATSAPP_APP_SECRET=your-app-secret
./alfred-ai
```

## Step 5: Test

1. Open WhatsApp
2. Send a message to the phone number associated with your WhatsApp Business API
3. The bot should respond

## Troubleshooting

### Webhook verification fails
- Ensure your verify token matches exactly between Meta settings and config
- Check that the webhook URL is publicly accessible
- Verify the webhook addr port is not blocked by a firewall

### Bot doesn't respond
- Check that the access token is valid and not expired
- Verify the phone number ID is correct
- Check logs for API errors from Meta

### "Invalid app secret" error
- Copy the app secret from App Settings > Basic (not the client token)

## Advanced Options

### Mention-only mode
```yaml
channels:
  - type: whatsapp
    whatsapp_token: ${WHATSAPP_TOKEN}
    whatsapp_phone_id: ${WHATSAPP_PHONE_ID}
    whatsapp_verify_token: ${WHATSAPP_VERIFY_TOKEN}
    whatsapp_app_secret: ${WHATSAPP_APP_SECRET}
    mention_only: true
```

### Custom webhook address
```yaml
channels:
  - type: whatsapp
    whatsapp_webhook_addr: ":4000"  # Custom port
```
