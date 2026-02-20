package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/usecase"
)

// --- handler test doubles ---

type handlerStubLLM struct{ resp string }

func (s *handlerStubLLM) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	return &domain.ChatResponse{
		Message: domain.Message{Role: domain.RoleAssistant, Content: s.resp, Timestamp: time.Now()},
	}, nil
}
func (s *handlerStubLLM) Name() string { return "handler-stub" }

type handlerStubTools struct{}

func (handlerStubTools) Get(name string) (domain.Tool, error) { return nil, domain.ErrToolNotFound }
func (handlerStubTools) Schemas() []domain.ToolSchema {
	return []domain.ToolSchema{{Name: "echo", Description: "echo tool"}}
}

type handlerStubMemory struct {
	entries []domain.MemoryEntry
}

func (m *handlerStubMemory) Store(_ context.Context, e domain.MemoryEntry) error {
	m.entries = append(m.entries, e)
	return nil
}
func (m *handlerStubMemory) Query(_ context.Context, _ string, _ int) ([]domain.MemoryEntry, error) {
	return m.entries, nil
}
func (m *handlerStubMemory) Delete(_ context.Context, _ string) error { return nil }
func (m *handlerStubMemory) Curate(_ context.Context, _ []domain.Message) (*domain.CurateResult, error) {
	return &domain.CurateResult{}, nil
}
func (m *handlerStubMemory) Sync(_ context.Context) error { return nil }
func (m *handlerStubMemory) Name() string                 { return "stub" }
func (m *handlerStubMemory) IsAvailable() bool            { return false }

type handlerStubPluginMgr struct{}

func (handlerStubPluginMgr) Discover() ([]domain.PluginManifest, error) { return nil, nil }
func (handlerStubPluginMgr) Load(_ domain.Plugin) error                 { return nil }
func (handlerStubPluginMgr) Unload(_ string) error                      { return nil }
func (handlerStubPluginMgr) List() []domain.PluginManifest {
	return []domain.PluginManifest{{Name: "test-plugin", Version: "1.0"}}
}
func (handlerStubPluginMgr) GetHooks() []domain.PluginHook { return nil }

func newHandlerDeps(t *testing.T) HandlerDeps {
	t.Helper()
	logger := slog.Default()
	bus := &testBus{}
	mem := &handlerStubMemory{}

	agent := usecase.NewAgent(usecase.AgentDeps{
		LLM:            &handlerStubLLM{resp: "hello from agent"},
		Memory:         mem,
		Tools:          handlerStubTools{},
		ContextBuilder: usecase.NewContextBuilder("test", "model", 50),
		Logger:         logger,
		MaxIterations:  5,
		Bus:            bus,
	})
	sessions := usecase.NewSessionManager(t.TempDir())
	router := usecase.NewRouter(agent, sessions, bus, logger)

	return HandlerDeps{
		Router:         router,
		Sessions:       sessions,
		Tools:          handlerStubTools{},
		Memory:         mem,
		Plugins:        handlerStubPluginMgr{},
		Bus:            bus,
		Logger:         logger,
		ActiveRequests: &sync.Map{},
	}
}

func callHandler(t *testing.T, h RPCHandler, payload string) (json.RawMessage, error) {
	t.Helper()
	return h(context.Background(), &ClientInfo{Name: "test"}, json.RawMessage(payload))
}

// --- tests ---

func TestHandlerChatSend(t *testing.T) {
	deps := newHandlerDeps(t)
	h := chatSendHandler(deps)

	result, err := callHandler(t, h, `{"session_id":"s1","content":"hi"}`)
	if err != nil {
		t.Fatalf("chatSend: %v", err)
	}

	var out domain.OutboundMessage
	json.Unmarshal(result, &out)
	if !strings.Contains(out.Content, "hello from agent") {
		t.Errorf("Content = %q, want to contain %q", out.Content, "hello from agent")
	}
}

func TestHandlerChatSendInvalidPayload(t *testing.T) {
	deps := newHandlerDeps(t)
	h := chatSendHandler(deps)

	_, err := callHandler(t, h, `invalid json`)
	if err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

func TestHandlerChatSendMissingFields(t *testing.T) {
	deps := newHandlerDeps(t)
	h := chatSendHandler(deps)

	_, err := callHandler(t, h, `{"session_id":"","content":""}`)
	if err == nil {
		t.Fatal("expected error for missing fields")
	}
}

func TestHandlerSessionList(t *testing.T) {
	deps := newHandlerDeps(t)
	// Create a session first.
	deps.Sessions.GetOrCreate("test-session")

	h := sessionListHandler(deps)
	result, err := callHandler(t, h, `null`)
	if err != nil {
		t.Fatalf("sessionList: %v", err)
	}

	var ids []string
	json.Unmarshal(result, &ids)
	if len(ids) != 1 || ids[0] != "test-session" {
		t.Errorf("ids = %v", ids)
	}
}

func TestHandlerSessionGet(t *testing.T) {
	deps := newHandlerDeps(t)
	deps.Sessions.GetOrCreate("s1")

	h := sessionGetHandler(deps)
	result, err := callHandler(t, h, `{"id":"s1"}`)
	if err != nil {
		t.Fatalf("sessionGet: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestHandlerSessionGetNotFound(t *testing.T) {
	deps := newHandlerDeps(t)
	h := sessionGetHandler(deps)

	_, err := callHandler(t, h, `{"id":"nope"}`)
	if err == nil {
		t.Fatal("expected error for missing session")
	}
}

func TestHandlerSessionDelete(t *testing.T) {
	deps := newHandlerDeps(t)
	deps.Sessions.GetOrCreate("del1")

	h := sessionDeleteHandler(deps)
	_, err := callHandler(t, h, `{"id":"del1"}`)
	if err != nil {
		t.Fatalf("sessionDelete: %v", err)
	}
}

func TestHandlerToolList(t *testing.T) {
	deps := newHandlerDeps(t)
	h := toolListHandler(deps)

	result, err := callHandler(t, h, `null`)
	if err != nil {
		t.Fatalf("toolList: %v", err)
	}

	var schemas []domain.ToolSchema
	json.Unmarshal(result, &schemas)
	if len(schemas) != 1 || schemas[0].Name != "echo" {
		t.Errorf("schemas = %v", schemas)
	}
}

func TestHandlerMemoryQuery(t *testing.T) {
	deps := newHandlerDeps(t)
	h := memoryQueryHandler(deps)

	result, err := callHandler(t, h, `{"query":"test","limit":5}`)
	if err != nil {
		t.Fatalf("memoryQuery: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestHandlerMemoryStore(t *testing.T) {
	deps := newHandlerDeps(t)
	h := memoryStoreHandler(deps)

	_, err := callHandler(t, h, `{"content":"test fact","tags":["test"]}`)
	if err != nil {
		t.Fatalf("memoryStore: %v", err)
	}

	mem := deps.Memory.(*handlerStubMemory)
	if len(mem.entries) != 1 {
		t.Errorf("stored %d entries", len(mem.entries))
	}
}

func TestHandlerMemoryDelete(t *testing.T) {
	deps := newHandlerDeps(t)
	h := memoryDeleteHandler(deps)

	_, err := callHandler(t, h, `{"id":"entry-1"}`)
	if err != nil {
		t.Fatalf("memoryDelete: %v", err)
	}
}

func TestHandlerPluginList(t *testing.T) {
	deps := newHandlerDeps(t)
	h := pluginListHandler(deps)

	result, err := callHandler(t, h, `null`)
	if err != nil {
		t.Fatalf("pluginList: %v", err)
	}

	var manifests []domain.PluginManifest
	json.Unmarshal(result, &manifests)
	if len(manifests) != 1 || manifests[0].Name != "test-plugin" {
		t.Errorf("manifests = %v", manifests)
	}
}

func TestHandlerConfigGet(t *testing.T) {
	h := configGetHandler(HandlerDeps{})
	result, err := callHandler(t, h, `null`)
	if err != nil {
		t.Fatalf("configGet: %v", err)
	}

	var cfg sanitizedConfig
	json.Unmarshal(result, &cfg)
	if !cfg.Features.Gateway {
		t.Error("expected gateway feature enabled")
	}
	if cfg.Features.Nodes {
		t.Error("expected nodes feature disabled when NodeManager is nil")
	}
	if cfg.Version != "phase-5" {
		t.Errorf("version = %q, want phase-5", cfg.Version)
	}
}

func TestHandlerToolApprove(t *testing.T) {
	deps := newHandlerDeps(t)
	h := toolApproveHandler(deps)

	_, err := callHandler(t, h, `{"tool_call_id":"c1"}`)
	if err != nil {
		t.Fatalf("toolApprove: %v", err)
	}
}

func TestHandlerToolDeny(t *testing.T) {
	deps := newHandlerDeps(t)
	h := toolDenyHandler(deps)

	_, err := callHandler(t, h, `{"tool_call_id":"c1"}`)
	if err != nil {
		t.Fatalf("toolDeny: %v", err)
	}
}

func TestHandlerChatAbortNoActive(t *testing.T) {
	deps := newHandlerDeps(t)
	h := chatAbortHandler(deps)

	result, err := callHandler(t, h, `{"session_id":"s1"}`)
	if err != nil {
		t.Fatalf("chatAbort: %v", err)
	}

	var resp map[string]bool
	json.Unmarshal(result, &resp)
	if resp["aborted"] {
		t.Error("expected aborted=false when no active request")
	}
}

func TestHandlerChatAbortInvalidPayload(t *testing.T) {
	deps := newHandlerDeps(t)
	h := chatAbortHandler(deps)

	_, err := callHandler(t, h, `invalid json`)
	if err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

func TestHandlerChatAbortEmptySessionID(t *testing.T) {
	deps := newHandlerDeps(t)
	h := chatAbortHandler(deps)

	_, err := callHandler(t, h, `{"session_id":""}`)
	if err == nil {
		t.Fatal("expected error for empty session_id")
	}
}

func TestHandlerChatAbortNilActiveRequests(t *testing.T) {
	deps := newHandlerDeps(t)
	deps.ActiveRequests = nil
	h := chatAbortHandler(deps)

	result, err := callHandler(t, h, `{"session_id":"s1"}`)
	if err != nil {
		t.Fatalf("chatAbort with nil ActiveRequests: %v", err)
	}

	var resp map[string]bool
	json.Unmarshal(result, &resp)
	if resp["aborted"] {
		t.Error("expected aborted=false when ActiveRequests is nil")
	}
}

func TestHandlerChatAbortCancelsContext(t *testing.T) {
	deps := newHandlerDeps(t)

	// Simulate an active request by manually storing a cancel function.
	ctx, cancel := context.WithCancel(context.Background())
	deps.ActiveRequests.Store("active-session", cancel)

	h := chatAbortHandler(deps)
	result, err := callHandler(t, h, `{"session_id":"active-session"}`)
	if err != nil {
		t.Fatalf("chatAbort: %v", err)
	}

	var resp map[string]bool
	json.Unmarshal(result, &resp)
	if !resp["aborted"] {
		t.Error("expected aborted=true for active request")
	}

	// Verify the context was actually cancelled.
	select {
	case <-ctx.Done():
		// ok
	default:
		t.Error("context should have been cancelled")
	}

	// Verify the entry was removed from the map.
	if _, ok := deps.ActiveRequests.Load("active-session"); ok {
		t.Error("active request should have been removed from map")
	}
}
