package domain

import (
	"encoding/json"
	"testing"
)

func TestAgentIdentityJSON(t *testing.T) {
	identity := AgentIdentity{
		ID:           "support",
		Name:         "Support Agent",
		Description:  "Handles support queries",
		SystemPrompt: "You are a support agent.",
		Model:        "gpt-4",
		Provider:     "openai",
		Tools:        []string{"web_search", "memory_query"},
		Skills:       []string{"summarize"},
		MaxIter:      20,
		Metadata:     map[string]string{"team": "cx"},
	}

	data, err := json.Marshal(identity)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded AgentIdentity
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ID != identity.ID {
		t.Errorf("ID: got %q, want %q", decoded.ID, identity.ID)
	}
	if decoded.Name != identity.Name {
		t.Errorf("Name: got %q, want %q", decoded.Name, identity.Name)
	}
	if decoded.MaxIter != identity.MaxIter {
		t.Errorf("MaxIter: got %d, want %d", decoded.MaxIter, identity.MaxIter)
	}
	if len(decoded.Tools) != len(identity.Tools) {
		t.Errorf("Tools: got %d, want %d", len(decoded.Tools), len(identity.Tools))
	}
	if decoded.Metadata["team"] != "cx" {
		t.Errorf("Metadata[team]: got %q, want %q", decoded.Metadata["team"], "cx")
	}
}

func TestAgentStatusJSON(t *testing.T) {
	status := AgentStatus{
		ID:             "support",
		Name:           "Support Agent",
		Provider:       "openai",
		Model:          "gpt-4",
		ActiveSessions: 5,
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded AgentStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ID != status.ID {
		t.Errorf("ID: got %q, want %q", decoded.ID, status.ID)
	}
	if decoded.ActiveSessions != 5 {
		t.Errorf("ActiveSessions: got %d, want 5", decoded.ActiveSessions)
	}
}

func TestAgentIdentityZeroValue(t *testing.T) {
	var identity AgentIdentity
	data, err := json.Marshal(identity)
	if err != nil {
		t.Fatalf("marshal zero value: %v", err)
	}

	var decoded AgentIdentity
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal zero value: %v", err)
	}
	if decoded.ID != "" {
		t.Errorf("expected empty ID, got %q", decoded.ID)
	}
	if decoded.Tools != nil {
		t.Errorf("expected nil Tools, got %v", decoded.Tools)
	}
}
