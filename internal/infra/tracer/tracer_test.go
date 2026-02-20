package tracer

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"

	"alfred-ai/internal/infra/config"
)

func TestSetupDisabled(t *testing.T) {
	cfg := config.TracerConfig{Enabled: false}
	shutdown, err := Setup(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer shutdown(context.Background())

	tp := otel.GetTracerProvider()
	if _, ok := tp.(noop.TracerProvider); !ok {
		t.Errorf("expected noop provider, got %T", tp)
	}
}

func TestSetupNoop(t *testing.T) {
	cfg := config.TracerConfig{Enabled: true, Exporter: "noop"}
	shutdown, err := Setup(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer shutdown(context.Background())
}

func TestSetupStdout(t *testing.T) {
	cfg := config.TracerConfig{Enabled: true, Exporter: "stdout"}
	shutdown, err := Setup(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer shutdown(context.Background())
}

func TestSetupEmptyExporter(t *testing.T) {
	cfg := config.TracerConfig{Enabled: true, Exporter: ""}
	shutdown, err := Setup(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer shutdown(context.Background())

	tp := otel.GetTracerProvider()
	if _, ok := tp.(noop.TracerProvider); !ok {
		t.Errorf("expected noop provider for empty exporter, got %T", tp)
	}
}

func TestSetupUnsupportedExporter(t *testing.T) {
	cfg := config.TracerConfig{Enabled: true, Exporter: "invalid"}
	_, err := Setup(context.Background(), cfg)
	if err == nil {
		t.Error("expected error for unsupported exporter")
	}
}

func TestStartSpanAndHelpers(t *testing.T) {
	// Use noop provider for testing
	otel.SetTracerProvider(noop.NewTracerProvider())

	ctx, span := StartSpan(context.Background(), "test-span")
	if ctx == nil {
		t.Error("context should not be nil")
	}

	// These should not panic
	SetOK(span)
	RecordError(span, errors.New("test error"))
	span.End()
}

func TestAttrHelpers(t *testing.T) {
	s := StringAttr("key", "value")
	if string(s.Key) != "key" {
		t.Errorf("StringAttr key = %q, want %q", s.Key, "key")
	}

	i := IntAttr("count", 42)
	if string(i.Key) != "count" {
		t.Errorf("IntAttr key = %q, want %q", i.Key, "count")
	}
}
