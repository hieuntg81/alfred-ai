# Testing Guide

alfred-ai has comprehensive testing at multiple levels: unit tests, fuzz tests, integration tests, and benchmarks.

## Quick Reference

| Command | Description |
|---------|-------------|
| `make test` | Run all unit tests |
| `make test-verbose` | Verbose output |
| `make test-race` | Race detection (requires CGO) |
| `make test-cover` | Generate coverage report |
| `make fuzz-short` | Quick fuzz regression (10s per tool) |
| `make fuzz-all` | Comprehensive fuzzing |
| `make bench` | Run all benchmarks |
| `make bench-cpu` | CPU profiling |
| `make bench-mem` | Memory profiling |
| `make bench-compare` | Compare with previous run |
| `make test-integration` | Run all integration tests |
| `make test-integration-llm` | LLM providers only |
| `make test-integration-e2e` | End-to-end workflows |
| `make test-integration-channels` | Channel tests |
| `make lint` | Run golangci-lint |

## Unit Tests

Standard Go unit tests with mocked dependencies.

```bash
# Run all tests
make test

# Run specific package
go test ./internal/usecase -v

# Run with coverage
make test-cover
go tool cover -html=coverage.out
```

### Writing Unit Tests

Use table-driven tests and hand-written mocks:

```go
func TestMyFeature(t *testing.T) {
    tests := []struct {
        name  string
        input string
        want  string
    }{
        {"basic", "hello", "HELLO"},
        {"empty", "", ""},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := MyFeature(tt.input)
            if got != tt.want {
                t.Errorf("MyFeature(%q) = %q, want %q", tt.input, got, tt.want)
            }
        })
    }
}
```

The project uses hand-written mocks co-located with test files. Mocks are designed with pre-programmed responses indexed by call order and are thread-safe. See `internal/usecase/usecase_test.go` for examples.

## Race Detection

Detects data races in concurrent code:

```bash
make test-race
# or manually:
CGO_ENABLED=1 go test -race ./...
```

Race detection requires CGO. If you see `"cgo: not enabled"`, ensure `CGO_ENABLED=1` is set.

**Common race conditions to watch for:**
- Concurrent map access without locks
- Shared state modification in goroutines
- Channel operations without proper synchronization

## Fuzz Testing

Security-focused fuzzing validates input sanitization for tools that process untrusted input:

```bash
# Quick regression (10s per tool)
make fuzz-short

# Comprehensive fuzzing (30min)
make fuzz-all

# Specific target
go test -fuzz=FuzzShellTool -fuzztime=30s ./internal/adapter/tool
```

Fuzz tests exist for:
- `FuzzShellTool` — shell command injection
- `FuzzFilesystemTool` — path traversal
- `FuzzWebTool` — SSRF bypass
- `FuzzBrowserTool` — browser injection
- `FuzzDelegateTool` — delegation input validation
- `FuzzCameraTool` — camera payload validation
- `FuzzLocationTool` — location input validation
- `FuzzVoiceCallTool` — voice call input validation
- `FuzzErrorClassify` — error classification edge cases
- `FuzzVectorSearch` — vector search input handling
- `FuzzPluginDiscovery` — plugin manifest parsing
- `FuzzPluginPermissions` — permission validation

## Integration Tests

Integration tests validate alfred-ai against real external services (LLM APIs, channels).

### Setup

```bash
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
export GEMINI_API_KEY="..."
```

### Running

```bash
# Check configuration
make test-integration-check

# Run all integration tests
make test-integration

# Run specific suites
make test-integration-llm      # LLM providers only
make test-integration-e2e      # End-to-end workflows
make test-integration-channels # Communication channels
```

### What's Tested

**LLM Providers** (`internal/adapter/llm/*_integration_test.go`):
- Basic chat completion with real APIs
- Single and parallel tool calling
- Tool result mapping (validates tool_call_id uniqueness)
- Multi-turn conversations with memory
- Error handling (rate limits, invalid params)

**End-to-End** (`internal/integration/e2e_test.go`):
- Full agent workflow with real LLM
- Multi-turn conversations with memory persistence
- Multi-step tool usage
- Session save/load from disk

**Channels** (`internal/adapter/channel/http_integration_test.go`):
- Real HTTP requests to HTTP channel
- Health endpoint
- Error handling

### Cost

Integration tests make real API calls:
- OpenAI (gpt-4o-mini): ~$0.05
- Anthropic (claude-3-5-haiku): ~$0.03
- Gemini (gemini-1.5-flash): ~$0.02
- **Total: ~$0.10 per full run**

Tests use the smallest/cheapest models with `temperature: 0` and low `max_tokens`.

### Best Practices

All integration tests must:
1. Use build tags: `//go:build integration`
2. Skip without API keys: `integration.SkipIfNoAPIKey(t, cfg.OpenAIKey, "OPENAI")`
3. Use timeouts: `ctx := integration.NewTestContext(t, 60*time.Second)`
4. Use cheap models (mini/haiku/flash)

## Benchmarks

Performance benchmarks for critical paths.

### Running

```bash
# Run all benchmarks
make bench

# Specific package
go test -bench=BenchmarkAgent -benchmem ./internal/usecase

# With profiling
make bench-cpu     # CPU profile
make bench-mem     # Memory profile

# Compare runs
go test -bench=. -benchmem -count=5 ./... | tee old.txt
# ... make changes ...
go test -bench=. -benchmem -count=5 ./... | tee new.txt
benchstat old.txt new.txt   # go install golang.org/x/perf/cmd/benchstat@latest
```

### Available Benchmarks

**Agent Core** (`internal/usecase/agent_bench_test.go`):

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkAgentStartup` | Agent construction overhead |
| `BenchmarkAgentChat` | HandleMessage by history size (0, 10, 50, 100, 500 messages) |
| `BenchmarkToolExecutionOverhead` | Tool dispatch (1, 5, 10 calls per turn) |
| `BenchmarkMessageRouting` | Router.Handle end-to-end |
| `BenchmarkConcurrentSessions` | N concurrent sessions throughput |
| `BenchmarkContextBuilder` | Context building with varying memory counts |
| `BenchmarkSessionOperations` | Create, append, copy |

**Compression** (`internal/usecase/compressor_bench_test.go`):

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkShouldCompress` | Threshold check across session sizes |
| `BenchmarkCompressMessages` | Message manipulation |
| `BenchmarkCompress` | Full compress pipeline |
| `BenchmarkForceCompress` | Emergency compression |

**Vector Memory** (`internal/adapter/memory/vector/search_bench_test.go`):

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkVectorSearch` | Cosine similarity across index sizes |

### Key Results

Reference results (Intel i9-13900H, Linux, Go 1.24):

| Operation | ns/op | B/op | allocs/op |
|-----------|------:|-----:|----------:|
| AgentChat (0 history) | ~6,600 | 2,032 | 22 |
| AgentChat (100 history) | ~19,800 | 37,984 | 20 |
| AgentChat (500 history) | ~226,000 | 301,641 | 540 |
| Tool dispatch (1 call) | ~18,600 | 5,735 | 50 |
| Tool dispatch (10 calls) | ~119,500 | 26,041 | 190 |
| Concurrent sessions (100) | ~62,000 | 28,705 | 75 |
| Session AddMessage | ~1,340 | 625 | 0 |
| Session GetOrCreate | ~210 | 17 | 1 |

Memory usage scales linearly with history size. Per-operation cost decreases with concurrency — the router handles concurrent sessions without contention bottlenecks.

### Interpreting Results

- **ns/op**: Nanoseconds per operation (lower is better)
- **B/op**: Bytes allocated per operation (lower is better)
- **allocs/op**: Heap allocations per operation (lower is better)

Use `benchstat` with at least 5 iterations (`-count=5`) for statistically significant comparisons.

## CI/CD Integration

Tests run automatically in GitHub Actions. See [CI/CD](ci-cd.md) for workflow details.

- **Unit tests + race detection**: Every push and PR
- **Security scanning**: Every push + weekly
- **Integration tests**: Weekly (Mondays) + manual dispatch
- **Benchmarks**: Every push to main (with PR comparison)

## Troubleshooting

### "cgo: not enabled"

Race detector requires CGO:

```bash
CGO_ENABLED=1 go test -race ./...
```

### Integration tests always skip

Set required environment variables:

```bash
export OPENAI_API_KEY=sk-...
go test -v -tags=integration ./internal/adapter/llm/
```

### Benchmarks show high variance

Run more iterations:

```bash
go test -bench=. -benchtime=10s ./...   # 10 seconds per benchmark
go test -bench=. -count=5 ./...         # 5 iterations
```

### "context deadline exceeded" in integration tests

Increase timeout or reduce parallelism:

```bash
go test -v -tags=integration -p 1 ./internal/adapter/llm/
```
