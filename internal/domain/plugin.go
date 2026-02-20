package domain

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// PluginType classifies what a plugin provides.
type PluginType string

const (
	PluginTypeChannel PluginType = "channel"
	PluginTypeTool    PluginType = "tool"
	PluginTypeMemory  PluginType = "memory"
	PluginTypeLLM     PluginType = "llm"
	PluginTypeHook    PluginType = "hook"
	PluginTypeSkill   PluginType = "skill"
	PluginTypeWASM    PluginType = "wasm"
)

// PluginManifest describes a plugin's identity and capabilities.
type PluginManifest struct {
	Name        string            `json:"name"        yaml:"name"`
	Version     string            `json:"version"     yaml:"version"`
	Description string            `json:"description" yaml:"description"`
	Author      string            `json:"author"      yaml:"author"`
	Types       []PluginType      `json:"types"       yaml:"types"`
	Permissions []string          `json:"permissions" yaml:"permissions"`
	WASMConfig  *WASMPluginConfig `json:"wasm,omitempty" yaml:"wasm,omitempty"`
}

// WASMPluginConfig holds configuration for a WASM plugin.
type WASMPluginConfig struct {
	Binary       string        `json:"binary"        yaml:"binary"`         // path to .wasm file (relative to plugin dir)
	MaxMemoryMB  int           `json:"max_memory_mb" yaml:"max_memory_mb"`  // default 64
	ExecTimeout  time.Duration `json:"exec_timeout"  yaml:"exec_timeout"`   // default 30s
	Capabilities []string      `json:"capabilities"  yaml:"capabilities"`   // allowed host functions
}

// PluginHook provides lifecycle hooks that plugins can implement.
type PluginHook interface {
	OnMessageReceived(ctx context.Context, msg InboundMessage) error
	OnBeforeToolExec(ctx context.Context, call ToolCall) error
	OnAfterToolExec(ctx context.Context, call ToolCall, result *ToolResult) error
	OnResponseReady(ctx context.Context, response string) (string, error)
}

// Plugin is the interface every in-process plugin must implement.
type Plugin interface {
	Manifest() PluginManifest
	Init(ctx context.Context, deps PluginDeps) error
	Close() error
}

// PluginDeps are dependencies injected into a plugin during Init.
type PluginDeps struct {
	Logger   *slog.Logger
	EventBus EventBus
	Config   json.RawMessage
}

// PluginManager handles the lifecycle of plugins.
type PluginManager interface {
	Discover() ([]PluginManifest, error)
	Load(plugin Plugin) error
	Unload(name string) error
	List() []PluginManifest
	GetHooks() []PluginHook
}
