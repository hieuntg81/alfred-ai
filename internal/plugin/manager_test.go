package plugin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"alfred-ai/internal/domain"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// testPlugin is a mock plugin for testing.
type testPlugin struct {
	manifest domain.PluginManifest
	initErr  error
	closeErr error
	inited   bool
	closed   bool
}

func (p *testPlugin) Manifest() domain.PluginManifest { return p.manifest }
func (p *testPlugin) Init(_ context.Context, _ domain.PluginDeps) error {
	p.inited = true
	return p.initErr
}
func (p *testPlugin) Close() error {
	p.closed = true
	return p.closeErr
}

// testHookPlugin implements both Plugin and PluginHook.
type testHookPlugin struct {
	testPlugin
}

func (p *testHookPlugin) OnMessageReceived(_ context.Context, _ domain.InboundMessage) error {
	return nil
}
func (p *testHookPlugin) OnBeforeToolExec(_ context.Context, _ domain.ToolCall) error { return nil }
func (p *testHookPlugin) OnAfterToolExec(_ context.Context, _ domain.ToolCall, _ *domain.ToolResult) error {
	return nil
}
func (p *testHookPlugin) OnResponseReady(_ context.Context, resp string) (string, error) {
	return resp, nil
}

// mockEventBus records published events for assertions.
type mockEventBus struct {
	mu     sync.Mutex
	events []domain.Event
}

func (b *mockEventBus) Publish(_ context.Context, e domain.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
}
func (b *mockEventBus) Subscribe(_ domain.EventType, _ domain.EventHandler) func() { return func() {} }
func (b *mockEventBus) SubscribeAll(_ domain.EventHandler) func()                  { return func() {} }
func (b *mockEventBus) Close()                                                     {}

func (b *mockEventBus) hasEvent(t domain.EventType) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range b.events {
		if e.Type == t {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Existing tests (migrated to testify)
// ---------------------------------------------------------------------------

func TestManagerLoad(t *testing.T) {
	mgr := NewManager(slog.Default(), nil, nil, nil, nil)
	p := &testPlugin{
		manifest: domain.PluginManifest{Name: "test", Version: "1.0"},
	}

	require.NoError(t, mgr.Load(p))
	assert.True(t, p.inited, "expected Init to be called")
	assert.Len(t, mgr.List(), 1)
}

func TestManagerUnload(t *testing.T) {
	mgr := NewManager(slog.Default(), nil, nil, nil, nil)
	p := &testPlugin{
		manifest: domain.PluginManifest{Name: "test", Version: "1.0"},
	}

	require.NoError(t, mgr.Load(p))
	require.NoError(t, mgr.Unload("test"))
	assert.True(t, p.closed, "expected Close to be called")
	assert.Empty(t, mgr.List())
}

func TestManagerUnloadNotFound(t *testing.T) {
	mgr := NewManager(slog.Default(), nil, nil, nil, nil)
	err := mgr.Unload("nonexistent")
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestManagerPermissions(t *testing.T) {
	mgr := NewManager(slog.Default(), nil, nil, []string{"read"}, []string{"exec"})
	p := &testPlugin{
		manifest: domain.PluginManifest{
			Name:        "bad",
			Permissions: []string{"exec"},
		},
	}

	err := mgr.Load(p)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrPermissionDenied)
	assert.False(t, p.inited, "Init should not be called for denied plugin")
}

func TestManagerHooks(t *testing.T) {
	mgr := NewManager(slog.Default(), nil, nil, nil, nil)
	hp := &testHookPlugin{
		testPlugin: testPlugin{
			manifest: domain.PluginManifest{Name: "hooky", Version: "1.0"},
		},
	}

	require.NoError(t, mgr.Load(hp))
	assert.Len(t, mgr.GetHooks(), 1)

	require.NoError(t, mgr.Unload("hooky"))
	assert.Empty(t, mgr.GetHooks())
}

func TestManagerDiscover(t *testing.T) {
	tmp := t.TempDir()
	pluginDir := filepath.Join(tmp, "myplugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte("name: myplugin\nversion: \"1.0\"\n"), 0644))

	mgr := NewManager(slog.Default(), nil, []string{tmp}, nil, nil)
	manifests, err := mgr.Discover()
	require.NoError(t, err)
	require.Len(t, manifests, 1)
	assert.Equal(t, "myplugin", manifests[0].Name)
}

func TestManagerLoadInitError(t *testing.T) {
	mgr := NewManager(slog.Default(), nil, nil, nil, nil)
	p := &testPlugin{
		manifest: domain.PluginManifest{Name: "fail"},
		initErr:  errors.New("init boom"),
	}

	err := mgr.Load(p)
	require.Error(t, err)
	assert.Empty(t, mgr.List(), "failed plugin should not be listed")
}

// ---------------------------------------------------------------------------
// New tests
// ---------------------------------------------------------------------------

func TestManagerLoad_DuplicateName(t *testing.T) {
	mgr := NewManager(slog.Default(), nil, nil, nil, nil)
	p1 := &testPlugin{
		manifest: domain.PluginManifest{Name: "dup", Version: "1.0"},
	}
	p2 := &testPlugin{
		manifest: domain.PluginManifest{Name: "dup", Version: "2.0"},
	}

	require.NoError(t, mgr.Load(p1))

	err := mgr.Load(p2)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrDuplicate)
	assert.False(t, p2.inited, "second plugin Init should not be called")
	// Original remains registered.
	assert.Len(t, mgr.List(), 1)
}

func TestManagerLoad_ConcurrentSafe(t *testing.T) {
	mgr := NewManager(slog.Default(), nil, nil, nil, nil)
	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)

	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p := &testPlugin{
				manifest: domain.PluginManifest{
					Name:    fmt.Sprintf("plugin-%d", idx),
					Version: "1.0",
				},
			}
			errs[idx] = mgr.Load(p)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "goroutine %d failed", i)
	}
	assert.Len(t, mgr.List(), n)
}

func TestManagerUnload_CloseError(t *testing.T) {
	mgr := NewManager(slog.Default(), nil, nil, nil, nil)
	p := &testPlugin{
		manifest: domain.PluginManifest{Name: "closefail", Version: "1.0"},
		closeErr: errors.New("close boom"),
	}

	require.NoError(t, mgr.Load(p))

	// Unload should still succeed; Close error is logged but not returned.
	err := mgr.Unload("closefail")
	assert.NoError(t, err)
	assert.True(t, p.closed)
	assert.Empty(t, mgr.List())
}

func TestManagerGetHooks_MultipleHookPlugins(t *testing.T) {
	mgr := NewManager(slog.Default(), nil, nil, nil, nil)
	for i := range 3 {
		hp := &testHookPlugin{
			testPlugin: testPlugin{
				manifest: domain.PluginManifest{Name: fmt.Sprintf("hook-%d", i), Version: "1.0"},
			},
		}
		require.NoError(t, mgr.Load(hp))
	}
	assert.Len(t, mgr.GetHooks(), 3)
}

func TestManagerGetHooks_MixedPlugins(t *testing.T) {
	mgr := NewManager(slog.Default(), nil, nil, nil, nil)

	// 2 hook plugins.
	for i := range 2 {
		hp := &testHookPlugin{
			testPlugin: testPlugin{
				manifest: domain.PluginManifest{Name: fmt.Sprintf("hook-%d", i), Version: "1.0"},
			},
		}
		require.NoError(t, mgr.Load(hp))
	}
	// 2 non-hook plugins.
	for i := range 2 {
		p := &testPlugin{
			manifest: domain.PluginManifest{Name: fmt.Sprintf("plain-%d", i), Version: "1.0"},
		}
		require.NoError(t, mgr.Load(p))
	}

	assert.Len(t, mgr.List(), 4)
	assert.Len(t, mgr.GetHooks(), 2)
}

func TestManagerGetHooks_UnloadOneOfMany(t *testing.T) {
	mgr := NewManager(slog.Default(), nil, nil, nil, nil)
	for i := range 3 {
		hp := &testHookPlugin{
			testPlugin: testPlugin{
				manifest: domain.PluginManifest{Name: fmt.Sprintf("hook-%d", i), Version: "1.0"},
			},
		}
		require.NoError(t, mgr.Load(hp))
	}

	// Unload the middle one.
	require.NoError(t, mgr.Unload("hook-1"))
	hooks := mgr.GetHooks()
	assert.Len(t, hooks, 2)
}

func TestManagerList_ReturnsCopy(t *testing.T) {
	mgr := NewManager(slog.Default(), nil, nil, nil, nil)
	require.NoError(t, mgr.Load(&testPlugin{
		manifest: domain.PluginManifest{Name: "copy-test", Version: "1.0"},
	}))

	list := mgr.List()
	list = append(list, domain.PluginManifest{Name: "injected"})

	// Original should be unaffected.
	assert.Len(t, mgr.List(), 1)
}

func TestManagerGetHooks_ReturnsCopy(t *testing.T) {
	mgr := NewManager(slog.Default(), nil, nil, nil, nil)
	hp := &testHookPlugin{
		testPlugin: testPlugin{
			manifest: domain.PluginManifest{Name: "hook-copy", Version: "1.0"},
		},
	}
	require.NoError(t, mgr.Load(hp))

	hooks := mgr.GetHooks()
	hooks = append(hooks, nil) // mutate the returned slice

	// Original should be unaffected.
	assert.Len(t, mgr.GetHooks(), 1)
}

func TestManagerLoad_PublishesEvent(t *testing.T) {
	bus := &mockEventBus{}
	mgr := NewManager(slog.Default(), bus, nil, nil, nil)
	p := &testPlugin{
		manifest: domain.PluginManifest{Name: "evented", Version: "1.0"},
	}

	require.NoError(t, mgr.Load(p))
	assert.True(t, bus.hasEvent(domain.EventPluginLoaded), "expected EventPluginLoaded to be published")
}

func TestManagerUnload_PublishesEvent(t *testing.T) {
	bus := &mockEventBus{}
	mgr := NewManager(slog.Default(), bus, nil, nil, nil)
	p := &testPlugin{
		manifest: domain.PluginManifest{Name: "evented", Version: "1.0"},
	}

	require.NoError(t, mgr.Load(p))
	require.NoError(t, mgr.Unload("evented"))
	assert.True(t, bus.hasEvent(domain.EventPluginUnloaded), "expected EventPluginUnloaded to be published")
}
