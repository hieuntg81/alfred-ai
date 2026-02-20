package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

// --- test backend ---

type testSmartHomeBackend struct {
	entities    []SmartHomeEntity
	automations []SmartHomeAutomation
	history     map[string][]SmartHomeState
	calls       []serviceCall

	listEntitiesErr      error
	getEntityErr         error
	callServiceErr       error
	getHistoryErr        error
	listAutomationsErr   error
	triggerAutomationErr error
}

func newTestSmartHomeBackend() *testSmartHomeBackend {
	return &testSmartHomeBackend{
		history: make(map[string][]SmartHomeState),
	}
}

func (b *testSmartHomeBackend) ListEntities(_ context.Context) ([]SmartHomeEntity, error) {
	if b.listEntitiesErr != nil {
		return nil, b.listEntitiesErr
	}
	return b.entities, nil
}

func (b *testSmartHomeBackend) GetEntity(_ context.Context, entityID string) (*SmartHomeEntity, error) {
	if b.getEntityErr != nil {
		return nil, b.getEntityErr
	}
	for _, e := range b.entities {
		if e.EntityID == entityID {
			return &e, nil
		}
	}
	return nil, fmt.Errorf("entity %q not found", entityID)
}

func (b *testSmartHomeBackend) CallService(_ context.Context, dom, service, entityID string, data map[string]any) error {
	if b.callServiceErr != nil {
		return b.callServiceErr
	}
	b.calls = append(b.calls, serviceCall{Domain: dom, Service: service, EntityID: entityID, Data: data})
	return nil
}

func (b *testSmartHomeBackend) GetHistory(_ context.Context, entityID string, _ HistoryOpts) ([]SmartHomeState, error) {
	if b.getHistoryErr != nil {
		return nil, b.getHistoryErr
	}
	return b.history[entityID], nil
}

func (b *testSmartHomeBackend) ListAutomations(_ context.Context) ([]SmartHomeAutomation, error) {
	if b.listAutomationsErr != nil {
		return nil, b.listAutomationsErr
	}
	return b.automations, nil
}

func (b *testSmartHomeBackend) TriggerAutomation(_ context.Context, automationID string) error {
	if b.triggerAutomationErr != nil {
		return b.triggerAutomationErr
	}
	for _, a := range b.automations {
		if a.ID == automationID {
			return nil
		}
	}
	return fmt.Errorf("automation %q not found", automationID)
}

// --- helpers ---

func newTestSmartHomeTool(t *testing.T) (*SmartHomeTool, *testSmartHomeBackend) {
	t.Helper()
	b := newTestSmartHomeBackend()
	tool := NewSmartHomeTool(b, "http://ha:8123", "token", 10*time.Second, 1000, newTestLogger())
	return tool, b
}

func execSmartHomeTool(t *testing.T, tool *SmartHomeTool, params any) *domain.ToolResult {
	t.Helper()
	data, _ := json.Marshal(params)
	result, err := tool.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return result
}

// --- metadata ---

func TestSmartHomeToolName(t *testing.T) {
	tool, _ := newTestSmartHomeTool(t)
	if tool.Name() != "smart_home" {
		t.Errorf("got %q, want %q", tool.Name(), "smart_home")
	}
}

func TestSmartHomeToolDescription(t *testing.T) {
	tool, _ := newTestSmartHomeTool(t)
	if tool.Description() == "" {
		t.Error("description should not be empty")
	}
}

func TestSmartHomeToolSchema(t *testing.T) {
	tool, _ := newTestSmartHomeTool(t)
	schema := tool.Schema()
	if schema.Name != "smart_home" {
		t.Errorf("schema name: got %q, want %q", schema.Name, "smart_home")
	}
	var params map[string]any
	if err := json.Unmarshal(schema.Parameters, &params); err != nil {
		t.Fatalf("invalid schema JSON: %v", err)
	}
}

// --- action success tests ---

func TestSmartHomeToolListEntities(t *testing.T) {
	tool, backend := newTestSmartHomeTool(t)
	backend.entities = []SmartHomeEntity{{EntityID: "light.living_room", State: "on"}}
	result := execSmartHomeTool(t, tool, map[string]any{"action": "list_entities"})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "light.living_room") {
		t.Errorf("expected entity: %s", result.Content)
	}
}

func TestSmartHomeToolListEntitiesEmpty(t *testing.T) {
	tool, _ := newTestSmartHomeTool(t)
	result := execSmartHomeTool(t, tool, map[string]any{"action": "list_entities"})
	if !strings.Contains(result.Content, "No entities") {
		t.Errorf("expected empty message: %s", result.Content)
	}
}

func TestSmartHomeToolListEntitiesCache(t *testing.T) {
	tool, backend := newTestSmartHomeTool(t)
	backend.entities = []SmartHomeEntity{{EntityID: "light.a", State: "on"}}

	execSmartHomeTool(t, tool, map[string]any{"action": "list_entities"})

	backend.entities = []SmartHomeEntity{{EntityID: "light.b", State: "off"}}
	result := execSmartHomeTool(t, tool, map[string]any{"action": "list_entities"})
	if strings.Contains(result.Content, "light.b") {
		t.Error("expected cached result, got fresh data")
	}
}

func TestSmartHomeToolGetEntity(t *testing.T) {
	tool, backend := newTestSmartHomeTool(t)
	backend.entities = []SmartHomeEntity{{EntityID: "switch.fan", State: "off"}}
	result := execSmartHomeTool(t, tool, map[string]any{
		"action": "get_entity", "entity_id": "switch.fan",
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "switch.fan") {
		t.Errorf("expected entity data: %s", result.Content)
	}
}

func TestSmartHomeToolCallService(t *testing.T) {
	tool, backend := newTestSmartHomeTool(t)
	result := execSmartHomeTool(t, tool, map[string]any{
		"action": "call_service", "domain": "light", "service": "turn_on",
		"entity_id": "light.kitchen",
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "light.turn_on") {
		t.Errorf("expected service call confirmation: %s", result.Content)
	}
	if len(backend.calls) != 1 {
		t.Errorf("expected 1 call, got %d", len(backend.calls))
	}
}

func TestSmartHomeToolCallServiceWithData(t *testing.T) {
	tool, backend := newTestSmartHomeTool(t)
	result := execSmartHomeTool(t, tool, map[string]any{
		"action": "call_service", "domain": "light", "service": "turn_on",
		"entity_id": "light.desk", "service_data": map[string]any{"brightness": 128},
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if len(backend.calls) != 1 || backend.calls[0].Data["brightness"] != float64(128) {
		t.Error("expected service data to be passed through")
	}
}

func TestSmartHomeToolGetHistory(t *testing.T) {
	tool, backend := newTestSmartHomeTool(t)
	backend.history["sensor.temp"] = []SmartHomeState{
		{State: "22.5", LastChanged: "2025-01-01T10:00:00Z"},
	}
	result := execSmartHomeTool(t, tool, map[string]any{
		"action": "get_history", "entity_id": "sensor.temp",
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "22.5") {
		t.Errorf("expected history data: %s", result.Content)
	}
}

func TestSmartHomeToolGetHistoryEmpty(t *testing.T) {
	tool, _ := newTestSmartHomeTool(t)
	result := execSmartHomeTool(t, tool, map[string]any{
		"action": "get_history", "entity_id": "sensor.x",
	})
	if !strings.Contains(result.Content, "No history") {
		t.Errorf("expected empty message: %s", result.Content)
	}
}

func TestSmartHomeToolListAutomations(t *testing.T) {
	tool, backend := newTestSmartHomeTool(t)
	backend.automations = []SmartHomeAutomation{{ID: "auto-1", Name: "Night Mode", Enabled: true}}
	result := execSmartHomeTool(t, tool, map[string]any{"action": "list_automations"})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Night Mode") {
		t.Errorf("expected automation: %s", result.Content)
	}
}

func TestSmartHomeToolListAutomationsEmpty(t *testing.T) {
	tool, _ := newTestSmartHomeTool(t)
	result := execSmartHomeTool(t, tool, map[string]any{"action": "list_automations"})
	if !strings.Contains(result.Content, "No automations") {
		t.Errorf("expected empty message: %s", result.Content)
	}
}

func TestSmartHomeToolTriggerAutomation(t *testing.T) {
	tool, backend := newTestSmartHomeTool(t)
	backend.automations = []SmartHomeAutomation{{ID: "auto-1", Name: "Night Mode"}}
	result := execSmartHomeTool(t, tool, map[string]any{
		"action": "trigger_automation", "automation_id": "auto-1",
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "triggered") {
		t.Errorf("expected trigger confirmation: %s", result.Content)
	}
}

// --- validation error tests ---

func TestSmartHomeToolGetEntityMissing(t *testing.T) {
	tool, _ := newTestSmartHomeTool(t)
	result := execSmartHomeTool(t, tool, map[string]any{"action": "get_entity"})
	if !result.IsError {
		t.Error("expected error for missing entity_id")
	}
}

func TestSmartHomeToolCallServiceMissing(t *testing.T) {
	tool, _ := newTestSmartHomeTool(t)
	result := execSmartHomeTool(t, tool, map[string]any{"action": "call_service"})
	if !result.IsError {
		t.Error("expected error for missing fields")
	}
}

func TestSmartHomeToolGetHistoryMissing(t *testing.T) {
	tool, _ := newTestSmartHomeTool(t)
	result := execSmartHomeTool(t, tool, map[string]any{"action": "get_history"})
	if !result.IsError {
		t.Error("expected error for missing entity_id")
	}
}

func TestSmartHomeToolTriggerAutomationMissing(t *testing.T) {
	tool, _ := newTestSmartHomeTool(t)
	result := execSmartHomeTool(t, tool, map[string]any{"action": "trigger_automation"})
	if !result.IsError {
		t.Error("expected error for missing automation_id")
	}
}

func TestSmartHomeToolUnknownAction(t *testing.T) {
	tool, _ := newTestSmartHomeTool(t)
	result := execSmartHomeTool(t, tool, map[string]any{"action": "bad"})
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

func TestSmartHomeToolInvalidJSON(t *testing.T) {
	tool, _ := newTestSmartHomeTool(t)
	result, err := tool.Execute(context.Background(), []byte(`{invalid`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

// --- rate limiting ---

func TestSmartHomeToolRateLimit(t *testing.T) {
	b := newTestSmartHomeBackend()
	tool := NewSmartHomeTool(b, "http://ha:8123", "t", 10*time.Second, 2, newTestLogger())
	b.entities = []SmartHomeEntity{{EntityID: "a"}}

	execSmartHomeTool(t, tool, map[string]any{"action": "list_entities"})
	execSmartHomeTool(t, tool, map[string]any{"action": "get_entity", "entity_id": "a"})

	result := execSmartHomeTool(t, tool, map[string]any{"action": "get_entity", "entity_id": "a"})
	if !result.IsError {
		t.Error("expected rate limit error")
	}
}

// --- backend error propagation ---

func TestSmartHomeToolBackendListEntitiesError(t *testing.T) {
	tool, backend := newTestSmartHomeTool(t)
	backend.listEntitiesErr = fmt.Errorf("api error")
	result := execSmartHomeTool(t, tool, map[string]any{"action": "list_entities"})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

func TestSmartHomeToolBackendGetEntityError(t *testing.T) {
	tool, backend := newTestSmartHomeTool(t)
	backend.getEntityErr = fmt.Errorf("api error")
	result := execSmartHomeTool(t, tool, map[string]any{
		"action": "get_entity", "entity_id": "x",
	})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

func TestSmartHomeToolBackendCallServiceError(t *testing.T) {
	tool, backend := newTestSmartHomeTool(t)
	backend.callServiceErr = fmt.Errorf("ha error")
	result := execSmartHomeTool(t, tool, map[string]any{
		"action": "call_service", "domain": "light", "service": "turn_on", "entity_id": "x",
	})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

func TestSmartHomeToolBackendTriggerAutomationError(t *testing.T) {
	tool, backend := newTestSmartHomeTool(t)
	backend.triggerAutomationErr = fmt.Errorf("ha error")
	result := execSmartHomeTool(t, tool, map[string]any{
		"action": "trigger_automation", "automation_id": "a1",
	})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

func TestSmartHomeToolNilBackendUsesMock(t *testing.T) {
	tool := NewSmartHomeTool(nil, "", "", 10*time.Second, 100, newTestLogger())
	result := execSmartHomeTool(t, tool, map[string]any{"action": "list_entities"})
	if result.IsError {
		t.Fatalf("expected success with mock: %s", result.Content)
	}
}

// --- edge cases ---

func TestSmartHomeToolGetEntityNotFound(t *testing.T) {
	tool, _ := newTestSmartHomeTool(t)
	result := execSmartHomeTool(t, tool, map[string]any{
		"action": "get_entity", "entity_id": "missing",
	})
	if !result.IsError {
		t.Error("expected not found error")
	}
}

func TestSmartHomeToolTriggerAutomationNotFound(t *testing.T) {
	tool, _ := newTestSmartHomeTool(t)
	result := execSmartHomeTool(t, tool, map[string]any{
		"action": "trigger_automation", "automation_id": "missing",
	})
	if !result.IsError {
		t.Error("expected not found error")
	}
}

// --- fuzz ---

func FuzzSmartHomeTool_Execute(f *testing.F) {
	f.Add([]byte(`{"action":"list_entities"}`))
	f.Add([]byte(`{"action":"call_service","domain":"light","service":"turn_on","entity_id":"x"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`invalid`))

	b := newTestSmartHomeBackend()
	tool := NewSmartHomeTool(b, "", "", 10*time.Second, 10000, newTestLogger())
	f.Fuzz(func(t *testing.T, data []byte) {
		tool.Execute(context.Background(), data)
	})
}
