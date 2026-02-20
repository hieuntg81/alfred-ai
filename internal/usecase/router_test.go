package usecase

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

// --- router-specific test doubles ---

// recordingBus records published events.
type recordingBus struct {
	mu     sync.Mutex
	events []domain.Event
}

func (b *recordingBus) Publish(_ context.Context, e domain.Event) {
	b.mu.Lock()
	b.events = append(b.events, e)
	b.mu.Unlock()
}
func (b *recordingBus) Subscribe(domain.EventType, domain.EventHandler) func() { return func() {} }
func (b *recordingBus) SubscribeAll(domain.EventHandler) func()                { return func() {} }
func (b *recordingBus) Close()                                                 {}

func (b *recordingBus) Events() []domain.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := make([]domain.Event, len(b.events))
	copy(cp, b.events)
	return cp
}

// spyHook records calls and optionally modifies response.
type spyHook struct {
	msgReceived   int
	responseReady int
	modifyResp    string // if non-empty, replace response
	errOnResponse error
}

func (h *spyHook) OnMessageReceived(_ context.Context, _ domain.InboundMessage) error {
	h.msgReceived++
	return nil
}
func (h *spyHook) OnBeforeToolExec(_ context.Context, _ domain.ToolCall) error { return nil }
func (h *spyHook) OnAfterToolExec(_ context.Context, _ domain.ToolCall, _ *domain.ToolResult) error {
	return nil
}
func (h *spyHook) OnResponseReady(_ context.Context, resp string) (string, error) {
	h.responseReady++
	if h.errOnResponse != nil {
		return resp, h.errOnResponse
	}
	if h.modifyResp != "" {
		return h.modifyResp, nil
	}
	return resp, nil
}

// routerFailingLLM always returns an error.
type routerFailingLLM struct{}

func (routerFailingLLM) Chat(context.Context, domain.ChatRequest) (*domain.ChatResponse, error) {
	return nil, fmt.Errorf("llm unavailable")
}
func (routerFailingLLM) Name() string { return "failing" }

// routerCountingMemory counts Store calls (thread-safe).
type routerCountingMemory struct {
	mu    sync.Mutex
	count int
}

func (m *routerCountingMemory) Store(_ context.Context, _ domain.MemoryEntry) error {
	m.mu.Lock()
	m.count++
	m.mu.Unlock()
	return nil
}
func (m *routerCountingMemory) storeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.count
}
func (m *routerCountingMemory) Query(context.Context, string, int) ([]domain.MemoryEntry, error) {
	return nil, nil
}
func (m *routerCountingMemory) Delete(context.Context, string) error { return nil }
func (m *routerCountingMemory) Curate(context.Context, []domain.Message) (*domain.CurateResult, error) {
	return &domain.CurateResult{}, nil
}
func (m *routerCountingMemory) Sync(context.Context) error { return nil }
func (m *routerCountingMemory) Name() string               { return "counting" }
func (m *routerCountingMemory) IsAvailable() bool          { return true }

// --- helpers ---

func newRouterWithResp(t *testing.T, llmResp string) (*Router, *SessionManager, *recordingBus) {
	t.Helper()
	bus := &recordingBus{}
	agent := NewAgent(AgentDeps{
		LLM:            &mockLLM{responses: []domain.ChatResponse{{Message: domain.Message{Role: domain.RoleAssistant, Content: llmResp}}}},
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("test", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  5,
		Bus:            bus,
	})
	sessions := NewSessionManager(t.TempDir())
	r := NewRouter(agent, sessions, bus, newTestLogger())
	return r, sessions, bus
}

// --- tests ---

func TestRouterSessionKeyNormalization(t *testing.T) {
	r, sm, _ := newRouterWithResp(t, "ok")

	msg := domain.InboundMessage{
		SessionID:   "42",
		Content:     "hello",
		ChannelName: "telegram",
	}

	_, err := r.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	ids := sm.ListSessions()
	found := false
	for _, id := range ids {
		if id == "telegram:42" {
			found = true
		}
	}
	if !found {
		t.Errorf("session key not normalized, got %v", ids)
	}
}

func TestRouterHookInvocation(t *testing.T) {
	r, _, _ := newRouterWithResp(t, "ok")
	hook := &spyHook{}
	r.SetHooks([]domain.PluginHook{hook})

	msg := domain.InboundMessage{
		SessionID:   "1",
		Content:     "hi",
		ChannelName: "cli",
	}

	_, err := r.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if hook.msgReceived != 1 {
		t.Errorf("OnMessageReceived called %d times", hook.msgReceived)
	}
	if hook.responseReady != 1 {
		t.Errorf("OnResponseReady called %d times", hook.responseReady)
	}
}

func TestRouterHookModifiesResponse(t *testing.T) {
	r, _, _ := newRouterWithResp(t, "original")
	hook := &spyHook{modifyResp: "modified"}
	r.SetHooks([]domain.PluginHook{hook})

	msg := domain.InboundMessage{
		SessionID:   "1",
		Content:     "hi",
		ChannelName: "cli",
	}

	out, err := r.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if out.Content != "modified" {
		t.Errorf("Content = %q, want modified", out.Content)
	}
}

func TestRouterHookErrorContinues(t *testing.T) {
	r, _, _ := newRouterWithResp(t, "ok")
	hook := &spyHook{errOnResponse: fmt.Errorf("boom")}
	r.SetHooks([]domain.PluginHook{hook})

	msg := domain.InboundMessage{
		SessionID:   "1",
		Content:     "hi",
		ChannelName: "cli",
	}

	out, err := r.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	// Response includes onboarding welcome on first contact; verify agent
	// response is present despite the hook error.
	if !strings.HasSuffix(out.Content, "ok") {
		t.Errorf("Content = %q, want suffix %q", out.Content, "ok")
	}
}

func TestRouterAgentError(t *testing.T) {
	bus := &recordingBus{}
	agent := NewAgent(AgentDeps{
		LLM:            routerFailingLLM{},
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("test", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  5,
		Bus:            bus,
	})
	sessions := NewSessionManager(t.TempDir())
	r := NewRouter(agent, sessions, bus, newTestLogger())

	msg := domain.InboundMessage{
		SessionID:   "1",
		Content:     "hi",
		ChannelName: "cli",
	}

	_, err := r.Handle(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "agent") {
		t.Errorf("err = %v, want agent-wrapped error", err)
	}
}

func TestRouterEventPublishing(t *testing.T) {
	r, _, bus := newRouterWithResp(t, "ok")

	msg := domain.InboundMessage{
		SessionID:   "1",
		Content:     "hi",
		ChannelName: "cli",
	}

	_, err := r.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	events := bus.Events()
	var types []domain.EventType
	for _, e := range events {
		types = append(types, e.Type)
	}

	hasReceived := false
	hasSent := false
	for _, et := range types {
		if et == domain.EventMessageReceived {
			hasReceived = true
		}
		if et == domain.EventMessageSent {
			hasSent = true
		}
	}
	if !hasReceived {
		t.Error("missing EventMessageReceived")
	}
	if !hasSent {
		t.Error("missing EventMessageSent")
	}
}

func TestRouterAutoCurate(t *testing.T) {
	bus := &recordingBus{}
	mem := &routerCountingMemory{}
	curateLLM := &mockLLM{responses: []domain.ChatResponse{
		{Message: domain.Message{Role: domain.RoleAssistant, Content: "POINT: test fact\nTAGS: test"}},
	}}

	agent := NewAgent(AgentDeps{
		LLM: &mockLLM{responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "ok"}},
		}},
		Memory:         mem,
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("test", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  5,
		Bus:            bus,
	})
	sessions := NewSessionManager(t.TempDir())
	curator := NewCurator(mem, curateLLM, newTestLogger())

	r := NewRouter(agent, sessions, bus, newTestLogger())
	r.SetCurator(curator)

	msg := domain.InboundMessage{
		SessionID:   "1",
		Content:     "remember this fact",
		ChannelName: "cli",
	}

	_, err := r.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// Auto-curate runs in a goroutine; wait a bit.
	time.Sleep(200 * time.Millisecond)

	if mem.storeCount() == 0 {
		t.Error("curator did not store anything")
	}
}

func TestRouterOutboundSessionID(t *testing.T) {
	r, _, _ := newRouterWithResp(t, "ok")

	msg := domain.InboundMessage{
		SessionID:   "42",
		Content:     "hi",
		ChannelName: "telegram",
	}

	out, err := r.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if out.SessionID != "42" {
		t.Errorf("out.SessionID = %q, want 42", out.SessionID)
	}
}

// --- multi-agent router tests ---

// staticAgentRouter always returns a fixed agent ID.
type staticAgentRouter struct {
	agentID string
}

func (r *staticAgentRouter) Route(_ context.Context, _ domain.InboundMessage) (string, error) {
	return r.agentID, nil
}

func newMultiRouterWithAgents(t *testing.T) (*Router, *recordingBus) {
	t.Helper()
	bus := &recordingBus{}

	// Create two agents with different responses.
	makeAgent := func(resp string) (*Agent, *SessionManager) {
		agent := NewAgent(AgentDeps{
			LLM:            &mockLLM{responses: []domain.ChatResponse{{Message: domain.Message{Role: domain.RoleAssistant, Content: resp}}}},
			Memory:         &mockMemory{},
			Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
			ContextBuilder: NewContextBuilder("test", "model", 50),
			Logger:         newTestLogger(),
			MaxIterations:  5,
			Bus:            bus,
		})
		return agent, NewSessionManager(t.TempDir())
	}

	mainAgent, mainSessions := makeAgent("main response")
	supportAgent, supportSessions := makeAgent("support response")

	lookup := func(agentID string) (*Agent, *SessionManager, error) {
		switch agentID {
		case "main":
			return mainAgent, mainSessions, nil
		case "support":
			return supportAgent, supportSessions, nil
		default:
			return nil, nil, domain.ErrNotFound
		}
	}

	return &Router{
		lookup:      lookup,
		agentRouter: &staticAgentRouter{agentID: "main"},
		bus:         bus,
		logger:      newTestLogger(),
	}, bus
}

func TestMultiRouterRoutes(t *testing.T) {
	r, _ := newMultiRouterWithAgents(t)
	r.agentRouter = &staticAgentRouter{agentID: "support"}

	msg := domain.InboundMessage{
		SessionID:   "1",
		Content:     "help",
		ChannelName: "cli",
	}

	out, err := r.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if out.Content != "support response" {
		t.Errorf("Content = %q, want %q", out.Content, "support response")
	}
}

func TestMultiRouterDefaultFallback(t *testing.T) {
	r, _ := newMultiRouterWithAgents(t)

	msg := domain.InboundMessage{
		SessionID:   "1",
		Content:     "hi",
		ChannelName: "cli",
	}

	out, err := r.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if out.Content != "main response" {
		t.Errorf("Content = %q, want %q", out.Content, "main response")
	}
}

func TestMultiRouterHooksWork(t *testing.T) {
	r, _ := newMultiRouterWithAgents(t)
	hook := &spyHook{modifyResp: "hooked"}
	r.SetHooks([]domain.PluginHook{hook})

	msg := domain.InboundMessage{
		SessionID:   "1",
		Content:     "hi",
		ChannelName: "cli",
	}

	out, err := r.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if out.Content != "hooked" {
		t.Errorf("Content = %q, want %q", out.Content, "hooked")
	}
	if hook.msgReceived != 1 || hook.responseReady != 1 {
		t.Errorf("hooks not called correctly: received=%d, ready=%d", hook.msgReceived, hook.responseReady)
	}
}

func TestMultiRouterRoutingEvent(t *testing.T) {
	r, bus := newMultiRouterWithAgents(t)

	msg := domain.InboundMessage{
		SessionID:   "1",
		Content:     "hi",
		ChannelName: "cli",
	}

	_, err := r.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	events := bus.Events()
	hasRouted := false
	for _, e := range events {
		if e.Type == domain.EventAgentRouted {
			hasRouted = true
		}
	}
	if !hasRouted {
		t.Error("missing EventAgentRouted")
	}
}
