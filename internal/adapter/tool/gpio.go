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

// GPIOBackend abstracts GPIO pin operations for testability.
type GPIOBackend interface {
	Read(pin int) (int, error)
	Write(pin, value int) error
	SetPWM(pin int, dutyCycle float64) error
	ListPins() []GPIOPinInfo
}

// GPIOPinInfo describes a GPIO pin's current state.
type GPIOPinInfo struct {
	Pin       int    `json:"pin"`
	Mode      string `json:"mode"`  // "input", "output", "pwm"
	Value     int    `json:"value"` // 0 or 1 for digital
	DutyCycle float64 `json:"duty_cycle,omitempty"`
}

// GPIOTool provides GPIO pin control for edge/IoT devices.
type GPIOTool struct {
	backend GPIOBackend
	logger  *slog.Logger
	mu      sync.Mutex
}

// NewGPIOTool creates a GPIO tool backed by the given backend.
func NewGPIOTool(backend GPIOBackend, logger *slog.Logger) *GPIOTool {
	return &GPIOTool{backend: backend, logger: logger}
}

func (t *GPIOTool) Name() string        { return "gpio" }
func (t *GPIOTool) Description() string  { return "Control GPIO pins on edge/IoT devices (read, write, PWM)." }

func (t *GPIOTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["read", "write", "pwm", "list_pins"],
					"description": "The GPIO action to perform."
				},
				"pin": {
					"type": "integer",
					"description": "The GPIO pin number (required for read, write, pwm)."
				},
				"value": {
					"type": "integer",
					"enum": [0, 1],
					"description": "The digital value to write (required for write)."
				},
				"duty_cycle": {
					"type": "number",
					"minimum": 0,
					"maximum": 1,
					"description": "The PWM duty cycle (0.0 to 1.0, required for pwm)."
				}
			},
			"required": ["action"]
		}`),
	}
}

type gpioParams struct {
	Action    string  `json:"action"`
	Pin       int     `json:"pin"`
	Value     int     `json:"value"`
	DutyCycle float64 `json:"duty_cycle"`
}

func (t *GPIOTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	p, errResult := ParseParams[gpioParams](params)
	if errResult != nil {
		return errResult, nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	switch p.Action {
	case "read":
		val, err := t.backend.Read(p.Pin)
		if err != nil {
			return &domain.ToolResult{Content: fmt.Sprintf("read pin %d: %v", p.Pin, err), IsError: true}, nil
		}
		t.logger.Info("gpio read", "pin", p.Pin, "value", val)
		return &domain.ToolResult{Content: fmt.Sprintf(`{"pin":%d,"value":%d}`, p.Pin, val)}, nil

	case "write":
		if p.Value != 0 && p.Value != 1 {
			return &domain.ToolResult{Content: "value must be 0 or 1", IsError: true}, nil
		}
		if err := t.backend.Write(p.Pin, p.Value); err != nil {
			return &domain.ToolResult{Content: fmt.Sprintf("write pin %d: %v", p.Pin, err), IsError: true}, nil
		}
		t.logger.Info("gpio write", "pin", p.Pin, "value", p.Value)
		return &domain.ToolResult{Content: fmt.Sprintf("Pin %d set to %d", p.Pin, p.Value)}, nil

	case "pwm":
		if p.DutyCycle < 0 || p.DutyCycle > 1 {
			return &domain.ToolResult{Content: "duty_cycle must be between 0.0 and 1.0", IsError: true}, nil
		}
		if err := t.backend.SetPWM(p.Pin, p.DutyCycle); err != nil {
			return &domain.ToolResult{Content: fmt.Sprintf("pwm pin %d: %v", p.Pin, err), IsError: true}, nil
		}
		t.logger.Info("gpio pwm", "pin", p.Pin, "duty_cycle", p.DutyCycle)
		return &domain.ToolResult{Content: fmt.Sprintf("Pin %d PWM set to %.2f", p.Pin, p.DutyCycle)}, nil

	case "list_pins":
		pins := t.backend.ListPins()
		if len(pins) == 0 {
			return &domain.ToolResult{Content: "No GPIO pins available"}, nil
		}
		data, _ := json.Marshal(pins)
		return &domain.ToolResult{Content: string(data)}, nil

	default:
		return &domain.ToolResult{
			Content: fmt.Sprintf("unknown action %q (want: read, write, pwm, list_pins)", p.Action),
			IsError: true,
		}, nil
	}
}

// --- Mock backend for testing ---

// MockGPIOBackend is a test double for GPIOBackend.
type MockGPIOBackend struct {
	pins map[int]*GPIOPinInfo
}

// NewMockGPIOBackend creates a mock GPIO backend.
func NewMockGPIOBackend() *MockGPIOBackend {
	return &MockGPIOBackend{pins: make(map[int]*GPIOPinInfo)}
}

// AddPin adds a pin to the mock backend.
func (m *MockGPIOBackend) AddPin(pin int, mode string, value int) {
	m.pins[pin] = &GPIOPinInfo{Pin: pin, Mode: mode, Value: value}
}

func (m *MockGPIOBackend) Read(pin int) (int, error) {
	p, ok := m.pins[pin]
	if !ok {
		return 0, fmt.Errorf("pin %d not configured", pin)
	}
	return p.Value, nil
}

func (m *MockGPIOBackend) Write(pin, value int) error {
	p, ok := m.pins[pin]
	if !ok {
		m.pins[pin] = &GPIOPinInfo{Pin: pin, Mode: "output", Value: value}
		return nil
	}
	p.Value = value
	p.Mode = "output"
	return nil
}

func (m *MockGPIOBackend) SetPWM(pin int, dutyCycle float64) error {
	p, ok := m.pins[pin]
	if !ok {
		m.pins[pin] = &GPIOPinInfo{Pin: pin, Mode: "pwm", DutyCycle: dutyCycle}
		return nil
	}
	p.Mode = "pwm"
	p.DutyCycle = dutyCycle
	return nil
}

func (m *MockGPIOBackend) ListPins() []GPIOPinInfo {
	var pins []GPIOPinInfo
	for _, p := range m.pins {
		pins = append(pins, *p)
	}
	return pins
}
