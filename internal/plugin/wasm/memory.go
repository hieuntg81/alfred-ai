package wasm

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero/api"

	"alfred-ai/internal/domain"
)

// ReadString reads a UTF-8 string from the guest module's linear memory.
func ReadString(mod api.Module, ptr, size uint32) (string, error) {
	b, err := ReadBytes(mod, ptr, size)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ReadBytes reads raw bytes from the guest module's linear memory.
func ReadBytes(mod api.Module, ptr, size uint32) ([]byte, error) {
	if size == 0 {
		return nil, nil
	}
	buf, ok := mod.Memory().Read(ptr, size)
	if !ok {
		return nil, fmt.Errorf("%w: memory read out of bounds at ptr=%d len=%d", domain.ErrToolFailure, ptr, size)
	}
	// Return a copy so the caller owns the slice.
	out := make([]byte, size)
	copy(out, buf)
	return out, nil
}

// WriteString writes a UTF-8 string into guest memory using the module's
// exported malloc function. Returns the pointer and length.
func WriteString(mod api.Module, data string) (uint32, uint32, error) {
	return WriteBytes(mod, []byte(data))
}

// WriteBytes writes raw bytes into guest memory using the module's exported
// malloc function. Returns the pointer and length.
func WriteBytes(mod api.Module, data []byte) (uint32, uint32, error) {
	size := uint32(len(data))
	if size == 0 {
		return 0, 0, nil
	}

	malloc := mod.ExportedFunction("malloc")
	if malloc == nil {
		return 0, 0, fmt.Errorf("%w: guest module does not export malloc", domain.ErrToolFailure)
	}

	results, err := malloc.Call(context.Background(), uint64(size))
	if err != nil {
		return 0, 0, fmt.Errorf("%w: malloc(%d) failed: %v", domain.ErrToolFailure, size, err)
	}
	if len(results) == 0 {
		return 0, 0, fmt.Errorf("%w: malloc returned no results", domain.ErrToolFailure)
	}

	ptr := uint32(results[0])
	if ptr == 0 {
		return 0, 0, fmt.Errorf("%w: malloc returned null pointer", domain.ErrToolFailure)
	}

	if !mod.Memory().Write(ptr, data) {
		return 0, 0, fmt.Errorf("%w: memory write out of bounds at ptr=%d len=%d", domain.ErrToolFailure, ptr, size)
	}

	return ptr, size, nil
}

// FreeBytes calls the guest's exported free function to release memory.
func FreeBytes(mod api.Module, ptr, size uint32) {
	if ptr == 0 || size == 0 {
		return
	}
	free := mod.ExportedFunction("free")
	if free == nil {
		return
	}
	_, _ = free.Call(context.Background(), uint64(ptr), uint64(size))
}
