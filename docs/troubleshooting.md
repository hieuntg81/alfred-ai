# Troubleshooting

Common issues and solutions for alfred-ai.

## Setup Issues

### "llm provider not found"

**Cause:** API key is missing or provider type is misconfigured.

**Solutions:**
1. Check API key is set: `echo $OPENAI_API_KEY`
2. Verify provider type matches one of: `openai`, `anthropic`, `gemini`, `openrouter`, `ollama`, `bedrock`
3. If using environment variables, ensure the variable name follows the pattern `ALFREDAI_LLM_PROVIDER_<NAME>_API_KEY`

### "Invalid API key" during setup

**Solutions:**
1. Verify key is copied correctly (no leading/trailing spaces)
2. Check if key has credits remaining on the provider dashboard
3. Ensure key is for the correct provider:
   - OpenAI: starts with `sk-proj-` or `sk-`
   - Anthropic: starts with `sk-ant-`
4. If setup wizard fails, skip API key and set via environment variable:
   ```bash
   export ALFREDAI_LLM_PROVIDER_OPENAI_API_KEY="sk-proj-..."
   ```

### "config validation failed"

**Cause:** One or more configuration values are invalid. alfred-ai collects all validation errors and reports them together.

**Solutions:**
1. Read the error messages — they list every invalid field
2. Check `config.yaml` against the [Configuration Reference](reference/config.md)
3. Ensure file permissions are `0600` or `0644` (more permissive modes are rejected)

### "Connection timeout" or "Connection failed"

**Solutions:**
1. Check your internet connection
2. Verify no firewall is blocking HTTPS traffic
3. Check provider status pages:
   - OpenAI: https://status.openai.com
   - Anthropic: https://status.anthropic.com

## Runtime Issues

### "memory provider unavailable"

**Solutions:**
1. Check path exists: `mkdir -p ./data/memory`
2. Verify write permissions: `ls -la ./data/memory/`
3. Check config: ensure `memory.provider` is not `noop`
4. For vector memory, ensure embedding provider is configured

### Memory not persisting across sessions

**Checks:**
1. Verify `memory.provider` is not `noop` in config.yaml
2. Check data directory exists and is writable
3. Look for memory files: `ls ./data/memory/`
4. If using encryption, note that encrypted data cannot be decrypted after restart unless the salt is persisted

### "rate limit exceeded"

**Solutions:**
1. Wait 60 seconds and try again
2. Enable failover to a backup provider:
   ```yaml
   llm:
     failover:
       enabled: true
       fallbacks: [anthropic, local]
   ```
3. Enable circuit breaker to prevent hammering a failing provider:
   ```yaml
   llm:
     circuit_breaker:
       enabled: true
       max_failures: 5
       timeout: 60s
   ```
4. Check your API usage on the provider dashboard

### "agent reached max iterations"

**Cause:** The agent's tool-calling loop hit the configured limit.

**Solutions:**
1. Increase `agent.max_iterations` (default: 10)
2. Check if a tool is failing repeatedly, causing the agent to retry
3. Review the conversation to see if the LLM is stuck in a loop

### "context window overflow"

**Cause:** The conversation history exceeded the model's context window.

**Solutions:**
1. Enable context compression:
   ```yaml
   agent:
     compression:
       enabled: true
       threshold: 30
       keep_recent: 10
   ```
2. Enable the context guard:
   ```yaml
   agent:
     context_guard:
       enabled: true
       max_tokens: 128000
       safety_margin: 0.15
   ```
3. Set `max_tokens` to match your model's actual context window size

### Tool execution errors

**"path is outside sandbox boundary":**
- The requested file path is outside `tools.sandbox_root`
- Check that your sandbox root is set correctly

**"request to private/reserved IP blocked":**
- SSRF protection blocked a request to a private IP
- This is expected behavior for security — the tool cannot access internal network addresses

**"command not allowed":**
- The shell command is not in `tools.allowed_commands`
- Add the command to the allowed list in config.yaml

## Docker Issues

### Container won't start

1. Check logs: `docker compose logs alfred-ai`
2. Verify `.env` file exists and has required variables
3. Ensure `config.yaml` is present and valid
4. Check port conflicts: `lsof -i :8080`

### "Config not found" in Docker

Ensure config is mounted correctly:
```yaml
volumes:
  - ./config.yaml:/app/config.yaml:ro
```

### Data not persisting after container restart

Ensure you're using a named volume:
```yaml
volumes:
  alfred-data:
```

## Build Issues

### "cgo: not enabled"

Race detector requires CGO:
```bash
CGO_ENABLED=1 go test -race ./...
```

### Edge build tags not working

Ensure you pass the build tag:
```bash
go build -tags edge -o alfred-ai ./cmd/agent
# or
make build BUILD_TAGS=edge
```

## Health Check

Run the built-in health check to diagnose issues:

```bash
alfred-ai doctor
```

This validates:
- Configuration file syntax and values
- API key connectivity
- Memory backend availability
- Tool dependencies (Chromium, SearXNG, etc.)

## Getting Help

- Check [GitHub Issues](https://github.com/hieuntg81/alfred-ai/issues) for known problems
- Type `help` in the alfred-ai CLI for in-app assistance
- Review the [Security documentation](security.md) for security-related issues
