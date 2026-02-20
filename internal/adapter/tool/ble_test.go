//go:build edge

package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestBLETool_Scan(t *testing.T) {
	backend := NewMockBLEBackend()
	backend.AddDevice("AA:BB:CC:DD:EE:01", "Sensor1", -45)
	backend.AddDevice("AA:BB:CC:DD:EE:02", "Sensor2", -60)
	tool := NewBLETool(backend, testLogger(t))

	result, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action":  "scan",
		"timeout": 3,
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	var devices []BLEDevice
	if err := json.Unmarshal([]byte(result.Content), &devices); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(devices) != 2 {
		t.Errorf("device count = %d, want 2", len(devices))
	}
}

func TestBLETool_ScanEmpty(t *testing.T) {
	backend := NewMockBLEBackend()
	tool := NewBLETool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "scan",
	}))
	if result.Content != "No BLE devices found" {
		t.Errorf("content = %q", result.Content)
	}
}

func TestBLETool_ConnectDisconnect(t *testing.T) {
	backend := NewMockBLEBackend()
	backend.AddDevice("AA:BB:CC:DD:EE:01", "Sensor1", -45)
	tool := NewBLETool(backend, testLogger(t))

	// Connect.
	result, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action":  "connect",
		"address": "AA:BB:CC:DD:EE:01",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	// Disconnect.
	result, err = tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action":  "disconnect",
		"address": "AA:BB:CC:DD:EE:01",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestBLETool_ConnectUnknownDevice(t *testing.T) {
	backend := NewMockBLEBackend()
	tool := NewBLETool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action":  "connect",
		"address": "FF:FF:FF:FF:FF:FF",
	}))
	if !result.IsError {
		t.Error("expected error for unknown device")
	}
}

func TestBLETool_ReadWriteCharacteristic(t *testing.T) {
	backend := NewMockBLEBackend()
	backend.AddDevice("AA:BB:CC:DD:EE:01", "Sensor1", -45)
	backend.SetCharacteristic("AA:BB:CC:DD:EE:01", "180f", "2a19", []byte{0x64}) // battery level = 100
	tool := NewBLETool(backend, testLogger(t))

	// Connect first.
	tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action":  "connect",
		"address": "AA:BB:CC:DD:EE:01",
	}))

	// Read.
	result, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action":               "read",
		"address":              "AA:BB:CC:DD:EE:01",
		"service_uuid":         "180f",
		"characteristic_uuid":  "2a19",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("read error: %s", result.Content)
	}
	if result.Content != "64" { // hex of 0x64 = 100
		t.Errorf("content = %q, want %q", result.Content, "64")
	}

	// Write.
	result, err = tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action":               "write",
		"address":              "AA:BB:CC:DD:EE:01",
		"service_uuid":         "180f",
		"characteristic_uuid":  "2a19",
		"data":                 "hello",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("write error: %s", result.Content)
	}
}

func TestBLETool_ReadNotConnected(t *testing.T) {
	backend := NewMockBLEBackend()
	backend.AddDevice("AA:BB:CC:DD:EE:01", "Sensor1", -45)
	tool := NewBLETool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action":               "read",
		"address":              "AA:BB:CC:DD:EE:01",
		"service_uuid":         "180f",
		"characteristic_uuid":  "2a19",
	}))
	if !result.IsError {
		t.Error("expected error reading from disconnected device")
	}
}

func TestBLETool_ReadMissingFields(t *testing.T) {
	backend := NewMockBLEBackend()
	tool := NewBLETool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "read",
	}))
	if !result.IsError {
		t.Error("expected error for missing fields")
	}
}

func TestBLETool_ListDevices(t *testing.T) {
	backend := NewMockBLEBackend()
	backend.AddDevice("AA:BB:CC:DD:EE:01", "Sensor1", -45)
	tool := NewBLETool(backend, testLogger(t))

	result, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "list_devices",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	var devices []BLEDevice
	if err := json.Unmarshal([]byte(result.Content), &devices); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(devices) != 1 {
		t.Errorf("device count = %d, want 1", len(devices))
	}
}

func TestBLETool_ListDevicesEmpty(t *testing.T) {
	backend := NewMockBLEBackend()
	tool := NewBLETool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "list_devices",
	}))
	if result.Content != "No known BLE devices" {
		t.Errorf("content = %q", result.Content)
	}
}

func TestBLETool_UnknownAction(t *testing.T) {
	backend := NewMockBLEBackend()
	tool := NewBLETool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "bogus",
	}))
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

func TestBLETool_InvalidParams(t *testing.T) {
	backend := NewMockBLEBackend()
	tool := NewBLETool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), json.RawMessage(`{invalid`))
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestBLETool_ConnectMissingAddress(t *testing.T) {
	backend := NewMockBLEBackend()
	tool := NewBLETool(backend, testLogger(t))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "connect",
	}))
	if !result.IsError {
		t.Error("expected error for missing address")
	}
}

func TestBLETool_WriteMissingData(t *testing.T) {
	backend := NewMockBLEBackend()
	backend.AddDevice("AA:BB:CC:DD:EE:01", "Sensor1", -45)
	tool := NewBLETool(backend, testLogger(t))

	tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action":  "connect",
		"address": "AA:BB:CC:DD:EE:01",
	}))

	result, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"action":               "write",
		"address":              "AA:BB:CC:DD:EE:01",
		"service_uuid":         "180f",
		"characteristic_uuid":  "2a19",
	}))
	if !result.IsError {
		t.Error("expected error for missing data")
	}
}
