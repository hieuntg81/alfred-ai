package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

// --- Mocks ---

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

type mockMemory struct {
	entries []domain.MemoryEntry
}

func (m *mockMemory) Store(_ context.Context, _ domain.MemoryEntry) error { return nil }
func (m *mockMemory) Query(_ context.Context, _ string, _ int) ([]domain.MemoryEntry, error) {
	return m.entries, nil
}
func (m *mockMemory) Delete(_ context.Context, _ string) error { return nil }
func (m *mockMemory) Curate(_ context.Context, _ []domain.Message) (*domain.CurateResult, error) {
	return &domain.CurateResult{}, nil
}
func (m *mockMemory) Sync(_ context.Context) error { return nil }
func (m *mockMemory) Name() string                 { return "mock" }
func (m *mockMemory) IsAvailable() bool            { return true }

type mockToolExecutor struct {
	tools   map[string]domain.Tool
	schemas []domain.ToolSchema
}

func (m *mockToolExecutor) Get(name string) (domain.Tool, error) {
	t, ok := m.tools[name]
	if !ok {
		return nil, domain.ErrToolNotFound
	}
	return t, nil
}

func (m *mockToolExecutor) Schemas() []domain.ToolSchema { return m.schemas }

type staticTool struct {
	name   string
	result string
}

func (t *staticTool) Name() string        { return t.name }
func (t *staticTool) Description() string { return "static test tool" }
func (t *staticTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{Name: t.name, Description: t.Description()}
}
func (t *staticTool) Execute(_ context.Context, _ json.RawMessage) (*domain.ToolResult, error) {
	return &domain.ToolResult{Content: t.result}, nil
}

type errorTool struct {
	name string
}

func (t *errorTool) Name() string        { return t.name }
func (t *errorTool) Description() string { return "error test tool" }
func (t *errorTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{Name: t.name}
}
func (t *errorTool) Execute(_ context.Context, _ json.RawMessage) (*domain.ToolResult, error) {
	return nil, fmt.Errorf("tool execution failed")
}

// testAuditLogger wraps file audit logging for tests.
type testAuditLogger struct {
	file *os.File
}

func newFileAuditLogger(path string) (*testAuditLogger, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	return &testAuditLogger{file: f}, nil
}

func (a *testAuditLogger) Log(_ context.Context, _ domain.AuditEvent) error { return nil }
func (a *testAuditLogger) Close() error                                     { return a.file.Close() }

// usecaseAuditLogger counts audit log calls.
type usecaseAuditLogger struct {
	logCount int
}

func (a *usecaseAuditLogger) Log(_ context.Context, _ domain.AuditEvent) error {
	a.logCount++
	return nil
}
func (a *usecaseAuditLogger) Close() error { return nil }

func newTestLogger() *slog.Logger { return slog.Default() }

// --- ContextBuilder tests ---

func TestContextBuilderBasic(t *testing.T) {
	cb := NewContextBuilder("You are a test bot.", "test-model", 50)

	history := []domain.Message{
		{Role: domain.RoleUser, Content: "hello"},
	}

	req := cb.Build(history, nil, nil)

	if len(req.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(req.Messages))
	}
	if req.Messages[0].Role != domain.RoleSystem {
		t.Errorf("first message role = %q, want %q", req.Messages[0].Role, domain.RoleSystem)
	}
	if req.Messages[1].Content != "hello" {
		t.Errorf("second message = %q, want %q", req.Messages[1].Content, "hello")
	}
	if req.Model != "test-model" {
		t.Errorf("model = %q, want %q", req.Model, "test-model")
	}
}

func TestContextBuilderWithMemory(t *testing.T) {
	cb := NewContextBuilder("system", "model", 50)

	memories := []domain.MemoryEntry{
		{Content: "user likes Go", Tags: []string{"preference"}},
	}

	req := cb.Build(nil, memories, nil)

	if len(req.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(req.Messages))
	}
	if req.Messages[0].Role != domain.RoleSystem {
		t.Error("expected system message")
	}
	// System prompt should include memory context
	if !contains(req.Messages[0].Content, "Relevant Memory Context") {
		t.Error("system prompt should include memory context")
	}
}

func TestContextBuilderTruncation(t *testing.T) {
	cb := NewContextBuilder("system", "model", 3)

	history := make([]domain.Message, 10)
	for i := range history {
		history[i] = domain.Message{Role: domain.RoleUser, Content: "msg"}
	}

	req := cb.Build(history, nil, nil)
	// 1 system + 3 truncated history
	if len(req.Messages) != 4 {
		t.Errorf("expected 4 messages, got %d", len(req.Messages))
	}
}

// --- Session tests ---

func TestSessionAddMessage(t *testing.T) {
	s := NewSession("test")

	s.AddMessage(domain.Message{Role: domain.RoleUser, Content: "hello"})
	s.AddMessage(domain.Message{Role: domain.RoleAssistant, Content: "hi"})

	msgs := s.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "hello" {
		t.Errorf("first msg = %q", msgs[0].Content)
	}
}

func TestSessionTruncate(t *testing.T) {
	s := NewSession("test")
	for i := 0; i < 10; i++ {
		s.AddMessage(domain.Message{Role: domain.RoleUser, Content: "msg"})
	}

	s.Truncate(3)
	if len(s.Messages()) != 3 {
		t.Errorf("expected 3 messages after truncation, got %d", len(s.Messages()))
	}
}

func TestSessionConcurrency(t *testing.T) {
	s := NewSession("test")
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.AddMessage(domain.Message{Role: domain.RoleUser, Content: "msg"})
		}()
	}

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Messages()
		}()
	}

	wg.Wait()

	if len(s.Messages()) != 100 {
		t.Errorf("expected 100 messages, got %d", len(s.Messages()))
	}
}

func TestSessionManagerPersistence(t *testing.T) {
	dir := t.TempDir()
	sm := NewSessionManager(dir)

	s := sm.GetOrCreate("test-session")
	s.AddMessage(domain.Message{Role: domain.RoleUser, Content: "saved"})

	if err := sm.Save("test-session"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file exists
	path := filepath.Join(dir, "test-session.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("session file not found: %v", err)
	}

	// Load in a new manager
	sm2 := NewSessionManager(dir)
	s2 := sm2.GetOrCreate("test-session")
	msgs := s2.Messages()
	if len(msgs) != 1 || msgs[0].Content != "saved" {
		t.Errorf("loaded messages = %+v", msgs)
	}
}

// --- Agent tests ---

func TestAgentSimpleResponse(t *testing.T) {
	llm := &mockLLM{
		responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "Hello!"}},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
	})

	session := NewSession("test")
	resp, err := agent.HandleMessage(context.Background(), session, "hi")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if resp != "Hello!" {
		t.Errorf("response = %q, want %q", resp, "Hello!")
	}
}

func TestAgentWithToolCall(t *testing.T) {
	llm := &mockLLM{
		responses: []domain.ChatResponse{
			// First response: request tool call
			{
				Message: domain.Message{
					Role: domain.RoleAssistant,
					ToolCalls: []domain.ToolCall{
						{ID: "call_1", Name: "test_tool", Arguments: json.RawMessage(`{}`)},
					},
				},
			},
			// Second response: final text
			{
				Message: domain.Message{
					Role:    domain.RoleAssistant,
					Content: "The tool returned: tool result",
				},
			},
		},
	}

	tools := &mockToolExecutor{
		tools: map[string]domain.Tool{
			"test_tool": &staticTool{name: "test_tool", result: "tool result"},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          tools,
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
	})

	session := NewSession("test")
	resp, err := agent.HandleMessage(context.Background(), session, "use the tool")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if resp != "The tool returned: tool result" {
		t.Errorf("response = %q", resp)
	}
}

func TestAgentMaxIterations(t *testing.T) {
	// LLM always requests tool calls - should hit max iterations
	llm := &mockLLM{
		responses: make([]domain.ChatResponse, 20),
	}
	for i := range llm.responses {
		llm.responses[i] = domain.ChatResponse{
			Message: domain.Message{
				Role: domain.RoleAssistant,
				ToolCalls: []domain.ToolCall{
					{ID: "call", Name: "test_tool", Arguments: json.RawMessage(`{}`)},
				},
			},
		}
	}

	tools := &mockToolExecutor{
		tools: map[string]domain.Tool{
			"test_tool": &staticTool{name: "test_tool", result: "ok"},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          tools,
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  3,
	})

	session := NewSession("test")
	_, err := agent.HandleMessage(context.Background(), session, "loop forever")
	if err != domain.ErrMaxIterations {
		t.Errorf("expected ErrMaxIterations, got %v", err)
	}
}

func TestNewAgentDefaultMaxIterations(t *testing.T) {
	agent := NewAgent(AgentDeps{
		LLM:            &mockLLM{},
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  0, // should default to 10
	})
	if agent.deps.MaxIterations != 10 {
		t.Errorf("MaxIterations = %d, want 10", agent.deps.MaxIterations)
	}
}

func TestIdentityMaxIterOverride(t *testing.T) {
	agent := NewAgent(AgentDeps{
		LLM:            &mockLLM{},
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		Identity:       domain.AgentIdentity{ID: "test", MaxIter: 25},
	})
	if agent.deps.MaxIterations != 25 {
		t.Errorf("MaxIterations = %d, want 25 (from Identity.MaxIter)", agent.deps.MaxIterations)
	}
}

func TestIdentityZeroFallback(t *testing.T) {
	agent := NewAgent(AgentDeps{
		LLM:            &mockLLM{},
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  5,
		Identity:       domain.AgentIdentity{ID: "test", MaxIter: 0}, // zero = no override
	})
	if agent.deps.MaxIterations != 5 {
		t.Errorf("MaxIterations = %d, want 5 (Identity.MaxIter=0 should not override)", agent.deps.MaxIterations)
	}
}

func TestExecuteToolGetError(t *testing.T) {
	agent := NewAgent(AgentDeps{
		LLM:            &mockLLM{},
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}}, // empty registry
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
	})

	call := domain.ToolCall{ID: "call_1", Name: "nonexistent", Arguments: json.RawMessage(`{}`)}
	msg := agent.executeTool(context.Background(), "test-session", call)

	if msg.Role != domain.RoleTool {
		t.Errorf("Role = %q, want %q", msg.Role, domain.RoleTool)
	}
	if msg.Content == "" {
		t.Error("expected error content in message")
	}
}

func TestExecuteToolWithAuditLogger(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	audit, err := newFileAuditLogger(auditPath)
	if err != nil {
		t.Fatalf("audit logger: %v", err)
	}
	defer audit.Close()

	tools := &mockToolExecutor{
		tools: map[string]domain.Tool{
			"test_tool": &staticTool{name: "test_tool", result: "ok"},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:            &mockLLM{},
		Memory:         &mockMemory{},
		Tools:          tools,
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		AuditLogger:    audit,
	})

	call := domain.ToolCall{ID: "call_1", Name: "test_tool", Arguments: json.RawMessage(`{}`)}
	msg := agent.executeTool(context.Background(), "test-session", call)

	if msg.Content != "ok" {
		t.Errorf("Content = %q, want %q", msg.Content, "ok")
	}
}

func TestExecuteToolAuditNil(t *testing.T) {
	tools := &mockToolExecutor{
		tools: map[string]domain.Tool{
			"test_tool": &staticTool{name: "test_tool", result: "ok"},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:            &mockLLM{},
		Memory:         &mockMemory{},
		Tools:          tools,
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		AuditLogger:    nil, // nil audit
	})

	call := domain.ToolCall{ID: "call_1", Name: "test_tool", Arguments: json.RawMessage(`{}`)}
	msg := agent.executeTool(context.Background(), "test-session", call)

	if msg.Content != "ok" {
		t.Errorf("Content = %q, want %q", msg.Content, "ok")
	}
}

func TestExecuteToolError(t *testing.T) {
	tools := &mockToolExecutor{
		tools: map[string]domain.Tool{
			"err_tool": &errorTool{name: "err_tool"},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:            &mockLLM{},
		Memory:         &mockMemory{},
		Tools:          tools,
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
	})

	call := domain.ToolCall{ID: "call_1", Name: "err_tool", Arguments: json.RawMessage(`{}`)}
	msg := agent.executeTool(context.Background(), "test-session", call)

	if msg.Role != domain.RoleTool {
		t.Errorf("Role = %q, want %q", msg.Role, domain.RoleTool)
	}
}

func TestContextBuilderSetSkills(t *testing.T) {
	cb := NewContextBuilder("system", "model", 50)
	skills := []domain.Skill{
		{Name: "summarize", Description: "Summarize text", Tags: []string{"text"}},
		{Name: "translate", Description: "Translate text"},
	}
	cb.SetSkills(skills)

	req := cb.Build(nil, nil, nil)
	content := req.Messages[0].Content
	if !contains(content, "Available Skills") {
		t.Error("system prompt should include skills section")
	}
	if !contains(content, "summarize") {
		t.Error("system prompt should include skill name")
	}
}

func TestContextBuilderSetThinkingBudget(t *testing.T) {
	cb := NewContextBuilder("system", "model", 50)
	cb.SetThinkingBudget(10000)

	req := cb.Build(nil, nil, nil)
	if req.ThinkingBudget != 10000 {
		t.Errorf("ThinkingBudget = %d, want 10000", req.ThinkingBudget)
	}
}

func TestContextBuilderDefaultThinkingBudget(t *testing.T) {
	cb := NewContextBuilder("system", "model", 50)

	req := cb.Build(nil, nil, nil)
	if req.ThinkingBudget != 0 {
		t.Errorf("ThinkingBudget = %d, want 0", req.ThinkingBudget)
	}
}

func TestTruncateHistoryNoTruncation(t *testing.T) {
	cb := NewContextBuilder("system", "model", 50)
	history := []domain.Message{
		{Role: domain.RoleUser, Content: "msg1"},
		{Role: domain.RoleUser, Content: "msg2"},
	}
	result := cb.truncateHistory(history)
	if len(result) != 2 {
		t.Errorf("expected 2 messages, got %d", len(result))
	}
}

func TestTruncateHistoryWithCompressionSummary(t *testing.T) {
	cb := NewContextBuilder("system", "model", 3)

	history := make([]domain.Message, 10)
	history[0] = domain.Message{Role: domain.RoleAssistant, Content: "summary", Name: compressSummaryName}
	for i := 1; i < 10; i++ {
		history[i] = domain.Message{Role: domain.RoleUser, Content: "msg"}
	}

	result := cb.truncateHistory(history)
	// Should preserve the compression summary + last 3
	if result[0].Name != compressSummaryName {
		t.Errorf("first message Name = %q, want %q", result[0].Name, compressSummaryName)
	}
}

func TestTruncateHistoryNormal(t *testing.T) {
	cb := NewContextBuilder("system", "model", 3)

	history := make([]domain.Message, 10)
	for i := range history {
		history[i] = domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("msg%d", i)}
	}

	result := cb.truncateHistory(history)
	if len(result) != 3 {
		t.Errorf("expected 3 messages, got %d", len(result))
	}
	if result[0].Content != "msg7" {
		t.Errorf("first message = %q, want %q", result[0].Content, "msg7")
	}
}

func TestTruncateHistoryZeroMax(t *testing.T) {
	cb := NewContextBuilder("system", "model", 0) // 0 means no truncation

	history := make([]domain.Message, 10)
	for i := range history {
		history[i] = domain.Message{Role: domain.RoleUser, Content: "msg"}
	}

	result := cb.truncateHistory(history)
	if len(result) != 10 {
		t.Errorf("expected 10 messages (no truncation), got %d", len(result))
	}
}

func TestSessionTruncateNoOp(t *testing.T) {
	s := NewSession("test")
	for i := 0; i < 5; i++ {
		s.AddMessage(domain.Message{Role: domain.RoleUser, Content: "msg"})
	}

	s.Truncate(100) // more than message count
	if len(s.Messages()) != 5 {
		t.Errorf("expected 5 messages (no truncation), got %d", len(s.Messages()))
	}
}

func TestSessionManagerListSessions(t *testing.T) {
	dir := t.TempDir()
	sm := NewSessionManager(dir)

	sm.GetOrCreate("session-1")
	sm.GetOrCreate("session-2")
	sm.GetOrCreate("session-3")

	list := sm.ListSessions()
	if len(list) != 3 {
		t.Errorf("ListSessions() returned %d, want 3", len(list))
	}
}

func TestAgentWithAuditLogger(t *testing.T) {
	audit := &usecaseAuditLogger{}
	llm := &mockLLM{
		responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "Hello!"},
				Usage: domain.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		AuditLogger:    audit,
	})

	session := NewSession("test")
	resp, err := agent.HandleMessage(context.Background(), session, "hi")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if resp != "Hello!" {
		t.Errorf("response = %q", resp)
	}
	if audit.logCount < 1 {
		t.Error("expected at least 1 audit log entry")
	}
}

func TestAgentWithToolCallAndAudit(t *testing.T) {
	audit := &usecaseAuditLogger{}
	llm := &mockLLM{
		responses: []domain.ChatResponse{
			{Message: domain.Message{
				Role:      domain.RoleAssistant,
				ToolCalls: []domain.ToolCall{{ID: "c1", Name: "test_tool", Arguments: json.RawMessage(`{}`)}},
			}},
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "Done"}},
		},
	}

	tools := &mockToolExecutor{
		tools: map[string]domain.Tool{
			"test_tool": &staticTool{name: "test_tool", result: "ok"},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          tools,
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		AuditLogger:    audit,
	})

	session := NewSession("test")
	_, err := agent.HandleMessage(context.Background(), session, "use tool")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	// Should have at least 2 audit entries: LLM call + tool exec
	if audit.logCount < 2 {
		t.Errorf("expected at least 2 audit logs, got %d", audit.logCount)
	}
}

func TestAgentNilMemory(t *testing.T) {
	llm := &mockLLM{
		responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "ok"}},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         nil, // nil memory
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
	})

	session := NewSession("test")
	resp, err := agent.HandleMessage(context.Background(), session, "hi")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if resp != "ok" {
		t.Errorf("response = %q", resp)
	}
}

func TestSessionManagerSaveNotFound(t *testing.T) {
	dir := t.TempDir()
	sm := NewSessionManager(dir)

	err := sm.Save("nonexistent")
	if err == nil {
		t.Error("expected error for saving nonexistent session")
	}
}

func TestAgentContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	llm := &mockLLM{
		responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "should not reach"}},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
	})

	session := NewSession("test")
	// The mock LLM doesn't check context, so it may still return.
	// This tests that the agent doesn't crash with cancelled context.
	_, _ = agent.HandleMessage(ctx, session, "test")
}

type errorQueryMemoryUsecase struct {
	mockMemory
}

func (m *errorQueryMemoryUsecase) Query(_ context.Context, _ string, _ int) ([]domain.MemoryEntry, error) {
	return nil, fmt.Errorf("memory query error")
}
func (m *errorQueryMemoryUsecase) IsAvailable() bool { return true }

type errorLLMUsecase struct{}

func (m *errorLLMUsecase) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	return nil, fmt.Errorf("llm error")
}
func (m *errorLLMUsecase) Name() string { return "error-llm" }

func TestAgentMemoryQueryError(t *testing.T) {
	llm := &mockLLM{
		responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "ok"}},
		},
	}

	mem := &errorQueryMemoryUsecase{}

	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         mem,
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
	})

	session := NewSession("test")
	resp, err := agent.HandleMessage(context.Background(), session, "hi")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	// Should still succeed (memory error is non-fatal)
	if resp != "ok" {
		t.Errorf("response = %q", resp)
	}
}

func TestAgentLLMError(t *testing.T) {
	llm := &errorLLMUsecase{}

	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
	})

	session := NewSession("test")
	_, err := agent.HandleMessage(context.Background(), session, "hi")
	if err == nil {
		t.Error("expected error from LLM failure")
	}
}

func TestAgentWithCompressor(t *testing.T) {
	llm := &mockLLM{
		responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "ok"}},
		},
	}

	compressor := NewCompressor(llm, CompressionConfig{Threshold: 3, KeepRecent: 2}, newTestLogger())

	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		Compressor:     compressor,
	})

	session := NewSession("test")
	// Add enough messages to trigger compression check
	for i := 0; i < 5; i++ {
		session.AddMessage(domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("old msg %d", i)})
	}

	_, err := agent.HandleMessage(context.Background(), session, "new msg")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
}

func TestExecuteToolErrorWithAudit(t *testing.T) {
	audit := &usecaseAuditLogger{}
	tools := &mockToolExecutor{
		tools: map[string]domain.Tool{
			"err_tool": &errorTool{name: "err_tool"},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:            &mockLLM{},
		Memory:         &mockMemory{},
		Tools:          tools,
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		AuditLogger:    audit,
	})

	call := domain.ToolCall{ID: "call_1", Name: "err_tool", Arguments: json.RawMessage(`{}`)}
	msg := agent.executeTool(context.Background(), "test-session", call)

	if msg.Role != domain.RoleTool {
		t.Errorf("Role = %q", msg.Role)
	}
	if audit.logCount != 1 {
		t.Errorf("expected 1 audit log, got %d", audit.logCount)
	}
}

func TestSessionManagerSaveMkdirError(t *testing.T) {
	// Use a path where MkdirAll will fail
	sm := NewSessionManager("/proc/nonexistent/sessions")
	sm.sessions["test"] = NewSession("test")

	err := sm.Save("test")
	if err == nil {
		t.Error("expected error from MkdirAll failure")
	}
}

func TestSessionManagerLoadCorruptFile(t *testing.T) {
	dir := t.TempDir()
	// Write corrupt JSON
	os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("not json"), 0600)

	sm := NewSessionManager(dir)
	s := sm.GetOrCreate("corrupt")
	// Should get a new session (corrupt file ignored)
	if len(s.Messages()) != 0 {
		t.Errorf("expected empty session, got %d messages", len(s.Messages()))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// unavailableMemory returns IsAvailable() = false.
type unavailableMemory struct {
	mockMemory
}

func (m *unavailableMemory) IsAvailable() bool { return false }

func TestAgentMemoryNotAvailable(t *testing.T) {
	llm := &mockLLM{
		responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "ok"}},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         &unavailableMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
	})

	session := NewSession("test")
	resp, err := agent.HandleMessage(context.Background(), session, "hi")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if resp != "ok" {
		t.Errorf("response = %q, want %q", resp, "ok")
	}
}

func TestSessionCompressMessagesNoOp(t *testing.T) {
	s := NewSession("test")
	for i := 0; i < 3; i++ {
		s.AddMessage(domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("msg %d", i)})
	}

	// keepRecent >= len(Msgs), so CompressMessages should be a no-op
	s.CompressMessages("summary text", 5)

	msgs := s.Messages()
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages (no-op), got %d", len(msgs))
	}
}

func TestTruncateHistoryWithSummaryInRange(t *testing.T) {
	// To hit the "return truncated" path (line 105) where truncated[0].Name == compressSummaryName,
	// we create a history where a second compression summary is within the truncated range.
	// history[0] is the original summary (triggers the if branch at line 96),
	// and history[1] is another summary that ends up as truncated[0].
	history := []domain.Message{
		{Role: domain.RoleAssistant, Content: "old summary", Name: compressSummaryName},
		{Role: domain.RoleAssistant, Content: "newer summary", Name: compressSummaryName},
		{Role: domain.RoleUser, Content: "msg1"},
		{Role: domain.RoleUser, Content: "msg2"},
		{Role: domain.RoleUser, Content: "msg3"},
	}
	cb := NewContextBuilder("system", "model", 4)
	result := cb.truncateHistory(history)

	// truncated = history[5-4:] = history[1:] = [newer summary, msg1, msg2, msg3]
	// truncated[0].Name == compressSummaryName -> true -> line 105: return truncated
	if len(result) != 4 {
		t.Errorf("expected 4 messages, got %d", len(result))
	}
	if result[0].Name != compressSummaryName {
		t.Errorf("first message should be compression summary, got Name=%q", result[0].Name)
	}
	if result[0].Content != "newer summary" {
		t.Errorf("first message content = %q, want %q", result[0].Content, "newer summary")
	}
}

func TestSessionManagerGetOrCreateCached(t *testing.T) {
	dir := t.TempDir()
	sm := NewSessionManager(dir)

	// First call creates a new session
	s1 := sm.GetOrCreate("cached-test")
	s1.AddMessage(domain.Message{Role: domain.RoleUser, Content: "hello"})

	// Second call should return the cached session (the ok branch)
	s2 := sm.GetOrCreate("cached-test")
	if s2 != s1 {
		t.Error("expected same session pointer for cached session")
	}
	msgs := s2.Messages()
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Errorf("cached session messages = %+v", msgs)
	}
}

func TestAgentWithCompressorBelowThreshold(t *testing.T) {
	// Compressor is non-nil but ShouldCompress returns false (below threshold).
	// This covers the false branch of a.deps.Compressor.ShouldCompress(session).
	llm := &mockLLM{
		responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "ok"}},
		},
	}

	compressor := NewCompressor(llm, CompressionConfig{Threshold: 100, KeepRecent: 50}, newTestLogger())

	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		Compressor:     compressor,
	})

	session := NewSession("test")
	resp, err := agent.HandleMessage(context.Background(), session, "hi")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if resp != "ok" {
		t.Errorf("response = %q, want %q", resp, "ok")
	}
}

func TestAgentWithCompressorError(t *testing.T) {
	// Test compression failure within HandleMessage (lines 117-119).
	// The compressor's LLM returns an error, causing Compress to fail.
	// HandleMessage should still return the response (compression failure is non-fatal).
	agentLLM := &mockLLM{
		responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "ok"}},
		},
	}

	// Compressor uses an error LLM so Compress will fail
	compressorLLM := &errorLLMUsecase{}
	compressor := NewCompressor(compressorLLM, CompressionConfig{Threshold: 3, KeepRecent: 2}, newTestLogger())

	agent := NewAgent(AgentDeps{
		LLM:            agentLLM,
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		Compressor:     compressor,
	})

	session := NewSession("test")
	// Add enough messages to exceed the threshold (3)
	for i := 0; i < 5; i++ {
		session.AddMessage(domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("old msg %d", i)})
	}

	resp, err := agent.HandleMessage(context.Background(), session, "new msg")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if resp != "ok" {
		t.Errorf("response = %q, want %q", resp, "ok")
	}
}

// --- Privacy Manager tests ---

func TestExportWriteError(t *testing.T) {
	pm := NewPrivacyManager(
		t.TempDir(),
		&mockMemory{entries: []domain.MemoryEntry{{ID: "1", Content: "test"}}},
		nil,
		domain.DataFlowInfo{},
	)
	// Try to write to a path under a non-existent directory
	_, err := pm.Export(context.Background(), "/nonexistent/dir/export.json")
	if err == nil {
		t.Error("expected error for invalid export path")
	}
}

func TestGrantConsentSaveError(t *testing.T) {
	// Create a regular file where a directory is expected, so MkdirAll fails
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	os.WriteFile(tmpFile, []byte("x"), 0600)
	pm := NewPrivacyManager(
		filepath.Join(tmpFile, "sub", "dir"), // parent is a file, not a dir
		&mockMemory{},
		nil,
		domain.DataFlowInfo{},
	)
	err := pm.GrantConsent(context.Background())
	if err == nil {
		t.Error("expected error for MkdirAll failure")
	}
}

func TestRevokeConsentSaveError(t *testing.T) {
	// Same technique: MkdirAll will fail because parent is a file
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	os.WriteFile(tmpFile, []byte("x"), 0600)
	pm := NewPrivacyManager(
		filepath.Join(tmpFile, "sub", "dir"),
		&mockMemory{},
		nil,
		domain.DataFlowInfo{},
	)
	err := pm.RevokeConsent(context.Background())
	if err == nil {
		t.Error("expected error for MkdirAll failure")
	}
}

// --- mockLLMSequence for error/retry testing ---

type llmResult struct {
	resp *domain.ChatResponse
	err  error
}

type mockLLMSequence struct {
	mu      sync.Mutex
	results []llmResult
	idx     int
}

func (m *mockLLMSequence) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.idx >= len(m.results) {
		return &domain.ChatResponse{
			Message: domain.Message{Role: domain.RoleAssistant, Content: "fallback"},
		}, nil
	}
	r := m.results[m.idx]
	m.idx++
	return r.resp, r.err
}

func (m *mockLLMSequence) Name() string { return "mock-sequence" }

func (m *mockLLMSequence) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.idx
}

// --- Recovery Loop tests ---

func TestAgentRecoveryRetryOnRateLimit(t *testing.T) {
	llm := &mockLLMSequence{
		results: []llmResult{
			{err: fmt.Errorf("API error 429: rate limit exceeded")},
			{resp: &domain.ChatResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "ok"}}},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:             llm,
		Memory:          &mockMemory{},
		Tools:           &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder:  NewContextBuilder("system", "model", 50),
		Logger:          newTestLogger(),
		MaxIterations:   10,
		ErrorClassifier: NewErrorClassifier(),
	})

	session := NewSession("test")
	resp, err := agent.HandleMessage(context.Background(), session, "hi")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if resp != "ok" {
		t.Errorf("response = %q, want %q", resp, "ok")
	}
	if llm.CallCount() != 2 {
		t.Errorf("LLM calls = %d, want 2", llm.CallCount())
	}
}

func TestAgentRecoveryPermanentError(t *testing.T) {
	llm := &mockLLMSequence{
		results: []llmResult{
			{err: fmt.Errorf("API error 401: unauthorized")},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:             llm,
		Memory:          &mockMemory{},
		Tools:           &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder:  NewContextBuilder("system", "model", 50),
		Logger:          newTestLogger(),
		MaxIterations:   10,
		ErrorClassifier: NewErrorClassifier(),
	})

	session := NewSession("test")
	_, err := agent.HandleMessage(context.Background(), session, "hi")
	if err == nil {
		t.Fatal("expected error for permanent failure")
	}
	if llm.CallCount() != 1 {
		t.Errorf("LLM calls = %d, want 1 (no retry for permanent)", llm.CallCount())
	}
}

func TestAgentRecoveryContextOverflowWithCompression(t *testing.T) {
	// First call fails with context overflow, second succeeds (after compression).
	llm := &mockLLMSequence{
		results: []llmResult{
			{err: fmt.Errorf("API error 400: maximum context length exceeded")},
			{resp: &domain.ChatResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "compressed ok"}}},
		},
	}

	compressor := NewCompressor(
		// Use the same llm for compression (it will succeed on the summary call).
		&mockLLM{responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "summary"}},
		}},
		CompressionConfig{Threshold: 3, KeepRecent: 2},
		newTestLogger(),
	)

	agent := NewAgent(AgentDeps{
		LLM:             llm,
		Memory:          &mockMemory{},
		Tools:           &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder:  NewContextBuilder("system", "model", 50),
		Logger:          newTestLogger(),
		MaxIterations:   10,
		ErrorClassifier: NewErrorClassifier(),
		Compressor:      compressor,
	})

	session := NewSession("test")
	// Add enough messages for compression to have something to work with.
	for i := 0; i < 5; i++ {
		session.AddMessage(domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("old msg %d", i)})
	}

	resp, err := agent.HandleMessage(context.Background(), session, "new msg")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if resp != "compressed ok" {
		t.Errorf("response = %q, want %q", resp, "compressed ok")
	}
}

func TestAgentRecoveryContextOverflowNoCompressor(t *testing.T) {
	// Context overflow but no compressor available — should retry anyway (backoff).
	llm := &mockLLMSequence{
		results: []llmResult{
			{err: fmt.Errorf("API error 400: maximum context length exceeded")},
			{resp: &domain.ChatResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "ok"}}},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:             llm,
		Memory:          &mockMemory{},
		Tools:           &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder:  NewContextBuilder("system", "model", 50),
		Logger:          newTestLogger(),
		MaxIterations:   10,
		ErrorClassifier: NewErrorClassifier(),
		Compressor:      nil, // no compressor
	})

	session := NewSession("test")
	resp, err := agent.HandleMessage(context.Background(), session, "hi")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if resp != "ok" {
		t.Errorf("response = %q, want %q", resp, "ok")
	}
}

func TestAgentRecoveryExhaustsRetries(t *testing.T) {
	// All attempts fail with 429 — should fail after maxLLMRetries.
	llm := &mockLLMSequence{
		results: []llmResult{
			{err: fmt.Errorf("API error 429: rate limit")},
			{err: fmt.Errorf("API error 429: rate limit")},
			{err: fmt.Errorf("API error 429: rate limit")},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:             llm,
		Memory:          &mockMemory{},
		Tools:           &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder:  NewContextBuilder("system", "model", 50),
		Logger:          newTestLogger(),
		MaxIterations:   10,
		ErrorClassifier: NewErrorClassifier(),
	})

	session := NewSession("test")
	_, err := agent.HandleMessage(context.Background(), session, "hi")
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if llm.CallCount() != maxLLMRetries {
		t.Errorf("LLM calls = %d, want %d", llm.CallCount(), maxLLMRetries)
	}
}

func TestAgentRecoveryContextCancelled(t *testing.T) {
	llm := &mockLLMSequence{
		results: []llmResult{
			{err: fmt.Errorf("API error 429: rate limit")},
			{err: fmt.Errorf("API error 429: rate limit")},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	agent := NewAgent(AgentDeps{
		LLM:             llm,
		Memory:          &mockMemory{},
		Tools:           &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder:  NewContextBuilder("system", "model", 50),
		Logger:          newTestLogger(),
		MaxIterations:   10,
		ErrorClassifier: NewErrorClassifier(),
	})

	session := NewSession("test")
	_, err := agent.HandleMessage(ctx, session, "hi")
	if err == nil {
		t.Fatal("expected error from context cancellation")
	}
}

func TestAgentNoClassifierPreservesExistingBehavior(t *testing.T) {
	llm := &mockLLMSequence{
		results: []llmResult{
			{err: fmt.Errorf("API error 429: rate limit")},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:             llm,
		Memory:          &mockMemory{},
		Tools:           &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder:  NewContextBuilder("system", "model", 50),
		Logger:          newTestLogger(),
		MaxIterations:   10,
		ErrorClassifier: nil, // NO classifier
	})

	session := NewSession("test")
	_, err := agent.HandleMessage(context.Background(), session, "hi")
	if err == nil {
		t.Fatal("expected error (no classifier = no retry)")
	}
	if llm.CallCount() != 1 {
		t.Errorf("LLM calls = %d, want 1 (no retry without classifier)", llm.CallCount())
	}
}

// --- Atomic Group Truncation tests ---

func TestTruncateHistoryAtomicGroupPreserved(t *testing.T) {
	cb := NewContextBuilder("system", "model", 4)

	history := []domain.Message{
		{Role: domain.RoleUser, Content: "old msg"},
		{Role: domain.RoleUser, Content: "use tools"},
		{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{
				{ID: "c1", Name: "t1"},
				{ID: "c2", Name: "t2"},
			},
		},
		{Role: domain.RoleTool, Name: "t1", Content: "r1", ToolCalls: []domain.ToolCall{{ID: "c1", Name: "t1"}}},
		{Role: domain.RoleTool, Name: "t2", Content: "r2", ToolCalls: []domain.ToolCall{{ID: "c2", Name: "t2"}}},
		{Role: domain.RoleAssistant, Content: "done"},
	}

	result := cb.truncateHistory(history)
	// Budget is 4. The group [assistant(2 calls), tool, tool] = 3 msgs + "done" = 4.
	// "old msg" and "use tools" should be dropped.
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
	if result[0].Role != domain.RoleAssistant || len(result[0].ToolCalls) != 2 {
		t.Errorf("result[0] should be assistant with tool calls")
	}
	if result[3].Content != "done" {
		t.Errorf("result[3] = %q, want %q", result[3].Content, "done")
	}
}

func TestTruncateHistoryAtomicGroupTooLarge(t *testing.T) {
	cb := NewContextBuilder("system", "model", 2)

	// A single group of 4 messages exceeds budget of 2.
	// The group should still be kept whole (never split).
	history := []domain.Message{
		{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{
				{ID: "c1", Name: "t1"},
				{ID: "c2", Name: "t2"},
				{ID: "c3", Name: "t3"},
			},
		},
		{Role: domain.RoleTool, Name: "t1", Content: "r1", ToolCalls: []domain.ToolCall{{ID: "c1", Name: "t1"}}},
		{Role: domain.RoleTool, Name: "t2", Content: "r2", ToolCalls: []domain.ToolCall{{ID: "c2", Name: "t2"}}},
		{Role: domain.RoleTool, Name: "t3", Content: "r3", ToolCalls: []domain.ToolCall{{ID: "c3", Name: "t3"}}},
	}

	result := cb.truncateHistory(history)
	if len(result) != 4 {
		t.Fatalf("expected 4 messages (group kept whole), got %d", len(result))
	}
}

func TestTruncateHistoryMixedGroupsAndSingles(t *testing.T) {
	cb := NewContextBuilder("system", "model", 5)

	history := []domain.Message{
		{Role: domain.RoleUser, Content: "msg1"},       // group 1 (1 msg)
		{Role: domain.RoleUser, Content: "msg2"},       // group 2 (1 msg)
		{Role: domain.RoleAssistant, Content: "reply"}, // group 3 (1 msg)
		{Role: domain.RoleUser, Content: "use tool"},   // group 4 (1 msg)
		{
			Role:      domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{{ID: "c1", Name: "t1"}},
		}, // group 5 start (2 msgs)
		{Role: domain.RoleTool, Name: "t1", Content: "r1", ToolCalls: []domain.ToolCall{{ID: "c1", Name: "t1"}}},
		{Role: domain.RoleAssistant, Content: "final"}, // group 6 (1 msg)
	}

	result := cb.truncateHistory(history)
	// Budget=5. From the end: group6(1)=1, group5(2)=3, group4(1)=4, group3(1)=5. Stop.
	if len(result) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(result))
	}
	if result[0].Content != "reply" {
		t.Errorf("result[0] = %q, want %q", result[0].Content, "reply")
	}
	if result[4].Content != "final" {
		t.Errorf("result[4] = %q, want %q", result[4].Content, "final")
	}
}

func TestTruncateHistoryWithSummaryAndAtomicGroups(t *testing.T) {
	cb := NewContextBuilder("system", "model", 3)

	history := []domain.Message{
		{Role: domain.RoleAssistant, Content: "summary", Name: compressSummaryName},
		{Role: domain.RoleUser, Content: "old"},
		{Role: domain.RoleUser, Content: "use tool"},
		{
			Role:      domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{{ID: "c1", Name: "t1"}},
		},
		{Role: domain.RoleTool, Name: "t1", Content: "r1", ToolCalls: []domain.ToolCall{{ID: "c1", Name: "t1"}}},
		{Role: domain.RoleAssistant, Content: "done"},
	}

	result := cb.truncateHistory(history)
	// Budget=3. From end: "done"(1)=1, [assistant+tool](2)=3. Stop.
	// Summary gets prepended.
	if len(result) < 3 {
		t.Fatalf("expected at least 3 messages, got %d", len(result))
	}
	if result[0].Name != compressSummaryName {
		t.Errorf("result[0] should be compression summary, got Name=%q", result[0].Name)
	}
}

func TestTruncateHistoryNoAtomicGroupsBackwardCompat(t *testing.T) {
	cb := NewContextBuilder("system", "model", 3)

	history := make([]domain.Message, 10)
	for i := range history {
		history[i] = domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("msg%d", i)}
	}

	result := cb.truncateHistory(history)
	if len(result) != 3 {
		t.Errorf("expected 3 messages, got %d", len(result))
	}
	if result[0].Content != "msg7" {
		t.Errorf("first message = %q, want %q", result[0].Content, "msg7")
	}
}

// Suppress unused import warning
var _ = time.Now
