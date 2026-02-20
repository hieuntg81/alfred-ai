package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"alfred-ai/internal/domain"
)

// HostModule is the namespace under which host functions are registered.
const HostModule = "alfred_v1"

// hostEnv holds the dependencies injected into host functions.
type hostEnv struct {
	sandbox    *Sandbox
	logger     *slog.Logger
	bus        domain.EventBus
	config     json.RawMessage
	toolResult []byte // last tool result written by guest
}

// RegisterHostFunctions registers the alfred_v1 host module on the given runtime.
// Only capabilities allowed by the sandbox are registered.
func RegisterHostFunctions(ctx context.Context, rt wazero.Runtime, env *hostEnv) (wazero.CompiledModule, error) {
	builder := rt.NewHostModuleBuilder(HostModule)

	// log(level, ptr, len) — always allowed (CapLog).
	builder.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			level := int32(stack[0])
			ptr := uint32(stack[1])
			size := uint32(stack[2])

			msg, err := ReadString(mod, ptr, size)
			if err != nil {
				env.logger.Error("wasm log: read failed", "error", err)
				return
			}

			switch {
			case level <= 0:
				env.logger.Debug(msg)
			case level == 1:
				env.logger.Info(msg)
			case level == 2:
				env.logger.Warn(msg)
			default:
				env.logger.Error(msg)
			}
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, nil).
		Export("log")

	// get_config(key_ptr, key_len) → (ptr, len)  — always allowed (CapConfig).
	builder.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Ignore key_ptr/key_len for now — return full config JSON.
			data := env.config
			if data == nil {
				data = []byte("{}")
			}
			ptr, size, err := WriteBytes(mod, data)
			if err != nil {
				env.logger.Error("wasm get_config: write failed", "error", err)
				stack[0] = 0
				stack[1] = 0
				return
			}
			stack[0] = uint64(ptr)
			stack[1] = uint64(size)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}).
		Export("get_config")

	// emit_event(type_ptr, type_len, payload_ptr, payload_len) — requires CapEventBus.
	if env.sandbox.AllowCapability(CapEventBus) {
		builder.NewFunctionBuilder().
			WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
				typePtr := uint32(stack[0])
				typeLen := uint32(stack[1])
				payloadPtr := uint32(stack[2])
				payloadLen := uint32(stack[3])

				eventType, err := ReadString(mod, typePtr, typeLen)
				if err != nil {
					env.logger.Error("wasm emit_event: read type failed", "error", err)
					return
				}
				payload, err := ReadBytes(mod, payloadPtr, payloadLen)
				if err != nil {
					env.logger.Error("wasm emit_event: read payload failed", "error", err)
					return
				}

				if env.bus != nil {
					env.bus.Publish(ctx, domain.Event{
						Type:      domain.EventType(eventType),
						Timestamp: time.Now(),
						Payload:   json.RawMessage(payload),
					})
				}
			}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, nil).
			Export("emit_event")
	}

	// tool_result(ptr, len) — requires CapToolResult.
	if env.sandbox.AllowCapability(CapToolResult) {
		builder.NewFunctionBuilder().
			WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
				ptr := uint32(stack[0])
				size := uint32(stack[1])

				data, err := ReadBytes(mod, ptr, size)
				if err != nil {
					env.logger.Error("wasm tool_result: read failed", "error", err)
					return
				}
				env.toolResult = data
			}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, nil).
			Export("tool_result")
	}

	compiled, err := builder.Compile(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: compile host module: %v", domain.ErrInvalidInput, err)
	}

	return compiled, nil
}
