# alfred-ai Documentation

Welcome to the alfred-ai documentation. Guides are organized by audience — pick your starting point below.

## Getting Started

- **[Getting Started](getting-started.md)** — Installation, setup wizard, configuration, and your first conversation
- **[Deployment Guide](deployment.md)** — Docker, Docker Compose, Kubernetes, and Fly.io

## User Guides

### Channel Setup

| Channel | Guide | Use Case |
|---------|-------|----------|
| CLI | [cli-setup.md](guides/cli-setup.md) | Local development and testing |
| HTTP API | [http-api-setup.md](guides/http-api-setup.md) | Custom integrations and REST clients |
| Telegram | [telegram-setup.md](guides/telegram-setup.md) | Personal or group chat bot |
| Discord | [discord-setup.md](guides/discord-setup.md) | Server bot with slash commands |
| Slack | [slack-setup.md](guides/slack-setup.md) | Workspace assistant |
| WhatsApp | [whatsapp-setup.md](guides/whatsapp-setup.md) | Business messaging |
| Matrix | [matrix-setup.md](guides/matrix-setup.md) | Self-hosted, federated chat |
| WebChat | [webchat-setup.md](guides/webchat-setup.md) | Embed in your website |

### Reference

- **[Configuration Reference](reference/config.md)** — All YAML options, environment variables, and encrypted secrets
- **[Skills Index](reference/skills.md)** — All 35 built-in skills with descriptions and triggers
- **[Tools Index](reference/tools.md)** — All 27+ built-in tools with descriptions and categories

## Security & Operations

- **[Security](security.md)** — Encryption, SSRF protection, sandboxing, audit logging, secret scanning, key rotation
- **[Troubleshooting](troubleshooting.md)** — Common errors and solutions

## Contributor Guides

- **[Contributing](../CONTRIBUTING.md)** — Development setup, code style, error handling conventions, PR process
- **[Architecture & Patterns](architecture.md)** — Clean architecture, design patterns, and code conventions
- **[Testing Guide](development/testing.md)** — Unit tests, integration tests, fuzz testing, benchmarks
- **[CI/CD](development/ci-cd.md)** — GitHub Actions workflows, security scanning, release process
- **[Development Patterns](development/patterns.md)** — Detailed pattern catalog with code examples
