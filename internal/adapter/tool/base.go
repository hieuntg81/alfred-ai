package tool

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"go.opentelemetry.io/otel/trace"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/tracer"
)

// ActionHandler is a function that handles a single action for a tool.
type ActionHandler[P any] func(ctx context.Context, p P) (any, error)

// ActionMap maps action names to their handlers for an action-based tool.
type ActionMap[P any] map[string]ActionHandler[P]

// Dispatch creates a handler function for Execute[P] that routes by action name.
// The getAction function extracts the action string from the params struct.
//
// Usage:
//
//	func (t *FooTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
//	    return Execute(ctx, "tool.foo", t.logger, params,
//	        Dispatch(func(p fooParams) string { return p.Action }, ActionMap[fooParams]{
//	            "create": t.handleCreate,
//	            "list":   t.handleList,
//	            "delete": t.handleDelete,
//	        }),
//	    )
//	}
func Dispatch[P any](
	getAction func(P) string,
	actions ActionMap[P],
) func(ctx context.Context, span trace.Span, p P) (any, error) {
	// Pre-compute sorted action names for deterministic BadAction messages.
	validActions := make([]string, 0, len(actions))
	for name := range actions {
		validActions = append(validActions, name)
	}
	sort.Strings(validActions)

	return func(ctx context.Context, span trace.Span, p P) (any, error) {
		action := getAction(p)
		span.SetAttributes(tracer.StringAttr("tool.action", action))

		handler, ok := actions[action]
		if !ok {
			return nil, BadAction(action, validActions...)
		}
		return handler(ctx, p)
	}
}

// PublishToolEvent publishes a domain event on the event bus from a tool.
// If the bus is nil, this is a no-op. The session ID is extracted from the context.
func PublishToolEvent(ctx context.Context, bus domain.EventBus, eventType domain.EventType, payload any) {
	if bus == nil {
		return
	}
	var raw json.RawMessage
	if payload != nil {
		data, err := json.Marshal(payload)
		if err == nil {
			raw = data
		}
	}
	bus.Publish(ctx, domain.Event{
		Type:      eventType,
		Timestamp: time.Now(),
		SessionID: domain.SessionIDFromContext(ctx),
		Payload:   raw,
	})
}
