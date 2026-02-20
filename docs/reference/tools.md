# Tools Index

alfred-ai includes 27+ built-in tools that the LLM can invoke during conversations. Tools are registered in the tool registry and exposed to the LLM via function calling.

## Enabling Tools

Most tools are disabled by default and must be explicitly enabled in `config.yaml`. See the [Configuration Reference](config.md) for all options.

```yaml
tools:
  sandbox_root: ./workspace
  browser_enabled: true
  github_enabled: true
  # ...
```

## Core Tools

These tools are always available when their prerequisites are met.

| Tool | Description | Config |
|------|-------------|--------|
| `shell` | Execute allowed shell commands within the workspace | `tools.allowed_commands`, `tools.shell_timeout` |
| `filesystem` | Read, write, and list files within the sandbox | `tools.sandbox_root` |
| `web_fetch` | Fetch content from a URL with SSRF protection | Always available |
| `web_search` | Search the web via SearXNG | `tools.searxng_url` |
| `browser` | Navigate pages, extract content, click, type, screenshot, run JS | `tools.browser_enabled` |

## AI & Task Processing

| Tool | Description | Config |
|------|-------------|--------|
| `llm_task` | Delegate a subtask to an LLM with JSON Schema validation | `tools.llm_task_enabled` |
| `delegate` | Delegate a task to another agent (multi-agent mode) | Requires `agents` config |
| `sub_agent` | Spawn sub-agents to execute tasks in parallel | `agent.sub_agent.enabled` |

## Scheduling & Automation

| Tool | Description | Config |
|------|-------------|--------|
| `cron` | Create, list, update, and delete scheduled cron jobs | `tools.cron_enabled` |
| `workflow` | Run multi-step pipelines (exec, HTTP, transform, approval) | `tools.workflow_enabled` |
| `process` | Manage background process sessions with streaming output | `tools.process_enabled` |

## Communication

| Tool | Description | Config |
|------|-------------|--------|
| `message` | Send messages to connected channels, broadcast, or reply to threads | `tools.message_enabled` |
| `email` | List inbox, read, search, draft, send, and reply to emails | `tools.email_enabled` |
| `voice_call` | Make outbound voice calls with text-to-speech and transcription | `tools.voice_call.enabled` |

## Personal Data

| Tool | Description | Config |
|------|-------------|--------|
| `notes` | Create, read, update, delete, and search personal markdown notes | `tools.notes_enabled` |
| `calendar` | Manage calendars and events (list, create, update, delete) | `tools.calendar_enabled` |

## Edge / IoT

These tools require the `edge` build tag or specific hardware build tags.

| Tool | Description | Config |
|------|-------------|--------|
| `gpio` | Control GPIO pins (read, write, PWM) | Build tag: `edge` |
| `serial` | Communicate via USB serial ports (open, close, read, write) | Build tag: `edge` |
| `ble` | Bluetooth Low Energy device communication | Build tag: `edge` |
| `mqtt` | Publish and subscribe to MQTT topics | `tools.mqtt_enabled` |

## Remote Nodes

These tools require `nodes.enabled: true` in the configuration.

| Tool | Description | Config |
|------|-------------|--------|
| `node_list` | List all registered remote nodes | `nodes.enabled` |
| `node_invoke` | Invoke a capability on a remote node | `nodes.enabled` |
| `camera` | Capture photos and record video on remote nodes | `tools.camera_enabled` |
| `location` | Get geographic location of a remote node | `tools.location_enabled` |

## Integrations

| Tool | Description | Config |
|------|-------------|--------|
| `smart_home` | Control Home Assistant devices and automations | `tools.smarthome_enabled` |
| `github` | Manage GitHub repos, issues, and pull requests | `tools.github_enabled` |
| `canvas` | Create interactive HTML/CSS/JS visualizations | `tools.canvas_enabled` |
| `mcp_bridge` | Connect to MCP (Model Context Protocol) servers | MCP server config |

## Tool Security

All tools operate within the security boundaries configured for the agent:

- **Filesystem tools** are restricted to `tools.sandbox_root` via the [filesystem sandbox](../security.md#sandbox-execution)
- **Web tools** use [SSRF-safe HTTP transport](../security.md#ssrf-protection) with DNS rebinding prevention
- **Shell tools** are limited to `tools.allowed_commands`
- **Tool approval** can require human confirmation before execution via `agent.tool_approval`

See the [Security documentation](../security.md) for details.
