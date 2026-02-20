package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"alfred-ai/internal/domain"
)

// --- Benchmark helpers ---

// benchLLM returns a fixed response with no latency. It optionally includes
// a tool call so the agent exercises the tool-dispatch path.
type benchLLM struct {
	mu        sync.Mutex
	responses []domain.ChatResponse
}

func (m *benchLLM) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.responses) == 0 {
		return &domain.ChatResponse{
			Message: domain.Message{Role: domain.RoleAssistant, Content: "ok"},
		}, nil
	}
	resp := m.responses[0]
	if len(m.responses) > 1 {
		m.responses = m.responses[1:]
	}
	return &resp, nil
}
func (m *benchLLM) Name() string { return "bench" }

// noopTool is a zero-cost tool for measuring dispatch overhead.
type noopTool struct{ name string }

func (t *noopTool) Name() string        { return t.name }
func (t *noopTool) Description() string { return "no-op benchmark tool" }
func (t *noopTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{Name: t.name, Description: t.Description()}
}
func (t *noopTool) Execute(_ context.Context, _ json.RawMessage) (*domain.ToolResult, error) {
	return &domain.ToolResult{Content: "done"}, nil
}

// benchToolExecutor wraps a set of tools for benchmarks.
type benchToolExecutor struct {
	tools   map[string]domain.Tool
	schemas []domain.ToolSchema
}

func newBenchToolExecutor(tools ...domain.Tool) *benchToolExecutor {
	m := &benchToolExecutor{tools: make(map[string]domain.Tool)}
	for _, t := range tools {
		m.tools[t.Name()] = t
		m.schemas = append(m.schemas, t.Schema())
	}
	return m
}

func (e *benchToolExecutor) Get(name string) (domain.Tool, error) {
	t, ok := e.tools[name]
	if !ok {
		return nil, domain.ErrToolNotFound
	}
	return t, nil
}
func (e *benchToolExecutor) Schemas() []domain.ToolSchema { return e.schemas }

// benchBus is a no-op event bus for benchmarks.
type benchBus struct{}

func (benchBus) Publish(context.Context, domain.Event)                   {}
func (benchBus) Subscribe(domain.EventType, domain.EventHandler) func() { return func() {} }
func (benchBus) SubscribeAll(domain.EventHandler) func()                { return func() {} }
func (benchBus) Close()                                                 {}

// --- BenchmarkAgentStartup ---
// Measures the time to construct an Agent with all dependencies.

func BenchmarkAgentStartup(b *testing.B) {
	llm := &instantLLM{summary: "ok"}
	mem := &mockMemory{}
	tools := newBenchToolExecutor(&noopTool{name: "noop"})
	cb := NewContextBuilder("You are a helpful assistant.", "bench-model", 50)
	logger := silentLogger()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		NewAgent(AgentDeps{
			LLM:            llm,
			Memory:         mem,
			Tools:          tools,
			ContextBuilder: cb,
			Logger:         logger,
			MaxIterations:  10,
		})
	}
}

// --- BenchmarkAgentChat ---
// Measures Agent.Chat() overhead (no tool calls, instant LLM).
// Parameterised by session history size to reveal scaling behaviour.

func BenchmarkAgentChat(b *testing.B) {
	sizes := []int{0, 10, 50, 100, 500}

	for _, n := range sizes {
		b.Run(fmt.Sprintf("history_%d", n), func(b *testing.B) {
			llm := &instantLLM{summary: "Here's my response."}
			agent := NewAgent(AgentDeps{
				LLM:            llm,
				Memory:         &mockMemory{},
				Tools:          newBenchToolExecutor(),
				ContextBuilder: NewContextBuilder("system", "model", 200),
				Logger:         silentLogger(),
				MaxIterations:  10,
			})
			ctx := context.Background()

			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				session := seedSession(n)
				b.StartTimer()
				agent.HandleMessage(ctx, session, "What is the capital of France?")
			}
		})
	}
}

// --- BenchmarkToolExecutionOverhead ---
// Measures the overhead of tool dispatch within Agent.Chat() (not the tool
// itself). The LLM returns one tool call, then a final text response.

func BenchmarkToolExecutionOverhead(b *testing.B) {
	toolCounts := []int{1, 5, 10}

	for _, tc := range toolCounts {
		b.Run(fmt.Sprintf("tools_%d", tc), func(b *testing.B) {
			// Build tool calls and responses.
			var toolCalls []domain.ToolCall
			for i := 0; i < tc; i++ {
				toolCalls = append(toolCalls, domain.ToolCall{
					ID:        fmt.Sprintf("call_%d", i),
					Name:      "noop",
					Arguments: json.RawMessage(`{}`),
				})
			}

			responses := []domain.ChatResponse{
				{
					Message: domain.Message{
						Role:      domain.RoleAssistant,
						Content:   "",
						ToolCalls: toolCalls,
					},
				},
				{
					Message: domain.Message{Role: domain.RoleAssistant, Content: "Done."},
				},
			}

			tools := newBenchToolExecutor(&noopTool{name: "noop"})
			ctx := context.Background()

			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				llm := &benchLLM{responses: cloneResponses(responses)}
				agent := NewAgent(AgentDeps{
					LLM:            llm,
					Memory:         &mockMemory{},
					Tools:          tools,
					ContextBuilder: NewContextBuilder("system", "model", 50),
					Logger:         silentLogger(),
					MaxIterations:  10,
				})
				session := NewSession("bench")
				b.StartTimer()
				agent.HandleMessage(ctx, session, "run the tools")
			}
		})
	}
}

// --- BenchmarkMessageRouting ---
// Measures Router.Handle() overhead with a mock agent (instant LLM).

func BenchmarkMessageRouting(b *testing.B) {
	llm := &instantLLM{summary: "Routed response."}
	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          newBenchToolExecutor(),
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         silentLogger(),
		MaxIterations:  10,
	})
	sessions := NewSessionManager(b.TempDir())
	bus := benchBus{}
	router := NewRouter(agent, sessions, bus, silentLogger())
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		router.Handle(ctx, domain.InboundMessage{
			SessionID:   "bench-session",
			Content:     "Hello, what's the weather like?",
			ChannelName: "bench",
			SenderName:  "user",
		})
	}
}

// --- BenchmarkConcurrentSessions ---
// Measures throughput of N concurrent sessions hitting the router.

func BenchmarkConcurrentSessions(b *testing.B) {
	concurrencies := []int{1, 10, 50, 100}

	for _, c := range concurrencies {
		b.Run(fmt.Sprintf("concurrent_%d", c), func(b *testing.B) {
			llm := &instantLLM{summary: "response"}
			agent := NewAgent(AgentDeps{
				LLM:            llm,
				Memory:         &mockMemory{},
				Tools:          newBenchToolExecutor(),
				ContextBuilder: NewContextBuilder("system", "model", 50),
				Logger:         silentLogger(),
				MaxIterations:  10,
			})
			sessions := NewSessionManager(b.TempDir())
			bus := benchBus{}
			router := NewRouter(agent, sessions, bus, silentLogger())
			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()

			b.RunParallel(func(pb *testing.PB) {
				id := 0
				for pb.Next() {
					sessionID := fmt.Sprintf("session-%d", id%c)
					id++
					router.Handle(ctx, domain.InboundMessage{
						SessionID:   sessionID,
						Content:     "benchmark message",
						ChannelName: "bench",
						SenderName:  "user",
					})
				}
			})
		})
	}
}

// --- BenchmarkContextBuilder ---
// Measures ContextBuilder.Build overhead with varying memory and tool counts.

func BenchmarkContextBuilder(b *testing.B) {
	memoryCounts := []int{0, 5, 20, 50}

	for _, mc := range memoryCounts {
		b.Run(fmt.Sprintf("memories_%d", mc), func(b *testing.B) {
			cb := NewContextBuilder("You are a helpful AI assistant.", "model", 50)

			history := make([]domain.Message, 20)
			for i := range history {
				role := domain.RoleUser
				if i%2 == 1 {
					role = domain.RoleAssistant
				}
				history[i] = domain.Message{Role: role, Content: fmt.Sprintf("Message %d content.", i)}
			}

			var memories []domain.MemoryEntry
			for i := 0; i < mc; i++ {
				memories = append(memories, domain.MemoryEntry{
					Content: fmt.Sprintf("Memory entry %d about user preferences and conversation history.", i),
					Tags:    []string{"preference", "context"},
				})
			}

			tools := make([]domain.ToolSchema, 5)
			for i := range tools {
				tools[i] = domain.ToolSchema{Name: fmt.Sprintf("tool_%d", i), Description: "A benchmark tool."}
			}

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				cb.Build(history, memories, tools)
			}
		})
	}
}

// --- BenchmarkSessionOperations ---
// Measures session create/lookup/message-append performance.

func BenchmarkSessionOperations(b *testing.B) {
	b.Run("GetOrCreate", func(b *testing.B) {
		mgr := NewSessionManager(b.TempDir())
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			mgr.GetOrCreate(fmt.Sprintf("session-%d", i%100))
		}
	})

	b.Run("AddMessage", func(b *testing.B) {
		session := NewSession("bench")
		msg := domain.Message{Role: domain.RoleUser, Content: "Hello, this is a typical user message for benchmarking purposes."}
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			session.AddMessage(msg)
		}
	})

	b.Run("Messages_Copy", func(b *testing.B) {
		session := seedSession(100)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = session.Messages()
		}
	})
}

// --- helpers ---

func cloneResponses(src []domain.ChatResponse) []domain.ChatResponse {
	dst := make([]domain.ChatResponse, len(src))
	copy(dst, src)
	return dst
}
