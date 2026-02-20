package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"alfred-ai/internal/domain"
)

// stubTool is a minimal tool for testing schema validation.
type stubTool struct {
	name   string
	schema json.RawMessage
	result *domain.ToolResult
}

func (s *stubTool) Name() string        { return s.name }
func (s *stubTool) Description() string { return "stub" }
func (s *stubTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        s.name,
		Description: "stub",
		Parameters:  s.schema,
	}
}
func (s *stubTool) Execute(_ context.Context, _ json.RawMessage) (*domain.ToolResult, error) {
	return s.result, nil
}

func TestSchemaValidation_ValidParams(t *testing.T) {
	inner := &stubTool{
		name: "test",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {"type": "string"}
			},
			"required": ["name"]
		}`),
		result: &domain.ToolResult{Content: "ok"},
	}

	wrapped, err := WithSchemaValidation(inner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := wrapped.Execute(context.Background(), json.RawMessage(`{"name":"alice"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if result.Content != "ok" {
		t.Errorf("expected 'ok', got %q", result.Content)
	}
}

func TestSchemaValidation_MissingRequiredField(t *testing.T) {
	inner := &stubTool{
		name: "test",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {"type": "string"}
			},
			"required": ["name"]
		}`),
		result: &domain.ToolResult{Content: "should not reach"},
	}

	wrapped, err := WithSchemaValidation(inner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := wrapped.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for missing required field")
	}
	if !strings.Contains(result.Content, "schema validation failed") {
		t.Errorf("expected schema validation error, got: %s", result.Content)
	}
}

func TestSchemaValidation_WrongType(t *testing.T) {
	inner := &stubTool{
		name: "test",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"count": {"type": "integer"}
			}
		}`),
		result: &domain.ToolResult{Content: "should not reach"},
	}

	wrapped, err := WithSchemaValidation(inner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := wrapped.Execute(context.Background(), json.RawMessage(`{"count":"not-a-number"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for wrong type")
	}
	if !strings.Contains(result.Content, "schema validation failed") {
		t.Errorf("expected schema validation error, got: %s", result.Content)
	}
}

func TestSchemaValidation_NoSchema_Passthrough(t *testing.T) {
	inner := &stubTool{
		name:   "test",
		schema: nil,
		result: &domain.ToolResult{Content: "passthrough"},
	}

	wrapped, err := WithSchemaValidation(inner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be the same tool (no wrapping).
	if wrapped != inner {
		t.Error("expected passthrough for nil schema")
	}
}

func TestSchemaValidation_NullSchema_Passthrough(t *testing.T) {
	inner := &stubTool{
		name:   "test",
		schema: json.RawMessage(`null`),
		result: &domain.ToolResult{Content: "passthrough"},
	}

	wrapped, err := WithSchemaValidation(inner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if wrapped != inner {
		t.Error("expected passthrough for null schema")
	}
}

func TestSchemaValidation_CompilationError(t *testing.T) {
	inner := &stubTool{
		name:   "test",
		schema: json.RawMessage(`{"type": "invalid_type"}`),
	}

	_, err := WithSchemaValidation(inner)
	if err == nil {
		t.Fatal("expected error for invalid schema")
	}
}

func TestSchemaValidation_DelegatesMetadata(t *testing.T) {
	inner := &stubTool{
		name: "my_tool",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {"x": {"type":"string"}}
		}`),
	}

	wrapped, err := WithSchemaValidation(inner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if wrapped.Name() != "my_tool" {
		t.Errorf("Name() = %q, want %q", wrapped.Name(), "my_tool")
	}
	if wrapped.Description() != "stub" {
		t.Errorf("Description() = %q, want %q", wrapped.Description(), "stub")
	}
	schema := wrapped.Schema()
	if schema.Name != "my_tool" {
		t.Errorf("Schema().Name = %q, want %q", schema.Name, "my_tool")
	}
}

func TestRegistry_SchemaValidation_Integration(t *testing.T) {
	reg := NewRegistry(nopLogger())

	inner := &stubTool{
		name: "validated_tool",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string"}
			},
			"required": ["action"]
		}`),
		result: &domain.ToolResult{Content: "executed"},
	}

	if err := reg.Register(inner); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, err := reg.Get("validated_tool")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	// Valid params should pass through.
	result, err := got.Execute(context.Background(), json.RawMessage(`{"action":"test"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != "executed" {
		t.Errorf("expected 'executed', got %q", result.Content)
	}

	// Missing required field should fail.
	result, err = got.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing action")
	}
}

func TestRegistry_SchemaCompilationError_GracefulFallback(t *testing.T) {
	reg := NewRegistry(nopLogger())

	inner := &stubTool{
		name:   "bad_schema_tool",
		schema: json.RawMessage(`{"type": "invalid_type"}`),
		result: &domain.ToolResult{Content: "fallback ok"},
	}

	if err := reg.Register(inner); err != nil {
		t.Fatalf("register should succeed despite bad schema: %v", err)
	}

	got, err := reg.Get("bad_schema_tool")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	// Tool should still work (no schema validation).
	result, err := got.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Content != "fallback ok" {
		t.Errorf("expected 'fallback ok', got %q", result.Content)
	}
}

func TestRegistry_NilLogger_NoSchemaValidation(t *testing.T) {
	reg := NewRegistry(nil)

	inner := &stubTool{
		name: "unwrapped",
		schema: json.RawMessage(`{
			"type": "object",
			"required": ["x"]
		}`),
		result: &domain.ToolResult{Content: "no validation"},
	}

	if err := reg.Register(inner); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, err := reg.Get("unwrapped")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	// Should pass even without required field (no schema validation).
	result, err := got.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Content != "no validation" {
		t.Errorf("expected 'no validation', got %q", result.Content)
	}
}
