package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"alfred-ai/internal/domain"
)

// mockStreamingLLM implements domain.StreamingLLMProvider with controlled deltas.
type mockStreamingLLM struct {
	mu       sync.Mutex
	streams  [][]domain.StreamDelta // one slice of deltas per ChatStream call
	callIdx  int
	chatResp []domain.ChatResponse // for Chat() fallback
	chatIdx  int
}

func (m *mockStreamingLLM) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.chatIdx >= len(m.chatResp) {
		return &domain.ChatResponse{
			Message: domain.Message{Role: domain.RoleAssistant, Content: "fallback"},
		}, nil
	}
	idx := m.chatIdx
	m.chatIdx++
	return &m.chatResp[idx], nil
}

func (m *mockStreamingLLM) Name() string { return "mock-streaming" }

func (m *mockStreamingLLM) ChatStream(_ context.Context, _ domain.ChatRequest) (<-chan domain.StreamDelta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.callIdx >= len(m.streams) {
		ch := make(chan domain.StreamDelta, 1)
		ch <- domain.StreamDelta{Content: "fallback", Done: true}
		close(ch)
		return ch, nil
	}
	deltas := m.streams[m.callIdx]
	m.callIdx++

	ch := make(chan domain.StreamDelta, len(deltas))
	for _, d := range deltas {
		ch <- d
	}
	close(ch)
	return ch, nil
}

func newStreamAgent(llm domain.LLMProvider, opts ...func(*AgentDeps)) *Agent {
	deps := AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("You are a test assistant.", "test-model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		Bus:            &recordingBus{},
	}
	for _, opt := range opts {
		opt(&deps)
	}
	return NewAgent(deps)
}

func TestHandleMessageStreamSimple(t *testing.T) {
	llm := &mockStreamingLLM{
		streams: [][]domain.StreamDelta{
			{
				{Content: "Hello"},
				{Content: " world"},
				{Content: "!", Done: true, Usage: &domain.Usage{
					PromptTokens: 10, CompletionTokens: 3, TotalTokens: 13,
				}},
			},
		},
	}

	bus := &recordingBus{}
	agent := newStreamAgent(llm, func(d *AgentDeps) { d.Bus = bus })
	session := NewSession("stream-simple")

	result, err := agent.HandleMessageStream(context.Background(), session, "Hi")
	require.NoError(t, err)
	assert.Equal(t, "Hello world!", result)

	// Verify events: StreamStarted, LLMCallStarted, StreamDelta (x3), LLMCallCompleted, StreamCompleted
	events := bus.Events()
	types := make([]domain.EventType, len(events))
	for i, e := range events {
		types[i] = e.Type
	}

	assert.Contains(t, types, domain.EventStreamStarted)
	assert.Contains(t, types, domain.EventStreamDelta)
	assert.Contains(t, types, domain.EventStreamCompleted)
	assert.Contains(t, types, domain.EventLLMCallStarted)
	assert.Contains(t, types, domain.EventLLMCallCompleted)

	// Verify StreamCompleted payload.
	for _, e := range events {
		if e.Type == domain.EventStreamCompleted {
			var payload domain.StreamCompletedPayload
			require.NoError(t, json.Unmarshal(e.Payload, &payload))
			assert.Equal(t, "Hello world!", payload.Content)
			require.NotNil(t, payload.Usage)
			assert.Equal(t, 13, payload.Usage.TotalTokens)
		}
	}
}

func TestHandleMessageStreamFallback(t *testing.T) {
	// Non-streaming LLM — should fall back to HandleMessage + emit StreamCompleted.
	llm := &mockLLM{
		responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "sync response"}},
		},
	}

	bus := &recordingBus{}
	agent := newStreamAgent(llm, func(d *AgentDeps) { d.Bus = bus })
	session := NewSession("stream-fallback")

	result, err := agent.HandleMessageStream(context.Background(), session, "Hi")
	require.NoError(t, err)
	assert.Equal(t, "sync response", result)

	// Should still get StreamCompleted (from the fallback path).
	events := bus.Events()
	hasCompleted := false
	for _, e := range events {
		if e.Type == domain.EventStreamCompleted {
			hasCompleted = true
			var payload domain.StreamCompletedPayload
			require.NoError(t, json.Unmarshal(e.Payload, &payload))
			assert.Equal(t, "sync response", payload.Content)
		}
	}
	assert.True(t, hasCompleted, "expected EventStreamCompleted from fallback path")
}

func TestHandleMessageStreamWithToolCalls(t *testing.T) {
	// LLM streams a tool call, agent executes it, then LLM streams final response.
	toolArgs := json.RawMessage(`{"query":"test"}`)
	llm := &mockStreamingLLM{
		streams: [][]domain.StreamDelta{
			// Iteration 0: tool call
			{
				{ToolCalls: []domain.ToolCall{
					{ID: "call-1", Name: "search", Arguments: toolArgs},
				}, Done: true},
			},
			// Iteration 1: final response after tool execution
			{
				{Content: "Found it!", Done: true},
			},
		},
	}

	searchTool := &capturingTool{name: "search", result: "result-data"}
	agent := newStreamAgent(llm, func(d *AgentDeps) {
		d.Tools = &mockToolExecutor{tools: map[string]domain.Tool{
			"search": searchTool,
		}}
	})
	session := NewSession("stream-tools")

	result, err := agent.HandleMessageStream(context.Background(), session, "search for something")
	require.NoError(t, err)
	assert.Equal(t, "Found it!", result)
	assert.Equal(t, 1, searchTool.CallCount(), "tool should be called once")
}

func TestHandleMessageStreamMultiIteration(t *testing.T) {
	// LLM does two tool calls across two iterations before final response.
	llm := &mockStreamingLLM{
		streams: [][]domain.StreamDelta{
			// Iteration 0: first tool call
			{{ToolCalls: []domain.ToolCall{
				{ID: "c1", Name: "step1", Arguments: json.RawMessage(`{}`)},
			}, Done: true}},
			// Iteration 1: second tool call
			{{ToolCalls: []domain.ToolCall{
				{ID: "c2", Name: "step2", Arguments: json.RawMessage(`{}`)},
			}, Done: true}},
			// Iteration 2: final response
			{{Content: "All done", Done: true}},
		},
	}

	step1 := &capturingTool{name: "step1", result: "ok1"}
	step2 := &capturingTool{name: "step2", result: "ok2"}
	agent := newStreamAgent(llm, func(d *AgentDeps) {
		d.Tools = &mockToolExecutor{tools: map[string]domain.Tool{
			"step1": step1,
			"step2": step2,
		}}
	})
	session := NewSession("stream-multi")

	result, err := agent.HandleMessageStream(context.Background(), session, "do both steps")
	require.NoError(t, err)
	assert.Equal(t, "All done", result)
	assert.Equal(t, 1, step1.CallCount())
	assert.Equal(t, 1, step2.CallCount())
}

func TestHandleMessageStreamMaxIterations(t *testing.T) {
	// LLM always returns a tool call — agent should hit max iterations.
	llm := &mockStreamingLLM{
		streams: func() [][]domain.StreamDelta {
			var s [][]domain.StreamDelta
			for i := 0; i < 15; i++ {
				s = append(s, []domain.StreamDelta{
					{ToolCalls: []domain.ToolCall{
						{ID: "c", Name: "loop", Arguments: json.RawMessage(`{}`)},
					}, Done: true},
				})
			}
			return s
		}(),
	}

	loopTool := &capturingTool{name: "loop", result: "ok"}
	agent := newStreamAgent(llm, func(d *AgentDeps) {
		d.MaxIterations = 3
		d.Tools = &mockToolExecutor{tools: map[string]domain.Tool{
			"loop": loopTool,
		}}
	})
	session := NewSession("stream-max")

	_, err := agent.HandleMessageStream(context.Background(), session, "loop forever")
	assert.ErrorIs(t, err, domain.ErrMaxIterations)
}

func TestHandleMessageStreamContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	llm := &mockStreamingLLM{
		streams: [][]domain.StreamDelta{
			{{Content: "should not appear", Done: true}},
		},
	}

	agent := newStreamAgent(llm)
	session := NewSession("stream-cancel")

	_, err := agent.HandleMessageStream(ctx, session, "Hi")
	assert.Error(t, err)
}

func TestStreamAccumulator(t *testing.T) {
	acc := newStreamAccumulator()

	// Content accumulation.
	acc.addDelta(domain.StreamDelta{Content: "Hello"})
	acc.addDelta(domain.StreamDelta{Content: " world"})

	// Tool call accumulation (fragmented arguments).
	acc.addDelta(domain.StreamDelta{
		ToolCalls: []domain.ToolCall{
			{ID: "c1", Name: "search", Arguments: json.RawMessage(`{"q":`)},
		},
	})
	acc.addDelta(domain.StreamDelta{
		ToolCalls: []domain.ToolCall{
			{Arguments: json.RawMessage(`"test"}`)},
		},
	})

	// Usage (last value wins).
	acc.addDelta(domain.StreamDelta{
		Done:  true,
		Usage: &domain.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	})

	msg, usage := acc.build()

	assert.Equal(t, "Hello world", msg.Content)
	assert.Equal(t, domain.RoleAssistant, msg.Role)
	require.Len(t, msg.ToolCalls, 1)
	assert.Equal(t, "c1", msg.ToolCalls[0].ID)
	assert.Equal(t, "search", msg.ToolCalls[0].Name)
	assert.Equal(t, `{"q":"test"}`, string(msg.ToolCalls[0].Arguments))
	assert.Equal(t, 15, usage.TotalTokens)
}

func TestStreamAccumulatorMultipleToolCalls(t *testing.T) {
	acc := newStreamAccumulator()

	// Two tool calls arriving in same delta.
	acc.addDelta(domain.StreamDelta{
		ToolCalls: []domain.ToolCall{
			{ID: "c1", Name: "search", Arguments: json.RawMessage(`{"q":"a"}`)},
			{ID: "c2", Name: "fetch", Arguments: json.RawMessage(`{"url":"b"}`)},
		},
		Done: true,
	})

	msg, _ := acc.build()
	require.Len(t, msg.ToolCalls, 2)
	assert.Equal(t, "search", msg.ToolCalls[0].Name)
	assert.Equal(t, "fetch", msg.ToolCalls[1].Name)
}

func TestStreamAccumulatorBoundsValidation(t *testing.T) {
	acc := newStreamAccumulator()

	// Build a delta with more tool calls than the bound allows.
	calls := make([]domain.ToolCall, maxToolCallsPerDelta+10)
	for i := range calls {
		calls[i] = domain.ToolCall{
			ID:        fmt.Sprintf("c%d", i),
			Name:      fmt.Sprintf("tool%d", i),
			Arguments: json.RawMessage(`{}`),
		}
	}

	acc.addDelta(domain.StreamDelta{ToolCalls: calls, Done: true})
	msg, _ := acc.build()

	// Only maxToolCallsPerDelta tool calls should be accumulated.
	assert.Len(t, msg.ToolCalls, maxToolCallsPerDelta)
	assert.Equal(t, "tool0", msg.ToolCalls[0].Name)
	assert.Equal(t, fmt.Sprintf("tool%d", maxToolCallsPerDelta-1), msg.ToolCalls[maxToolCallsPerDelta-1].Name)
}

func TestStreamAccumulatorEmptyDeltaWithDone(t *testing.T) {
	acc := newStreamAccumulator()

	// Some content, then an empty delta with Done=true.
	acc.addDelta(domain.StreamDelta{Content: "partial"})
	acc.addDelta(domain.StreamDelta{Done: true})

	msg, _ := acc.build()
	assert.Equal(t, "partial", msg.Content)
	assert.Empty(t, msg.ToolCalls)
	assert.Equal(t, domain.RoleAssistant, msg.Role)
}

func TestStreamAccumulatorOutOfOrderToolCallFragments(t *testing.T) {
	acc := newStreamAccumulator()

	// Fragment 1: ID and Name arrive for tool at index 0.
	acc.addDelta(domain.StreamDelta{
		ToolCalls: []domain.ToolCall{
			{ID: "c1", Name: "search"},
		},
	})

	// Fragment 2: partial arguments for index 0.
	acc.addDelta(domain.StreamDelta{
		ToolCalls: []domain.ToolCall{
			{Arguments: json.RawMessage(`{"query":`)},
		},
	})

	// Fragment 3: remaining arguments for index 0.
	acc.addDelta(domain.StreamDelta{
		ToolCalls: []domain.ToolCall{
			{Arguments: json.RawMessage(`"hello"}`)},
		},
	})

	msg, _ := acc.build()
	require.Len(t, msg.ToolCalls, 1)
	assert.Equal(t, "c1", msg.ToolCalls[0].ID)
	assert.Equal(t, "search", msg.ToolCalls[0].Name)
	assert.Equal(t, `{"query":"hello"}`, string(msg.ToolCalls[0].Arguments))
}

func TestStreamAccumulatorSparseToolCallIndices(t *testing.T) {
	acc := newStreamAccumulator()

	// Delta with tools at indices 0 and 2 (gap at index 1).
	acc.addDelta(domain.StreamDelta{
		ToolCalls: []domain.ToolCall{
			{ID: "c0", Name: "first", Arguments: json.RawMessage(`{}`)},
			{}, // index 1: empty placeholder
			{ID: "c2", Name: "third", Arguments: json.RawMessage(`{}`)},
		},
	})

	msg, _ := acc.build()
	require.Len(t, msg.ToolCalls, 3)
	assert.Equal(t, "first", msg.ToolCalls[0].Name)
	assert.Equal(t, "", msg.ToolCalls[1].Name) // gap remains empty
	assert.Equal(t, "third", msg.ToolCalls[2].Name)
}

func TestStreamAccumulatorUsageLastWins(t *testing.T) {
	acc := newStreamAccumulator()

	// First usage.
	acc.addDelta(domain.StreamDelta{
		Usage: &domain.Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7},
	})

	// Second usage overrides.
	acc.addDelta(domain.StreamDelta{
		Usage: &domain.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	})

	_, usage := acc.build()
	assert.Equal(t, 10, usage.PromptTokens)
	assert.Equal(t, 5, usage.CompletionTokens)
	assert.Equal(t, 15, usage.TotalTokens)
}

func TestStreamAccumulatorContentOnly(t *testing.T) {
	acc := newStreamAccumulator()

	acc.addDelta(domain.StreamDelta{Content: "a"})
	acc.addDelta(domain.StreamDelta{Content: "b"})
	acc.addDelta(domain.StreamDelta{Content: "c", Done: true})

	msg, usage := acc.build()
	assert.Equal(t, "abc", msg.Content)
	assert.Empty(t, msg.ToolCalls)
	assert.Zero(t, usage.TotalTokens)
}
