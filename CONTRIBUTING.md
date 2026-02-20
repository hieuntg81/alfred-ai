# Contributing to alfred-ai

Thank you for your interest in contributing! This guide covers everything you
need to get started.

## Getting Started

1. Fork the repository and clone your fork:

```bash
git clone https://github.com/<your-username>/alfredai.git
cd alfredai
```

2. Install dependencies:

```bash
go mod download
```

3. Run the test suite:

```bash
make test
```

4. Build the binary:

```bash
make build
```

## Development

### Requirements

- Go 1.24+
- (Optional) TinyGo for WASM plugin development
- (Optional) `golangci-lint` for linting

### Common Commands

| Command | Description |
|---------|-------------|
| `make test` | Run all tests |
| `make test-race` | Run tests with race detector |
| `make test-cover` | Generate coverage report |
| `make bench` | Run all benchmarks |
| `make lint` | Run linter |
| `make fmt` | Format code |
| `make vet` | Run go vet |
| `make build` | Build the binary |

## Architecture

alfred-ai follows clean architecture with four layers:

```
domain/      Pure types and interfaces (no external deps)
  |
usecase/     Business logic (agent, router, sessions, scheduling)
  |
adapter/     Implementations (LLM providers, channels, tools, gateway)
  |
infra/       Infrastructure (config, logging, tracing)
```

**Key principles:**

- Dependencies point inward: `adapter` imports `domain`, never the reverse.
- The `domain` package has zero external dependencies.
- `usecase` defines interfaces; `adapter` implements them.
- `cmd/agent/` wires everything together at startup.

### Key Packages

| Package | Purpose |
|---------|---------|
| `internal/usecase` | Agent loop, router, sessions, compression |
| `internal/adapter/llm` | LLM providers (OpenAI, Anthropic, Gemini, Ollama) |
| `internal/adapter/channel` | Chat channels (CLI, Discord, Slack, etc.) |
| `internal/adapter/tool` | Built-in tools (shell, filesystem, web, etc.) |
| `internal/adapter/gateway` | WebSocket RPC gateway + REST API |
| `internal/adapter/memory` | Memory providers (vector, markdown) |
| `internal/security` | Encryption, audit, secret scanning, sandboxing |
| `internal/plugin` | Plugin system (discovery, WASM runtime, registry) |
| `internal/infra/config` | YAML configuration with env var overrides |

## Code Style

- **Formatting**: `gofmt` (enforced by CI).
- **Linting**: `golangci-lint` with the project's `.golangci.yml`.
- **Naming**: Follow standard Go conventions. Explicit over clever.
- **Errors**: See the Error Handling section below.
- **Comments**: Only where the code isn't self-explanatory. No redundant godoc.

## Error Handling

All error creation and wrapping in alfred-ai follows boundary-specific rules.

### Which error constructor to use

| Boundary | Constructor | Example |
|----------|------------|---------|
| Infrastructure wrapping (adapter/, security/) | `domain.WrapOp(op, err)` or `fmt.Errorf("op: %w", err)` | `domain.WrapOp("LLM.Chat", err)` |
| Domain invariant violation | `domain.NewDomainError(op, sentinel, detail)` | `NewDomainError("Tool.Execute", ErrToolNotFound, name)` |
| Subsystem-specific domain error (new code) | `domain.NewSubSystemError(subsystem, op, sentinel, detail)` | `NewSubSystemError("workflow", "Run", ErrNotFound, id)` |

### Sentinel error rules

- **New code**: Use category sentinels (`ErrNotFound`, `ErrTimeout`, `ErrLimitReached`, etc.) with a `SubSystem` field to distinguish subsystems.
- **Existing code**: Legacy subsystem-specific sentinels (e.g., `ErrWorkflowNotFound`) are being migrated to category sentinels. Do not add new subsystem-specific sentinels.
- **Retryable errors**: Use `domain.IsRetryableError(err)` to check if an error is transient. Currently covers `ErrRateLimit` and `ErrContextOverflow`.

### Tool parameter validation

Use the helpers in `internal/adapter/tool/validate.go`:

```go
if err := ValidateAll(
    RequireField("url", p.URL),
    ValidateURL("url", p.URL),
    ValidateMaxLength("body", p.Body, 1<<20),
); err != nil {
    return nil, err
}
```

## Testing

- **Unit tests are required** for all new code.
- Use table-driven tests where appropriate.
- Name test files `*_test.go` in the same package (white-box testing).
- Benchmarks go in `*_bench_test.go` files.
- Run `make test-race` before submitting to catch race conditions.

### Test Patterns

```go
func TestFoo(t *testing.T) {
    tests := []struct {
        name string
        input string
        want  string
    }{
        {"basic", "hello", "HELLO"},
        {"empty", "", ""},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := Foo(tt.input)
            if got != tt.want {
                t.Errorf("Foo(%q) = %q, want %q", tt.input, got, tt.want)
            }
        })
    }
}
```

## Pull Request Process

1. Create a branch from `main`:

```bash
git checkout -b feature/your-feature
```

2. Make your changes with tests.

3. Run the full test suite:

```bash
make test-race
make lint
```

4. Commit with a descriptive message:

```
Add vector search caching for improved query latency

Introduce a TTL-based cache layer in front of vector similarity search
to avoid redundant embedding computations for repeated queries.
```

5. Push and open a PR against `main`.

### PR Guidelines

- Keep PRs focused: one feature or fix per PR.
- Include tests for all new functionality.
- Update relevant documentation if behavior changes.
- Fill in the PR template completely.

## Plugin Development

Create a new plugin:

```bash
alfred-ai plugin init my-plugin
cd my-plugin
# Edit main.go
make build
alfred-ai plugin validate .
```

See [docs/guides/](docs/guides/) for detailed plugin development guides.

### Submitting a Plugin to the Registry

1. Build and test your plugin locally.
2. Create a GitHub release with the `.tar.gz` artifact.
3. Compute the SHA256 checksum: `sha256sum my-plugin.tar.gz`
4. Submit a PR to the plugin registry adding your entry to `plugins.json`.

## Areas Needing Help

We welcome contributions in these areas:

- **Skills**: New skill definitions in `skills/`
- **Channels**: Additional chat platform integrations
- **LLM Providers**: New provider adapters
- **Tools**: New built-in tools
- **Documentation**: Improvements to guides and references
- **Performance**: Benchmark improvements and optimizations
- **Bug fixes**: Check the issue tracker for open bugs

## Questions?

Open an issue for questions, bugs, or feature requests.
