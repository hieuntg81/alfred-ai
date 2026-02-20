// Package pluginsdk provides types and helpers for alfred-ai plugin developers.
//
// NOTE: This package imports internal/domain via type aliases. It is usable by
// plugins that live inside the alfred-ai module (in-tree plugins). External,
// out-of-tree plugin authors cannot import internal/ packages directly; once the
// gRPC plugin protocol is available, they should use the generated protobuf
// types from pkg/pluginsdk/proto instead.
package pluginsdk

import (
	"context"

	"alfred-ai/internal/domain"
)

// Re-exported domain types for plugin developers.
type (
	Plugin          = domain.Plugin
	PluginManifest  = domain.PluginManifest
	PluginDeps      = domain.PluginDeps
	PluginHook      = domain.PluginHook
	PluginType      = domain.PluginType
	InboundMessage  = domain.InboundMessage
	OutboundMessage = domain.OutboundMessage
	MessageHandler  = domain.MessageHandler
	Channel         = domain.Channel
	Media           = domain.Media
	MediaType       = domain.MediaType
	ToolCall        = domain.ToolCall
	ToolResult      = domain.ToolResult

	// WASM plugin types (Phase 6).
	WASMPluginConfig = domain.WASMPluginConfig

	// Node system types (Phase 5).
	Node             = domain.Node
	NodeCapability   = domain.NodeCapability
	NodeStatus       = domain.NodeStatus
	NodeManager      = domain.NodeManager
	NodeTokenManager = domain.NodeTokenManager
)

// Re-exported plugin type constants.
const (
	TypeChannel = domain.PluginTypeChannel
	TypeTool    = domain.PluginTypeTool
	TypeMemory  = domain.PluginTypeMemory
	TypeLLM     = domain.PluginTypeLLM
	TypeHook    = domain.PluginTypeHook
	TypeSkill   = domain.PluginTypeSkill
	TypeWASM    = domain.PluginTypeWASM
)

// Re-exported media type constants.
const (
	MediaTypeImage    = domain.MediaTypeImage
	MediaTypeAudio    = domain.MediaTypeAudio
	MediaTypeVideo    = domain.MediaTypeVideo
	MediaTypeFile     = domain.MediaTypeFile
	MediaTypeLocation = domain.MediaTypeLocation
)

// BasePlugin provides default no-op implementations for the Plugin interface.
// Embed this in your plugin struct to only override the methods you need.
type BasePlugin struct {
	manifest PluginManifest
}

// NewBasePlugin creates a BasePlugin with the given manifest.
func NewBasePlugin(m PluginManifest) BasePlugin {
	return BasePlugin{manifest: m}
}

func (b BasePlugin) Manifest() PluginManifest                   { return b.manifest }
func (b BasePlugin) Init(_ context.Context, _ PluginDeps) error { return nil }
func (b BasePlugin) Close() error                               { return nil }

// BaseHook provides default no-op implementations for PluginHook.
// Embed this in your plugin struct to only override the hooks you need.
type BaseHook struct{}

func (BaseHook) OnMessageReceived(_ context.Context, _ InboundMessage) error { return nil }
func (BaseHook) OnBeforeToolExec(_ context.Context, _ ToolCall) error        { return nil }
func (BaseHook) OnAfterToolExec(_ context.Context, _ ToolCall, _ *ToolResult) error {
	return nil
}
func (BaseHook) OnResponseReady(_ context.Context, response string) (string, error) {
	return response, nil
}

// BaseChannel provides a partial Channel implementation for plugin authors.
// Embed this in your channel struct and override the methods you need.
type BaseChannel struct {
	name    string
	handler MessageHandler
}

// NewBaseChannel creates a BaseChannel with the given name.
func NewBaseChannel(name string) BaseChannel {
	return BaseChannel{name: name}
}

// Name returns the channel name.
func (b BaseChannel) Name() string { return b.name }

// Start stores the handler for later use. Override this to add transport setup.
func (b *BaseChannel) Start(_ context.Context, handler MessageHandler) error {
	b.handler = handler
	return nil
}

// Stop is a no-op. Override to add cleanup.
func (b *BaseChannel) Stop(_ context.Context) error { return nil }

// Send is a no-op. Override to add outbound message delivery.
func (b *BaseChannel) Send(_ context.Context, _ OutboundMessage) error { return nil }

// Dispatch invokes the stored handler with the given message.
// Use this from your transport layer when a message arrives.
func (b *BaseChannel) Dispatch(ctx context.Context, msg InboundMessage) error {
	if b.handler == nil {
		return nil
	}
	return b.handler(ctx, msg)
}
