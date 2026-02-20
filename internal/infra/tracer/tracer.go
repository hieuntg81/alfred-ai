package tracer

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"alfred-ai/internal/infra/config"
)

const tracerName = "alfred-ai"

// Setup initializes OpenTelemetry tracing and returns a shutdown function.
// When cfg.Enabled is false, a noop TracerProvider is used (zero overhead).
func Setup(ctx context.Context, cfg config.TracerConfig) (func(context.Context) error, error) {
	noopShutdown := func(context.Context) error { return nil }

	if !cfg.Enabled {
		otel.SetTracerProvider(noop.NewTracerProvider())
		return noopShutdown, nil
	}

	var exporter sdktrace.SpanExporter
	var err error

	switch cfg.Exporter {
	case "stdout":
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("create stdout exporter: %w", err)
		}
	case "noop", "":
		otel.SetTracerProvider(noop.NewTracerProvider())
		return noopShutdown, nil
	default:
		return nil, fmt.Errorf("unsupported exporter: %s", cfg.Exporter)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}

// StartSpan is a convenience helper to start a named span.
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return otel.Tracer(tracerName).Start(ctx, name, opts...)
}

// RecordError records an error on the span and sets error status.
func RecordError(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// SetOK sets the span status to OK.
func SetOK(span trace.Span) {
	span.SetStatus(codes.Ok, "")
}

// StringAttr is a convenience for attribute.String.
func StringAttr(key, value string) attribute.KeyValue {
	return attribute.String(key, value)
}

// IntAttr is a convenience for attribute.Int.
func IntAttr(key string, value int) attribute.KeyValue {
	return attribute.Int(key, value)
}
