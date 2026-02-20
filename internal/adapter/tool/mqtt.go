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

// MQTTBackend abstracts MQTT operations for testability.
type MQTTBackend interface {
	Publish(ctx context.Context, topic string, payload []byte, qos byte, retain bool) error
	Subscribe(ctx context.Context, topic string) (<-chan MQTTMessage, error)
	Unsubscribe(topic string) error
	ListSubscriptions() []string
	Close() error
}

// MQTTMessage is a message received from an MQTT subscription.
type MQTTMessage struct {
	Topic   string `json:"topic"`
	Payload string `json:"payload"`
	QoS     byte   `json:"qos"`
}

// MQTTTool provides MQTT pub/sub for IoT device communication.
type MQTTTool struct {
	backend MQTTBackend
	logger  *slog.Logger
	mu      sync.Mutex
	buffers map[string][]MQTTMessage // topic → buffered messages
}

// NewMQTTTool creates an MQTT tool backed by the given backend.
func NewMQTTTool(backend MQTTBackend, logger *slog.Logger) *MQTTTool {
	return &MQTTTool{
		backend: backend,
		logger:  logger,
		buffers: make(map[string][]MQTTMessage),
	}
}

func (t *MQTTTool) Name() string        { return "mqtt" }
func (t *MQTTTool) Description() string  { return "Publish and subscribe to MQTT topics for IoT device communication." }

func (t *MQTTTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["publish", "subscribe", "unsubscribe", "read", "list_subscriptions"],
					"description": "The MQTT action to perform."
				},
				"topic": {
					"type": "string",
					"description": "The MQTT topic (required for publish, subscribe, unsubscribe, read)."
				},
				"payload": {
					"type": "string",
					"description": "The message payload (required for publish)."
				},
				"qos": {
					"type": "integer",
					"enum": [0, 1, 2],
					"description": "Quality of Service level (0=at most once, 1=at least once, 2=exactly once). Default: 0."
				},
				"retain": {
					"type": "boolean",
					"description": "Whether the broker should retain the message. Default: false."
				}
			},
			"required": ["action"]
		}`),
	}
}

type mqttParams struct {
	Action  string `json:"action"`
	Topic   string `json:"topic"`
	Payload string `json:"payload"`
	QoS     int    `json:"qos"`
	Retain  bool   `json:"retain"`
}

func (t *MQTTTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	p, errResult := ParseParams[mqttParams](params)
	if errResult != nil {
		return errResult, nil
	}

	switch p.Action {
	case "publish":
		return t.publish(ctx, p)
	case "subscribe":
		return t.subscribe(ctx, p)
	case "unsubscribe":
		return t.unsubscribe(p)
	case "read":
		return t.read(p)
	case "list_subscriptions":
		return t.listSubscriptions()
	default:
		return &domain.ToolResult{
			Content: fmt.Sprintf("unknown action %q (want: publish, subscribe, unsubscribe, read, list_subscriptions)", p.Action),
			IsError: true,
		}, nil
	}
}

func (t *MQTTTool) publish(ctx context.Context, p mqttParams) (*domain.ToolResult, error) {
	if err := RequireField("topic", p.Topic); err != nil {
		return &domain.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	qos := byte(p.QoS)
	if qos > 2 {
		qos = 0
	}
	if err := t.backend.Publish(ctx, p.Topic, []byte(p.Payload), qos, p.Retain); err != nil {
		return &domain.ToolResult{Content: "publish failed: " + err.Error(), IsError: true}, nil
	}
	t.logger.Info("mqtt publish", "topic", p.Topic, "qos", qos, "retain", p.Retain)
	return &domain.ToolResult{Content: fmt.Sprintf("Published to %q (qos=%d, retain=%v)", p.Topic, qos, p.Retain)}, nil
}

func (t *MQTTTool) subscribe(ctx context.Context, p mqttParams) (*domain.ToolResult, error) {
	if err := RequireField("topic", p.Topic); err != nil {
		return &domain.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	ch, err := t.backend.Subscribe(ctx, p.Topic)
	if err != nil {
		return &domain.ToolResult{Content: "subscribe failed: " + err.Error(), IsError: true}, nil
	}

	// Start goroutine to buffer incoming messages.
	go func() {
		for msg := range ch {
			t.mu.Lock()
			t.buffers[p.Topic] = append(t.buffers[p.Topic], msg)
			// Keep buffer bounded.
			if len(t.buffers[p.Topic]) > 100 {
				t.buffers[p.Topic] = t.buffers[p.Topic][len(t.buffers[p.Topic])-100:]
			}
			t.mu.Unlock()
		}
	}()

	t.logger.Info("mqtt subscribe", "topic", p.Topic)
	return &domain.ToolResult{Content: fmt.Sprintf("Subscribed to %q", p.Topic)}, nil
}

func (t *MQTTTool) unsubscribe(p mqttParams) (*domain.ToolResult, error) {
	if err := RequireField("topic", p.Topic); err != nil {
		return &domain.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	if err := t.backend.Unsubscribe(p.Topic); err != nil {
		return &domain.ToolResult{Content: "unsubscribe failed: " + err.Error(), IsError: true}, nil
	}
	t.mu.Lock()
	delete(t.buffers, p.Topic)
	t.mu.Unlock()
	t.logger.Info("mqtt unsubscribe", "topic", p.Topic)
	return &domain.ToolResult{Content: fmt.Sprintf("Unsubscribed from %q", p.Topic)}, nil
}

func (t *MQTTTool) read(p mqttParams) (*domain.ToolResult, error) {
	if err := RequireField("topic", p.Topic); err != nil {
		return &domain.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	t.mu.Lock()
	msgs := t.buffers[p.Topic]
	t.buffers[p.Topic] = nil // drain after read
	t.mu.Unlock()

	if len(msgs) == 0 {
		return &domain.ToolResult{Content: fmt.Sprintf("No messages on %q", p.Topic)}, nil
	}
	data, _ := json.Marshal(msgs)
	return &domain.ToolResult{Content: string(data)}, nil
}

func (t *MQTTTool) listSubscriptions() (*domain.ToolResult, error) {
	subs := t.backend.ListSubscriptions()
	if len(subs) == 0 {
		return &domain.ToolResult{Content: "No active subscriptions"}, nil
	}
	data, _ := json.Marshal(subs)
	return &domain.ToolResult{Content: string(data)}, nil
}

// --- Mock backend for testing ---

// MockMQTTBackend is a test double for MQTTBackend.
type MockMQTTBackend struct {
	mu            sync.Mutex
	published     []MQTTMessage
	subscriptions map[string]chan MQTTMessage
}

// NewMockMQTTBackend creates a mock MQTT backend.
func NewMockMQTTBackend() *MockMQTTBackend {
	return &MockMQTTBackend{
		subscriptions: make(map[string]chan MQTTMessage),
	}
}

func (m *MockMQTTBackend) Publish(_ context.Context, topic string, payload []byte, qos byte, _ bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg := MQTTMessage{Topic: topic, Payload: string(payload), QoS: qos}
	m.published = append(m.published, msg)

	// Deliver to subscribers.
	if ch, ok := m.subscriptions[topic]; ok {
		select {
		case ch <- msg:
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil
}

func (m *MockMQTTBackend) Subscribe(_ context.Context, topic string) (<-chan MQTTMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan MQTTMessage, 16)
	m.subscriptions[topic] = ch
	return ch, nil
}

func (m *MockMQTTBackend) Unsubscribe(topic string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ch, ok := m.subscriptions[topic]; ok {
		close(ch)
		delete(m.subscriptions, topic)
	}
	return nil
}

func (m *MockMQTTBackend) ListSubscriptions() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var topics []string
	for t := range m.subscriptions {
		topics = append(topics, t)
	}
	return topics
}

func (m *MockMQTTBackend) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for topic, ch := range m.subscriptions {
		close(ch)
		delete(m.subscriptions, topic)
	}
	return nil
}

// Published returns the list of published messages (for test assertions).
func (m *MockMQTTBackend) Published() []MQTTMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]MQTTMessage{}, m.published...)
}
