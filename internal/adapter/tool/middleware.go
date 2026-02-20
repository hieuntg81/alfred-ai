package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/tracer"
)

// Execute is the standard tool execution pipeline: parse params -> start trace -> run handler -> format result.
//
// The handler receives the parsed params and an active trace span. It should return:
//   - (any Go value, nil) — the value is JSON-marshaled into a success ToolResult
//   - (string, nil) — wrapped in a plain-text ToolResult
//   - (*domain.ToolResult, nil) — returned as-is (for custom formatting)
//   - (nil, error) — turned into an error ToolResult with logging
func Execute[P any](
	ctx context.Context,
	spanName string,
	logger *slog.Logger,
	rawParams json.RawMessage,
	handler func(ctx context.Context, span trace.Span, params P) (any, error),
) (*domain.ToolResult, error) {
	ctx, span := tracer.StartSpan(ctx, spanName,
		trace.WithAttributes(tracer.StringAttr("tool.name", spanName)),
	)
	defer span.End()

	var p P
	if err := json.Unmarshal(rawParams, &p); err != nil {
		tracer.RecordError(span, err)
		return &domain.ToolResult{IsError: true, Content: fmt.Sprintf("invalid params: %v", err)}, nil
	}

	result, err := handler(ctx, span, p)
	if err != nil {
		tracer.RecordError(span, err)
		logger.Warn(spanName+" failed", "error", err)

		retryable := classifyToolError(err)
		content := err.Error()
		if retryable {
			content += " (transient error, may succeed on retry)"
		}
		return &domain.ToolResult{IsError: true, IsRetryable: retryable, Content: content}, nil
	}

	return formatResult(span, result)
}

// formatResult converts the handler's return value into a ToolResult.
func formatResult(span trace.Span, result any) (*domain.ToolResult, error) {
	switch v := result.(type) {
	case *domain.ToolResult:
		if v.IsError {
			tracer.RecordError(span, fmt.Errorf("%s", v.Content))
		} else {
			tracer.SetOK(span)
		}
		return v, nil
	case string:
		tracer.SetOK(span)
		return &domain.ToolResult{Content: v}, nil
	default:
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			tracer.RecordError(span, err)
			return &domain.ToolResult{
				IsError: true,
				Content: fmt.Sprintf("failed to format response: %v", err),
			}, nil
		}
		tracer.SetOK(span)
		return &domain.ToolResult{Content: string(data)}, nil
	}
}

// ParseParams unmarshals rawParams into T and returns it.
// On failure it returns a ToolResult with IsError=true, suitable for returning directly.
// Use this in tools that can't use the full Execute[P] middleware (e.g. tools that need
// custom span handling or mutex acquisition before parsing).
func ParseParams[P any](rawParams json.RawMessage) (P, *domain.ToolResult) {
	var p P
	if err := json.Unmarshal(rawParams, &p); err != nil {
		return p, &domain.ToolResult{
			IsError: true,
			Content: fmt.Sprintf("invalid params: %v", err),
		}
	}
	return p, nil
}

// ErrResult creates an error ToolResult. Use this for validation errors inside handlers
// that should be returned to the LLM without being logged as warnings.
func ErrResult(format string, args ...any) (*domain.ToolResult, error) {
	return &domain.ToolResult{
		IsError: true,
		Content: fmt.Sprintf(format, args...),
	}, nil
}

// JSONResult marshals v as indented JSON into a success ToolResult.
func JSONResult(v any) (*domain.ToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return &domain.ToolResult{Content: string(data)}, nil
}

// TextResult creates a plain text success ToolResult.
func TextResult(s string) *domain.ToolResult {
	return &domain.ToolResult{Content: s}
}

// BadAction returns an error for an unknown action with a hint listing valid actions.
func BadAction(got string, valid ...string) error {
	return fmt.Errorf("unknown action %q (want: %s)", got, joinComma(valid))
}

func joinComma(ss []string) string {
	switch len(ss) {
	case 0:
		return ""
	case 1:
		return ss[0]
	}
	out := ss[0]
	for _, s := range ss[1:] {
		out += ", " + s
	}
	return out
}
