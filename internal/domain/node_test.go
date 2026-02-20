package domain

import (
	"encoding/json"
	"testing"
)

func TestNodeStatusConstants(t *testing.T) {
	if NodeStatusOnline != "online" {
		t.Errorf("NodeStatusOnline = %q", NodeStatusOnline)
	}
	if NodeStatusOffline != "offline" {
		t.Errorf("NodeStatusOffline = %q", NodeStatusOffline)
	}
	if NodeStatusUnreachable != "unreachable" {
		t.Errorf("NodeStatusUnreachable = %q", NodeStatusUnreachable)
	}
}

func TestNodeJSONOmitsDeviceToken(t *testing.T) {
	n := Node{
		ID:          "node-1",
		Name:        "test-node",
		Platform:    "linux",
		Address:     "192.168.1.10:9090",
		Status:      NodeStatusOnline,
		DeviceToken: "secret-token-value",
		Metadata:    map[string]string{"env": "prod"},
	}

	data, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// DeviceToken has json:"-" so it must not appear.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal raw: %v", err)
	}
	if _, exists := raw["DeviceToken"]; exists {
		t.Error("DeviceToken should not appear in JSON")
	}
	if _, exists := raw["device_token"]; exists {
		t.Error("device_token should not appear in JSON")
	}

	// Round-trip should preserve other fields.
	var decoded Node
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.ID != "node-1" {
		t.Errorf("ID = %q", decoded.ID)
	}
	if decoded.DeviceToken != "" {
		t.Errorf("DeviceToken should be empty after round-trip, got %q", decoded.DeviceToken)
	}
}

func TestNodeCapabilityRoundTrip(t *testing.T) {
	cap := NodeCapability{
		Name:        "execute_command",
		Description: "Run a shell command on the node",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"cmd":{"type":"string"}}}`),
	}

	data, err := json.Marshal(cap)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded NodeCapability
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Name != cap.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, cap.Name)
	}
	if decoded.Description != cap.Description {
		t.Errorf("Description = %q, want %q", decoded.Description, cap.Description)
	}
	if string(decoded.Parameters) != string(cap.Parameters) {
		t.Errorf("Parameters = %s, want %s", decoded.Parameters, cap.Parameters)
	}
}
