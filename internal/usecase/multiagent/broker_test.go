package multiagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/usecase"
)

// --- Test helpers ---

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
	idx := m.callIdx
	m.callIdx++
	return new(m.responses[idx]), nil
}

func (m *mockLLM) Name() string { return "mock" }

type mockMemory struct{}

func (m *mockMemory) Store(_ context.Context, _ domain.MemoryEntry) error { return nil }
func (m *mockMemory) Query(_ context.Context, _ string, _ int) ([]domain.MemoryEntry, error) {
	return nil, nil
}
func (m *mockMemory) Delete(_ context.Context, _ string) error { return nil }
func (m *mockMemory) Curate(_ context.Context, _ []domain.Message) (*domain.CurateResult, error) {
	return &domain.CurateResult{}, nil
}
func (m *mockMemory) Sync(_ context.Context) error { return nil }
func (m *mockMemory) Name() string                 { return "mock" }
func (m *mockMemory) IsAvailable() bool            { return false }

type mockToolExecutor struct{}

func (m *mockToolExecutor) Get(_ string) (domain.Tool, error) { return nil, domain.ErrToolNotFound }
func (m *mockToolExecutor) Schemas() []domain.ToolSchema      { return nil }

type mockEventBus struct {
	mu     sync.Mutex
	events []domain.Event
}

func (b *mockEventBus) Publish(_ context.Context, e domain.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
}
func (b *mockEventBus) Subscribe(_ domain.EventType, _ domain.EventHandler) func() { return func() {} }
func (b *mockEventBus) SubscribeAll(_ domain.EventHandler) func()                  { return func() {} }
func (b *mockEventBus) Close()                                                     {}

func makeAgentInstance(id string, response string) *AgentInstance {
	llm := &mockLLM{
		responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: response}},
		},
	}
	agent := usecase.NewAgent(usecase.AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{},
		ContextBuilder: usecase.NewContextBuilder("You are "+id, "test-model", 50),
		Logger:         slog.Default(),
		MaxIterations:  10,
	})
	return &AgentInstance{
		Identity: domain.AgentIdentity{ID: id, Name: id},
		Agent:    agent,
		Sessions: usecase.NewSessionManager(""),
	}
}

// --- Tests ---

func TestBrokerSuccess(t *testing.T) {
	reg := NewRegistry("main", slog.Default())
	reg.Register(makeAgentInstance("main", "main response"))
	reg.Register(makeAgentInstance("support", "I can help!"))

	broker := NewBroker(reg, nil, slog.Default())
	resp, err := broker.Delegate(context.Background(), DelegateRequest{
		FromAgent: "main",
		ToAgent:   "support",
		SessionID: "sess-1",
		Message:   "help me",
	})
	if err != nil {
		t.Fatalf("Delegate: %v", err)
	}
	if resp.Content != "I can help!" {
		t.Errorf("Content = %q, want %q", resp.Content, "I can help!")
	}
	if resp.FromAgent != "support" {
		t.Errorf("FromAgent = %q, want %q", resp.FromAgent, "support")
	}
}

func TestBrokerTargetNotFound(t *testing.T) {
	reg := NewRegistry("main", slog.Default())
	reg.Register(makeAgentInstance("main", "ok"))

	broker := NewBroker(reg, nil, slog.Default())
	_, err := broker.Delegate(context.Background(), DelegateRequest{
		FromAgent: "main",
		ToAgent:   "nonexistent",
		SessionID: "sess-1",
		Message:   "hello",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent target agent")
	}
}

func TestBrokerSessionIsolation(t *testing.T) {
	reg := NewRegistry("main", slog.Default())
	inst := makeAgentInstance("support", "ok")
	reg.Register(inst)

	broker := NewBroker(reg, nil, slog.Default())

	// Two delegations with different from agents should create different sessions.
	broker.Delegate(context.Background(), DelegateRequest{
		FromAgent: "main", ToAgent: "support", SessionID: "s1", Message: "from main",
	})
	broker.Delegate(context.Background(), DelegateRequest{
		FromAgent: "sales", ToAgent: "support", SessionID: "s1", Message: "from sales",
	})

	sessions := inst.Sessions.ListSessions()
	if len(sessions) != 2 {
		t.Errorf("expected 2 isolated sessions, got %d: %v", len(sessions), sessions)
	}
}

func TestBrokerEventPublished(t *testing.T) {
	reg := NewRegistry("main", slog.Default())
	reg.Register(makeAgentInstance("main", "ok"))
	reg.Register(makeAgentInstance("support", "helping"))

	bus := &mockEventBus{}
	broker := NewBroker(reg, bus, slog.Default())

	broker.Delegate(context.Background(), DelegateRequest{
		FromAgent: "main",
		ToAgent:   "support",
		SessionID: "s1",
		Message:   "delegate this",
	})

	bus.mu.Lock()
	defer bus.mu.Unlock()
	if len(bus.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(bus.events))
	}
	if bus.events[0].Type != domain.EventAgentDelegated {
		t.Errorf("event type = %q, want %q", bus.events[0].Type, domain.EventAgentDelegated)
	}

	var payload DelegateRequest
	if err := json.Unmarshal(bus.events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.FromAgent != "main" || payload.ToAgent != "support" {
		t.Errorf("payload = %+v", payload)
	}
}

func TestBrokerAgentError(t *testing.T) {
	// Create an agent that will hit max iterations (always returns tool calls).
	llm := &mockLLM{
		responses: make([]domain.ChatResponse, 5),
	}
	for i := range llm.responses {
		llm.responses[i] = domain.ChatResponse{
			Message: domain.Message{
				Role: domain.RoleAssistant,
				ToolCalls: []domain.ToolCall{
					{ID: "c", Name: "noop", Arguments: json.RawMessage(`{}`)},
				},
			},
		}
	}
	agent := usecase.NewAgent(usecase.AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{},
		ContextBuilder: usecase.NewContextBuilder("sys", "model", 50),
		Logger:         slog.Default(),
		MaxIterations:  2,
	})

	reg := NewRegistry("err", slog.Default())
	reg.Register(&AgentInstance{
		Identity: domain.AgentIdentity{ID: "err", Name: "Error Agent"},
		Agent:    agent,
		Sessions: usecase.NewSessionManager(""),
	})

	broker := NewBroker(reg, nil, slog.Default())
	resp, err := broker.Delegate(context.Background(), DelegateRequest{
		FromAgent: "main",
		ToAgent:   "err",
		SessionID: "s1",
		Message:   "trigger error",
	})
	if err == nil {
		t.Fatal("expected error from agent failure")
	}
	if resp != nil {
		t.Error("expected nil response on error")
	}
}
