package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"alfred-ai/internal/domain"
)

// SchemaValidatingTool wraps a Tool with JSON Schema validation.
// On Execute, it validates params against the compiled schema before delegating.
type SchemaValidatingTool struct {
	inner  domain.Tool
	schema *jsonschema.Schema
}

// WithSchemaValidation wraps a tool so that Execute validates params against
// the tool's JSON Schema before forwarding to the inner tool.
// Returns error if the schema fails to compile.
func WithSchemaValidation(t domain.Tool) (domain.Tool, error) {
	raw := t.Schema().Parameters
	if len(raw) == 0 || string(raw) == "null" {
		return t, nil // no schema to validate against
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", bytes.NewReader(raw)); err != nil {
		return nil, fmt.Errorf("add schema resource for %q: %w", t.Name(), err)
	}
	compiled, err := compiler.Compile("schema.json")
	if err != nil {
		return nil, fmt.Errorf("compile schema for %q: %w", t.Name(), err)
	}

	return &SchemaValidatingTool{inner: t, schema: compiled}, nil
}

func (s *SchemaValidatingTool) Name() string              { return s.inner.Name() }
func (s *SchemaValidatingTool) Description() string       { return s.inner.Description() }
func (s *SchemaValidatingTool) Schema() domain.ToolSchema { return s.inner.Schema() }

func (s *SchemaValidatingTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	var v interface{}
	if err := json.Unmarshal(params, &v); err != nil {
		return &domain.ToolResult{
			IsError: true,
			Content: fmt.Sprintf("invalid JSON: %v", err),
		}, nil
	}

	if err := s.schema.Validate(v); err != nil {
		return &domain.ToolResult{
			IsError: true,
			Content: fmt.Sprintf("schema validation failed: %v", err),
		}, nil
	}

	return s.inner.Execute(ctx, params)
}
