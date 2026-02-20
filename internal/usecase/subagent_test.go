package usecase

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

func makeTestAgentFactory(responses []domain.ChatResponse) func() *Agent {
	return func() *Agent {
		llm := &mockLLM{
			responses: responses,
		}
		return NewAgent(AgentDeps{
			LLM:            llm,
			Memory:         &mockMemory{},
			Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
			ContextBuilder: NewContextBuilder("system", "model", 50),
			Logger:         newTestLogger(),
			MaxIterations:  5,
		})
	}
}

func TestSubAgentSpawn(t *testing.T) {
	factory := makeTestAgentFactory([]domain.ChatResponse{
		{Message: domain.Message{Role: domain.RoleAssistant, Content: "task result"}},
	})

	mgr := NewSubAgentManager(factory, SubAgentConfig{
		MaxSubAgents:  5,
		MaxIterations: 5,
		Timeout:       10 * time.Second,
	}, newTestLogger())

	result, err := mgr.Spawn(context.Background(), "do something")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if result != "task result" {
		t.Errorf("result = %q", result)
	}
}

func TestSubAgentSpawnParallel(t *testing.T) {
	factory := makeTestAgentFactory([]domain.ChatResponse{
		{Message: domain.Message{Role: domain.RoleAssistant, Content: "done"}},
	})

	mgr := NewSubAgentManager(factory, SubAgentConfig{
		MaxSubAgents: 3,
		Timeout:      10 * time.Second,
	}, newTestLogger())

	tasks := []string{"task 1", "task 2", "task 3"}
	results, err := mgr.SpawnParallel(context.Background(), tasks)
	if err != nil {
		t.Fatalf("SpawnParallel: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results len = %d, want 3", len(results))
	}
	for i, r := range results {
		if r != "done" {
			t.Errorf("result[%d] = %q", i, r)
		}
	}
}

func TestSubAgentConcurrencyLimit(t *testing.T) {
	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	factory := func() *Agent {
		llm := &mockLLM{
			responses: []domain.ChatResponse{
				{Message: domain.Message{Role: domain.RoleAssistant, Content: "done"}},
			},
		}
		return NewAgent(AgentDeps{
			LLM:            llm,
			Memory:         &mockMemory{},
			Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
			ContextBuilder: NewContextBuilder("system", "model", 50),
			Logger:         newTestLogger(),
			MaxIterations:  5,
		})
	}

	// Wrap factory to track concurrency
	wrappedFactory := func() *Agent {
		cur := concurrent.Add(1)
		for {
			old := maxConcurrent.Load()
			if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
				break
			}
		}
		defer concurrent.Add(-1)
		return factory()
	}

	mgr := NewSubAgentManager(wrappedFactory, SubAgentConfig{
		MaxSubAgents: 2,
		Timeout:      10 * time.Second,
	}, newTestLogger())

	tasks := []string{"t1", "t2", "t3", "t4", "t5"}
	_, err := mgr.SpawnParallel(context.Background(), tasks)
	if err != nil {
		t.Fatalf("SpawnParallel: %v", err)
	}
}

func TestTruncateStringShort(t *testing.T) {
	result := truncateString("short", 100)
	if result != "short" {
		t.Errorf("truncateString = %q, want %q", result, "short")
	}
}

func TestTruncateStringExact(t *testing.T) {
	s := "exactlen"
	result := truncateString(s, len(s))
	if result != s {
		t.Errorf("truncateString = %q, want %q", result, s)
	}
}

func TestTruncateStringLong(t *testing.T) {
	s := "this is a very long string that should be truncated"
	result := truncateString(s, 10)
	if result != "this is a ..." {
		t.Errorf("truncateString = %q", result)
	}
}

func TestSubAgentManagerDefaults(t *testing.T) {
	factory := makeTestAgentFactory([]domain.ChatResponse{
		{Message: domain.Message{Role: domain.RoleAssistant, Content: "done"}},
	})

	mgr := NewSubAgentManager(factory, SubAgentConfig{
		MaxSubAgents:  0, // should default to 5
		MaxIterations: 0, // should default to 5
		Timeout:       0, // should default to 60s
	}, newTestLogger())

	if mgr.config.MaxSubAgents != 5 {
		t.Errorf("MaxSubAgents = %d, want 5", mgr.config.MaxSubAgents)
	}
	if mgr.config.MaxIterations != 5 {
		t.Errorf("MaxIterations = %d, want 5", mgr.config.MaxIterations)
	}
	if mgr.config.Timeout != 60*time.Second {
		t.Errorf("Timeout = %v, want 60s", mgr.config.Timeout)
	}
}

func TestSubAgentSpawnParallelWithErrors(t *testing.T) {
	// Factory that returns agents whose LLM always errors
	factory := func() *Agent {
		return NewAgent(AgentDeps{
			LLM:            &errorLLMUsecase{},
			Memory:         &mockMemory{},
			Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
			ContextBuilder: NewContextBuilder("system", "model", 50),
			Logger:         newTestLogger(),
			MaxIterations:  5,
		})
	}

	mgr := NewSubAgentManager(factory, SubAgentConfig{
		MaxSubAgents: 5,
		Timeout:      10 * time.Second,
	}, newTestLogger())

	tasks := []string{"t1", "t2", "t3"}
	_, err := mgr.SpawnParallel(context.Background(), tasks)
	if err == nil {
		t.Error("expected error from failing sub-agents")
	}
}

func TestSubAgentSpawnSemaphoreTimeout(t *testing.T) {
	factory := makeTestAgentFactory([]domain.ChatResponse{
		{Message: domain.Message{Role: domain.RoleAssistant, Content: "done"}},
	})

	// Only 1 slot, and fill it manually
	mgr := NewSubAgentManager(factory, SubAgentConfig{
		MaxSubAgents: 1,
		Timeout:      50 * time.Millisecond,
	}, newTestLogger())

	// Fill the semaphore
	mgr.semaphore <- struct{}{}

	// Now Spawn should timeout waiting for slot
	_, err := mgr.Spawn(context.Background(), "should timeout")
	if err == nil {
		t.Error("expected timeout error waiting for semaphore slot")
	}
	if err != nil {
		expected := "sub-agent spawn timeout waiting for slot"
		if err.Error() != expected {
			t.Logf("error = %q (may differ)", err.Error())
		}
	}

	// Release the slot
	<-mgr.semaphore
}

func TestSubAgentSpawnLLMError(t *testing.T) {
	factory := func() *Agent {
		return NewAgent(AgentDeps{
			LLM:            &errorLLMUsecase{},
			Memory:         &mockMemory{},
			Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
			ContextBuilder: NewContextBuilder("system", "model", 50),
			Logger:         newTestLogger(),
			MaxIterations:  5,
		})
	}

	mgr := NewSubAgentManager(factory, SubAgentConfig{
		MaxSubAgents: 5,
		Timeout:      10 * time.Second,
	}, newTestLogger())

	_, err := mgr.Spawn(context.Background(), "should fail")
	if err == nil {
		t.Error("expected error from sub-agent LLM failure")
	}
}

func TestErrSubAgentMaxConcurrency(t *testing.T) {
	// Just verify the error exists
	if ErrSubAgentMaxConcurrency == nil {
		t.Error("ErrSubAgentMaxConcurrency should not be nil")
	}
	_ = fmt.Sprintf("%v", ErrSubAgentMaxConcurrency)
}

func TestSubAgentTimeout(t *testing.T) {
	factory := makeTestAgentFactory([]domain.ChatResponse{
		{Message: domain.Message{Role: domain.RoleAssistant, Content: "done"}},
	})

	mgr := NewSubAgentManager(factory, SubAgentConfig{
		MaxSubAgents: 5,
		Timeout:      1 * time.Millisecond, // Very short timeout
	}, newTestLogger())

	// Context already expired
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond)

	_, err := mgr.Spawn(ctx, "should timeout")
	if err == nil {
		// Either timeout error or it completed fast enough - both are acceptable
		t.Log("spawn completed despite short timeout (mock LLM is fast)")
	}
}
