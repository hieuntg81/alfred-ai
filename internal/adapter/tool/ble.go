//go:build edge

package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"alfred-ai/internal/domain"
)

// BLEBackend abstracts Bluetooth Low Energy operations for testability.
type BLEBackend interface {
	Scan(ctx context.Context, timeout int) ([]BLEDevice, error)
	Connect(ctx context.Context, address string) error
	Disconnect(address string) error
	ReadCharacteristic(ctx context.Context, address, serviceUUID, charUUID string) ([]byte, error)
	WriteCharacteristic(ctx context.Context, address, serviceUUID, charUUID string, data []byte) error
	ListDevices() []BLEDevice
}

// BLEDevice describes a discovered BLE device.
type BLEDevice struct {
	Address   string `json:"address"`
	Name      string `json:"name,omitempty"`
	RSSI      int    `json:"rssi,omitempty"`
	Connected bool   `json:"connected"`
}

// BLETool provides BLE device communication for edge/IoT devices.
type BLETool struct {
	backend BLEBackend
	logger  *slog.Logger
	mu      sync.Mutex
}

// NewBLETool creates a BLE tool backed by the given backend.
func NewBLETool(backend BLEBackend, logger *slog.Logger) *BLETool {
	return &BLETool{backend: backend, logger: logger}
}

func (t *BLETool) Name() string        { return "ble" }
func (t *BLETool) Description() string  { return "Communicate with Bluetooth Low Energy devices (scan, connect, read/write characteristics)." }

func (t *BLETool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["scan", "connect", "disconnect", "read", "write", "list_devices"],
					"description": "The BLE action to perform."
				},
				"address": {
					"type": "string",
					"description": "Device MAC address (required for connect, disconnect, read, write)."
				},
				"service_uuid": {
					"type": "string",
					"description": "GATT service UUID (required for read, write)."
				},
				"characteristic_uuid": {
					"type": "string",
					"description": "GATT characteristic UUID (required for read, write)."
				},
				"data": {
					"type": "string",
					"description": "Data to write as hex string (required for write)."
				},
				"timeout": {
					"type": "integer",
					"description": "Scan timeout in seconds (default: 5)."
				}
			},
			"required": ["action"]
		}`),
	}
}

type bleParams struct {
	Action             string `json:"action"`
	Address            string `json:"address"`
	ServiceUUID        string `json:"service_uuid"`
	CharacteristicUUID string `json:"characteristic_uuid"`
	Data               string `json:"data"`
	Timeout            int    `json:"timeout"`
}

func (t *BLETool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	p, errResult := ParseParams[bleParams](params)
	if errResult != nil {
		return errResult, nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	switch p.Action {
	case "scan":
		return t.scan(ctx, p)
	case "connect":
		return t.connect(ctx, p)
	case "disconnect":
		return t.disconnect(p)
	case "read":
		return t.readChar(ctx, p)
	case "write":
		return t.writeChar(ctx, p)
	case "list_devices":
		return t.listDevices()
	default:
		return &domain.ToolResult{
			Content: fmt.Sprintf("unknown action %q (want: scan, connect, disconnect, read, write, list_devices)", p.Action),
			IsError: true,
		}, nil
	}
}

func (t *BLETool) scan(ctx context.Context, p bleParams) (*domain.ToolResult, error) {
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 5
	}
	devices, err := t.backend.Scan(ctx, timeout)
	if err != nil {
		return &domain.ToolResult{Content: "scan failed: " + err.Error(), IsError: true}, nil
	}
	if len(devices) == 0 {
		return &domain.ToolResult{Content: "No BLE devices found"}, nil
	}
	t.logger.Info("ble scan completed", "devices", len(devices))
	data, _ := json.Marshal(devices)
	return &domain.ToolResult{Content: string(data)}, nil
}

func (t *BLETool) connect(ctx context.Context, p bleParams) (*domain.ToolResult, error) {
	if p.Address == "" {
		return &domain.ToolResult{Content: "'address' is required for connect", IsError: true}, nil
	}
	if err := t.backend.Connect(ctx, p.Address); err != nil {
		return &domain.ToolResult{Content: fmt.Sprintf("connect %s: %v", p.Address, err), IsError: true}, nil
	}
	t.logger.Info("ble connected", "address", p.Address)
	return &domain.ToolResult{Content: fmt.Sprintf("Connected to %s", p.Address)}, nil
}

func (t *BLETool) disconnect(p bleParams) (*domain.ToolResult, error) {
	if p.Address == "" {
		return &domain.ToolResult{Content: "'address' is required for disconnect", IsError: true}, nil
	}
	if err := t.backend.Disconnect(p.Address); err != nil {
		return &domain.ToolResult{Content: fmt.Sprintf("disconnect %s: %v", p.Address, err), IsError: true}, nil
	}
	t.logger.Info("ble disconnected", "address", p.Address)
	return &domain.ToolResult{Content: fmt.Sprintf("Disconnected from %s", p.Address)}, nil
}

func (t *BLETool) readChar(ctx context.Context, p bleParams) (*domain.ToolResult, error) {
	if p.Address == "" || p.ServiceUUID == "" || p.CharacteristicUUID == "" {
		return &domain.ToolResult{
			Content: "'address', 'service_uuid', and 'characteristic_uuid' are required for read",
			IsError: true,
		}, nil
	}
	data, err := t.backend.ReadCharacteristic(ctx, p.Address, p.ServiceUUID, p.CharacteristicUUID)
	if err != nil {
		return &domain.ToolResult{Content: fmt.Sprintf("read characteristic: %v", err), IsError: true}, nil
	}
	t.logger.Info("ble read", "address", p.Address, "service", p.ServiceUUID, "char", p.CharacteristicUUID, "bytes", len(data))
	return &domain.ToolResult{Content: fmt.Sprintf("%x", data)}, nil
}

func (t *BLETool) writeChar(ctx context.Context, p bleParams) (*domain.ToolResult, error) {
	if p.Address == "" || p.ServiceUUID == "" || p.CharacteristicUUID == "" {
		return &domain.ToolResult{
			Content: "'address', 'service_uuid', and 'characteristic_uuid' are required for write",
			IsError: true,
		}, nil
	}
	if p.Data == "" {
		return &domain.ToolResult{Content: "'data' is required for write", IsError: true}, nil
	}
	if err := t.backend.WriteCharacteristic(ctx, p.Address, p.ServiceUUID, p.CharacteristicUUID, []byte(p.Data)); err != nil {
		return &domain.ToolResult{Content: fmt.Sprintf("write characteristic: %v", err), IsError: true}, nil
	}
	t.logger.Info("ble write", "address", p.Address, "service", p.ServiceUUID, "char", p.CharacteristicUUID, "bytes", len(p.Data))
	return &domain.ToolResult{Content: fmt.Sprintf("Wrote %d bytes to %s/%s", len(p.Data), p.ServiceUUID, p.CharacteristicUUID)}, nil
}

func (t *BLETool) listDevices() (*domain.ToolResult, error) {
	devices := t.backend.ListDevices()
	if len(devices) == 0 {
		return &domain.ToolResult{Content: "No known BLE devices"}, nil
	}
	data, _ := json.Marshal(devices)
	return &domain.ToolResult{Content: string(data)}, nil
}

// --- Mock backend for testing ---

// MockBLEBackend is a test double for BLEBackend.
type MockBLEBackend struct {
	mu         sync.Mutex
	devices    map[string]*mockBLEDevice
	scanResult []BLEDevice
}

type mockBLEDevice struct {
	info           BLEDevice
	characteristics map[string]map[string][]byte // serviceUUID → charUUID → data
}

// NewMockBLEBackend creates a mock BLE backend.
func NewMockBLEBackend() *MockBLEBackend {
	return &MockBLEBackend{
		devices: make(map[string]*mockBLEDevice),
	}
}

// AddDevice adds a discoverable device to the mock.
func (m *MockBLEBackend) AddDevice(address, name string, rssi int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dev := &mockBLEDevice{
		info:           BLEDevice{Address: address, Name: name, RSSI: rssi},
		characteristics: make(map[string]map[string][]byte),
	}
	m.devices[address] = dev
	m.scanResult = append(m.scanResult, dev.info)
}

// SetCharacteristic sets the value of a GATT characteristic.
func (m *MockBLEBackend) SetCharacteristic(address, serviceUUID, charUUID string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dev, ok := m.devices[address]
	if !ok {
		return
	}
	if dev.characteristics[serviceUUID] == nil {
		dev.characteristics[serviceUUID] = make(map[string][]byte)
	}
	dev.characteristics[serviceUUID][charUUID] = data
}

func (m *MockBLEBackend) Scan(_ context.Context, _ int) ([]BLEDevice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]BLEDevice{}, m.scanResult...), nil
}

func (m *MockBLEBackend) Connect(_ context.Context, address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	dev, ok := m.devices[address]
	if !ok {
		return fmt.Errorf("device %s not found", address)
	}
	if dev.info.Connected {
		return fmt.Errorf("device %s already connected", address)
	}
	dev.info.Connected = true
	return nil
}

func (m *MockBLEBackend) Disconnect(address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	dev, ok := m.devices[address]
	if !ok {
		return fmt.Errorf("device %s not found", address)
	}
	if !dev.info.Connected {
		return fmt.Errorf("device %s not connected", address)
	}
	dev.info.Connected = false
	return nil
}

func (m *MockBLEBackend) ReadCharacteristic(_ context.Context, address, serviceUUID, charUUID string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dev, ok := m.devices[address]
	if !ok {
		return nil, fmt.Errorf("device %s not found", address)
	}
	if !dev.info.Connected {
		return nil, fmt.Errorf("device %s not connected", address)
	}
	svc, ok := dev.characteristics[serviceUUID]
	if !ok {
		return nil, fmt.Errorf("service %s not found", serviceUUID)
	}
	data, ok := svc[charUUID]
	if !ok {
		return nil, fmt.Errorf("characteristic %s not found", charUUID)
	}
	return data, nil
}

func (m *MockBLEBackend) WriteCharacteristic(_ context.Context, address, serviceUUID, charUUID string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	dev, ok := m.devices[address]
	if !ok {
		return fmt.Errorf("device %s not found", address)
	}
	if !dev.info.Connected {
		return fmt.Errorf("device %s not connected", address)
	}
	if dev.characteristics[serviceUUID] == nil {
		dev.characteristics[serviceUUID] = make(map[string][]byte)
	}
	dev.characteristics[serviceUUID][charUUID] = append([]byte{}, data...)
	return nil
}

func (m *MockBLEBackend) ListDevices() []BLEDevice {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []BLEDevice
	for _, dev := range m.devices {
		result = append(result, dev.info)
	}
	return result
}
