# WebChat Channel Setup

The WebChat channel provides a browser-based chat widget that can be embedded in any website.

## Prerequisites

- alfred-ai binary installed
- An LLM provider API key

## Configuration

```yaml
llm:
  default_provider: openai
  providers:
    - name: openai
      type: openai
      api_key: ${OPENAI_API_KEY}
      model: gpt-4o

channels:
  - type: webchat
```

## Run and Test

```bash
export OPENAI_API_KEY=sk-proj-...
./alfred-ai
```

Open your browser and navigate to `http://localhost:8080` to see the chat widget.

## Troubleshooting

### Page doesn't load
- Check that port 8080 is not in use by another application
- Verify the channel is configured in your config.yaml
- Check logs for binding errors

### Messages not sending
- Check browser console for JavaScript errors
- Verify CORS settings if embedding on a different domain

## Advanced Options

### Running alongside other channels
```yaml
channels:
  - type: webchat
  - type: telegram
    token: ${TELEGRAM_BOT_TOKEN}
```

### Docker deployment
The WebChat channel is exposed through the container's port mapping:
```bash
docker compose up -d
# Access at http://localhost:8080
```

### Behind a reverse proxy
When running behind nginx or similar, configure the proxy to pass WebSocket connections:
```nginx
location /ws {
    proxy_pass http://localhost:8080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
}
```
