package wasm

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"alfred-ai/internal/domain"
)

func testLogger() *slog.Logger {
	return slog.Default()
}

func TestNewSandbox_Defaults(t *testing.T) {
	sb := NewSandbox(domain.WASMPluginConfig{}, testLogger())

	assert.Equal(t, 64, sb.MaxMemoryMB())
	assert.Equal(t, 30*time.Second, sb.ExecTimeout())
	assert.True(t, sb.AllowCapability(CapLog), "log should always be allowed")
	assert.True(t, sb.AllowCapability(CapConfig), "config should always be allowed")
	assert.False(t, sb.AllowCapability(CapEventBus), "event_bus should not be allowed by default")
	assert.False(t, sb.AllowCapability(CapToolResult), "tool should not be allowed by default")
}

func TestNewSandbox_ExplicitCapabilities(t *testing.T) {
	sb := NewSandbox(domain.WASMPluginConfig{
		MaxMemoryMB:  128,
		ExecTimeout:  10 * time.Second,
		Capabilities: []string{CapEventBus, CapToolResult},
	}, testLogger())

	assert.Equal(t, 128, sb.MaxMemoryMB())
	assert.Equal(t, 10*time.Second, sb.ExecTimeout())
	assert.True(t, sb.AllowCapability(CapLog))
	assert.True(t, sb.AllowCapability(CapConfig))
	assert.True(t, sb.AllowCapability(CapEventBus))
	assert.True(t, sb.AllowCapability(CapToolResult))
}

func TestSandbox_MemoryPages(t *testing.T) {
	sb := NewSandbox(domain.WASMPluginConfig{MaxMemoryMB: 64}, testLogger())
	assert.Equal(t, uint32(1024), sb.MemoryPages()) // 64 * 16 = 1024
}

func TestValidateCapabilities_AllKnown(t *testing.T) {
	err := ValidateCapabilities([]string{CapLog, CapConfig, CapEventBus, CapToolResult})
	require.NoError(t, err)
}

func TestValidateCapabilities_Unknown(t *testing.T) {
	err := ValidateCapabilities([]string{CapLog, "network", "filesystem"})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrPermissionDenied)
	assert.Contains(t, err.Error(), "network")
	assert.Contains(t, err.Error(), "filesystem")
}

func TestValidateCapabilities_Empty(t *testing.T) {
	err := ValidateCapabilities(nil)
	require.NoError(t, err)
}
