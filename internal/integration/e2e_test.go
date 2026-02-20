//go:build integration
// +build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alfred-ai/internal/adapter/llm"
	"alfred-ai/internal/adapter/tool"
	"alfred-ai/internal/security"
	"alfred-ai/internal/usecase"
)

func TestE2E_AgentWithRealLLM(t *testing.T) {
	SkipIfShort(t)
	cfg := LoadConfig()
	SkipIfNoAPIKey(t, cfg.OpenAIKey, "OPENAI")

	ctx := NewTestContext(t, cfg.TestTimeout)

	// Setup real components (not mocks)
	tmpDir := t.TempDir()
	sessionMgr := usecase.NewSessionManager(tmpDir, nil)

	provider := llm.NewOpenAIProvider(cfg.OpenAIKey, "gpt-4o-mini", nil)

	// Add safe tools
	sandbox, _ := security.NewSandbox(tmpDir)
	tools := []usecase.Tool{
		tool.NewFilesystemTool(sandbox, nil),
	}

	agent := usecase.NewAgent(
		"test-agent",
		provider,
		tools,
		nil, // no memory
		sessionMgr,
		nil, // no logger
	)

	session := sessionMgr.GetOrCreate("e2e-test")

	// Test: Ask agent to create a file
	response, err := agent.HandleMessage(ctx, session, "Create a file called test.txt with content 'integration test'")
	if err != nil {
		t.Fatalf("Agent failed: %v", err)
	}

	t.Logf("Agent response: %s", response)

	// Verify file was created
	testFile := filepath.Join(tmpDir, "test.txt")
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Error("File test.txt was not created")
	}

	content, _ := os.ReadFile(testFile)
	if string(content) != "integration test" {
		t.Errorf("File content incorrect: %q", string(content))
	}
}

func TestE2E_MultiTurnConversation(t *testing.T) {
	SkipIfShort(t)
	cfg := LoadConfig()
	SkipIfNoAPIKey(t, cfg.OpenAIKey, "OPENAI")

	ctx := NewTestContext(t, 2*time.Minute)

	tmpDir := t.TempDir()
	sessionMgr := usecase.NewSessionManager(tmpDir, nil)
	provider := llm.NewOpenAIProvider(cfg.OpenAIKey, "gpt-4o-mini", nil)

	agent := usecase.NewAgent("test", provider, nil, nil, sessionMgr, nil)
	session := sessionMgr.GetOrCreate("multi-turn")

	// Turn 1: Introduction
	_, err := agent.HandleMessage(ctx, session, "My name is Alice")
	if err != nil {
		t.Fatalf("Turn 1 failed: %v", err)
	}

	// Turn 2: Recall name
	resp2, err := agent.HandleMessage(ctx, session, "What's my name?")
	if err != nil {
		t.Fatalf("Turn 2 failed: %v", err)
	}

	if !containsSubstring(resp2, "Alice") {
		t.Errorf("Agent didn't recall name. Response: %s", resp2)
	}

	t.Logf("Multi-turn test passed. Agent recalled: Alice")
}

func TestE2E_ToolExecutionWorkflow(t *testing.T) {
	SkipIfShort(t)
	cfg := LoadConfig()
	SkipIfNoAPIKey(t, cfg.OpenAIKey, "OPENAI")

	ctx := NewTestContext(t, 2*time.Minute)

	tmpDir := t.TempDir()
	sessionMgr := usecase.NewSessionManager(tmpDir, nil)
	provider := llm.NewOpenAIProvider(cfg.OpenAIKey, "gpt-4o-mini", nil)

	// Add filesystem tool
	sandbox, _ := security.NewSandbox(tmpDir)
	tools := []usecase.Tool{
		tool.NewFilesystemTool(sandbox, nil),
	}

	agent := usecase.NewAgent("test", provider, tools, nil, sessionMgr, nil)
	session := sessionMgr.GetOrCreate("tool-test")

	// Test: Multi-step workflow - create file then read it
	resp1, err := agent.HandleMessage(ctx, session, "Create a file called data.txt with the text 'Hello World'")
	if err != nil {
		t.Fatalf("Create file failed: %v", err)
	}
	t.Logf("Create response: %s", resp1)

	// Verify file exists
	dataFile := filepath.Join(tmpDir, "data.txt")
	if _, err := os.Stat(dataFile); os.IsNotExist(err) {
		t.Fatal("File data.txt was not created")
	}

	// Read the file back
	resp2, err := agent.HandleMessage(ctx, session, "Read the contents of data.txt")
	if err != nil {
		t.Fatalf("Read file failed: %v", err)
	}

	if !containsSubstring(resp2, "Hello World") {
		t.Errorf("Agent didn't read file correctly. Response: %s", resp2)
	}

	t.Logf("Read response: %s", resp2)
}

func TestE2E_SessionPersistence(t *testing.T) {
	SkipIfShort(t)
	cfg := LoadConfig()
	SkipIfNoAPIKey(t, cfg.OpenAIKey, "OPENAI")

	ctx := NewTestContext(t, 2*time.Minute)

	tmpDir := t.TempDir()
	sessionMgr := usecase.NewSessionManager(tmpDir, nil)
	provider := llm.NewOpenAIProvider(cfg.OpenAIKey, "gpt-4o-mini", nil)

	agent := usecase.NewAgent("test", provider, nil, nil, sessionMgr, nil)
	sessionID := "persist-test"
	session := sessionMgr.GetOrCreate(sessionID)

	// Turn 1: Store information
	_, err := agent.HandleMessage(ctx, session, "Remember that my favorite number is 42")
	if err != nil {
		t.Fatalf("Turn 1 failed: %v", err)
	}

	// Save session
	if err := sessionMgr.Save(sessionID); err != nil {
		t.Fatalf("Failed to save session: %v", err)
	}

	// Create new session manager (simulates restart)
	sessionMgr2 := usecase.NewSessionManager(tmpDir, nil)
	session2 := sessionMgr2.GetOrCreate(sessionID)

	// Session should be loaded from disk
	if len(session2.Messages()) < 2 {
		t.Errorf("Session not loaded correctly. Message count: %d", len(session2.Messages()))
	}

	// Turn 2: Recall information
	agent2 := usecase.NewAgent("test", provider, nil, nil, sessionMgr2, nil)
	resp2, err := agent2.HandleMessage(ctx, session2, "What's my favorite number?")
	if err != nil {
		t.Fatalf("Turn 2 failed: %v", err)
	}

	if !containsSubstring(resp2, "42") {
		t.Errorf("Agent didn't recall number from persisted session. Response: %s", resp2)
	}

	t.Logf("Session persistence test passed")
}

// Helper function for substring check
func containsSubstring(s, substr string) bool {
	return strings.Contains(s, substr)
}
