# HTTP API Channel Setup

The HTTP API channel exposes a REST endpoint for sending messages to alfred-ai programmatically. Use this for custom integrations, webhooks, or building your own frontend.

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
  - type: http
    http_addr: ":8080"
```

## Run and Test

```bash
export OPENAI_API_KEY=sk-proj-...
./alfred-ai
```

Send a message:
```bash
curl -X POST http://localhost:8080/api/v1/chat \
  -H "Content-Type: application/json" \
  -d '{"session_id": "test-session", "content": "Hello!"}'
```

Response:
```json
{
  "session_id": "test-session",
  "content": "Hello! I'm alfred-ai, your helpful AI assistant.",
  "is_error": false
}
```

## API Endpoints

### POST /api/v1/chat

Send a message and receive a response.

**Request body:**
```json
{
  "session_id": "string",
  "content": "string"
}
```

**Response:**
```json
{
  "session_id": "string",
  "content": "string",
  "is_error": false
}
```

## Troubleshooting

### "connection refused"
- Check the port isn't in use: `lsof -i :8080`
- Verify the http_addr in config

### Empty responses
- Check that the LLM provider is configured and the API key is valid
- Check logs for errors

### Slow responses
- LLM calls can take 2-10 seconds depending on the model
- Consider using streaming via the Gateway (port 8090)

## Advanced Options

### Custom port
```yaml
channels:
  - type: http
    http_addr: ":3000"
```

### Alongside other channels
```yaml
channels:
  - type: http
    http_addr: ":8080"
  - type: cli
```

### Gateway API (streaming)
For streaming responses, enable the gateway:
```yaml
gateway:
  enabled: true
  addr: ":8090"
```

This exposes a WebSocket/SSE endpoint for real-time streaming.

### Docker deployment
```bash
docker compose up -d
# API available at http://localhost:8080
```

### Rate limiting
Consider placing a reverse proxy (nginx, Caddy) in front of the HTTP channel for rate limiting and TLS.
