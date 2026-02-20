package wasm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogLevelConstants(t *testing.T) {
	assert.Equal(t, int32(0), LogDebug)
	assert.Equal(t, int32(1), LogInfo)
	assert.Equal(t, int32(2), LogWarn)
	assert.Equal(t, int32(3), LogError)
}
