# Deployment Guide

This guide covers deploying alfred-ai in production environments.

## Docker Compose (Recommended)

The simplest way to run alfred-ai in production.

### Basic Setup

```bash
git clone https://github.com/hieuntg81/alfred-ai && cd alfred-ai
cp .env.example .env
```

Edit `.env` with your API keys and configuration:

```env
OPENAI_API_KEY=sk-proj-...
ALFREDAI_PORT=8080
ALFREDAI_GATEWAY_PORT=8090
```

Create or customize `config.yaml` for your deployment, then start:

```bash
docker compose up -d
```

### With Web Search

Enable the SearXNG profile for privacy-respecting web search:

```bash
docker compose --profile search up -d
```

SearXNG will be available at `http://localhost:6060` and alfred-ai will use it automatically.

### With Browser Support

Build with Chromium for the browser tool:

```bash
INSTALL_BROWSER=true docker compose up -d --build
```

### Docker Compose Reference

```yaml
services:
  alfred-ai:
    build:
      context: .
      args:
        INSTALL_BROWSER: "${INSTALL_BROWSER:-false}"
    restart: unless-stopped
    ports:
      - "${ALFREDAI_PORT:-8080}:8080"       # HTTP channel
      - "${ALFREDAI_GATEWAY_PORT:-8090}:8090" # WebSocket gateway
    volumes:
      - ./config.yaml:/app/config.yaml:ro
      - alfred-data:/app/data
    env_file:
      - .env
```

### Data Persistence

The `alfred-data` Docker volume stores:

- Memory files (`/app/data/memories/`)
- Session data (`/app/data/sessions/`)
- Audit logs (`/app/data/audit.jsonl`)
- Cron job state, workflow data, notes

Back up the volume regularly:

```bash
docker run --rm -v alfred-data:/data -v $(pwd):/backup alpine tar czf /backup/alfred-data-backup.tar.gz /data
```

## Docker (Standalone)

Build and run directly:

```bash
docker build -t alfred-ai .
docker run -d \
  --name alfred-ai \
  -p 8080:8080 \
  -p 8090:8090 \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  -v alfred-data:/app/data \
  -e OPENAI_API_KEY=sk-proj-... \
  alfred-ai
```

### Edge/IoT Build

For a lightweight edge build without heavy dependencies:

```bash
docker build --build-arg BUILD_TAGS=edge -t alfred-ai:edge .
```

### Multi-Platform Build

Build for multiple architectures:

```bash
docker buildx build --platform linux/amd64,linux/arm64 -t alfred-ai:latest .
```

## Fly.io

alfred-ai includes a `fly.toml` for one-command deployment to [Fly.io](https://fly.io):

```bash
fly launch
fly secrets set OPENAI_API_KEY=sk-proj-...
fly deploy
```

The default configuration:

- **Region**: `sin` (Singapore) — change in `fly.toml`
- **Resources**: 512MB RAM, shared CPU
- **Auto-scaling**: Scales to zero when idle, starts on request
- **HTTPS**: Forced by default

### Fly.io with Persistent Storage

Add a volume for persistent data:

```bash
fly volumes create alfred_data --size 1 --region sin
```

Update `fly.toml`:

```toml
[mounts]
  source = "alfred_data"
  destination = "/app/data"
```

## Kubernetes

### Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: alfred-ai
spec:
  replicas: 1
  selector:
    matchLabels:
      app: alfred-ai
  template:
    metadata:
      labels:
        app: alfred-ai
    spec:
      containers:
        - name: alfred-ai
          image: alfred-ai:latest
          ports:
            - containerPort: 8080
              name: http
            - containerPort: 8090
              name: gateway
          env:
            - name: ALFREDAI_CONFIG
              value: /app/config.yaml
            - name: OPENAI_API_KEY
              valueFrom:
                secretKeyRef:
                  name: alfred-ai-secrets
                  key: openai-api-key
          volumeMounts:
            - name: config
              mountPath: /app/config.yaml
              subPath: config.yaml
              readOnly: true
            - name: data
              mountPath: /app/data
          resources:
            requests:
              memory: "64Mi"
              cpu: "100m"
            limits:
              memory: "256Mi"
              cpu: "500m"
          livenessProbe:
            exec:
              command: ["/app/alfred-ai", "--version"]
            initialDelaySeconds: 5
            periodSeconds: 30
      volumes:
        - name: config
          configMap:
            name: alfred-ai-config
        - name: data
          persistentVolumeClaim:
            claimName: alfred-ai-data
```

### Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: alfred-ai-secrets
type: Opaque
stringData:
  openai-api-key: "sk-proj-..."
  # Add other secrets as needed
```

### Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: alfred-ai
spec:
  selector:
    app: alfred-ai
  ports:
    - name: http
      port: 8080
      targetPort: http
    - name: gateway
      port: 8090
      targetPort: gateway
```

## Binary Deployment

For bare-metal or VM deployments:

```bash
# Build for your target platform
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o alfred-ai ./cmd/agent

# Copy to server
scp alfred-ai config.yaml user@server:/opt/alfred-ai/

# Run
ssh user@server '/opt/alfred-ai/alfred-ai'
```

### Systemd Service

```ini
[Unit]
Description=alfred-ai Agent
After=network.target

[Service]
Type=simple
User=alfredai
WorkingDirectory=/opt/alfred-ai
ExecStart=/opt/alfred-ai/alfred-ai
Restart=on-failure
RestartSec=5
Environment=ALFREDAI_CONFIG=/opt/alfred-ai/config.yaml
EnvironmentFile=/opt/alfred-ai/.env

[Install]
WantedBy=multi-user.target
```

```bash
sudo cp alfred-ai.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now alfred-ai
```

## Reverse Proxy

For production, place alfred-ai behind a reverse proxy for TLS termination and rate limiting.

### nginx

```nginx
http {
    limit_req_zone $binary_remote_addr zone=alfred:10m rate=10r/s;

    server {
        listen 443 ssl;
        server_name alfred.example.com;

        ssl_certificate /etc/ssl/certs/alfred.crt;
        ssl_certificate_key /etc/ssl/private/alfred.key;

        location /api/ {
            limit_req zone=alfred burst=20 nodelay;
            proxy_pass http://localhost:8080;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location /ws/ {
            proxy_pass http://localhost:8090;
            proxy_http_version 1.1;
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection "upgrade";
        }
    }
}
```

### Caddy

```
alfred.example.com {
    rate_limit {
        zone dynamic {
            key {remote_host}
            events 10
            window 1s
        }
    }

    handle /api/* {
        reverse_proxy localhost:8080
    }

    handle /ws/* {
        reverse_proxy localhost:8090
    }
}
```

## Environment Variables

All configuration can be overridden via environment variables. See the [Configuration Reference](reference/config.md#environment-variable-overrides) for the full list.

Key variables for deployment:

| Variable | Description |
|----------|-------------|
| `ALFREDAI_CONFIG` | Path to config file |
| `OPENAI_API_KEY` | OpenAI API key |
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `ALFREDAI_ENCRYPTION_KEY` | Encryption passphrase |
| `ALFREDAI_CONFIG_KEY` | Passphrase for decrypting `enc:` values in config |
| `ALFREDAI_GATEWAY_ENABLED` | Enable WebSocket gateway |
| `ALFREDAI_SECURITY_AUDIT_ENABLED` | Enable audit logging |

## Production Checklist

- [ ] API keys stored in secrets manager or environment variables (not in config file)
- [ ] TLS termination via reverse proxy
- [ ] Rate limiting configured
- [ ] `security.audit.enabled: true` for compliance logging
- [ ] `security.encryption.enabled: true` if handling sensitive data
- [ ] `tools.sandbox_root` set to a restricted directory
- [ ] Data volume backed up regularly
- [ ] Log rotation configured for audit logs
- [ ] Health check endpoint monitored
- [ ] Resource limits set (memory, CPU)
