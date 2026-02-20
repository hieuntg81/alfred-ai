package wasm

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/tetratelabs/wazero"

	"alfred-ai/internal/domain"
)

// RuntimeConfig holds configuration for the WASM runtime.
type RuntimeConfig struct {
	// MaxMemoryPages is the maximum number of 64KB WASM memory pages.
	// Default 1024 = 64MB.
	MaxMemoryPages uint32
}

// DefaultRuntimeConfig returns a RuntimeConfig with sensible defaults.
func DefaultRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{
		MaxMemoryPages: 1024, // 64MB
	}
}

// Runtime wraps a wazero.Runtime with shared configuration.
type Runtime struct {
	inner  wazero.Runtime
	config RuntimeConfig
	logger *slog.Logger
}

// NewRuntime creates a new WASM runtime. The caller must call Close when done.
func NewRuntime(ctx context.Context, cfg RuntimeConfig, logger *slog.Logger) (*Runtime, error) {
	if cfg.MaxMemoryPages == 0 {
		cfg.MaxMemoryPages = 1024
	}

	rtCfg := wazero.NewRuntimeConfig().
		WithCloseOnContextDone(true).
		WithMemoryLimitPages(cfg.MaxMemoryPages)

	rt := wazero.NewRuntimeWithConfig(ctx, rtCfg)

	logger.Info("wasm runtime created",
		"max_memory_pages", cfg.MaxMemoryPages,
		"max_memory_mb", cfg.MaxMemoryPages*64/1024,
	)

	return &Runtime{
		inner:  rt,
		config: cfg,
		logger: logger,
	}, nil
}

// Inner returns the underlying wazero.Runtime.
func (r *Runtime) Inner() wazero.Runtime {
	return r.inner
}

// Close releases all resources held by the runtime.
func (r *Runtime) Close(ctx context.Context) error {
	if err := r.inner.Close(ctx); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrInvalidInput, err)
	}
	r.logger.Info("wasm runtime closed")
	return nil
}
