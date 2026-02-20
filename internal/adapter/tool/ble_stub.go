//go:build !edge

package tool

import "context"

// BLE types are only available in edge builds.
// This file provides the BLEBackend interface and BLEDevice type so that
// non-edge code can reference them without compilation errors.

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
