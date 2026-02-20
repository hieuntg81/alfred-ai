package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"alfred-ai/internal/domain"
)

// nopLogger returns a logger that discards output.
func nopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- Execute tests ---

func TestExecute_Success_JSON(t *testing.T) {
	type params struct {
		Name string `json:"name"`
	}
	raw := json.RawMessage(`{"name":"alice"}`)

	result, err := Execute(context.Background(), "test.tool", nopLogger(), raw,
		func(_ context.Context, _ trace.Span, p params) (any, error) {
			return map[string]string{"greeting": "hello " + p.Name}, nil
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if !strings.Contains(result.Content, `"greeting"`) {
		t.Errorf("expected JSON with greeting, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "hello alice") {
		t.Errorf("expected 'hello alice', got: %s", result.Content)
	}
}

func TestExecute_Success_String(t *testing.T) {
	type params struct{}
	raw := json.RawMessage(`{}`)

	result, err := Execute(context.Background(), "test.tool", nopLogger(), raw,
		func(_ context.Context, _ trace.Span, _ params) (any, error) {
			return "plain text response", nil
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("unexpected error result")
	}
	if result.Content != "plain text response" {
		t.Errorf("expected plain text, got: %s", result.Content)
	}
}

func TestExecute_Success_CustomToolResult(t *testing.T) {
	type params struct{}
	raw := json.RawMessage(`{}`)

	custom := &domain.ToolResult{Content: "custom formatted"}
	result, err := Execute(context.Background(), "test.tool", nopLogger(), raw,
		func(_ context.Context, _ trace.Span, _ params) (any, error) {
			return custom, nil
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != custom {
		t.Error("expected exact custom ToolResult to be returned")
	}
}

func TestExecute_Success_CustomErrorToolResult(t *testing.T) {
	type params struct{}
	raw := json.RawMessage(`{}`)

	custom := &domain.ToolResult{IsError: true, Content: "validation failed"}
	result, err := Execute(context.Background(), "test.tool", nopLogger(), raw,
		func(_ context.Context, _ trace.Span, _ params) (any, error) {
			return custom, nil
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	if result.Content != "validation failed" {
		t.Errorf("expected 'validation failed', got: %s", result.Content)
	}
}

func TestExecute_InvalidJSON(t *testing.T) {
	type params struct {
		Name string `json:"name"`
	}
	raw := json.RawMessage(`{invalid`)

	result, err := Execute(context.Background(), "test.tool", nopLogger(), raw,
		func(_ context.Context, _ trace.Span, _ params) (any, error) {
			t.Fatal("handler should not be called")
			return nil, nil
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for invalid JSON")
	}
	if !strings.Contains(result.Content, "invalid params") {
		t.Errorf("expected 'invalid params' in content, got: %s", result.Content)
	}
}

func TestExecute_HandlerError_Permanent(t *testing.T) {
	type params struct{}
	raw := json.RawMessage(`{}`)

	result, err := Execute(context.Background(), "test.tool", nopLogger(), raw,
		func(_ context.Context, _ trace.Span, _ params) (any, error) {
			return nil, errors.New("invalid phone number format")
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	if result.Content != "invalid phone number format" {
		t.Errorf("expected exact error message, got: %s", result.Content)
	}
	if result.IsRetryable {
		t.Error("expected permanent error to have IsRetryable=false")
	}
}

func TestExecute_HandlerError_Retryable(t *testing.T) {
	type params struct{}
	raw := json.RawMessage(`{}`)

	result, err := Execute(context.Background(), "test.tool", nopLogger(), raw,
		func(_ context.Context, _ trace.Span, _ params) (any, error) {
			return nil, errors.New("dial tcp 10.0.0.1:50051: connection refused")
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	if !result.IsRetryable {
		t.Error("expected transient error to have IsRetryable=true")
	}
	if !strings.Contains(result.Content, "connection refused") {
		t.Errorf("expected error message in content, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "transient error") {
		t.Errorf("expected retry hint in content, got: %s", result.Content)
	}
}

func TestExecute_HandlerError_RetryableSentinel(t *testing.T) {
	type params struct{}
	raw := json.RawMessage(`{}`)

	result, err := Execute(context.Background(), "test.tool", nopLogger(), raw,
		func(_ context.Context, _ trace.Span, _ params) (any, error) {
			return nil, fmt.Errorf("snap on node-1: %w", domain.ErrTimeout)
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	if !result.IsRetryable {
		t.Error("expected ErrCameraTimeout to produce IsRetryable=true")
	}
	if !strings.Contains(result.Content, "transient error") {
		t.Errorf("expected retry hint in content, got: %s", result.Content)
	}
}

func TestExecute_NilResult(t *testing.T) {
	type params struct{}
	raw := json.RawMessage(`{}`)

	// nil result from handler should marshal as JSON null
	result, err := Execute(context.Background(), "test.tool", nopLogger(), raw,
		func(_ context.Context, _ trace.Span, _ params) (any, error) {
			return map[string]string{}, nil
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
}

func TestExecute_SpanAttributesAccessible(t *testing.T) {
	type params struct {
		Action string `json:"action"`
	}
	raw := json.RawMessage(`{"action":"test"}`)

	var spanCaptured trace.Span
	_, _ = Execute(context.Background(), "test.tool", nopLogger(), raw,
		func(_ context.Context, span trace.Span, p params) (any, error) {
			spanCaptured = span
			return "ok", nil
		},
	)

	if spanCaptured == nil {
		t.Fatal("expected span to be passed to handler")
	}
}

// --- ParseParams tests ---

func TestParseParams_ValidJSON(t *testing.T) {
	type params struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	raw := json.RawMessage(`{"name":"alice","age":30}`)

	p, errResult := ParseParams[params](raw)
	if errResult != nil {
		t.Fatalf("unexpected error result: %s", errResult.Content)
	}
	if p.Name != "alice" {
		t.Errorf("expected name=alice, got %s", p.Name)
	}
	if p.Age != 30 {
		t.Errorf("expected age=30, got %d", p.Age)
	}
}

func TestParseParams_InvalidJSON(t *testing.T) {
	type params struct {
		Name string `json:"name"`
	}
	raw := json.RawMessage(`{invalid`)

	_, errResult := ParseParams[params](raw)
	if errResult == nil {
		t.Fatal("expected error result for invalid JSON")
	}
	if !errResult.IsError {
		t.Fatal("expected IsError=true")
	}
	if !strings.Contains(errResult.Content, "invalid params") {
		t.Errorf("expected 'invalid params' in content, got: %s", errResult.Content)
	}
}

func TestParseParams_EmptyInput(t *testing.T) {
	type params struct {
		Name string `json:"name"`
	}

	// Empty/null JSON should zero-initialize the struct
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{}`),
		json.RawMessage(`null`),
	} {
		p, errResult := ParseParams[params](raw)
		if errResult != nil {
			t.Fatalf("unexpected error for %s: %s", string(raw), errResult.Content)
		}
		if p.Name != "" {
			t.Errorf("expected empty name for %s, got %q", string(raw), p.Name)
		}
	}
}

func TestParseParams_NilInput(t *testing.T) {
	type params struct {
		Name string `json:"name"`
	}

	// nil RawMessage should fail
	_, errResult := ParseParams[params](nil)
	if errResult == nil {
		t.Fatal("expected error result for nil input")
	}
	if !errResult.IsError {
		t.Fatal("expected IsError=true")
	}
}

// --- ErrResult tests ---

func TestErrResult(t *testing.T) {
	result, err := ErrResult("field %q is required", "name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	if result.Content != `field "name" is required` {
		t.Errorf("unexpected content: %s", result.Content)
	}
}

// --- JSONResult tests ---

func TestJSONResult_Success(t *testing.T) {
	result, err := JSONResult(map[string]int{"count": 42})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("unexpected error result")
	}
	if !strings.Contains(result.Content, `"count": 42`) {
		t.Errorf("expected JSON with count, got: %s", result.Content)
	}
}

func TestJSONResult_MarshalError(t *testing.T) {
	// chan cannot be marshaled
	_, err := JSONResult(make(chan int))
	if err == nil {
		t.Fatal("expected error for unmarshalable type")
	}
}

// --- TextResult tests ---

func TestTextResult(t *testing.T) {
	result := TextResult("hello world")
	if result.IsError {
		t.Fatal("unexpected error result")
	}
	if result.Content != "hello world" {
		t.Errorf("unexpected content: %s", result.Content)
	}
}

// --- BadAction tests ---

func TestBadAction(t *testing.T) {
	err := BadAction("foo", "bar", "baz", "qux")
	expected := `unknown action "foo" (want: bar, baz, qux)`
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestBadAction_SingleValid(t *testing.T) {
	err := BadAction("x", "y")
	if !strings.Contains(err.Error(), `"x"`) {
		t.Errorf("expected action in error: %v", err)
	}
}

// --- Validate tests ---

func TestRequireField(t *testing.T) {
	tests := []struct {
		name  string
		value string
		err   bool
	}{
		{"has value", "abc", false},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RequireField("field", tt.value)
			if (err != nil) != tt.err {
				t.Errorf("RequireField(%q) error = %v, want error: %v", tt.value, err, tt.err)
			}
		})
	}
}

func TestRequireFields(t *testing.T) {
	tests := []struct {
		name string
		kvs  []string
		err  bool
	}{
		{"all present", []string{"a", "1", "b", "2"}, false},
		{"first empty", []string{"a", "", "b", "2"}, true},
		{"second empty", []string{"a", "1", "b", ""}, true},
		{"odd args", []string{"a"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RequireFields(tt.kvs...)
			if (err != nil) != tt.err {
				t.Errorf("RequireFields(%v) error = %v, want error: %v", tt.kvs, err, tt.err)
			}
		})
	}
}

func TestValidateRange(t *testing.T) {
	tests := []struct {
		value    int
		min, max int
		err      bool
	}{
		{5, 0, 10, false},
		{0, 0, 10, false},
		{10, 0, 10, false},
		{-1, 0, 10, true},
		{11, 0, 10, true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d in [%d,%d]", tt.value, tt.min, tt.max), func(t *testing.T) {
			err := ValidateRange("field", tt.value, tt.min, tt.max)
			if (err != nil) != tt.err {
				t.Errorf("ValidateRange(%d, %d, %d) error = %v, want error: %v",
					tt.value, tt.min, tt.max, err, tt.err)
			}
		})
	}
}

func TestValidatePositive(t *testing.T) {
	tests := []struct {
		value int
		err   bool
	}{
		{1, false},
		{100, false},
		{0, true},
		{-1, true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.value), func(t *testing.T) {
			err := ValidatePositive("field", tt.value)
			if (err != nil) != tt.err {
				t.Errorf("ValidatePositive(%d) error = %v, want error: %v", tt.value, err, tt.err)
			}
		})
	}
}

func TestValidateEnum(t *testing.T) {
	tests := []struct {
		value   string
		allowed []string
		err     bool
	}{
		{"a", []string{"a", "b", "c"}, false},
		{"b", []string{"a", "b", "c"}, false},
		{"d", []string{"a", "b", "c"}, true},
		{"", []string{"a", "b"}, false}, // empty is allowed
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q in %v", tt.value, tt.allowed), func(t *testing.T) {
			err := ValidateEnum("field", tt.value, tt.allowed...)
			if (err != nil) != tt.err {
				t.Errorf("ValidateEnum(%q, %v) error = %v, want error: %v",
					tt.value, tt.allowed, err, tt.err)
			}
		})
	}
}

// --- joinComma tests ---

func TestJoinComma(t *testing.T) {
	tests := []struct {
		input    []string
		expected string
	}{
		{nil, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b"}, "a, b"},
		{[]string{"a", "b", "c"}, "a, b, c"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := joinComma(tt.input)
			if got != tt.expected {
				t.Errorf("joinComma(%v) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
