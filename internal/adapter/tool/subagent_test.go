package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/usecase"
)

type mockSubAgentLLM struct {
	response string
}

func (m *mockSubAgentLLM) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	return &domain.ChatResponse{
		Message: domain.Message{Role: domain.RoleAssistant, Content: m.response},
	}, nil
}

func (m *mockSubAgentLLM) Name() string { return "mock" }

type mockSubAgentErrorLLM struct{}

func (m *mockSubAgentErrorLLM) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	return nil, fmt.Errorf("llm error")
}

func (m *mockSubAgentErrorLLM) Name() string { return "mock-error" }

type mockSubAgentTools struct{}

func (m *mockSubAgentTools) Get(name string) (domain.Tool, error) {
	return nil, domain.ErrToolNotFound
}
func (m *mockSubAgentTools) Schemas() []domain.ToolSchema { return nil }

func TestSubAgentToolSchema(t *testing.T) {
	factory := func() *usecase.Agent {
		return usecase.NewAgent(usecase.AgentDeps{
			LLM:            &mockSubAgentLLM{response: "ok"},
			Memory:         nil,
			Tools:          &mockSubAgentTools{},
			ContextBuilder: usecase.NewContextBuilder("system", "model", 50),
			Logger:         slog.Default(),
			MaxIterations:  5,
		})
	}

	mgr := usecase.NewSubAgentManager(factory, usecase.SubAgentConfig{
		MaxSubAgents: 3,
		Timeout:      10 * time.Second,
	}, slog.Default())

	tool := NewSubAgentTool(mgr)

	if tool.Name() != "sub_agent" {
		t.Errorf("Name = %q", tool.Name())
	}

	schema := tool.Schema()
	if schema.Name != "sub_agent" {
		t.Errorf("Schema.Name = %q", schema.Name)
	}
}

func TestSubAgentToolExecute(t *testing.T) {
	factory := func() *usecase.Agent {
		return usecase.NewAgent(usecase.AgentDeps{
			LLM:            &mockSubAgentLLM{response: "task completed"},
			Memory:         nil,
			Tools:          &mockSubAgentTools{},
			ContextBuilder: usecase.NewContextBuilder("system", "model", 50),
			Logger:         slog.Default(),
			MaxIterations:  5,
		})
	}

	mgr := usecase.NewSubAgentManager(factory, usecase.SubAgentConfig{
		MaxSubAgents: 5,
		Timeout:      10 * time.Second,
	}, slog.Default())

	tool := NewSubAgentTool(mgr)

	params, _ := json.Marshal(map[string]interface{}{
		"tasks": []string{"do task 1", "do task 2"},
	})

	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	if result.Content == "" {
		t.Error("expected non-empty result")
	}
}

func TestSubAgentToolInvalidParams(t *testing.T) {
	factory := func() *usecase.Agent {
		return usecase.NewAgent(usecase.AgentDeps{
			LLM:            &mockSubAgentLLM{response: "ok"},
			Memory:         nil,
			Tools:          &mockSubAgentTools{},
			ContextBuilder: usecase.NewContextBuilder("system", "model", 50),
			Logger:         slog.Default(),
			MaxIterations:  5,
		})
	}

	mgr := usecase.NewSubAgentManager(factory, usecase.SubAgentConfig{
		MaxSubAgents: 5,
		Timeout:      10 * time.Second,
	}, slog.Default())

	tool := NewSubAgentTool(mgr)

	result, err := tool.Execute(context.Background(), json.RawMessage(`invalid json`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid params")
	}
}

func TestSubAgentToolDescription(t *testing.T) {
	factory := func() *usecase.Agent {
		return usecase.NewAgent(usecase.AgentDeps{
			LLM:            &mockSubAgentLLM{response: "ok"},
			Memory:         nil,
			Tools:          &mockSubAgentTools{},
			ContextBuilder: usecase.NewContextBuilder("system", "model", 50),
			Logger:         slog.Default(),
			MaxIterations:  5,
		})
	}

	mgr := usecase.NewSubAgentManager(factory, usecase.SubAgentConfig{
		MaxSubAgents: 3,
		Timeout:      10 * time.Second,
	}, slog.Default())

	tool := NewSubAgentTool(mgr)
	if tool.Description() == "" {
		t.Error("Description() returned empty string")
	}
}

func TestSubAgentToolTooManyTasks(t *testing.T) {
	factory := func() *usecase.Agent {
		return usecase.NewAgent(usecase.AgentDeps{
			LLM:            &mockSubAgentLLM{response: "ok"},
			Memory:         nil,
			Tools:          &mockSubAgentTools{},
			ContextBuilder: usecase.NewContextBuilder("system", "model", 50),
			Logger:         slog.Default(),
			MaxIterations:  5,
		})
	}

	mgr := usecase.NewSubAgentManager(factory, usecase.SubAgentConfig{
		MaxSubAgents: 5,
		Timeout:      10 * time.Second,
	}, slog.Default())

	tool := NewSubAgentTool(mgr)

	// Create >1000 tasks to trigger the DoS guard
	tasks := make([]string, 1001)
	for i := range tasks {
		tasks[i] = "task"
	}
	params, _ := json.Marshal(map[string]interface{}{"tasks": tasks})

	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for >1000 tasks")
	}
}

func TestSubAgentToolPartialFailure(t *testing.T) {
	// Use an LLM that returns errors to trigger the err != nil path in Execute
	errLLM := &mockSubAgentErrorLLM{}
	factory := func() *usecase.Agent {
		return usecase.NewAgent(usecase.AgentDeps{
			LLM:            errLLM,
			Memory:         nil,
			Tools:          &mockSubAgentTools{},
			ContextBuilder: usecase.NewContextBuilder("system", "model", 50),
			Logger:         slog.Default(),
			MaxIterations:  5,
		})
	}

	mgr := usecase.NewSubAgentManager(factory, usecase.SubAgentConfig{
		MaxSubAgents: 5,
		Timeout:      10 * time.Second,
	}, slog.Default())

	tool := NewSubAgentTool(mgr)
	params, _ := json.Marshal(map[string]interface{}{
		"tasks": []string{"fail task"},
	})

	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Should still return a result (possibly with warning about failures)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestSubAgentToolEmptyTasks(t *testing.T) {
	factory := func() *usecase.Agent {
		return usecase.NewAgent(usecase.AgentDeps{
			LLM:            &mockSubAgentLLM{response: "ok"},
			Memory:         nil,
			Tools:          &mockSubAgentTools{},
			ContextBuilder: usecase.NewContextBuilder("system", "model", 50),
			Logger:         slog.Default(),
			MaxIterations:  5,
		})
	}

	mgr := usecase.NewSubAgentManager(factory, usecase.SubAgentConfig{
		MaxSubAgents: 5,
		Timeout:      10 * time.Second,
	}, slog.Default())

	tool := NewSubAgentTool(mgr)

	params, _ := json.Marshal(map[string]interface{}{"tasks": []string{}})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for empty tasks")
	}
}
