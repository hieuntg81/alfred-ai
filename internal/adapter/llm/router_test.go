package llm

import (
	"context"
	"testing"

	"alfred-ai/internal/domain"
)

type stubProvider struct {
	name string
}

func (s *stubProvider) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	return &domain.ChatResponse{
		Message: domain.Message{Role: "assistant", Content: "ok from " + s.name},
	}, nil
}

func (s *stubProvider) Name() string { return s.name }

func TestPreferenceRouterRouteToMapped(t *testing.T) {
	reg := NewRegistry()
	fast := &stubProvider{name: "groq"}
	powerful := &stubProvider{name: "anthropic"}
	reg.Register(fast)
	reg.Register(powerful)

	fallback := &stubProvider{name: "openai"}

	router := NewPreferenceRouter(map[string]string{
		"fast":     "groq",
		"powerful": "anthropic",
	}, reg, fallback)

	p, err := router.Route("fast")
	if err != nil {
		t.Fatalf("Route(fast): %v", err)
	}
	if p.Name() != "groq" {
		t.Errorf("Route(fast) = %q, want groq", p.Name())
	}

	p, err = router.Route("powerful")
	if err != nil {
		t.Fatalf("Route(powerful): %v", err)
	}
	if p.Name() != "anthropic" {
		t.Errorf("Route(powerful) = %q, want anthropic", p.Name())
	}
}

func TestPreferenceRouterRouteDefault(t *testing.T) {
	reg := NewRegistry()
	fallback := &stubProvider{name: "openai"}

	router := NewPreferenceRouter(nil, reg, fallback)

	p, err := router.Route("default")
	if err != nil {
		t.Fatalf("Route(default): %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("Route(default) = %q, want openai", p.Name())
	}

	p, err = router.Route("")
	if err != nil {
		t.Fatalf("Route(''): %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("Route('') = %q, want openai", p.Name())
	}
}

func TestPreferenceRouterRouteUnknownFallback(t *testing.T) {
	reg := NewRegistry()
	fallback := &stubProvider{name: "openai"}

	router := NewPreferenceRouter(map[string]string{
		"fast": "groq",
	}, reg, fallback)

	p, err := router.Route("unknown_preference")
	if err != nil {
		t.Fatalf("Route(unknown_preference): %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("Route(unknown_preference) = %q, want openai (fallback)", p.Name())
	}
}

func TestPreferenceRouterRouteUnknownNoFallback(t *testing.T) {
	reg := NewRegistry()

	router := NewPreferenceRouter(nil, reg, nil)

	_, err := router.Route("fast")
	if err == nil {
		t.Fatal("expected error for unknown preference without fallback")
	}
}

func TestPreferenceRouterRouteNoDefaultProvider(t *testing.T) {
	reg := NewRegistry()

	router := NewPreferenceRouter(nil, reg, nil)

	_, err := router.Route("default")
	if err == nil {
		t.Fatal("expected error for default with no fallback")
	}
}

func TestPreferenceRouterRouteMappedToMissingProvider(t *testing.T) {
	reg := NewRegistry()
	fallback := &stubProvider{name: "openai"}

	router := NewPreferenceRouter(map[string]string{
		"fast": "nonexistent",
	}, reg, fallback)

	_, err := router.Route("fast")
	if err == nil {
		t.Fatal("expected error for mapped-to-missing provider")
	}
}

func TestPreferenceRouterMappedToDefault(t *testing.T) {
	reg := NewRegistry()
	fallback := &stubProvider{name: "openai"}

	router := NewPreferenceRouter(map[string]string{
		"fast": "default",
	}, reg, fallback)

	p, err := router.Route("fast")
	if err != nil {
		t.Fatalf("Route(fast→default): %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("Route(fast→default) = %q, want openai", p.Name())
	}
}
