//go:build !edge

package tool

// Serial types are only available in edge builds.
// This file provides the SerialBackend interface and SerialPortInfo type so that
// non-edge code can reference them without compilation errors.

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
	Name        string `json:"name"`
	Description string `json:"description"`
	IsOpen      bool   `json:"is_open"`
}
