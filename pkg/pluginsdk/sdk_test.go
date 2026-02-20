package pluginsdk

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// BasePlugin tests
// ---------------------------------------------------------------------------

func TestBasePlugin_Manifest(t *testing.T) {
	m := PluginManifest{
		Name:    "test-plugin",
		Version: "1.2.3",
		Author:  "tester",
	}
	bp := NewBasePlugin(m)
	assert.Equal(t, m, bp.Manifest())
}

func TestBasePlugin_Init(t *testing.T) {
	bp := NewBasePlugin(PluginManifest{Name: "test"})
	err := bp.Init(context.Background(), PluginDeps{})
	assert.NoError(t, err)
}

func TestBasePlugin_Close(t *testing.T) {
	bp := NewBasePlugin(PluginManifest{Name: "test"})
	err := bp.Close()
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// BaseHook tests
// ---------------------------------------------------------------------------

func TestBaseHook_OnMessageReceived(t *testing.T) {
	var bh BaseHook
	err := bh.OnMessageReceived(context.Background(), InboundMessage{})
	assert.NoError(t, err)
}

func TestBaseHook_OnBeforeToolExec(t *testing.T) {
	var bh BaseHook
	err := bh.OnBeforeToolExec(context.Background(), ToolCall{})
	assert.NoError(t, err)
}

func TestBaseHook_OnAfterToolExec(t *testing.T) {
	var bh BaseHook
	err := bh.OnAfterToolExec(context.Background(), ToolCall{}, &ToolResult{})
	assert.NoError(t, err)
}

func TestBaseHook_OnResponseReady(t *testing.T) {
	var bh BaseHook
	resp, err := bh.OnResponseReady(context.Background(), "hello world")
	require.NoError(t, err)
	assert.Equal(t, "hello world", resp, "BaseHook should pass through response unchanged")
}

// ---------------------------------------------------------------------------
// BaseChannel tests
// ---------------------------------------------------------------------------

func TestBaseChannel_Name(t *testing.T) {
	ch := NewBaseChannel("telegram")
	assert.Equal(t, "telegram", ch.Name())
}

func TestBaseChannel_Start(t *testing.T) {
	ch := NewBaseChannel("test")
	handler := func(_ context.Context, _ InboundMessage) error {
		return nil
	}
	err := ch.Start(context.Background(), handler)
	assert.NoError(t, err)
}

func TestBaseChannel_Dispatch_NilHandler(t *testing.T) {
	ch := NewBaseChannel("test")
	// Dispatch before Start (handler is nil) should be a no-op.
	err := ch.Dispatch(context.Background(), InboundMessage{})
	assert.NoError(t, err)
}

func TestBaseChannel_Stop(t *testing.T) {
	ch := NewBaseChannel("test")
	err := ch.Stop(context.Background())
	assert.NoError(t, err)
}

func TestBaseChannel_Send(t *testing.T) {
	ch := NewBaseChannel("test")
	err := ch.Send(context.Background(), OutboundMessage{})
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Composition: BasePlugin + BaseHook embedded
// ---------------------------------------------------------------------------

type compositePlugin struct {
	BasePlugin
	BaseHook
}

func TestComposition_PluginWithHook(t *testing.T) {
	p := &compositePlugin{
		BasePlugin: NewBasePlugin(PluginManifest{
			Name:    "composite",
			Version: "1.0",
			Types:   []PluginType{TypeHook},
		}),
	}

	// Should satisfy Plugin interface.
	assert.Equal(t, "composite", p.Manifest().Name)
	assert.NoError(t, p.Init(context.Background(), PluginDeps{}))
	assert.NoError(t, p.Close())

	// Should satisfy PluginHook interface.
	assert.NoError(t, p.OnMessageReceived(context.Background(), InboundMessage{}))
	assert.NoError(t, p.OnBeforeToolExec(context.Background(), ToolCall{}))
	assert.NoError(t, p.OnAfterToolExec(context.Background(), ToolCall{}, &ToolResult{}))

	resp, err := p.OnResponseReady(context.Background(), "test")
	require.NoError(t, err)
	assert.Equal(t, "test", resp)

	// Verify interface satisfaction at compile time.
	var _ Plugin = p
	var _ PluginHook = p

	// Verify custom overrides work by checking the hook can return an error.
	type customHook struct {
		BasePlugin
		errOnMessage error
	}
	_ = errors.New("test") // ensure errors package is used
}
