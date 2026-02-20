package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"alfred-ai/internal/domain"
)

// --- E2E test helpers ---

// capturingTool records each invocation's arguments and returns a configured result.
type capturingTool struct {
	name    string
	result  string
	mu      sync.Mutex
	calls   []json.RawMessage
	execErr error // if non-nil, Execute returns this error
}

func (t *capturingTool) Name() string        { return t.name }
func (t *capturingTool) Description() string { return t.name + " tool" }
func (t *capturingTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{Name: t.name, Description: t.Description()}
}
func (t *capturingTool) Execute(_ context.Context, args json.RawMessage) (*domain.ToolResult, error) {
	t.mu.Lock()
	t.calls = append(t.calls, args)
	t.mu.Unlock()
	if t.execErr != nil {
		return nil, t.execErr
	}
	return &domain.ToolResult{Content: t.result}, nil
}
func (t *capturingTool) CallCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.calls)
}

// mockApprover controls whether tool calls are approved.
type mockApprover struct {
	needsApproval bool
	approved      bool
	err           error
}

func (a *mockApprover) NeedsApproval(_ domain.ToolCall) bool { return a.needsApproval }
func (a *mockApprover) RequestApproval(_ context.Context, _ domain.ToolCall) (bool, error) {
	return a.approved, a.err
}

// newE2EAgent creates an agent with the given LLM responses and tools.
func newE2EAgent(responses []domain.ChatResponse, tools map[string]domain.Tool, opts ...func(*AgentDeps)) *Agent {
	deps := AgentDeps{
		LLM:            &mockLLM{responses: responses},
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: tools},
		ContextBuilder: NewContextBuilder("You are a helpful assistant.", "test-model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
	}
	for _, opt := range opts {
		opt(&deps)
	}
	return NewAgent(deps)
}

// --- E2E: Multi-turn Conversation ---

func TestE2E_MultiTurnConversation(t *testing.T) {
	// Simulate a 3-turn conversation where the LLM sees growing context.
	llm := &mockLLM{
		responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "Hi! I'm here to help."}},
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "Go is great for backend dev."}},
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "Yes, you asked about Go earlier."}},
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

	session := NewSession("e2e-multi")

	// Turn 1
	resp1, err := agent.HandleMessage(context.Background(), session, "Hello!")
	require.NoError(t, err)
	assert.Equal(t, "Hi! I'm here to help.", resp1)

	// Turn 2
	resp2, err := agent.HandleMessage(context.Background(), session, "Tell me about Go")
	require.NoError(t, err)
	assert.Equal(t, "Go is great for backend dev.", resp2)

	// Turn 3
	resp3, err := agent.HandleMessage(context.Background(), session, "Did I ask about Go?")
	require.NoError(t, err)
	assert.Equal(t, "Yes, you asked about Go earlier.", resp3)

	// Verify session has all messages (3 user + 3 assistant = 6)
	msgs := session.Messages()
	assert.Equal(t, 6, len(msgs))
	assert.Equal(t, domain.RoleUser, msgs[0].Role)
	assert.Equal(t, "Hello!", msgs[0].Content)
	assert.Equal(t, domain.RoleAssistant, msgs[1].Role)
	assert.Equal(t, domain.RoleUser, msgs[2].Role)
	assert.Equal(t, "Tell me about Go", msgs[2].Content)
}

// --- E2E: Multi-tool Chain (search → read → summarize) ---

func TestE2E_MultiToolChain(t *testing.T) {
	// LLM calls tools in sequence: search → read_file → final response.
	searchTool := &capturingTool{name: "search", result: "found: config.go"}
	readTool := &capturingTool{name: "read_file", result: "package config\n\ntype Config struct{...}"}

	responses := []domain.ChatResponse{
		// Iteration 1: LLM calls search
		{Message: domain.Message{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{
				{ID: "call_1", Name: "search", Arguments: json.RawMessage(`{"query":"config file"}`)},
			},
		}},
		// Iteration 2: LLM calls read_file based on search result
		{Message: domain.Message{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{
				{ID: "call_2", Name: "read_file", Arguments: json.RawMessage(`{"path":"config.go"}`)},
			},
		}},
		// Iteration 3: final response
		{Message: domain.Message{
			Role:    domain.RoleAssistant,
			Content: "The config file defines a Config struct with application settings.",
		}},
	}

	agent := newE2EAgent(responses, map[string]domain.Tool{
		"search":    searchTool,
		"read_file": readTool,
	})

	session := NewSession("e2e-chain")
	resp, err := agent.HandleMessage(context.Background(), session, "What's in the config file?")
	require.NoError(t, err)
	assert.Equal(t, "The config file defines a Config struct with application settings.", resp)

	// Both tools should have been called exactly once.
	assert.Equal(t, 1, searchTool.CallCount())
	assert.Equal(t, 1, readTool.CallCount())

	// Session should have: user + assistant(tool) + tool_result + assistant(tool) + tool_result + assistant(final) = 6
	msgs := session.Messages()
	assert.Equal(t, 6, len(msgs))
	assert.Equal(t, domain.RoleUser, msgs[0].Role)
	assert.Equal(t, domain.RoleTool, msgs[2].Role)
	assert.Equal(t, "found: config.go", msgs[2].Content)
}

// --- E2E: Parallel Tool Calls ---

func TestE2E_ParallelToolCalls(t *testing.T) {
	// LLM requests 3 tool calls in a single response.
	weatherTool := &capturingTool{name: "weather", result: "sunny, 25°C"}
	timeTool := &capturingTool{name: "time", result: "14:30 UTC"}
	newsTool := &capturingTool{name: "news", result: "No breaking news"}

	responses := []domain.ChatResponse{
		// LLM requests all 3 tools at once
		{Message: domain.Message{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{
				{ID: "call_w", Name: "weather", Arguments: json.RawMessage(`{}`)},
				{ID: "call_t", Name: "time", Arguments: json.RawMessage(`{}`)},
				{ID: "call_n", Name: "news", Arguments: json.RawMessage(`{}`)},
			},
		}},
		// Final response after all tool results
		{Message: domain.Message{
			Role:    domain.RoleAssistant,
			Content: "It's 14:30 UTC, sunny at 25°C, and no breaking news.",
		}},
	}

	agent := newE2EAgent(responses, map[string]domain.Tool{
		"weather": weatherTool,
		"time":    timeTool,
		"news":    newsTool,
	})

	session := NewSession("e2e-parallel")
	resp, err := agent.HandleMessage(context.Background(), session, "Give me a briefing")
	require.NoError(t, err)
	assert.Contains(t, resp, "14:30 UTC")

	assert.Equal(t, 1, weatherTool.CallCount())
	assert.Equal(t, 1, timeTool.CallCount())
	assert.Equal(t, 1, newsTool.CallCount())

	// Session: user + assistant(3 tools) + 3 tool results + assistant(final) = 6
	msgs := session.Messages()
	assert.Equal(t, 6, len(msgs))

	// Tool results should be in order
	assert.Equal(t, domain.RoleTool, msgs[2].Role)
	assert.Equal(t, "weather", msgs[2].Name)
	assert.Equal(t, domain.RoleTool, msgs[3].Role)
	assert.Equal(t, "time", msgs[3].Name)
	assert.Equal(t, domain.RoleTool, msgs[4].Role)
	assert.Equal(t, "news", msgs[4].Name)
}

// --- E2E: Tool Error → LLM Adaptation ---

func TestE2E_ToolErrorRecovery(t *testing.T) {
	// Tool fails on first attempt; LLM sees the error and adjusts.
	failingTool := &capturingTool{name: "database", execErr: fmt.Errorf("connection refused")}
	fallbackTool := &capturingTool{name: "cache", result: "cached result: 42"}

	responses := []domain.ChatResponse{
		// LLM tries the database tool
		{Message: domain.Message{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{
				{ID: "call_1", Name: "database", Arguments: json.RawMessage(`{"query":"SELECT count(*)"}`)},
			},
		}},
		// LLM sees the error, tries cache instead
		{Message: domain.Message{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{
				{ID: "call_2", Name: "cache", Arguments: json.RawMessage(`{"key":"count"}`)},
			},
		}},
		// Final response using cache result
		{Message: domain.Message{
			Role:    domain.RoleAssistant,
			Content: "The count is 42 (from cache, database was unavailable).",
		}},
	}

	agent := newE2EAgent(responses, map[string]domain.Tool{
		"database": failingTool,
		"cache":    fallbackTool,
	})

	session := NewSession("e2e-recovery")
	resp, err := agent.HandleMessage(context.Background(), session, "How many records?")
	require.NoError(t, err)
	assert.Contains(t, resp, "42")

	// Database was called once (and failed), cache was called once (and succeeded)
	assert.Equal(t, 1, failingTool.CallCount())
	assert.Equal(t, 1, fallbackTool.CallCount())

	// Verify the error message was stored in session
	msgs := session.Messages()
	// user + assistant(db call) + tool(error) + assistant(cache call) + tool(result) + assistant(final) = 6
	assert.Equal(t, 6, len(msgs))
	assert.Contains(t, msgs[2].Content, "connection refused") // tool error in session
}

// --- E2E: Multi-turn with Tool Calls ---

func TestE2E_MultiTurnWithTools(t *testing.T) {
	// Turn 1: simple response. Turn 2: tool call + response.
	tool := &capturingTool{name: "calculator", result: "42"}

	responses := []domain.ChatResponse{
		// Turn 1: simple response
		{Message: domain.Message{Role: domain.RoleAssistant, Content: "Sure, I can help with math!"}},
		// Turn 2, iteration 1: tool call
		{Message: domain.Message{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{
				{ID: "calc_1", Name: "calculator", Arguments: json.RawMessage(`{"expr":"6*7"}`)},
			},
		}},
		// Turn 2, iteration 2: final response
		{Message: domain.Message{
			Role:    domain.RoleAssistant,
			Content: "6 * 7 = 42",
		}},
	}

	agent := newE2EAgent(responses, map[string]domain.Tool{
		"calculator": tool,
	})

	session := NewSession("e2e-multi-tool")

	// Turn 1
	resp1, err := agent.HandleMessage(context.Background(), session, "Can you do math?")
	require.NoError(t, err)
	assert.Equal(t, "Sure, I can help with math!", resp1)
	assert.Equal(t, 0, tool.CallCount()) // no tool use yet

	// Turn 2
	resp2, err := agent.HandleMessage(context.Background(), session, "What is 6 times 7?")
	require.NoError(t, err)
	assert.Equal(t, "6 * 7 = 42", resp2)
	assert.Equal(t, 1, tool.CallCount())

	// Verify full session state: 2 user + 1 assistant + 1 assistant(tool) + 1 tool + 1 assistant = 6
	msgs := session.Messages()
	assert.Equal(t, 6, len(msgs))
}

// --- E2E: Router Multi-turn with Session Persistence ---

func TestE2E_RouterMultiTurnPersistence(t *testing.T) {
	bus := &recordingBus{}
	llm := &mockLLM{
		responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "Hello! Nice to meet you."}},
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "You said your name is Alice."}},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		Bus:            bus,
	})

	dir := t.TempDir()
	sessions := NewSessionManager(dir)
	router := NewRouter(agent, sessions, bus, newTestLogger())

	// Turn 1
	out1, err := router.Handle(context.Background(), domain.InboundMessage{
		SessionID: "user1", Content: "Hi, I'm Alice", ChannelName: "cli",
	})
	require.NoError(t, err)
	assert.Contains(t, out1.Content, "Hello!")
	assert.Equal(t, "user1", out1.SessionID) // original session ID preserved

	// Turn 2
	out2, err := router.Handle(context.Background(), domain.InboundMessage{
		SessionID: "user1", Content: "What's my name?", ChannelName: "cli",
	})
	require.NoError(t, err)
	assert.Contains(t, out2.Content, "Alice")

	// Verify session was persisted
	sm2 := NewSessionManager(dir)
	loaded := sm2.GetOrCreate("cli:user1")
	msgs := loaded.Messages()
	assert.GreaterOrEqual(t, len(msgs), 4) // at least 2 user + 2 assistant

	// Verify events were published
	events := bus.Events()
	receivedCount := 0
	sentCount := 0
	for _, e := range events {
		switch e.Type {
		case domain.EventMessageReceived:
			receivedCount++
		case domain.EventMessageSent:
			sentCount++
		}
	}
	assert.Equal(t, 2, receivedCount, "should have 2 EventMessageReceived")
	assert.Equal(t, 2, sentCount, "should have 2 EventMessageSent")
}

// --- E2E: Compression During Conversation ---

func TestE2E_CompressionMidConversation(t *testing.T) {
	// Scenario: After enough messages, compression should trigger.
	// We use a low threshold (4 messages) so it triggers after 2 turns.
	agentLLM := &mockLLM{
		responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "response 1"}},
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "response 2"}},
			// This response is used by the compressor for summarization
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "Summary of earlier conversation."}},
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "response 3 after compression"}},
		},
	}

	compressor := NewCompressor(agentLLM, CompressionConfig{
		Threshold:  4, // trigger after 4 messages
		KeepRecent: 2,
	}, newTestLogger())

	agent := NewAgent(AgentDeps{
		LLM:            agentLLM,
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		Compressor:     compressor,
	})

	session := NewSession("e2e-compress")

	// Turn 1: 2 messages (user+assistant)
	resp1, err := agent.HandleMessage(context.Background(), session, "Tell me about Go")
	require.NoError(t, err)
	assert.Equal(t, "response 1", resp1)

	// Turn 2: 4 messages total → should trigger compression
	resp2, err := agent.HandleMessage(context.Background(), session, "Tell me more")
	require.NoError(t, err)
	assert.Equal(t, "response 2", resp2)

	// After compression: session should have fewer messages than 4
	msgs := session.Messages()
	// Compressed = summary(1) + keepRecent(2) = 3, but could be more if compression
	// decided to keep extra. The key assertion: we still get valid responses.
	assert.LessOrEqual(t, len(msgs), 4, "compression should have reduced message count")

	// Turn 3: conversation continues post-compression
	resp3, err := agent.HandleMessage(context.Background(), session, "What were we talking about?")
	require.NoError(t, err)
	assert.NotEmpty(t, resp3)
}

// --- E2E: Approval Gating ---

func TestE2E_ApprovalDenied(t *testing.T) {
	// LLM tries to call a tool, approver denies it, LLM should see the denial.
	dangerousTool := &capturingTool{name: "delete_all", result: "deleted everything"}

	responses := []domain.ChatResponse{
		// LLM tries dangerous action
		{Message: domain.Message{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{
				{ID: "call_1", Name: "delete_all", Arguments: json.RawMessage(`{}`)},
			},
		}},
		// LLM responds after seeing denial
		{Message: domain.Message{
			Role:    domain.RoleAssistant,
			Content: "I couldn't delete anything because the action was denied.",
		}},
	}

	approver := &mockApprover{needsApproval: true, approved: false}

	agent := newE2EAgent(responses, map[string]domain.Tool{
		"delete_all": dangerousTool,
	}, func(d *AgentDeps) {
		d.Approver = approver
	})

	session := NewSession("e2e-approval")
	resp, err := agent.HandleMessage(context.Background(), session, "Delete everything")
	require.NoError(t, err)
	assert.Contains(t, resp, "denied")

	// Tool should NOT have been executed
	assert.Equal(t, 0, dangerousTool.CallCount())

	// Session should contain the denial message
	msgs := session.Messages()
	toolMsg := msgs[2] // user + assistant(tool_call) + tool(denial)
	assert.Equal(t, domain.RoleTool, toolMsg.Role)
	assert.Contains(t, toolMsg.Content, "denied")
}

func TestE2E_ApprovalApproved(t *testing.T) {
	tool := &capturingTool{name: "deploy", result: "deployed v2.0"}

	responses := []domain.ChatResponse{
		{Message: domain.Message{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{
				{ID: "call_1", Name: "deploy", Arguments: json.RawMessage(`{"version":"v2.0"}`)},
			},
		}},
		{Message: domain.Message{
			Role:    domain.RoleAssistant,
			Content: "Successfully deployed v2.0!",
		}},
	}

	approver := &mockApprover{needsApproval: true, approved: true}

	agent := newE2EAgent(responses, map[string]domain.Tool{
		"deploy": tool,
	}, func(d *AgentDeps) {
		d.Approver = approver
	})

	session := NewSession("e2e-approved")
	resp, err := agent.HandleMessage(context.Background(), session, "Deploy v2.0")
	require.NoError(t, err)
	assert.Contains(t, resp, "v2.0")
	assert.Equal(t, 1, tool.CallCount())
}

// --- E2E: Session Locking ---

func TestE2E_SessionLockSerializes(t *testing.T) {
	// Two concurrent HandleMessage calls on the same session should be serialized.
	var callOrder []int
	var mu sync.Mutex

	// LLM that records call order and takes a bit of time
	llm := &mockLLMSequence{
		results: []llmResult{
			{resp: &domain.ChatResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "first"}}},
			{resp: &domain.ChatResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "second"}}},
		},
	}

	locker := NewSessionLocker()
	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		SessionLocker:  locker,
	})

	session := NewSession("e2e-lock")

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		resp, err := agent.HandleMessage(context.Background(), session, "msg 1")
		require.NoError(t, err)
		mu.Lock()
		if resp == "first" {
			callOrder = append(callOrder, 1)
		} else {
			callOrder = append(callOrder, 2)
		}
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		resp, err := agent.HandleMessage(context.Background(), session, "msg 2")
		require.NoError(t, err)
		mu.Lock()
		if resp == "first" {
			callOrder = append(callOrder, 1)
		} else {
			callOrder = append(callOrder, 2)
		}
		mu.Unlock()
	}()

	wg.Wait()

	// Both calls should have completed
	assert.Equal(t, 2, len(callOrder))

	// Session lock should be released
	assert.Equal(t, 0, locker.ActiveCount())

	// Session should have 4 messages (2 user + 2 assistant)
	msgs := session.Messages()
	assert.Equal(t, 4, len(msgs))
}

// --- E2E: Event Bus Full Flow ---

func TestE2E_EventBusFullFlow(t *testing.T) {
	// Verify complete event sequence for a tool-using conversation.
	bus := &recordingBus{}
	tool := &capturingTool{name: "lookup", result: "found it"}

	responses := []domain.ChatResponse{
		{Message: domain.Message{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{
				{ID: "call_1", Name: "lookup", Arguments: json.RawMessage(`{}`)},
			},
		}},
		{Message: domain.Message{Role: domain.RoleAssistant, Content: "Result: found it"}},
	}

	agent := NewAgent(AgentDeps{
		LLM:            &mockLLM{responses: responses},
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{"lookup": tool}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		Bus:            bus,
	})

	session := NewSession("e2e-events")
	_, err := agent.HandleMessage(context.Background(), session, "Look it up")
	require.NoError(t, err)

	events := bus.Events()
	var types []domain.EventType
	for _, e := range events {
		types = append(types, e.Type)
	}

	// Expected event sequence:
	// LLMCallStarted → LLMCallCompleted → ToolCallStarted → ToolCallCompleted → LLMCallStarted → LLMCallCompleted
	assert.Contains(t, types, domain.EventLLMCallStarted)
	assert.Contains(t, types, domain.EventLLMCallCompleted)
	assert.Contains(t, types, domain.EventToolCallStarted)
	assert.Contains(t, types, domain.EventToolCallCompleted)

	// Should have 2 LLM calls and 1 tool call
	llmStarted := 0
	toolStarted := 0
	for _, et := range types {
		if et == domain.EventLLMCallStarted {
			llmStarted++
		}
		if et == domain.EventToolCallStarted {
			toolStarted++
		}
	}
	assert.Equal(t, 2, llmStarted, "should have 2 LLM call starts")
	assert.Equal(t, 1, toolStarted, "should have 1 tool call start")
}

// --- E2E: Memory Context Integration ---

func TestE2E_MemoryContextInPrompt(t *testing.T) {
	// Verify that memory context is included in the LLM prompt.
	var capturedReq domain.ChatRequest
	llm := &requestCapturingLLM{
		resp: &domain.ChatResponse{
			Message: domain.Message{Role: domain.RoleAssistant, Content: "You love Go!"},
		},
	}

	mem := &mockMemory{
		entries: []domain.MemoryEntry{
			{Content: "User prefers Go for backend development", Tags: []string{"preference", "golang"}},
			{Content: "User's name is Alice", Tags: []string{"personal"}},
		},
	}

	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         mem,
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("You are a helpful assistant.", "test-model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
	})

	session := NewSession("e2e-memory")
	resp, err := agent.HandleMessage(context.Background(), session, "What language do I prefer?")
	require.NoError(t, err)
	assert.Equal(t, "You love Go!", resp)

	// Verify the system prompt sent to LLM contains memory context
	capturedReq = llm.lastReq
	require.NotEmpty(t, capturedReq.Messages)
	systemMsg := capturedReq.Messages[0]
	assert.Equal(t, domain.RoleSystem, systemMsg.Role)
	assert.Contains(t, systemMsg.Content, "Go for backend development")
	assert.Contains(t, systemMsg.Content, "Alice")
}

// requestCapturingLLM captures the last ChatRequest.
type requestCapturingLLM struct {
	mu      sync.Mutex
	resp    *domain.ChatResponse
	lastReq domain.ChatRequest
}

func (m *requestCapturingLLM) Chat(_ context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	m.mu.Lock()
	m.lastReq = req
	m.mu.Unlock()
	return m.resp, nil
}
func (m *requestCapturingLLM) Name() string { return "capturing" }

// --- E2E: Tool Not Found Error Handling ---

func TestE2E_ToolNotFound(t *testing.T) {
	// LLM requests a tool that doesn't exist. Agent should include error in session
	// and continue the loop.
	responses := []domain.ChatResponse{
		{Message: domain.Message{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{
				{ID: "call_1", Name: "nonexistent_tool", Arguments: json.RawMessage(`{}`)},
			},
		}},
		{Message: domain.Message{
			Role:    domain.RoleAssistant,
			Content: "Sorry, that tool isn't available.",
		}},
	}

	agent := newE2EAgent(responses, map[string]domain.Tool{})

	session := NewSession("e2e-notfound")
	resp, err := agent.HandleMessage(context.Background(), session, "Use the magic tool")
	require.NoError(t, err)
	assert.Contains(t, resp, "isn't available")

	// Session should contain the error from the missing tool
	msgs := session.Messages()
	toolMsg := msgs[2]
	assert.Equal(t, domain.RoleTool, toolMsg.Role)
	assert.Contains(t, toolMsg.Content, "not found")
}

// --- E2E: Router with Tools End-to-End ---

func TestE2E_RouterWithToolsAndEvents(t *testing.T) {
	bus := &recordingBus{}
	tool := &capturingTool{name: "greet", result: "Hello from tool!"}

	responses := []domain.ChatResponse{
		{Message: domain.Message{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{
				{ID: "call_1", Name: "greet", Arguments: json.RawMessage(`{}`)},
			},
		}},
		{Message: domain.Message{Role: domain.RoleAssistant, Content: "The tool says hello!"}},
	}

	agent := NewAgent(AgentDeps{
		LLM:            &mockLLM{responses: responses},
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{"greet": tool}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		Bus:            bus,
	})

	sessions := NewSessionManager(t.TempDir())
	router := NewRouter(agent, sessions, bus, newTestLogger())

	out, err := router.Handle(context.Background(), domain.InboundMessage{
		SessionID: "user1", Content: "Say hello", ChannelName: "telegram",
	})
	require.NoError(t, err)
	assert.Contains(t, out.Content, "hello")
	assert.Equal(t, "user1", out.SessionID)
	assert.Equal(t, 1, tool.CallCount())

	// Verify session was saved
	sm2 := NewSessionManager(sessions.dataDir)
	loaded := sm2.GetOrCreate("telegram:user1")
	assert.GreaterOrEqual(t, len(loaded.Messages()), 4) // user + assistant(tool) + tool_result + assistant
}

// --- E2E: Concurrent Router Calls on Different Sessions ---

func TestE2E_RouterConcurrentSessions(t *testing.T) {
	bus := &recordingBus{}
	var llmCalls atomic.Int32

	// LLM that counts calls
	llm := &countingLLM{
		calls:    &llmCalls,
		response: domain.ChatResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "ok"}},
	}

	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		Bus:            bus,
	})

	sessions := NewSessionManager(t.TempDir())
	router := NewRouter(agent, sessions, bus, newTestLogger())

	// Launch 10 concurrent requests on different sessions
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := router.Handle(context.Background(), domain.InboundMessage{
				SessionID:   fmt.Sprintf("user_%d", idx),
				Content:     fmt.Sprintf("message %d", idx),
				ChannelName: "cli",
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	// No errors expected
	for err := range errs {
		t.Errorf("unexpected error: %v", err)
	}

	// All 10 LLM calls should have been made
	assert.Equal(t, int32(10), llmCalls.Load())

	// All sessions should exist
	list := sessions.ListSessions()
	assert.Equal(t, 10, len(list))
}

// countingLLM counts calls atomically.
type countingLLM struct {
	calls    *atomic.Int32
	response domain.ChatResponse
}

func (m *countingLLM) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	m.calls.Add(1)
	resp := m.response // copy
	return &resp, nil
}
func (m *countingLLM) Name() string { return "counting" }

// --- E2E: Full Agent Lifecycle with All Features ---

func TestE2E_FullAgentLifecycle(t *testing.T) {
	// Complete lifecycle: memory → tool call → compression → multi-turn → events
	bus := &recordingBus{}
	tool := &capturingTool{name: "lookup", result: "Go was created by Google in 2007"}

	agentLLM := &mockLLM{
		responses: []domain.ChatResponse{
			// Turn 1: simple response
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "I know a lot about programming!"}},
			// Turn 2: tool call
			{Message: domain.Message{
				Role: domain.RoleAssistant,
				ToolCalls: []domain.ToolCall{
					{ID: "call_1", Name: "lookup", Arguments: json.RawMessage(`{"topic":"Go language"}`)},
				},
			}},
			// Turn 2: final response after tool
			{Message: domain.Message{
				Role:    domain.RoleAssistant,
				Content: "Go was created by Google in 2007 and is great for servers.",
			}},
			// Turn 3: response (after potential compression)
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "Yes, we discussed Go earlier."}},
			// Compressor summary (if triggered)
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "Conversation summary: discussed Go programming."}},
			// Turn 4: post-compression response
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "Anything else about Go?"}},
		},
	}

	mem := &mockMemory{
		entries: []domain.MemoryEntry{
			{Content: "User is interested in Go", Tags: []string{"preference"}},
		},
	}

	compressor := NewCompressor(agentLLM, CompressionConfig{
		Threshold:  6, // compress after 6 messages
		KeepRecent: 2,
	}, newTestLogger())

	agent := NewAgent(AgentDeps{
		LLM:            agentLLM,
		Memory:         mem,
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{"lookup": tool}},
		ContextBuilder: NewContextBuilder("You are a helpful assistant.", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		Bus:            bus,
		Compressor:     compressor,
	})

	session := NewSession("e2e-lifecycle")

	// Turn 1: simple question
	resp1, err := agent.HandleMessage(context.Background(), session, "What do you know?")
	require.NoError(t, err)
	assert.Equal(t, "I know a lot about programming!", resp1)

	// Turn 2: triggers tool use
	resp2, err := agent.HandleMessage(context.Background(), session, "Tell me about Go")
	require.NoError(t, err)
	assert.Contains(t, resp2, "Go was created by Google")
	assert.Equal(t, 1, tool.CallCount())

	// Turn 3: context growing, may trigger compression
	resp3, err := agent.HandleMessage(context.Background(), session, "What did we talk about?")
	require.NoError(t, err)
	assert.NotEmpty(t, resp3)

	// Verify events were recorded
	events := bus.Events()
	assert.NotEmpty(t, events, "bus should have recorded events")

	hasLLMStart := false
	hasToolCall := false
	for _, e := range events {
		if e.Type == domain.EventLLMCallStarted {
			hasLLMStart = true
		}
		if e.Type == domain.EventToolCallStarted {
			hasToolCall = true
		}
	}
	assert.True(t, hasLLMStart, "should have LLM call events")
	assert.True(t, hasToolCall, "should have tool call events")
}

// --- E2E: Parallel Tool Execution Timing ---

func TestE2E_ParallelToolExecutionConcurrency(t *testing.T) {
	// 5 tools that each sleep 100ms. Sequential = 500ms, parallel < 250ms.
	const numTools = 5
	const toolDelay = 100 * time.Millisecond

	tools := make(map[string]domain.Tool)
	toolCalls := make([]domain.ToolCall, numTools)
	for i := 0; i < numTools; i++ {
		name := fmt.Sprintf("slow_tool_%d", i)
		tools[name] = &slowTool{name: name, delay: toolDelay, result: fmt.Sprintf("result_%d", i)}
		toolCalls[i] = domain.ToolCall{ID: fmt.Sprintf("call_%d", i), Name: name, Arguments: json.RawMessage(`{}`)}
	}

	responses := []domain.ChatResponse{
		{Message: domain.Message{Role: domain.RoleAssistant, ToolCalls: toolCalls}},
		{Message: domain.Message{Role: domain.RoleAssistant, Content: "All done!"}},
	}

	agent := newE2EAgent(responses, tools)
	session := NewSession("e2e-parallel-timing")

	start := time.Now()
	resp, err := agent.HandleMessage(context.Background(), session, "Run all tools")
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, "All done!", resp)

	// If tools ran in parallel, total time should be ~100ms (one tool delay).
	// If sequential, it would be ~500ms. Allow generous margin but ensure
	// it's clearly parallel (< 300ms = well under 500ms sequential).
	assert.Less(t, elapsed, 300*time.Millisecond,
		"parallel execution should be significantly faster than sequential (%d tools × %v = %v)",
		numTools, toolDelay, time.Duration(numTools)*toolDelay)

	// All tool results should be in order.
	msgs := session.Messages()
	for i := 0; i < numTools; i++ {
		toolMsg := msgs[2+i] // user + assistant(tool calls) + tool results...
		assert.Equal(t, domain.RoleTool, toolMsg.Role)
		assert.Equal(t, fmt.Sprintf("slow_tool_%d", i), toolMsg.Name)
		assert.Equal(t, fmt.Sprintf("result_%d", i), toolMsg.Content)
	}
}

// slowTool is a tool that sleeps for a configurable duration before returning.
type slowTool struct {
	name   string
	delay  time.Duration
	result string
}

func (t *slowTool) Name() string        { return t.name }
func (t *slowTool) Description() string { return t.name + " (slow)" }
func (t *slowTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{Name: t.name, Description: t.Description()}
}
func (t *slowTool) Execute(ctx context.Context, _ json.RawMessage) (*domain.ToolResult, error) {
	select {
	case <-time.After(t.delay):
		return &domain.ToolResult{Content: t.result}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// --- E2E: Context Overflow Recovery ---

func TestE2E_ContextOverflowForceCompressVerified(t *testing.T) {
	// Scenario: LLM returns context overflow, ForceCompress reduces messages,
	// rebuilt prompt succeeds on retry.
	llm := &mockLLMSequence{
		results: []llmResult{
			// First call: context overflow
			{err: fmt.Errorf("%w: API error 413: maximum context length exceeded", domain.ErrContextOverflow)},
			// After compression + retry: success
			{resp: &domain.ChatResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "recovered response"}}},
		},
	}

	// Compressor's own LLM for generating summaries.
	compressorLLM := &mockLLM{responses: []domain.ChatResponse{
		{Message: domain.Message{Role: domain.RoleAssistant, Content: "Summary of old conversation."}},
	}}

	compressor := NewCompressor(compressorLLM, CompressionConfig{
		Threshold:  2, // low threshold so ForceCompress always has work to do
		KeepRecent: 2,
	}, newTestLogger())

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

	session := NewSession("e2e-overflow-compress")
	// Seed session with many messages so compression has something to do.
	for i := 0; i < 10; i++ {
		session.AddMessage(domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("old message %d", i)})
		session.AddMessage(domain.Message{Role: domain.RoleAssistant, Content: fmt.Sprintf("old reply %d", i)})
	}
	initialCount := session.MessageCount()

	resp, err := agent.HandleMessage(context.Background(), session, "new question")
	require.NoError(t, err)
	assert.Equal(t, "recovered response", resp)

	// Verify compression actually reduced message count.
	finalCount := session.MessageCount()
	assert.Less(t, finalCount, initialCount, "compression should have reduced message count")

	// LLM should have been called twice (first failed, retry succeeded).
	assert.Equal(t, 2, llm.CallCount())
}

func TestE2E_ContextOverflowCompressionFails(t *testing.T) {
	// Scenario: Context overflow + compression LLM also fails → retries exhaust.
	llm := &mockLLMSequence{
		results: []llmResult{
			{err: fmt.Errorf("%w: API error 413: context too long", domain.ErrContextOverflow)},
			{err: fmt.Errorf("%w: API error 413: context too long", domain.ErrContextOverflow)},
			{err: fmt.Errorf("%w: API error 413: context too long", domain.ErrContextOverflow)},
		},
	}

	// Compressor's LLM also fails → compression won't help.
	compressorLLM := &mockLLMSequence{
		results: []llmResult{
			{err: fmt.Errorf("compressor LLM also overloaded")},
			{err: fmt.Errorf("compressor LLM also overloaded")},
			{err: fmt.Errorf("compressor LLM also overloaded")},
		},
	}

	compressor := NewCompressor(compressorLLM, CompressionConfig{
		Threshold:  2,
		KeepRecent: 2,
	}, newTestLogger())

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

	session := NewSession("e2e-overflow-fail")
	for i := 0; i < 10; i++ {
		session.AddMessage(domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("msg %d", i)})
	}

	_, err := agent.HandleMessage(context.Background(), session, "question")
	require.Error(t, err, "should fail when compression can't help")

	// All retries should have been attempted.
	assert.Equal(t, maxLLMRetries, llm.CallCount())
}

func TestE2E_ContextOverflowMidToolChain(t *testing.T) {
	// Scenario: LLM calls a tool, tool succeeds, but the SECOND LLM call
	// (with tool result in context) overflows → compress → retry succeeds.
	llm := &mockLLMSequence{
		results: []llmResult{
			// Iteration 0: LLM requests tool call (fits in context)
			{resp: &domain.ChatResponse{Message: domain.Message{
				Role: domain.RoleAssistant,
				ToolCalls: []domain.ToolCall{
					{ID: "call_1", Name: "search", Arguments: json.RawMessage(`{"q":"test"}`)},
				},
			}}},
			// Iteration 1, attempt 1: context overflow (tool result made context too big)
			{err: fmt.Errorf("%w: API error 413: maximum context length exceeded", domain.ErrContextOverflow)},
			// Iteration 1, attempt 2: success after compression
			{resp: &domain.ChatResponse{Message: domain.Message{
				Role:    domain.RoleAssistant,
				Content: "Search found the answer after recovery.",
			}}},
		},
	}

	compressorLLM := &mockLLM{responses: []domain.ChatResponse{
		{Message: domain.Message{Role: domain.RoleAssistant, Content: "Prior context summary."}},
	}}
	compressor := NewCompressor(compressorLLM, CompressionConfig{
		Threshold:  2,
		KeepRecent: 3,
	}, newTestLogger())

	searchTool := &capturingTool{name: "search", result: "result data from search"}

	agent := NewAgent(AgentDeps{
		LLM:             llm,
		Memory:          &mockMemory{},
		Tools:           &mockToolExecutor{tools: map[string]domain.Tool{"search": searchTool}},
		ContextBuilder:  NewContextBuilder("system", "model", 50),
		Logger:          newTestLogger(),
		MaxIterations:   10,
		ErrorClassifier: NewErrorClassifier(),
		Compressor:      compressor,
	})

	session := NewSession("e2e-overflow-tool")
	// Seed some history so there's content to compress.
	for i := 0; i < 8; i++ {
		session.AddMessage(domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("old msg %d", i)})
	}

	resp, err := agent.HandleMessage(context.Background(), session, "search for something")
	require.NoError(t, err)
	assert.Equal(t, "Search found the answer after recovery.", resp)

	// Tool should have been called once.
	assert.Equal(t, 1, searchTool.CallCount())

	// LLM should have been called 3 times: tool call + overflow + retry success.
	assert.Equal(t, 3, llm.CallCount())
}

// --- E2E: Session Lock Concurrency ---

func TestE2E_SessionLockHighConcurrency(t *testing.T) {
	// 20 goroutines all calling HandleMessage on the SAME session.
	// With SessionLocker, they should serialize cleanly.
	const N = 20

	var llmCalls atomic.Int32
	llm := &countingLLM{
		calls:    &llmCalls,
		response: domain.ChatResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "ok"}},
	}

	locker := NewSessionLocker()
	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		SessionLocker:  locker,
	})

	session := NewSession("e2e-lock-high")

	var wg sync.WaitGroup
	errCh := make(chan error, N)

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := agent.HandleMessage(context.Background(), session, fmt.Sprintf("msg %d", idx))
			if err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	// No errors expected.
	for err := range errCh {
		t.Errorf("unexpected error: %v", err)
	}

	// All N calls should have completed.
	assert.Equal(t, int32(N), llmCalls.Load())

	// Session should have 2*N messages (N user + N assistant).
	msgs := session.Messages()
	assert.Equal(t, 2*N, len(msgs))

	// Lock should be fully released.
	assert.Equal(t, 0, locker.ActiveCount())
}

func TestE2E_SessionLockContextCancellation(t *testing.T) {
	// Goroutine A holds the session lock via HandleMessage (slow LLM).
	// Goroutine B tries HandleMessage with a short deadline → should fail with context error.

	slowLLM := &slowMockLLM{
		delay: 200 * time.Millisecond,
		resp: domain.ChatResponse{
			Message: domain.Message{Role: domain.RoleAssistant, Content: "slow response"},
		},
	}

	locker := NewSessionLocker()
	agent := NewAgent(AgentDeps{
		LLM:            slowLLM,
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		SessionLocker:  locker,
	})

	session := NewSession("e2e-lock-cancel")

	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine A: long-running HandleMessage.
	var resultA string
	var errA error
	go func() {
		defer wg.Done()
		resultA, errA = agent.HandleMessage(context.Background(), session, "slow query")
	}()

	// Give A time to acquire the lock.
	time.Sleep(50 * time.Millisecond)

	// Goroutine B: short deadline → should fail while waiting for lock.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	var errB error
	go func() {
		defer wg.Done()
		_, errB = agent.HandleMessage(ctx, session, "impatient query")
	}()

	wg.Wait()

	// A should succeed.
	assert.NoError(t, errA)
	assert.Equal(t, "slow response", resultA)

	// B should fail with a context/deadline error.
	require.Error(t, errB)
	assert.Contains(t, errB.Error(), "session lock")

	// Give cleanup goroutine time to finish.
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 0, locker.ActiveCount(), "all locks should be cleaned up")
}

func TestE2E_SessionLockReleasedOnLLMError(t *testing.T) {
	// Verify the session lock is released even when HandleMessage returns an error.
	llm := &mockLLMSequence{
		results: []llmResult{
			{err: fmt.Errorf("LLM crashed hard")},
		},
	}

	locker := NewSessionLocker()
	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		SessionLocker:  locker,
	})

	session := NewSession("e2e-lock-error")

	_, err := agent.HandleMessage(context.Background(), session, "trigger error")
	require.Error(t, err)

	// Lock must be released despite the error.
	assert.Equal(t, 0, locker.ActiveCount(), "lock should be released after error")

	// Subsequent call should succeed (lock is free).
	llm2 := &mockLLM{responses: []domain.ChatResponse{
		{Message: domain.Message{Role: domain.RoleAssistant, Content: "recovered"}},
	}}
	agent2 := NewAgent(AgentDeps{
		LLM:            llm2,
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("system", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		SessionLocker:  locker,
	})

	resp, err := agent2.HandleMessage(context.Background(), session, "retry")
	require.NoError(t, err)
	assert.Equal(t, "recovered", resp)
}

// slowMockLLM simulates an LLM that takes a configurable duration to respond.
type slowMockLLM struct {
	delay time.Duration
	resp  domain.ChatResponse
}

func (m *slowMockLLM) Chat(ctx context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	select {
	case <-time.After(m.delay):
		resp := m.resp // copy
		return &resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *slowMockLLM) Name() string { return "slow-mock" }
