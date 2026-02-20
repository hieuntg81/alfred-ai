package gateway

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/usecase"
)

func apiTestDeps(t *testing.T) HandlerDeps {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessions := usecase.NewSessionManager(t.TempDir())
	// Pre-create some sessions.
	sessions.GetOrCreate("s1")
	sessions.GetOrCreate("s2")

	return HandlerDeps{
		Sessions: sessions,
		Tools:    handlerStubTools{},
		Memory:   &handlerStubMemory{},
		Logger:   logger,
	}
}

func TestStatusHandler_Success(t *testing.T) {
	deps := apiTestDeps(t)
	metrics := &Metrics{}
	metrics.ToolCallsTotal.Store(42)
	metrics.ToolErrorsTotal.Store(3)

	handler := statusHandler(deps, time.Now().Add(-60*time.Second), metrics, []string{"cli", "discord"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp StatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Agent.Name != "alfred-ai" {
		t.Errorf("Agent.Name = %q", resp.Agent.Name)
	}
	if resp.Agent.UptimeSeconds < 59 {
		t.Errorf("UptimeSeconds = %d, want >= 59", resp.Agent.UptimeSeconds)
	}
	if resp.Sessions.Active != 2 {
		t.Errorf("Sessions.Active = %d, want 2", resp.Sessions.Active)
	}
	if resp.Tools.Registered != 1 { // handlerStubTools returns 1 schema
		t.Errorf("Tools.Registered = %d, want 1", resp.Tools.Registered)
	}
	if resp.Tools.CallsTotal != 42 {
		t.Errorf("Tools.CallsTotal = %d, want 42", resp.Tools.CallsTotal)
	}
	if resp.Tools.ErrorsTotal != 3 {
		t.Errorf("Tools.ErrorsTotal = %d, want 3", resp.Tools.ErrorsTotal)
	}
	if resp.Memory.Provider != "stub" {
		t.Errorf("Memory.Provider = %q", resp.Memory.Provider)
	}
	if len(resp.Channels) != 2 {
		t.Errorf("Channels = %v, want 2 entries", resp.Channels)
	}
}

func TestStatusHandler_MethodNotAllowed(t *testing.T) {
	deps := apiTestDeps(t)
	handler := statusHandler(deps, time.Now(), &Metrics{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/status", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestMetricsHandler_PrometheusFormat(t *testing.T) {
	deps := apiTestDeps(t)
	metrics := &Metrics{}
	metrics.ToolCallsTotal.Store(10)
	metrics.LLMCallsTotal.Store(5)
	metrics.MessagesRecv.Store(20)
	metrics.MessagesSent.Store(15)

	handler := metricsHandler(deps, time.Now().Add(-120*time.Second), metrics)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "text/plain; version=0.0.4; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}

	body := w.Body.String()

	expectedMetrics := []string{
		"alfredai_sessions_active 2",
		"alfredai_tool_calls_total 10",
		"alfredai_tools_registered 1",
		"alfredai_llm_calls_total 5",
		"alfredai_messages_received_total 20",
		"alfredai_messages_sent_total 15",
		"alfredai_memory_available 0", // handlerStubMemory.IsAvailable() returns false
		"go_goroutines",
		"go_memstats_alloc_bytes",
	}

	for _, metric := range expectedMetrics {
		if !containsStr(body, metric) {
			t.Errorf("metrics output missing %q", metric)
		}
	}
}

func TestMetricsHandler_MethodNotAllowed(t *testing.T) {
	deps := apiTestDeps(t)
	handler := metricsHandler(deps, time.Now(), &Metrics{})

	req := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestRESTAuthMiddleware(t *testing.T) {
	deps := apiTestDeps(t)
	auth := &staticAuth{token: "test-token"}

	srv := NewServer(nil, auth, ":0", slog.New(slog.NewTextHandler(io.Discard, nil)))
	RegisterRESTHandlers(srv, deps, []string{"cli"})

	if len(srv.httpRoutes) != 2 {
		t.Fatalf("expected 2 HTTP routes, got %d", len(srv.httpRoutes))
	}

	// Test auth rejection (no token).
	for _, route := range srv.httpRoutes {
		req := httptest.NewRequest(http.MethodGet, route.pattern, nil)
		w := httptest.NewRecorder()
		route.handler(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("route %s without token: status = %d, want 401", route.pattern, w.Code)
		}
	}

	// Test auth success with Bearer header.
	for _, route := range srv.httpRoutes {
		req := httptest.NewRequest(http.MethodGet, route.pattern, nil)
		req.Header.Set("Authorization", "Bearer test-token")
		w := httptest.NewRecorder()
		route.handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("route %s with valid token: status = %d, want 200", route.pattern, w.Code)
		}
	}

	// Test auth success with query param.
	for _, route := range srv.httpRoutes {
		req := httptest.NewRequest(http.MethodGet, route.pattern+"?token=test-token", nil)
		w := httptest.NewRecorder()
		route.handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("route %s with query token: status = %d, want 200", route.pattern, w.Code)
		}
	}
}

type staticAuth struct {
	token string
}

func (a *staticAuth) Authenticate(token string) (*ClientInfo, error) {
	if token == a.token {
		return &ClientInfo{Name: "test", Roles: []string{"admin"}}, nil
	}
	return nil, domain.ErrGatewayAuthFailed
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
