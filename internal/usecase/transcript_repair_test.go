package usecase

import (
	"encoding/json"
	"testing"

	"alfred-ai/internal/domain"
)

func TestRepairTranscript_Empty(t *testing.T) {
	result := RepairTranscript(nil)
	if result != nil {
		t.Errorf("expected nil, got %d messages", len(result))
	}
}

func TestRepairTranscript_NoToolCalls(t *testing.T) {
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "hello"},
		{Role: domain.RoleAssistant, Content: "hi there"},
		{Role: domain.RoleUser, Content: "how are you?"},
	}
	result := RepairTranscript(msgs)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	for i, m := range result {
		if m.Content != msgs[i].Content {
			t.Errorf("message[%d] content = %q, want %q", i, m.Content, msgs[i].Content)
		}
	}
}

func TestRepairTranscript_ValidToolChain(t *testing.T) {
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "use the tool"},
		{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{
				{ID: "call_1", Name: "test_tool", Arguments: json.RawMessage(`{}`)},
			},
		},
		{
			Role: domain.RoleTool, Name: "test_tool", Content: "result",
			ToolCalls: []domain.ToolCall{{ID: "call_1", Name: "test_tool"}},
		},
		{Role: domain.RoleAssistant, Content: "done"},
	}
	result := RepairTranscript(msgs)
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
}

func TestRepairTranscript_MissingToolResult(t *testing.T) {
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "use the tool"},
		{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{
				{ID: "call_1", Name: "test_tool", Arguments: json.RawMessage(`{}`)},
			},
		},
		// Missing tool result — next is a user message
		{Role: domain.RoleUser, Content: "what happened?"},
	}
	result := RepairTranscript(msgs)
	// Should be: user, assistant(call), injected tool result, user
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
	if result[2].Role != domain.RoleTool {
		t.Errorf("result[2] role = %q, want %q", result[2].Role, domain.RoleTool)
	}
	if result[2].Content != "[error] tool call did not produce a result" {
		t.Errorf("result[2] content = %q", result[2].Content)
	}
	if result[3].Role != domain.RoleUser {
		t.Errorf("result[3] role = %q, want %q", result[3].Role, domain.RoleUser)
	}
}

func TestRepairTranscript_OrphanedToolResult(t *testing.T) {
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "hello"},
		{
			Role: domain.RoleTool, Name: "orphan_tool", Content: "orphan result",
			ToolCalls: []domain.ToolCall{{ID: "call_999", Name: "orphan_tool"}},
		},
		{Role: domain.RoleAssistant, Content: "ok"},
	}
	result := RepairTranscript(msgs)
	// Orphaned tool result should be dropped
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Content != "hello" {
		t.Errorf("result[0] content = %q", result[0].Content)
	}
	if result[1].Content != "ok" {
		t.Errorf("result[1] content = %q", result[1].Content)
	}
}

func TestRepairTranscript_MultipleToolCalls(t *testing.T) {
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "use tools"},
		{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{
				{ID: "call_1", Name: "tool_a", Arguments: json.RawMessage(`{}`)},
				{ID: "call_2", Name: "tool_b", Arguments: json.RawMessage(`{}`)},
				{ID: "call_3", Name: "tool_c", Arguments: json.RawMessage(`{}`)},
			},
		},
		// Only 2 of 3 results present
		{
			Role: domain.RoleTool, Name: "tool_a", Content: "result_a",
			ToolCalls: []domain.ToolCall{{ID: "call_1", Name: "tool_a"}},
		},
		{
			Role: domain.RoleTool, Name: "tool_b", Content: "result_b",
			ToolCalls: []domain.ToolCall{{ID: "call_2", Name: "tool_b"}},
		},
		{Role: domain.RoleAssistant, Content: "done"},
	}
	result := RepairTranscript(msgs)
	// Should inject 1 missing result for call_3, then assistant
	// user, assistant(3 calls), tool_a, tool_b, injected_tool_c, assistant
	if len(result) != 6 {
		t.Fatalf("expected 6 messages, got %d", len(result))
	}
	// Find the injected result
	injected := result[4]
	if injected.Role != domain.RoleTool {
		t.Errorf("injected role = %q, want tool", injected.Role)
	}
	if injected.Name != "tool_c" {
		t.Errorf("injected name = %q, want tool_c", injected.Name)
	}
}

func TestRepairTranscript_ConsecutiveAssistantMessages(t *testing.T) {
	msgs := []domain.Message{
		{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{
				{ID: "call_1", Name: "tool_a", Arguments: json.RawMessage(`{}`)},
			},
		},
		// No result for call_1, immediately another assistant message
		{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{
				{ID: "call_2", Name: "tool_b", Arguments: json.RawMessage(`{}`)},
			},
		},
		{
			Role: domain.RoleTool, Name: "tool_b", Content: "result_b",
			ToolCalls: []domain.ToolCall{{ID: "call_2", Name: "tool_b"}},
		},
	}
	result := RepairTranscript(msgs)
	// assistant(call_1), injected_tool_a, assistant(call_2), tool_b
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
	if result[1].Role != domain.RoleTool || result[1].Name != "tool_a" {
		t.Errorf("result[1] = %q/%q, want tool/tool_a", result[1].Role, result[1].Name)
	}
}

func TestRepairTranscript_TrailingPendingCalls(t *testing.T) {
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "use tool"},
		{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{
				{ID: "call_1", Name: "tool_a", Arguments: json.RawMessage(`{}`)},
			},
		},
		// History ends without any result
	}
	result := RepairTranscript(msgs)
	// user, assistant(call), injected_tool_a
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	if result[2].Role != domain.RoleTool {
		t.Errorf("result[2] role = %q, want tool", result[2].Role)
	}
}
