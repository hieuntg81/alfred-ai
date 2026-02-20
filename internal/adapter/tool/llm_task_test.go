package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"alfred-ai/internal/adapter/llm"
	"alfred-ai/internal/domain"
)

// mockLLMProvider captures the last request and returns a configured response.
type mockLLMProvider struct {
	response *domain.ChatResponse
	err      error
	lastReq  domain.ChatRequest
	name     string
}

func (m *mockLLMProvider) Chat(_ context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	m.lastReq = req
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func (m *mockLLMProvider) Name() string {
	if m.name != "" {
		return m.name
	}
	return "mock"
}

func mockResponse(content string) *domain.ChatResponse {
	return &domain.ChatResponse{
		Message: domain.Message{
			Role:    domain.RoleAssistant,
			Content: content,
		},
	}
}

func newTestLLMTaskTool(t *testing.T, mock *mockLLMProvider, cfgOverrides ...func(*LLMTaskConfig)) *LLMTaskTool {
	t.Helper()
	reg := llm.NewRegistry()
	reg.Register(mock)

	cfg := LLMTaskConfig{
		DefaultModel:  "mock-model",
		MaxTokens:     4096,
		Timeout:       5 * time.Second,
		MaxPromptSize: 32 * 1024,
		MaxInputSize:  256 * 1024,
	}
	for _, override := range cfgOverrides {
		override(&cfg)
	}

	return NewLLMTaskTool(mock, reg, cfg, newTestLogger())
}

func execLLMTaskTool(t *testing.T, tool *LLMTaskTool, params any) *domain.ToolResult {
	t.Helper()
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	result, err := tool.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return result
}

// --- Basic tests ---

func TestLLMTaskToolName(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{}`)}
	tool := newTestLLMTaskTool(t, mock)
	if got := tool.Name(); got != "llm_task" {
		t.Errorf("Name() = %q, want %q", got, "llm_task")
	}
}

func TestLLMTaskToolDescription(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{}`)}
	tool := newTestLLMTaskTool(t, mock)
	if tool.Description() == "" {
		t.Error("Description() must not be empty")
	}
}

func TestLLMTaskToolSchema(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{}`)}
	tool := newTestLLMTaskTool(t, mock)
	schema := tool.Schema()
	if schema.Name != "llm_task" {
		t.Errorf("Schema().Name = %q, want %q", schema.Name, "llm_task")
	}

	var schemaObj map[string]any
	if err := json.Unmarshal(schema.Parameters, &schemaObj); err != nil {
		t.Fatalf("Schema JSON invalid: %v", err)
	}
	props, ok := schemaObj["properties"].(map[string]any)
	if !ok {
		t.Fatal("Schema missing 'properties'")
	}
	if _, ok := props["prompt"]; !ok {
		t.Error("missing 'prompt' property")
	}
	req, ok := schemaObj["required"].([]any)
	if !ok {
		t.Fatal("Schema missing 'required'")
	}
	found := false
	for _, r := range req {
		if r == "prompt" {
			found = true
			break
		}
	}
	if !found {
		t.Error("'prompt' should be in required")
	}
}

// --- Success cases ---

func TestLLMTaskToolSuccess(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{"result": 42}`)}
	tool := newTestLLMTaskTool(t, mock)

	result := execLLMTaskTool(t, tool, map[string]any{
		"prompt": "return the number 42",
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content), &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if v, ok := parsed["result"].(float64); !ok || v != 42 {
		t.Errorf("got result %v, want 42", parsed["result"])
	}
}

func TestLLMTaskToolWithInput(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{"classified": "positive"}`)}
	tool := newTestLLMTaskTool(t, mock)

	result := execLLMTaskTool(t, tool, map[string]any{
		"prompt": "classify sentiment",
		"input":  map[string]any{"text": "I love this"},
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content)
	}

	// Verify input appears in the user message sent to LLM
	userMsg := mock.lastReq.Messages[1].Content
	if !strings.Contains(userMsg, "INPUT_JSON") {
		t.Error("expected INPUT_JSON in user message")
	}
	if !strings.Contains(userMsg, "I love this") {
		t.Error("expected input content in user message")
	}
}

func TestLLMTaskToolNoTools(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{}`)}
	tool := newTestLLMTaskTool(t, mock)

	execLLMTaskTool(t, tool, map[string]any{"prompt": "test"})

	if len(mock.lastReq.Tools) != 0 {
		t.Errorf("ChatRequest.Tools should be empty, got %d tools", len(mock.lastReq.Tools))
	}
}

// --- Error cases ---

func TestLLMTaskToolEmptyPrompt(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{}`)}
	tool := newTestLLMTaskTool(t, mock)

	result := execLLMTaskTool(t, tool, map[string]any{"prompt": ""})
	if !result.IsError {
		t.Error("expected error for empty prompt")
	}

	result = execLLMTaskTool(t, tool, map[string]any{"prompt": "   "})
	if !result.IsError {
		t.Error("expected error for whitespace-only prompt")
	}
}

func TestLLMTaskToolPromptTooLarge(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{}`)}
	tool := newTestLLMTaskTool(t, mock, func(cfg *LLMTaskConfig) {
		cfg.MaxPromptSize = 10
	})

	result := execLLMTaskTool(t, tool, map[string]any{"prompt": "this is too long"})
	if !result.IsError {
		t.Error("expected error for prompt too large")
	}
}

func TestLLMTaskToolInputTooLarge(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{}`)}
	tool := newTestLLMTaskTool(t, mock, func(cfg *LLMTaskConfig) {
		cfg.MaxInputSize = 5
	})

	result := execLLMTaskTool(t, tool, map[string]any{
		"prompt": "test",
		"input":  map[string]any{"data": "large payload"},
	})
	if !result.IsError {
		t.Error("expected error for input too large")
	}
}

func TestLLMTaskToolInvalidParams(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{}`)}
	tool := newTestLLMTaskTool(t, mock)

	result, err := tool.Execute(context.Background(), []byte(`{invalid`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON params")
	}
}

func TestLLMTaskToolLLMError(t *testing.T) {
	mock := &mockLLMProvider{err: fmt.Errorf("rate limit exceeded")}
	tool := newTestLLMTaskTool(t, mock)

	result := execLLMTaskTool(t, tool, map[string]any{"prompt": "test"})
	if !result.IsError {
		t.Error("expected error when LLM fails")
	}
	if !strings.Contains(result.Content, "rate limit exceeded") {
		t.Errorf("error should contain LLM error, got: %s", result.Content)
	}
}

func TestLLMTaskToolEmptyResponse(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse("")}
	tool := newTestLLMTaskTool(t, mock)

	result := execLLMTaskTool(t, tool, map[string]any{"prompt": "test"})
	if !result.IsError {
		t.Error("expected error for empty LLM response")
	}
}

func TestLLMTaskToolInvalidJSON(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse("this is not json")}
	tool := newTestLLMTaskTool(t, mock)

	result := execLLMTaskTool(t, tool, map[string]any{"prompt": "test"})
	if !result.IsError {
		t.Error("expected error for invalid JSON response")
	}
	if !strings.Contains(result.Content, "invalid JSON") {
		t.Errorf("error should mention invalid JSON, got: %s", result.Content)
	}
}

// --- Code fence stripping ---

func TestLLMTaskToolCodeFenceStripping(t *testing.T) {
	cases := []struct {
		name     string
		response string
	}{
		{"json fence", "```json\n{\"key\": \"value\"}\n```"},
		{"plain fence", "```\n{\"key\": \"value\"}\n```"},
		{"no fence", `{"key": "value"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockLLMProvider{response: mockResponse(tc.response)}
			tool := newTestLLMTaskTool(t, mock)

			result := execLLMTaskTool(t, tool, map[string]any{"prompt": "test"})
			if result.IsError {
				t.Fatalf("expected success, got error: %s", result.Content)
			}

			var parsed map[string]any
			if err := json.Unmarshal([]byte(result.Content), &parsed); err != nil {
				t.Fatalf("unmarshal result: %v", err)
			}
			if parsed["key"] != "value" {
				t.Errorf("got key=%v, want 'value'", parsed["key"])
			}
		})
	}
}

// --- Schema validation ---

func TestLLMTaskToolSchemaValidationSuccess(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{"name": "test", "age": 25}`)}
	tool := newTestLLMTaskTool(t, mock)

	result := execLLMTaskTool(t, tool, map[string]any{
		"prompt": "generate a person",
		"schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"age":  map[string]any{"type": "integer"},
			},
			"required": []string{"name"},
		},
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content)
	}
}

func TestLLMTaskToolSchemaValidationFailure(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{"wrong": 1}`)}
	tool := newTestLLMTaskTool(t, mock)

	result := execLLMTaskTool(t, tool, map[string]any{
		"prompt": "generate a person",
		"schema": map[string]any{
			"type":       "object",
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
			"required":   []string{"name"},
		},
	})

	if !result.IsError {
		t.Error("expected error for schema validation failure")
	}
	if !strings.Contains(result.Content, "did not match schema") {
		t.Errorf("error should mention schema, got: %s", result.Content)
	}
}

func TestLLMTaskToolInvalidSchema(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{"ok": true}`)}
	tool := newTestLLMTaskTool(t, mock)

	// "type" must be a string per JSON Schema spec; passing a number is genuinely broken
	result := execLLMTaskTool(t, tool, map[string]any{
		"prompt": "test",
		"schema": map[string]any{"type": 123},
	})

	if !result.IsError {
		t.Error("expected error for invalid schema")
	}
}

func TestLLMTaskToolNullSchemaSkipped(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{"ok": true}`)}
	tool := newTestLLMTaskTool(t, mock)

	// null schema should be skipped (no validation)
	data, _ := json.Marshal(map[string]any{
		"prompt": "test",
		"schema": nil,
	})
	result, err := tool.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success with null schema, got error: %s", result.Content)
	}
}

// --- Provider override ---

func TestLLMTaskToolProviderOverride(t *testing.T) {
	defaultMock := &mockLLMProvider{
		name:     "default",
		response: mockResponse(`{"from": "default"}`),
	}
	overrideMock := &mockLLMProvider{
		name:     "override",
		response: mockResponse(`{"from": "override"}`),
	}

	reg := llm.NewRegistry()
	reg.Register(defaultMock)
	reg.Register(overrideMock)

	tool := NewLLMTaskTool(defaultMock, reg, LLMTaskConfig{
		DefaultModel: "test-model",
		MaxTokens:    4096,
		Timeout:      5 * time.Second,
	}, newTestLogger())

	result := execLLMTaskTool(t, tool, map[string]any{
		"prompt":   "test",
		"provider": "override",
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content)
	}

	// Verify override provider was called, not the default
	if overrideMock.lastReq.Messages == nil {
		t.Error("expected override provider to be called")
	}
	if defaultMock.lastReq.Messages != nil {
		t.Error("expected default provider NOT to be called")
	}
}

func TestLLMTaskToolProviderNotFound(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{}`)}
	tool := newTestLLMTaskTool(t, mock)

	result := execLLMTaskTool(t, tool, map[string]any{
		"prompt":   "test",
		"provider": "nonexistent",
	})
	if !result.IsError {
		t.Error("expected error for nonexistent provider")
	}
}

// --- Model allowlist ---

func TestLLMTaskToolModelAllowlistAllowed(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{}`)}
	tool := newTestLLMTaskTool(t, mock, func(cfg *LLMTaskConfig) {
		cfg.AllowedModels = []string{"mock/gpt-4o", "mock/mock-model"}
	})

	result := execLLMTaskTool(t, tool, map[string]any{"prompt": "test"})
	if result.IsError {
		t.Fatalf("expected success for allowed model, got error: %s", result.Content)
	}
}

func TestLLMTaskToolModelAllowlistDenied(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{}`)}
	tool := newTestLLMTaskTool(t, mock, func(cfg *LLMTaskConfig) {
		cfg.AllowedModels = []string{"mock/gpt-4o"}
		cfg.DefaultModel = "forbidden-model"
	})

	result := execLLMTaskTool(t, tool, map[string]any{"prompt": "test"})
	if !result.IsError {
		t.Error("expected error for disallowed model")
	}
	if !strings.Contains(result.Content, "not in allowlist") {
		t.Errorf("error should mention allowlist, got: %s", result.Content)
	}
}

// --- Parameter overrides ---

func TestLLMTaskToolTemperatureOverride(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{}`)}
	tool := newTestLLMTaskTool(t, mock)

	execLLMTaskTool(t, tool, map[string]any{
		"prompt":      "test",
		"temperature": 0.7,
	})

	if mock.lastReq.Temperature != 0.7 {
		t.Errorf("Temperature = %f, want 0.7", mock.lastReq.Temperature)
	}
}

func TestLLMTaskToolMaxTokensOverride(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{}`)}
	tool := newTestLLMTaskTool(t, mock)

	execLLMTaskTool(t, tool, map[string]any{
		"prompt":     "test",
		"max_tokens": 1000,
	})

	if mock.lastReq.MaxTokens != 1000 {
		t.Errorf("MaxTokens = %d, want 1000", mock.lastReq.MaxTokens)
	}
}

func TestLLMTaskToolDefaultTemperatureZero(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{}`)}
	tool := newTestLLMTaskTool(t, mock)

	execLLMTaskTool(t, tool, map[string]any{"prompt": "test"})

	if mock.lastReq.Temperature != 0.0 {
		t.Errorf("default Temperature = %f, want 0.0", mock.lastReq.Temperature)
	}
}

func TestLLMTaskToolModelOverride(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{}`)}
	tool := newTestLLMTaskTool(t, mock)

	execLLMTaskTool(t, tool, map[string]any{
		"prompt": "test",
		"model":  "custom-model",
	})

	if mock.lastReq.Model != "custom-model" {
		t.Errorf("Model = %q, want %q", mock.lastReq.Model, "custom-model")
	}
}

func TestLLMTaskToolTimeoutOverride(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{}`)}
	tool := newTestLLMTaskTool(t, mock)

	result := execLLMTaskTool(t, tool, map[string]any{
		"prompt":     "test",
		"timeout_ms": 5000,
	})
	if result.IsError {
		t.Fatalf("expected success with timeout_ms override, got error: %s", result.Content)
	}
}

// --- MaxTokens cap ---

func TestLLMTaskToolMaxTokensCap(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{}`)}
	tool := newTestLLMTaskTool(t, mock, func(cfg *LLMTaskConfig) {
		cfg.MaxTokens = 4096
	})

	// Override exceeds config limit — should be capped
	execLLMTaskTool(t, tool, map[string]any{
		"prompt":     "test",
		"max_tokens": 999999,
	})

	if mock.lastReq.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096 (capped at config limit)", mock.lastReq.MaxTokens)
	}

	// Override within config limit — should be used as-is
	execLLMTaskTool(t, tool, map[string]any{
		"prompt":     "test",
		"max_tokens": 1000,
	})

	if mock.lastReq.MaxTokens != 1000 {
		t.Errorf("MaxTokens = %d, want 1000 (within config limit)", mock.lastReq.MaxTokens)
	}
}

// --- Temperature clamping ---

func TestLLMTaskToolTemperatureClamp(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{}`)}
	tool := newTestLLMTaskTool(t, mock)

	// Negative temperature should be clamped to 0
	execLLMTaskTool(t, tool, map[string]any{
		"prompt":      "test",
		"temperature": -1.0,
	})
	if mock.lastReq.Temperature != 0.0 {
		t.Errorf("Temperature = %f, want 0.0 (clamped from -1.0)", mock.lastReq.Temperature)
	}

	// Excessive temperature should be clamped to 2.0
	execLLMTaskTool(t, tool, map[string]any{
		"prompt":      "test",
		"temperature": 5.0,
	})
	if mock.lastReq.Temperature != 2.0 {
		t.Errorf("Temperature = %f, want 2.0 (clamped from 5.0)", mock.lastReq.Temperature)
	}

	// Valid temperature should pass through
	execLLMTaskTool(t, tool, map[string]any{
		"prompt":      "test",
		"temperature": 1.5,
	})
	if mock.lastReq.Temperature != 1.5 {
		t.Errorf("Temperature = %f, want 1.5", mock.lastReq.Temperature)
	}
}

// --- Non-object JSON responses ---

func TestLLMTaskToolNonObjectJSON(t *testing.T) {
	cases := []struct {
		name     string
		response string
		want     string
	}{
		{"array", `[1, 2, 3]`, "[\n  1,\n  2,\n  3\n]"},
		{"number", `42`, "42"},
		{"string", `"hello"`, `"hello"`},
		{"boolean", `true`, "true"},
		{"null", `null`, "null"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockLLMProvider{response: mockResponse(tc.response)}
			tool := newTestLLMTaskTool(t, mock)

			result := execLLMTaskTool(t, tool, map[string]any{"prompt": "test"})
			if result.IsError {
				t.Fatalf("expected success for %s, got error: %s", tc.name, result.Content)
			}

			if strings.TrimSpace(result.Content) != tc.want {
				t.Errorf("got %q, want %q", result.Content, tc.want)
			}
		})
	}
}

// --- Combined provider + model override with allowlist ---

func TestLLMTaskToolProviderModelOverrideAllowlist(t *testing.T) {
	defaultMock := &mockLLMProvider{
		name:     "default",
		response: mockResponse(`{}`),
	}
	customMock := &mockLLMProvider{
		name:     "custom",
		response: mockResponse(`{"ok": true}`),
	}

	reg := llm.NewRegistry()
	reg.Register(defaultMock)
	reg.Register(customMock)

	tool := NewLLMTaskTool(defaultMock, reg, LLMTaskConfig{
		DefaultModel:  "default-model",
		MaxTokens:     4096,
		Timeout:       5 * time.Second,
		AllowedModels: []string{"custom/special-model", "default/default-model"},
	}, newTestLogger())

	// Allowed combination: custom provider + special-model
	result := execLLMTaskTool(t, tool, map[string]any{
		"prompt":   "test",
		"provider": "custom",
		"model":    "special-model",
	})
	if result.IsError {
		t.Fatalf("expected success for allowed model, got error: %s", result.Content)
	}

	// Verify the model key was "custom/special-model" (custom provider used)
	if customMock.lastReq.Model != "special-model" {
		t.Errorf("Model = %q, want %q", customMock.lastReq.Model, "special-model")
	}

	// Denied combination: custom provider + forbidden model
	result = execLLMTaskTool(t, tool, map[string]any{
		"prompt":   "test",
		"provider": "custom",
		"model":    "forbidden-model",
	})
	if !result.IsError {
		t.Error("expected error for denied model with custom provider")
	}
	if !strings.Contains(result.Content, "not in allowlist") {
		t.Errorf("error should mention allowlist, got: %s", result.Content)
	}
}

// --- Helper function tests ---

func TestStripCodeFences(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{`{"key": "value"}`, `{"key": "value"}`},
		{"```json\n{\"key\": \"value\"}\n```", `{"key": "value"}`},
		{"```JSON\n{\"key\": \"value\"}\n```", `{"key": "value"}`},
		{"```Json\n{\"key\": \"value\"}\n```", `{"key": "value"}`},
		{"```\n{\"key\": \"value\"}\n```", `{"key": "value"}`},
		{"  ```json\n{}\n```  ", `{}`},
		{"not fenced", "not fenced"},
	}

	for _, tc := range cases {
		got := stripCodeFences(tc.input)
		if got != tc.want {
			t.Errorf("stripCodeFences(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate short = %q, want %q", got, "short")
	}
	if got := truncate("this is long text", 7); got != "this is..." {
		t.Errorf("truncate long = %q, want %q", got, "this is...")
	}

	// UTF-8 safety: "héllo" has 'é' as 2 bytes; truncating at byte 2 should
	// not split the multi-byte character.
	got := truncate("héllo world", 3)
	// Should truncate to a clean rune boundary, not produce invalid UTF-8
	prefix := strings.TrimSuffix(got, "...")
	if !utf8.ValidString(prefix) {
		t.Errorf("truncate produced invalid UTF-8: %q", got)
	}
	if len(got) > 6 { // "hé" (3 bytes) + "..." (3 bytes) = 6 max
		t.Errorf("truncate UTF-8 result too long: %q (len=%d)", got, len(got))
	}
}

// --- System prompt verification ---

func TestLLMTaskToolSystemPrompt(t *testing.T) {
	mock := &mockLLMProvider{response: mockResponse(`{}`)}
	tool := newTestLLMTaskTool(t, mock)

	execLLMTaskTool(t, tool, map[string]any{"prompt": "test"})

	if len(mock.lastReq.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(mock.lastReq.Messages))
	}

	systemMsg := mock.lastReq.Messages[0]
	if systemMsg.Role != domain.RoleSystem {
		t.Errorf("first message role = %q, want %q", systemMsg.Role, domain.RoleSystem)
	}
	if !strings.Contains(systemMsg.Content, "JSON-only") {
		t.Error("system prompt should mention JSON-only")
	}
	if !strings.Contains(systemMsg.Content, "Do not call tools") {
		t.Error("system prompt should instruct not to call tools")
	}
}
