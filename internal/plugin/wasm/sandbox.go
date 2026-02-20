package wasm

import (
	"fmt"
	"log/slog"
	"time"

	"alfred-ai/internal/domain"
)

// Capability constants define the host functions a WASM plugin can access.
const (
	CapLog        = "log"        // always allowed
	CapConfig     = "config"     // always allowed
	CapEventBus   = "event_bus"  // requires explicit grant
	CapToolResult = "tool"       // requires explicit grant
)

// knownCapabilities is the set of all valid capability strings.
var knownCapabilities = map[string]bool{
	CapLog:        true,
	CapConfig:     true,
	CapEventBus:   true,
	CapToolResult: true,
}

// alwaysAllowed capabilities are granted regardless of manifest configuration.
var alwaysAllowed = map[string]bool{
	CapLog:    true,
	CapConfig: true,
}

// Sandbox enforces capability-based restrictions on WASM plugin host function access.
type Sandbox struct {
	capabilities map[string]bool
	maxMemoryMB  int
	execTimeout  time.Duration
	logger       *slog.Logger
}

// NewSandbox creates a Sandbox from the given WASM plugin config.
func NewSandbox(cfg domain.WASMPluginConfig, logger *slog.Logger) *Sandbox {
	maxMem := cfg.MaxMemoryMB
	if maxMem <= 0 {
		maxMem = 64
	}

	timeout := cfg.ExecTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	caps := make(map[string]bool)
	// Always-allowed capabilities.
	for cap := range alwaysAllowed {
		caps[cap] = true
	}
	// Explicitly requested capabilities.
	for _, cap := range cfg.Capabilities {
		caps[cap] = true
	}

	return &Sandbox{
		capabilities: caps,
		maxMemoryMB:  maxMem,
		execTimeout:  timeout,
		logger:       logger,
	}
}

// AllowCapability reports whether the given capability is permitted.
func (s *Sandbox) AllowCapability(cap string) bool {
	return s.capabilities[cap]
}

// MaxMemoryMB returns the memory limit in megabytes.
func (s *Sandbox) MaxMemoryMB() int {
	return s.maxMemoryMB
}

// ExecTimeout returns the execution timeout for guest function calls.
func (s *Sandbox) ExecTimeout() time.Duration {
	return s.execTimeout
}

// MemoryPages returns the number of WASM 64KB memory pages corresponding
// to the configured memory limit.
func (s *Sandbox) MemoryPages() uint32 {
	return uint32(s.maxMemoryMB) * 16 // 1 MB = 16 pages of 64KB
}

// ValidateCapabilities checks that all requested capabilities are known.
// Returns an error listing unknown capabilities.
func ValidateCapabilities(requested []string) error {
	var unknown []string
	for _, cap := range requested {
		if !knownCapabilities[cap] {
			unknown = append(unknown, cap)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("%w: unknown capabilities: %v", domain.ErrPermissionDenied, unknown)
	}
	return nil
}
