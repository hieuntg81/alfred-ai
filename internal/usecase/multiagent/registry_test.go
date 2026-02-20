package multiagent

import (
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/usecase"
)

func testLogger() *slog.Logger { return slog.Default() }

func makeInstance(id, name string) *AgentInstance {
	return &AgentInstance{
		Identity: domain.AgentIdentity{ID: id, Name: name, Provider: "mock", Model: "test"},
		Agent:    &usecase.Agent{},
		Sessions: usecase.NewSessionManager(""),
	}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry("support", testLogger())
	inst := makeInstance("support", "Support Agent")
	if err := r.Register(inst); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, err := r.Get("support")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Identity.ID != "support" {
		t.Errorf("ID = %q, want %q", got.Identity.ID, "support")
	}
}

func TestRegistryDuplicate(t *testing.T) {
	r := NewRegistry("support", testLogger())
	inst := makeInstance("support", "Support Agent")
	if err := r.Register(inst); err != nil {
		t.Fatalf("Register: %v", err)
	}
	err := r.Register(inst)
	if err != domain.ErrDuplicate {
		t.Errorf("expected ErrDuplicate, got %v", err)
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	r := NewRegistry("support", testLogger())
	_, err := r.Get("nonexistent")
	if err != domain.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRegistryDefault(t *testing.T) {
	r := NewRegistry("main", testLogger())
	r.Register(makeInstance("main", "Main Agent"))
	r.Register(makeInstance("support", "Support Agent"))

	inst, err := r.Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	if inst.Identity.ID != "main" {
		t.Errorf("Default ID = %q, want %q", inst.Identity.ID, "main")
	}
}

func TestRegistryDefaultNotFound(t *testing.T) {
	r := NewRegistry("nonexistent", testLogger())
	_, err := r.Default()
	if err != domain.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry("a", testLogger())
	r.Register(makeInstance("b", "B"))
	r.Register(makeInstance("a", "A"))

	list := r.List()
	if len(list) != 2 {
		t.Fatalf("List length = %d, want 2", len(list))
	}
	// Should be sorted by ID
	if list[0].ID != "a" || list[1].ID != "b" {
		t.Errorf("List order: [%s, %s], want [a, b]", list[0].ID, list[1].ID)
	}
}

func TestRegistryRemove(t *testing.T) {
	r := NewRegistry("support", testLogger())
	r.Register(makeInstance("support", "Support"))

	if err := r.Remove("support"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	_, err := r.Get("support")
	if err != domain.ErrNotFound {
		t.Errorf("after Remove, expected ErrNotFound, got %v", err)
	}
}

func TestRegistryRemoveNotFound(t *testing.T) {
	r := NewRegistry("x", testLogger())
	err := r.Remove("nonexistent")
	if err != domain.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRegistryConcurrent(t *testing.T) {
	r := NewRegistry("agent_0", testLogger())
	var wg sync.WaitGroup

	// Concurrent registrations
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			r.Register(makeInstance(id, id))
		}(fmt.Sprintf("agent_%d", i))
	}

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.List()
		}()
	}

	wg.Wait()

	list := r.List()
	if len(list) == 0 {
		t.Error("expected some agents after concurrent registration")
	}
}
