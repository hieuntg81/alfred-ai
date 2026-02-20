package tool

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/usecase"
	"alfred-ai/internal/usecase/multiagent"
)

// --- test helpers for delegate ---

type delegateMockLLM struct {
	mu        sync.Mutex
	responses []domain.ChatResponse
	callIdx   int
}

func (m *delegateMockLLM) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
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

func (m *delegateMockLLM) Name() string { return "mock" }

type delegateMockMemory struct{}

func (m *delegateMockMemory) Store(_ context.Context, _ domain.MemoryEntry) error { return nil }
func (m *delegateMockMemory) Query(_ context.Context, _ string, _ int) ([]domain.MemoryEntry, error) {
	return nil, nil
}
func (m *delegateMockMemory) Delete(_ context.Context, _ string) error { return nil }
func (m *delegateMockMemory) Curate(_ context.Context, _ []domain.Message) (*domain.CurateResult, error) {
	return &domain.CurateResult{}, nil
}
func (m *delegateMockMemory) Sync(_ context.Context) error { return nil }
func (m *delegateMockMemory) Name() string                 { return "mock" }
func (m *delegateMockMemory) IsAvailable() bool            { return false }

type delegateMockToolExec struct{}

func (m *delegateMockToolExec) Get(_ string) (domain.Tool, error) { return nil, domain.ErrToolNotFound }
func (m *delegateMockToolExec) Schemas() []domain.ToolSchema      { return nil }

func makeDelegateTestAgentInstance(id, response string) *multiagent.AgentInstance {
	llm := &delegateMockLLM{
		responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: response}},
		},
	}
	agent := usecase.NewAgent(usecase.AgentDeps{
		LLM:            llm,
		Memory:         &delegateMockMemory{},
		Tools:          &delegateMockToolExec{},
		ContextBuilder: usecase.NewContextBuilder("sys", "model", 50),
		Logger:         slog.Default(),
		MaxIterations:  10,
	})
	return &multiagent.AgentInstance{
		Identity: domain.AgentIdentity{ID: id, Name: id + " Agent"},
		Agent:    agent,
		Sessions: usecase.NewSessionManager(""),
	}
}

func setupDelegateTool(t *testing.T) (*DelegateTool, *multiagent.Registry) {
	t.Helper()
	reg := multiagent.NewRegistry("main", slog.Default())
	reg.Register(makeDelegateTestAgentInstance("main", "main resp"))
	reg.Register(makeDelegateTestAgentInstance("support", "support resp"))

	broker := multiagent.NewBroker(reg, nil, slog.Default())
	dt := NewDelegateTool(broker, reg, "main")
	return dt, reg
}

// --- Tests ---

func TestDelegateToolDescription(t *testing.T) {
	dt, _ := setupDelegateTool(t)
	desc := dt.Description()
	if desc == "" {
		t.Error("Description() returned empty string")
	}
}

func TestDelegateToolSchema(t *testing.T) {
	dt, _ := setupDelegateTool(t)
	schema := dt.Schema()

	if schema.Name != "delegate" {
		t.Errorf("Name = %q, want %q", schema.Name, "delegate")
	}
	if !strings.Contains(schema.Description, "support") {
		t.Errorf("Description should list available agents: %q", schema.Description)
	}
	// "main" should NOT appear in the list (it's the owner agent).
	if strings.Contains(schema.Description, "main (main Agent)") {
		t.Error("Description should not list the owner agent")
	}
}

func TestDelegateToolExecuteSuccess(t *testing.T) {
	dt, _ := setupDelegateTool(t)
	params := json.RawMessage(`{"agent_id": "support", "message": "help me"}`)

	result, err := dt.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != "support resp" {
		t.Errorf("Content = %q, want %q", result.Content, "support resp")
	}
}

func TestDelegateToolInvalidParams(t *testing.T) {
	dt, _ := setupDelegateTool(t)
	result, err := dt.Execute(context.Background(), json.RawMessage(`not-json`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for invalid JSON")
	}
}

func TestDelegateToolMissingFields(t *testing.T) {
	dt, _ := setupDelegateTool(t)

	// Missing message
	result, err := dt.Execute(context.Background(), json.RawMessage(`{"agent_id": "support"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for missing message")
	}

	// Missing agent_id
	result, err = dt.Execute(context.Background(), json.RawMessage(`{"message": "hi"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for missing agent_id")
	}
}

func TestDelegateToolDelegationError(t *testing.T) {
	dt, _ := setupDelegateTool(t)
	params := json.RawMessage(`{"agent_id": "nonexistent", "message": "hello"}`)

	result, err := dt.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for delegation to unknown agent")
	}
	if !strings.Contains(result.Content, "delegation failed") {
		t.Errorf("Content = %q, want to contain 'delegation failed'", result.Content)
	}
}
