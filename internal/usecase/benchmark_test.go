package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

// Benchmark basic operations that are performance-critical

// BenchmarkContextBuilderBuild benchmarks building chat requests
func BenchmarkContextBuilderBuild(b *testing.B) {
	cb := NewContextBuilder("You are a helpful assistant.", "test-model", 100)

	history := make([]domain.Message, 10)
	for i := 0; i < 10; i++ {
		role := domain.RoleUser
		if i%2 == 1 {
			role = domain.RoleAssistant
		}
		history[i] = domain.Message{
			Role:      role,
			Content:   fmt.Sprintf("Message %d", i),
			Timestamp: time.Now(),
		}
	}

	memories := []domain.MemoryEntry{
		{ID: "1", Content: "Relevant memory 1"},
		{ID: "2", Content: "Relevant memory 2"},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = cb.Build(history, memories, nil)
	}
}

// BenchmarkSessionAddMessage benchmarks adding messages to session
func BenchmarkSessionAddMessage(b *testing.B) {
	session := &Session{
		ID:        "bench-session",
		Msgs:      make([]domain.Message, 0, 1000),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	msg := domain.Message{
		Role:      domain.RoleUser,
		Content:   "Benchmark message",
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		session.AddMessage(msg)
		// Reset periodically to avoid unbounded growth
		if i%100 == 99 {
			session.Msgs = session.Msgs[:0]
		}
	}
}

// BenchmarkSessionMessages benchmarks copying message history
func BenchmarkSessionMessages(b *testing.B) {
	session := &Session{
		ID:   "bench-session",
		Msgs: make([]domain.Message, 50),
	}

	// Populate with messages
	for i := 0; i < 50; i++ {
		session.Msgs[i] = domain.Message{
			Role:      domain.RoleUser,
			Content:   fmt.Sprintf("Message %d", i),
			Timestamp: time.Now(),
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = session.Messages()
	}
}

// BenchmarkSessionManagerGetOrCreate benchmarks session lookup/creation
func BenchmarkSessionManagerGetOrCreate(b *testing.B) {
	mgr := NewSessionManager(b.TempDir())

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		sessionID := fmt.Sprintf("session-%d", i%100) // Reuse 100 sessions
		_ = mgr.GetOrCreate(sessionID)
	}
}

// BenchmarkSessionManagerConcurrent benchmarks concurrent session access
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
				Role:      domain.RoleUser,
				Content:   "Concurrent message",
				Timestamp: time.Now(),
			})
			i++
		}
	})
}

// BenchmarkToolExecutorGet benchmarks tool lookup
func BenchmarkToolExecutorGet(b *testing.B) {
	tool := &staticTool{name: "test_tool", result: "result"}
	executor := &mockToolExecutor{
		tools: map[string]domain.Tool{
			"test_tool": tool,
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = executor.Get("test_tool")
	}
}

// BenchmarkToolExecute benchmarks tool execution
func BenchmarkToolExecute(b *testing.B) {
	tool := &staticTool{name: "bench_tool", result: "done"}
	ctx := context.Background()
	params := json.RawMessage(`{"key":"value"}`)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = tool.Execute(ctx, params)
	}
}

// BenchmarkMemoryQuery benchmarks memory provider queries (mocked)
func BenchmarkMemoryQuery(b *testing.B) {
	mem := &mockMemory{
		entries: []domain.MemoryEntry{
			{ID: "1", Content: "Memory 1", Tags: []string{"tag1"}},
			{ID: "2", Content: "Memory 2", Tags: []string{"tag2"}},
			{ID: "3", Content: "Memory 3", Tags: []string{"tag1", "tag2"}},
		},
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = mem.Query(ctx, "test query", 10)
	}
}

// BenchmarkLLMChatMock benchmarks mock LLM chat calls
func BenchmarkLLMChatMock(b *testing.B) {
	llm := &mockLLM{
		responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "Response"}},
		},
	}

	ctx := context.Background()
	req := domain.ChatRequest{
		Model: "test",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Hello"},
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = llm.Chat(ctx, req)
		// Reset call index
		if i%10 == 9 {
			llm.callIdx = 0
		}
	}
}

// BenchmarkAgentThinkActLoop benchmarks the complete agent processing cycle
func BenchmarkAgentThinkActLoop(b *testing.B) {
	llm := &mockLLM{
		responses: []domain.ChatResponse{
			{
				Message: domain.Message{
					Role:    domain.RoleAssistant,
					Content: "Hello! How can I help you today?",
				},
				Usage: domain.Usage{
					PromptTokens:     10,
					CompletionTokens: 8,
					TotalTokens:      18,
				},
			},
		},
	}

	mem := &mockMemory{
		entries: []domain.MemoryEntry{
			{ID: "1", Content: "User prefers concise answers"},
			{ID: "2", Content: "Previous topic: Go programming"},
		},
	}

	tools := &mockToolExecutor{
		tools:   map[string]domain.Tool{},
		schemas: []domain.ToolSchema{},
	}

	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         mem,
		Tools:          tools,
		ContextBuilder: NewContextBuilder("You are a helpful assistant.", "test-model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		session := NewSession(fmt.Sprintf("bench-session-%d", i))
		_, _ = agent.HandleMessage(context.Background(), session, "Hello")
		// Reset LLM call index
		if i%10 == 9 {
			llm.callIdx = 0
		}
	}
}

// BenchmarkAgentThinkActLoopWithToolCall benchmarks agent with tool execution
func BenchmarkAgentThinkActLoopWithToolCall(b *testing.B) {
	llm := &mockLLM{
		responses: []domain.ChatResponse{
			// First response: request tool call
			{
				Message: domain.Message{
					Role: domain.RoleAssistant,
					ToolCalls: []domain.ToolCall{
						{ID: "call_1", Name: "test_tool", Arguments: json.RawMessage(`{"action":"test"}`)},
					},
				},
			},
			// Second response: final answer
			{
				Message: domain.Message{
					Role:    domain.RoleAssistant,
					Content: "Task completed successfully.",
				},
			},
		},
	}

	tools := &mockToolExecutor{
		tools: map[string]domain.Tool{
			"test_tool": &staticTool{name: "test_tool", result: "tool executed"},
		},
		schemas: []domain.ToolSchema{
			{Name: "test_tool", Description: "Test tool"},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          tools,
		ContextBuilder: NewContextBuilder("You are a helpful assistant.", "test-model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		session := NewSession(fmt.Sprintf("bench-session-%d", i))
		_, _ = agent.HandleMessage(context.Background(), session, "Execute the test tool")
		// Reset LLM call index
		if i%2 == 1 {
			llm.callIdx = 0
		}
	}
}

// BenchmarkContextCompression benchmarks context compression performance
func BenchmarkContextCompression(b *testing.B) {
	llm := &mockLLM{
		responses: []domain.ChatResponse{
			{
				Message: domain.Message{
					Role:    domain.RoleAssistant,
					Content: "This is a summary of the previous conversation covering key points and decisions.",
				},
			},
		},
	}

	compressor := NewCompressor(llm, CompressionConfig{
		Threshold:  10,
		KeepRecent: 3,
	}, newTestLogger())

	// Create session with messages exceeding threshold
	session := NewSession("bench-compress")
	for i := 0; i < 15; i++ {
		role := domain.RoleUser
		if i%2 == 1 {
			role = domain.RoleAssistant
		}
		session.AddMessage(domain.Message{
			Role:      role,
			Content:   fmt.Sprintf("Message number %d with some content to compress", i),
			Timestamp: time.Now(),
		})
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Make a copy to avoid modifying original session
		testSession := NewSession("bench-compress-test")
		for _, msg := range session.Messages() {
			testSession.AddMessage(msg)
		}

		_ = compressor.Compress(ctx, testSession)

		// Reset LLM call index
		if i%10 == 9 {
			llm.callIdx = 0
		}
	}
}

// BenchmarkContextCompressionShouldCompress benchmarks compression check
func BenchmarkContextCompressionShouldCompress(b *testing.B) {
	llm := &mockLLM{}
	compressor := NewCompressor(llm, CompressionConfig{
		Threshold:  30,
		KeepRecent: 10,
	}, newTestLogger())

	session := NewSession("bench-check")
	for i := 0; i < 50; i++ {
		session.AddMessage(domain.Message{
			Role:    domain.RoleUser,
			Content: "test message",
		})
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = compressor.ShouldCompress(session)
	}
}
