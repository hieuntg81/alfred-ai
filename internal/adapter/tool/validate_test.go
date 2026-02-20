package tool

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ValidateAll ---

func TestValidateAll_AllNil(t *testing.T) {
	assert.NoError(t, ValidateAll(nil, nil, nil))
}

func TestValidateAll_Empty(t *testing.T) {
	assert.NoError(t, ValidateAll())
}

func TestValidateAll_ReturnsFirst(t *testing.T) {
	first := fmt.Errorf("first")
	second := fmt.Errorf("second")
	err := ValidateAll(nil, first, second)
	assert.Equal(t, first, err)
}

func TestValidateAll_IntegrationWithRequireField(t *testing.T) {
	err := ValidateAll(
		RequireField("name", "ok"),
		RequireField("url", ""),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "'url' is required")
}

// --- ValidateURL ---

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr string
	}{
		{"empty is ok", "", ""},
		{"valid https", "https://example.com/path", ""},
		{"valid http", "http://localhost:8080", ""},
		{"missing scheme", "example.com", "scheme must be http or https"},
		{"ftp scheme", "ftp://example.com", "scheme must be http or https"},
		{"missing host", "http://", "missing host"},
		{"not a url", "://broken", "invalid url"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURL("url", tt.value)
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

// --- ValidateMaxLength ---

func TestValidateMaxLength(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		max     int
		wantErr bool
	}{
		{"empty", "", 10, false},
		{"within limit", "hello", 10, false},
		{"at limit", "hello", 5, false},
		{"over limit", "hello!", 5, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMaxLength("field", tt.value, tt.max)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "exceeds maximum length")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- ValidateJSON ---

func TestValidateJSON(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"empty is ok", "", false},
		{"valid object", `{"key":"value"}`, false},
		{"valid array", `[1,2,3]`, false},
		{"valid string", `"hello"`, false},
		{"valid null", `null`, false},
		{"invalid", `{broken`, true},
		{"trailing garbage", `{}extra`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateJSON("body", tt.value)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "not valid JSON")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- Edge case tests for validators ---

func TestRequireField_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		field     string
		value     string
		wantErr   bool
		wantInMsg string
	}{
		{"whitespace-only passes (not trimmed)", "name", "   ", false, ""},
		{"tab-only passes", "name", "\t", false, ""},
		{"newline-only passes", "name", "\n", false, ""},
		{"error includes field name", "my_field", "", true, "'my_field' is required"},
		{"error format is consistent", "url", "", true, "'url' is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RequireField(tt.field, tt.value)
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Equal(t, tt.wantInMsg, err.Error())
		})
	}
}

func TestRequireFields_EdgeCases(t *testing.T) {
	t.Run("zero args returns nil", func(t *testing.T) {
		assert.NoError(t, RequireFields())
	})

	t.Run("error identifies the correct missing field", func(t *testing.T) {
		err := RequireFields("name", "alice", "email", "", "phone", "555")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "'email'")
		assert.NotContains(t, err.Error(), "'name'")
		assert.NotContains(t, err.Error(), "'phone'")
	})

	t.Run("returns first missing field only", func(t *testing.T) {
		err := RequireFields("a", "", "b", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "'a'")
		assert.NotContains(t, err.Error(), "'b'")
	})

	t.Run("odd args detected with single arg", func(t *testing.T) {
		err := RequireFields("lonely")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "odd number")
	})

	t.Run("odd args detected with three args", func(t *testing.T) {
		err := RequireFields("a", "1", "b")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "odd number")
	})
}

func TestValidateRange_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		value   int
		min     int
		max     int
		wantErr bool
		wantMsg string
	}{
		{"negative range", -5, -10, -1, false, ""},
		{"at negative min", -10, -10, -1, false, ""},
		{"at negative max", -1, -10, -1, false, ""},
		{"below negative min", -11, -10, -1, true, "field must be -10--1"},
		{"above negative max", 0, -10, -1, true, "field must be -10--1"},
		{"single-value range (at value)", 5, 5, 5, false, ""},
		{"single-value range (below)", 4, 5, 5, true, "field must be 5-5"},
		{"single-value range (above)", 6, 5, 5, true, "field must be 5-5"},
		{"error message format", 100, 0, 50, true, "field must be 0-50"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRange("field", tt.value, tt.min, tt.max)
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Equal(t, tt.wantMsg, err.Error())
		})
	}
}

func TestValidatePositive_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		value   int
		wantErr bool
		wantMsg string
	}{
		{"zero is rejected", 0, true, "'count' is required and must be > 0"},
		{"negative is rejected", -42, true, "'count' is required and must be > 0"},
		{"large negative rejected", -999999, true, "'count' is required and must be > 0"},
		{"one is valid", 1, false, ""},
		{"large positive valid", 999999, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePositive("count", tt.value)
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Equal(t, tt.wantMsg, err.Error())
		})
	}
}

func TestValidateEnum_EdgeCases(t *testing.T) {
	t.Run("case sensitivity", func(t *testing.T) {
		err := ValidateEnum("mode", "JSON", "json", "xml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"JSON"`)
	})

	t.Run("error lists allowed values", func(t *testing.T) {
		err := ValidateEnum("format", "yaml", "json", "xml", "csv")
		require.Error(t, err)
		msg := err.Error()
		assert.Contains(t, msg, "json")
		assert.Contains(t, msg, "xml")
		assert.Contains(t, msg, "csv")
		assert.Contains(t, msg, `"yaml"`) // shows the invalid value quoted
	})

	t.Run("single allowed value", func(t *testing.T) {
		assert.NoError(t, ValidateEnum("type", "text", "text"))
		err := ValidateEnum("type", "binary", "text")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "text")
		// with single allowed value, no comma in output
		assert.False(t, strings.Contains(err.Error(), ","))
	})

	t.Run("error format includes field name", func(t *testing.T) {
		err := ValidateEnum("protocol", "grpc", "http", "ws")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "protocol")
	})
}
