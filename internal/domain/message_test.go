package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMessageJSONRoundTrip(t *testing.T) {
	msg := Message{
		Role:      RoleUser,
		Content:   "hello",
		Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Role != msg.Role || got.Content != msg.Content {
		t.Errorf("got %+v, want %+v", got, msg)
	}
}

func TestChatResponseJSONRoundTrip(t *testing.T) {
	resp := ChatResponse{
		ID:    "resp-1",
		Model: "gpt-4o-mini",
		Message: Message{
			Role:    RoleAssistant,
			Content: "hi there",
		},
		Usage: Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ChatResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ID != resp.ID || got.Usage.TotalTokens != 15 {
		t.Errorf("got %+v, want %+v", got, resp)
	}
}

func TestMessageWithToolCalls(t *testing.T) {
	msg := Message{
		Role:    RoleAssistant,
		Content: "",
		ToolCalls: []ToolCall{
			{ID: "call-1", Name: "filesystem", Arguments: json.RawMessage(`{"action":"read"}`)},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(got.ToolCalls) != 1 || got.ToolCalls[0].Name != "filesystem" {
		t.Errorf("tool calls mismatch: got %+v", got.ToolCalls)
	}
}

func TestRoleConstants(t *testing.T) {
	roles := map[string]string{
		"system":    RoleSystem,
		"user":      RoleUser,
		"assistant": RoleAssistant,
		"tool":      RoleTool,
	}
	for expected, got := range roles {
		if got != expected {
			t.Errorf("Role %q = %q, want %q", expected, got, expected)
		}
	}
}
