// Package wasm provides a guest SDK for building alfred-ai WASM plugins.
//
// This package is designed for use with TinyGo and the WASI target.
// It provides host function bindings, memory management exports, and
// lifecycle hooks that the alfred-ai WASM runtime expects.
//
// Usage (in a TinyGo plugin):
//
//	//go:build tinygo
//
//	package main
//
//	import "unsafe"
//
//	// Import host functions from alfred_v1 module:
//	//go:wasmimport alfred_v1 log
//	func hostLog(level int32, ptr uintptr, size uint32)
//
//	// Export required memory management:
//	//export malloc
//	func malloc(size uint32) uintptr { ... }
//
//	//export free
//	func free(ptr uintptr, size uint32) { ... }
//
//	// Export plugin lifecycle hooks:
//	//export _init
//	func pluginInit() { ... }
//
//	//export tool_execute
//	func toolExecute(ptr uintptr, size uint32) { ... }
//
// # Host Functions (alfred_v1 module)
//
// The following host functions are available for import:
//
//   - log(level int32, ptr uintptr, len uint32)
//     Write a log message. Levels: 0=debug, 1=info, 2=warn, 3=error.
//
//   - get_config(key_ptr uintptr, key_len uint32) (ptr uintptr, len uint32)
//     Read plugin configuration JSON. Returns a pointer and length in guest memory.
//
//   - emit_event(type_ptr uintptr, type_len uint32, payload_ptr uintptr, payload_len uint32)
//     Publish an event to the alfred-ai event bus. Requires "event_bus" capability.
//
//   - tool_result(ptr uintptr, len uint32)
//     Write tool execution result JSON back to the host. Requires "tool" capability.
//
// # Required Exports
//
// The guest module must export:
//
//   - malloc(size uint32) uintptr — allocate memory for host-to-guest data transfer
//   - free(ptr uintptr, size uint32) — free memory (can be no-op with GC)
//
// # Optional Exports
//
//   - _init() — called once when the plugin is loaded
//   - _close() — called when the plugin is unloaded
//   - tool_execute(ptr uintptr, size uint32) — handle tool execution
//   - tool_schema() (ptr uintptr, size uint32) — return JSON schema for tool parameters
//   - on_message_received(ptr uintptr, size uint32) — hook: message received
//   - on_before_tool_exec(ptr uintptr, size uint32) — hook: before tool execution
//   - on_after_tool_exec(ptr uintptr, size uint32) — hook: after tool execution
//   - on_response_ready(ptr uintptr, size uint32) (ptr uintptr, size uint32) — hook: modify response
//
// # Capabilities
//
// Capabilities control which host functions a plugin can access:
//
//   - "log" — always allowed
//   - "config" — always allowed
//   - "event_bus" — must be declared in plugin.yaml capabilities
//   - "tool" — must be declared in plugin.yaml capabilities
package wasm

// LogLevel constants for the host log function.
const (
	LogDebug int32 = 0
	LogInfo  int32 = 1
	LogWarn  int32 = 2
	LogError int32 = 3
)
