//go:build edge

package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSerialTool_ListPorts(t *testing.T) {
	backend := NewMockSerialBackend()
	backend.AddPort("/dev/ttyUSB0", "USB-Serial CH340")
	backend.AddPort("/dev/ttyACM0", "Arduino Uno")
	tool := NewSerialTool(backend, testLogger(t))

	result, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "list_ports",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	var ports []SerialPortInfo
	if err := json.Unmarshal([]byte(result.Content), &ports); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ports) != 2 {
		t.Errorf("port count = %d, want 2", len(ports))
	}
}

func TestSerialTool_ListPortsEmpty(t *testing.T) {
	backend := NewMockSerialBackend()
	tool := NewSerialTool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "list_ports",
	}))
	if result.Content != "No serial ports found" {
		t.Errorf("content = %q", result.Content)
	}
}

func TestSerialTool_OpenClosePort(t *testing.T) {
	backend := NewMockSerialBackend()
	backend.AddPort("/dev/ttyUSB0", "")
	tool := NewSerialTool(backend, testLogger(t))

	// Open.
	result, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action":    "open",
		"port":      "/dev/ttyUSB0",
		"baud_rate": 115200,
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	// Close.
	result, err = tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "close",
		"port":   "/dev/ttyUSB0",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestSerialTool_OpenNonexistentPort(t *testing.T) {
	backend := NewMockSerialBackend()
	tool := NewSerialTool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "open",
		"port":   "/dev/ttyNOPE",
	}))
	if !result.IsError {
		t.Error("expected error for nonexistent port")
	}
}

func TestSerialTool_ReadWrite(t *testing.T) {
	backend := NewMockSerialBackend()
	backend.AddPort("/dev/ttyUSB0", "")
	tool := NewSerialTool(backend, testLogger(t))

	// Open port first.
	tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "open",
		"port":   "/dev/ttyUSB0",
	}))

	// Write.
	result, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "write",
		"port":   "/dev/ttyUSB0",
		"data":   "AT\r\n",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("write error: %s", result.Content)
	}

	// Inject response data and read.
	backend.InjectData("/dev/ttyUSB0", []byte("OK\r\n"))
	result, err = tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "read",
		"port":   "/dev/ttyUSB0",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("read error: %s", result.Content)
	}
	if result.Content != "OK\r\n" {
		t.Errorf("content = %q, want %q", result.Content, "OK\r\n")
	}
}

func TestSerialTool_ReadEmpty(t *testing.T) {
	backend := NewMockSerialBackend()
	backend.AddPort("/dev/ttyUSB0", "")
	tool := NewSerialTool(backend, testLogger(t))

	tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "open",
		"port":   "/dev/ttyUSB0",
	}))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "read",
		"port":   "/dev/ttyUSB0",
	}))
	if result.Content != "No data available" {
		t.Errorf("content = %q", result.Content)
	}
}

func TestSerialTool_ReadClosedPort(t *testing.T) {
	backend := NewMockSerialBackend()
	backend.AddPort("/dev/ttyUSB0", "")
	tool := NewSerialTool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "read",
		"port":   "/dev/ttyUSB0",
	}))
	if !result.IsError {
		t.Error("expected error reading closed port")
	}
}

func TestSerialTool_WriteMissingPort(t *testing.T) {
	backend := NewMockSerialBackend()
	tool := NewSerialTool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "write",
		"data":   "hello",
	}))
	if !result.IsError {
		t.Error("expected error for missing port")
	}
}

func TestSerialTool_WriteMissingData(t *testing.T) {
	backend := NewMockSerialBackend()
	backend.AddPort("/dev/ttyUSB0", "")
	tool := NewSerialTool(backend, testLogger(t))

	tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "open",
		"port":   "/dev/ttyUSB0",
	}))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "write",
		"port":   "/dev/ttyUSB0",
	}))
	if !result.IsError {
		t.Error("expected error for missing data")
	}
}

func TestSerialTool_DefaultBaudRate(t *testing.T) {
	backend := NewMockSerialBackend()
	backend.AddPort("/dev/ttyUSB0", "")
	tool := NewSerialTool(backend, testLogger(t))

	// Open without baud_rate — should default to 9600.
	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "open",
		"port":   "/dev/ttyUSB0",
	}))
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != "Opened /dev/ttyUSB0 at 9600 baud" {
		t.Errorf("content = %q", result.Content)
	}
}

func TestSerialTool_UnknownAction(t *testing.T) {
	backend := NewMockSerialBackend()
	tool := NewSerialTool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "bogus",
	}))
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

func TestSerialTool_InvalidParams(t *testing.T) {
	backend := NewMockSerialBackend()
	tool := NewSerialTool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), json.RawMessage(`{invalid`))
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}
