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

// SerialBackend abstracts serial port operations for testability.
type SerialBackend interface {
	ListPorts() ([]SerialPortInfo, error)
	Open(port string, baudRate int) error
	Close(port string) error
	Read(port string, maxBytes int) ([]byte, error)
	Write(port string, data []byte) error
}

// SerialPortInfo describes an available serial port.
type SerialPortInfo struct {
	Name        string `json:"name"`         // e.g. "/dev/ttyUSB0"
	Description string `json:"description"`  // vendor/product description
	IsOpen      bool   `json:"is_open"`
}

// SerialTool provides serial port communication for edge/IoT devices.
type SerialTool struct {
	backend SerialBackend
	logger  *slog.Logger
	mu      sync.Mutex
}

// NewSerialTool creates a serial tool backed by the given backend.
func NewSerialTool(backend SerialBackend, logger *slog.Logger) *SerialTool {
	return &SerialTool{backend: backend, logger: logger}
}

func (t *SerialTool) Name() string        { return "serial" }
func (t *SerialTool) Description() string  { return "Communicate with devices via USB serial ports (open, close, read, write, list)." }

func (t *SerialTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["list_ports", "open", "close", "read", "write"],
					"description": "The serial action to perform."
				},
				"port": {
					"type": "string",
					"description": "Serial port name (e.g. /dev/ttyUSB0). Required for open, close, read, write."
				},
				"baud_rate": {
					"type": "integer",
					"description": "Baud rate for opening the port (default: 9600)."
				},
				"data": {
					"type": "string",
					"description": "Data to write (required for write)."
				},
				"max_bytes": {
					"type": "integer",
					"description": "Maximum bytes to read (default: 1024)."
				}
			},
			"required": ["action"]
		}`),
	}
}

type serialParams struct {
	Action   string `json:"action"`
	Port     string `json:"port"`
	BaudRate int    `json:"baud_rate"`
	Data     string `json:"data"`
	MaxBytes int    `json:"max_bytes"`
}

func (t *SerialTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	p, errResult := ParseParams[serialParams](params)
	if errResult != nil {
		return errResult, nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	switch p.Action {
	case "list_ports":
		return t.listPorts()
	case "open":
		return t.openPort(p)
	case "close":
		return t.closePort(p)
	case "read":
		return t.readPort(p)
	case "write":
		return t.writePort(p)
	default:
		return &domain.ToolResult{
			Content: fmt.Sprintf("unknown action %q (want: list_ports, open, close, read, write)", p.Action),
			IsError: true,
		}, nil
	}
}

func (t *SerialTool) listPorts() (*domain.ToolResult, error) {
	ports, err := t.backend.ListPorts()
	if err != nil {
		return &domain.ToolResult{Content: "list ports: " + err.Error(), IsError: true}, nil
	}
	if len(ports) == 0 {
		return &domain.ToolResult{Content: "No serial ports found"}, nil
	}
	data, _ := json.Marshal(ports)
	return &domain.ToolResult{Content: string(data)}, nil
}

func (t *SerialTool) openPort(p serialParams) (*domain.ToolResult, error) {
	if err := RequireField("port", p.Port); err != nil {
		return &domain.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	baudRate := p.BaudRate
	if baudRate <= 0 {
		baudRate = 9600
	}
	if err := t.backend.Open(p.Port, baudRate); err != nil {
		return &domain.ToolResult{Content: fmt.Sprintf("open %s: %v", p.Port, err), IsError: true}, nil
	}
	t.logger.Info("serial port opened", "port", p.Port, "baud_rate", baudRate)
	return &domain.ToolResult{Content: fmt.Sprintf("Opened %s at %d baud", p.Port, baudRate)}, nil
}

func (t *SerialTool) closePort(p serialParams) (*domain.ToolResult, error) {
	if err := RequireField("port", p.Port); err != nil {
		return &domain.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	if err := t.backend.Close(p.Port); err != nil {
		return &domain.ToolResult{Content: fmt.Sprintf("close %s: %v", p.Port, err), IsError: true}, nil
	}
	t.logger.Info("serial port closed", "port", p.Port)
	return &domain.ToolResult{Content: fmt.Sprintf("Closed %s", p.Port)}, nil
}

func (t *SerialTool) readPort(p serialParams) (*domain.ToolResult, error) {
	if err := RequireField("port", p.Port); err != nil {
		return &domain.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	maxBytes := p.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 1024
	}
	data, err := t.backend.Read(p.Port, maxBytes)
	if err != nil {
		return &domain.ToolResult{Content: fmt.Sprintf("read %s: %v", p.Port, err), IsError: true}, nil
	}
	if len(data) == 0 {
		return &domain.ToolResult{Content: "No data available"}, nil
	}
	return &domain.ToolResult{Content: string(data)}, nil
}

func (t *SerialTool) writePort(p serialParams) (*domain.ToolResult, error) {
	if err := RequireFields("port", p.Port, "data", p.Data); err != nil {
		return &domain.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	if err := t.backend.Write(p.Port, []byte(p.Data)); err != nil {
		return &domain.ToolResult{Content: fmt.Sprintf("write %s: %v", p.Port, err), IsError: true}, nil
	}
	t.logger.Info("serial write", "port", p.Port, "bytes", len(p.Data))
	return &domain.ToolResult{Content: fmt.Sprintf("Wrote %d bytes to %s", len(p.Data), p.Port)}, nil
}

// --- Mock backend for testing ---

// MockSerialBackend is a test double for SerialBackend.
type MockSerialBackend struct {
	ports   map[string]*mockSerialPort
	portsMu sync.Mutex
}

type mockSerialPort struct {
	name     string
	baudRate int
	isOpen   bool
	buffer   []byte
}

// NewMockSerialBackend creates a mock serial backend.
func NewMockSerialBackend() *MockSerialBackend {
	return &MockSerialBackend{ports: make(map[string]*mockSerialPort)}
}

// AddPort adds a discoverable port to the mock.
func (m *MockSerialBackend) AddPort(name, description string) {
	m.portsMu.Lock()
	defer m.portsMu.Unlock()
	m.ports[name] = &mockSerialPort{name: name}
}

// InjectData injects data into a port's read buffer (for testing).
func (m *MockSerialBackend) InjectData(port string, data []byte) {
	m.portsMu.Lock()
	defer m.portsMu.Unlock()
	if p, ok := m.ports[port]; ok {
		p.buffer = append(p.buffer, data...)
	}
}

func (m *MockSerialBackend) ListPorts() ([]SerialPortInfo, error) {
	m.portsMu.Lock()
	defer m.portsMu.Unlock()
	var result []SerialPortInfo
	for _, p := range m.ports {
		result = append(result, SerialPortInfo{
			Name:   p.name,
			IsOpen: p.isOpen,
		})
	}
	return result, nil
}

func (m *MockSerialBackend) Open(port string, baudRate int) error {
	m.portsMu.Lock()
	defer m.portsMu.Unlock()
	p, ok := m.ports[port]
	if !ok {
		return fmt.Errorf("port %q not found", port)
	}
	if p.isOpen {
		return fmt.Errorf("port %q already open", port)
	}
	p.isOpen = true
	p.baudRate = baudRate
	return nil
}

func (m *MockSerialBackend) Close(port string) error {
	m.portsMu.Lock()
	defer m.portsMu.Unlock()
	p, ok := m.ports[port]
	if !ok {
		return fmt.Errorf("port %q not found", port)
	}
	if !p.isOpen {
		return fmt.Errorf("port %q not open", port)
	}
	p.isOpen = false
	return nil
}

func (m *MockSerialBackend) Read(port string, maxBytes int) ([]byte, error) {
	m.portsMu.Lock()
	defer m.portsMu.Unlock()
	p, ok := m.ports[port]
	if !ok {
		return nil, fmt.Errorf("port %q not found", port)
	}
	if !p.isOpen {
		return nil, fmt.Errorf("port %q not open", port)
	}
	if len(p.buffer) == 0 {
		return nil, nil
	}
	n := len(p.buffer)
	if n > maxBytes {
		n = maxBytes
	}
	data := make([]byte, n)
	copy(data, p.buffer[:n])
	p.buffer = p.buffer[n:]
	return data, nil
}

func (m *MockSerialBackend) Write(port string, data []byte) error {
	m.portsMu.Lock()
	defer m.portsMu.Unlock()
	p, ok := m.ports[port]
	if !ok {
		return fmt.Errorf("port %q not found", port)
	}
	if !p.isOpen {
		return fmt.Errorf("port %q not open", port)
	}
	// In mock, writing is a no-op (data is discarded).
	return nil
}
