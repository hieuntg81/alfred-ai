//go:build !edge

package tool

// GPIO types are only available in edge builds.
// This file provides the GPIOBackend interface and GPIOPinInfo type so that
// non-edge code can reference them without compilation errors.

// GPIOBackend abstracts GPIO pin operations for testability.
type GPIOBackend interface {
	Read(pin int) (int, error)
	Write(pin, value int) error
	SetPWM(pin int, dutyCycle float64) error
	ListPins() []GPIOPinInfo
}

// GPIOPinInfo describes a GPIO pin's current state.
type GPIOPinInfo struct {
	Pin       int     `json:"pin"`
	Mode      string  `json:"mode"`
	Value     int     `json:"value"`
	DutyCycle float64 `json:"duty_cycle,omitempty"`
}
