# Patterns Guide — alfred-ai

This document catalogs the notable design patterns found in the alfred-ai codebase. Each pattern includes a description, a real code snippet, and guidance on when to reuse it.

---

## Project Structure

### Layered Directory Layout

The project follows a strict layered architecture with clear boundaries:

```
cmd/agent/          # Composition root — wires everything together
internal/domain/    # Pure domain types and interfaces (zero external deps)
internal/usecase/   # Business logic orchestration
internal/adapter/   # External system integrations (LLM, channels, tools, TUI)
internal/infra/     # Cross-cutting infrastructure (config, logger, tracer)
internal/security/  # Security primitives (sandbox, encryption, SSRF, audit)
internal/plugin/    # Plugin system lifecycle
internal/integration/ # Integration test helpers
pkg/nodesdk/        # Public SDK for building node agents
pkg/pluginsdk/      # Public SDK for building plugins
```

The `internal/` boundary prevents external consumers from importing implementation details, while `pkg/` exposes stable public APIs. The `cmd/agent/` directory acts as the composition root — it imports from all layers but no layer imports from it.

**When to apply:** Any Go project with multiple external integrations. The key insight is that `cmd/` files handle all dependency wiring, keeping business logic free of initialization concerns.

### Phased Initialization in cmd/

The `cmd/agent/` directory splits initialization into focused files by concern:

```go
// cmd/agent/init_agent.go
type AgentComponents struct {
    Agent          *usecase.Agent
    ToolRegistry   *tool.Registry
    SessionManager *usecase.SessionManager
    ContextBuilder *usecase.ContextBuilder
    Compressor     *usecase.Compressor
    Approver       domain.ToolApprover
    ProcessManager *usecase.ProcessManager
}

func initAgent(ctx context.Context, cfg *config.Config, ...) (*AgentComponents, error) {
    // 1. Init tool approver
    // 2. Init tool backends + registry
    // 3. Init session manager
    // 4. Init context builder
    // 5. Load skills
    // 6. Init compressor
    // 7. Init agent
    // 8. Init sub-agent
    return &AgentComponents{...}, nil
}
```

Each `init_*.go` file returns a typed components struct: `SecurityComponents`, `LLMComponents`, `AgentComponents`, `FeatureComponents`, `RuntimeComponents`. The `main()` function chains them in dependency order:

```go
// cmd/agent/main.go
func run() error {
    // 1. Config
    // 2. Logger & Tracer
    // 3. Security (sandbox, encryption, audit)
    // 4. LLM providers
    // 5. Event bus
    // 6. Memory
    // 7. Agent components
    // 8. Features (plugins, nodes, privacy, curator)
    // 9. Runtime (router, channels, scheduler, gateway)
    // 10. Graceful shutdown
    // ...
}
```

**When to apply:** Projects with complex initialization sequences. Splitting init into focused files keeps `main()` readable and makes each subsystem independently testable.

---

## Architecture Patterns

### Domain-Centric Interface Design

All core interfaces live in `internal/domain/` with zero external dependencies. The domain package defines contracts; adapters implement them:

```go
// internal/domain/provider.go
type LLMProvider interface {
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    Name() string
}

type StreamingLLMProvider interface {
    LLMProvider
    ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamDelta, error)
}
```

```go
// internal/domain/channel.go
type Channel interface {
    Start(ctx context.Context, handler MessageHandler) error
    Stop(ctx context.Context) error
    Send(ctx context.Context, msg OutboundMessage) error
    Name() string
}
```

Interfaces are small (3-5 methods), single-purpose, and consumer-defined. The `StreamingLLMProvider` extends `LLMProvider` through embedding, allowing callers to check for streaming support via type assertion.

**When to apply:** Any project with multiple implementations of the same capability. Keep interfaces in the domain layer so that adapters depend inward, never outward.

### Dependency Injection via Structs

The project uses dependency structs rather than long constructor parameter lists:

```go
// internal/usecase/agent.go
type AgentDeps struct {
    LLM            domain.LLMProvider
    Memory         domain.MemoryProvider
    Tools          ToolExecutor
    ContextBuilder *ContextBuilder
    Logger         *slog.Logger
    MaxIterations  int
    AuditLogger    domain.AuditLogger   // optional, nil = no audit
    Compressor     *Compressor          // optional, nil = no compression
    Bus            domain.EventBus      // optional, nil = no events
    Approver       domain.ToolApprover  // optional, nil = no approval gating
    Identity       domain.AgentIdentity // optional, for multi-agent mode
}

func NewAgent(deps AgentDeps) *Agent {
    if deps.Identity.MaxIter > 0 {
        deps.MaxIterations = deps.Identity.MaxIter
    }
    if deps.MaxIterations <= 0 {
        deps.MaxIterations = 10
    }
    return &Agent{deps: deps}
}
```

Optional dependencies are simply nil-checked at usage sites. This eliminates the need for builder patterns or complex option chains for internal types.

**When to apply:** When a type has 5+ dependencies, especially when some are optional. The struct makes the dependency graph explicit and self-documenting.

### Registry Pattern

Thread-safe registries manage named resources throughout the codebase:

```go
// internal/adapter/tool/registry.go
type Registry struct {
    mu    sync.RWMutex
    tools map[string]domain.Tool
}

func (r *Registry) Register(t domain.Tool) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    name := t.Name()
    if _, exists := r.tools[name]; exists {
        return fmt.Errorf("tool %q already registered", name)
    }
    r.tools[name] = t
    return nil
}

func (r *Registry) Get(name string) (domain.Tool, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    t, ok := r.tools[name]
    if !ok {
        return nil, domain.NewDomainError("Registry.Get", domain.ErrToolNotFound, name)
    }
    return t, nil
}
```

The same pattern appears for `llm.Registry` (LLM providers), `multiagent.Registry` (agent instances), and `tool.ChannelRegistry` (channels). All use `sync.RWMutex` for concurrent read access with exclusive write.

**When to apply:** Any system with named, dynamically-registered components that may be looked up concurrently.

---

## Interface Design

### Thin Interfaces with Capability Extension

Interfaces are kept small. Extended capabilities are expressed through interface embedding and type assertions:

```go
// internal/domain/provider.go — base interface
type LLMProvider interface {
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    Name() string
}

// Extended capability — optional
type StreamingLLMProvider interface {
    LLMProvider
    ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamDelta, error)
}
```

Callers check for the extended capability at runtime:

```go
// internal/adapter/llm/failover.go
func (f *FailoverProvider) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamDelta, error) {
    if sp, ok := f.primary.(domain.StreamingLLMProvider); ok {
        ch, err := sp.ChatStream(ctx, req)
        if err == nil {
            return ch, nil
        }
    }
    // ... try fallbacks
}
```

**When to apply:** When not all implementations support the full capability set. This avoids forcing stub methods on simpler implementations.

### Compile-Time Interface Assertions

The codebase uses blank var declarations to verify interface compliance at compile time:

```go
// internal/adapter/llm/failover.go
var (
    _ domain.LLMProvider          = (*FailoverProvider)(nil)
    _ domain.StreamingLLMProvider = (*FailoverProvider)(nil)
)

// internal/plugin/manager.go
var _ domain.PluginManager = (*Manager)(nil)
```

**When to apply:** Always, when a concrete type is intended to satisfy an interface. Catches drift between interface and implementation during compilation rather than at runtime.

### Scoped Executor (Decorator Pattern)

Per-agent tool access is restricted through a decorator that filters available tools:

```go
// internal/usecase/scoped_tools.go
func NewScopedToolExecutor(inner ToolExecutor, allowedTools []string) ToolExecutor {
    if len(allowedTools) == 0 {
        return inner // zero overhead pass-through
    }
    allowed := make(map[string]bool, len(allowedTools))
    for _, name := range allowedTools {
        allowed[name] = true
    }
    return &scopedToolExecutor{inner: inner, allowed: allowed}
}

func (s *scopedToolExecutor) Get(name string) (domain.Tool, error) {
    if !s.allowed[name] {
        return nil, domain.ErrToolNotFound
    }
    return s.inner.Get(name)
}
```

When `allowedTools` is empty, it returns the inner executor directly — zero allocation, zero overhead.

**When to apply:** Multi-tenant or multi-agent systems where different consumers need different views of the same resource set.

---

## Error Handling

### Sentinel Errors with Phased Organization

Domain errors are defined as package-level sentinels, grouped by feature phase:

```go
// internal/domain/errors.go
var (
    ErrProviderNotFound   = fmt.Errorf("llm provider not found")
    ErrToolNotFound       = fmt.Errorf("tool not found")
    ErrMaxIterations      = fmt.Errorf("agent reached max iterations")
    ErrPathOutsideSandbox = fmt.Errorf("path is outside sandbox boundary")
    ErrSSRFBlocked        = fmt.Errorf("request to private/reserved IP blocked")

    // Gateway / RPC errors (Phase 2).
    ErrGatewayAuthFailed = fmt.Errorf("gateway authentication failed")

    // Multi-agent errors (Phase 3).
    ErrAgentNotFound  = fmt.Errorf("agent not found")
    ErrAgentDuplicate = fmt.Errorf("agent already registered")

    // Node system errors (Phase 5).
    ErrNodeNotFound    = fmt.Errorf("node not found")
    ErrNodeUnreachable = fmt.Errorf("node unreachable")
)
```

Phase comments document the development history and make it clear when each error was introduced.

### Structured Domain Errors

A `DomainError` type wraps sentinel errors with operation context:

```go
// internal/domain/errors.go
type DomainError struct {
    Op     string // operation name (e.g., "Tool.Execute")
    Err    error  // underlying sentinel or wrapped error
    Detail string // human-readable detail
}

func (e *DomainError) Error() string {
    if e.Detail != "" {
        return fmt.Sprintf("%s: %s: %s", e.Op, e.Detail, e.Err)
    }
    return fmt.Sprintf("%s: %s", e.Op, e.Err)
}

func (e *DomainError) Unwrap() error { return e.Err }
```

Used throughout the codebase for contextual error reporting:

```go
// internal/security/sandbox.go
return "", domain.NewDomainError("Sandbox.ValidatePath", domain.ErrPathOutsideSandbox,
    fmt.Sprintf("resolved %q is outside root %q", resolved, s.root))
```

This allows callers to use `errors.Is(err, domain.ErrPathOutsideSandbox)` while still getting rich error messages.

**When to apply:** Any project where errors cross layer boundaries. The Op field identifies exactly where the error originated.

### Non-Fatal Error Degradation

Memory query failures and hook errors are treated as non-fatal — the system degrades gracefully:

```go
// internal/usecase/agent.go — memory failure is non-fatal
memories, err = a.deps.Memory.Query(memCtx, userMsg, 5)
if err != nil {
    a.deps.Logger.Warn("memory query failed", "error", err)
    // Non-fatal: continue without memory context
}

// internal/usecase/router.go — hook errors are non-fatal
for _, h := range r.hooks {
    if err := h.OnMessageReceived(ctx, msg); err != nil {
        r.logger.Warn("hook OnMessageReceived error", "error", err)
        // Continue — hook errors are non-fatal.
    }
}
```

**When to apply:** When an auxiliary subsystem failing should not block the primary user-facing flow.

---

## Configuration & Environment

### Defaults-First Configuration Loading

Configuration starts from sensible defaults, then layers overrides:

```go
// internal/infra/config/config.go
func Load(path string) (*Config, error) {
    cfg := Defaults()        // 1. Start with defaults

    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            ApplyEnvOverrides(cfg)  // 2. Apply env overrides
            if err := Validate(cfg); err != nil { return nil, err }
            return cfg, nil
        }
        return nil, fmt.Errorf("read config: %w", err)
    }

    yaml.Unmarshal(data, cfg)    // 3. Unmarshal YAML over defaults
    ApplyEnvOverrides(cfg)       // 4. Env vars take highest precedence

    passphrase := os.Getenv("ALFREDAI_CONFIG_KEY")
    if passphrase != "" {
        decryptSecrets(cfg, passphrase)  // 5. Decrypt secrets
    }

    Validate(cfg)                // 6. Validate final state
    return cfg, nil
}
```

The precedence chain: defaults < YAML file < environment variables. This means every config value has a working default and can be overridden without touching files.

**When to apply:** Any application with multiple configuration sources. The defaults-first approach means the app runs with zero configuration.

### Accumulative Validation

The config validator collects all errors instead of failing on the first one:

```go
// internal/infra/config/validate.go
type ValidationError struct {
    Errors []string
}

func (v *ValidationError) Error() string {
    return "config validation failed:\n  - " + strings.Join(v.Errors, "\n  - ")
}

func Validate(cfg *Config) error {
    ve := &ValidationError{}
    validateAgent(cfg, ve)
    validateLLM(cfg, ve)
    validateMemory(cfg, ve)
    validateTools(cfg, ve)
    validateChannels(cfg, ve)
    // ...
    if ve.HasErrors() {
        return ve
    }
    return nil // returns nil, not &ValidationError{} with empty errors
}
```

**When to apply:** User-facing config validation. Showing all errors at once is much friendlier than forcing users to fix one error at a time.

### Config-Level Secret Encryption

API keys can be stored encrypted in the YAML file using an `enc:` prefix:

```go
// internal/infra/config/config.go
func decryptSecrets(cfg *Config, passphrase string) error {
    for i := range cfg.LLM.Providers {
        key := cfg.LLM.Providers[i].APIKey
        if strings.HasPrefix(key, "enc:") {
            decrypted, err := DecryptValue(strings.TrimPrefix(key, "enc:"), passphrase)
            if err != nil {
                return fmt.Errorf("provider %s api_key: %w", cfg.LLM.Providers[i].Name, err)
            }
            cfg.LLM.Providers[i].APIKey = decrypted
        }
    }
    // ... same for channel tokens, gateway tokens, embedding keys
    return nil
}
```

Uses AES-256-GCM with Argon2id key derivation. The passphrase is read from `ALFREDAI_CONFIG_KEY` env var, never from the config file.

**When to apply:** Any project that stores secrets in config files. The `enc:` prefix convention makes it obvious which values are encrypted.

---

## Concurrency Patterns

### Event Bus with Panic Recovery

The event bus dispatches each handler in its own goroutine with recovery:

```go
// internal/usecase/eventbus/bus.go
func (b *Bus) dispatch(ctx context.Context, event domain.Event, sub subscription) {
    b.wg.Add(1)
    go func() {
        defer b.wg.Done()
        defer func() {
            if r := recover(); r != nil {
                b.logger.Error("event handler panicked",
                    "event", string(event.Type),
                    "panic", r,
                )
            }
        }()
        sub.handler(ctx, event)
    }()
}

func (b *Bus) Close() {
    if b.closed.Swap(true) {
        return // idempotent
    }
    b.wg.Wait()
}
```

Key details: `atomic.Bool` for closed state prevents races, `sync.WaitGroup` ensures all in-flight handlers complete before Close returns, subscriber lists are copied under read lock before dispatch to avoid holding the lock during handler execution.

**When to apply:** In-process event systems where handler isolation matters. The per-handler goroutine + recovery pattern prevents one bad handler from taking down the bus.

### Graceful Multi-Channel Shutdown

Multiple channels start in parallel goroutines and shut down via context cancellation:

```go
// cmd/agent/main.go
var wg sync.WaitGroup
errCh := make(chan error, len(runtime.Channels))

for _, ch := range runtime.Channels {
    wg.Add(1)
    go func(c domain.Channel) {
        defer wg.Done()
        if err := c.Start(ctx, handler(c.Send)); err != nil {
            errCh <- fmt.Errorf("channel %s: %w", c.Name(), err)
        }
    }(ch)
}

<-ctx.Done()
wg.Wait()
```

Signal handling uses `signal.NotifyContext` for clean cancellation:

```go
ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
defer cancel()
```

**When to apply:** Any server with multiple concurrent listeners that need coordinated shutdown.

### Thread-Safe Session with Read-Write Lock

Sessions use `sync.RWMutex` with defensive copying for reads:

```go
// internal/usecase/session.go
func (s *Session) Messages() []domain.Message {
    s.mu.RLock()
    defer s.mu.RUnlock()
    cp := make([]domain.Message, len(s.Msgs))
    copy(cp, s.Msgs)
    return cp
}

func (s *Session) AddMessage(msg domain.Message) {
    s.mu.Lock()
    defer s.mu.Unlock()
    if msg.Timestamp.IsZero() {
        msg.Timestamp = time.Now()
    }
    s.Msgs = append(s.Msgs, msg)
    s.UpdatedAt = time.Now()
}
```

The `Messages()` method returns a copy to prevent data races when callers iterate while other goroutines write.

### Two-Phase Session Reaping

Stale session cleanup separates identification from deletion to minimize lock contention:

```go
// internal/usecase/session.go
func (sm *SessionManager) ReapStaleSessions(maxAge time.Duration) int {
    cutoff := time.Now().Add(-maxAge)

    // Phase 1: identify stale sessions under read lock
    sm.mu.RLock()
    var staleIDs []string
    for id, s := range sm.sessions {
        s.mu.RLock()
        stale := s.UpdatedAt.Before(cutoff)
        s.mu.RUnlock()
        if stale {
            staleIDs = append(staleIDs, id)
        }
    }
    sm.mu.RUnlock()

    // Phase 2: delete under write lock
    sm.mu.Lock()
    for _, id := range staleIDs {
        delete(sm.sessions, id)
    }
    sm.mu.Unlock()

    // Phase 3: clean up disk files (no lock needed)
    for _, id := range staleIDs {
        path := filepath.Join(sm.dataDir, id+".json")
        os.Remove(path)
    }
    return len(staleIDs)
}
```

**When to apply:** Any cache/store cleanup where reads are frequent but eviction is rare.

### Ring Buffer for Bounded Output Capture

A thread-safe ring buffer captures process output without unbounded growth:

```go
// internal/usecase/ringbuffer.go
type ringBuffer struct {
    mu      sync.Mutex
    data    []byte
    max     int
    written int64 // total bytes ever written (including dropped)
}

func (rb *ringBuffer) Write(p []byte) (int, error) {
    rb.mu.Lock()
    defer rb.mu.Unlock()
    rb.data = append(rb.data, p...)
    rb.written += int64(len(p))
    if len(rb.data) > rb.max {
        rb.data = rb.data[len(rb.data)-rb.max:]
    }
    return len(p), nil
}
```

The `written` counter tracks total bytes for offset-based incremental reads, even after data has been dropped.

**When to apply:** Capturing output from long-running processes where only the most recent data matters.

---

## Testing Patterns

### Hand-Written Mocks in Test Files

The project uses hand-written mocks co-located with tests, not generated code:

```go
// internal/usecase/usecase_test.go
type mockLLM struct {
    mu        sync.Mutex
    responses []domain.ChatResponse
    callIdx   int
}

func (m *mockLLM) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    if m.callIdx >= len(m.responses) {
        return &domain.ChatResponse{
            Message: domain.Message{Role: domain.RoleAssistant, Content: "fallback"},
        }, nil
    }
    m.callIdx++
    return new(m.responses[m.callIdx]), nil
}

func (m *mockLLM) Name() string { return "mock" }
```

Mocks are designed with pre-programmed responses (indexed by call order) and are fully thread-safe. The same pattern is used for `mockMemory`, `mockToolExecutor`, `staticTool`, `errorTool`.

**When to apply:** When mock behavior is simple and predictable. Hand-written mocks are more readable and maintainable than generated ones for small interfaces.

### Mock Composition via Embedding

Mocks can selectively override methods by embedding a base mock:

```go
// internal/usecase/usecase_test.go
type errorQueryMemoryUsecase struct {
    mockMemory // embed base mock
}

func (m *errorQueryMemoryUsecase) Query(_ context.Context, _ string, _ int) ([]domain.MemoryEntry, error) {
    return nil, fmt.Errorf("memory query error")
}
func (m *errorQueryMemoryUsecase) IsAvailable() bool { return true }
```

This avoids duplicating all mock methods when only one needs to change.

### Benchmark Tests

Performance-critical paths have dedicated benchmarks:

```go
// internal/usecase/benchmark_test.go
func BenchmarkSessionManagerConcurrent(b *testing.B) {
    mgr := NewSessionManager(b.TempDir())
    b.ResetTimer()
    b.ReportAllocs()

    b.RunParallel(func(pb *testing.PB) {
        i := 0
        for pb.Next() {
            sessionID := fmt.Sprintf("session-%d", i%10)
            session := mgr.GetOrCreate(sessionID)
            session.AddMessage(domain.Message{
                Role:    domain.RoleUser,
                Content: "Concurrent message",
            })
            i++
        }
    })
}
```

**When to apply:** For hot paths (session management, context building, tool lookup). The `b.RunParallel` pattern is especially important for testing concurrent data structures.

### Fuzz Testing for Security Boundaries

Security-sensitive tools have fuzz tests:

```
internal/adapter/tool/shell_fuzz_test.go
internal/adapter/tool/filesystem_fuzz_test.go
internal/adapter/tool/web_fuzz_test.go
internal/adapter/tool/web_search_fuzz_test.go
internal/adapter/tool/delegate_fuzz_test.go
internal/adapter/tool/browser_fuzz_test.go
internal/adapter/tool/node_invoke_fuzz_test.go
internal/adapter/tool/node_list_fuzz_test.go
internal/adapter/tool/subagent_fuzz_test.go
```

With Makefile targets for different fuzz durations:

```makefile
fuzz-short: ## Quick fuzz regression (10s per test)
    go test -fuzz=FuzzShellTool -fuzztime=10s ./internal/adapter/tool
    go test -fuzz=FuzzFilesystemTool -fuzztime=10s ./internal/adapter/tool
    go test -fuzz=FuzzWebTool -fuzztime=10s ./internal/adapter/tool
```

**When to apply:** Any code that processes untrusted input (tool parameters from LLM, user-supplied paths, URLs).

### Integration Test Helpers with Build Tags

Integration tests use build tags and helper functions:

```go
// internal/integration/testing.go
func SkipIfNoAPIKey(t *testing.T, key, name string) {
    t.Helper()
    if key == "" {
        t.Skipf("Skipping %s integration test: %s_API_KEY not set", name, name)
    }
}

func NewTestContext(t *testing.T, timeout time.Duration) context.Context {
    t.Helper()
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    t.Cleanup(cancel)
    return ctx
}
```

```makefile
test-integration-llm: ## Run LLM provider integration tests
    @test -n "$$OPENAI_API_KEY" || (echo "OPENAI_API_KEY not set" && exit 1)
    go test -v -tags=integration -run TestOpenAI ./internal/adapter/llm/
```

**When to apply:** Tests that require external services. The `t.Helper()` + `t.Skipf()` pattern ensures clean CI output without failures when credentials aren't available.

---

## Logging & Observability

### Structured Logging with slog

The project uses Go's standard `log/slog` throughout:

```go
// internal/infra/logger/logger.go
func New(cfg config.LoggerConfig) (*slog.Logger, func() error, error) {
    writer, closer, err := openOutput(cfg.Output)
    if err != nil {
        return nil, nil, fmt.Errorf("open log output: %w", err)
    }
    level := parseLevel(cfg.Level)
    opts := &slog.HandlerOptions{Level: level}

    var handler slog.Handler
    switch strings.ToLower(cfg.Format) {
    case "json":
        handler = slog.NewJSONHandler(writer, opts)
    default:
        handler = slog.NewTextHandler(writer, opts)
    }
    return slog.New(handler), closer, nil
}
```

Logger creation returns a closer function for file-based output, following the cleanup pattern used throughout the codebase.

**When to apply:** All Go projects should prefer `slog` over third-party loggers. The stdlib is sufficient and avoids dependency churn.

### OpenTelemetry Tracing with Convenience Helpers

Tracing wraps OpenTelemetry with thin helpers for ergonomic usage:

```go
// internal/infra/tracer/tracer.go
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
    return otel.Tracer(tracerName).Start(ctx, name, opts...)
}

func RecordError(span trace.Span, err error) {
    span.RecordError(err)
    span.SetStatus(codes.Error, err.Error())
}

func SetOK(span trace.Span) {
    span.SetStatus(codes.Ok, "")
}
```

Used throughout the agent loop:

```go
// internal/usecase/agent.go
ctx, span := tracer.StartSpan(ctx, "agent.handle_message")
defer span.End()
// ...
llmCtx, llmSpan := tracer.StartSpan(ctx, "agent.llm_call")
resp, err := a.deps.LLM.Chat(llmCtx, chatReq)
llmSpan.End()
```

When tracing is disabled, a noop provider is installed — zero overhead:

```go
if !cfg.Enabled {
    otel.SetTracerProvider(noop.NewTracerProvider())
    return noopShutdown, nil
}
```

**When to apply:** Any system where request tracing is useful. The noop pattern means you pay nothing when tracing is off.

### Audit Logging for Security Events

Security-sensitive operations are logged to a JSONL audit file:

```go
// internal/usecase/agent.go
if a.deps.AuditLogger != nil {
    a.deps.AuditLogger.Log(ctx, domain.AuditEvent{
        Type: domain.AuditToolExec,
        Detail: map[string]string{
            "tool":    call.Name,
            "success": success,
        },
    })
}
```

The audit logger is optional (nil-checked), keeping the core agent logic clean.

**When to apply:** Any system handling user data or executing operations on behalf of an AI. Audit logs provide accountability and compliance support.

---

## Security Patterns

### Filesystem Sandbox with Symlink Resolution

The sandbox validates paths after resolving symlinks to prevent escape:

```go
// internal/security/sandbox.go
func (s *Sandbox) ValidatePath(requested string) (string, error) {
    abs, err := filepath.Abs(requested)
    if err != nil { /* ... */ }

    resolved, err := filepath.EvalSymlinks(abs)
    if err != nil {
        // Path doesn't exist yet - validate the parent directory
        parent := filepath.Dir(abs)
        resolvedParent, err2 := filepath.EvalSymlinks(parent)
        if err2 != nil { /* ... */ }
        resolved = filepath.Join(resolvedParent, filepath.Base(abs))
    }

    if !s.isWithinRoot(resolved) {
        return "", domain.NewDomainError("Sandbox.ValidatePath", domain.ErrPathOutsideSandbox,
            fmt.Sprintf("resolved %q is outside root %q", resolved, s.root))
    }
    return resolved, nil
}
```

The critical detail: symlinks are evaluated *after* computing the absolute path, preventing symlink-based sandbox escape. For new files that don't exist yet, the parent directory is validated instead.

**When to apply:** Any tool that allows file operations based on user or LLM input.

### SSRF Protection with DNS Rebinding Prevention

URL validation prevents SSRF in two layers: pre-request validation and dial-time verification:

```go
// internal/security/ssrf.go
func NewSSRFSafeTransport() *http.Transport {
    return &http.Transport{
        DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
            host, port, _ := net.SplitHostPort(addr)

            // Resolve DNS once
            ips, _ := net.DefaultResolver.LookupIPAddr(ctx, host)

            // Validate ALL resolved IPs
            for _, ip := range ips {
                if IsPrivateIP(ip.IP) {
                    return nil, domain.NewDomainError(...)
                }
            }

            // Connect directly to first validated IP (no second DNS lookup)
            dialer := &net.Dialer{Timeout: 10 * time.Second}
            return dialer.DialContext(ctx, network,
                net.JoinHostPort(ips[0].IP.String(), port))
        },
    }
}
```

The transport resolves DNS once, validates all IPs, then connects directly to the validated IP — no second DNS lookup. This prevents TOCTOU attacks where DNS changes between validation and connection.

**When to apply:** Any system that makes HTTP requests based on user or LLM-provided URLs.

### Memory Encryption at Serialization Boundary

Content encryption is applied at the serialize/deserialize boundary, not in the domain layer:

```go
// internal/adapter/memory/markdown.go
func (m *MarkdownMemory) renderEntry(entry domain.MemoryEntry) string {
    body := entry.Content
    if m.encryptor != nil {
        encrypted, err := m.encryptor.Encrypt(body)
        if err == nil {
            body = encrypted
        }
    }
    // ... write YAML frontmatter + encrypted body
}
```

The search index sees plaintext previews (built *before* encryption), while the persisted file contains encrypted content. Key material is zeroed on shutdown:

```go
// internal/security/encryption.go
func (e *AESContentEncryptor) Zeroize() {
    e.mu.Lock()
    defer e.mu.Unlock()
    for i := range e.key {
        e.key[i] = 0
    }
}
```

**When to apply:** Privacy-sensitive applications where data at rest must be encrypted but in-memory search needs to work on plaintext.

---

## Go-Specific Idioms

### Functional Options Pattern

The project uses the classic functional options for configurable types:

```go
// pkg/nodesdk/options.go
type Option func(*NodeAgent)

func WithServer(addr string) Option {
    return func(n *NodeAgent) { n.serverAddr = addr }
}

func WithToken(token string) Option {
    return func(n *NodeAgent) { n.deviceToken = token }
}

func WithLogger(logger *slog.Logger) Option {
    return func(n *NodeAgent) { n.logger = logger }
}
```

```go
// Usage
agent := nodesdk.New("my-node", "My Device",
    nodesdk.WithPlatform("linux/arm64"),
    nodesdk.WithServer("bot.example.com:9090"),
)
```

The same pattern is used for `MarkdownOption`, `TelegramOption`, `DiscordOption`, `ShellToolOption`.

**When to apply:** Public APIs where backward-compatible extensibility matters. For internal types, the deps struct pattern (see above) is preferred.

### Build Tags for Optional Features

Heavy or platform-specific dependencies are gated behind build tags:

```go
// cmd/agent/channel_discord.go
//go:build discord

package main

func buildDiscordChannel(cc config.ChannelConfig, log *slog.Logger) (domain.Channel, error) {
    return channel.NewDiscordChannel(cc.DiscordToken, log, opts...), nil
}
```

```go
// cmd/agent/channel_discord_stub.go
//go:build !discord

package main

func buildDiscordChannel(_ config.ChannelConfig, _ *slog.Logger) (domain.Channel, error) {
    return nil, fmt.Errorf("discord channel requires build with -tags discord")
}
```

This pattern is used for: `discord`, `slack`, `grpc_node` (gRPC transport), `mdns` (mDNS discovery), `vector_memory` (vector store + embeddings). The stub files return clear error messages telling the user how to enable the feature.

**When to apply:** Features with heavy C dependencies (CGO), platform-specific code, or optional third-party service integrations.

### Noop/Null Object Implementations

Disabled features use null objects instead of nil checks:

```go
// internal/adapter/memory/noop.go
type NoopMemory struct{}

func NewNoopMemory() *NoopMemory { return &NoopMemory{} }

func (n *NoopMemory) Store(_ context.Context, _ domain.MemoryEntry) error { return nil }
func (n *NoopMemory) Query(_ context.Context, _ string, _ int) ([]domain.MemoryEntry, error) {
    return nil, nil
}
func (n *NoopMemory) Name() string      { return "noop" }
func (n *NoopMemory) IsAvailable() bool { return true }
```

Similarly, `node/discovery_noop.go` and `node/invoker_noop.go` provide null implementations for the node subsystem.

**When to apply:** When a subsystem is optional. The noop pattern eliminates nil checks throughout the calling code.

### Context Values for Cross-Cutting Concerns

Session ID propagation uses typed context keys:

```go
// internal/domain/context.go
type ctxKey string

const sessionCtxKey ctxKey = "session_id"

func ContextWithSessionID(ctx context.Context, sessionID string) context.Context {
    return context.WithValue(ctx, sessionCtxKey, sessionID)
}

func SessionIDFromContext(ctx context.Context) string {
    if v, ok := ctx.Value(sessionCtxKey).(string); ok {
        return v
    }
    return ""
}
```

The typed key (`type ctxKey string`) prevents collisions with other packages that might use the same string key.

**When to apply:** Propagating request-scoped metadata (session IDs, trace IDs, user IDs) through function call chains without threading parameters.

### Constructor Functions with Safe Defaults

All constructors apply safe defaults for zero-value fields:

```go
// internal/usecase/agent.go
func NewAgent(deps AgentDeps) *Agent {
    if deps.Identity.MaxIter > 0 {
        deps.MaxIterations = deps.Identity.MaxIter
    }
    if deps.MaxIterations <= 0 {
        deps.MaxIterations = 10
    }
    return &Agent{deps: deps}
}

// internal/usecase/compressor.go
func NewCompressor(llm domain.LLMProvider, cfg CompressionConfig, logger *slog.Logger) *Compressor {
    if cfg.Threshold <= 0 {
        cfg.Threshold = 30
    }
    if cfg.KeepRecent <= 0 {
        cfg.KeepRecent = 10
    }
    return &Compressor{llm: llm, config: cfg, logger: logger}
}
```

**When to apply:** Always. Zero-value safety prevents subtle bugs from forgotten initialization.

---

## Data Flow & Middleware

### Message Handler Callback Chain

Channels receive a `MessageHandler` callback during startup, creating a clean data flow:

```go
// cmd/agent/main.go
handler := func(sendFn func(context.Context, domain.OutboundMessage) error) domain.MessageHandler {
    return func(ctx context.Context, msg domain.InboundMessage) error {
        out, err := runtime.Router.Handle(ctx, msg)
        if err != nil {
            return sendFn(ctx, domain.OutboundMessage{
                SessionID: msg.SessionID,
                Content:   fmt.Sprintf("%v", err),
                IsError:   true,
            })
        }
        return sendFn(ctx, out)
    }
}

// Each channel gets its own send function
ch.Start(ctx, handler(ch.Send))
```

The handler closure captures the channel's `Send` method, creating a per-channel response path. The Router processes the message through hooks, agent, auto-curation, and returns a response.

### Router as Message Pipeline

The Router implements a 10-step pipeline for every message:

```go
// internal/usecase/router.go
func (r *Router) Handle(ctx context.Context, msg domain.InboundMessage) (domain.OutboundMessage, error) {
    // 1. Resolve agent (single or multi-agent mode)
    // 2. Normalize session key
    // 3. Get or create session
    // 4. Invoke OnMessageReceived hooks
    // 5. Publish EventMessageReceived
    // 6. Call agent.HandleMessage
    // 7. Build outbound + onboarding hints
    // 8. Invoke OnResponseReady hooks
    // 9. Publish EventMessageSent
    // 10. Save session + fire-and-forget auto-curate
    return out, nil
}
```

**When to apply:** Systems with multiple cross-cutting concerns (logging, hooks, events, persistence) that apply to every request.

### Fire-and-Forget Background Operations

Post-response operations run asynchronously with timeout and panic recovery:

```go
// internal/usecase/router.go
if r.curator != nil {
    r.wg.Add(1)
    go func() {
        defer r.wg.Done()
        defer func() {
            if rec := recover(); rec != nil {
                r.logger.Error("curator panicked", "panic", rec)
            }
        }()
        curateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
        defer cancel()
        result, curErr := r.curator.CurateConversation(curateCtx, session.Messages())
        // ...
    }()
}
```

The `Router.Wait()` method blocks until all background goroutines complete, called during shutdown.

---

## API Design

### Backend Interface Abstraction for Tools

Every tool delegates to a pluggable backend interface:

```go
// internal/adapter/tool/fs_backend.go (inferred)
type FilesystemBackend interface { /* read, write, list */ }

// internal/adapter/tool/shell_backend.go (inferred)
type ShellBackend interface { /* execute */ }

// internal/adapter/tool/search_backend.go (inferred)
type SearchBackend interface { /* search */ }

// internal/adapter/tool/browser_backend.go (inferred)
type BrowserBackend interface { /* navigate, click, screenshot, ... */ }
```

Factory functions in `cmd/agent/init_agent.go` select the concrete implementation:

```go
func createFilesystemBackend(cfg *config.Config) tool.FilesystemBackend {
    switch cfg.Tools.FilesystemBackend {
    case "local":
        return tool.NewLocalFilesystemBackend()
    default:
        return tool.NewLocalFilesystemBackend()
    }
}
```

**When to apply:** Any tool/adapter that might need alternative implementations (e.g., local vs. remote, real vs. mock).

### LLM Failover Decorator

The failover provider wraps a primary provider with a fallback chain:

```go
// internal/adapter/llm/failover.go
func (f *FailoverProvider) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
    resp, err := f.primary.Chat(ctx, req)
    if err == nil {
        return resp, nil
    }
    f.logger.Warn("primary LLM failed, trying fallbacks",
        "primary", f.primary.Name(), "error", err)

    allErrors := []string{fmt.Sprintf("%s: %v", f.primary.Name(), err)}

    for _, fb := range f.fallbacks {
        resp, err = fb.Chat(ctx, req)
        if err == nil {
            f.logger.Info("failover succeeded", "provider", fb.Name())
            return resp, nil
        }
        allErrors = append(allErrors, fmt.Sprintf("%s: %v", fb.Name(), err))
    }
    return nil, fmt.Errorf("all providers failed: [%s]", joinErrors(allErrors))
}
```

This is a transparent decorator — it implements the same interface as the wrapped provider. The aggregated error message shows all failures for debugging.

**When to apply:** Any system with multiple backends where availability matters more than latency.

---

## Plugin System

### Plugin SDK with Base Types

The public plugin SDK exposes base implementations for easy extension:

```go
// pkg/pluginsdk/sdk.go (inferred from analysis)
type BasePlugin struct { /* default no-op implementations */ }
type BaseHook struct { /* default no-op hook handlers */ }
type BaseChannel struct { /* partial channel implementation */ }
```

External plugins embed these bases and override only what they need. Type aliases in `pluginsdk` re-export domain types without exposing `internal/`:

```go
type Plugin = domain.Plugin
type PluginManifest = domain.PluginManifest
type PluginDeps = domain.PluginDeps
```

### Permission-Based Plugin Validation

Plugins declare permissions in their manifest; the manager validates against allow/deny lists:

```go
// internal/plugin/permissions.go
func ValidatePermissions(manifest domain.PluginManifest, allowed, denied []string) error {
    denySet := make(map[string]bool, len(denied))
    for _, d := range denied { denySet[d] = true }
    allowSet := make(map[string]bool, len(allowed))
    for _, a := range allowed { allowSet[a] = true }

    for _, perm := range manifest.Permissions {
        if denySet[perm] {
            return fmt.Errorf("%w: plugin %q requests denied permission %q",
                domain.ErrPluginPermission, manifest.Name, perm)
        }
        if len(allowSet) > 0 && !allowSet[perm] {
            return fmt.Errorf("%w: plugin %q requests unlisted permission %q",
                domain.ErrPluginPermission, manifest.Name, perm)
        }
    }
    return nil
}
```

**When to apply:** Any extensible system where third-party code runs with the host's privileges.

---

## Potential Improvements

1. **Session ID validation for path traversal**: `SessionManager.validateSessionID` uses both `strings.ContainsAny` and `filepath.Clean` checks. The `filepath.Clean` check alone would suffice and be more robust.

2. **mockLLM response indexing**: In `usecase_test.go`, `mockLLM.Chat` increments `callIdx` *before* using it (`m.callIdx++; return new(m.responses[m.callIdx])`), which skips index 0. This means the first configured response is always skipped.

3. **Backend factory functions**: The `createFilesystemBackend`, `createShellBackend`, etc. all have identical default branches that duplicate the single-case logic. These could simply return the default without a switch statement.

4. **Config file permission check**: `validatePermissions` allows `0644` (world-readable) for a file that may contain API keys. A stricter `0600` requirement would be safer, especially when config-level encryption is not used.

5. **Event bus subscriber copy overhead**: `Publish` copies the entire subscriber slice on every event. For high-frequency events, pre-allocating or using a lock-free structure would reduce GC pressure.

6. **Error type for validation**: `ValidationError` could implement `errors.Unwrap()` to support `errors.As` for programmatic access to individual validation failures.

7. **Sentinel errors use `fmt.Errorf`**: Domain sentinel errors are created with `fmt.Errorf` which allocates a new `*errors.errorString` each time. Using `errors.New` would be more conventional and allocate identically.

---

## Quick Reference

| Pattern | Description | Reference File(s) |
|---|---|---|
| Layered Directory Layout | cmd / domain / usecase / adapter / infra separation | Project root structure |
| Phased Initialization | Typed component structs returned by init functions | `cmd/agent/init_*.go` |
| Domain-Centric Interfaces | All contracts in domain, zero external deps | `internal/domain/*.go` |
| Dependency Injection Structs | Named struct fields for 5+ dependencies | `internal/usecase/agent.go` |
| Registry Pattern | Thread-safe name-to-resource maps | `internal/adapter/tool/registry.go`, `internal/adapter/llm/registry.go` |
| Compile-Time Interface Assertions | `var _ Interface = (*Type)(nil)` | `internal/adapter/llm/failover.go`, `internal/plugin/manager.go` |
| Scoped Executor | Decorator filtering available tools per agent | `internal/usecase/scoped_tools.go` |
| Sentinel Errors | Phase-organized package-level error values | `internal/domain/errors.go` |
| Structured Domain Errors | Op + Err + Detail wrapping | `internal/domain/errors.go` |
| Non-Fatal Degradation | Memory/hook failures logged but not blocking | `internal/usecase/agent.go`, `internal/usecase/router.go` |
| Defaults-First Config | Defaults < YAML < Env vars | `internal/infra/config/config.go` |
| Accumulative Validation | Collect all errors before returning | `internal/infra/config/validate.go` |
| Config Secret Encryption | `enc:` prefix with AES-256-GCM + Argon2id | `internal/infra/config/config.go` |
| Event Bus + Panic Recovery | Per-handler goroutines with recover | `internal/usecase/eventbus/bus.go` |
| Graceful Shutdown | signal.NotifyContext + WaitGroup | `cmd/agent/main.go` |
| Thread-Safe Session | RWMutex + defensive copy on read | `internal/usecase/session.go` |
| Two-Phase Reaping | Read-lock identify, write-lock delete | `internal/usecase/session.go` |
| Ring Buffer | Bounded circular buffer for process output | `internal/usecase/ringbuffer.go` |
| Hand-Written Mocks | Pre-programmed response sequences | `internal/usecase/usecase_test.go` |
| Mock Embedding | Selective method override via struct embedding | `internal/usecase/usecase_test.go` |
| Benchmark Tests | `b.RunParallel` for concurrent benchmarks | `internal/usecase/benchmark_test.go` |
| Fuzz Tests | Fuzzing security-sensitive tool inputs | `internal/adapter/tool/*_fuzz_test.go` |
| Integration Test Helpers | `SkipIfNoAPIKey` + build tags | `internal/integration/testing.go` |
| Structured Logging (slog) | Stdlib logger with JSON/text modes | `internal/infra/logger/logger.go` |
| OpenTelemetry Tracing | Convenience wrappers + noop when disabled | `internal/infra/tracer/tracer.go` |
| Audit Logging | JSONL file for security events | `internal/security/audit.go` |
| Filesystem Sandbox | Symlink-aware path containment | `internal/security/sandbox.go` |
| SSRF Protection | DNS rebinding prevention in HTTP transport | `internal/security/ssrf.go` |
| Encryption at Boundary | Encrypt on write, decrypt on read | `internal/adapter/memory/markdown.go`, `internal/security/encryption.go` |
| Functional Options | `WithXxx` option functions for public APIs | `pkg/nodesdk/options.go`, `internal/adapter/memory/markdown.go` |
| Build Tags | Optional features compiled conditionally | `cmd/agent/channel_discord.go` / `_stub.go` |
| Noop Implementations | Null object pattern for disabled features | `internal/adapter/memory/noop.go` |
| Context Values | Typed keys for session ID propagation | `internal/domain/context.go` |
| Safe Defaults | Zero-value fields get sensible defaults in constructors | `internal/usecase/agent.go`, `internal/usecase/compressor.go` |
| Backend Interfaces | Pluggable implementations for tools | `internal/adapter/tool/*_backend.go` |
| Failover Decorator | Transparent provider chain with error aggregation | `internal/adapter/llm/failover.go` |
| Plugin Permission Validation | Allow/deny lists for plugin capabilities | `internal/plugin/permissions.go` |
| Message Pipeline | Router as 10-step message processing pipeline | `internal/usecase/router.go` |
| Fire-and-Forget | Background goroutines with WaitGroup + panic recovery | `internal/usecase/router.go` |
