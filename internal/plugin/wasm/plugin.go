package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"alfred-ai/internal/domain"
)

// WASMPlugin wraps a WASM module as a domain.Plugin, and optionally
// implements domain.PluginHook and domain.Tool based on module exports.
type WASMPlugin struct {
	manifest domain.PluginManifest
	module   api.Module
	compiled wazero.CompiledModule
	runtime  *Runtime
	sandbox  *Sandbox
	hostEnv  *hostEnv
	logger   *slog.Logger

	// Interface probing flags set during load.
	hasHooks bool
	hasTool  bool
}

// Compile-time checks.
var (
	_ domain.Plugin     = (*WASMPlugin)(nil)
	_ domain.PluginHook = (*WASMPlugin)(nil)
	_ domain.Tool       = (*WASMPlugin)(nil)
)

// LoadPlugin creates a WASMPlugin by compiling and instantiating a .wasm binary.
func LoadPlugin(ctx context.Context, rt *Runtime, wasmPath string, manifest domain.PluginManifest, sandbox *Sandbox) (*WASMPlugin, error) {
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %v", domain.ErrInvalidInput, wasmPath, err)
	}

	compiled, err := rt.Inner().CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: compile: %v", domain.ErrInvalidInput, err)
	}

	logger := rt.logger.With("plugin", manifest.Name)

	env := &hostEnv{
		sandbox: sandbox,
		logger:  logger,
		config:  nil, // set during Init
	}

	// Register host functions and instantiate the host module.
	hostCompiled, err := RegisterHostFunctions(ctx, rt.Inner(), env)
	if err != nil {
		return nil, err
	}
	if _, err := rt.Inner().InstantiateModule(ctx, hostCompiled, wazero.NewModuleConfig().WithName(HostModule)); err != nil {
		return nil, fmt.Errorf("%w: instantiate host module: %v", domain.ErrInvalidInput, err)
	}

	// Instantiate the guest module.
	modCfg := wazero.NewModuleConfig().
		WithName(manifest.Name).
		WithStartFunctions() // Don't auto-call _start; we call _init explicitly.

	mod, err := rt.Inner().InstantiateModule(ctx, compiled, modCfg)
	if err != nil {
		return nil, fmt.Errorf("%w: instantiate guest: %v", domain.ErrInvalidInput, err)
	}

	// Probe exports to determine which interfaces this module satisfies.
	hasHooks := mod.ExportedFunction("on_message_received") != nil ||
		mod.ExportedFunction("on_before_tool_exec") != nil ||
		mod.ExportedFunction("on_after_tool_exec") != nil ||
		mod.ExportedFunction("on_response_ready") != nil

	hasTool := mod.ExportedFunction("tool_execute") != nil

	p := &WASMPlugin{
		manifest: manifest,
		module:   mod,
		compiled: compiled,
		runtime:  rt,
		sandbox:  sandbox,
		hostEnv:  env,
		logger:   logger,
		hasHooks: hasHooks,
		hasTool:  hasTool,
	}

	// Call guest's _init if exported.
	if initFn := mod.ExportedFunction("_init"); initFn != nil {
		execCtx, cancel := context.WithTimeout(ctx, sandbox.ExecTimeout())
		defer cancel()
		if _, err := initFn.Call(execCtx); err != nil {
			return nil, fmt.Errorf("%w: _init: %v", domain.ErrToolFailure, err)
		}
	}

	logger.Info("wasm plugin loaded",
		"path", wasmPath,
		"has_hooks", hasHooks,
		"has_tool", hasTool,
	)

	return p, nil
}

// Manifest implements domain.Plugin.
func (p *WASMPlugin) Manifest() domain.PluginManifest {
	return p.manifest
}

// Init implements domain.Plugin.
func (p *WASMPlugin) Init(_ context.Context, deps domain.PluginDeps) error {
	p.logger = deps.Logger
	p.hostEnv.logger = deps.Logger
	p.hostEnv.bus = deps.EventBus
	p.hostEnv.config = deps.Config
	return nil
}

// Close implements domain.Plugin.
func (p *WASMPlugin) Close() error {
	if closeFn := p.module.ExportedFunction("_close"); closeFn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := closeFn.Call(ctx); err != nil {
			p.logger.Warn("wasm _close failed", "error", err)
		}
	}
	return p.module.Close(context.Background())
}

// HasHooks reports whether this WASM module exports any hook functions.
func (p *WASMPlugin) HasHooks() bool {
	return p.hasHooks
}

// HasTool reports whether this WASM module exports tool_execute.
func (p *WASMPlugin) HasTool() bool {
	return p.hasTool
}

// --- domain.PluginHook ---

// OnMessageReceived implements domain.PluginHook.
func (p *WASMPlugin) OnMessageReceived(ctx context.Context, msg domain.InboundMessage) error {
	return p.callHookWithJSON(ctx, "on_message_received", msg)
}

// OnBeforeToolExec implements domain.PluginHook.
func (p *WASMPlugin) OnBeforeToolExec(ctx context.Context, call domain.ToolCall) error {
	return p.callHookWithJSON(ctx, "on_before_tool_exec", call)
}

// OnAfterToolExec implements domain.PluginHook.
func (p *WASMPlugin) OnAfterToolExec(ctx context.Context, call domain.ToolCall, result *domain.ToolResult) error {
	payload := struct {
		Call   domain.ToolCall    `json:"call"`
		Result *domain.ToolResult `json:"result"`
	}{Call: call, Result: result}
	return p.callHookWithJSON(ctx, "on_after_tool_exec", payload)
}

// OnResponseReady implements domain.PluginHook.
func (p *WASMPlugin) OnResponseReady(ctx context.Context, response string) (string, error) {
	fn := p.module.ExportedFunction("on_response_ready")
	if fn == nil {
		return response, nil
	}

	ptr, size, err := WriteString(p.module, response)
	if err != nil {
		return response, fmt.Errorf("%w: write input: %v", domain.ErrToolFailure, err)
	}
	defer FreeBytes(p.module, ptr, size)

	execCtx, cancel := context.WithTimeout(ctx, p.sandbox.ExecTimeout())
	defer cancel()

	results, err := fn.Call(execCtx, uint64(ptr), uint64(size))
	if err != nil {
		if execCtx.Err() != nil {
			return response, fmt.Errorf("%w: on_response_ready", domain.ErrTimeout)
		}
		return response, fmt.Errorf("%w: on_response_ready: %v", domain.ErrToolFailure, err)
	}

	if len(results) >= 2 {
		outPtr := uint32(results[0])
		outLen := uint32(results[1])
		if outPtr != 0 && outLen != 0 {
			modified, err := ReadString(p.module, outPtr, outLen)
			if err == nil {
				return modified, nil
			}
		}
	}

	return response, nil
}

// --- domain.Tool ---

// Name implements domain.Tool.
func (p *WASMPlugin) Name() string {
	return p.manifest.Name
}

// Description implements domain.Tool.
func (p *WASMPlugin) Description() string {
	return p.manifest.Description
}

// Schema implements domain.Tool.
func (p *WASMPlugin) Schema() domain.ToolSchema {
	schema := domain.ToolSchema{
		Name:        p.manifest.Name,
		Description: p.manifest.Description,
		Parameters:  json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"}}}`),
	}

	// If the guest exports tool_schema, call it to get the actual schema.
	if fn := p.module.ExportedFunction("tool_schema"); fn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		results, err := fn.Call(ctx)
		if err == nil && len(results) >= 2 {
			ptr := uint32(results[0])
			size := uint32(results[1])
			if data, err := ReadBytes(p.module, ptr, size); err == nil {
				schema.Parameters = json.RawMessage(data)
			}
		}
	}

	return schema
}

// Execute implements domain.Tool.
func (p *WASMPlugin) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	fn := p.module.ExportedFunction("tool_execute")
	if fn == nil {
		return nil, fmt.Errorf("%w: module does not export tool_execute", domain.ErrToolFailure)
	}

	ptr, size, err := WriteBytes(p.module, params)
	if err != nil {
		return nil, fmt.Errorf("%w: write params: %v", domain.ErrToolFailure, err)
	}
	defer FreeBytes(p.module, ptr, size)

	// Clear previous tool result.
	p.hostEnv.toolResult = nil

	execCtx, cancel := context.WithTimeout(ctx, p.sandbox.ExecTimeout())
	defer cancel()

	_, err = fn.Call(execCtx, uint64(ptr), uint64(size))
	if err != nil {
		if execCtx.Err() != nil {
			return nil, fmt.Errorf("%w: tool_execute", domain.ErrTimeout)
		}
		return nil, fmt.Errorf("%w: tool_execute: %v", domain.ErrToolFailure, err)
	}

	// Check if guest wrote a result via the tool_result host function.
	if p.hostEnv.toolResult != nil {
		var result domain.ToolResult
		if err := json.Unmarshal(p.hostEnv.toolResult, &result); err != nil {
			return &domain.ToolResult{
				Content: string(p.hostEnv.toolResult),
			}, nil
		}
		return &result, nil
	}

	return &domain.ToolResult{Content: "ok"}, nil
}

// --- helpers ---

// callHookWithJSON serializes data to JSON, passes it to the named guest function.
func (p *WASMPlugin) callHookWithJSON(ctx context.Context, name string, data any) error {
	fn := p.module.ExportedFunction(name)
	if fn == nil {
		return nil // hook not implemented by guest
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("%w: marshal %s input: %v", domain.ErrToolFailure, name, err)
	}

	ptr, size, err := WriteBytes(p.module, payload)
	if err != nil {
		return fmt.Errorf("%w: write %s input: %v", domain.ErrToolFailure, name, err)
	}
	defer FreeBytes(p.module, ptr, size)

	execCtx, cancel := context.WithTimeout(ctx, p.sandbox.ExecTimeout())
	defer cancel()

	if _, err := fn.Call(execCtx, uint64(ptr), uint64(size)); err != nil {
		if execCtx.Err() != nil {
			return fmt.Errorf("%w: %s", domain.ErrTimeout, name)
		}
		return fmt.Errorf("%w: %s: %v", domain.ErrToolFailure, name, err)
	}

	return nil
}
