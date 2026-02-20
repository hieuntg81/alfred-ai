package wasm

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero"

	"alfred-ai/internal/domain"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// buildNoopWASM creates a minimal WASM module binary that exports
// malloc, free, and memory. malloc returns a fixed pointer (1024).
func buildNoopWASM(t *testing.T) []byte {
	t.Helper()

	return []byte{
		0x00, 0x61, 0x73, 0x6d, // magic: \0asm
		0x01, 0x00, 0x00, 0x00, // version: 1

		// Type section (id=1): 2 function types, content=11 bytes
		0x01, 0x0b,
		0x02,                               // 2 types
		0x60, 0x01, 0x7f, 0x01, 0x7f,       // type 0: (i32) -> (i32)  [malloc]
		0x60, 0x02, 0x7f, 0x7f, 0x00,       // type 1: (i32, i32) -> () [free]

		// Function section (id=3): 2 functions, content=3 bytes
		0x03, 0x03,
		0x02, // 2 functions
		0x00, // func 0 = type 0
		0x01, // func 1 = type 1

		// Memory section (id=5): 1 memory, content=3 bytes
		0x05, 0x03,
		0x01,       // 1 memory
		0x00, 0x01, // min=1, no max

		// Export section (id=7): 3 exports, content=26 bytes
		0x07, 0x1a,
		0x03, // 3 exports
		// "malloc" -> func 0
		0x06, 'm', 'a', 'l', 'l', 'o', 'c', 0x00, 0x00,
		// "free" -> func 1
		0x04, 'f', 'r', 'e', 'e', 0x00, 0x01,
		// "memory" -> memory 0
		0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00,

		// Code section (id=10): 2 bodies, content=10 bytes
		0x0a, 0x0a,
		0x02, // 2 bodies
		// func 0 (malloc): return 1024; body=5 bytes
		0x05, 0x00, 0x41, 0x80, 0x08, 0x0b,
		// func 1 (free): nop; body=2 bytes
		0x02, 0x00, 0x0b,
	}
}

func writeTestWASM(t *testing.T, dir string) string {
	t.Helper()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	err := os.WriteFile(wasmPath, buildNoopWASM(t), 0o644)
	require.NoError(t, err)
	return wasmPath
}

func TestRuntime_NewAndClose(t *testing.T) {
	ctx := context.Background()
	rt, err := NewRuntime(ctx, DefaultRuntimeConfig(), newTestLogger())
	require.NoError(t, err)
	require.NotNil(t, rt)
	require.NotNil(t, rt.Inner())
	require.NoError(t, rt.Close(ctx))
}

func TestRuntime_CompileValidModule(t *testing.T) {
	ctx := context.Background()
	rt, err := NewRuntime(ctx, DefaultRuntimeConfig(), newTestLogger())
	require.NoError(t, err)
	defer rt.Close(ctx)

	compiled, err := rt.Inner().CompileModule(ctx, buildNoopWASM(t))
	require.NoError(t, err)
	require.NotNil(t, compiled)
}

func TestRuntime_CompileInvalidModule(t *testing.T) {
	ctx := context.Background()
	rt, err := NewRuntime(ctx, DefaultRuntimeConfig(), newTestLogger())
	require.NoError(t, err)
	defer rt.Close(ctx)

	_, err = rt.Inner().CompileModule(ctx, []byte("not a wasm binary"))
	require.Error(t, err)
}

func TestMemory_ReadWriteString(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer rt.Close(ctx)

	compiled, err := rt.CompileModule(ctx, buildNoopWASM(t))
	require.NoError(t, err)

	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	require.NoError(t, err)
	defer mod.Close(ctx)

	// Write a string using the module's malloc export.
	testStr := "hello wasm"
	ptr, size, err := WriteString(mod, testStr)
	require.NoError(t, err)
	assert.Equal(t, uint32(1024), ptr) // our noop malloc always returns 1024
	assert.Equal(t, uint32(len(testStr)), size)

	// Read back.
	got, err := ReadString(mod, ptr, size)
	require.NoError(t, err)
	assert.Equal(t, testStr, got)
}

func TestMemory_ReadOutOfBounds(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer rt.Close(ctx)

	compiled, err := rt.CompileModule(ctx, buildNoopWASM(t))
	require.NoError(t, err)

	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	require.NoError(t, err)
	defer mod.Close(ctx)

	// Try to read way beyond memory bounds.
	_, err = ReadBytes(mod, 0xFFFFFF, 100)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrToolFailure)
}

func TestMemory_WriteEmptyString(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer rt.Close(ctx)

	compiled, err := rt.CompileModule(ctx, buildNoopWASM(t))
	require.NoError(t, err)

	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	require.NoError(t, err)
	defer mod.Close(ctx)

	ptr, size, err := WriteString(mod, "")
	require.NoError(t, err)
	assert.Equal(t, uint32(0), ptr)
	assert.Equal(t, uint32(0), size)
}

func TestHostEnv_ToolResult(t *testing.T) {
	env := &hostEnv{
		sandbox: NewSandbox(domain.WASMPluginConfig{
			Capabilities: []string{CapToolResult},
		}, newTestLogger()),
		logger: newTestLogger(),
	}

	result := json.RawMessage(`{"content":"test","is_error":false}`)
	env.toolResult = result

	assert.Equal(t, result, json.RawMessage(env.toolResult))
}
