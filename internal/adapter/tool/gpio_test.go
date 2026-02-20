//go:build edge

package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestGPIOTool_Read(t *testing.T) {
	backend := NewMockGPIOBackend()
	backend.AddPin(17, "input", 1)
	tool := NewGPIOTool(backend, testLogger(t))

	result, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "read",
		"pin":    17,
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != `{"pin":17,"value":1}` {
		t.Errorf("content = %q, want pin 17 value 1", result.Content)
	}
}

func TestGPIOTool_ReadUnconfiguredPin(t *testing.T) {
	backend := NewMockGPIOBackend()
	tool := NewGPIOTool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "read",
		"pin":    99,
	}))
	if !result.IsError {
		t.Error("expected error for unconfigured pin")
	}
}

func TestGPIOTool_Write(t *testing.T) {
	backend := NewMockGPIOBackend()
	tool := NewGPIOTool(backend, testLogger(t))

	result, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "write",
		"pin":    18,
		"value":  1,
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	// Verify pin was set.
	val, err := backend.Read(18)
	if err != nil {
		t.Fatalf("Read after write: %v", err)
	}
	if val != 1 {
		t.Errorf("value = %d, want 1", val)
	}
}

func TestGPIOTool_WriteInvalidValue(t *testing.T) {
	backend := NewMockGPIOBackend()
	tool := NewGPIOTool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "write",
		"pin":    18,
		"value":  5,
	}))
	if !result.IsError {
		t.Error("expected error for invalid value")
	}
}

func TestGPIOTool_PWM(t *testing.T) {
	backend := NewMockGPIOBackend()
	tool := NewGPIOTool(backend, testLogger(t))

	result, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action":     "pwm",
		"pin":        12,
		"duty_cycle": 0.75,
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestGPIOTool_PWMInvalidDutyCycle(t *testing.T) {
	backend := NewMockGPIOBackend()
	tool := NewGPIOTool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action":     "pwm",
		"pin":        12,
		"duty_cycle": 1.5,
	}))
	if !result.IsError {
		t.Error("expected error for invalid duty cycle")
	}
}

func TestGPIOTool_ListPins(t *testing.T) {
	backend := NewMockGPIOBackend()
	backend.AddPin(17, "input", 0)
	backend.AddPin(18, "output", 1)
	tool := NewGPIOTool(backend, testLogger(t))

	result, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "list_pins",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	var pins []GPIOPinInfo
	if err := json.Unmarshal([]byte(result.Content), &pins); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(pins) != 2 {
		t.Errorf("pin count = %d, want 2", len(pins))
	}
}

func TestGPIOTool_ListPinsEmpty(t *testing.T) {
	backend := NewMockGPIOBackend()
	tool := NewGPIOTool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "list_pins",
	}))
	if result.Content != "No GPIO pins available" {
		t.Errorf("content = %q", result.Content)
	}
}

func TestGPIOTool_UnknownAction(t *testing.T) {
	backend := NewMockGPIOBackend()
	tool := NewGPIOTool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "bogus",
	}))
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

func TestGPIOTool_InvalidParams(t *testing.T) {
	backend := NewMockGPIOBackend()
	tool := NewGPIOTool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), json.RawMessage(`{invalid`))
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}
