package tool

import (
	"context"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"alfred-ai/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Dispatch tests ---

type testParams struct {
	Action string `json:"action"`
	Value  string `json:"value"`
}

func TestDispatch_RoutesToCorrectHandler(t *testing.T) {
	handler := Dispatch(
		func(p testParams) string { return p.Action },
		ActionMap[testParams]{
			"create": func(_ context.Context, p testParams) (any, error) {
				return "created:" + p.Value, nil
			},
			"delete": func(_ context.Context, p testParams) (any, error) {
				return "deleted:" + p.Value, nil
			},
		},
	)

	// Test create
	result, err := handler(context.Background(), trace.SpanFromContext(context.Background()), testParams{Action: "create", Value: "foo"})
	require.NoError(t, err)
	assert.Equal(t, "created:foo", result)

	// Test delete
	result, err = handler(context.Background(), trace.SpanFromContext(context.Background()), testParams{Action: "delete", Value: "bar"})
	require.NoError(t, err)
	assert.Equal(t, "deleted:bar", result)
}

func TestDispatch_UnknownActionReturnsBadAction(t *testing.T) {
	handler := Dispatch(
		func(p testParams) string { return p.Action },
		ActionMap[testParams]{
			"create": func(_ context.Context, _ testParams) (any, error) {
				return nil, nil
			},
			"delete": func(_ context.Context, _ testParams) (any, error) {
				return nil, nil
			},
		},
	)

	_, err := handler(context.Background(), trace.SpanFromContext(context.Background()), testParams{Action: "unknown"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown action "unknown"`)
	// Valid actions should be sorted alphabetically in the error.
	assert.Contains(t, err.Error(), "create, delete")
}

func TestDispatch_EmptyAction(t *testing.T) {
	handler := Dispatch(
		func(p testParams) string { return p.Action },
		ActionMap[testParams]{
			"run": func(_ context.Context, _ testParams) (any, error) {
				return "ran", nil
			},
		},
	)

	_, err := handler(context.Background(), trace.SpanFromContext(context.Background()), testParams{Action: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown action ""`)
}

func TestDispatch_HandlerErrorPropagated(t *testing.T) {
	handler := Dispatch(
		func(p testParams) string { return p.Action },
		ActionMap[testParams]{
			"fail": func(_ context.Context, _ testParams) (any, error) {
				return nil, assert.AnError
			},
		},
	)

	_, err := handler(context.Background(), trace.SpanFromContext(context.Background()), testParams{Action: "fail"})
	assert.ErrorIs(t, err, assert.AnError)
}

func TestDispatch_ValidActionsSorted(t *testing.T) {
	handler := Dispatch(
		func(p testParams) string { return p.Action },
		ActionMap[testParams]{
			"zebra":   func(_ context.Context, _ testParams) (any, error) { return nil, nil },
			"alpha":   func(_ context.Context, _ testParams) (any, error) { return nil, nil },
			"middle":  func(_ context.Context, _ testParams) (any, error) { return nil, nil },
		},
	)

	_, err := handler(context.Background(), trace.SpanFromContext(context.Background()), testParams{Action: "bad"})
	require.Error(t, err)
	// Should list actions alphabetically.
	errMsg := err.Error()
	alphaIdx := strings.Index(errMsg, "alpha")
	middleIdx := strings.Index(errMsg, "middle")
	zebraIdx := strings.Index(errMsg, "zebra")
	assert.Greater(t, middleIdx, alphaIdx, "middle should come after alpha")
	assert.Greater(t, zebraIdx, middleIdx, "zebra should come after middle")
}

// --- PublishToolEvent tests ---

type recordingEventBus struct {
	mu     sync.Mutex
	events []domain.Event
}

func (b *recordingEventBus) Publish(_ context.Context, e domain.Event) {
	b.mu.Lock()
	b.events = append(b.events, e)
	b.mu.Unlock()
}
func (b *recordingEventBus) Subscribe(_ domain.EventType, _ domain.EventHandler) func() {
	return func() {}
}
func (b *recordingEventBus) SubscribeAll(_ domain.EventHandler) func() { return func() {} }
func (b *recordingEventBus) Close()                                    {}

func (b *recordingEventBus) Events() []domain.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := make([]domain.Event, len(b.events))
	copy(cp, b.events)
	return cp
}

func TestPublishToolEvent_WithPayload(t *testing.T) {
	bus := &recordingEventBus{}
	ctx := domain.ContextWithSessionID(context.Background(), "sess-123")

	PublishToolEvent(ctx, bus, domain.EventCanvasCreated, map[string]string{"name": "test"})

	events := bus.Events()
	require.Len(t, events, 1)
	assert.Equal(t, domain.EventCanvasCreated, events[0].Type)
	assert.Equal(t, "sess-123", events[0].SessionID)
	assert.Contains(t, string(events[0].Payload), `"name"`)
}

func TestPublishToolEvent_NilPayload(t *testing.T) {
	bus := &recordingEventBus{}
	ctx := context.Background()

	PublishToolEvent(ctx, bus, domain.EventCanvasHidden, nil)

	events := bus.Events()
	require.Len(t, events, 1)
	assert.Nil(t, events[0].Payload)
}

func TestPublishToolEvent_NilBus(t *testing.T) {
	// Should not panic.
	PublishToolEvent(context.Background(), nil, domain.EventCanvasCreated, nil)
}

func TestPublishToolEvent_NoSessionInContext(t *testing.T) {
	bus := &recordingEventBus{}
	PublishToolEvent(context.Background(), bus, domain.EventToolCallStarted, nil)

	events := bus.Events()
	require.Len(t, events, 1)
	assert.Empty(t, events[0].SessionID)
}
