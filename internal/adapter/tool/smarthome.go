package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"alfred-ai/internal/domain"
)

// Smart Home data types.

// SmartHomeEntity describes a smart home device/entity.
type SmartHomeEntity struct {
	EntityID   string         `json:"entity_id"`
	State      string         `json:"state"`
	Attributes map[string]any `json:"attributes,omitempty"`
	LastChanged string        `json:"last_changed,omitempty"`
}

// SmartHomeState is a historical state entry.
type SmartHomeState struct {
	State       string `json:"state"`
	LastChanged string `json:"last_changed"`
}

// HistoryOpts controls history retrieval.
type HistoryOpts struct {
	StartTime string `json:"start_time,omitempty"` // ISO 8601
	EndTime   string `json:"end_time,omitempty"`   // ISO 8601
}

// SmartHomeAutomation describes an automation.
type SmartHomeAutomation struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// SmartHomeBackend abstracts smart home operations.
type SmartHomeBackend interface {
	ListEntities(ctx context.Context) ([]SmartHomeEntity, error)
	GetEntity(ctx context.Context, entityID string) (*SmartHomeEntity, error)
	CallService(ctx context.Context, domain, service, entityID string, data map[string]any) error
	GetHistory(ctx context.Context, entityID string, opts HistoryOpts) ([]SmartHomeState, error)
	ListAutomations(ctx context.Context) ([]SmartHomeAutomation, error)
	TriggerAutomation(ctx context.Context, automationID string) error
}

// MockSmartHomeBackend is a no-op backend for testing/development.
type MockSmartHomeBackend struct {
	entities    []SmartHomeEntity
	automations []SmartHomeAutomation
	history     map[string][]SmartHomeState
	serviceCalls []serviceCall
}

type serviceCall struct {
	Domain   string
	Service  string
	EntityID string
	Data     map[string]any
}

// NewMockSmartHomeBackend creates a mock smart home backend.
func NewMockSmartHomeBackend() *MockSmartHomeBackend {
	return &MockSmartHomeBackend{
		history: make(map[string][]SmartHomeState),
	}
}

func (m *MockSmartHomeBackend) ListEntities(_ context.Context) ([]SmartHomeEntity, error) {
	return m.entities, nil
}

func (m *MockSmartHomeBackend) GetEntity(_ context.Context, entityID string) (*SmartHomeEntity, error) {
	for _, e := range m.entities {
		if e.EntityID == entityID {
			return &e, nil
		}
	}
	return nil, fmt.Errorf("entity %q not found", entityID)
}

func (m *MockSmartHomeBackend) CallService(_ context.Context, dom, service, entityID string, data map[string]any) error {
	m.serviceCalls = append(m.serviceCalls, serviceCall{Domain: dom, Service: service, EntityID: entityID, Data: data})
	return nil
}

func (m *MockSmartHomeBackend) GetHistory(_ context.Context, entityID string, _ HistoryOpts) ([]SmartHomeState, error) {
	return m.history[entityID], nil
}

func (m *MockSmartHomeBackend) ListAutomations(_ context.Context) ([]SmartHomeAutomation, error) {
	return m.automations, nil
}

func (m *MockSmartHomeBackend) TriggerAutomation(_ context.Context, automationID string) error {
	for _, a := range m.automations {
		if a.ID == automationID {
			return nil
		}
	}
	return fmt.Errorf("automation %q not found", automationID)
}

// SmartHomeTool provides smart home control to the LLM.
type SmartHomeTool struct {
	backend     SmartHomeBackend
	logger      *slog.Logger
	rateLimiter *RateLimiter

	// TTL cache for list_entities.
	mu            sync.Mutex
	entitiesCache []SmartHomeEntity
	cacheTime     time.Time
	cacheTTL      time.Duration
}

// NewSmartHomeTool creates a smart home tool. If backend is nil, a MockSmartHomeBackend is used.
func NewSmartHomeTool(
	backend SmartHomeBackend,
	url string,
	token string,
	timeout time.Duration,
	maxCallsPerMin int,
	logger *slog.Logger,
) *SmartHomeTool {
	if backend == nil {
		backend = NewMockSmartHomeBackend()
	}
	return &SmartHomeTool{
		backend:     backend,
		logger:      logger,
		rateLimiter: NewRateLimiter(maxCallsPerMin, time.Minute),
		cacheTTL:    time.Minute,
	}
}

func (t *SmartHomeTool) Name() string { return "smart_home" }
func (t *SmartHomeTool) Description() string {
	return "Control smart home devices: list entities, get entity state, call services (turn on/off, etc.), " +
		"view history, list automations, and trigger automations."
}

func (t *SmartHomeTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["list_entities", "get_entity", "call_service", "get_history", "list_automations", "trigger_automation"],
					"description": "The smart home action to perform"
				},
				"entity_id": {
					"type": "string",
					"description": "Entity ID (e.g. light.living_room)"
				},
				"domain": {
					"type": "string",
					"description": "Service domain (e.g. light, switch, climate)"
				},
				"service": {
					"type": "string",
					"description": "Service name (e.g. turn_on, turn_off, toggle)"
				},
				"service_data": {
					"type": "object",
					"description": "Additional data for the service call"
				},
				"automation_id": {
					"type": "string",
					"description": "Automation ID for trigger_automation"
				},
				"start_time": {
					"type": "string",
					"description": "History start time (ISO 8601)"
				},
				"end_time": {
					"type": "string",
					"description": "History end time (ISO 8601)"
				}
			},
			"required": ["action"]
		}`),
	}
}

type smartHomeParams struct {
	Action       string         `json:"action"`
	EntityID     string         `json:"entity_id,omitempty"`
	Domain       string         `json:"domain,omitempty"`
	Service      string         `json:"service,omitempty"`
	ServiceData  map[string]any `json:"service_data,omitempty"`
	AutomationID string         `json:"automation_id,omitempty"`
	StartTime    string         `json:"start_time,omitempty"`
	EndTime      string         `json:"end_time,omitempty"`
}

func (t *SmartHomeTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	return Execute(ctx, "tool.smart_home", t.logger, params,
		Dispatch(func(p smartHomeParams) string { return p.Action }, ActionMap[smartHomeParams]{
			"list_entities":      t.handleListEntities,
			"get_entity":         t.handleGetEntity,
			"call_service":       t.handleCallService,
			"get_history":        t.handleGetHistory,
			"list_automations":   t.handleListAutomations,
			"trigger_automation": t.handleTriggerAutomation,
		}),
	)
}

func (t *SmartHomeTool) checkRateLimit() error {
	if !t.rateLimiter.Allow() {
		return domain.ErrRateLimit
	}
	return nil
}

func (t *SmartHomeTool) handleListEntities(ctx context.Context, _ smartHomeParams) (any, error) {
	if err := t.checkRateLimit(); err != nil {
		return nil, err
	}

	// Check cache.
	t.mu.Lock()
	if t.entitiesCache != nil && time.Since(t.cacheTime) < t.cacheTTL {
		cached := t.entitiesCache
		t.mu.Unlock()
		return cached, nil
	}
	t.mu.Unlock()

	entities, err := t.backend.ListEntities(ctx)
	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	t.entitiesCache = entities
	t.cacheTime = time.Now()
	t.mu.Unlock()

	if len(entities) == 0 {
		return TextResult("No entities found."), nil
	}
	return entities, nil
}

func (t *SmartHomeTool) handleGetEntity(ctx context.Context, p smartHomeParams) (any, error) {
	if err := t.checkRateLimit(); err != nil {
		return nil, err
	}
	if err := RequireField("entity_id", p.EntityID); err != nil {
		return nil, err
	}
	return t.backend.GetEntity(ctx, p.EntityID)
}

func (t *SmartHomeTool) handleCallService(ctx context.Context, p smartHomeParams) (any, error) {
	if err := t.checkRateLimit(); err != nil {
		return nil, err
	}
	if err := RequireFields("domain", p.Domain, "service", p.Service, "entity_id", p.EntityID); err != nil {
		return nil, err
	}
	if err := t.backend.CallService(ctx, p.Domain, p.Service, p.EntityID, p.ServiceData); err != nil {
		return nil, err
	}
	t.logger.Info("service called", "domain", p.Domain, "service", p.Service, "entity_id", p.EntityID)
	return TextResult(fmt.Sprintf("Service %s.%s called on %s", p.Domain, p.Service, p.EntityID)), nil
}

func (t *SmartHomeTool) handleGetHistory(ctx context.Context, p smartHomeParams) (any, error) {
	if err := t.checkRateLimit(); err != nil {
		return nil, err
	}
	if err := RequireField("entity_id", p.EntityID); err != nil {
		return nil, err
	}
	history, err := t.backend.GetHistory(ctx, p.EntityID, HistoryOpts{
		StartTime: p.StartTime, EndTime: p.EndTime,
	})
	if err != nil {
		return nil, err
	}
	if len(history) == 0 {
		return TextResult("No history found for entity."), nil
	}
	return history, nil
}

func (t *SmartHomeTool) handleListAutomations(ctx context.Context, _ smartHomeParams) (any, error) {
	if err := t.checkRateLimit(); err != nil {
		return nil, err
	}
	automations, err := t.backend.ListAutomations(ctx)
	if err != nil {
		return nil, err
	}
	if len(automations) == 0 {
		return TextResult("No automations found."), nil
	}
	return automations, nil
}

func (t *SmartHomeTool) handleTriggerAutomation(ctx context.Context, p smartHomeParams) (any, error) {
	if err := t.checkRateLimit(); err != nil {
		return nil, err
	}
	if err := RequireField("automation_id", p.AutomationID); err != nil {
		return nil, err
	}
	if err := t.backend.TriggerAutomation(ctx, p.AutomationID); err != nil {
		return nil, err
	}
	t.logger.Info("automation triggered", "automation_id", p.AutomationID)
	return TextResult(fmt.Sprintf("Automation %q triggered", p.AutomationID)), nil
}
